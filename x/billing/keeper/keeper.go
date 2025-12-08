package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"cosmossdk.io/collections/indexes"
	storetypes "cosmossdk.io/core/store"
	"cosmossdk.io/log"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

// LeaseIndexes defines the indexes for the Lease collection.
type LeaseIndexes struct {
	// Tenant is a multi-index that indexes Leases by tenant address.
	Tenant *indexes.Multi[string, uint64, types.Lease]
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
			collections.StringKey,
			collections.Uint64Key,
			func(_ uint64, lease types.Lease) (string, error) {
				return lease.Tenant, nil
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

// BankKeeper defines the expected bank keeper interface.
type BankKeeper interface {
	SendCoins(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) error
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
}

// AccountKeeper defines the expected account keeper interface.
type AccountKeeper interface {
	GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI
	NewAccountWithAddress(ctx context.Context, addr sdk.AccAddress) sdk.AccountI
	SetAccount(ctx context.Context, acc sdk.AccountI)
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
	CreditAccounts collections.Map[string, types.CreditAccount] // keyed by tenant address

	authority string

	// keepers (to be set via setters for now, full DI later)
	skuKeeper     SKUKeeper
	bankKeeper    BankKeeper
	accountKeeper AccountKeeper
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
			collections.StringKey,
			codec.CollValue[types.CreditAccount](cdc),
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
func (k *Keeper) SetBankKeeper(bk BankKeeper) {
	k.bankKeeper = bk
}

// SetAccountKeeper sets the account keeper.
func (k *Keeper) SetAccountKeeper(ak AccountKeeper) {
	k.accountKeeper = ak
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
		if err := k.CreditAccounts.Set(ctx, ca.Tenant, ca); err != nil {
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
	err = k.CreditAccounts.Walk(ctx, nil, func(_ string, ca types.CreditAccount) (bool, error) {
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

	iter, err := k.Leases.Indexes.Tenant.MatchExact(ctx, tenant)
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
	ca, err := k.CreditAccounts.Get(ctx, tenant)
	if err != nil {
		return types.CreditAccount{}, types.ErrCreditAccountNotFound
	}
	return ca, nil
}

// SetCreditAccount sets a CreditAccount in the store.
func (k *Keeper) SetCreditAccount(ctx context.Context, ca types.CreditAccount) error {
	return k.CreditAccounts.Set(ctx, ca.Tenant, ca)
}

// GetAllCreditAccounts returns all CreditAccounts in the store.
func (k *Keeper) GetAllCreditAccounts(ctx context.Context) ([]types.CreditAccount, error) {
	var accounts []types.CreditAccount

	err := k.CreditAccounts.Walk(ctx, nil, func(_ string, ca types.CreditAccount) (bool, error) {
		accounts = append(accounts, ca)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return accounts, nil
}

// CountActiveLeasesByTenant counts the number of active leases for a tenant.
func (k *Keeper) CountActiveLeasesByTenant(ctx context.Context, tenant string) (uint64, error) {
	var count uint64

	iter, err := k.Leases.Indexes.Tenant.MatchExact(ctx, tenant)
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
