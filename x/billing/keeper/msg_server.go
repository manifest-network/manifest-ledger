package keeper

import (
	"context"
	"strconv"
	"time"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

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

	// Validate denom matches billing params
	params, err := ms.k.GetParams(ctx)
	if err != nil {
		return nil, err
	}

	if msg.Amount.Denom != params.Denom {
		return nil, types.ErrInvalidDenom.Wrapf("expected %s, got %s", params.Denom, msg.Amount.Denom)
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

	// Get the new balance from the bank module
	newBalance := ms.k.bankKeeper.GetBalance(ctx, creditAddr, params.Denom)

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
	leaseID      uint64
	providerID   uint64
	itemCount    int
	totalRate    math.Int
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

	// 1. Verify tenant has sufficient credit balance
	creditBalance, err := ms.k.GetCreditBalance(ctx, tenant, params.Denom)
	if err != nil {
		return nil, err
	}

	if creditBalance.Amount.LT(params.MinCreditBalance) {
		return nil, types.ErrInsufficientCredit.Wrapf(
			"credit balance %s is less than minimum required %s",
			creditBalance.Amount.String(),
			params.MinCreditBalance.String(),
		)
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
	var providerID uint64
	leaseItems := make([]types.LeaseItem, 0, len(items))
	totalRatePerSecond := math.ZeroInt()

	for i, inputItem := range items {
		sku, err := ms.k.skuKeeper.GetSKU(ctx, inputItem.SkuId)
		if err != nil {
			return nil, types.ErrSKUNotFound.Wrapf("sku_id %d not found", inputItem.SkuId)
		}

		if !sku.Active {
			return nil, types.ErrSKUNotActive.Wrapf("sku_id %d is not active", inputItem.SkuId)
		}

		// Check provider consistency
		if i == 0 {
			providerID = sku.ProviderId
		} else if sku.ProviderId != providerID {
			return nil, types.ErrMixedProviders.Wrapf(
				"sku_id %d belongs to provider %d, expected provider %d",
				inputItem.SkuId,
				sku.ProviderId,
				providerID,
			)
		}

		// Verify provider is active
		provider, err := ms.k.skuKeeper.GetProvider(ctx, sku.ProviderId)
		if err != nil {
			return nil, types.ErrProviderNotFound.Wrapf("provider_id %d not found", sku.ProviderId)
		}
		if !provider.Active {
			return nil, types.ErrProviderNotActive.Wrapf("provider_id %d is not active", sku.ProviderId)
		}

		// 4. Lock price from SKU (convert to per-second rate)
		lockedPricePerSecond := ConvertBasePriceToPerSecond(sku.BasePrice, sku.Unit)

		// Accumulate total rate for event (use math.Int for type safety)
		quantityInt := math.NewIntFromUint64(inputItem.Quantity)
		totalRatePerSecond = totalRatePerSecond.Add(lockedPricePerSecond.Mul(quantityInt))

		leaseItems = append(leaseItems, types.LeaseItem{
			SkuId:       inputItem.SkuId,
			Quantity:    inputItem.Quantity,
			LockedPrice: lockedPricePerSecond,
		})
	}

	// 5. Create lease
	leaseID, err := ms.k.GetNextLeaseID(ctx)
	if err != nil {
		return nil, err
	}

	lease := types.Lease{
		Id:            leaseID,
		Tenant:        tenant,
		ProviderId:    providerID,
		Items:         leaseItems,
		State:         types.LEASE_STATE_ACTIVE,
		CreatedAt:     blockTime,
		LastSettledAt: blockTime,
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
		leaseID:      leaseID,
		providerID:   providerID,
		itemCount:    len(leaseItems),
		totalRate:    totalRatePerSecond,
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
			sdk.NewAttribute(types.AttributeKeyLeaseID, strconv.FormatUint(result.leaseID, 10)),
			sdk.NewAttribute(types.AttributeKeyTenant, msg.Tenant),
			sdk.NewAttribute(types.AttributeKeyProviderID, strconv.FormatUint(result.providerID, 10)),
			sdk.NewAttribute(types.AttributeKeyItemCount, strconv.Itoa(result.itemCount)),
			sdk.NewAttribute(types.AttributeKeyTotalRate, result.totalRate.String()),
			sdk.NewAttribute(types.AttributeKeyActiveLeaseCount, strconv.FormatUint(result.activeLeases, 10)),
			sdk.NewAttribute(types.AttributeKeyCreatedBy, "tenant"),
		),
	)

	return &types.MsgCreateLeaseResponse{
		LeaseId: result.leaseID,
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
			sdk.NewAttribute(types.AttributeKeyLeaseID, strconv.FormatUint(result.leaseID, 10)),
			sdk.NewAttribute(types.AttributeKeyTenant, msg.Tenant),
			sdk.NewAttribute(types.AttributeKeyProviderID, strconv.FormatUint(result.providerID, 10)),
			sdk.NewAttribute(types.AttributeKeyItemCount, strconv.Itoa(result.itemCount)),
			sdk.NewAttribute(types.AttributeKeyTotalRate, result.totalRate.String()),
			sdk.NewAttribute(types.AttributeKeyActiveLeaseCount, strconv.FormatUint(result.activeLeases, 10)),
			sdk.NewAttribute(types.AttributeKeyCreatedBy, "authority"),
		),
	)

	return &types.MsgCreateLeaseForTenantResponse{
		LeaseId: result.leaseID,
	}, nil
}

// CloseLease closes an active lease.
func (ms msgServer) CloseLease(ctx context.Context, msg *types.MsgCloseLease) (*types.MsgCloseLeaseResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	// 1. Verify lease exists
	lease, err := ms.k.GetLease(ctx, msg.LeaseId)
	if err != nil {
		return nil, err
	}

	// 2. Verify lease is active
	if lease.State != types.LEASE_STATE_ACTIVE {
		return nil, types.ErrLeaseNotActive.Wrapf("lease %d is not active", msg.LeaseId)
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
		provider, err := ms.k.skuKeeper.GetProvider(ctx, lease.ProviderId)
		if err == nil && msg.Sender == provider.Address {
			authorized = true
			closedBy = "provider"
		}
	}

	if !authorized {
		return nil, types.ErrUnauthorized.Wrapf(
			"sender %s is not authorized to close lease %d",
			msg.Sender,
			msg.LeaseId,
		)
	}

	// 4. Calculate duration for event
	duration := blockTime.Sub(lease.LastSettledAt)

	// 5. Settle accrued charges
	settledAmount, err := ms.settleLease(ctx, &lease, blockTime)
	if err != nil {
		return nil, err
	}

	// 6. Update lease state to inactive
	lease.State = types.LEASE_STATE_INACTIVE
	lease.ClosedAt = &blockTime

	if err := ms.k.SetLease(ctx, lease); err != nil {
		return nil, err
	}

	// 7. Decrement active lease count in credit account
	creditAccount, err := ms.k.GetCreditAccount(ctx, lease.Tenant)
	if err == nil && creditAccount.ActiveLeaseCount > 0 {
		creditAccount.ActiveLeaseCount--
		if err := ms.k.SetCreditAccount(ctx, creditAccount); err != nil {
			return nil, err
		}
	}

	params, err := ms.k.GetParams(ctx)
	if err != nil {
		return nil, err
	}

	// 8. Emit detailed event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeLeaseClosed,
			sdk.NewAttribute(types.AttributeKeyLeaseID, strconv.FormatUint(msg.LeaseId, 10)),
			sdk.NewAttribute(types.AttributeKeyTenant, lease.Tenant),
			sdk.NewAttribute(types.AttributeKeyProviderID, strconv.FormatUint(lease.ProviderId, 10)),
			sdk.NewAttribute(types.AttributeKeySettledAmount, settledAmount.String()),
			sdk.NewAttribute(types.AttributeKeyClosedBy, closedBy),
			sdk.NewAttribute(types.AttributeKeyDuration, strconv.FormatInt(int64(duration.Seconds()), 10)),
			sdk.NewAttribute(types.AttributeKeyActiveLeaseCount, strconv.FormatUint(creditAccount.ActiveLeaseCount, 10)),
		),
	)

	return &types.MsgCloseLeaseResponse{
		SettledAmount: sdk.NewCoin(params.Denom, settledAmount),
	}, nil
}

// Withdraw allows a provider to withdraw accrued funds from a specific lease.
func (ms msgServer) Withdraw(ctx context.Context, msg *types.MsgWithdraw) (*types.MsgWithdrawResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	// 1. Verify lease exists
	lease, err := ms.k.GetLease(ctx, msg.LeaseId)
	if err != nil {
		return nil, err
	}

	// 2. Get provider and verify sender is authorized (provider address or authority)
	provider, err := ms.k.skuKeeper.GetProvider(ctx, lease.ProviderId)
	if err != nil {
		return nil, types.ErrProviderNotFound.Wrapf("provider_id %d not found", lease.ProviderId)
	}

	if msg.Sender != provider.Address && msg.Sender != ms.k.GetAuthority() {
		return nil, types.ErrUnauthorized.Wrapf(
			"sender %s is not authorized to withdraw from lease %d",
			msg.Sender,
			msg.LeaseId,
		)
	}

	// 3. Calculate accrued amount since last settlement
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
			SkuID:                item.SkuId,
			Quantity:             item.Quantity,
			LockedPricePerSecond: item.LockedPrice,
		})
	}
	accruedAmount, err := CalculateTotalAccruedForLease(items, duration)
	if err != nil {
		return nil, types.ErrInvalidCreditOperation.Wrapf("accrual calculation error: %s", err)
	}

	if accruedAmount.IsZero() {
		return nil, types.ErrNoWithdrawableAmount
	}

	// 4. Transfer accrued amount from credit account to provider payout address
	params, err := ms.k.GetParams(ctx)
	if err != nil {
		return nil, err
	}

	creditAddr, err := types.DeriveCreditAddressFromBech32(lease.Tenant)
	if err != nil {
		return nil, err
	}

	payoutAddr, err := sdk.AccAddressFromBech32(provider.PayoutAddress)
	if err != nil {
		return nil, types.ErrProviderNotFound.Wrapf("invalid payout address: %s", err)
	}

	// Check if credit account has sufficient balance
	creditBalance := ms.k.bankKeeper.GetBalance(ctx, creditAddr, params.Denom)

	// Transfer the minimum of accrued amount and available balance
	transferAmount := accruedAmount
	if creditBalance.Amount.LT(accruedAmount) {
		transferAmount = creditBalance.Amount
	}

	if transferAmount.IsPositive() {
		if err := ms.k.bankKeeper.SendCoins(
			ctx,
			creditAddr,
			payoutAddr,
			sdk.NewCoins(sdk.NewCoin(params.Denom, transferAmount)),
		); err != nil {
			return nil, types.ErrInvalidCreditOperation.Wrapf("failed to transfer: %s", err)
		}
	}

	// 5. Update last_settled_at
	if lease.State == types.LEASE_STATE_ACTIVE {
		lease.LastSettledAt = blockTime
		if err := ms.k.SetLease(ctx, lease); err != nil {
			return nil, err
		}
	}

	// 6. Emit detailed event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeProviderWithdraw,
			sdk.NewAttribute(types.AttributeKeyLeaseID, strconv.FormatUint(msg.LeaseId, 10)),
			sdk.NewAttribute(types.AttributeKeyProviderID, strconv.FormatUint(lease.ProviderId, 10)),
			sdk.NewAttribute(types.AttributeKeyAmount, transferAmount.String()),
			sdk.NewAttribute(types.AttributeKeyPayoutAddress, provider.PayoutAddress),
		),
	)

	return &types.MsgWithdrawResponse{
		Amount:        sdk.NewCoin(params.Denom, transferAmount),
		PayoutAddress: provider.PayoutAddress,
	}, nil
}

// WithdrawAll allows a provider to withdraw all accrued funds from all their leases.
func (ms msgServer) WithdrawAll(ctx context.Context, msg *types.MsgWithdrawAll) (*types.MsgWithdrawAllResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	// 1. Get provider by ID (validated in ValidateBasic to be > 0)
	provider, err := ms.k.skuKeeper.GetProvider(ctx, msg.ProviderId)
	if err != nil {
		return nil, types.ErrProviderNotFound.Wrapf("provider_id %d not found", msg.ProviderId)
	}
	providerID := msg.ProviderId

	// Verify sender is authorized (provider address or authority)
	if msg.Sender != provider.Address && msg.Sender != ms.k.GetAuthority() {
		return nil, types.ErrUnauthorized.Wrapf(
			"sender %s is not authorized for provider %d",
			msg.Sender,
			providerID,
		)
	}

	// 2. Get all leases for provider
	leases, err := ms.k.GetLeasesByProviderID(ctx, providerID)
	if err != nil {
		return nil, err
	}

	params, err := ms.k.GetParams(ctx)
	if err != nil {
		return nil, err
	}

	payoutAddr, err := sdk.AccAddressFromBech32(provider.PayoutAddress)
	if err != nil {
		return nil, types.ErrProviderNotFound.Wrapf("invalid payout address: %s", err)
	}

	// 3. For each lease, calculate and withdraw accrued amount
	totalAmount := math.ZeroInt()
	var leaseCount uint64

	for _, lease := range leases {
		// Calculate accrued amount
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
				SkuID:                item.SkuId,
				Quantity:             item.Quantity,
				LockedPricePerSecond: item.LockedPrice,
			})
		}
		accruedAmount, err := CalculateTotalAccruedForLease(items, duration)
		if err != nil {
			// Log overflow error but continue with other leases
			ms.k.Logger().Error("accrual calculation overflow",
				"lease_id", lease.Id,
				"error", err,
			)
			continue
		}

		if accruedAmount.IsZero() {
			continue
		}

		// Get credit balance
		creditAddr, err := types.DeriveCreditAddressFromBech32(lease.Tenant)
		if err != nil {
			continue
		}

		creditBalance := ms.k.bankKeeper.GetBalance(ctx, creditAddr, params.Denom)

		// Transfer the minimum of accrued and available
		transferAmount := accruedAmount
		if creditBalance.Amount.LT(accruedAmount) {
			transferAmount = creditBalance.Amount
		}

		if transferAmount.IsPositive() {
			if err := ms.k.bankKeeper.SendCoins(
				ctx,
				creditAddr,
				payoutAddr,
				sdk.NewCoins(sdk.NewCoin(params.Denom, transferAmount)),
			); err != nil {
				// Log error but continue with other leases
				ms.k.Logger().Error("failed to withdraw from lease",
					"lease_id", lease.Id,
					"error", err,
				)
				continue
			}

			totalAmount = totalAmount.Add(transferAmount)
			leaseCount++

			// Update last_settled_at for active leases
			if lease.State == types.LEASE_STATE_ACTIVE {
				lease.LastSettledAt = blockTime
				if err := ms.k.SetLease(ctx, lease); err != nil {
					ms.k.Logger().Error("failed to update lease",
						"lease_id", lease.Id,
						"error", err,
					)
				}
			}
		}
	}

	// 4. Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeProviderWithdrawAll,
			sdk.NewAttribute(types.AttributeKeyProviderID, strconv.FormatUint(providerID, 10)),
			sdk.NewAttribute(types.AttributeKeyAmount, totalAmount.String()),
			sdk.NewAttribute(types.AttributeKeyLeaseCount, strconv.FormatUint(leaseCount, 10)),
			sdk.NewAttribute(types.AttributeKeyPayoutAddress, provider.GetPayoutAddress()),
		),
	)

	return &types.MsgWithdrawAllResponse{
		TotalAmount:   sdk.NewCoin(params.Denom, totalAmount),
		LeaseCount:    leaseCount,
		PayoutAddress: provider.GetPayoutAddress(),
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
// to the provider's payout address. Returns the amount settled.
func (ms msgServer) settleLease(ctx context.Context, lease *types.Lease, settleTime time.Time) (math.Int, error) {
	// Calculate duration since last settlement
	duration := settleTime.Sub(lease.LastSettledAt)
	if duration <= 0 {
		return math.ZeroInt(), nil
	}

	// Calculate total accrued with overflow checking
	items := make([]LeaseItemWithPrice, 0, len(lease.Items))
	for _, item := range lease.Items {
		items = append(items, LeaseItemWithPrice{
			SkuID:                item.SkuId,
			Quantity:             item.Quantity,
			LockedPricePerSecond: item.LockedPrice,
		})
	}
	accruedAmount, err := CalculateTotalAccruedForLease(items, duration)
	if err != nil {
		return math.ZeroInt(), types.ErrInvalidCreditOperation.Wrapf("accrual calculation error: %s", err)
	}

	if accruedAmount.IsZero() {
		return math.ZeroInt(), nil
	}

	// Get params for denom
	params, err := ms.k.GetParams(ctx)
	if err != nil {
		return math.ZeroInt(), err
	}

	// Get credit address
	creditAddr, err := types.DeriveCreditAddressFromBech32(lease.Tenant)
	if err != nil {
		return math.ZeroInt(), err
	}

	// Get provider payout address
	provider, err := ms.k.skuKeeper.GetProvider(ctx, lease.ProviderId)
	if err != nil {
		return math.ZeroInt(), types.ErrProviderNotFound.Wrapf("provider_id %d not found", lease.ProviderId)
	}

	payoutAddr, err := sdk.AccAddressFromBech32(provider.PayoutAddress)
	if err != nil {
		return math.ZeroInt(), types.ErrProviderNotFound.Wrapf("invalid payout address: %s", err)
	}

	// Check credit balance
	creditBalance := ms.k.bankKeeper.GetBalance(ctx, creditAddr, params.Denom)

	// Transfer minimum of accrued and available
	transferAmount := accruedAmount
	if creditBalance.Amount.LT(accruedAmount) {
		transferAmount = creditBalance.Amount
	}

	if transferAmount.IsPositive() {
		if err := ms.k.bankKeeper.SendCoins(
			ctx,
			creditAddr,
			payoutAddr,
			sdk.NewCoins(sdk.NewCoin(params.Denom, transferAmount)),
		); err != nil {
			return math.ZeroInt(), types.ErrInvalidCreditOperation.Wrapf("failed to transfer: %s", err)
		}
	}

	// Update last_settled_at
	lease.LastSettledAt = settleTime

	return transferAmount, nil
}
