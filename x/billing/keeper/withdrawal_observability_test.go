package keeper_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestProviderWithdrawable_FailuresAreOrderedObservableAndRetryable(t *testing.T) {
	s := newSelfPayoutBatchFixture(t)
	k := s.f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	normalLease, err := k.GetLease(s.f.Ctx, s.normalLeaseUUID)
	require.NoError(t, err)
	require.Len(t, normalLease.Items, 1)
	skuUUID := normalLease.Items[0].SkuUuid

	// UUIDv7 generation is sequence-backed in tests, so these additions follow
	// the fixture's normal and first self-payout leases in provider-index order.
	secondSelfLeaseUUID := s.f.createAndAcknowledgeLease(
		t,
		s.msgServer,
		s.selfPayoutTenant,
		s.providerAddr,
		[]types.LeaseItemInput{{SkuUuid: skuUUID, Quantity: 1}},
	)
	tailLeaseUUID := s.f.createAndAcknowledgeLease(
		t,
		s.msgServer,
		s.normalTenant,
		s.providerAddr,
		[]types.LeaseItemInput{{SkuUuid: skuUUID, Quantity: 1}},
	)
	require.Less(t, s.normalLeaseUUID, s.selfLeaseUUID)
	require.Less(t, s.selfLeaseUUID, secondSelfLeaseUUID)
	require.Less(t, secondSelfLeaseUUID, tailLeaseUUID)

	failedBefore, err := k.GetLease(s.f.Ctx, s.selfLeaseUUID)
	require.NoError(t, err)
	settleCtx := s.f.Ctx.WithBlockTime(s.f.Ctx.BlockTime().Add(100 * time.Second)).
		WithEventManager(sdk.NewEventManager())
	s.f.Ctx = settleCtx

	estimate, err := querier.ProviderWithdrawable(settleCtx, &types.QueryProviderWithdrawableRequest{
		ProviderUuid: normalLease.ProviderUuid,
		Pagination:   &query.PageRequest{Limit: 3},
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 100)), estimate.Amounts)
	require.Equal(t, uint64(1), estimate.LeaseCount)
	require.Equal(t, []string{s.selfLeaseUUID, secondSelfLeaseUUID}, estimate.FailedLeaseUuids)
	require.NotNil(t, estimate.Pagination)
	require.Equal(t, []byte(tailLeaseUUID), estimate.Pagination.NextKey,
		"query cursors identify the first unread lease")

	// Query execution is a discarded simulation, including successful earlier
	// leases and both failed per-lease caches.
	afterEstimate, err := k.GetLease(settleCtx, s.normalLeaseUUID)
	require.NoError(t, err)
	require.Equal(t, normalLease.LastSettledAt, afterEstimate.LastSettledAt)

	firstPage, err := s.msgServer.Withdraw(settleCtx, &types.MsgWithdraw{
		Sender:       s.providerAddr.String(),
		ProviderUuid: normalLease.ProviderUuid,
		Limit:        3,
	})
	require.NoError(t, err)
	require.Equal(t, estimate.Amounts, firstPage.TotalAmounts)
	require.Equal(t, estimate.LeaseCount, firstPage.WithdrawalCount)
	require.Equal(t, estimate.FailedLeaseUuids, firstPage.FailedLeaseUuids)
	require.True(t, firstPage.HasMore)
	require.Equal(t, []byte(secondSelfLeaseUUID), firstPage.NextKey,
		"transaction cursors identify the last processed lease, including failures")

	batchEvent := findEvent(t, settleCtx, types.EventTypeBatchWithdraw)
	require.Equal(t, "2", attrValue(t, batchEvent, types.AttributeKeyFailedLeaseCount))
	require.Equal(t,
		strings.Join([]string{s.selfLeaseUUID, secondSelfLeaseUUID}, ","),
		attrValue(t, batchEvent, types.AttributeKeyFailedLeaseUUIDs),
	)

	failedAfter, err := k.GetLease(settleCtx, s.selfLeaseUUID)
	require.NoError(t, err)
	require.Equal(t, failedBefore.LastSettledAt, failedAfter.LastSettledAt,
		"a failed lease cache must remain retryable")
	normalAfter, err := k.GetLease(settleCtx, s.normalLeaseUUID)
	require.NoError(t, err)
	require.True(t, normalAfter.LastSettledAt.After(normalLease.LastSettledAt),
		"a successful lease before the failure must commit independently")

	// The failed UUIDs are before the transaction cursor. Continuing the cursor
	// therefore reaches the unread tail without silently implying the failures
	// succeeded; callers retain the explicit ordered retry list above.
	secondPage, err := s.msgServer.Withdraw(settleCtx, &types.MsgWithdraw{
		Sender:       s.providerAddr.String(),
		ProviderUuid: normalLease.ProviderUuid,
		Limit:        3,
		Key:          firstPage.NextKey,
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 100)), secondPage.TotalAmounts)
	require.Equal(t, uint64(1), secondPage.WithdrawalCount)
	require.Empty(t, secondPage.FailedLeaseUuids)
	require.False(t, secondPage.HasMore)
	require.Empty(t, secondPage.NextKey)

	// Correct the provider configuration and explicitly retry the response's
	// failed UUIDs. Their discarded caches preserve the full accrued interval.
	provider, err := s.f.App.SKUKeeper.GetProvider(settleCtx, normalLease.ProviderUuid)
	require.NoError(t, err)
	provider.PayoutAddress = s.f.TestAccs[3].String()
	require.NoError(t, s.f.App.SKUKeeper.SetProvider(settleCtx, provider))

	retry, err := s.msgServer.Withdraw(settleCtx, &types.MsgWithdraw{
		Sender:     s.providerAddr.String(),
		LeaseUuids: firstPage.FailedLeaseUuids,
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 200)), retry.TotalAmounts)
	require.Equal(t, uint64(2), retry.WithdrawalCount)
	require.Empty(t, retry.FailedLeaseUuids)
	require.Equal(t, s.f.TestAccs[3].String(), retry.PayoutAddress)
}

func TestMsgWithdraw_AutoCloseEventsReportPositiveTransfer(t *testing.T) {
	testCases := []struct {
		name          string
		providerWide  bool
		eventType     string
		reasonPresent bool
	}{
		{
			name:         "specific lease",
			providerWide: false,
			eventType:    types.EventTypeProviderWithdraw,
		},
		{
			name:          "provider wide",
			providerWide:  true,
			eventType:     types.EventTypeLeaseAutoClose,
			reasonPresent: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)
			tenant := f.TestAccs[0]
			providerAddr := f.TestAccs[1]
			payoutAddr := f.TestAccs[2]

			params := types.DefaultParams()
			params.MinLeaseDuration = 10
			require.NoError(t, f.App.BillingKeeper.SetParams(f.Ctx, params))

			provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
			sku := f.createTestSKU(t, provider.Uuid, 3600) // 1 unit per second
			creditAddr := types.DeriveCreditAddress(tenant)
			funding := sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10)))
			f.fundAccount(t, creditAddr, funding)
			require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
				Tenant:        tenant.String(),
				CreditAddress: creditAddr.String(),
			}))
			leaseUUID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{{
				SkuUuid:  sku.Uuid,
				Quantity: 1,
			}})

			f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second)).
				WithEventManager(sdk.NewEventManager())
			msg := &types.MsgWithdraw{Sender: providerAddr.String()}
			if tc.providerWide {
				msg.ProviderUuid = provider.Uuid
				msg.Limit = 1
			} else {
				msg.LeaseUuids = []string{leaseUUID}
			}

			response, err := msgServer.Withdraw(f.Ctx, msg)
			require.NoError(t, err)
			require.Equal(t, funding, response.TotalAmounts)
			require.Equal(t, uint64(1), response.WithdrawalCount)
			require.Empty(t, response.FailedLeaseUuids)

			event := findEvent(t, f.Ctx, tc.eventType)
			require.Equal(t, leaseUUID, attrValue(t, event, types.AttributeKeyLeaseUUID))
			require.Equal(t, funding.String(), attrValue(t, event, types.AttributeKeyAmount))
			require.Equal(t, payoutAddr.String(), attrValue(t, event, types.AttributeKeyPayoutAddress))
			if tc.reasonPresent {
				require.Equal(t, "credit_exhausted", attrValue(t, event, types.AttributeKeyReason))
				batchEvent := findEvent(t, f.Ctx, types.EventTypeBatchWithdraw)
				require.Equal(t, "0", attrValue(t, batchEvent, types.AttributeKeyFailedLeaseCount))
				require.Empty(t, attrValue(t, batchEvent, types.AttributeKeyFailedLeaseUUIDs))
			} else {
				require.Equal(t, "true", attrValue(t, event, types.AttributeKeyAutoClosed))
			}

			closed, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseUUID)
			require.NoError(t, err)
			require.Equal(t, types.LEASE_STATE_CLOSED, closed.State)
			require.Equal(t, sdkmath.NewInt(10),
				f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom).Amount)
		})
	}
}
