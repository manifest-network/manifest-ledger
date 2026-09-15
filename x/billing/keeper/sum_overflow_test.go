package keeper_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

func TestSumOverflow_PreservesOtherDenomsInQuotesAndSettlement(t *testing.T) {
	for _, operation := range []string{"close", "withdraw"} {
		for ordinaryPosition := range 3 {
			t.Run(fmt.Sprintf("%s/ordinary-at-%d", operation, ordinaryPosition), func(t *testing.T) {
				f := initFixture(t)
				k := f.App.BillingKeeper
				msgServer := keeper.NewMsgServerImpl(k)
				tenant, providerAddress := f.TestAccs[0], f.TestAccs[1]
				provider := f.createTestProvider(t, providerAddress.String(), providerAddress.String())
				ordinary := f.createTestSKUWithDenom(t, provider.Uuid, 3600, "uok")
				largeA := f.createTestSKUWithDenom(t, provider.Uuid, 3600, "uoverflow")
				largeB := f.createTestSKUWithDenom(t, provider.Uuid, 3600, "uoverflow")

				// Both hourly SKU prices, the creation reservation, and each item
				// accrual fit math.Int. Only the combined 7,201-second charge in
				// uoverflow exceeds its range; the uok charge remains exact.
				rate := highBitBillingTestInt().QuoRaw(7200)
				largeA.BasePrice = sdk.NewCoin("uoverflow", rate.MulRaw(3600))
				largeB.BasePrice = largeA.BasePrice
				require.NoError(t, skutypes.ValidatePriceAndUnit(largeA.BasePrice, largeA.Unit))
				require.NoError(t, skutypes.ValidatePriceAndUnit(largeB.BasePrice, largeB.Unit))
				require.NoError(t, f.App.SKUKeeper.SetSKU(f.Ctx, largeA))
				require.NoError(t, f.App.SKUKeeper.SetSKU(f.Ctx, largeB))

				creditAddress := types.DeriveCreditAddress(tenant)
				initialCredit := sdk.NewCoins(
					sdk.NewInt64Coin("uok", 10_000),
					sdk.NewCoin("uoverflow", rate.MulRaw(7200)),
				)
				f.fundAccount(t, creditAddress, initialCredit)
				require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
					Tenant: tenant.String(), CreditAddress: creditAddress.String(),
				}))
				items := []types.LeaseItemInput{
					{SkuUuid: ordinary.Uuid, Quantity: 1},
					{SkuUuid: largeA.Uuid, Quantity: 1},
					{SkuUuid: largeB.Uuid, Quantity: 1},
				}
				items[0], items[ordinaryPosition] = items[ordinaryPosition], items[0]
				leaseUUID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddress, items)
				leaseBefore, err := k.GetLease(f.Ctx, leaseUUID)
				require.NoError(t, err)
				creditBefore, err := k.GetCreditAccount(f.Ctx, tenant.String())
				require.NoError(t, err)
				f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(7201 * time.Second))
				expected := sdk.NewCoins(sdk.NewInt64Coin("uok", 7201), sdk.NewCoin("uoverflow", rate.MulRaw(7200)))

				querier := keeper.NewQuerier(k)
				singleQuote, err := querier.WithdrawableAmount(f.Ctx, &types.QueryWithdrawableAmountRequest{LeaseUuid: leaseUUID})
				require.NoError(t, err)
				require.Equal(t, expected, singleQuote.Amounts)
				providerQuote, err := querier.ProviderWithdrawable(f.Ctx, &types.QueryProviderWithdrawableRequest{ProviderUuid: provider.Uuid})
				require.NoError(t, err)
				require.Equal(t, expected, providerQuote.Amounts)
				require.Equal(t, uint64(1), providerQuote.LeaseCount)
				require.Empty(t, providerQuote.FailedLeaseUuids)

				// Dry-run auto-close and settlement must not consume state.
				leaseAfterQuote, err := k.GetLease(f.Ctx, leaseUUID)
				require.NoError(t, err)
				require.Equal(t, leaseBefore, leaseAfterQuote)
				creditAfterQuote, err := k.GetCreditAccount(f.Ctx, tenant.String())
				require.NoError(t, err)
				require.Equal(t, creditBefore, creditAfterQuote)
				require.Equal(t, initialCredit, f.App.BankKeeper.GetAllBalances(f.Ctx, creditAddress))
				require.Empty(t, f.App.BankKeeper.GetAllBalances(f.Ctx, providerAddress))

				if operation == "close" {
					response, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{Sender: tenant.String(), LeaseUuids: []string{leaseUUID}})
					require.NoError(t, err)
					require.Equal(t, expected, response.TotalSettledAmounts)
				} else {
					response, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{Sender: providerAddress.String(), ProviderUuid: provider.Uuid})
					require.NoError(t, err)
					require.Equal(t, providerQuote.Amounts, response.TotalAmounts)
					require.Equal(t, providerQuote.LeaseCount, response.WithdrawalCount)
					require.Empty(t, response.FailedLeaseUuids)
				}

				stored, err := k.GetLease(f.Ctx, leaseUUID)
				require.NoError(t, err)
				require.Equal(t, types.LEASE_STATE_CLOSED, stored.State)
				require.Equal(t, types.ClosureReasonCreditExhausted, stored.ClosureReason)
				require.Equal(t, f.Ctx.BlockTime(), stored.LastSettledAt)
				require.Empty(t, stored.Reservation.RemainingAmounts)
				account, err := k.GetCreditAccount(f.Ctx, tenant.String())
				require.NoError(t, err)
				require.Zero(t, account.ActiveLeaseCount)
				require.Empty(t, account.ReservedAmounts)
				require.Equal(t, expected, f.App.BankKeeper.GetAllBalances(f.Ctx, providerAddress))
				require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("uok", 2799)), f.App.BankKeeper.GetAllBalances(f.Ctx, creditAddress))
			})
		}
	}
}
