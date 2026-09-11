package keeper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestMsgWithdrawImportedClosedLeaseFinalizesWithoutDebt(t *testing.T) {
	for _, test := range []struct {
		name       string
		available  sdk.Coins
		multiDenom bool
	}{
		{name: "zero payment", available: sdk.NewCoins()},
		{name: "partial payment", available: sdk.NewCoins(sdk.NewInt64Coin(testDenom, 500))},
		{name: "full payment", available: sdk.NewCoins(sdk.NewInt64Coin(testDenom, 3600))},
		{name: "one denomination paid in full", available: sdk.NewCoins(sdk.NewInt64Coin(testDenom, 3600), sdk.NewInt64Coin(testDenom2, 100)), multiDenom: true},
		{name: "one denomination unpaid", available: sdk.NewCoins(sdk.NewInt64Coin(testDenom, 500)), multiDenom: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, provider, payout, closed := setupImportedClosedWithdrawal(t, test.available, test.multiDenom)
			k := f.App.BillingKeeper
			server := keeper.NewMsgServerImpl(k)
			creditAddress := types.DeriveCreditAddress(f.TestAccs[0])
			accountBefore, err := k.GetCreditAccount(f.Ctx, closed.Tenant)
			require.NoError(t, err)
			activeBefore, err := k.GetLease(f.Ctx, testLeaseUUID2)
			require.NoError(t, err)
			payoutBefore := f.App.BankKeeper.GetAllBalances(f.Ctx, payout)
			f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())

			response, err := server.Withdraw(f.Ctx, &types.MsgWithdraw{Sender: provider.String(), LeaseUuids: []string{closed.Uuid}})
			require.NoError(t, err, "a terminal interval can be finalized even when no unreserved credit is available")
			require.Equal(t, test.available.String(), response.TotalAmounts.String())
			stored, err := k.GetLease(f.Ctx, closed.Uuid)
			require.NoError(t, err)
			require.Equal(t, types.LEASE_STATE_CLOSED, stored.State)
			require.Equal(t, closed.ClosedAt, stored.ClosedAt)
			require.True(t, stored.Reservation.RemainingAmounts.IsZero())
			require.Equal(t, payoutBefore.Add(test.available...).String(), f.App.BankKeeper.GetAllBalances(f.Ctx, payout).String())
			require.Equal(t, accountBefore.ReservedAmounts.String(), f.App.BankKeeper.GetAllBalances(f.Ctx, creditAddress).String(), "another live lease's full reservation is protected")
			accountAfter, err := k.GetCreditAccount(f.Ctx, closed.Tenant)
			require.NoError(t, err)
			require.Equal(t, accountBefore.String(), accountAfter.String())
			activeAfter, err := k.GetLease(f.Ctx, testLeaseUUID2)
			require.NoError(t, err)
			require.Equal(t, activeBefore.String(), activeAfter.String())
			require.NoError(t, k.ExportGenesis(f.Ctx).ValidateCurrentState())

			var payoutEvents sdk.Events
			for _, event := range f.Ctx.EventManager().Events() {
				if event.Type == types.EventTypeProviderWithdraw {
					payoutEvents = append(payoutEvents, event)
				}
			}
			if test.available.IsZero() {
				require.Zero(t, response.WithdrawalCount)
				require.Empty(t, payoutEvents, "finalizing an unpaid terminal interval is not a payout")
			} else {
				require.Equal(t, uint64(1), response.WithdrawalCount)
				require.Len(t, payoutEvents, 1)
				require.Equal(t, test.available.String(), attrValue(t, payoutEvents[0], types.AttributeKeyAmount))
			}

			// A later deposit cannot revive written-off accrual or charge the
			// already-paid part again, including in another denomination.
			f.fundAccount(t, creditAddress, accountBefore.ReservedAmounts)
			f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(time.Hour)).WithEventManager(sdk.NewEventManager())
			storesBefore := snapshotPayoutStores(t, f, f.Ctx)
			response, err = server.Withdraw(f.Ctx, &types.MsgWithdraw{Sender: provider.String(), LeaseUuids: []string{closed.Uuid}})
			require.ErrorIs(t, err, types.ErrNoWithdrawableAmount, "retry response: %v", response)
			require.Nil(t, response)
			require.Equal(t, storesBefore, snapshotPayoutStores(t, f, f.Ctx))
			require.Empty(t, f.Ctx.EventManager().Events())
			require.Equal(t, *closed.ClosedAt, stored.LastSettledAt, "terminal shortfalls are written off, not carried as debt")
		})
	}
}

func TestMsgWithdrawImportedClosedLeaseBatchCountsOnlyPayouts(t *testing.T) {
	for _, available := range []sdk.Coins{sdk.NewCoins(), sdk.NewCoins(sdk.NewInt64Coin(testDenom, 500))} {
		name, count, countText := "zero-payment batch", uint64(0), "0"
		if !available.IsZero() {
			name, count, countText = "paid and zero-payment batch", 1, "1"
		}
		t.Run(name, func(t *testing.T) {
			f, provider, _, closed := setupImportedClosedWithdrawal(t, available, false)
			k := f.App.BillingKeeper
			genesis := k.ExportGenesis(f.Ctx)
			second := closed
			second.Uuid = "01912345-6789-7abc-8def-0123456789b0"
			second.Reservation = &types.LeaseReservation{}
			genesis.Leases = append(genesis.Leases, second)
			genesis.LeaseSequence++
			require.NoError(t, k.InitGenesis(f.Ctx, genesis))
			f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())

			response, err := keeper.NewMsgServerImpl(k).Withdraw(f.Ctx, &types.MsgWithdraw{
				Sender: provider.String(), LeaseUuids: []string{closed.Uuid, second.Uuid},
			})
			require.NoError(t, err)
			require.Equal(t, count, response.WithdrawalCount)
			require.Equal(t, available.String(), response.TotalAmounts.String())
			for _, uuid := range []string{closed.Uuid, second.Uuid} {
				stored, err := k.GetLease(f.Ctx, uuid)
				require.NoError(t, err)
				require.Equal(t, *closed.ClosedAt, stored.LastSettledAt)
			}
			var payoutCount uint64
			for _, event := range f.Ctx.EventManager().Events() {
				if event.Type == types.EventTypeProviderWithdraw {
					payoutCount++
					require.Equal(t, closed.Uuid, attrValue(t, event, types.AttributeKeyLeaseUUID))
					require.Equal(t, available.String(), attrValue(t, event, types.AttributeKeyAmount))
				}
			}
			require.Equal(t, count, payoutCount)
			batch := findEvent(t, f.Ctx, types.EventTypeBatchWithdraw)
			require.Equal(t, countText, attrValue(t, batch, types.AttributeKeyLeaseCount))
			require.Equal(t, available.String(), attrValue(t, batch, types.AttributeKeyAmount))
			require.NoError(t, k.ExportGenesis(f.Ctx).ValidateCurrentState())
		})
	}
}

func TestMsgWithdrawImportedClosedLeaseDiscardsSubsecondRemainder(t *testing.T) {
	f, provider, _, closed := setupImportedClosedWithdrawal(t, sdk.NewCoins(), false)
	k := f.App.BillingKeeper
	genesis := k.ExportGenesis(f.Ctx)
	for i := range genesis.Leases {
		if genesis.Leases[i].Uuid == closed.Uuid {
			genesis.Leases[i].LastSettledAt = closed.ClosedAt.Add(-500 * time.Millisecond)
		}
	}
	require.NoError(t, k.InitGenesis(f.Ctx, genesis))
	f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())
	response, err := keeper.NewMsgServerImpl(k).Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender: provider.String(), LeaseUuids: []string{closed.Uuid},
	})
	require.NoError(t, err)
	require.Zero(t, response.WithdrawalCount)
	require.Empty(t, response.TotalAmounts)
	stored, err := k.GetLease(f.Ctx, closed.Uuid)
	require.NoError(t, err)
	require.Equal(t, *closed.ClosedAt, stored.LastSettledAt)
	require.Empty(t, f.Ctx.EventManager().Events())
}

// Import a pre-v4 aggregate-only snapshot through the real import path. The
// CLOSED lease has a billable hour left, while an ACTIVE lease owns every
// reserved coin. Only the explicitly supplied extra credit can be withdrawn.
func setupImportedClosedWithdrawal(t *testing.T, available sdk.Coins, multiDenom bool) (*testFixture, sdk.AccAddress, sdk.AccAddress, types.Lease) {
	t.Helper()
	f := initFixture(t)
	tenant, providerAddress, payoutAddress := f.TestAccs[0], f.TestAccs[1], f.TestAccs[2]
	provider := f.createTestProvider(t, providerAddress.String(), payoutAddress.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)
	items := []types.LeaseItem{{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewInt64Coin(testDenom, 1), ServiceName: "first"}}
	reserved := sdk.NewCoins(sdk.NewInt64Coin(testDenom, 3600))
	if multiDenom {
		sku2 := f.createTestSKUWithDenom(t, provider.Uuid, 7200, testDenom2)
		items = append(items, types.LeaseItem{SkuUuid: sku2.Uuid, Quantity: 1, LockedPrice: sdk.NewInt64Coin(testDenom2, 2), ServiceName: "second"})
		reserved = reserved.Add(sdk.NewInt64Coin(testDenom2, 7200))
	}
	now := f.Ctx.BlockTime()
	closed := types.Lease{
		Uuid: testLeaseUUID1, Tenant: tenant.String(), ProviderUuid: provider.Uuid, Items: items,
		State: types.LEASE_STATE_CLOSED, CreatedAt: now.Add(-time.Hour), LastSettledAt: now.Add(-time.Hour),
		ClosedAt: &now, MinLeaseDurationAtCreation: 3600,
	}
	active := types.Lease{
		Uuid: testLeaseUUID2, Tenant: tenant.String(), ProviderUuid: provider.Uuid, Items: items,
		State: types.LEASE_STATE_ACTIVE, CreatedAt: now, LastSettledAt: now, MinLeaseDurationAtCreation: 3600,
	}
	creditAddress := types.DeriveCreditAddress(tenant)
	f.fundAccount(t, creditAddress, reserved.Add(available...))
	genesis := types.NewGenesisState(types.DefaultParams(), []types.Lease{closed, active}, []types.CreditAccount{{
		Tenant: tenant.String(), CreditAddress: creditAddress.String(), ReservedAmounts: reserved, ActiveLeaseCount: 1,
	}}, 2)
	require.NoError(t, genesis.Validate())
	require.NoError(t, genesis.ValidateWithBlockTime(now))
	require.NoError(t, f.App.BillingKeeper.InitGenesis(f.Ctx, genesis))
	require.NoError(t, f.App.BillingKeeper.ExportGenesis(f.Ctx).ValidateCurrentState())
	return f, providerAddress, payoutAddress, closed
}
