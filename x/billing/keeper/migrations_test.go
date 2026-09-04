package keeper_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdkcodec "github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

const (
	testParamsStoragePrefix        = "\x00billing/params/v1"
	testLeaseStoragePrefix         = "\x00billing/lease/v1"
	testCreditAccountStoragePrefix = "\x00billing/credit-account/v1"
)

func migrationAllowedList(size int) []string {
	addresses := make([]string, size)
	for i := range addresses {
		addresses[i] = sdk.AccAddress(bytes.Repeat([]byte{byte(i + 1)}, 20)).String()
	}
	return addresses
}

func TestMigratorMigrate2to3RejectsOversizedAllowedListBeforeWrite(t *testing.T) {
	f := initFixture(t)
	params := types.DefaultParams()
	params.AllowedList = migrationAllowedList(types.MaxAllowedListEntries + 1)
	require.NoError(t, f.App.BillingKeeper.SetParams(f.Ctx, params))

	store := f.Ctx.KVStore(f.App.GetKey(types.StoreKey))
	before := bytes.Clone(store.Get(types.ParamsKey.Bytes()))
	err := keeper.NewMigrator(f.App.BillingKeeper).Migrate2to3(f.Ctx)
	require.ErrorIs(t, err, types.ErrInvalidParams)
	require.Contains(t, err.Error(), "allowed list has 101 entries, maximum allowed is 100")
	require.Equal(t, before, store.Get(types.ParamsKey.Bytes()))
}

func TestMigratorMigrate2to3RewritesLegacyAddressStrings(t *testing.T) {
	f := initFixture(t)
	tenant := f.TestAccs[0]
	allowed := f.TestAccs[1]
	credit := types.DeriveCreditAddress(tenant)
	upperTenant := strings.ToUpper(tenant.String())
	upperAllowed := strings.ToUpper(allowed.String())
	upperCredit := strings.ToUpper(credit.String())

	params := types.DefaultParams()
	params.AllowedList = []string{allowed.String(), upperAllowed}
	lease := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       upperTenant,
		ProviderUuid: testProviderUUID,
		Items: []types.LeaseItem{{
			SkuUuid:     testSKUUUID,
			Quantity:    1,
			LockedPrice: sdk.NewCoin(testDenom, math.NewInt(2)),
		}},
		State:                      types.LEASE_STATE_ACTIVE,
		CreatedAt:                  f.Ctx.BlockTime(),
		LastSettledAt:              f.Ctx.BlockTime(),
		MinLeaseDurationAtCreation: types.DefaultMinLeaseDuration,
	}
	knownReservationFloor := calculateLeaseReservation(t, lease.Items, lease.MinLeaseDurationAtCreation)
	doubledReservation := addTestCoins(t, knownReservationFloor, knownReservationFloor)
	account := types.CreditAccount{
		Tenant:           upperTenant,
		CreditAddress:    upperCredit,
		ActiveLeaseCount: 1,
		ReservedAmounts:  doubledReservation,
	}

	// Seed the indexes through the current keeper, then replace only the three
	// primary values with their exact legacy CollValue encodings.
	require.NoError(t, f.App.BillingKeeper.SetParams(f.Ctx, params))
	require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, lease))
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, account))

	legacyParams, err := sdkcodec.CollValue[types.Params](f.EncodingCfg.Codec).Encode(params)
	require.NoError(t, err)
	legacyLease, err := sdkcodec.CollValue[types.Lease](f.EncodingCfg.Codec).Encode(lease)
	require.NoError(t, err)
	legacyAccount, err := sdkcodec.CollValue[types.CreditAccount](f.EncodingCfg.Codec).Encode(account)
	require.NoError(t, err)

	store := f.Ctx.KVStore(f.App.GetKey(types.StoreKey))
	leaseKey, err := collections.EncodeKeyWithPrefix(types.LeaseKey.Bytes(), collections.StringKey, lease.Uuid)
	require.NoError(t, err)
	accountKey, err := collections.EncodeKeyWithPrefix(types.CreditAccountKey.Bytes(), sdk.AccAddressKey, tenant)
	require.NoError(t, err)
	store.Set(types.ParamsKey.Bytes(), legacyParams)
	store.Set(leaseKey, legacyLease)
	store.Set(accountKey, legacyAccount)
	require.True(t, bytes.Contains(store.Get(types.ParamsKey.Bytes()), []byte(upperAllowed)))
	require.True(t, bytes.Contains(store.Get(leaseKey), []byte(upperTenant)))
	require.True(t, bytes.Contains(store.Get(accountKey), []byte(upperCredit)))

	indexedBefore, err := f.App.BillingKeeper.GetLeasesByTenantAndState(f.Ctx, tenant.String(), types.LEASE_STATE_ACTIVE)
	require.NoError(t, err)
	require.Len(t, indexedBefore, 1)

	require.NoError(t, keeper.NewMigrator(f.App.BillingKeeper).Migrate2to3(f.Ctx))

	rawParams := store.Get(types.ParamsKey.Bytes())
	rawLease := store.Get(leaseKey)
	rawAccount := store.Get(accountKey)
	assertRawAddressStorage(t, rawParams, testParamsStoragePrefix, allowed)
	assertRawAddressStorage(t, rawLease, testLeaseStoragePrefix, tenant)
	assertRawAddressStorage(t, rawAccount, testCreditAccountStoragePrefix, tenant)
	require.True(t, bytes.Contains(rawAccount, credit.Bytes()))
	require.False(t, bytes.Contains(rawAccount, []byte(credit.String())))
	require.False(t, bytes.Contains(rawAccount, []byte(upperCredit)))

	gotParams, err := f.App.BillingKeeper.GetParams(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, []string{allowed.String()}, gotParams.AllowedList)
	gotLease, err := f.App.BillingKeeper.GetLease(f.Ctx, lease.Uuid)
	require.NoError(t, err)
	require.Equal(t, tenant.String(), gotLease.Tenant)
	gotAccount, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, tenant.String(), gotAccount.Tenant)
	require.Equal(t, credit.String(), gotAccount.CreditAddress)
	require.True(t, knownReservationFloor.Equal(gotAccount.ReservedAmounts),
		"fully verifiable reservations must be reconciled exactly",
	)

	indexedAfter, err := f.App.BillingKeeper.GetLeasesByTenantAndState(f.Ctx, tenant.String(), types.LEASE_STATE_ACTIVE)
	require.NoError(t, err)
	require.Len(t, indexedAfter, 1)
	require.Equal(t, lease.Uuid, indexedAfter[0].Uuid)

	// Re-running the migration is byte-for-byte stable.
	paramsSnapshot := bytes.Clone(rawParams)
	leaseSnapshot := bytes.Clone(rawLease)
	accountSnapshot := bytes.Clone(rawAccount)
	require.NoError(t, keeper.NewMigrator(f.App.BillingKeeper).Migrate2to3(f.Ctx))
	require.Equal(t, paramsSnapshot, store.Get(types.ParamsKey.Bytes()))
	require.Equal(t, leaseSnapshot, store.Get(leaseKey))
	require.Equal(t, accountSnapshot, store.Get(accountKey))
}

func TestMigratorMigrate2to3RepairsReservationFloorAndLeaseCounts(t *testing.T) {
	tests := []struct {
		name        string
		legacyState types.LeaseState
	}{
		{name: "live legacy lease", legacyState: types.LEASE_STATE_PENDING},
		{name: "terminal legacy lease", legacyState: types.LEASE_STATE_CLOSED},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			tenant := f.TestAccs[0]
			upperTenant := strings.ToUpper(tenant.String())
			creditAddress := types.DeriveCreditAddress(tenant)
			f.fundAccount(t, creditAddress, sdk.NewCoins(
				sdk.NewCoin(testDenom, math.NewInt(1_000_000)),
			))
			createdAt := f.Ctx.BlockTime()
			items := []types.LeaseItem{{
				SkuUuid:     testSKUUUID,
				Quantity:    1,
				LockedPrice: sdk.NewCoin(testDenom, math.OneInt()),
			}}
			legacyItems := []types.LeaseItem{{
				SkuUuid:     "01912345-6789-7abc-8def-0123456789af",
				Quantity:    1,
				LockedPrice: sdk.NewCoin("uother", math.OneInt()),
			}}
			modernLease := types.Lease{
				Uuid:                       testLeaseUUID1,
				Tenant:                     tenant.String(),
				ProviderUuid:               testProviderUUID,
				Items:                      items,
				State:                      types.LEASE_STATE_ACTIVE,
				CreatedAt:                  createdAt,
				LastSettledAt:              createdAt,
				MinLeaseDurationAtCreation: types.DefaultMinLeaseDuration,
			}
			modernPendingLease := types.Lease{
				Uuid:                       "01912345-6789-7abc-8def-0123456789b0",
				Tenant:                     upperTenant,
				ProviderUuid:               testProviderUUID,
				Items:                      items,
				State:                      types.LEASE_STATE_PENDING,
				CreatedAt:                  createdAt,
				LastSettledAt:              createdAt,
				MinLeaseDurationAtCreation: types.DefaultMinLeaseDuration,
			}
			legacyLease := types.Lease{
				Uuid:                       testLeaseUUID2,
				Tenant:                     upperTenant,
				ProviderUuid:               testProviderUUID,
				Items:                      legacyItems,
				State:                      tc.legacyState,
				CreatedAt:                  createdAt,
				LastSettledAt:              createdAt,
				MinLeaseDurationAtCreation: 0,
			}
			if tc.legacyState == types.LEASE_STATE_CLOSED {
				legacyLease.ClosedAt = &createdAt
				legacyLease.ClosureReason = "tenant requested"
			}

			knownFloor := calculateLeaseReservation(t,
				modernLease.Items,
				modernLease.MinLeaseDurationAtCreation,
			)
			knownPendingFloor := calculateLeaseReservation(t,
				modernPendingLease.Items,
				modernPendingLease.MinLeaseDurationAtCreation,
			)
			knownFloor = addTestCoins(t, knownFloor, knownPendingFloor)
			legacyResidual := sdk.NewCoin("uother", math.NewInt(1800))
			underBacked := sdk.NewCoins(
				sdk.NewCoin(testDenom, math.NewInt(1800)),
				legacyResidual,
			)
			account := types.CreditAccount{
				Tenant:          upperTenant,
				CreditAddress:   strings.ToUpper(creditAddress.String()),
				ReservedAmounts: underBacked,
			}

			// Seed valid byte-addressed indexes, then replace the primary values
			// with exact v2 encodings to model the pre-upgrade store.
			require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, modernLease))
			require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, modernPendingLease))
			require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, legacyLease))
			require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, account))
			require.NoError(t, f.App.BillingKeeper.LeaseSequence.Set(f.Ctx, 3))

			store := f.Ctx.KVStore(f.App.GetKey(types.StoreKey))
			legacyLeaseCodec := sdkcodec.CollValue[types.Lease](f.EncodingCfg.Codec)
			for _, lease := range []types.Lease{modernLease, modernPendingLease, legacyLease} {
				encoded, err := legacyLeaseCodec.Encode(lease)
				require.NoError(t, err)
				key, err := collections.EncodeKeyWithPrefix(
					types.LeaseKey.Bytes(),
					collections.StringKey,
					lease.Uuid,
				)
				require.NoError(t, err)
				store.Set(key, encoded)
			}
			legacyAccountCodec := sdkcodec.CollValue[types.CreditAccount](f.EncodingCfg.Codec)
			legacyAccount, err := legacyAccountCodec.Encode(account)
			require.NoError(t, err)
			accountKey, err := collections.EncodeKeyWithPrefix(
				types.CreditAccountKey.Bytes(),
				sdk.AccAddressKey,
				tenant,
			)
			require.NoError(t, err)
			store.Set(accountKey, legacyAccount)
			balanceBeforeMigration := f.App.BankKeeper.GetAllBalances(f.Ctx, creditAddress)

			preMigrationGenesis := f.App.BillingKeeper.ExportGenesis(f.Ctx)
			preparedGenesis, err := preMigrationGenesis.PrepareForImport()
			require.NoError(t, err)
			require.Len(t, preparedGenesis.CreditAccounts, 1)
			require.True(t, underBacked.Equal(preMigrationGenesis.CreditAccounts[0].ReservedAmounts),
				"import preparation must not mutate the exported source",
			)

			require.NoError(t, keeper.NewMigrator(f.App.BillingKeeper).Migrate2to3(f.Ctx))
			require.Equal(t, balanceBeforeMigration, f.App.BankKeeper.GetAllBalances(f.Ctx, creditAddress),
				"reservation repair must not move bank balances",
			)

			repairedAccount, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
			require.NoError(t, err)
			expectedAfterMigration := knownFloor
			if tc.legacyState == types.LEASE_STATE_PENDING {
				expectedAfterMigration = knownFloor.Add(legacyResidual)
			}
			require.True(t, expectedAfterMigration.Equal(repairedAccount.ReservedAmounts),
				"migration must preserve only unknown live-legacy excess",
			)
			require.True(t, preparedGenesis.CreditAccounts[0].ReservedAmounts.Equal(repairedAccount.ReservedAmounts),
				"direct import preparation and the live v2-to-v3 migration must apply the same repair policy",
			)
			require.Equal(t, uint64(1), repairedAccount.ActiveLeaseCount)
			expectedPendingCount := uint64(1)
			if tc.legacyState == types.LEASE_STATE_PENDING {
				expectedPendingCount++
			}
			require.Equal(t, expectedPendingCount, repairedAccount.PendingLeaseCount,
				"migration must reconstruct byte-identity lease counts rather than preserve alias-corrupted counters",
			)
			require.Equal(t, repairedAccount.ActiveLeaseCount, preparedGenesis.CreditAccounts[0].ActiveLeaseCount)
			require.Equal(t, repairedAccount.PendingLeaseCount, preparedGenesis.CreditAccounts[0].PendingLeaseCount)

			// Re-running the migration after the repair is byte-for-byte stable.
			repairedRawAccount := bytes.Clone(store.Get(accountKey))
			require.NoError(t, keeper.NewMigrator(f.App.BillingKeeper).Migrate2to3(f.Ctx))
			require.Equal(t, repairedRawAccount, store.Get(accountKey))

			if tc.legacyState == types.LEASE_STATE_PENDING {
				// The live network upgrades from v2 through both registered
				// migrations. Lifecycle code requires the reservation wrapper
				// initialized by v3→v4 before this legacy lease can expire.
				require.NoError(t, keeper.NewMigrator(f.App.BillingKeeper).Migrate3to4(f.Ctx))
				migratedLegacy, err := f.App.BillingKeeper.GetLease(f.Ctx, legacyLease.Uuid)
				require.NoError(t, err)
				require.NoError(t, f.App.BillingKeeper.ExpirePendingLease(f.Ctx, &migratedLegacy))

				repairedAccount, err = f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
				require.NoError(t, err)
				require.True(t, knownFloor.Equal(repairedAccount.ReservedAmounts),
					"the last legacy transition must reconcile to the exact modern floor",
				)
			}

			exported := f.App.BillingKeeper.ExportGenesis(f.Ctx)
			require.NoError(t, exported.Validate())
		})
	}
}

func TestMigratorMigrate2to3RepairsBech32AliasLeaseCount(t *testing.T) {
	f := initFixture(t)
	tenant := f.TestAccs[0]
	upperTenant := strings.ToUpper(tenant.String())
	createdAt := f.Ctx.BlockTime()
	items := []types.LeaseItem{{
		SkuUuid:     testSKUUUID,
		Quantity:    1,
		LockedPrice: sdk.NewCoin(testDenom, math.OneInt()),
	}}
	leases := []types.Lease{
		{
			Uuid:                       testLeaseUUID1,
			Tenant:                     tenant.String(),
			ProviderUuid:               testProviderUUID,
			Items:                      items,
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  createdAt,
			LastSettledAt:              createdAt,
			MinLeaseDurationAtCreation: types.DefaultMinLeaseDuration,
		},
		{
			Uuid:                       testLeaseUUID2,
			Tenant:                     upperTenant,
			ProviderUuid:               testProviderUUID,
			Items:                      items,
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  createdAt,
			LastSettledAt:              createdAt,
			MinLeaseDurationAtCreation: types.DefaultMinLeaseDuration,
		},
	}
	perLeaseReservation := calculateLeaseReservation(t, items, types.DefaultMinLeaseDuration)
	doubledReservation := addTestCoins(t, perLeaseReservation, perLeaseReservation)
	account := types.CreditAccount{
		Tenant:           upperTenant,
		CreditAddress:    strings.ToUpper(types.DeriveCreditAddress(tenant).String()),
		ActiveLeaseCount: 1,
		ReservedAmounts:  doubledReservation,
	}

	for _, lease := range leases {
		require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, lease))
	}
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, account))
	require.NoError(t, f.App.BillingKeeper.LeaseSequence.Set(f.Ctx, uint64(len(leases))))

	store := f.Ctx.KVStore(f.App.GetKey(types.StoreKey))
	legacyLeaseCodec := sdkcodec.CollValue[types.Lease](f.EncodingCfg.Codec)
	for _, lease := range leases {
		encoded, err := legacyLeaseCodec.Encode(lease)
		require.NoError(t, err)
		key, err := collections.EncodeKeyWithPrefix(types.LeaseKey.Bytes(), collections.StringKey, lease.Uuid)
		require.NoError(t, err)
		store.Set(key, encoded)
	}
	legacyAccount, err := sdkcodec.CollValue[types.CreditAccount](f.EncodingCfg.Codec).Encode(account)
	require.NoError(t, err)
	accountKey, err := collections.EncodeKeyWithPrefix(types.CreditAccountKey.Bytes(), sdk.AccAddressKey, tenant)
	require.NoError(t, err)
	store.Set(accountKey, legacyAccount)

	require.ErrorContains(t, f.App.BillingKeeper.ExportGenesis(f.Ctx).ValidateStrict(), "has 2 active leases")
	require.NoError(t, keeper.NewMigrator(f.App.BillingKeeper).Migrate2to3(f.Ctx))

	repaired, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(2), repaired.ActiveLeaseCount)
	require.Zero(t, repaired.PendingLeaseCount)
	require.True(t, account.ReservedAmounts.Equal(repaired.ReservedAmounts))
	require.NoError(t, f.App.BillingKeeper.ExportGenesis(f.Ctx).ValidateStrict())
}

func TestMigratorMigrate2to3ProcessesMultipleOrderedPages(t *testing.T) {
	f := initFixture(t)
	tenant := f.TestAccs[0]
	upperTenant := strings.ToUpper(tenant.String())
	store := f.Ctx.KVStore(f.App.GetKey(types.StoreKey))
	legacyCodec := sdkcodec.CollValue[types.Lease](f.EncodingCfg.Codec)

	const leaseCount = 1001
	keys := make([][]byte, 0, leaseCount)
	for i := 0; i < leaseCount; i++ {
		uuid := fmt.Sprintf("migration-lease-%04d", i)
		legacy, err := legacyCodec.Encode(types.Lease{Uuid: uuid, Tenant: upperTenant})
		require.NoError(t, err)
		key, err := collections.EncodeKeyWithPrefix(types.LeaseKey.Bytes(), collections.StringKey, uuid)
		require.NoError(t, err)
		store.Set(key, legacy)
		keys = append(keys, key)
	}

	require.NoError(t, keeper.NewMigrator(f.App.BillingKeeper).Migrate2to3(f.Ctx))
	for _, key := range keys {
		assertRawAddressStorage(t, store.Get(key), testLeaseStoragePrefix, tenant)
	}
}

func TestMigratorMigrate2to3RejectsMismatchedCreditAccountKey(t *testing.T) {
	f := initFixture(t)
	tenant := f.TestAccs[0]
	otherTenant := f.TestAccs[1]
	account := types.CreditAccount{
		Tenant:        otherTenant.String(),
		CreditAddress: types.DeriveCreditAddress(otherTenant).String(),
	}
	legacy, err := sdkcodec.CollValue[types.CreditAccount](f.EncodingCfg.Codec).Encode(account)
	require.NoError(t, err)
	key, err := collections.EncodeKeyWithPrefix(types.CreditAccountKey.Bytes(), sdk.AccAddressKey, tenant)
	require.NoError(t, err)
	f.Ctx.KVStore(f.App.GetKey(types.StoreKey)).Set(key, legacy)

	err = keeper.NewMigrator(f.App.BillingKeeper).Migrate2to3(f.Ctx)
	require.ErrorContains(t, err, "does not match its store key")
}

func TestMigratorMigrate2to3RejectsStaleLiveTenantStateEntry(t *testing.T) {
	f := initFixture(t)
	tenant := f.TestAccs[0]
	createdAt := f.Ctx.BlockTime()
	indexedLease := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       tenant.String(),
		ProviderUuid: testProviderUUID,
		Items: []types.LeaseItem{{
			SkuUuid:     testSKUUUID,
			Quantity:    1,
			LockedPrice: sdk.NewCoin(testDenom, math.OneInt()),
		}},
		State:                      types.LEASE_STATE_PENDING,
		CreatedAt:                  createdAt,
		LastSettledAt:              createdAt,
		MinLeaseDurationAtCreation: types.DefaultMinLeaseDuration,
	}
	reservation := calculateLeaseReservation(t,
		indexedLease.Items,
		indexedLease.MinLeaseDurationAtCreation,
	)
	account := types.CreditAccount{
		Tenant:            tenant.String(),
		CreditAddress:     types.DeriveCreditAddress(tenant).String(),
		PendingLeaseCount: 1,
		ReservedAmounts:   reservation,
	}

	// Seed a PENDING TenantState entry, then model a corrupt v2 primary value
	// whose state changed without the secondary index being updated.
	require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, indexedLease))
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, account))
	storedLease := indexedLease
	storedLease.State = types.LEASE_STATE_ACTIVE
	legacy, err := sdkcodec.CollValue[types.Lease](f.EncodingCfg.Codec).Encode(storedLease)
	require.NoError(t, err)
	key, err := collections.EncodeKeyWithPrefix(
		types.LeaseKey.Bytes(),
		collections.StringKey,
		storedLease.Uuid,
	)
	require.NoError(t, err)
	f.Ctx.KVStore(f.App.GetKey(types.StoreKey)).Set(key, legacy)
	stored, err := f.App.BillingKeeper.GetLease(f.Ctx, storedLease.Uuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, stored.State)
	indexedAsPending, err := f.App.BillingKeeper.GetLeasesByTenantAndState(
		f.Ctx,
		tenant.String(),
		types.LEASE_STATE_PENDING,
	)
	require.NoError(t, err)
	require.Len(t, indexedAsPending, 1)
	require.Equal(t, types.LEASE_STATE_ACTIVE, indexedAsPending[0].State)

	err = keeper.NewMigrator(f.App.BillingKeeper).Migrate2to3(f.Ctx)
	require.ErrorContains(t, err, "does not match its LEASE_STATE_PENDING tenant-state index entry")
}

func assertRawAddressStorage(t *testing.T, encoded []byte, prefix string, address sdk.AccAddress) {
	t.Helper()
	require.True(t, bytes.HasPrefix(encoded, []byte(prefix)))
	require.True(t, bytes.Contains(encoded, address.Bytes()))
	require.False(t, bytes.Contains(encoded, []byte(address.String())))
	require.False(t, bytes.Contains(encoded, []byte(strings.ToUpper(address.String()))))
}

func TestMigrate2to3_ArithmeticOverflowReturnsError(t *testing.T) {
	f := initFixture(t)
	tenant := f.TestAccs[0]
	now := f.Ctx.BlockTime()
	lease := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       tenant.String(),
		ProviderUuid: testProviderUUID,
		Items: []types.LeaseItem{{
			SkuUuid:     testSKUUUID,
			Quantity:    2,
			LockedPrice: sdk.NewCoin(testDenom, highBitBillingTestInt()),
		}},
		State:                      types.LEASE_STATE_ACTIVE,
		CreatedAt:                  now,
		LastSettledAt:              now,
		MinLeaseDurationAtCreation: 1,
	}
	require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, lease))
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:           tenant.String(),
		CreditAddress:    types.DeriveCreditAddress(tenant).String(),
		ActiveLeaseCount: 1,
	}))

	var err error
	require.NotPanics(t, func() {
		err = keeper.NewMigrator(f.App.BillingKeeper).Migrate2to3(f.Ctx)
	})
	require.ErrorIs(t, err, types.ErrArithmeticOverflow)
	codespace, code, _ := errorsmod.ABCIInfo(err, false)
	require.Equal(t, types.ModuleName, codespace)
	require.Equal(t, uint32(20), code)
}

func TestMigrate2to3_RejectsStoredQuantityAboveMaximum(t *testing.T) {
	f := initFixture(t)
	tenant := f.TestAccs[0]
	now := f.Ctx.BlockTime()
	lease := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       tenant.String(),
		ProviderUuid: testProviderUUID,
		Items: []types.LeaseItem{{
			SkuUuid:     testSKUUUID,
			Quantity:    types.MaxQuantityPerItem + 1,
			LockedPrice: sdk.NewCoin(testDenom, math.OneInt()),
		}},
		State:                      types.LEASE_STATE_ACTIVE,
		CreatedAt:                  now,
		LastSettledAt:              now,
		MinLeaseDurationAtCreation: 1,
	}
	require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, lease))
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:           tenant.String(),
		CreditAddress:    types.DeriveCreditAddress(tenant).String(),
		ActiveLeaseCount: 1,
	}))

	require.NotPanics(t, func() {
		err := keeper.NewMigrator(f.App.BillingKeeper).Migrate2to3(f.Ctx)
		require.ErrorIs(t, err, types.ErrInvalidQuantity)
	})
}
