package keeper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestLiveSettlementPreservesFractionalAccrualAcrossPartitions(t *testing.T) {
	tests := []struct {
		name         string
		providerWide bool
	}{
		{name: "specific lease"},
		{name: "provider wide", providerWide: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := initFixture(t)
			msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)
			baseTime := time.Date(2030, time.January, 2, 3, 4, 5, 100_000_000, time.UTC)
			f.Ctx = f.Ctx.WithBlockTime(baseTime).WithEventManager(sdk.NewEventManager())

			repeatedTenant := f.TestAccs[0]
			combinedTenant := f.TestAccs[1]
			repeatedProviderAddress := f.TestAccs[2]
			combinedProviderAddress := f.TestAccs[3]
			payoutAddress := f.TestAccs[4]
			repeatedProvider := f.createTestProvider(
				t,
				repeatedProviderAddress.String(),
				payoutAddress.String(),
			)
			combinedProvider := f.createTestProvider(
				t,
				combinedProviderAddress.String(),
				payoutAddress.String(),
			)
			repeatedSKU := f.createTestSKU(t, repeatedProvider.Uuid, 3_600)
			combinedSKU := f.createTestSKU(t, combinedProvider.Uuid, 3_600)

			for _, tenant := range []sdk.AccAddress{repeatedTenant, combinedTenant} {
				creditAddress := types.DeriveCreditAddress(tenant)
				f.fundAccount(t, creditAddress, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 100_000)))
				require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
					Tenant:        tenant.String(),
					CreditAddress: creditAddress.String(),
				}))
			}

			repeatedLeaseUUID := f.createAndAcknowledgeLease(
				t,
				msgServer,
				repeatedTenant,
				repeatedProviderAddress,
				[]types.LeaseItemInput{{SkuUuid: repeatedSKU.Uuid, Quantity: 1}},
			)
			combinedLeaseUUID := f.createAndAcknowledgeLease(
				t,
				msgServer,
				combinedTenant,
				combinedProviderAddress,
				[]types.LeaseItemInput{{SkuUuid: combinedSKU.Uuid, Quantity: 1}},
			)

			withdraw := func(
				providerAddress sdk.AccAddress,
				providerUUID,
				leaseUUID string,
			) (*types.MsgWithdrawResponse, error) {
				msg := &types.MsgWithdraw{Sender: providerAddress.String()}
				if tt.providerWide {
					msg.ProviderUuid = providerUUID
				} else {
					msg.LeaseUuids = []string{leaseUUID}
				}
				return msgServer.Withdraw(f.Ctx, msg)
			}

			offsets := [...]time.Duration{
				600 * time.Millisecond,
				1_200 * time.Millisecond,
				1_800 * time.Millisecond,
				2_400 * time.Millisecond,
				3_100 * time.Millisecond,
			}
			expectedStepAmounts := [...]int64{0, 1, 0, 1, 1}
			expectedCumulativeAmounts := [...]int64{0, 1, 1, 2, 3}
			repeatedTotal := sdkmath.ZeroInt()
			for index, offset := range offsets {
				f.Ctx = f.Ctx.WithBlockTime(baseTime.Add(offset)).WithEventManager(sdk.NewEventManager())
				response, err := withdraw(
					repeatedProviderAddress,
					repeatedProvider.Uuid,
					repeatedLeaseUUID,
				)
				if expectedStepAmounts[index] == 0 {
					if tt.providerWide {
						require.NoError(t, err)
						require.NotNil(t, response)
						require.True(t, response.TotalAmounts.IsZero())
						require.Zero(t, response.WithdrawalCount)
					} else {
						require.ErrorIs(t, err, types.ErrNoWithdrawableAmount)
						require.Nil(t, response)
					}
				} else {
					require.NoError(t, err)
					require.Equal(
						t,
						sdkmath.NewInt(expectedStepAmounts[index]),
						response.TotalAmounts.AmountOf(testDenom),
					)
					repeatedTotal = repeatedTotal.Add(response.TotalAmounts.AmountOf(testDenom))
				}

				stored, err := f.App.BillingKeeper.GetLease(f.Ctx, repeatedLeaseUUID)
				require.NoError(t, err)
				expectedCursor := baseTime.Add(
					time.Duration(expectedCumulativeAmounts[index]) * time.Second,
				)
				require.True(t, stored.LastSettledAt.Equal(expectedCursor),
					"operation %d stored cursor %s, expected %s",
					index, stored.LastSettledAt, expectedCursor,
				)
			}

			combinedResponse, err := withdraw(
				combinedProviderAddress,
				combinedProvider.Uuid,
				combinedLeaseUUID,
			)
			require.NoError(t, err)
			combinedTotal := combinedResponse.TotalAmounts.AmountOf(testDenom)
			require.Equal(t, sdkmath.NewInt(3), combinedTotal)
			require.Equal(t, combinedTotal, repeatedTotal)

			// A terminal close charges the final complete second from the retained
			// remainder, then records the exact close time and discards the final
			// sub-second remainder because the lease can never accrue again.
			closeTime := baseTime.Add(4_200 * time.Millisecond)
			f.Ctx = f.Ctx.WithBlockTime(closeTime).WithEventManager(sdk.NewEventManager())
			for _, entry := range []struct {
				tenant    sdk.AccAddress
				leaseUUID string
			}{
				{tenant: repeatedTenant, leaseUUID: repeatedLeaseUUID},
				{tenant: combinedTenant, leaseUUID: combinedLeaseUUID},
			} {
				closeResponse, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
					Sender:     entry.tenant.String(),
					LeaseUuids: []string{entry.leaseUUID},
				})
				require.NoError(t, err)
				require.Equal(t, sdkmath.OneInt(), closeResponse.TotalSettledAmounts.AmountOf(testDenom))

				stored, err := f.App.BillingKeeper.GetLease(f.Ctx, entry.leaseUUID)
				require.NoError(t, err)
				require.Equal(t, types.LEASE_STATE_CLOSED, stored.State)
				require.NotNil(t, stored.ClosedAt)
				require.True(t, stored.ClosedAt.Equal(closeTime))
				require.True(t, stored.LastSettledAt.Equal(closeTime))
			}
		})
	}
}
