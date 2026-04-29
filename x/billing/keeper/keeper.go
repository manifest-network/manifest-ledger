package keeper

import (
	"context"
	"errors"
	"fmt"
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
	// ProviderState is a compound index that indexes Leases by (provider_uuid, state).
	// This enables O(1) lookup of leases by provider and state combined.
	ProviderState *indexes.Multi[collections.Pair[string, int32], string, types.Lease]
	// TenantState is a compound index that indexes Leases by (tenant, state).
	// This enables O(1) lookup of leases by tenant and state combined.
	TenantState *indexes.Multi[collections.Pair[sdk.AccAddress, int32], string, types.Lease]
	// StateCreatedAt is a compound index that indexes Leases by (state, created_at).
	// This enables efficient time-based queries for leases in a specific state,
	// particularly for EndBlocker pending lease expiration.
	StateCreatedAt *indexes.Multi[collections.Pair[int32, time.Time], string, types.Lease]
}

// IndexesList returns all indexes defined for the Lease collection.
func (i LeaseIndexes) IndexesList() []collections.Index[string, types.Lease] {
	return []collections.Index[string, types.Lease]{i.Tenant, i.Provider, i.State, i.ProviderState, i.TenantState, i.StateCreatedAt}
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
		ProviderState: indexes.NewMulti(
			sb,
			types.LeaseByProviderStateIndexKey,
			"leases_by_provider_state",
			collections.PairKeyCodec(collections.StringKey, collections.Int32Key),
			collections.StringKey,
			func(_ string, lease types.Lease) (collections.Pair[string, int32], error) {
				return collections.Join(lease.ProviderUuid, int32(lease.State)), nil
			},
		),
		TenantState: indexes.NewMulti(
			sb,
			types.LeaseByTenantStateIndexKey,
			"leases_by_tenant_state",
			collections.PairKeyCodec(sdk.AccAddressKey, collections.Int32Key),
			collections.StringKey,
			func(_ string, lease types.Lease) (collections.Pair[sdk.AccAddress, int32], error) {
				tenantAddr, err := sdk.AccAddressFromBech32(lease.Tenant)
				if err != nil {
					return collections.Pair[sdk.AccAddress, int32]{}, err
				}
				return collections.Join(tenantAddr, int32(lease.State)), nil
			},
		),
		StateCreatedAt: indexes.NewMulti(
			sb,
			types.LeaseByStateCreatedAtIndexKey,
			"leases_by_state_created_at",
			collections.PairKeyCodec(collections.Int32Key, sdk.TimeKey),
			collections.StringKey,
			func(_ string, lease types.Lease) (collections.Pair[int32, time.Time], error) {
				return collections.Join(int32(lease.State), lease.CreatedAt), nil
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
	// LeaseBySKUIndex is a many-to-many index from SKU UUID to Lease UUID.
	// Since a lease can contain multiple SKUs, this is managed as a separate Map
	// with composite key (sku_uuid, lease_uuid) rather than as part of LeaseIndexes.
	LeaseBySKUIndex collections.Map[collections.Pair[string, string], bool]
	// CustomDomainIndex is the unique reverse index from custom_domain to
	// CustomDomainTarget{lease_uuid, service_name}. Maintained automatically by
	// SetLease via reconcileCustomDomainIndex, which derives entries from each
	// item's (lease.State, item.CustomDomain): the entry exists iff the lease is
	// in PENDING or ACTIVE state and the item's CustomDomain is non-empty. Not
	// part of LeaseIndexes because empty domains must not be indexed and
	// lifecycle removal is conditional on state. Storage-level uniqueness is
	// enforced inside reconcileCustomDomainIndex; callers that mutate
	// LeaseItem.CustomDomain outside SetLeaseItemCustomDomain should wrap their
	// SetLease call in a CacheContext to roll back cleanly on
	// ErrCustomDomainAlreadyClaimed.
	CustomDomainIndex collections.Map[string, types.CustomDomainTarget]

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
		LeaseBySKUIndex: collections.NewMap(
			sb,
			types.LeaseBySKUIndexKey,
			"leases_by_sku",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey), // (sku_uuid, lease_uuid)
			collections.BoolValue,
		),
		CustomDomainIndex: collections.NewMap(
			sb,
			types.CustomDomainIndexKey,
			"leases_by_custom_domain",
			collections.StringKey,
			codec.CollValue[types.CustomDomainTarget](cdc),
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

	// Validate and set leases
	// NOTE: This validation requires the SKU module to be initialized first.
	// Genesis order ensures: sku -> billing (see app/app.go)
	for _, lease := range gs.Leases {
		// Validate provider exists in SKU module
		if _, err := k.skuKeeper.GetProvider(ctx, lease.ProviderUuid); err != nil {
			return fmt.Errorf("lease %s references non-existent provider %s: %w",
				lease.Uuid, lease.ProviderUuid, err)
		}

		// Validate each SKU exists and belongs to the lease's provider
		for i, item := range lease.Items {
			sku, err := k.skuKeeper.GetSKU(ctx, item.SkuUuid)
			if err != nil {
				return fmt.Errorf("lease %s item %d references non-existent SKU %s: %w",
					lease.Uuid, i, item.SkuUuid, err)
			}
			if sku.ProviderUuid != lease.ProviderUuid {
				return fmt.Errorf("lease %s item %d SKU %s belongs to provider %s, not %s",
					lease.Uuid, i, item.SkuUuid, sku.ProviderUuid, lease.ProviderUuid)
			}
		}

		// SetLease populates LeaseBySKUIndex and reconciles the custom_domain
		// reverse index from (state, custom_domain). Storage-level uniqueness
		// detects two genesis leases claiming the same domain via
		// ErrCustomDomainAlreadyClaimed.
		if err := k.SetLease(ctx, lease); err != nil {
			return err
		}
	}

	for _, ca := range gs.CreditAccounts {
		// Use SetCreditAccount to also populate the reverse index
		if err := k.SetCreditAccount(ctx, ca); err != nil {
			return err
		}
	}

	// Restore UUID generation sequence so new leases don't collide
	// with previously generated UUIDs after a genesis export/import cycle.
	if gs.LeaseSequence > 0 {
		if err := k.LeaseSequence.Set(ctx, gs.LeaseSequence); err != nil {
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

	leaseSeq, err := k.LeaseSequence.Peek(ctx)
	if err != nil {
		panic(err)
	}

	return &types.GenesisState{
		Params:         params,
		Leases:         leases,
		CreditAccounts: creditAccounts,
		LeaseSequence:  leaseSeq,
	}
}

// Lease operations

// GetLease returns a Lease by its UUID.
func (k *Keeper) GetLease(ctx context.Context, uuid string) (types.Lease, error) {
	lease, err := k.Leases.Get(ctx, uuid)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.Lease{}, types.ErrLeaseNotFound
		}
		return types.Lease{}, err
	}
	return lease, nil
}

// SetLease sets a Lease in the store and reconciles all derived indexes.
// The SKU index update is idempotent. The custom_domain reverse index is
// reconciled per-item from (lease.State, item.CustomDomain) for each item: if
// the lease is editable (PENDING or ACTIVE) and the item's CustomDomain is
// non-empty, an index entry points the domain at (lease.Uuid, item.ServiceName);
// otherwise the entry (if any) is removed. This collapses lifecycle cleanup
// into a single rule and removes the need for callers to clear index entries
// around state transitions.
//
// The previous lease (if any) is read once to detect renames (the old domain
// must be released even if the new domain belongs to the same item) and to
// enforce uniqueness at the storage layer (an in-flight write that would
// overwrite another claim returns ErrCustomDomainAlreadyClaimed).
func (k *Keeper) SetLease(ctx context.Context, lease types.Lease) error {
	prev, hadPrev, err := k.getPreviousLease(ctx, lease.Uuid)
	if err != nil {
		return err
	}

	if err := k.Leases.Set(ctx, lease.Uuid, lease); err != nil {
		return err
	}

	for _, item := range lease.Items {
		key := collections.Join(item.SkuUuid, lease.Uuid)
		if err := k.LeaseBySKUIndex.Set(ctx, key, true); err != nil {
			return err
		}
	}

	return k.reconcileCustomDomainIndex(ctx, prev, hadPrev, lease)
}

// getPreviousLease loads the existing lease at uuid (if any) before SetLease
// overwrites it. Returns (lease, true, nil) when present, (zero, false, nil)
// when not, or (zero, false, err) on a real error.
func (k *Keeper) getPreviousLease(ctx context.Context, uuid string) (types.Lease, bool, error) {
	prev, err := k.Leases.Get(ctx, uuid)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.Lease{}, false, nil
		}
		return types.Lease{}, false, err
	}
	return prev, true, nil
}

// reconcileCustomDomainIndex enforces the per-item (state, custom_domain) →
// index invariant after a SetLease write. It walks both the previous and
// current item slices, releases entries for items whose domain changed or
// whose lease moved to terminal, and installs entries for items in editable
// state that carry a non-empty custom_domain. Returns ErrCustomDomainAlreadyClaimed
// if installing an entry would overwrite a claim by a different (lease, item)
// — a defence-in-depth check above SetLeaseItemCustomDomain's pre-check.
//
// Items in lease.Items are immutable post-creation today, so the "item removed
// in update" branch is theoretical but cheap to support.
func (k *Keeper) reconcileCustomDomainIndex(ctx context.Context, prev types.Lease, hadPrev bool, lease types.Lease) error {
	editable := lease.State == types.LEASE_STATE_PENDING || lease.State == types.LEASE_STATE_ACTIVE

	// Build per-service maps of the live (non-empty) domain claims for both
	// snapshots. Service_name is the lease's commit-time uniqueness key, so
	// it suffices as the diff key. Empty service_name is a valid map key
	// (used by 1-item legacy leases — only one entry per lease in that case).
	prevByService := map[string]string{}
	if hadPrev {
		for _, item := range prev.Items {
			if item.CustomDomain != "" {
				prevByService[item.ServiceName] = item.CustomDomain
			}
		}
	}
	newByService := map[string]string{}
	for _, item := range lease.Items {
		if item.CustomDomain != "" {
			newByService[item.ServiceName] = item.CustomDomain
		}
	}

	// Release any prev entry whose live domain disappeared, changed, or whose
	// lease moved to terminal state.
	for s, prevDomain := range prevByService {
		if !editable || newByService[s] != prevDomain {
			if err := k.CustomDomainIndex.Remove(ctx, prevDomain); err != nil &&
				!errors.Is(err, collections.ErrNotFound) {
				return err
			}
		}
	}

	if !editable {
		return nil
	}

	// Install / verify entries for current items. Storage-level uniqueness
	// rejects overwriting a different (lease, service) pair.
	for s, newDomain := range newByService {
		existing, err := k.CustomDomainIndex.Get(ctx, newDomain)
		switch {
		case err == nil:
			switch {
			case existing.LeaseUuid == lease.Uuid && existing.ServiceName == s:
				continue // idempotent re-set
			case existing.LeaseUuid == lease.Uuid:
				return types.ErrCustomDomainAlreadyClaimed.Wrapf(
					"domain %q is already claimed by item %q on this lease",
					newDomain, existing.ServiceName,
				)
			default:
				return types.ErrCustomDomainAlreadyClaimed.Wrapf(
					"domain %q is already claimed by lease %s item %q",
					newDomain, existing.LeaseUuid, existing.ServiceName,
				)
			}
		case errors.Is(err, collections.ErrNotFound):
			if err := k.CustomDomainIndex.Set(ctx, newDomain, types.CustomDomainTarget{
				LeaseUuid:   lease.Uuid,
				ServiceName: s,
			}); err != nil {
				return err
			}
		default:
			return err
		}
	}

	return nil
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

// maxGetLeasesByProviderUUID is a safety limit to prevent unbounded memory usage.
const maxGetLeasesByProviderUUID = 10_000

// GetLeasesByProviderUUID returns leases for a given provider UUID.
// Results are capped at maxGetLeasesByProviderUUID to prevent unbounded memory usage.
// For production query paths, use the paginated querier or IterateLeasesByProvider instead.
func (k *Keeper) GetLeasesByProviderUUID(ctx context.Context, providerUUID string) ([]types.Lease, error) {
	var leases []types.Lease

	err := k.IterateLeasesByProvider(ctx, providerUUID, func(lease types.Lease) (stop bool, err error) {
		leases = append(leases, lease)
		return len(leases) >= maxGetLeasesByProviderUUID, nil
	})
	if err != nil {
		return nil, err
	}

	return leases, nil
}

// IterateLeasesByProvider iterates over all leases for a provider, calling the
// callback for each lease. The callback should return (stop=true, nil) to stop
// iteration early, or (false, err) to abort with an error.
// This is the preferred method for processing large numbers of leases as it
// doesn't load all leases into memory at once.
func (k *Keeper) IterateLeasesByProvider(ctx context.Context, providerUUID string, cb func(lease types.Lease) (stop bool, err error)) error {
	iter, err := k.Leases.Indexes.Provider.MatchExact(ctx, providerUUID)
	if err != nil {
		return err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		leaseUUID, err := iter.PrimaryKey()
		if err != nil {
			return err
		}
		lease, err := k.Leases.Get(ctx, leaseUUID)
		if err != nil {
			return err
		}

		stop, err := cb(lease)
		if err != nil {
			return err
		}
		if stop {
			break
		}
	}

	return nil
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
		if errors.Is(err, collections.ErrNotFound) {
			return types.CreditAccount{}, types.ErrCreditAccountNotFound
		}
		return types.CreditAccount{}, err
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
	if !errors.Is(err, types.ErrCreditAccountNotFound) {
		return 0, err
	}

	// Fall back to iteration if credit account doesn't exist
	return k.countLeasesByTenantAndStateScan(ctx, tenant, types.LEASE_STATE_ACTIVE)
}

// countLeasesByTenantAndStateScan counts leases in a specific state using the TenantState
// compound index. This is used as a fallback when credit account doesn't exist.
func (k *Keeper) countLeasesByTenantAndStateScan(ctx context.Context, tenant string, state types.LeaseState) (uint64, error) {
	var count uint64

	// Convert bech32 address to bytes for index lookup
	tenantAddr, err := sdk.AccAddressFromBech32(tenant)
	if err != nil {
		return 0, err
	}

	// Use the TenantState compound index for efficient lookup by (tenant, state)
	key := collections.Join(tenantAddr, int32(state))
	iter, err := k.Leases.Indexes.TenantState.MatchExact(ctx, key)
	if err != nil {
		return 0, err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		count++
	}

	return count, nil
}

// GetCreditBalance returns the credit balance for a specific denom from the bank module for a tenant.
func (k *Keeper) GetCreditBalance(ctx context.Context, tenant string, denom string) (sdk.Coin, error) {
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant)
	if err != nil {
		return sdk.Coin{}, err
	}
	return k.bankKeeper.GetBalance(ctx, creditAddr, denom), nil
}

// getCreditBalancesForDenoms returns credit balances for only the specified denoms,
// using per-denom GetBalance to avoid loading dust from unrelated token sends.
func (k *Keeper) getCreditBalancesForDenoms(ctx context.Context, tenant string, denoms []string) (sdk.Coins, error) {
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant)
	if err != nil {
		return nil, err
	}
	coins := sdk.NewCoins()
	for _, denom := range denoms {
		bal := k.bankKeeper.GetBalance(ctx, creditAddr, denom)
		if bal.IsPositive() {
			coins = coins.Add(bal)
		}
	}
	return coins, nil
}

// getRelevantDenomsForTenant collects the unique denoms from a tenant's active and pending leases
// plus any denoms in the reserved amounts. This avoids GetAllBalances which loads dust from spam.
// Uses streaming iteration with a cap to avoid loading all leases into memory.
func (k *Keeper) getRelevantDenomsForTenant(ctx context.Context, tenant string, reservedAmounts sdk.Coins) ([]string, error) {
	denomSet := make(map[string]struct{})
	denoms := make([]string, 0, 4)

	addDenom := func(d string) {
		if _, ok := denomSet[d]; !ok {
			denomSet[d] = struct{}{}
			denoms = append(denoms, d)
		}
	}

	// Include denoms from reserved amounts
	for _, coin := range reservedAmounts {
		addDenom(coin.Denom)
	}

	tenantAddr, err := sdk.AccAddressFromBech32(tenant)
	if err != nil {
		return nil, err
	}

	// Include denoms from active and pending leases via streaming iteration.
	// Cap to MaxCreditEstimateLeases per state to bound query cost.
	for _, state := range []types.LeaseState{types.LEASE_STATE_ACTIVE, types.LEASE_STATE_PENDING} {
		key := collections.Join(tenantAddr, int32(state))
		iter, err := k.Leases.Indexes.TenantState.MatchExact(ctx, key)
		if err != nil {
			return nil, err
		}

		var count int
		for ; iter.Valid(); iter.Next() {
			if count >= int(types.MaxCreditEstimateLeases) {
				break
			}
			count++

			leaseUUID, err := iter.PrimaryKey()
			if err != nil {
				iter.Close()
				return nil, err
			}
			lease, err := k.Leases.Get(ctx, leaseUUID)
			if err != nil {
				iter.Close()
				return nil, err
			}
			for _, item := range lease.Items {
				addDenom(item.LockedPrice.Denom)
			}
		}
		iter.Close()
	}

	return denoms, nil
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
	items := LeaseItemsToWithPrice(lease.Items)
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

	// Get credit balances for only the lease's denoms to cap the withdrawable amounts
	creditBalances, err := k.getCreditBalancesForDenoms(ctx, lease.Tenant, leaseItemDenoms(lease.Items))
	if err != nil {
		k.logger.Error("failed to get credit balances for withdrawable calculation",
			"lease_uuid", lease.Uuid,
			"tenant", lease.Tenant,
			"error", err,
		)
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

// ShouldAutoCloseLease checks if a lease should be auto-closed due to exhausted credit.
// This implements "lazy evaluation" / "check on touch" pattern.
// Returns true if the lease should be closed, along with the close time to use.
// This is O(1) per lease check, avoiding O(n) scanning of all leases in EndBlock.
//
// IMPORTANT: This function does NOT modify any state. The caller is responsible for:
// 1. Calling PerformSettlementSilent to settle the lease
// 2. Updating the lease state (State, ClosedAt, LastSettledAt)
// 3. Updating the credit account's ActiveLeaseCount
// 4. Persisting the changes
// 5. Emitting the appropriate event
//
// The function performs settlement calculation to determine if the balance would be exhausted
// after accrual, rather than just checking the current balance. This ensures leases are
// closed promptly when credit runs out, even if the balance isn't exactly zero yet.
func (k *Keeper) ShouldAutoCloseLease(ctx context.Context, lease *types.Lease) (shouldClose bool, closeTime time.Time, err error) {
	// Only check active leases
	if lease.State != types.LEASE_STATE_ACTIVE {
		return false, time.Time{}, nil
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	// Calculate duration since last settlement
	duration := blockTime.Sub(lease.LastSettledAt)
	if duration < 0 {
		// LastSettledAt is in the future - this indicates data corruption or clock issues.
		// Return an error rather than silently masking the issue, as this could leave
		// leases in an invalid state where credit is exhausted but the lease stays active.
		k.logger.Error("data inconsistency: LastSettledAt is in the future",
			"lease_uuid", lease.Uuid,
			"tenant", lease.Tenant,
			"last_settled_at", lease.LastSettledAt,
			"block_time", blockTime,
			"difference", -duration,
		)
		return false, time.Time{}, types.ErrInvalidLease.Wrapf(
			"lease %s has LastSettledAt (%s) in the future relative to block time (%s)",
			lease.Uuid, lease.LastSettledAt, blockTime,
		)
	}

	// Check tenant's credit balances for only the lease's denoms
	creditBalances, err := k.getCreditBalancesForDenoms(ctx, lease.Tenant, leaseItemDenoms(lease.Items))
	if err != nil {
		return false, time.Time{}, err
	}

	// Calculate what would be accrued for each denom
	items := LeaseItemsToWithPrice(lease.Items)

	// If duration is zero, no accrual - check if any balance is exhausted
	shouldClose = false
	if duration > 0 {
		accruedAmounts, calcErr := CalculateTotalAccruedForLease(items, duration)
		if calcErr != nil {
			// Overflow in accrual calculation means the accrued amount is extremely large,
			// which certainly exceeds any credit balance. Defensively close the lease.
			k.logger.Error("accrual calculation overflow in auto-close check, closing lease defensively",
				"lease_uuid", lease.Uuid,
				"tenant", lease.Tenant,
				"duration", duration.String(),
				"error", calcErr,
			)
			shouldClose = true
		} else {
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
		return false, time.Time{}, nil
	}

	return true, blockTime, nil
}

// AutoCloseLeaseResult holds the result of an auto-close operation.
type AutoCloseLeaseResult struct {
	TransferAmounts sdk.Coins
}

// AutoCloseLease performs the auto-close sequence for a lease with exhausted credit.
// It settles the lease, updates its state to CLOSED, decrements the active lease count,
// and releases the reservation. All changes are applied to the provided context.
// The caller is responsible for CacheContext management and event emission.
func (k *Keeper) AutoCloseLease(ctx context.Context, lease *types.Lease, closeTime time.Time, minLeaseDuration uint64) (*AutoCloseLeaseResult, error) {
	result, err := k.PerformSettlementSilent(ctx, lease, closeTime)
	if err != nil {
		return nil, err
	}

	lease.State = types.LEASE_STATE_CLOSED
	lease.ClosedAt = &closeTime
	lease.LastSettledAt = closeTime
	lease.ClosureReason = types.ClosureReasonCreditExhausted

	if err := k.SetLease(ctx, *lease); err != nil {
		return nil, err
	}

	creditAccount, err := k.GetCreditAccount(ctx, lease.Tenant)
	if err != nil {
		return nil, err
	}

	k.DecrementActiveLeaseCount(&creditAccount, lease.Uuid)
	k.ReleaseLeaseReservation(&creditAccount, lease, minLeaseDuration)

	if err := k.SetCreditAccount(ctx, creditAccount); err != nil {
		return nil, err
	}

	return &AutoCloseLeaseResult{TransferAmounts: result.TransferAmounts}, nil
}

// DecrementActiveLeaseCount decrements the active lease count on a credit account.
// If the count is already zero, it logs a warning about data inconsistency but does not fail.
// This helper ensures consistent handling of lease count decrements across all code paths.
func (k *Keeper) DecrementActiveLeaseCount(ca *types.CreditAccount, leaseUUID string) {
	if ca.ActiveLeaseCount > 0 {
		ca.ActiveLeaseCount--
	} else {
		k.logger.Warn("data inconsistency: active lease count already zero",
			"tenant", ca.Tenant,
			"lease_uuid", leaseUUID,
		)
	}
}

// DecrementPendingLeaseCount decrements the pending lease count on a credit account.
// If the count is already zero, it logs a warning about data inconsistency but does not fail.
// This helper ensures consistent handling of lease count decrements across all code paths.
func (k *Keeper) DecrementPendingLeaseCount(ca *types.CreditAccount, leaseUUID string) {
	if ca.PendingLeaseCount > 0 {
		ca.PendingLeaseCount--
	} else {
		k.logger.Warn("data inconsistency: pending lease count already zero",
			"tenant", ca.Tenant,
			"lease_uuid", leaseUUID,
		)
	}
}

// ReleaseLeaseReservation releases the reservation for a lease from a credit account.
// It checks for potential underflow (releasing more than reserved) and logs a warning
// if detected, which indicates a data inconsistency. The release proceeds regardless
// to maintain forward progress (SubtractReservation clamps to zero).
func (k *Keeper) ReleaseLeaseReservation(ca *types.CreditAccount, lease *types.Lease, minLeaseDuration uint64) {
	reservationAmount := types.GetLeaseReservationAmount(lease, minLeaseDuration)

	// Check for underflow before release (for observability)
	underflows := types.CheckReservationRelease(ca.ReservedAmounts, reservationAmount)
	if len(underflows) > 0 {
		k.logger.Warn("data inconsistency: reservation release would underflow",
			"tenant", ca.Tenant,
			"lease_uuid", lease.Uuid,
			"underflows", underflows,
			"current_reserved", ca.ReservedAmounts.String(),
			"attempting_to_release", reservationAmount.String(),
		)
	}

	// Subtract reservation (clamps negative values to zero)
	ca.ReservedAmounts = types.SubtractReservation(ca.ReservedAmounts, reservationAmount)
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
	if !errors.Is(err, types.ErrCreditAccountNotFound) {
		return 0, err
	}

	// Fall back to iteration if credit account doesn't exist
	return k.countLeasesByTenantAndStateScan(ctx, tenant, types.LEASE_STATE_PENDING)
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
// Uses the compound (provider, state) index for O(1) direct lookup instead of filtering.
func (k *Keeper) GetPendingLeasesByProvider(ctx context.Context, providerUUID string) ([]types.Lease, error) {
	return k.GetLeasesByProviderAndState(ctx, providerUUID, types.LEASE_STATE_PENDING)
}

// GetLeasesByProviderAndState returns leases for a provider with a specific state.
// Uses the compound (provider, state) index for O(1) direct lookup.
func (k *Keeper) GetLeasesByProviderAndState(ctx context.Context, providerUUID string, state types.LeaseState) ([]types.Lease, error) {
	var leases []types.Lease

	// Use the compound index for direct lookup
	key := collections.Join(providerUUID, int32(state))
	iter, err := k.Leases.Indexes.ProviderState.MatchExact(ctx, key)
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

// GetLeasesByTenantAndState returns leases for a tenant with a specific state.
// Uses the compound (tenant, state) index for O(1) direct lookup.
func (k *Keeper) GetLeasesByTenantAndState(ctx context.Context, tenant string, state types.LeaseState) ([]types.Lease, error) {
	var leases []types.Lease

	tenantAddr, err := sdk.AccAddressFromBech32(tenant)
	if err != nil {
		return nil, err
	}

	// Use the compound index for direct lookup
	key := collections.Join(tenantAddr, int32(state))
	iter, err := k.Leases.Indexes.TenantState.MatchExact(ctx, key)
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

// GetLeasesBySKU returns leases that contain the specified SKU.
// Uses the LeaseBySKUIndex for efficient O(k) lookup where k = leases containing the SKU.
func (k *Keeper) GetLeasesBySKU(ctx context.Context, skuUUID string) ([]types.Lease, error) {
	var leases []types.Lease

	// Create a range that matches all (skuUUID, *) keys
	rng := collections.NewPrefixedPairRange[string, string](skuUUID)
	iter, err := k.LeaseBySKUIndex.Iterate(ctx, rng)
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			return nil, err
		}
		leaseUUID := key.K2()
		lease, err := k.Leases.Get(ctx, leaseUUID)
		if err != nil {
			return nil, err
		}
		leases = append(leases, lease)
	}

	return leases, nil
}

// ExpirePendingLease expires a pending lease, unlocking the tenant's credit.
// This is called by the EndBlocker when a lease exceeds the pending timeout.
// Uses CacheContext for atomicity - if any state update fails, no changes are committed.
func (k *Keeper) ExpirePendingLease(ctx context.Context, lease *types.Lease) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	// Validate lease state
	if lease.State != types.LEASE_STATE_PENDING {
		return types.ErrLeaseNotPending.Wrapf("lease %s is not pending", lease.Uuid)
	}

	// Get params for reservation calculation
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}

	// Use CacheContext for atomic state changes
	cacheCtx, write := sdkCtx.CacheContext()

	// Update lease state to EXPIRED
	lease.State = types.LEASE_STATE_EXPIRED
	lease.ExpiredAt = &blockTime

	if err := k.SetLease(cacheCtx, *lease); err != nil {
		return err
	}

	// Decrement pending lease count and release reservation in credit account
	creditAccount, err := k.GetCreditAccount(cacheCtx, lease.Tenant)
	if err != nil {
		// A pending lease should always have a credit account. If it's missing,
		// do not commit the lease state change to avoid inconsistent state.
		k.logger.Error("credit account not found when expiring lease, skipping expiration",
			"tenant", lease.Tenant,
			"lease_uuid", lease.Uuid,
			"error", err,
		)
		return err
	}

	k.DecrementPendingLeaseCount(&creditAccount, lease.Uuid)

	// Release reservation for this lease (PENDING leases have reservations)
	k.ReleaseLeaseReservation(&creditAccount, lease, params.MinLeaseDuration)

	if err := k.SetCreditAccount(cacheCtx, creditAccount); err != nil {
		return err
	}

	// Commit all state changes atomically
	write()

	// Emit event (after commit, events are not part of CacheContext)
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
// It uses an iterator to process leases one-by-one without loading all into memory,
// preventing DoS attacks from large numbers of pending leases.
// Rate limited to MaxPendingLeaseExpirationsPerBlock expirations per block.
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

	// Collect pending lease UUIDs that need expiration first, then process them.
	// This two-pass approach avoids iterator invalidation: when ExpirePendingLease
	// changes a lease's state from PENDING to EXPIRED, the State index is modified.
	// Modifying an index while iterating over it can cause undefined behavior.
	//
	// NOTE: The StateCreatedAt compound index exists for potential time-bounded
	// range queries, but the Collections PairRange API doesn't support partial-prefix
	// range queries on compound reference keys (Pair[int32, time.Time]). The State
	// index with per-lease time filtering is sufficient given the rate limit.
	iter, err := k.Leases.Indexes.State.MatchExact(ctx, int32(types.LEASE_STATE_PENDING))
	if err != nil {
		return err
	}

	// First pass: collect UUIDs of leases that have exceeded pending timeout
	var expiredUUIDs []string
	for ; iter.Valid(); iter.Next() {
		// Rate limit: stop collecting after max expirations to process
		if len(expiredUUIDs) >= types.MaxPendingLeaseExpirationsPerBlock {
			break
		}

		leaseUUID, err := iter.PrimaryKey()
		if err != nil {
			k.logger.Error("failed to get lease UUID from iterator",
				"error", err,
			)
			continue
		}

		lease, err := k.Leases.Get(ctx, leaseUUID)
		if err != nil {
			k.logger.Error("failed to get lease from storage",
				"lease_uuid", leaseUUID,
				"error", err,
			)
			continue
		}

		// Check if lease has exceeded pending timeout
		expirationTime := lease.CreatedAt.Add(pendingTimeout)
		if blockTime.After(expirationTime) {
			expiredUUIDs = append(expiredUUIDs, leaseUUID)
		}
	}

	// Close iterator before modifying state to avoid iterator invalidation
	if err := iter.Close(); err != nil {
		k.logger.Error("failed to close iterator", "error", err)
	}

	// Second pass: expire the collected leases
	expiredCount := 0
	for _, leaseUUID := range expiredUUIDs {
		lease, err := k.Leases.Get(ctx, leaseUUID)
		if err != nil {
			k.logger.Error("failed to get lease for expiration",
				"lease_uuid", leaseUUID,
				"error", err,
			)
			continue
		}

		if err := k.ExpirePendingLease(ctx, &lease); err != nil {
			k.logger.Error("failed to expire pending lease",
				"lease_uuid", lease.Uuid,
				"error", err,
			)
			continue
		}
		expiredCount++
	}

	if expiredCount > 0 {
		k.logger.Info("expired pending leases in EndBlocker",
			"expired_count", expiredCount,
			"collected_count", len(expiredUUIDs),
		)
	}

	if len(expiredUUIDs) >= types.MaxPendingLeaseExpirationsPerBlock {
		k.logger.Warn("reached max pending lease expirations per block",
			"limit", types.MaxPendingLeaseExpirationsPerBlock,
		)
	}

	return nil
}
