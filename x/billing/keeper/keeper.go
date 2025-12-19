package keeper

import (
	"context"
	"time"

	"cosmossdk.io/collections"
	collcodec "cosmossdk.io/collections/codec"
	"cosmossdk.io/collections/indexes"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"

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
	Tenant *indexes.Multi[sdk.AccAddress, string, types.Lease]
	// Provider is a multi-index that indexes Leases by provider_uuid.
	Provider *indexes.Multi[string, string, types.Lease]
	// State is a multi-index that indexes Leases by their state (pending, active, closed, etc).
	State *indexes.Multi[int32, string, types.Lease]
}

// IndexesList returns all indexes defined for the Lease collection.
func (i LeaseIndexes) IndexesList() []collections.Index[string, types.Lease] {
	return []collections.Index[string, types.Lease]{i.Tenant, i.Provider, i.State}
}

// NewLeaseIndexes creates a new LeaseIndexes instance.
func NewLeaseIndexes(sb *collections.SchemaBuilder) LeaseIndexes {
	return LeaseIndexes{
		Tenant: indexes.NewMulti(
			sb,
			types.LeaseByTenantIndexKey,
			"leases_by_tenant",
			sdk.AccAddressKey, // Use SDK's AccAddressKey for type safety and efficiency
			collections.StringKey,
			func(_ string, lease types.Lease) (sdk.AccAddress, error) {
				// Convert bech32 tenant address to AccAddress for indexing
				return sdk.AccAddressFromBech32(lease.Tenant)
			},
		),
		Provider: indexes.NewMulti(
			sb,
			types.LeaseByProviderIndexKey,
			"leases_by_provider",
			collections.StringKey,
			collections.StringKey,
			func(_ string, lease types.Lease) (string, error) {
				return lease.ProviderUuid, nil
			},
		),
		State: indexes.NewMulti(
			sb,
			types.LeaseByStateIndexKey,
			"leases_by_state",
			collections.Int32Key,
			collections.StringKey,
			func(_ string, lease types.Lease) (int32, error) {
				return int32(lease.State), nil
			},
		),
	}
}

// SKUKeeper defines the expected SKU keeper interface.
type SKUKeeper interface {
	GetSKU(ctx context.Context, uuid string) (skutypes.SKU, error)
	GetProvider(ctx context.Context, uuid string) (skutypes.Provider, error)
}

// Keeper of the billing store.
type Keeper struct {
	cdc    codec.BinaryCodec
	logger log.Logger

	// state management
	Schema         collections.Schema
	Params         collections.Item[types.Params]
	Leases         *collections.IndexedMap[string, types.Lease, LeaseIndexes]
	LeaseSequence  collections.Sequence                                 // For deterministic UUIDv7 generation
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
			collections.StringKey,
			codec.CollValue[types.Lease](cdc),
			NewLeaseIndexes(sb),
		),
		LeaseSequence: collections.NewSequence(
			sb,
			types.LeaseSequenceKey,
			"lease_sequence",
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
		if err := k.Leases.Set(ctx, lease.Uuid, lease); err != nil {
			return err
		}
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
	err = k.Leases.Walk(ctx, nil, func(_ string, lease types.Lease) (bool, error) {
		leases = append(leases, lease)
		return false, nil
	})
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
	}
}

// Lease operations

// GetLease returns a Lease by its UUID.
func (k *Keeper) GetLease(ctx context.Context, uuid string) (types.Lease, error) {
	lease, err := k.Leases.Get(ctx, uuid)
	if err != nil {
		return types.Lease{}, types.ErrLeaseNotFound
	}
	return lease, nil
}

// SetLease sets a Lease in the store.
func (k *Keeper) SetLease(ctx context.Context, lease types.Lease) error {
	return k.Leases.Set(ctx, lease.Uuid, lease)
}

// GetNextLeaseSequence returns the next sequence number for deterministic UUID generation.
func (k *Keeper) GetNextLeaseSequence(ctx context.Context) (uint64, error) {
	return k.LeaseSequence.Next(ctx)
}

// GetAllLeases returns all Leases in the store.
func (k *Keeper) GetAllLeases(ctx context.Context) ([]types.Lease, error) {
	var leases []types.Lease

	err := k.Leases.Walk(ctx, nil, func(_ string, lease types.Lease) (bool, error) {
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
		leaseUUID, err := iter.PrimaryKey()
		if err != nil {
			return nil, err
		}
		lease, err := k.Leases.Get(ctx, leaseUUID)
		if err != nil {
			return nil, err
		}
		leases = append(leases, lease)
	}

	return leases, nil
}

// GetLeasesByProviderUUID returns all Leases for a given provider UUID.
func (k *Keeper) GetLeasesByProviderUUID(ctx context.Context, providerUUID string) ([]types.Lease, error) {
	var leases []types.Lease

	iter, err := k.Leases.Indexes.Provider.MatchExact(ctx, providerUUID)
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		leaseUUID, err := iter.PrimaryKey()
		if err != nil {
			return nil, err
		}
		lease, err := k.Leases.Get(ctx, leaseUUID)
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
		leaseUUID, err := iter.PrimaryKey()
		if err != nil {
			return 0, err
		}
		lease, err := k.Leases.Get(ctx, leaseUUID)
		if err != nil {
			return 0, err
		}
		if lease.State == types.LEASE_STATE_ACTIVE {
			count++
		}
	}

	return count, nil
}

// GetCreditBalances returns all credit balances from the bank module for a tenant.
func (k *Keeper) GetCreditBalances(ctx context.Context, tenant string) (sdk.Coins, error) {
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant)
	if err != nil {
		return nil, err
	}
	return k.bankKeeper.GetAllBalances(ctx, creditAddr), nil
}

// GetCreditBalance returns the credit balance for a specific denom from the bank module for a tenant.
func (k *Keeper) GetCreditBalance(ctx context.Context, tenant string, denom string) (sdk.Coin, error) {
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant)
	if err != nil {
		return sdk.Coin{}, err
	}
	return k.bankKeeper.GetBalance(ctx, creditAddr, denom), nil
}

// CalculateWithdrawableForLease calculates the amounts that can be withdrawn from a lease.
// It considers the time since last settlement and the credit balance available.
// Returns a Coins collection (one entry per denom).
func (k *Keeper) CalculateWithdrawableForLease(ctx context.Context, lease types.Lease) sdk.Coins {
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
			return sdk.NewCoins()
		}
	}

	if duration <= 0 {
		return sdk.NewCoins()
	}

	// Calculate total accrued with overflow handling
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
		// Log overflow error and return empty
		k.logger.Error("accrual calculation overflow in withdrawable calculation",
			"lease_uuid", lease.Uuid,
			"error", err,
		)
		return sdk.NewCoins()
	}

	if accruedAmounts.IsZero() {
		return sdk.NewCoins()
	}

	// Get credit balances to cap the withdrawable amounts
	creditBalances, err := k.GetCreditBalances(ctx, lease.Tenant)
	if err != nil {
		return sdk.NewCoins()
	}

	// For each denom, return the minimum of accrued amount and available balance
	result := sdk.NewCoins()
	for _, accrued := range accruedAmounts {
		balance := creditBalances.AmountOf(accrued.Denom)
		if balance.LT(accrued.Amount) {
			if balance.IsPositive() {
				result = result.Add(sdk.NewCoin(accrued.Denom, balance))
			}
		} else {
			result = result.Add(accrued)
		}
	}

	return result
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

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	// Calculate duration since last settlement
	duration := blockTime.Sub(lease.LastSettledAt)
	if duration < 0 {
		duration = 0
	}

	// Check tenant's current credit balances
	creditBalances, err := k.GetCreditBalances(ctx, lease.Tenant)
	if err != nil {
		return false, err
	}

	// Calculate what would be accrued for each denom
	items := make([]LeaseItemWithPrice, 0, len(lease.Items))
	for _, item := range lease.Items {
		items = append(items, LeaseItemWithPrice{
			SkuUUID:              item.SkuUuid,
			Quantity:             item.Quantity,
			LockedPricePerSecond: item.LockedPrice,
		})
	}

	// If duration is zero, no accrual - check if any balance is exhausted
	shouldClose := false
	if duration > 0 {
		accruedAmounts, calcErr := CalculateTotalAccruedForLease(items, duration)
		if calcErr == nil {
			// Check if any denom's accrued amount exceeds the balance
			for _, accrued := range accruedAmounts {
				balance := creditBalances.AmountOf(accrued.Denom)
				if accrued.Amount.GTE(balance) {
					shouldClose = true
					break
				}
			}
		}
	} else {
		// Check if any required denom balance is zero
		for _, item := range lease.Items {
			balance := creditBalances.AmountOf(item.LockedPrice.Denom)
			if balance.IsZero() {
				shouldClose = true
				break
			}
		}
	}

	if !shouldClose {
		return false, nil
	}

	// Perform final settlement (transfer remaining balance to provider)
	settledAmounts, err := k.settleAndCloseLease(ctx, lease, blockTime)
	if err != nil {
		return false, err
	}

	// Emit event for auto-closed lease
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeLeaseAutoClose,
			sdk.NewAttribute(types.AttributeKeyLeaseUUID, lease.Uuid),
			sdk.NewAttribute(types.AttributeKeyTenant, lease.Tenant),
			sdk.NewAttribute(types.AttributeKeyProviderUUID, lease.ProviderUuid),
			sdk.NewAttribute(types.AttributeKeySettledAmounts, settledAmounts.String()),
			sdk.NewAttribute(types.AttributeKeyReason, "credit_exhausted"),
		),
	)

	k.logger.Info("auto-closed exhausted lease",
		"lease_uuid", lease.Uuid,
		"tenant", lease.Tenant,
		"settled_amounts", settledAmounts.String(),
	)

	return true, nil
}

// settleAndCloseLease performs final settlement and closes a lease.
// This is used by both manual close and auto-close operations.
// Returns the settled amounts (one per denom).
func (k *Keeper) settleAndCloseLease(ctx context.Context, lease *types.Lease, closeTime time.Time) (sdk.Coins, error) {
	// Calculate duration since last settlement
	duration := closeTime.Sub(lease.LastSettledAt)
	if duration < 0 {
		duration = 0
	}

	settledAmounts := sdk.NewCoins()

	if duration > 0 {
		// Calculate accrued amounts
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
			// On overflow, use empty coins (better than failing the close)
			accruedAmounts = sdk.NewCoins()
		}

		// Get credit balances
		creditAddr, err := types.DeriveCreditAddressFromBech32(lease.Tenant)
		if err != nil {
			return sdk.NewCoins(), err
		}
		creditBalances := k.bankKeeper.GetAllBalances(ctx, creditAddr)

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
			// Get provider payout address
			provider, err := k.skuKeeper.GetProvider(ctx, lease.ProviderUuid)
			if err != nil {
				return sdk.NewCoins(), types.ErrProviderNotFound.Wrapf("provider_uuid %s not found", lease.ProviderUuid)
			}

			payoutAddr, err := sdk.AccAddressFromBech32(provider.PayoutAddress)
			if err != nil {
				return sdk.NewCoins(), types.ErrProviderNotFound.Wrapf("invalid payout address: %s", err)
			}

			if err := k.bankKeeper.SendCoins(
				ctx,
				creditAddr,
				payoutAddr,
				transferAmounts,
			); err != nil {
				return sdk.NewCoins(), types.ErrInvalidCreditOperation.Wrapf("failed to transfer: %s", err)
			}
		}

		settledAmounts = transferAmounts
	}

	// Update lease state
	lease.State = types.LEASE_STATE_CLOSED
	lease.ClosedAt = &closeTime
	lease.LastSettledAt = closeTime

	if err := k.SetLease(ctx, *lease); err != nil {
		return sdk.NewCoins(), err
	}

	// Decrement active lease count in credit account
	creditAccount, err := k.GetCreditAccount(ctx, lease.Tenant)
	if err == nil && creditAccount.ActiveLeaseCount > 0 {
		creditAccount.ActiveLeaseCount--
		if err := k.SetCreditAccount(ctx, creditAccount); err != nil {
			return settledAmounts, err
		}
	}

	return settledAmounts, nil
}

// CountPendingLeasesByTenant counts the number of pending leases for a tenant.
// This method uses the CreditAccount's cached PendingLeaseCount for O(1) performance.
// Falls back to iterating leases if credit account doesn't exist.
func (k *Keeper) CountPendingLeasesByTenant(ctx context.Context, tenant string) (uint64, error) {
	// Try to get from credit account's cached count (O(1))
	creditAccount, err := k.GetCreditAccount(ctx, tenant)
	if err == nil {
		return creditAccount.PendingLeaseCount, nil
	}

	// Fall back to iteration if credit account doesn't exist
	return k.countPendingLeasesByTenantScan(ctx, tenant)
}

// countPendingLeasesByTenantScan counts pending leases by iterating (O(n)).
// This is used as a fallback when credit account doesn't exist.
func (k *Keeper) countPendingLeasesByTenantScan(ctx context.Context, tenant string) (uint64, error) {
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
		leaseUUID, err := iter.PrimaryKey()
		if err != nil {
			return 0, err
		}
		lease, err := k.Leases.Get(ctx, leaseUUID)
		if err != nil {
			return 0, err
		}
		if lease.State == types.LEASE_STATE_PENDING {
			count++
		}
	}

	return count, nil
}

// GetPendingLeases returns all leases in PENDING state.
// Uses the state index for O(n) where n is pending leases, not all leases.
func (k *Keeper) GetPendingLeases(ctx context.Context) ([]types.Lease, error) {
	return k.GetLeasesByState(ctx, types.LEASE_STATE_PENDING)
}

// GetLeasesByState returns all leases with a specific state.
// Uses the state index for efficient lookup.
func (k *Keeper) GetLeasesByState(ctx context.Context, state types.LeaseState) ([]types.Lease, error) {
	var leases []types.Lease

	iter, err := k.Leases.Indexes.State.MatchExact(ctx, int32(state))
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		leaseUUID, err := iter.PrimaryKey()
		if err != nil {
			return nil, err
		}
		lease, err := k.Leases.Get(ctx, leaseUUID)
		if err != nil {
			return nil, err
		}
		leases = append(leases, lease)
	}

	return leases, nil
}

// GetActiveLeases returns all leases in ACTIVE state.
// Uses the state index for efficient lookup.
func (k *Keeper) GetActiveLeases(ctx context.Context) ([]types.Lease, error) {
	return k.GetLeasesByState(ctx, types.LEASE_STATE_ACTIVE)
}

// GetPendingLeasesByProvider returns all pending leases for a specific provider.
// Uses provider index and filters by state.
func (k *Keeper) GetPendingLeasesByProvider(ctx context.Context, providerUUID string) ([]types.Lease, error) {
	var pendingLeases []types.Lease

	iter, err := k.Leases.Indexes.Provider.MatchExact(ctx, providerUUID)
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		leaseUUID, err := iter.PrimaryKey()
		if err != nil {
			return nil, err
		}
		lease, err := k.Leases.Get(ctx, leaseUUID)
		if err != nil {
			return nil, err
		}
		if lease.State == types.LEASE_STATE_PENDING {
			pendingLeases = append(pendingLeases, lease)
		}
	}

	return pendingLeases, nil
}

// ExpirePendingLease expires a pending lease, unlocking the tenant's credit.
// This is called by the EndBlocker when a lease exceeds the pending timeout.
func (k *Keeper) ExpirePendingLease(ctx context.Context, lease *types.Lease) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	// Validate lease state
	if lease.State != types.LEASE_STATE_PENDING {
		return types.ErrLeaseNotPending.Wrapf("lease %s is not pending", lease.Uuid)
	}

	// Update lease state to EXPIRED
	lease.State = types.LEASE_STATE_EXPIRED
	lease.ExpiredAt = &blockTime

	if err := k.SetLease(ctx, *lease); err != nil {
		return err
	}

	// Decrement pending lease count in credit account
	creditAccount, err := k.GetCreditAccount(ctx, lease.Tenant)
	if err == nil && creditAccount.PendingLeaseCount > 0 {
		creditAccount.PendingLeaseCount--
		if err := k.SetCreditAccount(ctx, creditAccount); err != nil {
			return err
		}
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeLeaseExpired,
			sdk.NewAttribute(types.AttributeKeyLeaseUUID, lease.Uuid),
			sdk.NewAttribute(types.AttributeKeyTenant, lease.Tenant),
			sdk.NewAttribute(types.AttributeKeyProviderUUID, lease.ProviderUuid),
			sdk.NewAttribute(types.AttributeKeyReason, "pending_timeout"),
		),
	)

	k.logger.Info("expired pending lease",
		"lease_uuid", lease.Uuid,
		"tenant", lease.Tenant,
		"provider_uuid", lease.ProviderUuid,
	)

	return nil
}

// EndBlocker processes pending lease expirations.
// It checks all pending leases and expires those that have exceeded the pending timeout.
// Rate limited to MaxPendingLeaseExpirationsPerBlock to prevent DoS attacks.
func (k *Keeper) EndBlocker(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}

	// Get pending timeout duration
	// #nosec G115 -- PendingTimeout is validated in params to be within safe bounds (60-86400 seconds)
	pendingTimeout := time.Duration(params.PendingTimeout) * time.Second

	// Get all pending leases
	pendingLeases, err := k.GetPendingLeases(ctx)
	if err != nil {
		return err
	}

	// Rate limit: process max N expirations per block to prevent DoS
	const maxExpirationsPerBlock = 100
	expiredCount := 0

	for i := range pendingLeases {
		lease := &pendingLeases[i]

		// Check if lease has exceeded pending timeout
		expirationTime := lease.CreatedAt.Add(pendingTimeout)
		if blockTime.After(expirationTime) {
			if err := k.ExpirePendingLease(ctx, lease); err != nil {
				k.logger.Error("failed to expire pending lease",
					"lease_uuid", lease.Uuid,
					"error", err,
				)
				continue
			}
			expiredCount++

			// Rate limit check
			if expiredCount >= maxExpirationsPerBlock {
				k.logger.Warn("reached max pending lease expirations per block",
					"expired_count", expiredCount,
					"remaining", len(pendingLeases)-i-1,
				)
				break
			}
		}
	}

	if expiredCount > 0 {
		k.logger.Info("expired pending leases in EndBlocker",
			"count", expiredCount,
		)
	}

	return nil
}
