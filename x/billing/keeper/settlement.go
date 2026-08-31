package keeper

import (
	"context"
	"errors"
	"time"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// SettlementResult contains the results of a settlement operation.
type SettlementResult struct {
	// TransferAmounts is the actual amount transferred to the provider.
	TransferAmounts sdk.Coins
	// AccruedAmounts contains exact representable accruals plus the remaining
	// balance for each overflowed denom (and may exceed transfers when credit is insufficient).
	AccruedAmounts sdk.Coins
	// CreditBalanceAfter is the tenant's credit balance after the settlement.
	CreditBalanceAfter sdk.Coins
	// AccrualOverflow lists denoms whose mathematical accrued amount exceeded
	// math.Int. For silent settlement, only these denoms are clamped to their
	// remaining credit balance. Order is deterministic first-detected overflow order.
	AccrualOverflow []string
}

// leaseItemDenoms returns the unique denoms used by a lease's items.
func leaseItemDenoms(items []types.LeaseItem) []string {
	seen := make(map[string]struct{}, len(items))
	denoms := make([]string, 0, len(items))
	for _, item := range items {
		if _, ok := seen[item.LockedPrice.Denom]; !ok {
			seen[item.LockedPrice.Denom] = struct{}{}
			denoms = append(denoms, item.LockedPrice.Denom)
		}
	}
	return denoms
}

// LeaseItemsToWithPrice converts lease items to LeaseItemWithPrice for accrual calculations.
func LeaseItemsToWithPrice(items []types.LeaseItem) []LeaseItemWithPrice {
	result := make([]LeaseItemWithPrice, 0, len(items))
	for _, item := range items {
		result = append(result, LeaseItemWithPrice{
			SkuUUID:              item.SkuUuid,
			Quantity:             item.Quantity,
			LockedPricePerSecond: item.LockedPrice,
		})
	}
	return result
}

// CalculateTransferAmounts returns the minimum of accrued and available for each denom.
// This ensures we never try to transfer more than what's available in the credit account.
func CalculateTransferAmounts(accrued, available sdk.Coins) sdk.Coins {
	return accrued.Min(available)
}

// PerformSettlement calculates and transfers accrued amounts from a tenant's credit account
// to the provider's payout address. This is the core settlement logic used by all settlement operations.
//
// Parameters:
//   - ctx: the context
//   - lease: the lease to settle (LastSettledAt will NOT be modified - caller must handle this)
//   - settleTime: the time to settle up to
//
// Returns:
//   - SettlementResult containing transfer amounts and balances
//   - error if settlement fails (including accrual calculation errors)
//
// Note: This function does NOT update the lease's LastSettledAt - the caller is responsible
// for updating the lease state after a successful settlement.
func (k *Keeper) PerformSettlement(ctx context.Context, lease *types.Lease, settleTime time.Time) (*SettlementResult, error) {
	return k.performSettlementCore(ctx, lease, settleTime, false)
}

// PerformSettlementSilent is like PerformSettlement but, on a representational
// accrual overflow, retains exact totals for unaffected denoms and clamps each
// overflowed denom to its remaining balance instead of returning an error. This
// is used by close operations so an unrepresentably large charge cannot grant
// free service or drain an unaffected denomination.
func (k *Keeper) PerformSettlementSilent(ctx context.Context, lease *types.Lease, settleTime time.Time) (*SettlementResult, error) {
	return k.performSettlementCore(ctx, lease, settleTime, true)
}

// performSettlementCore contains the shared settlement logic.
// If silentOnOverflow is true, only ErrArithmeticOverflow is handled
// conservatively; malformed pricing and other calculation errors are returned.
func (k *Keeper) performSettlementCore(ctx context.Context, lease *types.Lease, settleTime time.Time, silentOnOverflow bool) (*SettlementResult, error) {
	// Compare timestamps before deriving seconds so a historical interval beyond
	// time.Duration's range cannot saturate and undercharge the lease.
	if !settleTime.After(lease.LastSettledAt) {
		return &SettlementResult{
			TransferAmounts:    sdk.NewCoins(),
			AccruedAmounts:     sdk.NewCoins(),
			CreditBalanceAfter: sdk.NewCoins(),
		}, nil
	}
	durationSeconds, err := elapsedWholeSeconds(lease.LastSettledAt, settleTime)
	if err != nil {
		return nil, err
	}
	duration := settleTime.Sub(lease.LastSettledAt) // logging only; may saturate

	// Calculate accrued amounts
	items := LeaseItemsToWithPrice(lease.Items)
	accruedAmounts, err := calculateTotalAccruedForLeaseSeconds(items, durationSeconds)
	var accrualOverflow *AccrualOverflowError
	if err != nil {
		if errors.As(err, &accrualOverflow) && silentOnOverflow {
			// On overflow, the accrued amount exceeds representable range.
			// For each affected denom, accrued exceeds every representable bank
			// balance. Clamp only those denoms to their remaining credit rather
			// than granting free service or draining unaffected denoms.
			k.logger.Warn("accrual calculation overflow during settlement, will transfer remaining credit",
				"lease_uuid", lease.Uuid,
				"tenant", lease.Tenant,
				"duration", duration.String(),
				"denoms", accrualOverflow.Denoms,
				"error", err,
			)
		} else {
			return nil, errorsmod.Wrap(err, "accrual calculation error")
		}
	}

	// Get credit balances for only the denoms used by this lease.
	// This avoids loading dust from unrelated token sends to the credit address.
	creditBalances, err := k.getCreditBalancesForDenoms(ctx, lease.Tenant, leaseItemDenoms(lease.Items))
	if err != nil {
		return nil, err
	}

	if accrualOverflow != nil {
		for _, denom := range accrualOverflow.Denoms {
			balance := creditBalances.AmountOf(denom)
			if !balance.IsPositive() {
				continue
			}
			accruedAmounts, err = types.SafeAddCoins(
				accruedAmounts,
				sdk.Coins{{Denom: denom, Amount: balance}},
			)
			if err != nil {
				return nil, errorsmod.Wrapf(err, "clamp overflowed accrual for denom %s", denom)
			}
		}
	}
	overflowDenoms := make([]string, 0)
	if accrualOverflow != nil {
		overflowDenoms = append(overflowDenoms, accrualOverflow.Denoms...)
	}

	// If nothing accrued, return early with current balances
	if accruedAmounts.IsZero() {
		return &SettlementResult{
			TransferAmounts:    sdk.NewCoins(),
			AccruedAmounts:     sdk.NewCoins(),
			CreditBalanceAfter: creditBalances,
			AccrualOverflow:    overflowDenoms,
		}, nil
	}

	// Calculate transfer amounts (min of accrued and available)
	transferAmounts := CalculateTransferAmounts(accruedAmounts, creditBalances)

	// If nothing to transfer, return early
	if transferAmounts.IsZero() {
		return &SettlementResult{
			TransferAmounts:    sdk.NewCoins(),
			AccruedAmounts:     accruedAmounts,
			CreditBalanceAfter: creditBalances,
			AccrualOverflow:    overflowDenoms,
		}, nil
	}

	// Get credit address for the transfer
	creditAddr, err := types.DeriveCreditAddressFromBech32(lease.Tenant)
	if err != nil {
		return nil, err
	}

	// Get provider payout address
	provider, err := k.skuKeeper.GetProvider(ctx, lease.ProviderUuid)
	if err != nil {
		return nil, types.ErrProviderNotFound.Wrapf("provider_uuid %s not found", lease.ProviderUuid)
	}

	payoutAddr, err := sdk.AccAddressFromBech32(provider.PayoutAddress)
	if err != nil {
		return nil, types.ErrProviderNotFound.Wrapf("invalid payout address: %s", err)
	}

	// Transfer funds
	if err := k.bankKeeper.SendCoins(ctx, creditAddr, payoutAddr, transferAmounts); err != nil {
		return nil, types.ErrInvalidCreditOperation.Wrapf("failed to transfer: %s", err)
	}

	return &SettlementResult{
		TransferAmounts:    transferAmounts,
		AccruedAmounts:     accruedAmounts,
		CreditBalanceAfter: creditBalances.Sub(transferAmounts...),
		AccrualOverflow:    overflowDenoms,
	}, nil
}
