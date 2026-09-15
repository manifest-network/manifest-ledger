package keeper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestInitGenesisRetainedClosedIntervalRequiresCreditAccountBeforeWrites(t *testing.T) {
	for _, format := range []string{"aggregate-only", "consumable"} {
		for _, interval := range []struct {
			name     string
			duration time.Duration
			payment  sdk.Coins
		}{
			{name: "whole second", duration: time.Second, payment: sdk.NewCoins(sdk.NewInt64Coin(testDenom, 2))},
			{name: "subsecond", duration: time.Nanosecond},
		} {
			t.Run(format+"/"+interval.name, func(t *testing.T) {
				f := initFixture(t)
				k := f.App.BillingKeeper
				tenant, providerAddress, payoutAddress := f.TestAccs[0], f.TestAccs[1], f.TestAccs[2]
				provider := f.createTestProvider(t, providerAddress.String(), payoutAddress.String())
				sku := f.createTestSKU(t, provider.Uuid, 7200)
				creditAddress := types.DeriveCreditAddress(tenant)
				// A bank balance alone is not a billing credit account. Import must
				// reject the missing primary record before creating any billing state.
				funding := sdk.NewCoins(sdk.NewInt64Coin(testDenom, 7))
				f.fundAccount(t, creditAddress, funding)
				now := f.Ctx.BlockTime()
				lease := types.Lease{
					Uuid: testLeaseUUID1, Tenant: tenant.String(), ProviderUuid: provider.Uuid,
					Items: []types.LeaseItem{{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewInt64Coin(testDenom, 2)}},
					State: types.LEASE_STATE_CLOSED, CreatedAt: now.Add(-interval.duration),
					LastSettledAt: now.Add(-interval.duration), ClosedAt: &now, MinLeaseDurationAtCreation: 3600,
				}
				if format == "consumable" {
					lease.Reservation = &types.LeaseReservation{}
				}
				params := types.DefaultParams()
				params.MaxLeasesPerTenant++
				genesis := types.NewGenesisState(params, []types.Lease{lease}, nil, 1)
				before := snapshotPayoutStores(t, f, f.Ctx)
				err := k.InitGenesis(f.Ctx, genesis)
				require.ErrorContains(t, err, "retained final interval but no credit account")
				require.Equal(t, before, snapshotPayoutStores(t, f, f.Ctx))

				// Supplying the deterministic, empty billing account is sufficient;
				// the import does not need to invent an auth account or bank balance.
				genesis.CreditAccounts = []types.CreditAccount{{Tenant: tenant.String(), CreditAddress: creditAddress.String()}}
				require.NoError(t, k.InitGenesis(f.Ctx, genesis))
				require.NoError(t, k.ExportGenesis(f.Ctx).ValidateCurrentState())
				f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())
				quote, err := keeper.NewQuerier(k).WithdrawableAmount(f.Ctx, &types.QueryWithdrawableAmountRequest{LeaseUuid: lease.Uuid})
				require.NoError(t, err)
				require.Equal(t, interval.payment.String(), quote.Amounts.String())
				response, err := keeper.NewMsgServerImpl(k).Withdraw(f.Ctx, &types.MsgWithdraw{Sender: providerAddress.String(), LeaseUuids: []string{lease.Uuid}})
				require.NoError(t, err)
				require.Equal(t, quote.Amounts.String(), response.TotalAmounts.String())
				stored, err := k.GetLease(f.Ctx, lease.Uuid)
				require.NoError(t, err)
				require.Equal(t, now, stored.LastSettledAt)
				require.Equal(t, interval.payment.String(), f.App.BankKeeper.GetAllBalances(f.Ctx, payoutAddress).String())
				require.Equal(t, funding.Sub(interval.payment...).String(), f.App.BankKeeper.GetAllBalances(f.Ctx, creditAddress).String())
			})
		}
	}
}
