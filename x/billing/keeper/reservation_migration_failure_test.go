package keeper_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// A migration error must not be mistaken for a completed cutover. These cases
// start from old aggregate-only state and introduce storage inconsistencies
// that normal message handlers do not create. The caller owns the cache: a
// direct Migrate3to4 call can have written earlier accounts before failing.
func TestReservationMigrationRejectsCorruptionWithinCallerCache(t *testing.T) {
	for _, test := range []struct {
		name        string
		mutate      func(*testing.T, *testFixture, sdk.AccAddress, types.Lease, types.CreditAccount)
		err         string
		earlyReject bool
	}{
		{
			name: "mixed initialization",
			mutate: func(t *testing.T, f *testFixture, _ sdk.AccAddress, lease types.Lease, _ types.CreditAccount) {
				lease.Reservation = &types.LeaseReservation{}
				require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, lease))
			},
			err: "partially initialized lease state", earlyReject: true,
		},
		{
			name: "account tenant differs from byte key",
			mutate: func(t *testing.T, f *testFixture, tenant sdk.AccAddress, _ types.Lease, account types.CreditAccount) {
				account.Tenant = f.TestAccs[0].String()
				require.NoError(t, f.App.BillingKeeper.CreditAccounts.Set(f.Ctx, tenant, account))
			},
			err: "does not match its byte key",
		},
		{
			name: "credit address differs from derived identity",
			mutate: func(t *testing.T, f *testFixture, tenant sdk.AccAddress, _ types.Lease, account types.CreditAccount) {
				account.CreditAddress = f.TestAccs[0].String()
				require.NoError(t, f.App.BillingKeeper.CreditAccounts.Set(f.Ctx, tenant, account))
			},
			err: "does not match its derived byte identity",
		},
		{
			name: "preexisting v4 cohort amount",
			mutate: func(t *testing.T, f *testFixture, tenant sdk.AccAddress, _ types.Lease, account types.CreditAccount) {
				account.UnattributedReservedAmounts = sdk.NewCoins(sdk.NewInt64Coin(testDenom, 1))
				require.NoError(t, f.App.BillingKeeper.CreditAccounts.Set(f.Ctx, tenant, account))
			},
			err: "unattributed reservations before the v3 to v4 migration",
		},
		{
			name: "preexisting v4 cohort membership",
			mutate: func(t *testing.T, f *testFixture, tenant sdk.AccAddress, _ types.Lease, account types.CreditAccount) {
				account.UnattributedLeaseCount = 1
				require.NoError(t, f.App.BillingKeeper.CreditAccounts.Set(f.Ctx, tenant, account))
			},
			err: "unattributed lease count 1 before the v3 to v4 migration",
		},
		{
			name: "negative historical aggregate",
			mutate: func(t *testing.T, f *testFixture, tenant sdk.AccAddress, _ types.Lease, account types.CreditAccount) {
				account.ReservedAmounts = sdk.Coins{{Denom: testDenom, Amount: sdkmath.NewInt(-1)}}
				require.NoError(t, f.App.BillingKeeper.CreditAccounts.Set(f.Ctx, tenant, account))
			},
			err: "invalid pre-migration reservations",
		},
		{
			name: "live lease without account",
			mutate: func(t *testing.T, f *testFixture, tenant sdk.AccAddress, _ types.Lease, _ types.CreditAccount) {
				require.NoError(t, f.App.BillingKeeper.CreditAccounts.Remove(f.Ctx, tenant))
			},
			err: "was not reached through its tenant account and state index",
		},
		{
			name: "live lease missing tenant-state index",
			mutate: func(t *testing.T, f *testFixture, _ sdk.AccAddress, lease types.Lease, _ types.CreditAccount) {
				require.NoError(t, f.App.BillingKeeper.Leases.Indexes.TenantState.Unreference(f.Ctx, lease.Uuid, func() (types.Lease, error) {
					return lease, nil
				}))
			},
			err: "was not reached through its tenant account and state index",
		},
		{
			name: "stale tenant-state index",
			mutate: func(t *testing.T, f *testFixture, _ sdk.AccAddress, lease types.Lease, _ types.CreditAccount) {
				lease.State = types.LEASE_STATE_REJECTED
				writeReservationMigrationPrimary(t, f, lease)
			},
			err: "does not match its LEASE_STATE_ACTIVE tenant-state index entry",
		},
		{
			name: "dangling tenant-state index",
			mutate: func(t *testing.T, f *testFixture, _ sdk.AccAddress, lease types.Lease, _ types.CreditAccount) {
				key, err := collections.EncodeKeyWithPrefix(types.LeaseKey.Bytes(), collections.StringKey, lease.Uuid)
				require.NoError(t, err)
				f.Ctx.KVStore(f.App.GetKey(types.StoreKey)).Delete(key)
			},
			err: "read billing lease",
		},
		{
			name: "nominal reservation overflow",
			mutate: func(t *testing.T, f *testFixture, _ sdk.AccAddress, lease types.Lease, _ types.CreditAccount) {
				lease.Items[0].LockedPrice = sdk.NewCoin(testDenom, highBitBillingTestInt())
				writeReservationMigrationPrimary(t, f, lease)
			},
			err: "nominal reservation",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := initFixture(t)
			// Explicit byte order guarantees the healthy account is normalized
			// before the corrupt account, independently of random test addresses.
			firstTenant := sdk.AccAddress(bytes.Repeat([]byte{0x11}, 20))
			lastTenant := sdk.AccAddress(bytes.Repeat([]byte{0x22}, 20))
			firstLease, _ := seedReservationMigrationAccount(t, f, firstTenant, testLeaseUUID1)
			lastLease, lastAccount := seedReservationMigrationAccount(t, f, lastTenant, testLeaseUUID2)
			test.mutate(t, f, lastTenant, lastLease, lastAccount)
			storesBefore := snapshotPayoutStores(t, f, f.Ctx)
			f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())
			cacheCtx, _ := f.Ctx.CacheContext()

			err := keeper.NewMigrator(f.App.BillingKeeper).Migrate3to4(cacheCtx)
			require.ErrorContains(t, err, test.err)
			require.Equal(t, storesBefore, snapshotPayoutStores(t, f, f.Ctx), "discarding the failed migration cache preserves all source values, indexes, and balances")
			require.Empty(t, f.Ctx.EventManager().Events())
			firstInCache, err := f.App.BillingKeeper.GetLease(cacheCtx, firstLease.Uuid)
			require.NoError(t, err)
			if test.earlyReject {
				require.Nil(t, firstInCache.Reservation, "mixed formats must fail before any account normalization")
			} else {
				require.NotNil(t, firstInCache.Reservation, "the failure must occur after an earlier account was actually written in the discarded cache")
				require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 50)), firstInCache.Reservation.RemainingAmounts)
			}
		})
	}
}

func seedReservationMigrationAccount(t *testing.T, f *testFixture, tenant sdk.AccAddress, uuid string) (types.Lease, types.CreditAccount) {
	t.Helper()
	lease := types.Lease{
		Uuid: uuid, Tenant: tenant.String(), ProviderUuid: testProviderUUID,
		Items: []types.LeaseItem{{SkuUuid: testSKUUUID, Quantity: 1, LockedPrice: sdk.NewInt64Coin(testDenom, 1)}},
		State: types.LEASE_STATE_ACTIVE, CreatedAt: f.Ctx.BlockTime(), LastSettledAt: f.Ctx.BlockTime(),
		MinLeaseDurationAtCreation: 100,
	}
	account := types.CreditAccount{
		Tenant: tenant.String(), CreditAddress: types.DeriveCreditAddress(tenant).String(), ActiveLeaseCount: 1,
		ReservedAmounts: sdk.NewCoins(sdk.NewInt64Coin(testDenom, 100)),
	}
	require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, lease))
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, account))
	f.fundAccount(t, types.DeriveCreditAddress(tenant), sdk.NewCoins(sdk.NewInt64Coin(testDenom, 50)))
	return lease, account
}

// Write only the primary value so the deliberately stale secondary index is
// retained. Current SetLease would maintain the index and mask the corruption.
func writeReservationMigrationPrimary(t *testing.T, f *testFixture, lease types.Lease) {
	t.Helper()
	encoded, err := f.App.BillingKeeper.Leases.ValueCodec().Encode(lease)
	require.NoError(t, err)
	key, err := collections.EncodeKeyWithPrefix(types.LeaseKey.Bytes(), collections.StringKey, lease.Uuid)
	require.NoError(t, err)
	f.Ctx.KVStore(f.App.GetKey(types.StoreKey)).Set(key, encoded)
}
