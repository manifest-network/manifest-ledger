package keeper

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"cosmossdk.io/collections"
	collcodec "cosmossdk.io/collections/codec"
	"cosmossdk.io/collections/indexes"
	storetypes "cosmossdk.io/core/store"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/log"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

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
	cdc          codec.BinaryCodec
	storeService storetypes.KVStoreService
	logger       log.Logger

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
	// enforced inside reconcileCustomDomainIndex; SetLease itself is atomic
	// (wraps the lease + index updates in a CacheContext), so a uniqueness
	// conflict cannot leave a partially-applied lease record.
	CustomDomainIndex collections.Map[string, types.CustomDomainTarget]

	authority string

	// Module dependencies are constructor-injected so a Keeper cannot be used
	// in a partially initialized state.
	skuKeeper     SKUKeeper
	bankKeeper    types.BankKeeper
	accountKeeper types.AccountKeeper
}

// NewKeeper creates a new billing Keeper instance.
func NewKeeper(
	cdc codec.BinaryCodec,
	storeService storetypes.KVStoreService,
	logger log.Logger,
	authority string,
	skuKeeper SKUKeeper,
	bankKeeper types.BankKeeper,
	accountKeeper types.AccountKeeper,
) Keeper {
	logger = logger.With(log.ModuleKey, "x/"+types.ModuleName)

	sb := collections.NewSchemaBuilder(storeService)

	k := Keeper{
		cdc:           cdc,
		storeService:  storeService,
		logger:        logger,
		authority:     authority,
		skuKeeper:     skuKeeper,
		bankKeeper:    bankKeeper,
		accountKeeper: accountKeeper,

		Params: collections.NewItem(
			sb,
			types.ParamsKey,
			"params",
			newParamsValueCodec(cdc),
		),
		Leases: collections.NewIndexedMap(
			sb,
			types.LeaseKey,
			"leases",
			collections.StringKey,
			newLeaseValueCodec(cdc),
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
			newCreditAccountValueCodec(cdc),
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

// GetAccountKeeper returns the account keeper (for simulation).
func (k *Keeper) GetAccountKeeper() types.AccountKeeper {
	return k.accountKeeper
}

// GetBankKeeper returns the bank keeper (for simulation).
func (k *Keeper) GetBankKeeper() types.BankKeeper {
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

	// Prepare and validate structural and accounting invariants before any
	// writes. Import preparation canonicalizes historically reachable Bech32
	// aliases without replaying claim-time domain policy or unverifiable legacy
	// reservation calculations.
	importGenesis, err := gs.PrepareForImport()
	if err != nil {
		return err
	}
	preparedGenesis, err := k.prepareGenesisReservationState(sdkCtx, importGenesis)
	if err != nil {
		return err
	}

	// Validate timestamps against block time
	// This ensures LastSettledAt is not in the future (important for chain restarts)
	if err := preparedGenesis.ValidateWithBlockTime(blockTime); err != nil {
		return err
	}
	if err := k.validateGenesisSKUReferences(ctx, preparedGenesis.Leases); err != nil {
		return err
	}

	if err := k.Params.Set(ctx, preparedGenesis.Params); err != nil {
		return err
	}

	// Validate and set leases
	// NOTE: Reference validation above requires the SKU module to be initialized first.
	// Genesis order ensures: sku -> billing (see app/app.go)
	for _, lease := range preparedGenesis.Leases {
		// SetLease populates LeaseBySKUIndex and reconciles the custom_domain
		// reverse index from (state, custom_domain). Storage-level uniqueness
		// detects two genesis leases claiming the same domain via
		// ErrCustomDomainAlreadyClaimed.
		if err := k.SetLease(ctx, lease); err != nil {
			return err
		}
	}

	for _, ca := range preparedGenesis.CreditAccounts {
		// Use SetCreditAccount to also populate the reverse index
		if err := k.SetCreditAccount(ctx, ca); err != nil {
			return err
		}
	}

	// Restore UUID generation sequence so new leases don't collide
	// with previously generated UUIDs after a genesis export/import cycle.
	if preparedGenesis.LeaseSequence > 0 {
		if err := k.LeaseSequence.Set(ctx, preparedGenesis.LeaseSequence); err != nil {
			return err
		}
	}

	return nil
}

// validateGenesisSKUReferences completes the read-only import preflight before
// InitGenesis writes billing state. Genesis module ordering guarantees SKU has
// already initialized its provider and product collections.
func (k *Keeper) validateGenesisSKUReferences(ctx context.Context, leases []types.Lease) error {
	for _, lease := range leases {
		if _, err := k.skuKeeper.GetProvider(ctx, lease.ProviderUuid); err != nil {
			return fmt.Errorf("lease %s references non-existent provider %s: %w",
				lease.Uuid, lease.ProviderUuid, err)
		}

		for itemIndex, item := range lease.Items {
			sku, err := k.skuKeeper.GetSKU(ctx, item.SkuUuid)
			if err != nil {
				return fmt.Errorf("lease %s item %d references non-existent SKU %s: %w",
					lease.Uuid, itemIndex, item.SkuUuid, err)
			}
			if sku.ProviderUuid != lease.ProviderUuid {
				return fmt.Errorf("lease %s item %d SKU %s belongs to provider %s, not %s",
					lease.Uuid, itemIndex, item.SkuUuid, sku.ProviderUuid, lease.ProviderUuid)
			}
		}
	}
	return nil
}

// ExportGenesis exports the module's state to a genesis state.
func (k *Keeper) ExportGenesis(ctx context.Context) *types.GenesisState {
	genesis, err := k.exportGenesis(ctx)
	if err != nil {
		panic(err)
	}
	return genesis
}

// exportGenesis is the error-returning form used by diagnostic paths. The
// module ExportGenesis interface cannot return an error and therefore retains
// its conventional panic-on-failure wrapper above.
func (k *Keeper) exportGenesis(ctx context.Context) (*types.GenesisState, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("read billing params: %w", err)
	}

	var leases []types.Lease
	err = k.Leases.Walk(ctx, nil, func(_ string, lease types.Lease) (bool, error) {
		leases = append(leases, lease)
		return false, nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk leases: %w", err)
	}

	var creditAccounts []types.CreditAccount
	err = k.CreditAccounts.Walk(ctx, nil, func(_ sdk.AccAddress, ca types.CreditAccount) (bool, error) {
		creditAccounts = append(creditAccounts, ca)
		return false, nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk credit accounts: %w", err)
	}

	leaseSeq, err := k.LeaseSequence.Peek(ctx)
	if err != nil {
		return nil, fmt.Errorf("read lease sequence: %w", err)
	}

	return &types.GenesisState{
		Params:         params,
		Leases:         leases,
		CreditAccounts: creditAccounts,
		LeaseSequence:  leaseSeq,
	}, nil
}

// Lease operations

// GetLease returns a Lease by its UUID.
func (k *Keeper) GetLease(ctx context.Context, uuid string) (types.Lease, error) {
	lease, err := k.Leases.Get(ctx, uuid)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.Lease{}, types.ErrLeaseNotFound
		}
		return types.Lease{}, types.ErrInternalCorruption.Wrapf("read lease %s: %v", uuid, err)
	}
	return lease, nil
}

// SetLease sets a Lease in the store and reconciles all derived indexes.
// The SKU index is reconciled against the previous item set. The custom_domain reverse index is
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
//
// SetLease is atomic: the lease record, the SKU index, and the custom_domain
// reverse index updates are staged in a CacheContext and committed only if
// every step succeeds. A reconcile error (uniqueness conflict or KV failure)
// rolls back the partial writes, so callers do not need their own
// CacheContext to keep state consistent.
func (k *Keeper) SetLease(ctx context.Context, lease types.Lease) error {
	tenantAddr, err := sdk.AccAddressFromBech32(lease.Tenant)
	if err != nil {
		return err
	}
	lease.Tenant = tenantAddr.String()

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	cacheCtx, write := sdkCtx.CacheContext()

	prev, hadPrev, err := k.getPreviousLease(cacheCtx, lease.Uuid)
	if err != nil {
		return err
	}

	if err := k.Leases.Set(cacheCtx, lease.Uuid, lease); err != nil {
		return err
	}

	if err := k.reconcileLeaseBySKUIndex(cacheCtx, prev, hadPrev, lease); err != nil {
		return err
	}

	if err := k.reconcileCustomDomainIndex(cacheCtx, prev, hadPrev, lease); err != nil {
		return err
	}

	write()
	return nil
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

// reconcileLeaseBySKUIndex makes the many-to-many SKU index exactly match the
// lease's current item set. Lease items are immutable through the public API,
// but exact reconciliation keeps SetLease correct for migrations, imports, and
// future internal callers. Sorted unique slices drive every store operation;
// no map iteration is used in this consensus path.
func (k *Keeper) reconcileLeaseBySKUIndex(ctx context.Context, prev types.Lease, hadPrev bool, lease types.Lease) error {
	previousSKUs := make([]string, 0, len(prev.Items))
	if hadPrev {
		for _, item := range prev.Items {
			previousSKUs = append(previousSKUs, item.SkuUuid)
		}
	}
	currentSKUs := make([]string, 0, len(lease.Items))
	for _, item := range lease.Items {
		currentSKUs = append(currentSKUs, item.SkuUuid)
	}

	slices.Sort(previousSKUs)
	previousSKUs = slices.Compact(previousSKUs)
	slices.Sort(currentSKUs)
	currentSKUs = slices.Compact(currentSKUs)

	previousIndex, currentIndex := 0, 0
	for previousIndex < len(previousSKUs) || currentIndex < len(currentSKUs) {
		switch {
		case currentIndex == len(currentSKUs) ||
			(previousIndex < len(previousSKUs) && previousSKUs[previousIndex] < currentSKUs[currentIndex]):
			if err := k.LeaseBySKUIndex.Remove(ctx, collections.Join(previousSKUs[previousIndex], lease.Uuid)); err != nil {
				return err
			}
			previousIndex++
		case previousIndex == len(previousSKUs) || currentSKUs[currentIndex] < previousSKUs[previousIndex]:
			if err := k.LeaseBySKUIndex.Set(ctx, collections.Join(currentSKUs[currentIndex], lease.Uuid), true); err != nil {
				return err
			}
			currentIndex++
		default:
			if err := k.LeaseBySKUIndex.Set(ctx, collections.Join(currentSKUs[currentIndex], lease.Uuid), true); err != nil {
				return err
			}
			previousIndex++
			currentIndex++
		}
	}

	return nil
}

// reconcileCustomDomainIndex enforces the per-item (state, custom_domain) →
// index invariant after a SetLease write. It walks both the previous and
// current item slices, releases entries for items whose domain changed or
// whose lease moved to terminal, and installs entries for items in editable
// state that carry a non-empty custom_domain. Returns ErrCustomDomainAlreadyClaimed
// if installing an entry would overwrite a claim by a different (lease, item)
// — a defence-in-depth check above SetItemCustomDomain's pre-check.
//
// Items in lease.Items are immutable post-creation today, so the "item removed
// in update" branch is theoretical but cheap to support.
//
// Both loops below iterate service names in sorted order rather than ranging
// over the maps directly. SetLease runs in the consensus path, and the install
// loop can perform some store operations and then return an error partway
// through; Go randomises map iteration order per run, so ranging over the map
// would make the number of store ops executed before that return — and hence
// GasUsed, which CometBFT hashes into LastResultsHash — differ between
// validators. Same reason msg_server.go carries an explicit tenantOrder slice.
func (k *Keeper) reconcileCustomDomainIndex(ctx context.Context, prev types.Lease, hadPrev bool, lease types.Lease) error {
	editable := lease.State == types.LEASE_STATE_PENDING || lease.State == types.LEASE_STATE_ACTIVE

	// Build per-service lookup maps plus explicit service-order slices. The
	// maps are never iterated; the sorted slices drive every store operation.
	// Service_name is the lease's commit-time uniqueness key, so it suffices as
	// the diff key. Empty service_name is valid for 1-item legacy leases.
	prevByService := map[string]string{}
	prevServices := make([]string, 0, len(prev.Items))
	if hadPrev {
		for _, item := range prev.Items {
			if item.CustomDomain != "" {
				if _, exists := prevByService[item.ServiceName]; !exists {
					prevServices = append(prevServices, item.ServiceName)
				}
				prevByService[item.ServiceName] = item.CustomDomain
			}
		}
	}
	newByService := map[string]string{}
	newServices := make([]string, 0, len(lease.Items))
	for _, item := range lease.Items {
		if item.CustomDomain != "" {
			if _, exists := newByService[item.ServiceName]; !exists {
				newServices = append(newServices, item.ServiceName)
			}
			newByService[item.ServiceName] = item.CustomDomain
		}
	}
	slices.Sort(prevServices)
	slices.Sort(newServices)

	// Release any prev entry whose live domain disappeared, changed, or whose
	// lease moved to terminal state. The Remove is gated on an ownership check
	// against the current index target: terminal-state leases preserve their
	// LeaseItem.CustomDomain field for audit, so subsequent SetLease calls on
	// the same closed lease (e.g. post-close withdraw settlement, which updates
	// LastSettledAt) re-derive prevByService from the still-populated field
	// and would otherwise unconditionally Remove an entry that another lease
	// has since legitimately claimed. Removing only when the index still
	// targets (this lease, this service) closes that hole without sacrificing
	// the audit-trail field on the closed lease record.
	for _, s := range prevServices {
		prevDomain := prevByService[s]
		if editable && newByService[s] == prevDomain {
			continue
		}
		existing, err := k.CustomDomainIndex.Get(ctx, prevDomain)
		switch {
		case err == nil:
			if existing.LeaseUuid != lease.Uuid || existing.ServiceName != s {
				// Entry has been re-claimed by another (lease, service) since
				// our prev snapshot. Do not remove; it isn't ours.
				continue
			}
			if err := k.CustomDomainIndex.Remove(ctx, prevDomain); err != nil {
				return err
			}
		case errors.Is(err, collections.ErrNotFound):
			// Already gone (e.g., a previous reconcile already cleaned up).
			// Idempotent — nothing to do.
		default:
			return err
		}
	}

	if !editable {
		return nil
	}

	// Install / verify entries for current items. Storage-level uniqueness
	// rejects overwriting a different (lease, service) pair.
	for _, s := range newServices {
		newDomain := newByService[s]
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
	next, err := k.LeaseSequence.Peek(ctx)
	if err != nil {
		return 0, err
	}
	if next == math.MaxUint64 {
		return 0, types.ErrSequenceExhausted
	}
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
func (k *Keeper) GetLeasesByTenant(ctx context.Context, tenant string) (leases []types.Lease, err error) {
	// Convert bech32 address to AccAddress for index lookup
	tenantAddr, err := sdk.AccAddressFromBech32(tenant)
	if err != nil {
		return nil, err
	}

	iter, err := k.Leases.Indexes.Tenant.MatchExact(ctx, tenantAddr)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := iter.Close(); err == nil {
			err = closeErr
		}
	}()

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
func (k *Keeper) IterateLeasesByProvider(ctx context.Context, providerUUID string, cb func(lease types.Lease) (stop bool, err error)) (err error) {
	iter, err := k.Leases.Indexes.Provider.MatchExact(ctx, providerUUID)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := iter.Close(); err == nil {
			err = closeErr
		}
	}()

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
		return types.CreditAccount{}, types.ErrInternalCorruption.Wrapf("read credit account for tenant %s: %v", tenantAddr, err)
	}
	return ca, nil
}

// SetCreditAccount validates and canonicalizes a CreditAccount, then atomically
// updates the primary record and its byte-keyed reverse lookup index.
func (k *Keeper) SetCreditAccount(ctx context.Context, ca types.CreditAccount) error {
	tenantAddr, err := sdk.AccAddressFromBech32(ca.Tenant)
	if err != nil {
		return types.ErrInvalidCreditOperation.Wrapf("invalid tenant address: %v", err)
	}
	expectedCreditAddr := types.DeriveCreditAddress(tenantAddr)
	creditAddr, err := sdk.AccAddressFromBech32(ca.CreditAddress)
	if err != nil {
		return types.ErrInvalidCreditOperation.Wrapf("invalid credit address for tenant %s: %v", tenantAddr, err)
	}
	if !expectedCreditAddr.Equals(creditAddr) {
		return types.ErrInvalidCreditOperation.Wrapf(
			"credit address %s does not match derived address %s for tenant %s",
			creditAddr,
			expectedCreditAddr,
			tenantAddr,
		)
	}
	ca.Tenant = tenantAddr.String()
	ca.CreditAddress = expectedCreditAddr.String()

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	cacheCtx, write := sdkCtx.CacheContext()
	if err := k.CreditAccounts.Set(cacheCtx, tenantAddr, ca); err != nil {
		return err
	}
	if err := k.CreditAddressIndex.Set(cacheCtx, expectedCreditAddr, tenantAddr); err != nil {
		return err
	}

	write()
	return nil
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
func (k *Keeper) countLeasesByTenantAndStateScan(ctx context.Context, tenant string, state types.LeaseState) (count uint64, err error) {
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
	defer func() {
		if closeErr := iter.Close(); err == nil {
			err = closeErr
		}
	}()

	for ; iter.Valid(); iter.Next() {
		count++
	}

	return count, nil
}

// GetCreditBalance returns the credit balance for a specific denom from the bank module for a tenant.
func (k *Keeper) GetCreditBalance(ctx context.Context, tenant string, denom string) (sdk.Coin, error) {
	if err := sdk.ValidateDenom(denom); err != nil {
		return sdk.Coin{}, types.ErrInvalidCreditOperation.Wrapf("invalid credit balance denom %q: %s", denom, err)
	}
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
	orderedDenoms := slices.Clone(denoms)
	slices.Sort(orderedDenoms)
	coins := make(sdk.Coins, 0, len(orderedDenoms))
	for index, denom := range orderedDenoms {
		if index > 0 && denom == orderedDenoms[index-1] {
			continue
		}
		if err := sdk.ValidateDenom(denom); err != nil {
			return nil, types.ErrInvalidCreditOperation.Wrapf("invalid credit balance denom %q: %s", denom, err)
		}
		bal := k.bankKeeper.GetBalance(ctx, creditAddr, denom)
		if bal.IsPositive() {
			coins = append(coins, bal)
		}
	}
	return coins, nil
}

// CalculateWithdrawableForLease calculates the amounts that can be withdrawn from a lease.
// It considers the time since last settlement and the credit balance available.
// Returns a Coins collection (one entry per denom), balance-capping any denom
// whose complete accrued amount cannot be represented. Malformed stored lease
// pricing and balance lookup failures are returned as errors.
func (k *Keeper) CalculateWithdrawableForLease(ctx context.Context, lease types.Lease) (sdk.Coins, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	// Identify the settlement interval without time.Time.Sub, whose duration
	// result saturates for intervals beyond roughly 292 years.
	var settleTime time.Time
	if lease.State == types.LEASE_STATE_ACTIVE {
		settleTime = blockTime
	} else {
		// For inactive leases, calculate from last settled to closed
		if lease.ClosedAt != nil {
			settleTime = *lease.ClosedAt
		} else {
			return sdk.NewCoins(), nil
		}
	}

	if !settleTime.After(lease.LastSettledAt) {
		return sdk.NewCoins(), nil
	}
	durationSeconds, err := elapsedWholeSeconds(lease.LastSettledAt, settleTime)
	if err != nil {
		return nil, err
	}

	// Calculate total accrued with overflow handling
	items := LeaseItemsToWithPrice(lease.Items)
	accruedAmounts, err := calculateTotalAccruedForLeaseSeconds(items, durationSeconds)
	var accrualOverflow *AccrualOverflowError
	if err != nil {
		if !errors.As(err, &accrualOverflow) {
			return nil, errorsmod.Wrapf(err, "calculate withdrawable amount for lease %s", lease.Uuid)
		}
	}

	if accruedAmounts.IsZero() && accrualOverflow == nil {
		return sdk.NewCoins(), nil
	}

	creditAccount, err := k.GetCreditAccount(ctx, lease.Tenant)
	if err != nil {
		return nil, errorsmod.Wrapf(err, "get credit account for withdrawable lease %s", lease.Uuid)
	}
	creditBalances, reservedAmounts, leaseAllocation, err := k.reservationSpendInputs(ctx, &lease, &creditAccount)
	if err != nil {
		return nil, errorsmod.Wrapf(err, "get reservation spend inputs for lease %s", lease.Uuid)
	}
	spendPlan, err := types.PlanReservationSpend(
		creditBalances,
		reservedAmounts,
		leaseAllocation,
		sdk.NewCoins(),
	)
	if err != nil {
		return nil, errorsmod.Wrapf(err, "calculate spendable credit for lease %s", lease.Uuid)
	}

	// For each representable denom, return the minimum of accrued and the
	// lease's allocation plus genuinely unreserved credit.
	result := accruedAmounts.Min(spendPlan.Spendable)
	if accrualOverflow != nil {
		for _, denom := range accrualOverflow.Denoms {
			spendable := spendPlan.Spendable.AmountOf(denom)
			if !spendable.IsPositive() {
				continue
			}
			result, err = types.SafeAddCoins(result, sdk.Coins{{Denom: denom, Amount: spendable}})
			if err != nil {
				return nil, errorsmod.Wrapf(err, "clamp withdrawable overflow for denom %s", denom)
			}
		}
	}

	return result, nil
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
func (k *Keeper) ShouldAutoCloseLease(
	ctx context.Context,
	lease *types.Lease,
	creditAccount *types.CreditAccount,
) (shouldClose bool, closeTime time.Time, err error) {
	// Only check active leases
	if lease.State != types.LEASE_STATE_ACTIVE {
		return false, time.Time{}, nil
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	// Compare timestamps directly so a future value beyond time.Duration's range
	// cannot be mistaken for an old value after duration saturation.
	if blockTime.Before(lease.LastSettledAt) {
		// LastSettledAt is in the future - this indicates data corruption or clock issues.
		// Return an error rather than silently masking the issue, as this could leave
		// leases in an invalid state where credit is exhausted but the lease stays active.
		k.logger.Error("data inconsistency: LastSettledAt is in the future",
			"lease_uuid", lease.Uuid,
			"tenant", lease.Tenant,
			"last_settled_at", lease.LastSettledAt,
			"block_time", blockTime,
		)
		return false, time.Time{}, types.ErrInvalidLease.Wrapf(
			"lease %s has LastSettledAt (%s) in the future relative to block time (%s)",
			lease.Uuid, lease.LastSettledAt, blockTime,
		)
	}

	// Validate stored items before deriving denoms or calling the bank keeper.
	// In particular, bank GetBalance assumes the denom is SDK-valid and may
	// panic if corrupt imported state reaches it unchecked.
	items := LeaseItemsToWithPrice(lease.Items)
	if err := validateLeaseAccrualItems(items); err != nil {
		return false, time.Time{}, errorsmod.Wrapf(err, "validate accrual items for lease %s", lease.Uuid)
	}

	creditBalances, reservedAmounts, leaseAllocation, err := k.reservationSpendInputs(ctx, lease, creditAccount)
	if err != nil {
		return false, time.Time{}, err
	}
	spendPlan, err := types.PlanReservationSpend(
		creditBalances,
		reservedAmounts,
		leaseAllocation,
		sdk.NewCoins(),
	)
	if err != nil {
		return false, time.Time{}, errorsmod.Wrapf(err, "calculate spendable credit for lease %s", lease.Uuid)
	}

	// If duration is zero, no accrual - check if any balance is exhausted
	shouldClose = false
	if blockTime.After(lease.LastSettledAt) {
		durationSeconds, secondsErr := elapsedWholeSeconds(lease.LastSettledAt, blockTime)
		if secondsErr != nil {
			return false, time.Time{}, secondsErr
		}
		accruedAmounts, calcErr := calculateTotalAccruedForLeaseSeconds(items, durationSeconds)
		if calcErr != nil {
			var accrualOverflow *AccrualOverflowError
			if !errors.As(calcErr, &accrualOverflow) {
				return false, time.Time{}, errorsmod.Wrapf(calcErr, "calculate accrued amount for lease %s", lease.Uuid)
			}
			// Overflow in accrual calculation means the accrued amount is extremely large,
			// which certainly exceeds any credit balance. Defensively close the lease.
			k.logger.Error("accrual calculation overflow in auto-close check, closing lease defensively",
				"lease_uuid", lease.Uuid,
				"tenant", lease.Tenant,
				"duration_seconds", durationSeconds.String(),
				"denoms", accrualOverflow.Denoms,
				"error", calcErr,
			)
			shouldClose = true
		} else {
			// Check if any denom's accrued amount exhausts this lease's
			// allocation plus genuinely unreserved credit.
			for _, accrued := range accruedAmounts {
				spendable := spendPlan.Spendable.AmountOf(accrued.Denom)
				if accrued.Amount.GTE(spendable) {
					shouldClose = true
					break
				}
			}
		}
	} else {
		// Check if any required denom has no lease-spendable credit.
		for _, item := range lease.Items {
			spendable := spendPlan.Spendable.AmountOf(item.LockedPrice.Denom)
			if spendable.IsZero() {
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
func (k *Keeper) AutoCloseLease(
	ctx context.Context,
	lease *types.Lease,
	creditAccount *types.CreditAccount,
	closeTime time.Time,
) (*AutoCloseLeaseResult, error) {
	result, err := k.PerformSettlementSilent(ctx, lease, creditAccount, closeTime)
	if err != nil {
		return nil, err
	}

	lease.State = types.LEASE_STATE_CLOSED
	lease.ClosedAt = &closeTime
	lease.LastSettledAt = closeTime
	lease.ClosureReason = types.ClosureReasonCreditExhausted

	k.DecrementActiveLeaseCount(creditAccount, lease.Uuid)
	if err := k.ReleaseLeaseReservation(ctx, creditAccount, lease); err != nil {
		return nil, err
	}

	if err := k.SetLease(ctx, *lease); err != nil {
		return nil, err
	}
	if err := k.SetCreditAccount(ctx, *creditAccount); err != nil {
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

// ReleaseLeaseReservation releases the reservation for a lease from a credit
// account. Modern leases release their exact remaining tranche. Historical
// zero-duration leases share only the explicitly unattributed cohort, which is
// released when the persisted live-member count reaches zero.
func (k *Keeper) ReleaseLeaseReservation(_ context.Context, ca *types.CreditAccount, lease *types.Lease) error {
	if err := validateLeaseCreditAccountIdentity(lease, ca); err != nil {
		return err
	}
	if lease.Reservation == nil {
		return types.ErrReservationInvariant.Wrapf(
			"lease %s has no initialized reservation to release",
			lease.Uuid,
		)
	}

	remaining, err := types.SafeAddCoins(sdk.NewCoins(), lease.Reservation.RemainingAmounts)
	if err != nil {
		return types.ErrReservationInvariant.Wrapf(
			"lease %s has invalid remaining reservation: %s",
			lease.Uuid, err,
		)
	}
	if !ca.UnattributedReservedAmounts.IsZero() && ca.UnattributedLeaseCount == 0 {
		return types.ErrReservationInvariant.Wrapf(
			"credit account for lease %s has an unattributed reservation without live cohort members",
			lease.Uuid,
		)
	}

	if lease.MinLeaseDurationAtCreation == 0 {
		if !remaining.IsZero() {
			return types.ErrReservationInvariant.Wrapf(
				"legacy lease %s has an attributed reservation",
				lease.Uuid,
			)
		}
		if ca.UnattributedLeaseCount == 0 {
			return types.ErrReservationInvariant.Wrapf(
				"legacy lease %s is not represented in the unattributed lease count",
				lease.Uuid,
			)
		}
		if ca.UnattributedLeaseCount > 1 {
			ca.UnattributedLeaseCount--
			return nil
		}

		reservedAfter, err := types.SafeSubtractCoins(ca.ReservedAmounts, ca.UnattributedReservedAmounts)
		if err != nil {
			return types.ErrReservationInvariant.Wrapf(
				"release unattributed reservation for legacy lease %s: %s",
				lease.Uuid, err,
			)
		}
		ca.ReservedAmounts = reservedAfter
		ca.UnattributedReservedAmounts = sdk.NewCoins()
		ca.UnattributedLeaseCount = 0
		return nil
	}

	attributedReserved, err := types.SafeSubtractCoins(
		ca.ReservedAmounts,
		ca.UnattributedReservedAmounts,
	)
	if err != nil {
		return types.ErrReservationInvariant.Wrapf(
			"credit account for lease %s does not contain its unattributed reservation: %s",
			lease.Uuid, err,
		)
	}
	if _, err := types.SafeSubtractCoins(attributedReserved, remaining); err != nil {
		return types.ErrReservationInvariant.Wrapf(
			"credit account for lease %s does not contain its attributed reservation: %s",
			lease.Uuid, err,
		)
	}
	reservedAfter, err := types.SafeSubtractCoins(ca.ReservedAmounts, remaining)
	if err != nil {
		return types.ErrReservationInvariant.Wrapf(
			"release remaining reservation for lease %s: %s",
			lease.Uuid, err,
		)
	}
	ca.ReservedAmounts = reservedAfter
	lease.Reservation.RemainingAmounts = sdk.NewCoins()
	return nil
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
func (k *Keeper) GetLeasesByState(ctx context.Context, state types.LeaseState) (leases []types.Lease, err error) {
	iter, err := k.Leases.Indexes.State.MatchExact(ctx, int32(state))
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := iter.Close(); err == nil {
			err = closeErr
		}
	}()

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
func (k *Keeper) GetLeasesByProviderAndState(ctx context.Context, providerUUID string, state types.LeaseState) (leases []types.Lease, err error) {
	// Use the compound index for direct lookup
	key := collections.Join(providerUUID, int32(state))
	iter, err := k.Leases.Indexes.ProviderState.MatchExact(ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := iter.Close(); err == nil {
			err = closeErr
		}
	}()

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
func (k *Keeper) GetLeasesByTenantAndState(ctx context.Context, tenant string, state types.LeaseState) (leases []types.Lease, err error) {
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
	defer func() {
		if closeErr := iter.Close(); err == nil {
			err = closeErr
		}
	}()

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
func (k *Keeper) GetLeasesBySKU(ctx context.Context, skuUUID string) (leases []types.Lease, err error) {
	// Create a range that matches all (skuUUID, *) keys
	rng := collections.NewPrefixedPairRange[string, string](skuUUID)
	iter, err := k.LeaseBySKUIndex.Iterate(ctx, rng)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := iter.Close(); err == nil {
			err = closeErr
		}
	}()

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

	// Use CacheContext for atomic state changes
	cacheCtx, write := sdkCtx.CacheContext()

	// Update lease state to EXPIRED
	lease.State = types.LEASE_STATE_EXPIRED
	lease.ExpiredAt = &blockTime

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
	if err := k.ReleaseLeaseReservation(cacheCtx, &creditAccount, lease); err != nil {
		return err
	}
	if err := k.SetLease(cacheCtx, *lease); err != nil {
		return err
	}

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
// It uses an ordered, time-bounded index iterator and only buffers the bounded
// set of UUIDs selected for expiration, preventing unbounded memory use.
// Rate limited to MaxPendingLeaseExpirationsPerBlock expirations per block.
func (k *Keeper) EndBlocker(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}

	// Get pending timeout duration.
	pendingTimeout := params.PendingTimeoutDuration()

	// Collect pending lease UUIDs that need expiration first, then process them.
	// This two-pass approach avoids iterator invalidation: when ExpirePendingLease
	// changes a lease's state from PENDING to EXPIRED, the index is modified, and
	// mutating an index while iterating over it can cause undefined behavior.
	//
	// A pending lease is expired once blockTime is strictly after
	// created_at + pendingTimeout, i.e. created_at < blockTime - pendingTimeout.
	// Range-query the StateCreatedAt index — keyed ((state, created_at), uuid) — so
	// the first pass visits only leases that can actually expire, keeping the work
	// O(expired) instead of O(total pending). Without this bound a large backlog of
	// not-yet-expired pending leases is walked and proto-decoded every block even
	// though none of them can expire.
	//
	// Note: the Collections pair-range helper (NewPrefixedPairRange) cannot express
	// this — it fixes the entire reference key, so it can only pin an exact
	// created_at, never range over it. A manual collections.Range over the composite
	// key does work: sdk.TimeKey (SortableTimeFormat) encodes time in an
	// order-preserving, fixed-width form, so the byte range matches the created_at
	// range. The upper bound is exclusive at created_at == cutoff because expiry is
	// strict (created_at < cutoff); a lease created exactly at the cutoff is not yet
	// expired.
	expiredCutoff := blockTime.Add(-pendingTimeout)
	pendingState := int32(types.LEASE_STATE_PENDING)
	scanRange := new(collections.Range[collections.Pair[collections.Pair[int32, time.Time], string]]).
		StartInclusive(collections.Join(collections.Join(pendingState, time.Time{}), "")).
		EndExclusive(collections.Join(collections.Join(pendingState, expiredCutoff), ""))

	iter, err := k.Leases.Indexes.StateCreatedAt.Iterate(ctx, scanRange)
	if err != nil {
		return err
	}

	// First pass: collect UUIDs of the (already time-bounded) pending leases,
	// oldest first. In consistent state the range restricts iteration to expirable
	// PENDING leases. Primary UUID/state and timeout checks remain defense-in-depth:
	// stale index rows cannot consume the processing quota, and a not-yet-expired
	// lease cannot be expired even if the range bounds were ever mis-encoded.
	//
	// Corrupt rows are logged and skipped instead of halting consensus. The number
	// of rows visited is deliberately not capped separately from successful
	// expirations: without a repair or persisted cursor, a fixed visit cap would
	// let the same oldest stale rows permanently starve every valid row after them.
	// Normal writes maintain this index atomically; the registered invariant is the
	// operator-facing mechanism for detecting corruption that violates that bound.
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
			if errors.Is(err, collections.ErrNotFound) {
				k.logger.Error("pending expiration index references missing lease",
					"lease_uuid", leaseUUID,
				)
			} else {
				k.logger.Error("pending expiration index references unreadable lease",
					"lease_uuid", leaseUUID,
					"error", err,
				)
			}
			continue
		}
		if lease.Uuid != leaseUUID {
			k.logger.Error("pending expiration index references mismatched lease primary",
				"index_lease_uuid", leaseUUID,
				"stored_lease_uuid", lease.Uuid,
			)
			continue
		}
		if lease.State != types.LEASE_STATE_PENDING {
			// A stale index row must not consume the expiration quota and
			// indefinitely starve valid pending leases ordered after it.
			k.logger.Error("pending expiration index references non-pending lease",
				"lease_uuid", leaseUUID,
				"lease_state", lease.State.String(),
			)
			continue
		}

		// Check if lease has exceeded pending timeout (defense-in-depth; the range
		// bound already guarantees this).
		if params.PendingLeaseDeadlineExceeded(blockTime, lease.CreatedAt) {
			expiredUUIDs = append(expiredUUIDs, leaseUUID)
		}
	}

	// Close iterator before modifying state to avoid iterator invalidation
	if err := iter.Close(); err != nil {
		return fmt.Errorf("close pending-lease expiration iterator: %w", err)
	}

	// Second pass: expire the collected leases
	expiredCount := 0
	for _, leaseUUID := range expiredUUIDs {
		lease, err := k.Leases.Get(ctx, leaseUUID)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				k.logger.Error("collected pending lease is missing before expiration",
					"lease_uuid", leaseUUID,
				)
			} else {
				k.logger.Error("collected pending lease is unreadable before expiration",
					"lease_uuid", leaseUUID,
					"error", err,
				)
			}
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
