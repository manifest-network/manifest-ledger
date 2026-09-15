package keeper_test

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestMsgWithdrawImportedClosedOverflowFinalizesWithoutDebt(t *testing.T) {
	for _, test := range []struct {
		name        string
		available   sdk.Coins
		expected    sdk.Coins
		multiDenom  bool
		sumOverflow bool
	}{
		{name: "positive credit", available: sdk.NewCoins(sdk.NewInt64Coin(testDenom, 500)), expected: sdk.NewCoins(sdk.NewInt64Coin(testDenom, 500))},
		{name: "zero credit"},
		{
			name: "overflow does not drain ordinary denom", multiDenom: true,
			available: sdk.NewCoins(sdk.NewInt64Coin(testDenom, 500), sdk.NewInt64Coin(testDenom2, 20_000)),
			expected:  sdk.NewCoins(sdk.NewInt64Coin(testDenom, 500), sdk.NewInt64Coin(testDenom2, 14_400)),
		},
		{
			name: "zero overflow credit preserves ordinary payment", multiDenom: true,
			available: sdk.NewCoins(sdk.NewInt64Coin(testDenom2, 20_000)),
			expected:  sdk.NewCoins(sdk.NewInt64Coin(testDenom2, 14_400)),
		},
		{
			name: "sum overflow preserves ordinary payment", multiDenom: true, sumOverflow: true,
			available: sdk.NewCoins(sdk.NewInt64Coin(testDenom, 500), sdk.NewInt64Coin(testDenom2, 20_000)),
			expected:  sdk.NewCoins(sdk.NewInt64Coin(testDenom, 500), sdk.NewInt64Coin(testDenom2, 14_400)),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, provider, payout, closed := setupImportedClosedOverflow(t, test.available, test.multiDenom, test.sumOverflow)
			k := f.App.BillingKeeper
			server := keeper.NewMsgServerImpl(k)
			querier := keeper.NewQuerier(k)
			creditAddress := types.DeriveCreditAddress(f.TestAccs[0])
			accountBefore, err := k.GetCreditAccount(f.Ctx, closed.Tenant)
			require.NoError(t, err)
			activeBefore, err := k.GetLease(f.Ctx, testLeaseUUID2)
			require.NoError(t, err)
			payoutBefore := f.App.BankKeeper.GetAllBalances(f.Ctx, payout)
			f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())
			beforeQuery := snapshotPayoutStores(t, f, f.Ctx)
			quote, err := querier.WithdrawableAmount(f.Ctx, &types.QueryWithdrawableAmountRequest{LeaseUuid: closed.Uuid})
			require.NoError(t, err)
			require.Equal(t, test.expected.String(), quote.Amounts.String())
			require.Equal(t, beforeQuery, snapshotPayoutStores(t, f, f.Ctx))

			response, err := server.Withdraw(f.Ctx, &types.MsgWithdraw{Sender: provider.String(), LeaseUuids: []string{closed.Uuid}})
			require.NoError(t, err, "overflowing retained accrual must use the same spendable cap as its quote")
			require.Equal(t, quote.Amounts.String(), response.TotalAmounts.String())
			stored, err := k.GetLease(f.Ctx, closed.Uuid)
			require.NoError(t, err)
			require.Equal(t, types.LEASE_STATE_CLOSED, stored.State)
			require.Equal(t, closed.ClosedAt, stored.ClosedAt)
			require.Equal(t, *closed.ClosedAt, stored.LastSettledAt)
			require.True(t, stored.Reservation.RemainingAmounts.IsZero())
			require.Equal(t, payoutBefore.Add(test.expected...).String(), f.App.BankKeeper.GetAllBalances(f.Ctx, payout).String())
			remainder := test.available.Sub(test.expected...)
			require.Equal(t, accountBefore.ReservedAmounts.Add(remainder...).String(), f.App.BankKeeper.GetAllBalances(f.Ctx, creditAddress).String())
			accountAfter, err := k.GetCreditAccount(f.Ctx, closed.Tenant)
			require.NoError(t, err)
			require.Equal(t, accountBefore, accountAfter, "terminal settlement cannot consume a live sibling's reservation")
			activeAfter, err := k.GetLease(f.Ctx, activeBefore.Uuid)
			require.NoError(t, err)
			require.Equal(t, activeBefore, activeAfter)
			require.NoError(t, k.ExportGenesis(f.Ctx).ValidateCurrentState())

			var payoutEvents sdk.Events
			for _, event := range f.Ctx.EventManager().Events() {
				if event.Type == types.EventTypeProviderWithdraw {
					payoutEvents = append(payoutEvents, event)
				}
			}
			if test.expected.IsZero() {
				require.Zero(t, response.WithdrawalCount)
				require.Empty(t, payoutEvents)
			} else {
				require.Equal(t, uint64(1), response.WithdrawalCount)
				require.Len(t, payoutEvents, 1)
				require.Equal(t, test.expected.String(), attrValue(t, payoutEvents[0], types.AttributeKeyAmount))
			}

			// A later deposit cannot revive the overflowed charge or an unpaid
			// representable denomination once the terminal interval is finalized.
			f.fundAccount(t, creditAddress, accountBefore.ReservedAmounts)
			f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(time.Hour)).WithEventManager(sdk.NewEventManager())
			beforeRetry := snapshotPayoutStores(t, f, f.Ctx)
			quote, err = querier.WithdrawableAmount(f.Ctx, &types.QueryWithdrawableAmountRequest{LeaseUuid: closed.Uuid})
			require.NoError(t, err)
			require.Empty(t, quote.Amounts)
			response, err = server.Withdraw(f.Ctx, &types.MsgWithdraw{Sender: provider.String(), LeaseUuids: []string{closed.Uuid}})
			require.ErrorIs(t, err, types.ErrNoWithdrawableAmount)
			require.Nil(t, response)
			require.Equal(t, beforeRetry, snapshotPayoutStores(t, f, f.Ctx))
			require.Empty(t, f.Ctx.EventManager().Events())
		})
	}
}

func TestMsgWithdrawImportedClosedOverflowMixedActiveBatch(t *testing.T) {
	for _, closedFirst := range []bool{true, false} {
		name := "active first"
		if closedFirst {
			name = "closed first"
		}
		t.Run(name, func(t *testing.T) {
			available := sdk.NewCoins(sdk.NewInt64Coin(testDenom, 500))
			f, provider, payout, closed := setupImportedClosedOverflow(t, available, false, false)
			k := f.App.BillingKeeper
			activeBefore, err := k.GetLease(f.Ctx, testLeaseUUID2)
			require.NoError(t, err)
			accountBefore, err := k.GetCreditAccount(f.Ctx, closed.Tenant)
			require.NoError(t, err)
			f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(time.Second)).WithEventManager(sdk.NewEventManager())
			uuids := []string{closed.Uuid, activeBefore.Uuid}
			if !closedFirst {
				slices.Reverse(uuids)
			}
			response, err := keeper.NewMsgServerImpl(k).Withdraw(f.Ctx, &types.MsgWithdraw{Sender: provider.String(), LeaseUuids: uuids})
			require.NoError(t, err, "the retained overflow must not roll back a valid ACTIVE sibling")
			require.Equal(t, uint64(2), response.WithdrawalCount)
			require.Equal(t, "501"+testDenom, response.TotalAmounts.String())
			require.Equal(t, response.TotalAmounts, f.App.BankKeeper.GetAllBalances(f.Ctx, payout))
			storedClosed, err := k.GetLease(f.Ctx, closed.Uuid)
			require.NoError(t, err)
			require.Equal(t, *closed.ClosedAt, storedClosed.LastSettledAt)
			storedActive, err := k.GetLease(f.Ctx, activeBefore.Uuid)
			require.NoError(t, err)
			require.Equal(t, types.LEASE_STATE_ACTIVE, storedActive.State)
			require.Equal(t, f.Ctx.BlockTime(), storedActive.LastSettledAt)
			require.Equal(t, "3599"+testDenom, storedActive.Reservation.RemainingAmounts.String())
			accountAfter, err := k.GetCreditAccount(f.Ctx, closed.Tenant)
			require.NoError(t, err)
			require.Equal(t, accountBefore.ActiveLeaseCount, accountAfter.ActiveLeaseCount)
			require.Equal(t, storedActive.Reservation.RemainingAmounts, accountAfter.ReservedAmounts)
			require.Equal(t, accountAfter.ReservedAmounts, f.App.BankKeeper.GetAllBalances(f.Ctx, types.DeriveCreditAddress(f.TestAccs[0])))
			batch := findEvent(t, f.Ctx, types.EventTypeBatchWithdraw)
			require.Equal(t, "2", attrValue(t, batch, types.AttributeKeyLeaseCount))
			require.Equal(t, response.TotalAmounts.String(), attrValue(t, batch, types.AttributeKeyAmount))
			require.NoError(t, k.ExportGenesis(f.Ctx).ValidateCurrentState())
		})
	}
}

// Import an actual v4 snapshot with a retained two-hour CLOSED interval. The
// original one-hour reservation fits math.Int, but the later charge overflows.
// The ordinary ACTIVE sibling still owns the fixture's complete reservation.
func setupImportedClosedOverflow(t *testing.T, available sdk.Coins, multiDenom, sumOverflow bool) (*testFixture, sdk.AccAddress, sdk.AccAddress, types.Lease) {
	t.Helper()
	f, provider, payout, closed := setupImportedClosedWithdrawal(t, available, multiDenom)
	k := f.App.BillingKeeper
	genesis := k.ExportGenesis(f.Ctx)
	for i := range genesis.Leases {
		lease := &genesis.Leases[i]
		if lease.Uuid != closed.Uuid {
			continue
		}
		lease.LastSettledAt = lease.ClosedAt.Add(-2 * time.Hour)
		lease.CreatedAt = lease.LastSettledAt
		lease.Items[0].LockedPrice = sdk.NewCoin(testDenom, maxBillingTestInt().QuoRaw(3600))
		if sumOverflow {
			// Each two-hour item charge fits, while their same-denom sum does not.
			lease.Items[0].LockedPrice = sdk.NewCoin(testDenom, maxBillingTestInt().QuoRaw(10_000))
			second := lease.Items[0]
			second.ServiceName = "overflow-sibling"
			lease.Items = append(lease.Items, second)
		}
		closed = *lease
	}
	require.NoError(t, genesis.ValidateCurrentState())
	require.NoError(t, k.InitGenesis(f.Ctx, genesis))
	require.NoError(t, k.ExportGenesis(f.Ctx).ValidateCurrentState())
	return f, provider, payout, closed
}
