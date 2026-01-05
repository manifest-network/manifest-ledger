package keeper

import (
	"context"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// SettlementResult contains the results of a settlement operation.
type SettlementResult struct {
	// TransferAmounts is the actual amount transferred to the provider.
	TransferAmounts sdk.Coins
	// AccruedAmounts is the total amount accrued (may be higher than transferred if credit is insufficient).
	AccruedAmounts sdk.Coins
	// CreditBalanceAfter is the tenant's credit balance after the settlement.
	CreditBalanceAfter sdk.Coins
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
	result := sdk.NewCoins()
	for _, coin := range accrued {
		balance := available.AmountOf(coin.Denom)
		transferAmount := coin.Amount
		if balance.LT(coin.Amount) {
			transferAmount = balance
		}
		if transferAmount.IsPositive() {
			result = result.Add(sdk.NewCoin(coin.Denom, transferAmount))
		}
	}
	return result
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
//   - error if settlement fails
//
// Note: This function does NOT update the lease's LastSettledAt - the caller is responsible
// for updating the lease state after a successful settlement.
func (k *Keeper) PerformSettlement(ctx context.Context, lease *types.Lease, settleTime time.Time) (*SettlementResult, error) {
	// Calculate duration since last settlement
	duration := settleTime.Sub(lease.LastSettledAt)
	if duration <= 0 {
		return &SettlementResult{
			TransferAmounts:    sdk.NewCoins(),
			AccruedAmounts:     sdk.NewCoins(),
			CreditBalanceAfter: sdk.NewCoins(),
		}, nil
	}

	// Calculate accrued amounts
	items := LeaseItemsToWithPrice(lease.Items)
	accruedAmounts, err := CalculateTotalAccruedForLease(items, duration)
	if err != nil {
		return nil, types.ErrInvalidCreditOperation.Wrapf("accrual calculation error: %s", err)
	}

	// Get credit address and balances
	creditAddr, err := types.DeriveCreditAddressFromBech32(lease.Tenant)
	if err != nil {
		return nil, err
	}
	creditBalances := k.bankKeeper.GetAllBalances(ctx, creditAddr)

	// If nothing accrued, return early with current balances
	if accruedAmounts.IsZero() {
		return &SettlementResult{
			TransferAmounts:    sdk.NewCoins(),
			AccruedAmounts:     sdk.NewCoins(),
			CreditBalanceAfter: creditBalances,
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
		}, nil
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
	}, nil
}

// PerformSettlementSilent is like PerformSettlement but returns empty amounts on overflow
// instead of an error. This is useful for close operations where we want to proceed
// even if accrual calculation fails.
func (k *Keeper) PerformSettlementSilent(ctx context.Context, lease *types.Lease, settleTime time.Time) (*SettlementResult, error) {
	// Calculate duration since last settlement
	duration := settleTime.Sub(lease.LastSettledAt)
	if duration <= 0 {
		return &SettlementResult{
			TransferAmounts:    sdk.NewCoins(),
			AccruedAmounts:     sdk.NewCoins(),
			CreditBalanceAfter: sdk.NewCoins(),
		}, nil
	}

	// Calculate accrued amounts - silently use empty coins on overflow
	items := LeaseItemsToWithPrice(lease.Items)
	accruedAmounts, err := CalculateTotalAccruedForLease(items, duration)
	if err != nil {
		// On overflow, proceed with empty amounts rather than failing
		accruedAmounts = sdk.NewCoins()
	}

	// Get credit address and balances
	creditAddr, err := types.DeriveCreditAddressFromBech32(lease.Tenant)
	if err != nil {
		return nil, err
	}
	creditBalances := k.bankKeeper.GetAllBalances(ctx, creditAddr)

	// If nothing accrued, return early with current balances
	if accruedAmounts.IsZero() {
		return &SettlementResult{
			TransferAmounts:    sdk.NewCoins(),
			AccruedAmounts:     sdk.NewCoins(),
			CreditBalanceAfter: creditBalances,
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
		}, nil
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
	}, nil
}
