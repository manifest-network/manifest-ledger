package keeper_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// Callers of the settlement helper supply reservation snapshots. Inconsistent
// snapshots must fail before a transfer or any mutation of those caller values,
// including when the inconsistency concerns a different denomination/cohort.
func TestReservationSettlementRejectsInconsistentSnapshotsBeforeMutation(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*types.Lease, *types.CreditAccount, sdk.AccAddress)
		err    string
	}{
		{
			name: "invalid lease identity",
			mutate: func(lease *types.Lease, _ *types.CreditAccount, _ sdk.AccAddress) {
				lease.Tenant = "invalid"
			},
			err: "invalid tenant address",
		},
		{
			name: "invalid account identity",
			mutate: func(_ *types.Lease, account *types.CreditAccount, _ sdk.AccAddress) {
				account.Tenant = "invalid"
			},
			err: "credit account has invalid tenant address",
		},
		{
			name: "different tenant account",
			mutate: func(_ *types.Lease, account *types.CreditAccount, other sdk.AccAddress) {
				account.Tenant = other.String()
			},
			err: "tenant does not match credit account tenant",
		},
		{
			name: "uninitialized reservation",
			mutate: func(lease *types.Lease, _ *types.CreditAccount, _ sdk.AccAddress) {
				lease.Reservation = nil
			},
			err: "no initialized reservation",
		},
		{
			name: "negative remaining reservation",
			mutate: func(lease *types.Lease, _ *types.CreditAccount, _ sdk.AccAddress) {
				lease.Reservation.RemainingAmounts = sdk.Coins{{Denom: testDenom, Amount: sdkmath.NewInt(-1)}}
			},
			err: "invalid remaining reservation",
		},
		{
			name: "terminal attributed reservation",
			mutate: func(lease *types.Lease, _ *types.CreditAccount, _ sdk.AccAddress) {
				lease.State = types.LEASE_STATE_CLOSED
			},
			err: "terminal lease",
		},
		{
			name: "legacy lease claims modern tranche",
			mutate: func(lease *types.Lease, _ *types.CreditAccount, _ sdk.AccAddress) {
				lease.MinLeaseDurationAtCreation = 0
			},
			err: "must not have an attributed reservation",
		},
		{
			name: "legacy lease absent from cohort count",
			mutate: func(lease *types.Lease, _ *types.CreditAccount, _ sdk.AccAddress) {
				lease.MinLeaseDurationAtCreation = 0
				lease.Reservation.RemainingAmounts = sdk.NewCoins()
			},
			err: "not represented in the unattributed lease count",
		},
		{
			name: "orphan legacy reservation on modern settlement",
			mutate: func(_ *types.Lease, account *types.CreditAccount, _ sdk.AccAddress) {
				account.UnattributedReservedAmounts = sdk.NewCoins(sdk.NewInt64Coin(testDenom, 1))
			},
			err: "without live cohort members",
		},
		{
			name: "legacy cohort exceeds aggregate",
			mutate: func(_ *types.Lease, account *types.CreditAccount, _ sdk.AccAddress) {
				account.UnattributedReservedAmounts = sdk.NewCoins(sdk.NewInt64Coin(testDenom, 101))
				account.UnattributedLeaseCount = 1
			},
			err: "does not contain its unattributed reservation",
		},
		{
			name: "allocation includes unrelated denomination",
			mutate: func(lease *types.Lease, account *types.CreditAccount, _ sdk.AccAddress) {
				lease.Reservation.RemainingAmounts = sdk.NewCoins(sdk.NewInt64Coin(testDenom2, 1))
				account.ReservedAmounts = account.ReservedAmounts.Add(lease.Reservation.RemainingAmounts...)
			},
			err: "reservation contains unrelated denom",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, lease, account := reservationFailureSnapshot(t)
			test.mutate(&lease, &account, f.TestAccs[3])
			leaseBefore, err := lease.Marshal()
			require.NoError(t, err)
			accountBefore, err := account.Marshal()
			require.NoError(t, err)
			storesBefore := snapshotPayoutStores(t, f, f.Ctx)
			result, err := f.App.BillingKeeper.PerformSettlement(f.Ctx, &lease, &account, f.Ctx.BlockTime().Add(time.Second))
			require.ErrorIs(t, err, types.ErrReservationInvariant)
			require.ErrorContains(t, err, test.err)
			require.Nil(t, result)
			leaseAfter, err := lease.Marshal()
			require.NoError(t, err)
			accountAfter, err := account.Marshal()
			require.NoError(t, err)
			require.Equal(t, leaseBefore, leaseAfter)
			require.Equal(t, accountBefore, accountAfter)
			require.Equal(t, storesBefore, snapshotPayoutStores(t, f, f.Ctx))
			require.Empty(t, f.Ctx.EventManager().Events())
		})
	}
}

func TestTerminalLegacySettlementHasNoCohortClaim(t *testing.T) {
	f, lease, account := reservationFailureSnapshot(t)
	sibling := lease
	sibling.Uuid = testLeaseUUID2
	require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, sibling))
	lease.MinLeaseDurationAtCreation = 0
	lease.State = types.LEASE_STATE_CLOSED
	lease.Reservation = &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()}
	closedAt := f.Ctx.BlockTime().Add(150 * time.Second)
	lease.ClosedAt = &closedAt
	f.Ctx = f.Ctx.WithBlockTime(closedAt)
	// A terminal legacy lease no longer belongs to the live cohort. An
	// equivalent uppercase tenant spelling must still identify its account.
	lease.Tenant = strings.ToUpper(lease.Tenant)
	require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, lease))
	require.NoError(t, f.App.BillingKeeper.LeaseSequence.Set(f.Ctx, 2))
	require.NoError(t, f.App.BillingKeeper.ExportGenesis(f.Ctx).ValidateCurrentState())
	accountBefore, err := account.Marshal()
	require.NoError(t, err)
	result, err := f.App.BillingKeeper.PerformSettlement(f.Ctx, &lease, &account, closedAt)
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 100)), result.TransferAmounts,
		"terminal legacy accrual may consume only unreserved credit")
	accountAfter, err := account.Marshal()
	require.NoError(t, err)
	require.Equal(t, accountBefore, accountAfter)
	require.Empty(t, lease.Reservation.RemainingAmounts)
	require.Equal(t, account.ReservedAmounts, f.App.BankKeeper.GetAllBalances(f.Ctx, types.DeriveCreditAddress(f.TestAccs[0])))
	storedSibling, err := f.App.BillingKeeper.GetLease(f.Ctx, sibling.Uuid)
	require.NoError(t, err)
	require.Equal(t, sibling, storedSibling)
}

func reservationFailureSnapshot(t *testing.T) (*testFixture, types.Lease, types.CreditAccount) {
	t.Helper()
	f := initFixture(t)
	tenant, providerAddress, payout := f.TestAccs[0], f.TestAccs[1], f.TestAccs[2]
	provider := f.createTestProvider(t, providerAddress.String(), payout.String())
	credit := types.DeriveCreditAddress(tenant)
	f.fundAccount(t, credit, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 200)))
	lease := types.Lease{
		Uuid: testLeaseUUID1, Tenant: tenant.String(), ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{{SkuUuid: testSKUUUID, Quantity: 1, LockedPrice: sdk.NewInt64Coin(testDenom, 1)}},
		State: types.LEASE_STATE_ACTIVE, CreatedAt: f.Ctx.BlockTime(), LastSettledAt: f.Ctx.BlockTime(),
		MinLeaseDurationAtCreation: 100,
		Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins(sdk.NewInt64Coin(testDenom, 100))},
	}
	account := types.CreditAccount{
		Tenant: tenant.String(), CreditAddress: credit.String(), ActiveLeaseCount: 1,
		ReservedAmounts: sdk.NewCoins(sdk.NewInt64Coin(testDenom, 100)),
	}
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, account))
	f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())
	return f, lease, account
}
