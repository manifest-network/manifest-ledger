package keeper

import (
	"context"
	"strconv"
	"time"

	"cosmossdk.io/collections"
	collcodec "cosmossdk.io/collections/codec"
	"cosmossdk.io/collections/indexes"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	accountkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

// LeaseIndexes defines the indexes for the Lease collection.
type LeaseIndexes struct {
	// Tenant is a multi-index that indexes Leases by tenant address.
	Tenant *indexes.Multi[sdk.AccAddress, uint64, types.Lease]
	// Provider is a multi-index that indexes Leases by provider_id.
	Provider *indexes.Multi[uint64, uint64, types.Lease]
}

// IndexesList returns all indexes defined for the Lease collection.
func (i LeaseIndexes) IndexesList() []collections.Index[uint64, types.Lease] {
	return []collections.Index[uint64, types.Lease]{i.Tenant, i.Provider}
}

// NewLeaseIndexes creates a new LeaseIndexes instance.
func NewLeaseIndexes(sb *collections.SchemaBuilder) LeaseIndexes {
	return LeaseIndexes{
		Tenant: indexes.NewMulti(
			sb,
			types.LeaseByTenantIndexKey,
			"leases_by_tenant",
			sdk.AccAddressKey, // Use SDK's AccAddressKey for type safety and efficiency
			collections.Uint64Key,
			func(_ uint64, lease types.Lease) (sdk.AccAddress, error) {
				// Convert bech32 tenant address to AccAddress for indexing
				return sdk.AccAddressFromBech32(lease.Tenant)
			},
		),
		Provider: indexes.NewMulti(
			sb,
			types.LeaseByProviderIndexKey,
			"leases_by_provider",
			collections.Uint64Key,
			collections.Uint64Key,
			func(_ uint64, lease types.Lease) (uint64, error) {
				return lease.ProviderId, nil
			},
		),
	}
}

// SKUKeeper defines the expected SKU keeper interface.
type SKUKeeper interface {
	GetSKU(ctx context.Context, id uint64) (skutypes.SKU, error)
	GetProvider(ctx context.Context, id uint64) (skutypes.Provider, error)
}

// Keeper of the billing store.
type Keeper struct {
	cdc    codec.BinaryCodec
	logger log.Logger

	// state management
	Schema         collections.Schema
	Params         collections.Item[types.Params]
	Leases         *collections.IndexedMap[uint64, types.Lease, LeaseIndexes]
	NextLeaseID    collections.Sequence
	CreditAccounts collections.Map[sdk.AccAddress, types.CreditAccount] // keyed by tenant AccAddress
	// CreditAddressIndex is a reverse lookup from derived credit address to tenant address.
	// This enables O(1) lookup to check if an address is a credit account.
	CreditAddressIndex collections.Map[sdk.AccAddress, sdk.AccAddress] // keyed by derived credit address, value is tenant address

	authority string

	// keepers (to be set via setters for now, full DI later)
	skuKeeper     SKUKeeper
	bankKeeper    bankkeeper.Keeper
	accountKeeper accountkeeper.AccountKeeper
}

// NewKeeper creates a new billing Keeper instance.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	logger log.Logger,
	authority string,
) Keeper {
	logger = logger.With(log.ModuleKey, "x/"+types.ModuleName)

	sb := collections.NewSchemaBuilder(storeService)

	k := Keeper{
		cdc:       cdc,
		logger:    logger,
		authority: authority,

		Params: collections.NewItem(
			sb,
			types.ParamsKey,
			"params",
			codec.CollValue[types.Params](cdc),
		),
		Leases: collections.NewIndexedMap(
			sb,
			types.LeaseKey,
			"leases",
			collections.Uint64Key,
			codec.CollValue[types.Lease](cdc),
			NewLeaseIndexes(sb),
		),
		NextLeaseID: collections.NewSequence(
			sb,
			types.LeaseSequenceKey,
			"next_lease_id",
		),
		CreditAccounts: collections.NewMap(
			sb,
			types.CreditAccountKey,
			"credit_accounts",
			sdk.AccAddressKey, // Use SDK's AccAddressKey for type safety and efficiency
			codec.CollValue[types.CreditAccount](cdc),
		),
		CreditAddressIndex: collections.NewMap(
			sb,
			types.CreditAddressIndexKey,
			"credit_address_index",
			sdk.AccAddressKey, // derived credit address
			collcodec.KeyToValueCodec(sdk.AccAddressKey), // tenant address
		),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}

	k.Schema = schema

	return k
}

// Logger returns the module logger.
func (k *Keeper) Logger() log.Logger {
	return k.logger
}

// GetAuthority returns the module's authority.
func (k *Keeper) GetAuthority() string {
	return k.authority
}

// SetAuthority sets the module's authority (used for testing).
func (k *Keeper) SetAuthority(authority string) {
	k.authority = authority
}

// SetSKUKeeper sets the SKU keeper.
func (k *Keeper) SetSKUKeeper(sk SKUKeeper) {
	k.skuKeeper = sk
}

// SetBankKeeper sets the bank keeper.
func (k *Keeper) SetBankKeeper(bk bankkeeper.Keeper) {
	k.bankKeeper = bk
}

// SetAccountKeeper sets the account keeper.
func (k *Keeper) SetAccountKeeper(ak accountkeeper.AccountKeeper) {
	k.accountKeeper = ak
}

// GetAccountKeeper returns the account keeper (for simulation).
func (k *Keeper) GetAccountKeeper() accountkeeper.AccountKeeper {
	return k.accountKeeper
}

// GetBankKeeper returns the bank keeper (for simulation).
func (k *Keeper) GetBankKeeper() bankkeeper.Keeper {
	return k.bankKeeper
}

// GetParams returns the module parameters.
func (k *Keeper) GetParams(ctx context.Context) (types.Params, error) {
	return k.Params.Get(ctx)
}

// SetParams sets the module parameters.
func (k *Keeper) SetParams(ctx context.Context, params types.Params) error {
	return k.Params.Set(ctx, params)
}

// InitGenesis initializes the module's state from a provided genesis state.
func (k *Keeper) InitGenesis(ctx context.Context, gs *types.GenesisState) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	// Validate timestamps against block time
	// This ensures LastSettledAt is not in the future (important for chain restarts)
	if err := gs.ValidateWithBlockTime(blockTime); err != nil {
		return err
	}

	if err := k.Params.Set(ctx, gs.Params); err != nil {
		return err
	}

	for _, lease := range gs.Leases {
		if err := k.Leases.Set(ctx, lease.Id, lease); err != nil {
			return err
		}
	}

	if err := k.NextLeaseID.Set(ctx, gs.NextLeaseId); err != nil {
		return err
	}

	for _, ca := range gs.CreditAccounts {
		// Use SetCreditAccount to also populate the reverse index
		if err := k.SetCreditAccount(ctx, ca); err != nil {
			return err
		}
	}

	return nil
}

// ExportGenesis exports the module's state to a genesis state.
func (k *Keeper) ExportGenesis(ctx context.Context) *types.GenesisState {
	params, err := k.Params.Get(ctx)
	if err != nil {
		panic(err)
	}

	var leases []types.Lease
	err = k.Leases.Walk(ctx, nil, func(_ uint64, lease types.Lease) (bool, error) {
		leases = append(leases, lease)
		return false, nil
	})
	if err != nil {
		panic(err)
	}

	nextLeaseID, err := k.NextLeaseID.Peek(ctx)
	if err != nil {
		panic(err)
	}

	var creditAccounts []types.CreditAccount
	err = k.CreditAccounts.Walk(ctx, nil, func(_ sdk.AccAddress, ca types.CreditAccount) (bool, error) {
		creditAccounts = append(creditAccounts, ca)
		return false, nil
	})
	if err != nil {
		panic(err)
	}

	return &types.GenesisState{
		Params:         params,
		Leases:         leases,
		CreditAccounts: creditAccounts,
		NextLeaseId:    nextLeaseID,
	}
}

// Lease operations

// GetLease returns a Lease by its ID.
func (k *Keeper) GetLease(ctx context.Context, id uint64) (types.Lease, error) {
	lease, err := k.Leases.Get(ctx, id)
	if err != nil {
		return types.Lease{}, types.ErrLeaseNotFound
	}
	return lease, nil
}

// SetLease sets a Lease in the store.
func (k *Keeper) SetLease(ctx context.Context, lease types.Lease) error {
	return k.Leases.Set(ctx, lease.Id, lease)
}

// GetNextLeaseID returns the next Lease ID and increments the sequence.
func (k *Keeper) GetNextLeaseID(ctx context.Context) (uint64, error) {
	return k.NextLeaseID.Next(ctx)
}

// GetAllLeases returns all Leases in the store.
func (k *Keeper) GetAllLeases(ctx context.Context) ([]types.Lease, error) {
	var leases []types.Lease

	err := k.Leases.Walk(ctx, nil, func(_ uint64, lease types.Lease) (bool, error) {
		leases = append(leases, lease)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return leases, nil
}

// GetLeasesByTenant returns all Leases for a given tenant address.
func (k *Keeper) GetLeasesByTenant(ctx context.Context, tenant string) ([]types.Lease, error) {
	var leases []types.Lease

	// Convert bech32 address to AccAddress for index lookup
	tenantAddr, err := sdk.AccAddressFromBech32(tenant)
	if err != nil {
		return nil, err
	}

	iter, err := k.Leases.Indexes.Tenant.MatchExact(ctx, tenantAddr)
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		leaseID, err := iter.PrimaryKey()
		if err != nil {
			return nil, err
		}
		lease, err := k.Leases.Get(ctx, leaseID)
		if err != nil {
			return nil, err
		}
		leases = append(leases, lease)
	}

	return leases, nil
}

// GetLeasesByProviderID returns all Leases for a given provider ID.
func (k *Keeper) GetLeasesByProviderID(ctx context.Context, providerID uint64) ([]types.Lease, error) {
	var leases []types.Lease

	iter, err := k.Leases.Indexes.Provider.MatchExact(ctx, providerID)
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		leaseID, err := iter.PrimaryKey()
		if err != nil {
			return nil, err
		}
		lease, err := k.Leases.Get(ctx, leaseID)
		if err != nil {
			return nil, err
		}
		leases = append(leases, lease)
	}

	return leases, nil
}

// Credit Account operations

// GetCreditAccount returns a CreditAccount by tenant address.
func (k *Keeper) GetCreditAccount(ctx context.Context, tenant string) (types.CreditAccount, error) {
	// Convert bech32 address to AccAddress for storage lookup
	tenantAddr, err := sdk.AccAddressFromBech32(tenant)
	if err != nil {
		return types.CreditAccount{}, types.ErrCreditAccountNotFound.Wrapf("invalid tenant address: %s", err)
	}

	ca, err := k.CreditAccounts.Get(ctx, tenantAddr)
	if err != nil {
		return types.CreditAccount{}, types.ErrCreditAccountNotFound
	}
	return ca, nil
}

// SetCreditAccount sets a CreditAccount in the store and updates the reverse lookup index.
func (k *Keeper) SetCreditAccount(ctx context.Context, ca types.CreditAccount) error {
	// Convert bech32 address to AccAddress for storage
	tenantAddr, err := sdk.AccAddressFromBech32(ca.Tenant)
	if err != nil {
		return err
	}

	// Store the credit account keyed by tenant AccAddress
	if err := k.CreditAccounts.Set(ctx, tenantAddr, ca); err != nil {
		return err
	}

	// Update the reverse lookup index (derived address -> tenant)
	derivedAddr := types.DeriveCreditAddress(tenantAddr)
	return k.CreditAddressIndex.Set(ctx, derivedAddr, tenantAddr)
}

// GetAllCreditAccounts returns all CreditAccounts in the store.
func (k *Keeper) GetAllCreditAccounts(ctx context.Context) ([]types.CreditAccount, error) {
	var accounts []types.CreditAccount

	err := k.CreditAccounts.Walk(ctx, nil, func(_ sdk.AccAddress, ca types.CreditAccount) (bool, error) {
		accounts = append(accounts, ca)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return accounts, nil
}

// CountActiveLeasesByTenant counts the number of active leases for a tenant.
// This method uses the CreditAccount's cached ActiveLeaseCount for O(1) performance.
// Falls back to iterating leases if credit account doesn't exist.
func (k *Keeper) CountActiveLeasesByTenant(ctx context.Context, tenant string) (uint64, error) {
	// Try to get from credit account's cached count (O(1))
	creditAccount, err := k.GetCreditAccount(ctx, tenant)
	if err == nil {
		return creditAccount.ActiveLeaseCount, nil
	}

	// Fall back to iteration if credit account doesn't exist
	return k.countActiveLeasesByTenantScan(ctx, tenant)
}

// countActiveLeasesByTenantScan counts active leases by iterating (O(n)).
// This is used as a fallback when credit account doesn't exist.
func (k *Keeper) countActiveLeasesByTenantScan(ctx context.Context, tenant string) (uint64, error) {
	var count uint64

	// Convert bech32 address to bytes for index lookup
	tenantAddr, err := sdk.AccAddressFromBech32(tenant)
	if err != nil {
		return 0, err
	}

	iter, err := k.Leases.Indexes.Tenant.MatchExact(ctx, tenantAddr)
	if err != nil {
		return 0, err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		leaseID, err := iter.PrimaryKey()
		if err != nil {
			return 0, err
		}
		lease, err := k.Leases.Get(ctx, leaseID)
		if err != nil {
			return 0, err
		}
		if lease.State == types.LEASE_STATE_ACTIVE {
			count++
		}
	}

	return count, nil
}

// GetCreditBalance returns the actual credit balance from the bank module for a tenant.
func (k *Keeper) GetCreditBalance(ctx context.Context, tenant string, denom string) (sdk.Coin, error) {
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant)
	if err != nil {
		return sdk.Coin{}, err
	}
	return k.bankKeeper.GetBalance(ctx, creditAddr, denom), nil
}

// CalculateWithdrawableForLease calculates the amount that can be withdrawn from a lease.
// It considers the time since last settlement and the credit balance available.
func (k *Keeper) CalculateWithdrawableForLease(ctx context.Context, lease types.Lease) math.Int {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	// Calculate duration since last settlement
	var duration time.Duration
	if lease.State == types.LEASE_STATE_ACTIVE {
		duration = blockTime.Sub(lease.LastSettledAt)
	} else {
		// For inactive leases, calculate from last settled to closed
		if lease.ClosedAt != nil {
			duration = lease.ClosedAt.Sub(lease.LastSettledAt)
		} else {
			return math.ZeroInt()
		}
	}

	if duration <= 0 {
		return math.ZeroInt()
	}

	// Calculate total accrued with overflow handling
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
		// Log overflow error and return zero
		k.logger.Error("accrual calculation overflow in withdrawable calculation",
			"lease_id", lease.Id,
			"error", err,
		)
		return math.ZeroInt()
	}

	if accruedAmount.IsZero() {
		return math.ZeroInt()
	}

	// Get credit balance to cap the withdrawable amount
	params, err := k.GetParams(ctx)
	if err != nil {
		return math.ZeroInt()
	}

	creditBalance, err := k.GetCreditBalance(ctx, lease.Tenant, params.Denom)
	if err != nil {
		return math.ZeroInt()
	}

	// Return the minimum of accrued amount and available balance
	if creditBalance.Amount.LT(accruedAmount) {
		return creditBalance.Amount
	}

	return accruedAmount
}

// CheckAndCloseExhaustedLease checks if a lease should be auto-closed due to exhausted credit
// and closes it if necessary. This implements "lazy evaluation" / "check on touch" pattern.
// Returns true if the lease was closed, the updated lease, and any error.
// This is O(1) per lease check, avoiding O(n) scanning of all leases in EndBlock.
//
// The function performs settlement calculation to determine if the balance would be exhausted
// after accrual, rather than just checking the current balance. This ensures leases are
// closed promptly when credit runs out, even if the balance isn't exactly zero yet.
func (k *Keeper) CheckAndCloseExhaustedLease(ctx context.Context, lease *types.Lease) (closed bool, err error) {
	// Only check active leases
	if lease.State != types.LEASE_STATE_ACTIVE {
		return false, nil
	}

	params, err := k.GetParams(ctx)
	if err != nil {
		return false, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	// Calculate duration since last settlement
	duration := blockTime.Sub(lease.LastSettledAt)
	if duration < 0 {
		duration = 0
	}

	// Check tenant's current credit balance
	creditBalance, err := k.GetCreditBalance(ctx, lease.Tenant, params.Denom)
	if err != nil {
		return false, err
	}

	// If balance is already zero or negative, definitely close
	shouldClose := creditBalance.Amount.IsZero() || creditBalance.Amount.IsNegative()

	// Also check if the accrued amount would exhaust the balance
	if !shouldClose && duration > 0 {
		// Calculate what would be accrued
		items := make([]LeaseItemWithPrice, 0, len(lease.Items))
		for _, item := range lease.Items {
			items = append(items, LeaseItemWithPrice{
				SkuID:                item.SkuId,
				Quantity:             item.Quantity,
				LockedPricePerSecond: item.LockedPrice,
			})
		}
		accruedAmount, calcErr := CalculateTotalAccruedForLease(items, duration)
		if calcErr == nil && accruedAmount.GTE(creditBalance.Amount) {
			// Accrued amount >= balance means balance will be exhausted
			shouldClose = true
		}
	}

	if !shouldClose {
		return false, nil
	}

	// Perform final settlement (transfer remaining balance to provider)
	settledAmount, err := k.settleAndCloseLease(ctx, lease, blockTime, params)
	if err != nil {
		return false, err
	}

	// Emit event for auto-closed lease
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeLeaseAutoClose,
			sdk.NewAttribute(types.AttributeKeyLeaseID, strconv.FormatUint(lease.Id, 10)),
			sdk.NewAttribute(types.AttributeKeyTenant, lease.Tenant),
			sdk.NewAttribute(types.AttributeKeyProviderID, strconv.FormatUint(lease.ProviderId, 10)),
			sdk.NewAttribute(types.AttributeKeySettledAmount, settledAmount.String()),
			sdk.NewAttribute(types.AttributeKeyReason, "credit_exhausted"),
		),
	)

	k.logger.Info("auto-closed exhausted lease",
		"lease_id", lease.Id,
		"tenant", lease.Tenant,
		"settled_amount", settledAmount.String(),
	)

	return true, nil
}

// settleAndCloseLease performs final settlement and closes a lease.
// This is used by both manual close and auto-close operations.
func (k *Keeper) settleAndCloseLease(ctx context.Context, lease *types.Lease, closeTime time.Time, params types.Params) (math.Int, error) {
	// Calculate duration since last settlement
	duration := closeTime.Sub(lease.LastSettledAt)
	if duration < 0 {
		duration = 0
	}

	var settledAmount math.Int

	if duration > 0 {
		// Calculate accrued amount
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
			// On overflow, use zero (better than failing the close)
			accruedAmount = math.ZeroInt()
		}

		// Get credit balance
		creditAddr, err := types.DeriveCreditAddressFromBech32(lease.Tenant)
		if err != nil {
			return math.ZeroInt(), err
		}
		creditBalance := k.bankKeeper.GetBalance(ctx, creditAddr, params.Denom)

		// Transfer minimum of accrued and available
		transferAmount := accruedAmount
		if creditBalance.Amount.LT(accruedAmount) {
			transferAmount = creditBalance.Amount
		}

		if transferAmount.IsPositive() {
			// Get provider payout address
			provider, err := k.skuKeeper.GetProvider(ctx, lease.ProviderId)
			if err != nil {
				return math.ZeroInt(), types.ErrProviderNotFound.Wrapf("provider_id %d not found", lease.ProviderId)
			}

			payoutAddr, err := sdk.AccAddressFromBech32(provider.PayoutAddress)
			if err != nil {
				return math.ZeroInt(), types.ErrProviderNotFound.Wrapf("invalid payout address: %s", err)
			}

			if err := k.bankKeeper.SendCoins(
				ctx,
				creditAddr,
				payoutAddr,
				sdk.NewCoins(sdk.NewCoin(params.Denom, transferAmount)),
			); err != nil {
				return math.ZeroInt(), types.ErrInvalidCreditOperation.Wrapf("failed to transfer: %s", err)
			}
		}

		settledAmount = transferAmount
	} else {
		settledAmount = math.ZeroInt()
	}

	// Update lease state
	lease.State = types.LEASE_STATE_INACTIVE
	lease.ClosedAt = &closeTime
	lease.LastSettledAt = closeTime

	if err := k.SetLease(ctx, *lease); err != nil {
		return math.ZeroInt(), err
	}

	// Decrement active lease count in credit account
	creditAccount, err := k.GetCreditAccount(ctx, lease.Tenant)
	if err == nil && creditAccount.ActiveLeaseCount > 0 {
		creditAccount.ActiveLeaseCount--
		if err := k.SetCreditAccount(ctx, creditAccount); err != nil {
			return settledAmount, err
		}
	}

	return settledAmount, nil
}
