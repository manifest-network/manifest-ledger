package keeper

import (
	"context"
	"encoding/hex"
	"errors"
	"strconv"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/pkg/sanitize"
	pkguuid "github.com/manifest-network/manifest-ledger/pkg/uuid"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
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
// Thin wrapper over the keeper's shared HasAdminPrivileges helper.
func (ms msgServer) isAuthorizedForTenantLeaseCreation(ctx context.Context, sender string) (bool, error) {
	role, err := ms.k.HasAdminPrivileges(ctx, sender)
	if err != nil {
		return false, err
	}
	return role != "", nil
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

	// Parse sender address
	senderAddr, err := sdk.AccAddressFromBech32(msg.Sender)
	if err != nil {
		return nil, err
	}

	// Use CacheContext to ensure atomicity: either all operations succeed
	// (token transfer + credit account creation) or none do.
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	cacheCtx, writeCache := sdkCtx.CacheContext()

	// Transfer tokens from sender to credit address
	if err := ms.k.bankKeeper.SendCoins(cacheCtx, senderAddr, creditAddr, sdk.NewCoins(msg.Amount)); err != nil {
		return nil, types.ErrInvalidCreditOperation.Wrapf("failed to transfer tokens: %s", err)
	}

	// Get or create credit account
	creditAccount, err := ms.k.GetCreditAccount(cacheCtx, msg.Tenant)
	if err != nil {
		if !errors.Is(err, types.ErrCreditAccountNotFound) {
			return nil, types.ErrInvalidCreditOperation.Wrapf("failed to get credit account: %s", err)
		}
		// Credit account doesn't exist, create it
		creditAccount = types.CreditAccount{
			Tenant:            msg.Tenant,
			CreditAddress:     creditAddr.String(),
			ActiveLeaseCount:  0,
			PendingLeaseCount: 0,
		}

		// Ensure the credit account address is registered in the account keeper
		if ms.k.accountKeeper.GetAccount(cacheCtx, creditAddr) == nil {
			acc := ms.k.accountKeeper.NewAccountWithAddress(cacheCtx, creditAddr)
			ms.k.accountKeeper.SetAccount(cacheCtx, acc)
		}
	}

	if err := ms.k.SetCreditAccount(cacheCtx, creditAccount); err != nil {
		return nil, err
	}

	// All operations succeeded - commit atomically
	writeCache()

	// Get the new balance from the bank module for the funded denom (after commit)
	newBalance := ms.k.bankKeeper.GetBalance(ctx, creditAddr, msg.Amount.Denom)

	// Emit event on original context (events are not cached)
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
	leaseUUID     string
	providerUUID  string
	itemCount     int
	totalRates    sdk.Coins // total rate per second by denom
	pendingLeases uint64
	metaHash      []byte
}

// createLeaseInternal contains the shared lease creation logic.
// It validates inputs, creates the lease, and returns the result for event emission.
func (ms msgServer) createLeaseInternal(ctx context.Context, tenant string, items []types.LeaseItemInput, metaHash []byte) (*leaseCreationResult, error) {
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

	// 1. Get credit account and verify tenant hasn't exceeded max leases (O(1) check)
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

	// Also check pending lease limit
	if creditAccount.PendingLeaseCount >= params.MaxPendingLeasesPerTenant {
		return nil, types.ErrMaxPendingLeasesReached.Wrapf(
			"tenant has %d pending leases, max is %d",
			creditAccount.PendingLeaseCount,
			params.MaxPendingLeasesPerTenant,
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

		// Lock price from SKU (convert to per-second rate, preserving denom)
		lockedPricePerSecond, err := ConvertBasePriceToPerSecond(sku.BasePrice, sku.Unit)
		if err != nil {
			// This should not happen for valid SKUs (validated at creation time)
			return nil, types.ErrSKUNotFound.Wrapf("invalid SKU pricing: %s", err)
		}

		// Accumulate total rate for each denom
		itemRate := sdk.NewCoin(lockedPricePerSecond.Denom, lockedPricePerSecond.Amount.Mul(sdkmath.NewIntFromUint64(inputItem.Quantity)))
		totalRatesPerSecond = totalRatesPerSecond.Add(itemRate)

		leaseItems = append(leaseItems, types.LeaseItem{
			SkuUuid:     inputItem.SkuUuid,
			Quantity:    inputItem.Quantity,
			LockedPrice: lockedPricePerSecond,
			ServiceName: inputItem.ServiceName,
		})
	}

	// 4. Verify provider is active (only need to check once since all SKUs belong to same provider)
	provider, err := ms.k.skuKeeper.GetProvider(ctx, providerUUID)
	if err != nil {
		return nil, types.ErrProviderNotFound.Wrapf("provider_uuid %s not found", providerUUID)
	}
	if !provider.Active {
		return nil, types.ErrProviderNotActive.Wrapf("provider_uuid %s is not active", providerUUID)
	}

	// 5. Calculate reservation and verify tenant has enough AVAILABLE credit
	// Available credit = balance - already reserved amounts
	// This prevents overbooking where multiple leases could exhaust the same credit
	reservationAmount := types.CalculateLeaseReservationFromRates(totalRatesPerSecond, params.MinLeaseDuration)

	// Fetch credit balances for only the denoms needed by this lease.
	// This avoids loading dust from unrelated token sends to the credit address.
	// totalRatesPerSecond is sdk.Coins (sorted, deduplicated) so leaseDenoms has no duplicates.
	leaseDenoms := make([]string, 0, len(totalRatesPerSecond))
	for _, coin := range totalRatesPerSecond {
		leaseDenoms = append(leaseDenoms, coin.Denom)
	}
	creditBalances, err := ms.k.getCreditBalancesForDenoms(ctx, tenant, leaseDenoms)
	if err != nil {
		return nil, err
	}

	availableCredit := types.GetAvailableCredit(creditBalances, creditAccount.ReservedAmounts)

	// Check each denom in the reservation has sufficient available credit
	for _, res := range reservationAmount {
		available := availableCredit.AmountOf(res.Denom)
		if available.LT(res.Amount) {
			return nil, types.ErrInsufficientCredit.Wrapf(
				"insufficient available credit for denom %s: need %s, have %s available (balance: %s, reserved: %s)",
				res.Denom,
				res.Amount.String(),
				available.String(),
				creditBalances.AmountOf(res.Denom).String(),
				creditAccount.ReservedAmounts.AmountOf(res.Denom).String(),
			)
		}
	}

	// Reserve credit immediately (lease is PENDING but credit is locked)
	creditAccount.ReservedAmounts = types.AddReservation(creditAccount.ReservedAmounts, reservationAmount)

	// 6. Create lease with deterministic UUIDv7
	leaseSeq, err := ms.k.GetNextLeaseSequence(ctx)
	if err != nil {
		return nil, err
	}
	leaseUUID := pkguuid.GenerateUUIDv7(sdkCtx, types.ModuleName, leaseSeq)

	lease := types.Lease{
		Uuid:                       leaseUUID,
		Tenant:                     tenant,
		ProviderUuid:               providerUUID,
		Items:                      leaseItems,
		State:                      types.LEASE_STATE_PENDING, // Start in PENDING, awaiting provider acknowledgement
		CreatedAt:                  blockTime,
		MetaHash:                   metaHash,
		MinLeaseDurationAtCreation: params.MinLeaseDuration, // Store for consistent reservation release
	}

	if err := ms.k.SetLease(ctx, lease); err != nil {
		return nil, err
	}

	// 7. Increment pending lease count in credit account (lease starts in PENDING state)
	creditAccount.PendingLeaseCount++
	if err := ms.k.SetCreditAccount(ctx, creditAccount); err != nil {
		return nil, err
	}

	return &leaseCreationResult{
		leaseUUID:     leaseUUID,
		providerUUID:  providerUUID,
		itemCount:     len(leaseItems),
		totalRates:    totalRatesPerSecond,
		pendingLeases: creditAccount.PendingLeaseCount,
		metaHash:      metaHash,
	}, nil
}

// emitLeaseCreatedEvent emits a lease_created event with the given parameters.
func emitLeaseCreatedEvent(ctx sdk.Context, result *leaseCreationResult, tenant, createdBy string) {
	eventAttrs := []sdk.Attribute{
		sdk.NewAttribute(types.AttributeKeyLeaseUUID, result.leaseUUID),
		sdk.NewAttribute(types.AttributeKeyTenant, tenant),
		sdk.NewAttribute(types.AttributeKeyProviderUUID, result.providerUUID),
		sdk.NewAttribute(types.AttributeKeyItemCount, strconv.Itoa(result.itemCount)),
		sdk.NewAttribute(types.AttributeKeyTotalRate, result.totalRates.String()),
		sdk.NewAttribute(types.AttributeKeyPendingLeaseCount, strconv.FormatUint(result.pendingLeases, 10)),
		sdk.NewAttribute(types.AttributeKeyCreatedBy, createdBy),
	}
	if len(result.metaHash) > 0 {
		eventAttrs = append(eventAttrs, sdk.NewAttribute(types.AttributeKeyMetaHash, hex.EncodeToString(result.metaHash)))
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeLeaseCreated, eventAttrs...))
}

// CreateLease creates a new lease for the tenant.
func (ms msgServer) CreateLease(ctx context.Context, msg *types.MsgCreateLease) (*types.MsgCreateLeaseResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	result, err := ms.createLeaseInternal(ctx, msg.Tenant, msg.Items, msg.MetaHash)
	if err != nil {
		return nil, err
	}

	emitLeaseCreatedEvent(sdk.UnwrapSDKContext(ctx), result, msg.Tenant, "tenant")

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

	result, err := ms.createLeaseInternal(ctx, msg.Tenant, msg.Items, msg.MetaHash)
	if err != nil {
		return nil, err
	}

	emitLeaseCreatedEvent(sdk.UnwrapSDKContext(ctx), result, msg.Tenant, "authority")

	return &types.MsgCreateLeaseForTenantResponse{
		LeaseUuid: result.leaseUUID,
	}, nil
}

// CloseLease closes one or more active leases.
// Sender must be authorized for each lease (tenant, provider, or authority).
// This is an atomic operation: all leases succeed or all fail.
func (ms msgServer) CloseLease(ctx context.Context, msg *types.MsgCloseLease) (*types.MsgCloseLeaseResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	// Get params for reservation calculation
	params, err := ms.k.GetParams(ctx)
	if err != nil {
		return nil, err
	}

	// Phase 1: Validate ALL leases and authorization first (fail-fast)
	leases := make([]types.Lease, 0, len(msg.LeaseUuids))
	creditAccounts := make(map[string]types.CreditAccount) // keyed by tenant address
	tenantOrder := make([]string, 0, len(msg.LeaseUuids))  // deterministic iteration order
	providerCache := make(map[string]string)               // provider UUID -> provider address
	var closedBy string                                    // consistent role for all leases

	isAuthority := msg.Sender == ms.k.GetAuthority()

	for _, uuid := range msg.LeaseUuids {
		lease, err := ms.k.GetLease(ctx, uuid)
		if err != nil {
			return nil, types.ErrLeaseNotFound.Wrapf("lease %s not found", uuid)
		}

		if lease.State != types.LEASE_STATE_ACTIVE {
			return nil, types.ErrLeaseNotActive.Wrapf("lease %s is not active", uuid)
		}

		// Determine authorization for this lease
		leaseClosedBy := ""

		// Check if sender is tenant
		if msg.Sender == lease.Tenant {
			leaseClosedBy = "tenant"
		}

		// Check if sender is authority (can close any lease)
		if isAuthority {
			leaseClosedBy = "authority"
		}

		// Check if sender is provider address (cache provider lookups)
		if leaseClosedBy == "" {
			providerAddr, exists := providerCache[lease.ProviderUuid]
			if !exists {
				provider, err := ms.k.skuKeeper.GetProvider(ctx, lease.ProviderUuid)
				if err != nil {
					if !errors.Is(err, skutypes.ErrProviderNotFound) {
						return nil, err
					}
					// Provider not found — sender cannot be the provider
				} else {
					providerAddr = provider.Address
					providerCache[lease.ProviderUuid] = providerAddr
				}
			}
			if providerAddr != "" && msg.Sender == providerAddr {
				leaseClosedBy = "provider"
			}
		}

		if leaseClosedBy == "" {
			return nil, types.ErrUnauthorized.Wrapf(
				"sender %s is not authorized to close lease %s",
				msg.Sender,
				uuid,
			)
		}

		// For non-authority senders, ensure consistent role across all leases
		if !isAuthority {
			if closedBy == "" {
				closedBy = leaseClosedBy
			} else if closedBy != leaseClosedBy {
				return nil, types.ErrUnauthorized.Wrapf(
					"sender %s has inconsistent authorization: lease %s requires %s role but batch uses %s",
					msg.Sender,
					uuid,
					leaseClosedBy,
					closedBy,
				)
			}
		} else {
			closedBy = "authority"
		}

		// Validate credit account exists for this tenant (only fetch once per tenant)
		if _, exists := creditAccounts[lease.Tenant]; !exists {
			creditAccount, err := ms.k.GetCreditAccount(ctx, lease.Tenant)
			if err != nil {
				return nil, types.ErrCreditAccountNotFound.Wrapf(
					"credit account not found for tenant %s (lease %s): data integrity issue",
					lease.Tenant,
					uuid,
				)
			}
			creditAccounts[lease.Tenant] = creditAccount
			tenantOrder = append(tenantOrder, lease.Tenant)
		}

		leases = append(leases, lease)
	}

	// Phase 2: Apply all changes atomically using CacheContext
	// This ensures that if any operation fails, all changes are rolled back.
	cacheCtx, writeCache := sdkCtx.CacheContext()
	totalSettledAmounts := sdk.NewCoins()

	// Streams are settled once per message: every lease of the batch that
	// shares a stream sees the same settlement, so the closure reason does not
	// depend on the order of the batch.
	streams := ms.k.newStreamCache(blockTime)

	// Track events to emit after successful commit
	type leaseEvent struct {
		uuid          string
		tenant        string
		providerUUID  string
		closedBy      string
		activeCount   uint64
		closureReason string
	}
	leaseEvents := make([]leaseEvent, 0, len(leases))

	for i := range leases {
		leaseClosedBy := closedBy
		closeTime := blockTime

		// Settle the lease's streams up to now and remove the lease from them.
		settlement, err := streams.leave(cacheCtx, &leases[i])
		if err != nil {
			return nil, err
		}

		leases[i].State = types.LEASE_STATE_CLOSED
		leases[i].ClosedAt = &closeTime

		// A stream freezes on a GTE boundary (accrued >= balance), so it also
		// freezes when the tenant's credit EXACTLY covers the accrued charge.
		// That is a full payment (the credit did its job), not exhaustion, so
		// the caller-supplied reason and role are preserved (ENG-577).
		// Everything else is genuine exhaustion: an unpaid remainder, an accrual
		// overflow, a zero balance with nothing accrued, or a stream that was
		// already frozen before this message.
		if settlement.CreditExhausted {
			leases[i].ClosureReason = types.ClosureReasonCreditExhausted
			leaseClosedBy = "credit_exhaustion"
		} else {
			leases[i].ClosureReason = msg.Reason
		}

		// SetLease reconciles the custom_domain reverse index from the lease's
		// new (terminal) state.
		if err := ms.k.SetLease(cacheCtx, leases[i]); err != nil {
			return nil, types.ErrInvalidLease.Wrapf("failed to update lease %s: %s", leases[i].Uuid, err)
		}

		// Update lease counts: decrement active (in memory map)
		creditAccount := creditAccounts[leases[i].Tenant]
		ms.k.DecrementActiveLeaseCount(&creditAccount, leases[i].Uuid)

		// Release reservation for this lease
		ms.k.ReleaseLeaseReservation(&creditAccount, &leases[i], params.MinLeaseDuration)

		creditAccounts[leases[i].Tenant] = creditAccount

		// Aggregate what the stream settlements of this message paid out
		totalSettledAmounts = totalSettledAmounts.Add(settlement.Transferred...)

		// Queue event for emission after successful commit
		// (creditAccount already has the updated ActiveLeaseCount from above)
		leaseEvents = append(leaseEvents, leaseEvent{
			uuid:          leases[i].Uuid,
			tenant:        leases[i].Tenant,
			providerUUID:  leases[i].ProviderUuid,
			closedBy:      leaseClosedBy,
			activeCount:   creditAccount.ActiveLeaseCount,
			closureReason: leases[i].ClosureReason,
		})
	}

	// Write each touched stream once (deleting streams left without leases).
	if err := streams.flush(cacheCtx); err != nil {
		return nil, err
	}

	// Persist all credit account updates to the cache context.
	// Use tenantOrder (insertion-order slice) instead of ranging over the map.
	for _, tenant := range tenantOrder {
		if err := ms.k.SetCreditAccount(cacheCtx, creditAccounts[tenant]); err != nil {
			return nil, err
		}
	}

	// All operations succeeded - commit the cache to the main context
	writeCache()

	// Emit events after successful commit (events go to the original context)
	for _, ev := range leaseEvents {
		eventAttrs := []sdk.Attribute{
			sdk.NewAttribute(types.AttributeKeyLeaseUUID, ev.uuid),
			sdk.NewAttribute(types.AttributeKeyTenant, ev.tenant),
			sdk.NewAttribute(types.AttributeKeyProviderUUID, ev.providerUUID),
			sdk.NewAttribute(types.AttributeKeyClosedBy, ev.closedBy),
			sdk.NewAttribute(types.AttributeKeyActiveLeaseCount, strconv.FormatUint(ev.activeCount, 10)),
		}
		if ev.closureReason != "" {
			eventAttrs = append(eventAttrs, sdk.NewAttribute(types.AttributeKeyClosureReason, sanitize.EventAttribute(ev.closureReason)))
		}
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(types.EventTypeLeaseClosed, eventAttrs...),
		)
	}

	// Emit batch summary event when multiple leases are closed
	if len(leases) > 1 {
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeBatchClosed,
				sdk.NewAttribute(types.AttributeKeyLeaseCount, strconv.FormatUint(uint64(len(leases)), 10)),
				sdk.NewAttribute(types.AttributeKeyClosedBy, closedBy),
				sdk.NewAttribute(types.AttributeKeySettledAmounts, totalSettledAmounts.String()),
			),
		)
	}

	return &types.MsgCloseLeaseResponse{
		ClosedAt:            blockTime,
		ClosedCount:         uint64(len(leases)),
		TotalSettledAmounts: totalSettledAmounts,
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

// SetItemCustomDomain sets or clears the custom_domain on a specific
// LeaseItem within a lease.
func (ms msgServer) SetItemCustomDomain(ctx context.Context, msg *types.MsgSetItemCustomDomain) (*types.MsgSetItemCustomDomainResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	if _, err := ms.k.SetItemCustomDomain(ctx, msg.Sender, msg.LeaseUuid, msg.ServiceName, msg.CustomDomain); err != nil {
		return nil, err
	}
	return &types.MsgSetItemCustomDomainResponse{}, nil
}

// pendingLeaseBatchResult holds the result of validating a batch of pending leases.
type pendingLeaseBatchResult struct {
	leases         []types.Lease
	creditAccounts map[string]types.CreditAccount
	tenantOrder    []string // deterministic iteration order (insertion order)
	providerUUID   string
}

// validatePendingLeaseBatch validates a batch of leases for provider operations (acknowledge/reject).
// It ensures all leases exist, are in PENDING state, belong to the same provider, and have valid credit accounts.
// Returns the validated leases, credit accounts map, and provider UUID.
func (ms msgServer) validatePendingLeaseBatch(ctx context.Context, leaseUuids []string) (*pendingLeaseBatchResult, error) {
	leases := make([]types.Lease, 0, len(leaseUuids))
	creditAccounts := make(map[string]types.CreditAccount)
	tenantOrder := make([]string, 0, len(leaseUuids))
	var providerUUID string

	for _, uuid := range leaseUuids {
		lease, err := ms.k.GetLease(ctx, uuid)
		if err != nil {
			return nil, types.ErrLeaseNotFound.Wrapf("lease %s not found", uuid)
		}

		if lease.State != types.LEASE_STATE_PENDING {
			return nil, types.ErrLeaseNotPending.Wrapf("lease %s is not in PENDING state", uuid)
		}

		// All leases must belong to same provider
		if providerUUID == "" {
			providerUUID = lease.ProviderUuid
		} else if lease.ProviderUuid != providerUUID {
			return nil, types.ErrMixedProviders.Wrapf("lease %s belongs to provider %s, expected %s", uuid, lease.ProviderUuid, providerUUID)
		}

		// Validate credit account exists for this tenant (only fetch once per tenant)
		if _, exists := creditAccounts[lease.Tenant]; !exists {
			creditAccount, err := ms.k.GetCreditAccount(ctx, lease.Tenant)
			if err != nil {
				return nil, types.ErrCreditAccountNotFound.Wrapf(
					"credit account not found for tenant %s (lease %s): data integrity issue",
					lease.Tenant,
					uuid,
				)
			}
			creditAccounts[lease.Tenant] = creditAccount
			tenantOrder = append(tenantOrder, lease.Tenant)
		}

		leases = append(leases, lease)
	}

	return &pendingLeaseBatchResult{
		leases:         leases,
		creditAccounts: creditAccounts,
		tenantOrder:    tenantOrder,
		providerUUID:   providerUUID,
	}, nil
}

// validateProviderAuthorization verifies the sender is authorized for provider operations.
// Returns the provider if authorized, or an error if not.
func (ms msgServer) validateProviderAuthorization(ctx context.Context, sender, providerUUID, operation string) (skutypes.Provider, error) {
	provider, err := ms.k.skuKeeper.GetProvider(ctx, providerUUID)
	if err != nil {
		return skutypes.Provider{}, types.ErrProviderNotFound.Wrapf("provider_uuid %s not found", providerUUID)
	}

	if sender != provider.Address && sender != ms.k.GetAuthority() {
		return skutypes.Provider{}, types.ErrUnauthorized.Wrapf(
			"sender %s is not authorized to %s leases for provider %s",
			sender,
			operation,
			providerUUID,
		)
	}

	return provider, nil
}

// AcknowledgeLease allows a provider to acknowledge one or more PENDING leases.
// This transitions the leases to ACTIVE state and starts billing.
// All leases must belong to the same provider. This is an atomic operation:
// all leases succeed or all fail.
func (ms msgServer) AcknowledgeLease(ctx context.Context, msg *types.MsgAcknowledgeLease) (*types.MsgAcknowledgeLeaseResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	// Phase 1: Validate all leases and authorization (fail-fast)
	validated, err := ms.validatePendingLeaseBatch(ctx, msg.LeaseUuids)
	if err != nil {
		return nil, err
	}

	if _, err := ms.validateProviderAuthorization(ctx, msg.Sender, validated.providerUUID, "acknowledge"); err != nil {
		return nil, err
	}

	leases := validated.leases
	creditAccounts := validated.creditAccounts

	// Phase 2: Apply all changes atomically using CacheContext
	// This ensures that if any operation fails, all changes are rolled back.
	cacheCtx, writeCache := sdkCtx.CacheContext()

	// Each stream the batch touches is settled and written once.
	streams := ms.k.newStreamCache(blockTime)

	// Track events to emit after successful commit
	type leaseEvent struct {
		uuid         string
		tenant       string
		providerUUID string
	}
	leaseEvents := make([]leaseEvent, 0, len(leases))

	for i := range leases {
		// Transition lease to ACTIVE state
		leases[i].State = types.LEASE_STATE_ACTIVE
		leases[i].AcknowledgedAt = &blockTime

		if err := ms.k.SetLease(cacheCtx, leases[i]); err != nil {
			return nil, types.ErrInvalidLease.Wrapf("failed to update lease %s: %s", leases[i].Uuid, err)
		}

		// Billing starts from acknowledgement: the lease joins its streams,
		// which are settled up to now before their rate grows. A lease may join
		// a stream that is frozen for exhausted credit; the EndBlocker then
		// closes it with the stream's other leases.
		if _, err := streams.join(cacheCtx, &leases[i]); err != nil {
			return nil, err
		}

		// Update lease counts: decrement pending, increment active
		// Credit account existence was validated in Phase 1
		creditAccount := creditAccounts[leases[i].Tenant]
		ms.k.DecrementPendingLeaseCount(&creditAccount, leases[i].Uuid)
		creditAccount.ActiveLeaseCount++
		creditAccounts[leases[i].Tenant] = creditAccount // Update map with new counts

		// Queue event for emission after successful commit
		leaseEvents = append(leaseEvents, leaseEvent{
			uuid:         leases[i].Uuid,
			tenant:       leases[i].Tenant,
			providerUUID: leases[i].ProviderUuid,
		})
	}

	// Write each touched stream once.
	if err := streams.flush(cacheCtx); err != nil {
		return nil, err
	}

	// Persist all credit account updates to the cache context.
	// Use validated.tenantOrder (insertion-order slice) instead of ranging over the map.
	for _, tenant := range validated.tenantOrder {
		if err := ms.k.SetCreditAccount(cacheCtx, creditAccounts[tenant]); err != nil {
			return nil, err
		}
	}

	// All operations succeeded - commit the cache to the main context
	writeCache()

	// Emit per-lease events after successful commit (events go to the original context)
	for _, ev := range leaseEvents {
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeLeaseAcknowledged,
				sdk.NewAttribute(types.AttributeKeyLeaseUUID, ev.uuid),
				sdk.NewAttribute(types.AttributeKeyTenant, ev.tenant),
				sdk.NewAttribute(types.AttributeKeyProviderUUID, ev.providerUUID),
				sdk.NewAttribute(types.AttributeKeyAcknowledgedBy, msg.Sender),
			),
		)
	}

	// Emit batch summary event when multiple leases are acknowledged
	if len(leases) > 1 {
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeBatchAcknowledged,
				sdk.NewAttribute(types.AttributeKeyLeaseCount, strconv.FormatUint(uint64(len(leases)), 10)),
				sdk.NewAttribute(types.AttributeKeyProviderUUID, validated.providerUUID),
				sdk.NewAttribute(types.AttributeKeyAcknowledgedBy, msg.Sender),
			),
		)
	}

	return &types.MsgAcknowledgeLeaseResponse{
		AcknowledgedAt:    blockTime,
		AcknowledgedCount: uint64(len(leases)),
	}, nil
}

// RejectLease allows a provider to reject one or more PENDING leases.
// All leases must belong to the same provider and be in PENDING state.
// This is an atomic operation: all leases succeed or all fail.
func (ms msgServer) RejectLease(ctx context.Context, msg *types.MsgRejectLease) (*types.MsgRejectLeaseResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	// Get params for reservation calculation
	params, err := ms.k.GetParams(ctx)
	if err != nil {
		return nil, err
	}

	// Phase 1: Validate all leases and authorization (fail-fast)
	validated, err := ms.validatePendingLeaseBatch(ctx, msg.LeaseUuids)
	if err != nil {
		return nil, err
	}

	if _, err := ms.validateProviderAuthorization(ctx, msg.Sender, validated.providerUUID, "reject"); err != nil {
		return nil, err
	}

	leases := validated.leases
	creditAccounts := validated.creditAccounts

	// Phase 2: Apply all changes atomically using CacheContext
	// This ensures that if any operation fails, all changes are rolled back.
	cacheCtx, writeCache := sdkCtx.CacheContext()

	// Track events to emit after successful commit (events are not cached)
	type leaseEvent struct {
		uuid         string
		tenant       string
		providerUUID string
	}
	leaseEvents := make([]leaseEvent, 0, len(leases))

	for i := range leases {
		// Transition lease to REJECTED state
		leases[i].State = types.LEASE_STATE_REJECTED
		leases[i].RejectedAt = &blockTime
		leases[i].RejectionReason = msg.Reason

		if err := ms.k.SetLease(cacheCtx, leases[i]); err != nil {
			return nil, types.ErrInvalidLease.Wrapf("failed to update lease %s: %s", leases[i].Uuid, err)
		}

		// Update lease counts: decrement pending
		// Credit account existence was validated in Phase 1
		creditAccount := creditAccounts[leases[i].Tenant]
		ms.k.DecrementPendingLeaseCount(&creditAccount, leases[i].Uuid)

		// Release reservation for this lease (PENDING leases have reservations)
		ms.k.ReleaseLeaseReservation(&creditAccount, &leases[i], params.MinLeaseDuration)

		creditAccounts[leases[i].Tenant] = creditAccount // Update map with new counts

		// Queue event for emission after successful commit
		leaseEvents = append(leaseEvents, leaseEvent{
			uuid:         leases[i].Uuid,
			tenant:       leases[i].Tenant,
			providerUUID: leases[i].ProviderUuid,
		})
	}

	// Persist all credit account updates to the cache context.
	// Use validated.tenantOrder (insertion-order slice) instead of ranging over the map.
	for _, tenant := range validated.tenantOrder {
		if err := ms.k.SetCreditAccount(cacheCtx, creditAccounts[tenant]); err != nil {
			return nil, err
		}
	}

	// All operations succeeded - commit the cache to the main context
	writeCache()

	// Emit per-lease events after successful commit (events go to the original context)
	// NOTE: We sanitize the rejection reason to prevent log injection attacks.
	for _, ev := range leaseEvents {
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeLeaseRejected,
				sdk.NewAttribute(types.AttributeKeyLeaseUUID, ev.uuid),
				sdk.NewAttribute(types.AttributeKeyTenant, ev.tenant),
				sdk.NewAttribute(types.AttributeKeyProviderUUID, ev.providerUUID),
				sdk.NewAttribute(types.AttributeKeyRejectedBy, msg.Sender),
				sdk.NewAttribute(types.AttributeKeyRejectionReason, sanitize.EventAttribute(msg.Reason)),
			),
		)
	}

	// Emit batch summary event when multiple leases are rejected
	if len(leases) > 1 {
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeBatchRejected,
				sdk.NewAttribute(types.AttributeKeyLeaseCount, strconv.FormatUint(uint64(len(leases)), 10)),
				sdk.NewAttribute(types.AttributeKeyProviderUUID, validated.providerUUID),
				sdk.NewAttribute(types.AttributeKeyRejectedBy, msg.Sender),
			),
		)
	}

	return &types.MsgRejectLeaseResponse{
		RejectedAt:    blockTime,
		RejectedCount: uint64(len(leases)),
	}, nil
}

// CancelLease allows a tenant to cancel one or more of their own PENDING leases.
// All leases must belong to the tenant and be in PENDING state.
// This is an atomic operation: all leases succeed or all fail.
func (ms msgServer) CancelLease(ctx context.Context, msg *types.MsgCancelLease) (*types.MsgCancelLeaseResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	// Get params for reservation calculation
	params, err := ms.k.GetParams(ctx)
	if err != nil {
		return nil, err
	}

	// Phase 1: Validate ALL leases first (fail-fast on any error)
	// Check lease existence and ownership before getting credit account
	leases := make([]types.Lease, 0, len(msg.LeaseUuids))

	for _, leaseUUID := range msg.LeaseUuids {
		// Get lease
		lease, err := ms.k.GetLease(ctx, leaseUUID)
		if err != nil {
			return nil, err
		}

		// Verify tenant owns this lease (check ownership before state)
		if msg.Tenant != lease.Tenant {
			return nil, types.ErrUnauthorized.Wrapf(
				"sender %s is not the tenant of lease %s (owned by %s)",
				msg.Tenant,
				leaseUUID,
				lease.Tenant,
			)
		}

		// Verify lease is in PENDING state
		if lease.State != types.LEASE_STATE_PENDING {
			return nil, types.ErrLeaseNotPending.Wrapf("lease %s is not in PENDING state", leaseUUID)
		}

		leases = append(leases, lease)
	}

	// Get and cache the tenant's credit account (after ownership validation)
	creditAccount, err := ms.k.GetCreditAccount(ctx, msg.Tenant)
	if err != nil {
		return nil, types.ErrCreditAccountNotFound.Wrapf(
			"credit account not found for tenant %s",
			msg.Tenant,
		)
	}

	// Phase 2: Apply all changes atomically using CacheContext
	cacheCtx, writeCache := sdkCtx.CacheContext()

	for i := range leases {
		// Transition lease to REJECTED state (cancelled by tenant)
		leases[i].State = types.LEASE_STATE_REJECTED
		leases[i].RejectedAt = &blockTime
		leases[i].RejectionReason = types.RejectionReasonCancelledByTenant

		if err := ms.k.SetLease(cacheCtx, leases[i]); err != nil {
			return nil, types.ErrInvalidLease.Wrapf("failed to update lease %s: %s", leases[i].Uuid, err)
		}

		// Decrement pending lease count in credit account
		ms.k.DecrementPendingLeaseCount(&creditAccount, leases[i].Uuid)

		// Release reservation for this lease (PENDING leases have reservations)
		ms.k.ReleaseLeaseReservation(&creditAccount, &leases[i], params.MinLeaseDuration)
	}

	// Save credit account with updated pending count and released reservations
	if err := ms.k.SetCreditAccount(cacheCtx, creditAccount); err != nil {
		return nil, err
	}

	// All operations succeeded - commit the cache to the main context
	writeCache()

	// Phase 3: Emit events after successful commit
	for i := range leases {
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeLeaseCancelled,
				sdk.NewAttribute(types.AttributeKeyLeaseUUID, leases[i].Uuid),
				sdk.NewAttribute(types.AttributeKeyTenant, leases[i].Tenant),
				sdk.NewAttribute(types.AttributeKeyProviderUUID, leases[i].ProviderUuid),
				sdk.NewAttribute(types.AttributeKeyCancelledBy, msg.Tenant),
			),
		)
	}

	// Emit batch event if multiple leases cancelled
	if len(leases) > 1 {
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeBatchCancelled,
				sdk.NewAttribute(types.AttributeKeyLeaseCount, strconv.FormatUint(uint64(len(leases)), 10)),
				sdk.NewAttribute(types.AttributeKeyTenant, msg.Tenant),
				sdk.NewAttribute(types.AttributeKeyCancelledBy, msg.Tenant),
			),
		)
	}

	return &types.MsgCancelLeaseResponse{
		CancelledAt:    blockTime,
		CancelledCount: uint64(len(leases)),
	}, nil
}
