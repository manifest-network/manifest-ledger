package keeper_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"

	sdkcodec "github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestMigrate2to3Then3to4ComposesFromV2State(t *testing.T) {
	f := initFixture(t)
	tenant := f.TestAccs[0]
	upperTenant := strings.ToUpper(tenant.String())
	creditAddress := types.DeriveCreditAddress(tenant)
	now := f.Ctx.BlockTime()
	pendingUUID := "01912345-6789-7abc-8def-0123456789a0"
	items := []types.LeaseItem{{
		SkuUuid:     testSKUUUID,
		Quantity:    1,
		LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(2)),
	}}
	leases := []types.Lease{
		{
			Uuid: testLeaseUUID2, Tenant: upperTenant, ProviderUuid: testProviderUUID,
			Items: items, State: types.LEASE_STATE_ACTIVE, CreatedAt: now, LastSettledAt: now,
			MinLeaseDurationAtCreation: 1,
		},
		{
			Uuid: testLeaseUUID1, Tenant: tenant.String(), ProviderUuid: testProviderUUID,
			Items: items, State: types.LEASE_STATE_ACTIVE, CreatedAt: now, LastSettledAt: now,
			MinLeaseDurationAtCreation: 1,
		},
		{
			Uuid: pendingUUID, Tenant: upperTenant, ProviderUuid: testProviderUUID,
			Items: items, State: types.LEASE_STATE_PENDING, CreatedAt: now, LastSettledAt: now,
			MinLeaseDurationAtCreation: 1,
		},
	}
	for _, lease := range leases {
		require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, lease))
	}
	f.fundAccount(t, creditAddress, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(5))))
	account := types.CreditAccount{
		Tenant:            upperTenant,
		CreditAddress:     strings.ToUpper(creditAddress.String()),
		ActiveLeaseCount:  1, // v2 alias-corrupted cache, repaired by Migrate2to3.
		PendingLeaseCount: 0,
		ReservedAmounts:   sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(6))),
	}
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, account))

	// Replace primary rows with v2 CollValue encodings while retaining the
	// byte-addressed keys and secondary indexes used by both migrations.
	for _, lease := range leases {
		overwriteLeaseWithLegacyEncoding(t, f, lease)
	}
	legacyAccount, err := sdkcodec.CollValue[types.CreditAccount](f.EncodingCfg.Codec).Encode(account)
	require.NoError(t, err)
	accountKey, err := collections.EncodeKeyWithPrefix(
		types.CreditAccountKey.Bytes(),
		sdk.AccAddressKey,
		tenant,
	)
	require.NoError(t, err)
	f.Ctx.KVStore(f.App.GetKey(types.StoreKey)).Set(accountKey, legacyAccount)

	migrator := keeper.NewMigrator(f.App.BillingKeeper)
	require.NoError(t, migrator.Migrate2to3(f.Ctx))
	accountAfterV3, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(2), accountAfterV3.ActiveLeaseCount)
	require.Equal(t, uint64(1), accountAfterV3.PendingLeaseCount)
	require.Equal(t, sdkmath.NewInt(6), accountAfterV3.ReservedAmounts.AmountOf(testDenom))
	assertRawAddressStorage(t,
		f.Ctx.KVStore(f.App.GetKey(types.StoreKey)).Get(accountKey),
		testCreditAccountStoragePrefix,
		tenant,
	)
	for _, lease := range leases {
		leaseKey, err := collections.EncodeKeyWithPrefix(
			types.LeaseKey.Bytes(),
			collections.StringKey,
			lease.Uuid,
		)
		require.NoError(t, err)
		assertRawAddressStorage(t,
			f.Ctx.KVStore(f.App.GetKey(types.StoreKey)).Get(leaseKey),
			testLeaseStoragePrefix,
			tenant,
		)
		stored, err := f.App.BillingKeeper.GetLease(f.Ctx, lease.Uuid)
		require.NoError(t, err)
		require.Nil(t, stored.Reservation, "v2 to v3 must leave the v4 presence marker absent")
	}

	require.NoError(t, migrator.Migrate3to4(f.Ctx))
	pending, err := f.App.BillingKeeper.GetLease(f.Ctx, pendingUUID)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(2), pending.Reservation.RemainingAmounts.AmountOf(testDenom))
	activeEarlier, err := f.App.BillingKeeper.GetLease(f.Ctx, testLeaseUUID1)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(2), activeEarlier.Reservation.RemainingAmounts.AmountOf(testDenom))
	activeLater, err := f.App.BillingKeeper.GetLease(f.Ctx, testLeaseUUID2)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(1), activeLater.Reservation.RemainingAmounts.AmountOf(testDenom))
	accountAfterV4, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(5), accountAfterV4.ReservedAmounts.AmountOf(testDenom))
	require.True(t, accountAfterV4.UnattributedReservedAmounts.IsZero())
	require.Zero(t, accountAfterV4.UnattributedLeaseCount)
	require.Equal(t, sdkmath.NewInt(5),
		f.App.BankKeeper.GetBalance(f.Ctx, creditAddress, testDenom).Amount,
		"sequential migrations must not mint or transfer bank balances")
}

func TestMigrate3to4CountsLiveLegacyCohortWithZeroAllocation(t *testing.T) {
	f := initFixture(t)
	tenant := f.TestAccs[0]
	now := f.Ctx.BlockTime()
	legacy := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       tenant.String(),
		ProviderUuid: testProviderUUID,
		Items: []types.LeaseItem{{
			SkuUuid:     testSKUUUID,
			Quantity:    1,
			LockedPrice: sdk.NewInt64Coin(testDenom, 1),
		}},
		State:         types.LEASE_STATE_ACTIVE,
		CreatedAt:     now,
		LastSettledAt: now,
	}
	require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, legacy))
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:           tenant.String(),
		CreditAddress:    types.DeriveCreditAddress(tenant).String(),
		ActiveLeaseCount: 1,
	}))

	require.NoError(t, keeper.NewMigrator(f.App.BillingKeeper).Migrate3to4(f.Ctx))
	account, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.True(t, account.ReservedAmounts.IsZero())
	require.True(t, account.UnattributedReservedAmounts.IsZero())
	require.Equal(t, uint64(1), account.UnattributedLeaseCount)
	stored, err := f.App.BillingKeeper.GetLease(f.Ctx, legacy.Uuid)
	require.NoError(t, err)
	require.NotNil(t, stored.Reservation)
	require.True(t, stored.Reservation.RemainingAmounts.IsZero())
}

func TestMigrate3to4ProcessesAccountAndTerminalLeasePageBoundaries(t *testing.T) {
	f := initFixture(t)
	const entryCount = 1001 // storage migration pages contain 1000 entries.

	tenants := make([]sdk.AccAddress, 0, entryCount)
	leaseUUIDs := make([]string, 0, entryCount)
	for index := range entryCount {
		// Exactly 20 deterministic bytes, accepted by the SDK account-address
		// verifier and ordered by the zero-padded numeric suffix.
		tenant := sdk.AccAddress([]byte(fmt.Sprintf("tenant-%013d", index)))
		leaseUUID := fmt.Sprintf("reservation-page-%04d", index)
		tenants = append(tenants, tenant)
		leaseUUIDs = append(leaseUUIDs, leaseUUID)

		// A terminal row still needs the v4 presence wrapper initialized. Give
		// every corresponding v3 account a historical residual so crossing the
		// account page boundary is observable as well: with no live claim, v4
		// must release the residual from reservation accounting.
		require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, types.Lease{
			Uuid:         leaseUUID,
			Tenant:       tenant.String(),
			ProviderUuid: testProviderUUID,
			State:        types.LEASE_STATE_REJECTED,
		}))
		require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
			Tenant:          tenant.String(),
			CreditAddress:   types.DeriveCreditAddress(tenant).String(),
			ReservedAmounts: sdk.NewCoins(sdk.NewInt64Coin(testDenom, 1)),
		}))
	}

	representativeIndexes := [...]int{0, 999, 1000}
	for _, index := range representativeIndexes {
		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseUUIDs[index])
		require.NoError(t, err)
		require.Nil(t, lease.Reservation)
		account, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenants[index].String())
		require.NoError(t, err)
		require.Equal(t, sdkmath.OneInt(), account.ReservedAmounts.AmountOf(testDenom))
	}

	require.NoError(t, keeper.NewMigrator(f.App.BillingKeeper).Migrate3to4(f.Ctx))

	for _, index := range representativeIndexes {
		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseUUIDs[index])
		require.NoError(t, err)
		require.NotNil(t, lease.Reservation)
		require.True(t, lease.Reservation.RemainingAmounts.IsZero())
		account, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenants[index].String())
		require.NoError(t, err)
		require.True(t, account.ReservedAmounts.IsZero())
	}
}

func TestModuleManagerRunMigrationsAdvancesBillingFromV2ToV4(t *testing.T) {
	f := initFixture(t)
	tenant := f.TestAccs[0]
	upperTenant := strings.ToUpper(tenant.String())
	creditAddress := types.DeriveCreditAddress(tenant)
	now := f.Ctx.BlockTime()
	lease := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       upperTenant,
		ProviderUuid: testProviderUUID,
		Items: []types.LeaseItem{{
			SkuUuid:     testSKUUUID,
			Quantity:    1,
			LockedPrice: sdk.NewInt64Coin(testDenom, 2),
		}},
		State:                      types.LEASE_STATE_ACTIVE,
		CreatedAt:                  now,
		LastSettledAt:              now,
		MinLeaseDurationAtCreation: 1,
	}
	account := types.CreditAccount{
		Tenant:           upperTenant,
		CreditAddress:    strings.ToUpper(creditAddress.String()),
		ActiveLeaseCount: 0, // v2 cached-count drift repaired by Migrate2to3.
		ReservedAmounts:  sdk.NewCoins(sdk.NewInt64Coin(testDenom, 2)),
	}

	// Populate byte-addressed indexes, then replace public values with their v2
	// CollValue encodings. Running through ModuleManager must invoke both
	// registered steps and leave fully consumable v4 state.
	require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, lease))
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, account))
	require.NoError(t, f.App.BillingKeeper.LeaseSequence.Set(f.Ctx, 1))
	f.fundAccount(t, creditAddress, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 2)))
	overwriteLeaseWithLegacyEncoding(t, f, lease)
	legacyAccount, err := sdkcodec.CollValue[types.CreditAccount](f.EncodingCfg.Codec).Encode(account)
	require.NoError(t, err)
	accountKey, err := collections.EncodeKeyWithPrefix(
		types.CreditAccountKey.Bytes(),
		sdk.AccAddressKey,
		tenant,
	)
	require.NoError(t, err)
	store := f.Ctx.KVStore(f.App.GetKey(types.StoreKey))
	store.Set(accountKey, legacyAccount)
	params, err := f.App.BillingKeeper.GetParams(f.Ctx)
	require.NoError(t, err)
	legacyParams, err := sdkcodec.CollValue[types.Params](f.EncodingCfg.Codec).Encode(params)
	require.NoError(t, err)
	store.Set(types.ParamsKey.Bytes(), legacyParams)
	bankBefore := f.App.BankKeeper.GetAllBalances(f.Ctx, creditAddress)

	versionMap := f.App.ModuleManager.GetVersionMap()
	versionMap[types.ModuleName] = 2

	updatedVersionMap, err := f.App.ModuleManager.RunMigrations(
		f.Ctx,
		f.App.Configurator(),
		versionMap,
	)
	require.NoError(t, err)
	require.Equal(t, uint64(4), updatedVersionMap[types.ModuleName])

	migratedLease, err := f.App.BillingKeeper.GetLease(f.Ctx, lease.Uuid)
	require.NoError(t, err)
	require.Equal(t, tenant.String(), migratedLease.Tenant)
	require.NotNil(t, migratedLease.Reservation)
	require.Equal(t, sdkmath.NewInt(2), migratedLease.Reservation.RemainingAmounts.AmountOf(testDenom))
	migratedAccount, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, tenant.String(), migratedAccount.Tenant)
	require.Equal(t, creditAddress.String(), migratedAccount.CreditAddress)
	require.Equal(t, uint64(1), migratedAccount.ActiveLeaseCount)
	require.Equal(t, sdkmath.NewInt(2), migratedAccount.ReservedAmounts.AmountOf(testDenom))
	require.True(t, migratedAccount.UnattributedReservedAmounts.IsZero())
	require.Zero(t, migratedAccount.UnattributedLeaseCount)
	require.True(t, bankBefore.Equal(f.App.BankKeeper.GetAllBalances(f.Ctx, creditAddress)),
		"sequential module migrations must not mint or transfer bank balances")
	require.NoError(t, f.App.BillingKeeper.ExportGenesis(f.Ctx).Validate())
}

func TestMigrate3to4AllocatesBankBackedReservationsDeterministically(t *testing.T) {
	f := initFixture(t)
	tenant := f.TestAccs[0]
	creditAddress := types.DeriveCreditAddress(tenant)
	now := f.Ctx.BlockTime()

	pendingUUID := "01912345-6789-7abc-8def-0123456789a0"
	terminalUUID := "01912345-6789-7abc-8def-0123456789b0"
	legacyUUID := "01912345-6789-7abc-8def-0123456789b1"
	leasingItem := func(amount int64) []types.LeaseItem {
		return []types.LeaseItem{{
			SkuUuid:     testSKUUUID,
			Quantity:    1,
			LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(amount)),
		}}
	}
	leasing := func(uuid string, state types.LeaseState, amount int64, minimum uint64) types.Lease {
		return types.Lease{
			Uuid:                       uuid,
			Tenant:                     tenant.String(),
			ProviderUuid:               testProviderUUID,
			Items:                      leasingItem(amount),
			State:                      state,
			CreatedAt:                  now,
			LastSettledAt:              now,
			MinLeaseDurationAtCreation: minimum,
		}
	}

	// Insert the ACTIVE claims in reverse UUID order. The migration must still
	// use canonical UUID order for equal largest remainders.
	activeLater := leasing(testLeaseUUID2, types.LEASE_STATE_ACTIVE, 3, 1)
	activeEarlier := leasing(testLeaseUUID1, types.LEASE_STATE_ACTIVE, 3, 1)
	pending := leasing(pendingUUID, types.LEASE_STATE_PENDING, 2, 1)
	legacy := leasing(legacyUUID, types.LEASE_STATE_ACTIVE, 1, 0)
	terminal := leasing(terminalUUID, types.LEASE_STATE_REJECTED, 1, 1)
	for _, lease := range []types.Lease{activeLater, legacy, terminal, pending, activeEarlier} {
		require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, lease))
	}

	// Old aggregate 10 = exact PENDING 2 + two ACTIVE nominal claims of 3 +
	// opaque legacy excess 2. Only 7 is bank-backed. After preserving PENDING,
	// Hamilton allocation of 5 across claims 3:3:2 is 2:2:1.
	f.fundAccount(t, creditAddress, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(7))))
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:            tenant.String(),
		CreditAddress:     creditAddress.String(),
		ActiveLeaseCount:  3,
		PendingLeaseCount: 1,
		ReservedAmounts:   sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10))),
	}))

	bankBefore := f.App.BankKeeper.GetAllBalances(f.Ctx, creditAddress)
	require.NoError(t, keeper.NewMigrator(f.App.BillingKeeper).Migrate3to4(f.Ctx))
	require.True(t, bankBefore.Equal(f.App.BankKeeper.GetAllBalances(f.Ctx, creditAddress)))

	assertAllocation := func(uuid string, expected int64) types.Lease {
		t.Helper()
		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, uuid)
		require.NoError(t, err)
		require.NotNil(t, lease.Reservation)
		require.Equal(t, sdkmath.NewInt(expected), lease.Reservation.RemainingAmounts.AmountOf(testDenom))
		return lease
	}
	pendingAfter := assertAllocation(pendingUUID, 2)
	activeEarlierAfter := assertAllocation(testLeaseUUID1, 2)
	activeLaterAfter := assertAllocation(testLeaseUUID2, 2)
	legacyAfter := assertAllocation(legacyUUID, 0)
	terminalAfter := assertAllocation(terminalUUID, 0)

	accountAfter, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(7), accountAfter.ReservedAmounts.AmountOf(testDenom))
	require.Equal(t, sdkmath.NewInt(1), accountAfter.UnattributedReservedAmounts.AmountOf(testDenom))
	require.Equal(t, uint64(1), accountAfter.UnattributedLeaseCount)

	// Direct retries are idempotent as well as framework-versioned retries.
	require.NoError(t, keeper.NewMigrator(f.App.BillingKeeper).Migrate3to4(f.Ctx))
	for _, expected := range []types.Lease{
		pendingAfter,
		activeEarlierAfter,
		activeLaterAfter,
		legacyAfter,
		terminalAfter,
	} {
		actual, err := f.App.BillingKeeper.GetLease(f.Ctx, expected.Uuid)
		require.NoError(t, err)
		require.Equal(t, expected, actual)
	}
	accountAfterRetry, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, accountAfter, accountAfterRetry)
}

func TestMigrate3to4LargestRemainderPrefersActiveBeforeLegacyCohort(t *testing.T) {
	f := initFixture(t)
	tenant := f.TestAccs[0]
	creditAddress := types.DeriveCreditAddress(tenant)
	now := f.Ctx.BlockTime()
	leases := []types.Lease{
		{
			Uuid:         testLeaseUUID1,
			Tenant:       tenant.String(),
			ProviderUuid: testProviderUUID,
			Items: []types.LeaseItem{{
				SkuUuid: testSKUUUID, Quantity: 1,
				LockedPrice: sdk.NewCoin(testDenom, sdkmath.OneInt()),
			}},
			State: types.LEASE_STATE_ACTIVE, CreatedAt: now, LastSettledAt: now,
			MinLeaseDurationAtCreation: 1,
		},
		{
			Uuid:         testLeaseUUID2,
			Tenant:       tenant.String(),
			ProviderUuid: testProviderUUID,
			Items: []types.LeaseItem{{
				SkuUuid: testSKUUUID, Quantity: 1,
				LockedPrice: sdk.NewCoin(testDenom, sdkmath.OneInt()),
			}},
			State: types.LEASE_STATE_PENDING, CreatedAt: now, LastSettledAt: now,
			MinLeaseDurationAtCreation: 1,
		},
		{
			Uuid:         "01912345-6789-7abc-8def-0123456789b1",
			Tenant:       tenant.String(),
			ProviderUuid: testProviderUUID,
			Items: []types.LeaseItem{{
				SkuUuid: testSKUUUID, Quantity: 1,
				LockedPrice: sdk.NewCoin(testDenom, sdkmath.OneInt()),
			}},
			State: types.LEASE_STATE_ACTIVE, CreatedAt: now, LastSettledAt: now,
		},
	}
	for _, lease := range leases {
		require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, lease))
	}
	f.fundAccount(t, creditAddress, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(2))))
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:            tenant.String(),
		CreditAddress:     creditAddress.String(),
		ActiveLeaseCount:  2,
		PendingLeaseCount: 1,
		ReservedAmounts:   sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(3))),
	}))

	require.NoError(t, keeper.NewMigrator(f.App.BillingKeeper).Migrate3to4(f.Ctx))
	active, err := f.App.BillingKeeper.GetLease(f.Ctx, testLeaseUUID1)
	require.NoError(t, err)
	require.Equal(t, sdkmath.OneInt(), active.Reservation.RemainingAmounts.AmountOf(testDenom))
	account, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.True(t, account.UnattributedReservedAmounts.IsZero(),
		"the ACTIVE UUID must win an equal largest-remainder tie over the cohort")
}

func TestMigrate3to4RejectsUnfundedExactPendingReservation(t *testing.T) {
	tests := []struct {
		name           string
		oldReservation int64
		bankBalance    int64
		errorText      string
	}{
		{
			name:           "old aggregate is insufficient despite bank surplus",
			oldReservation: 4,
			bankBalance:    10,
			errorText:      "old aggregate cannot fully back exact PENDING reservations",
		},
		{
			name:           "bank balance is insufficient",
			oldReservation: 10,
			bankBalance:    4,
			errorText:      "bank balance cannot fully back exact PENDING reservations",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := initFixture(t)
			tenant := f.TestAccs[0]
			creditAddress := types.DeriveCreditAddress(tenant)
			now := f.Ctx.BlockTime()
			lease := types.Lease{
				Uuid:         testLeaseUUID1,
				Tenant:       tenant.String(),
				ProviderUuid: testProviderUUID,
				Items: []types.LeaseItem{{
					SkuUuid: testSKUUUID, Quantity: 1,
					LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(5)),
				}},
				State: types.LEASE_STATE_PENDING, CreatedAt: now, LastSettledAt: now,
				MinLeaseDurationAtCreation: 1,
			}
			require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, lease))
			f.fundAccount(t, creditAddress, sdk.NewCoins(
				sdk.NewCoin(testDenom, sdkmath.NewInt(test.bankBalance)),
			))
			account := types.CreditAccount{
				Tenant:            tenant.String(),
				CreditAddress:     creditAddress.String(),
				PendingLeaseCount: 1,
				ReservedAmounts: sdk.NewCoins(
					sdk.NewCoin(testDenom, sdkmath.NewInt(test.oldReservation)),
				),
			}
			require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, account))

			err := keeper.NewMigrator(f.App.BillingKeeper).Migrate3to4(f.Ctx)
			require.ErrorIs(t, err, types.ErrReservationInvariant)
			require.ErrorContains(t, err, test.errorText)
			storedLease, getErr := f.App.BillingKeeper.GetLease(f.Ctx, lease.Uuid)
			require.NoError(t, getErr)
			require.Nil(t, storedLease.Reservation)
			storedAccount, getErr := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
			require.NoError(t, getErr)
			require.Equal(t, account, storedAccount)
			require.Equal(t, sdkmath.NewInt(test.bankBalance),
				f.App.BankKeeper.GetBalance(f.Ctx, creditAddress, testDenom).Amount)
		})
	}
}

func TestMigrate3to4ReleasesExcessWithoutLiveLegacyCohort(t *testing.T) {
	f := initFixture(t)
	tenant := f.TestAccs[0]
	creditAddress := types.DeriveCreditAddress(tenant)
	now := f.Ctx.BlockTime()
	lease := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       tenant.String(),
		ProviderUuid: testProviderUUID,
		Items: []types.LeaseItem{{
			SkuUuid: testSKUUUID, Quantity: 1,
			LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(3)),
		}},
		State: types.LEASE_STATE_ACTIVE, CreatedAt: now, LastSettledAt: now,
		MinLeaseDurationAtCreation: 1,
	}
	require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, lease))
	f.fundAccount(t, creditAddress, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10))))
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:           tenant.String(),
		CreditAddress:    creditAddress.String(),
		ActiveLeaseCount: 1,
		ReservedAmounts:  sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10))),
	}))

	require.NoError(t, keeper.NewMigrator(f.App.BillingKeeper).Migrate3to4(f.Ctx))
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, lease.Uuid)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(3), lease.Reservation.RemainingAmounts.AmountOf(testDenom))
	account, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(3), account.ReservedAmounts.AmountOf(testDenom))
	require.True(t, account.UnattributedReservedAmounts.IsZero())
	require.Equal(t, sdkmath.NewInt(10),
		f.App.BankKeeper.GetBalance(f.Ctx, creditAddress, testDenom).Amount,
		"migration must only change accounting state, never bank balances")
}
