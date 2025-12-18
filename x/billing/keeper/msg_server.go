package keeper

import (
	"context"
	"strconv"
	"time"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	pkguuid "github.com/manifest-network/manifest-ledger/pkg/uuid"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
	k Keeper
}

// NewMsgServerImpl returns an implementation of the MsgServer interface.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{k: keeper}
}

// isAuthorizedForTenantLeaseCreation checks if the sender is the authority or in the allowed list.
func (ms msgServer) isAuthorizedForTenantLeaseCreation(ctx context.Context, sender string) (bool, error) {
	if ms.k.GetAuthority() == sender {
		return true, nil
	}
	params, err := ms.k.GetParams(ctx)
	if err != nil {
		return false, err
	}
	return params.IsAllowed(sender), nil
}

// FundCredit funds a tenant's credit account.
func (ms msgServer) FundCredit(ctx context.Context, msg *types.MsgFundCredit) (*types.MsgFundCreditResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	// Derive credit address for the tenant
	creditAddr, err := types.DeriveCreditAddressFromBech32(msg.Tenant)
	if err != nil {
		return nil, err
	}

	// Transfer tokens from sender to credit address
	senderAddr, err := sdk.AccAddressFromBech32(msg.Sender)
	if err != nil {
		return nil, err
	}

	if err := ms.k.bankKeeper.SendCoins(ctx, senderAddr, creditAddr, sdk.NewCoins(msg.Amount)); err != nil {
		return nil, types.ErrInvalidCreditOperation.Wrapf("failed to transfer tokens: %s", err)
	}

	// Get or create credit account
	creditAccount, err := ms.k.GetCreditAccount(ctx, msg.Tenant)
	if err != nil {
		// Credit account doesn't exist, create it
		creditAccount = types.CreditAccount{
			Tenant:           msg.Tenant,
			CreditAddress:    creditAddr.String(),
			ActiveLeaseCount: 0,
		}

		// Ensure the credit account address is registered in the account keeper
		if ms.k.accountKeeper.GetAccount(ctx, creditAddr) == nil {
			acc := ms.k.accountKeeper.NewAccountWithAddress(ctx, creditAddr)
			ms.k.accountKeeper.SetAccount(ctx, acc)
		}
	}

	if err := ms.k.SetCreditAccount(ctx, creditAccount); err != nil {
		return nil, err
	}

	// Get the new balance from the bank module for the funded denom
	newBalance := ms.k.bankKeeper.GetBalance(ctx, creditAddr, msg.Amount.Denom)

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeCreditFunded,
			sdk.NewAttribute(types.AttributeKeyTenant, msg.Tenant),
			sdk.NewAttribute(types.AttributeKeyCreditAddress, creditAddr.String()),
			sdk.NewAttribute(types.AttributeKeySender, msg.Sender),
			sdk.NewAttribute(types.AttributeKeyAmount, msg.Amount.String()),
			sdk.NewAttribute(types.AttributeKeyNewBalance, newBalance.String()),
		),
	)

	return &types.MsgFundCreditResponse{
		CreditAddress: creditAddr.String(),
		NewBalance:    newBalance,
	}, nil
}

// leaseCreationResult holds the result of lease creation for use by the public methods.
type leaseCreationResult struct {
	leaseUUID    string
	providerUUID string
	itemCount    int
	totalRates   sdk.Coins // total rate per second by denom
	activeLeases uint64
}

// createLeaseInternal contains the shared lease creation logic.
// It validates inputs, creates the lease, and returns the result for event emission.
func (ms msgServer) createLeaseInternal(ctx context.Context, tenant string, items []types.LeaseItemInput) (*leaseCreationResult, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	params, err := ms.k.GetParams(ctx)
	if err != nil {
		return nil, err
	}

	// 0. Verify item count doesn't exceed max_items_per_lease param
	if uint64(len(items)) > params.MaxItemsPerLease {
		return nil, types.ErrTooManyLeaseItems.Wrapf(
			"lease has %d items, maximum allowed is %d",
			len(items),
			params.MaxItemsPerLease,
		)
	}

	// 1. Get tenant's credit balances (all denoms)
	creditBalances, err := ms.k.GetCreditBalances(ctx, tenant)
	if err != nil {
		return nil, err
	}

	// 2. Get credit account and verify tenant hasn't exceeded max leases (O(1) check)
	creditAccount, err := ms.k.GetCreditAccount(ctx, tenant)
	if err != nil {
		return nil, types.ErrCreditAccountNotFound.Wrapf("tenant %s has no credit account", tenant)
	}

	if creditAccount.ActiveLeaseCount >= params.MaxLeasesPerTenant {
		return nil, types.ErrMaxLeasesReached.Wrapf(
			"tenant has %d active leases, max is %d",
			creditAccount.ActiveLeaseCount,
			params.MaxLeasesPerTenant,
		)
	}

	// 3. Verify all SKUs exist, are active, and belong to the same provider
	var providerUUID string
	leaseItems := make([]types.LeaseItem, 0, len(items))
	totalRatesPerSecond := sdk.NewCoins() // Accumulate rates by denom

	for i, inputItem := range items {
		sku, err := ms.k.skuKeeper.GetSKU(ctx, inputItem.SkuUuid)
		if err != nil {
			return nil, types.ErrSKUNotFound.Wrapf("sku_uuid %s not found", inputItem.SkuUuid)
		}

		if !sku.Active {
			return nil, types.ErrSKUNotActive.Wrapf("sku_uuid %s is not active", inputItem.SkuUuid)
		}

		// Check provider consistency
		if i == 0 {
			providerUUID = sku.ProviderUuid
		} else if sku.ProviderUuid != providerUUID {
			return nil, types.ErrMixedProviders.Wrapf(
				"sku_uuid %s belongs to provider %s, expected provider %s",
				inputItem.SkuUuid,
				sku.ProviderUuid,
				providerUUID,
			)
		}

		// Verify provider is active
		provider, err := ms.k.skuKeeper.GetProvider(ctx, sku.ProviderUuid)
		if err != nil {
			return nil, types.ErrProviderNotFound.Wrapf("provider_uuid %s not found", sku.ProviderUuid)
		}
		if !provider.Active {
			return nil, types.ErrProviderNotActive.Wrapf("provider_uuid %s is not active", sku.ProviderUuid)
		}

		// 4. Lock price from SKU (convert to per-second rate, preserving denom)
		lockedPricePerSecond := ConvertBasePriceToPerSecond(sku.BasePrice, sku.Unit)

		// Accumulate total rate for each denom
		itemRate := sdk.NewCoin(lockedPricePerSecond.Denom, lockedPricePerSecond.Amount.Mul(sdkmath.NewIntFromUint64(inputItem.Quantity)))
		totalRatesPerSecond = totalRatesPerSecond.Add(itemRate)

		leaseItems = append(leaseItems, types.LeaseItem{
			SkuUuid:     inputItem.SkuUuid,
			Quantity:    inputItem.Quantity,
			LockedPrice: lockedPricePerSecond,
		})
	}

	// 4. Verify tenant has enough credit to cover minimum lease duration for EACH denom
	// Required credit per denom = totalRatePerSecond[denom] * minLeaseDuration
	for _, rate := range totalRatesPerSecond {
		requiredCredit := rate.Amount.Mul(sdkmath.NewIntFromUint64(params.MinLeaseDuration))
		balance := creditBalances.AmountOf(rate.Denom)
		if balance.LT(requiredCredit) {
			return nil, types.ErrInsufficientCredit.Wrapf(
				"credit balance %s %s cannot cover minimum lease duration of %d seconds (requires %s %s at rate %s/second)",
				balance.String(),
				rate.Denom,
				params.MinLeaseDuration,
				requiredCredit.String(),
				rate.Denom,
				rate.Amount.String(),
			)
		}
	}

	// 5. Create lease with deterministic UUIDv7
	leaseSeq, err := ms.k.GetNextLeaseSequence(ctx)
	if err != nil {
		return nil, err
	}
	leaseUUID := pkguuid.GenerateUUIDv7(sdkCtx, types.ModuleName, leaseSeq)

	lease := types.Lease{
		Uuid:          leaseUUID,
		Tenant:        tenant,
		ProviderUuid:  providerUUID,
		Items:         leaseItems,
		State:         types.LEASE_STATE_PENDING, // Start in PENDING, awaiting provider acknowledgement
		CreatedAt:     blockTime,
		LastSettledAt: blockTime, // Will be updated to AcknowledgedAt when provider acknowledges
	}

	if err := ms.k.SetLease(ctx, lease); err != nil {
		return nil, err
	}

	// 6. Increment active lease count in credit account
	creditAccount.ActiveLeaseCount++
	if err := ms.k.SetCreditAccount(ctx, creditAccount); err != nil {
		return nil, err
	}

	return &leaseCreationResult{
		leaseUUID:    leaseUUID,
		providerUUID: providerUUID,
		itemCount:    len(leaseItems),
		totalRates:   totalRatesPerSecond,
		activeLeases: creditAccount.ActiveLeaseCount,
	}, nil
}

// CreateLease creates a new lease for the tenant.
func (ms msgServer) CreateLease(ctx context.Context, msg *types.MsgCreateLease) (*types.MsgCreateLeaseResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	result, err := ms.createLeaseInternal(ctx, msg.Tenant, msg.Items)
	if err != nil {
		return nil, err
	}

	// Emit detailed event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeLeaseCreated,
			sdk.NewAttribute(types.AttributeKeyLeaseUUID, result.leaseUUID),
			sdk.NewAttribute(types.AttributeKeyTenant, msg.Tenant),
			sdk.NewAttribute(types.AttributeKeyProviderUUID, result.providerUUID),
			sdk.NewAttribute(types.AttributeKeyItemCount, strconv.Itoa(result.itemCount)),
			sdk.NewAttribute(types.AttributeKeyTotalRate, result.totalRates.String()),
			sdk.NewAttribute(types.AttributeKeyActiveLeaseCount, strconv.FormatUint(result.activeLeases, 10)),
			sdk.NewAttribute(types.AttributeKeyCreatedBy, "tenant"),
		),
	)

	return &types.MsgCreateLeaseResponse{
		LeaseUuid: result.leaseUUID,
	}, nil
}

// CreateLeaseForTenant allows authority or allowed addresses to create a lease on behalf of a tenant.
// This is used for migrating off-chain leases to on-chain.
func (ms msgServer) CreateLeaseForTenant(ctx context.Context, msg *types.MsgCreateLeaseForTenant) (*types.MsgCreateLeaseForTenantResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	// Verify sender is authorized (authority or in allowed list)
	authorized, err := ms.isAuthorizedForTenantLeaseCreation(ctx, msg.Authority)
	if err != nil {
		return nil, types.ErrUnauthorized.Wrapf("failed to check authorization: %s", err)
	}
	if !authorized {
		return nil, types.ErrUnauthorized.Wrapf("%s is not the authority or in the allowed list", msg.Authority)
	}

	result, err := ms.createLeaseInternal(ctx, msg.Tenant, msg.Items)
	if err != nil {
		return nil, err
	}

	// Emit detailed event (with created_by = "authority" to distinguish from tenant-created leases)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeLeaseCreated,
			sdk.NewAttribute(types.AttributeKeyLeaseUUID, result.leaseUUID),
			sdk.NewAttribute(types.AttributeKeyTenant, msg.Tenant),
			sdk.NewAttribute(types.AttributeKeyProviderUUID, result.providerUUID),
			sdk.NewAttribute(types.AttributeKeyItemCount, strconv.Itoa(result.itemCount)),
			sdk.NewAttribute(types.AttributeKeyTotalRate, result.totalRates.String()),
			sdk.NewAttribute(types.AttributeKeyActiveLeaseCount, strconv.FormatUint(result.activeLeases, 10)),
			sdk.NewAttribute(types.AttributeKeyCreatedBy, "authority"),
		),
	)

	return &types.MsgCreateLeaseForTenantResponse{
		LeaseUuid: result.leaseUUID,
	}, nil
}

// CloseLease closes an active lease.
func (ms msgServer) CloseLease(ctx context.Context, msg *types.MsgCloseLease) (*types.MsgCloseLeaseResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	// 1. Get lease (without auto-close - we'll handle it explicitly)
	lease, err := ms.k.GetLease(ctx, msg.LeaseUuid)
	if err != nil {
		return nil, err
	}

	// 2. If lease is already inactive, nothing to do
	if lease.State != types.LEASE_STATE_ACTIVE {
		return nil, types.ErrLeaseNotActive.Wrapf("lease %s is not active", msg.LeaseUuid)
	}

	// 3. Verify sender is authorized (tenant, provider address, or authority)
	authorized := false
	closedBy := "unknown"

	// Check if sender is tenant
	if msg.Sender == lease.Tenant {
		authorized = true
		closedBy = "tenant"
	}

	// Check if sender is authority
	if msg.Sender == ms.k.GetAuthority() {
		authorized = true
		closedBy = "authority"
	}

	// Check if sender is provider address
	if !authorized {
		provider, err := ms.k.skuKeeper.GetProvider(ctx, lease.ProviderUuid)
		if err == nil && msg.Sender == provider.Address {
			authorized = true
			closedBy = "provider"
		}
	}

	if !authorized {
		return nil, types.ErrUnauthorized.Wrapf(
			"sender %s is not authorized to close lease %s",
			msg.Sender,
			msg.LeaseUuid,
		)
	}

	// 4. Check if lease should be auto-closed due to exhausted credit
	// If so, the settlement happens during auto-close
	closed, err := ms.k.CheckAndCloseExhaustedLease(ctx, &lease)
	if err != nil {
		return nil, err
	}

	var settledAmounts sdk.Coins
	var duration time.Duration
	if closed {
		// Lease was auto-closed due to credit exhaustion
		// Settlement already happened, so we just return success
		settledAmounts = sdk.NewCoins()
		closedBy = "credit_exhaustion"
		duration = 0
	} else {
		// 5. Calculate duration for event
		duration = blockTime.Sub(lease.LastSettledAt)

		// 6. Settle accrued charges
		settledAmounts, err = ms.settleLease(ctx, &lease, blockTime)
		if err != nil {
			return nil, err
		}

		// 7. Update lease state to inactive
		lease.State = types.LEASE_STATE_CLOSED
		lease.ClosedAt = &blockTime

		if err := ms.k.SetLease(ctx, lease); err != nil {
			return nil, err
		}

		// Decrement active lease count in credit account
		creditAccount, err := ms.k.GetCreditAccount(ctx, lease.Tenant)
		if err == nil && creditAccount.ActiveLeaseCount > 0 {
			creditAccount.ActiveLeaseCount--
			if err := ms.k.SetCreditAccount(ctx, creditAccount); err != nil {
				return nil, err
			}
		}
	}

	// Get active lease count for event (may have been decremented above or by auto-close)
	creditAccount, _ := ms.k.GetCreditAccount(ctx, lease.Tenant)

	// 8. Emit detailed event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeLeaseClosed,
			sdk.NewAttribute(types.AttributeKeyLeaseUUID, msg.LeaseUuid),
			sdk.NewAttribute(types.AttributeKeyTenant, lease.Tenant),
			sdk.NewAttribute(types.AttributeKeyProviderUUID, lease.ProviderUuid),
			sdk.NewAttribute(types.AttributeKeySettledAmounts, settledAmounts.String()),
			sdk.NewAttribute(types.AttributeKeyClosedBy, closedBy),
			sdk.NewAttribute(types.AttributeKeyDuration, strconv.FormatInt(int64(duration.Seconds()), 10)),
			sdk.NewAttribute(types.AttributeKeyActiveLeaseCount, strconv.FormatUint(creditAccount.ActiveLeaseCount, 10)),
		),
	)

	return &types.MsgCloseLeaseResponse{
		SettledAmounts: settledAmounts,
	}, nil
}

// Withdraw allows a provider to withdraw accrued funds from a specific lease.
func (ms msgServer) Withdraw(ctx context.Context, msg *types.MsgWithdraw) (*types.MsgWithdrawResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	// 1. Get lease (without auto-close - we'll handle it explicitly)
	lease, err := ms.k.GetLease(ctx, msg.LeaseUuid)
	if err != nil {
		return nil, err
	}

	// 2. Get provider and verify sender is authorized (provider address or authority)
	provider, err := ms.k.skuKeeper.GetProvider(ctx, lease.ProviderUuid)
	if err != nil {
		return nil, types.ErrProviderNotFound.Wrapf("provider_uuid %s not found", lease.ProviderUuid)
	}

	if msg.Sender != provider.Address && msg.Sender != ms.k.GetAuthority() {
		return nil, types.ErrUnauthorized.Wrapf(
			"sender %s is not authorized to withdraw from lease %s",
			msg.Sender,
			msg.LeaseUuid,
		)
	}

	// 3. For active leases, check if we need to auto-close due to exhausted credit
	if lease.State == types.LEASE_STATE_ACTIVE {
		closed, err := ms.k.CheckAndCloseExhaustedLease(ctx, &lease)
		if err != nil {
			return nil, err
		}
		if closed {
			// Auto-close already performed settlement and transferred funds
			// Emit withdrawal event with zero amount (settlement was done during auto-close)
			sdkCtx.EventManager().EmitEvent(
				sdk.NewEvent(
					types.EventTypeProviderWithdraw,
					sdk.NewAttribute(types.AttributeKeyLeaseUUID, msg.LeaseUuid),
					sdk.NewAttribute(sdk.AttributeKeyAmount, "0"),
					sdk.NewAttribute(types.AttributeKeyProviderUUID, lease.ProviderUuid),
					sdk.NewAttribute("auto_closed", "true"),
				),
			)
			return &types.MsgWithdrawResponse{
				Amounts:       sdk.NewCoins(),
				PayoutAddress: provider.PayoutAddress,
			}, nil
		}
	}

	// 4. Calculate accrued amounts since last settlement
	var duration time.Duration
	if lease.State == types.LEASE_STATE_ACTIVE {
		duration = blockTime.Sub(lease.LastSettledAt)
	} else {
		// For inactive leases, calculate from last settled to closed
		if lease.ClosedAt != nil {
			duration = lease.ClosedAt.Sub(lease.LastSettledAt)
		} else {
			duration = 0
		}
	}

	// Calculate total accrued with overflow checking
	items := make([]LeaseItemWithPrice, 0, len(lease.Items))
	for _, item := range lease.Items {
		items = append(items, LeaseItemWithPrice{
			SkuUUID:              item.SkuUuid,
			Quantity:             item.Quantity,
			LockedPricePerSecond: item.LockedPrice,
		})
	}
	accruedAmounts, err := CalculateTotalAccruedForLease(items, duration)
	if err != nil {
		return nil, types.ErrInvalidCreditOperation.Wrapf("accrual calculation error: %s", err)
	}

	if accruedAmounts.IsZero() {
		return nil, types.ErrNoWithdrawableAmount
	}

	// 5. Transfer accrued amounts from credit account to provider payout address
	creditAddr, err := types.DeriveCreditAddressFromBech32(lease.Tenant)
	if err != nil {
		return nil, err
	}

	payoutAddr, err := sdk.AccAddressFromBech32(provider.PayoutAddress)
	if err != nil {
		return nil, types.ErrProviderNotFound.Wrapf("invalid payout address: %s", err)
	}

	// Get credit balances
	creditBalances := ms.k.bankKeeper.GetAllBalances(ctx, creditAddr)

	// Calculate transfer amounts (minimum of accrued and available for each denom)
	transferAmounts := sdk.NewCoins()
	for _, accrued := range accruedAmounts {
		balance := creditBalances.AmountOf(accrued.Denom)
		transferAmount := accrued.Amount
		if balance.LT(accrued.Amount) {
			transferAmount = balance
		}
		if transferAmount.IsPositive() {
			transferAmounts = transferAmounts.Add(sdk.NewCoin(accrued.Denom, transferAmount))
		}
	}

	if !transferAmounts.IsZero() {
		if err := ms.k.bankKeeper.SendCoins(
			ctx,
			creditAddr,
			payoutAddr,
			transferAmounts,
		); err != nil {
			return nil, types.ErrInvalidCreditOperation.Wrapf("failed to transfer: %s", err)
		}
	}

	// 6. Update last_settled_at
	if lease.State == types.LEASE_STATE_ACTIVE {
		lease.LastSettledAt = blockTime
		if err := ms.k.SetLease(ctx, lease); err != nil {
			return nil, err
		}
	}

	// 7. Emit detailed event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeProviderWithdraw,
			sdk.NewAttribute(types.AttributeKeyLeaseUUID, msg.LeaseUuid),
			sdk.NewAttribute(types.AttributeKeyProviderUUID, lease.ProviderUuid),
			sdk.NewAttribute(types.AttributeKeyAmount, transferAmounts.String()),
			sdk.NewAttribute(types.AttributeKeyPayoutAddress, provider.PayoutAddress),
		),
	)

	return &types.MsgWithdrawResponse{
		Amounts:       transferAmounts,
		PayoutAddress: provider.PayoutAddress,
	}, nil
}

// WithdrawAll allows a provider to withdraw all accrued funds from all their leases.
// Supports pagination via the `limit` field to avoid gas exhaustion for providers with many leases.
func (ms msgServer) WithdrawAll(ctx context.Context, msg *types.MsgWithdrawAll) (*types.MsgWithdrawAllResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	// 1. Get provider by UUID (validated in ValidateBasic)
	provider, err := ms.k.skuKeeper.GetProvider(ctx, msg.ProviderUuid)
	if err != nil {
		return nil, types.ErrProviderNotFound.Wrapf("provider_uuid %s not found", msg.ProviderUuid)
	}
	providerUUID := msg.ProviderUuid

	// Verify sender is authorized (provider address or authority)
	if msg.Sender != provider.Address && msg.Sender != ms.k.GetAuthority() {
		return nil, types.ErrUnauthorized.Wrapf(
			"sender %s is not authorized for provider %s",
			msg.Sender,
			providerUUID,
		)
	}

	// 2. Get all leases for provider
	leases, err := ms.k.GetLeasesByProviderUUID(ctx, providerUUID)
	if err != nil {
		return nil, err
	}

	payoutAddr, err := sdk.AccAddressFromBech32(provider.PayoutAddress)
	if err != nil {
		return nil, types.ErrProviderNotFound.Wrapf("invalid payout address: %s", err)
	}

	// 3. For each lease (up to limit), calculate and withdraw accrued amounts
	totalAmounts := sdk.NewCoins()
	var leaseCount uint64
	var processedCount uint64

	// Apply default limit if not specified (limit=0 means use default, not unlimited)
	limit := msg.Limit
	if limit == 0 {
		limit = types.DefaultWithdrawAllLimit
	}
	hasMore := false

	for _, lease := range leases {
		// Check if we've reached the limit (always enforced now)
		if processedCount >= limit {
			hasMore = true
			break
		}

		// Calculate accrued amounts
		var duration time.Duration
		if lease.State == types.LEASE_STATE_ACTIVE {
			duration = blockTime.Sub(lease.LastSettledAt)
		} else if lease.ClosedAt != nil {
			duration = lease.ClosedAt.Sub(lease.LastSettledAt)
		}

		if duration <= 0 {
			continue
		}

		items := make([]LeaseItemWithPrice, 0, len(lease.Items))
		for _, item := range lease.Items {
			items = append(items, LeaseItemWithPrice{
				SkuUUID:              item.SkuUuid,
				Quantity:             item.Quantity,
				LockedPricePerSecond: item.LockedPrice,
			})
		}
		accruedAmounts, err := CalculateTotalAccruedForLease(items, duration)
		if err != nil {
			// Log overflow error but continue with other leases
			ms.k.Logger().Error("accrual calculation overflow",
				"lease_id", lease.Uuid,
				"error", err,
			)
			continue
		}

		if accruedAmounts.IsZero() {
			continue
		}

		// Get credit balances
		creditAddr, err := types.DeriveCreditAddressFromBech32(lease.Tenant)
		if err != nil {
			continue
		}

		creditBalances := ms.k.bankKeeper.GetAllBalances(ctx, creditAddr)

		// Calculate transfer amounts (minimum of accrued and available for each denom)
		transferAmounts := sdk.NewCoins()
		for _, accrued := range accruedAmounts {
			balance := creditBalances.AmountOf(accrued.Denom)
			transferAmount := accrued.Amount
			if balance.LT(accrued.Amount) {
				transferAmount = balance
			}
			if transferAmount.IsPositive() {
				transferAmounts = transferAmounts.Add(sdk.NewCoin(accrued.Denom, transferAmount))
			}
		}

		if !transferAmounts.IsZero() {
			if err := ms.k.bankKeeper.SendCoins(
				ctx,
				creditAddr,
				payoutAddr,
				transferAmounts,
			); err != nil {
				// Log error but continue with other leases
				ms.k.Logger().Error("failed to withdraw from lease",
					"lease_id", lease.Uuid,
					"error", err,
				)
				continue
			}

			totalAmounts = totalAmounts.Add(transferAmounts...)
			leaseCount++

			// Update last_settled_at for active leases
			if lease.State == types.LEASE_STATE_ACTIVE {
				lease.LastSettledAt = blockTime
				if err := ms.k.SetLease(ctx, lease); err != nil {
					ms.k.Logger().Error("failed to update lease",
						"lease_id", lease.Uuid,
						"error", err,
					)
				}
			}
		}

		processedCount++
	}

	// 4. Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeProviderWithdrawAll,
			sdk.NewAttribute(types.AttributeKeyProviderUUID, providerUUID),
			sdk.NewAttribute(types.AttributeKeyAmount, totalAmounts.String()),
			sdk.NewAttribute(types.AttributeKeyLeaseCount, strconv.FormatUint(leaseCount, 10)),
			sdk.NewAttribute(types.AttributeKeyPayoutAddress, provider.GetPayoutAddress()),
		),
	)

	return &types.MsgWithdrawAllResponse{
		TotalAmounts:  totalAmounts,
		LeaseCount:    leaseCount,
		PayoutAddress: provider.GetPayoutAddress(),
		HasMore:       hasMore,
	}, nil
}

// UpdateParams updates the module parameters.
func (ms msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	if ms.k.GetAuthority() != msg.Authority {
		return nil, types.ErrUnauthorized.Wrapf("expected %s, got %s", ms.k.GetAuthority(), msg.Authority)
	}

	if err := ms.k.SetParams(ctx, msg.Params); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeParamsUpdated),
	)

	return &types.MsgUpdateParamsResponse{}, nil
}

// settleLease calculates and transfers accrued charges from tenant's credit account
// to the provider's payout address. Returns the amounts settled (one per denom).
func (ms msgServer) settleLease(ctx context.Context, lease *types.Lease, settleTime time.Time) (sdk.Coins, error) {
	// Calculate duration since last settlement
	duration := settleTime.Sub(lease.LastSettledAt)
	if duration <= 0 {
		return sdk.NewCoins(), nil
	}

	// Calculate total accrued with overflow checking
	items := make([]LeaseItemWithPrice, 0, len(lease.Items))
	for _, item := range lease.Items {
		items = append(items, LeaseItemWithPrice{
			SkuUUID:              item.SkuUuid,
			Quantity:             item.Quantity,
			LockedPricePerSecond: item.LockedPrice,
		})
	}
	accruedAmounts, err := CalculateTotalAccruedForLease(items, duration)
	if err != nil {
		return sdk.NewCoins(), types.ErrInvalidCreditOperation.Wrapf("accrual calculation error: %s", err)
	}

	if accruedAmounts.IsZero() {
		return sdk.NewCoins(), nil
	}

	// Get credit address
	creditAddr, err := types.DeriveCreditAddressFromBech32(lease.Tenant)
	if err != nil {
		return sdk.NewCoins(), err
	}

	// Get provider payout address
	provider, err := ms.k.skuKeeper.GetProvider(ctx, lease.ProviderUuid)
	if err != nil {
		return sdk.NewCoins(), types.ErrProviderNotFound.Wrapf("provider_uuid %s not found", lease.ProviderUuid)
	}

	payoutAddr, err := sdk.AccAddressFromBech32(provider.PayoutAddress)
	if err != nil {
		return sdk.NewCoins(), types.ErrProviderNotFound.Wrapf("invalid payout address: %s", err)
	}

	// Get credit balances
	creditBalances := ms.k.bankKeeper.GetAllBalances(ctx, creditAddr)

	// Calculate transfer amounts (minimum of accrued and available for each denom)
	transferAmounts := sdk.NewCoins()
	for _, accrued := range accruedAmounts {
		balance := creditBalances.AmountOf(accrued.Denom)
		transferAmount := accrued.Amount
		if balance.LT(accrued.Amount) {
			transferAmount = balance
		}
		if transferAmount.IsPositive() {
			transferAmounts = transferAmounts.Add(sdk.NewCoin(accrued.Denom, transferAmount))
		}
	}

	if !transferAmounts.IsZero() {
		if err := ms.k.bankKeeper.SendCoins(
			ctx,
			creditAddr,
			payoutAddr,
			transferAmounts,
		); err != nil {
			return sdk.NewCoins(), types.ErrInvalidCreditOperation.Wrapf("failed to transfer: %s", err)
		}
	}

	// Update last_settled_at
	lease.LastSettledAt = settleTime

	return transferAmounts, nil
}

// AcknowledgeLease allows a provider to acknowledge a PENDING lease.
// This transitions the lease to ACTIVE state and starts billing.
func (ms msgServer) AcknowledgeLease(ctx context.Context, msg *types.MsgAcknowledgeLease) (*types.MsgAcknowledgeLeaseResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	// 1. Get lease
	lease, err := ms.k.GetLease(ctx, msg.LeaseUuid)
	if err != nil {
		return nil, err
	}

	// 2. Verify lease is in PENDING state
	if lease.State != types.LEASE_STATE_PENDING {
		return nil, types.ErrLeaseNotPending.Wrapf("lease %s is not in PENDING state", msg.LeaseUuid)
	}

	// 3. Verify sender is authorized (provider address or authority)
	provider, err := ms.k.skuKeeper.GetProvider(ctx, lease.ProviderUuid)
	if err != nil {
		return nil, types.ErrProviderNotFound.Wrapf("provider_uuid %s not found", lease.ProviderUuid)
	}

	if msg.Sender != provider.Address && msg.Sender != ms.k.GetAuthority() {
		return nil, types.ErrUnauthorized.Wrapf(
			"sender %s is not authorized to acknowledge lease %s",
			msg.Sender,
			msg.LeaseUuid,
		)
	}

	// 4. Transition lease to ACTIVE state
	lease.State = types.LEASE_STATE_ACTIVE
	lease.AcknowledgedAt = &blockTime
	lease.LastSettledAt = blockTime // Billing starts from acknowledgement

	if err := ms.k.SetLease(ctx, lease); err != nil {
		return nil, err
	}

	// 5. Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeLeaseAcknowledged,
			sdk.NewAttribute(types.AttributeKeyLeaseUUID, msg.LeaseUuid),
			sdk.NewAttribute(types.AttributeKeyTenant, lease.Tenant),
			sdk.NewAttribute(types.AttributeKeyProviderUUID, lease.ProviderUuid),
			sdk.NewAttribute(types.AttributeKeyAcknowledgedBy, msg.Sender),
		),
	)

	return &types.MsgAcknowledgeLeaseResponse{
		AcknowledgedAt: blockTime,
	}, nil
}

// RejectLease allows a provider to reject a PENDING lease.
// This transitions the lease to REJECTED state and unlocks tenant credit.
func (ms msgServer) RejectLease(ctx context.Context, msg *types.MsgRejectLease) (*types.MsgRejectLeaseResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	// 1. Get lease
	lease, err := ms.k.GetLease(ctx, msg.LeaseUuid)
	if err != nil {
		return nil, err
	}

	// 2. Verify lease is in PENDING state
	if lease.State != types.LEASE_STATE_PENDING {
		return nil, types.ErrLeaseNotPending.Wrapf("lease %s is not in PENDING state", msg.LeaseUuid)
	}

	// 3. Verify sender is authorized (provider address or authority)
	provider, err := ms.k.skuKeeper.GetProvider(ctx, lease.ProviderUuid)
	if err != nil {
		return nil, types.ErrProviderNotFound.Wrapf("provider_uuid %s not found", lease.ProviderUuid)
	}

	if msg.Sender != provider.Address && msg.Sender != ms.k.GetAuthority() {
		return nil, types.ErrUnauthorized.Wrapf(
			"sender %s is not authorized to reject lease %s",
			msg.Sender,
			msg.LeaseUuid,
		)
	}

	// 4. Transition lease to REJECTED state
	lease.State = types.LEASE_STATE_REJECTED
	lease.RejectedAt = &blockTime
	lease.RejectionReason = msg.Reason

	if err := ms.k.SetLease(ctx, lease); err != nil {
		return nil, err
	}

	// 5. Decrement pending lease count in credit account
	creditAccount, err := ms.k.GetCreditAccount(ctx, lease.Tenant)
	if err == nil && creditAccount.ActiveLeaseCount > 0 {
		creditAccount.ActiveLeaseCount--
		if err := ms.k.SetCreditAccount(ctx, creditAccount); err != nil {
			return nil, err
		}
	}

	// 6. Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeLeaseRejected,
			sdk.NewAttribute(types.AttributeKeyLeaseUUID, msg.LeaseUuid),
			sdk.NewAttribute(types.AttributeKeyTenant, lease.Tenant),
			sdk.NewAttribute(types.AttributeKeyProviderUUID, lease.ProviderUuid),
			sdk.NewAttribute(types.AttributeKeyRejectedBy, msg.Sender),
			sdk.NewAttribute(types.AttributeKeyRejectionReason, msg.Reason),
		),
	)

	return &types.MsgRejectLeaseResponse{
		RejectedAt: blockTime,
	}, nil
}

// CancelLease allows a tenant to cancel their own PENDING lease.
// This transitions the lease to REJECTED state and unlocks tenant credit.
func (ms msgServer) CancelLease(ctx context.Context, msg *types.MsgCancelLease) (*types.MsgCancelLeaseResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	// 1. Get lease
	lease, err := ms.k.GetLease(ctx, msg.LeaseUuid)
	if err != nil {
		return nil, err
	}

	// 2. Verify lease is in PENDING state
	if lease.State != types.LEASE_STATE_PENDING {
		return nil, types.ErrLeaseNotPending.Wrapf("lease %s is not in PENDING state", msg.LeaseUuid)
	}

	// 3. Verify sender is the tenant
	if msg.Tenant != lease.Tenant {
		return nil, types.ErrUnauthorized.Wrapf(
			"sender %s is not the tenant of lease %s",
			msg.Tenant,
			msg.LeaseUuid,
		)
	}

	// 4. Transition lease to REJECTED state (cancelled by tenant)
	lease.State = types.LEASE_STATE_REJECTED
	lease.RejectedAt = &blockTime
	lease.RejectionReason = "cancelled by tenant"

	if err := ms.k.SetLease(ctx, lease); err != nil {
		return nil, err
	}

	// 5. Decrement pending lease count in credit account
	creditAccount, err := ms.k.GetCreditAccount(ctx, lease.Tenant)
	if err == nil && creditAccount.ActiveLeaseCount > 0 {
		creditAccount.ActiveLeaseCount--
		if err := ms.k.SetCreditAccount(ctx, creditAccount); err != nil {
			return nil, err
		}
	}

	// 6. Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeLeaseCancelled,
			sdk.NewAttribute(types.AttributeKeyLeaseUUID, msg.LeaseUuid),
			sdk.NewAttribute(types.AttributeKeyTenant, lease.Tenant),
			sdk.NewAttribute(types.AttributeKeyProviderUUID, lease.ProviderUuid),
		),
	)

	return &types.MsgCancelLeaseResponse{
		CancelledAt: blockTime,
	}, nil
}
