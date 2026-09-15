package keeper_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/store/types"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestEndBlockerAdvancesPastFailedPageBoundary(t *testing.T) {
	f, providerUUID, skuUUID, denom, baseTime := newExpiryFixture(t)
	k := f.App.BillingKeeper
	const failedIndex = types.MaxPendingLeaseExpirationsPerBlock - 1
	const candidateCount = types.MaxPendingLeaseExpirationsPerBlock + 1
	for i := range candidateCount {
		uuid := fmt.Sprintf("exclusive-boundary-%03d", i)
		f.setPendingLease(t, uuid, providerUUID, skuUUID, denom, baseTime)
		if i == failedIndex {
			lease, err := k.GetLease(f.Ctx, uuid)
			require.NoError(t, err)
			lease.Reservation = nil
			require.NoError(t, k.SetLease(f.Ctx, lease))
		}
	}

	// The first page makes 99 successful expirations, then fails at its last
	// row. The next page has room for one candidate: an inclusive cursor would
	// retry the same failure forever. A finite meter turns that regression into
	// a prompt test failure without a watchdog goroutine surviving the test.
	ctx := f.Ctx.WithBlockTime(baseTime.Add(61 * time.Second)).
		WithGasMeter(storetypes.NewGasMeter(20_000_000))
	require.NotPanics(t, func() { require.NoError(t, k.EndBlocker(ctx)) })
	t.Logf("expiration gas used: %d", ctx.GasMeter().GasConsumed())

	for i := range candidateCount {
		lease, err := k.GetLease(f.Ctx, fmt.Sprintf("exclusive-boundary-%03d", i))
		require.NoError(t, err)
		if i == failedIndex {
			require.Equal(t, types.LEASE_STATE_PENDING, lease.State)
			require.Nil(t, lease.ExpiredAt)
		} else {
			require.Equal(t, types.LEASE_STATE_EXPIRED, lease.State)
			require.Equal(t, ctx.BlockTime(), *lease.ExpiredAt)
		}
	}
	account, err := k.GetCreditAccount(f.Ctx, f.TestAccs[0].String())
	require.NoError(t, err)
	require.Equal(t, uint64(1), account.PendingLeaseCount)
	params, err := k.GetParams(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprint(params.MinLeaseDuration), account.ReservedAmounts.AmountOf(denom).String())
}

func TestExpirePendingLeaseFailureLeavesCallerAndStoreUnchanged(t *testing.T) {
	for _, failure := range []string{"credit lookup", "reservation release", "index write after reservation release"} {
		t.Run(failure, func(t *testing.T) {
			f, providerUUID, skuUUID, denom, baseTime := newExpiryFixture(t)
			k := f.App.BillingKeeper
			const uuid = "expiry-atomicity"
			f.setPendingLease(t, uuid, providerUUID, skuUUID, denom, baseTime)
			lease, err := k.GetLease(f.Ctx, uuid)
			require.NoError(t, err)
			switch failure {
			case "credit lookup":
				require.NoError(t, k.CreditAccounts.Remove(f.Ctx, f.TestAccs[0]))
			case "reservation release":
				lease.Reservation = nil
				require.NoError(t, k.SetLease(f.Ctx, lease))
			case "index write after reservation release":
				const domain = "expiry-atomicity.example.com"
				lease.Items[0].CustomDomain = domain
				require.NoError(t, k.SetLease(f.Ctx, lease))
				key, err := collections.EncodeKeyWithPrefix(types.CustomDomainIndexKey.Bytes(), collections.StringKey, domain)
				require.NoError(t, err)
				f.Ctx.KVStore(f.App.GetKey(types.StoreKey)).Set(key, []byte{0xff})
			}
			before := lease.String()
			accountBefore, accountErr := k.GetCreditAccount(f.Ctx, lease.Tenant)
			ctx := f.Ctx.WithBlockTime(baseTime.Add(61 * time.Second)).WithEventManager(sdk.NewEventManager())

			err = k.ExpirePendingLease(ctx, &lease)
			require.Error(t, err)
			if failure == "index write after reservation release" {
				require.ErrorIs(t, err, types.ErrInternalCorruption)
			}
			require.Equal(t, before, lease.String(), "caller must retain its state, timestamps, and reservation on failure")
			stored, err := k.GetLease(ctx, uuid)
			require.NoError(t, err)
			require.Equal(t, before, stored.String())
			accountAfter, err := k.GetCreditAccount(ctx, lease.Tenant)
			if accountErr != nil {
				require.ErrorIs(t, err, types.ErrCreditAccountNotFound)
			} else {
				require.NoError(t, err)
				require.Equal(t, accountBefore, accountAfter)
			}
			require.Empty(t, ctx.EventManager().Events())
		})
	}
}

func TestExpirePendingLeaseSuccessUpdatesCallerAfterCommit(t *testing.T) {
	f, providerUUID, skuUUID, denom, baseTime := newExpiryFixture(t)
	k := f.App.BillingKeeper
	const uuid = "expiry-success"
	f.setPendingLease(t, uuid, providerUUID, skuUUID, denom, baseTime)
	lease, err := k.GetLease(f.Ctx, uuid)
	require.NoError(t, err)
	ctx := f.Ctx.WithBlockTime(baseTime.Add(61 * time.Second)).WithEventManager(sdk.NewEventManager())
	require.NoError(t, k.ExpirePendingLease(ctx, &lease))
	require.Equal(t, types.LEASE_STATE_EXPIRED, lease.State)
	require.Equal(t, ctx.BlockTime(), *lease.ExpiredAt)
	require.Empty(t, lease.Reservation.RemainingAmounts)
	stored, err := k.GetLease(ctx, uuid)
	require.NoError(t, err)
	require.Equal(t, lease.String(), stored.String())
	account, err := k.GetCreditAccount(ctx, lease.Tenant)
	require.NoError(t, err)
	require.Zero(t, account.PendingLeaseCount)
	require.Empty(t, account.ReservedAmounts)
	require.Len(t, ctx.EventManager().Events(), 1)
	require.Equal(t, types.EventTypeLeaseExpired, ctx.EventManager().Events()[0].Type)
}

func TestClosedLifecycleTimestampsRemainValid(t *testing.T) {
	for _, elapsed := range []time.Duration{0, time.Hour + 500*time.Millisecond} {
		t.Run(elapsed.String(), func(t *testing.T) {
			f, _, skuUUID, _, baseTime := newExpiryFixture(t)
			k := f.App.BillingKeeper
			ms := keeper.NewMsgServerImpl(k)
			uuid := f.createAndAcknowledgeLease(t, ms, f.TestAccs[0], f.TestAccs[1],
				[]types.LeaseItemInput{{SkuUuid: skuUUID, Quantity: 1}})
			ctx := f.Ctx.WithBlockTime(baseTime.Add(elapsed))
			_, err := ms.CloseLease(ctx, &types.MsgCloseLease{Sender: f.TestAccs[0].String(), LeaseUuids: []string{uuid}})
			require.NoError(t, err)
			lease, err := k.GetLease(ctx, uuid)
			require.NoError(t, err)
			require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
			require.False(t, lease.ClosedAt.Before(lease.CreatedAt))
			require.False(t, lease.ClosedAt.Before(lease.LastSettledAt))
			genesis := k.ExportGenesis(ctx)
			require.NoError(t, genesis.ValidateCurrentState())
			require.NoError(t, genesis.Validate())
		})
	}
}

func TestCancelLeasePreservesTenantBatchValidationContract(t *testing.T) {
	t.Run("multiple providers and request order", func(t *testing.T) {
		f, _, firstSKU, _, _ := newExpiryFixture(t)
		k := f.App.BillingKeeper
		ms := keeper.NewMsgServerImpl(k)
		otherProvider := f.createTestProvider(t, f.TestAccs[3].String(), f.TestAccs[2].String())
		otherSKU := f.createTestSKU(t, otherProvider.Uuid, 3600)
		uuidOrder := make([]string, 0, 2)
		for _, skuUUID := range []string{firstSKU, otherSKU.Uuid} {
			response, err := ms.CreateLease(f.Ctx, &types.MsgCreateLease{
				Tenant: f.TestAccs[0].String(), Items: []types.LeaseItemInput{{SkuUuid: skuUUID, Quantity: 1}},
			})
			require.NoError(t, err)
			uuidOrder = append(uuidOrder, response.LeaseUuid)
		}
		uuidOrder[0], uuidOrder[1] = uuidOrder[1], uuidOrder[0]
		ctx := f.Ctx.WithEventManager(sdk.NewEventManager())
		response, err := ms.CancelLease(ctx, &types.MsgCancelLease{Tenant: f.TestAccs[0].String(), LeaseUuids: uuidOrder})
		require.NoError(t, err)
		require.Equal(t, uint64(2), response.CancelledCount)
		var eventOrder []string
		for _, event := range ctx.EventManager().Events() {
			if event.Type != types.EventTypeLeaseCancelled {
				continue
			}
			for _, attribute := range event.Attributes {
				if attribute.Key == types.AttributeKeyLeaseUUID {
					eventOrder = append(eventOrder, attribute.Value)
				}
			}
		}
		require.Equal(t, uuidOrder, eventOrder)
		for _, uuid := range uuidOrder {
			lease, err := k.GetLease(ctx, uuid)
			require.NoError(t, err)
			require.Equal(t, types.LEASE_STATE_REJECTED, lease.State)
			require.Equal(t, types.RejectionReasonCancelledByTenant, lease.RejectionReason)
			require.Empty(t, lease.Reservation.RemainingAmounts)
		}
		require.NoError(t, k.ExportGenesis(ctx).ValidateCurrentState())
	})

	t.Run("ownership precedes state and credit lookup", func(t *testing.T) {
		f, _, skuUUID, _, _ := newExpiryFixture(t)
		k := f.App.BillingKeeper
		ms := keeper.NewMsgServerImpl(k)
		uuid := f.createAndAcknowledgeLease(t, ms, f.TestAccs[0], f.TestAccs[1],
			[]types.LeaseItemInput{{SkuUuid: skuUUID, Quantity: 1}})
		require.NoError(t, k.CreditAccounts.Remove(f.Ctx, f.TestAccs[0]))
		before, err := k.GetLease(f.Ctx, uuid)
		require.NoError(t, err)
		ctx := f.Ctx.WithEventManager(sdk.NewEventManager())
		_, err = ms.CancelLease(ctx, &types.MsgCancelLease{Tenant: f.TestAccs[3].String(), LeaseUuids: []string{uuid}})
		require.ErrorIs(t, err, types.ErrUnauthorized)
		after, err := k.GetLease(ctx, uuid)
		require.NoError(t, err)
		require.Equal(t, before, after)
		require.Empty(t, ctx.EventManager().Events())
	})
}

func TestEndBlockerFailedExpirationsDoNotConsumeSuccessQuota(t *testing.T) {
	f, providerUUID, skuUUID, denom, baseTime := newExpiryFixture(t)
	k := f.App.BillingKeeper
	const failedCount = types.MaxPendingLeaseExpirationsPerBlock + 1
	const validCount = types.MaxPendingLeaseExpirationsPerBlock + 5
	for i := range failedCount {
		uuid := fmt.Sprintf("failed-expiration-%03d", i)
		f.setPendingLease(t, uuid, providerUUID, skuUUID, denom, baseTime.Add(-time.Minute))
		lease, err := k.GetLease(f.Ctx, uuid)
		require.NoError(t, err)
		// Real expiration reaches the reservation release and fails atomically.
		// The persistent bad row must not monopolize the next block's quota.
		lease.Reservation = nil
		require.NoError(t, k.SetLease(f.Ctx, lease))
	}
	for i := range validCount {
		f.setPendingLease(t, fmt.Sprintf("valid-expiration-%03d", i), providerUUID, skuUUID, denom, baseTime)
	}

	ctx := f.Ctx.WithBlockTime(baseTime.Add(61 * time.Second))
	for block, expectedExpired := range []int{types.MaxPendingLeaseExpirationsPerBlock, validCount, validCount} {
		ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
		require.NoError(t, k.EndBlocker(ctx), "block %d", block)
		for i := range validCount {
			lease, err := k.GetLease(ctx, fmt.Sprintf("valid-expiration-%03d", i))
			require.NoError(t, err)
			if i < expectedExpired {
				require.Equal(t, types.LEASE_STATE_EXPIRED, lease.State)
			} else {
				require.Equal(t, types.LEASE_STATE_PENDING, lease.State, "successful writes must remain capped")
			}
		}
		for i := range failedCount {
			lease, err := k.GetLease(ctx, fmt.Sprintf("failed-expiration-%03d", i))
			require.NoError(t, err)
			require.Equal(t, types.LEASE_STATE_PENDING, lease.State)
			require.Nil(t, lease.ExpiredAt, "failed expiration must not persist partial changes")
		}
		account, err := k.GetCreditAccount(ctx, f.TestAccs[0].String())
		require.NoError(t, err)
		require.Equal(t, uint64(failedCount+validCount-expectedExpired), account.PendingLeaseCount) //nolint:gosec // expectedExpired is bounded to [100, 105], leaving [101, 106].
	}
}

func TestUnservedLeasesCannotAccrueFromCorruptClosedAt(t *testing.T) {
	for _, state := range []types.LeaseState{types.LEASE_STATE_PENDING, types.LEASE_STATE_REJECTED, types.LEASE_STATE_EXPIRED} {
		t.Run(state.String(), func(t *testing.T) {
			f, providerUUID, skuUUID, denom, baseTime := newExpiryFixture(t)
			k := f.App.BillingKeeper
			ms := keeper.NewMsgServerImpl(k)
			created, err := ms.CreateLease(f.Ctx, &types.MsgCreateLease{
				Tenant: f.TestAccs[0].String(), Items: []types.LeaseItemInput{{SkuUuid: skuUUID, Quantity: 1}},
			})
			require.NoError(t, err)
			ctx := f.Ctx.WithBlockTime(baseTime.Add(61 * time.Second))
			switch state {
			case types.LEASE_STATE_REJECTED:
				_, err = ms.RejectLease(ctx, &types.MsgRejectLease{Sender: f.TestAccs[1].String(), LeaseUuids: []string{created.LeaseUuid}})
				require.NoError(t, err)
			case types.LEASE_STATE_EXPIRED:
				require.NoError(t, k.EndBlocker(ctx))
			}
			lease, err := k.GetLease(ctx, created.LeaseUuid)
			require.NoError(t, err)
			require.Equal(t, state, lease.State)
			require.Nil(t, lease.ClosedAt, "real lifecycle does not use closed_at for unserved leases")
			require.NoError(t, k.ExportGenesis(ctx).ValidateCurrentState())

			corruptCloseTime := ctx.BlockTime()
			lease.ClosedAt = &corruptCloseTime
			require.NoError(t, k.SetLease(ctx, lease))
			creditAddress := types.DeriveCreditAddress(f.TestAccs[0])
			balanceBefore := f.App.BankKeeper.GetBalance(ctx, creditAddress, denom)
			payoutBefore := f.App.BankKeeper.GetBalance(ctx, f.TestAccs[2], denom)
			accountBefore, err := k.GetCreditAccount(ctx, lease.Tenant)
			require.NoError(t, err)
			withdrawable, err := k.CalculateWithdrawableForLease(ctx, lease)
			require.NoError(t, err)
			require.True(t, withdrawable.IsZero())
			for _, withdrawal := range []*types.MsgWithdraw{
				{Sender: f.TestAccs[1].String(), LeaseUuids: []string{lease.Uuid}},
				{Sender: f.TestAccs[1].String(), ProviderUuid: providerUUID},
			} {
				response, err := ms.Withdraw(ctx, withdrawal)
				if withdrawal.ProviderUuid == "" {
					require.ErrorIs(t, err, types.ErrNoWithdrawableAmount)
				} else {
					require.NoError(t, err)
					require.Zero(t, response.WithdrawalCount)
					require.True(t, response.TotalAmounts.IsZero())
				}
			}
			require.Equal(t, balanceBefore, f.App.BankKeeper.GetBalance(ctx, creditAddress, denom))
			require.Equal(t, payoutBefore, f.App.BankKeeper.GetBalance(ctx, f.TestAccs[2], denom))
			accountAfter, err := k.GetCreditAccount(ctx, lease.Tenant)
			require.NoError(t, err)
			require.Equal(t, accountBefore, accountAfter)
			stored, err := k.GetLease(ctx, lease.Uuid)
			require.NoError(t, err)
			require.Equal(t, lease, stored)
		})
	}
}

func TestSettlementWithPositiveAccrualAndNoSpendableCredit(t *testing.T) {
	for _, protectOtherLease := range []bool{false, true} {
		t.Run(fmt.Sprintf("other lease protected %t", protectOtherLease), func(t *testing.T) {
			f := initFixture(t)
			k := f.App.BillingKeeper
			ms := keeper.NewMsgServerImpl(k)
			tenant := f.TestAccs[0]
			creditAddress := types.DeriveCreditAddress(tenant)
			provider := f.createTestProvider(t, f.TestAccs[1].String(), f.TestAccs[2].String())
			sku := f.createTestSKU(t, provider.Uuid, 3_600)
			baseTime := f.Ctx.BlockTime()
			// An exhausted ACTIVE allocation is valid current state (for example,
			// an allocated migration cohort awaiting its first lifecycle touch).
			lease := types.Lease{
				Uuid: "01912345-6789-7abc-8def-0123456789ab", Tenant: tenant.String(), ProviderUuid: provider.Uuid,
				Items: []types.LeaseItem{{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewInt64Coin(testDenom, 1)}},
				State: types.LEASE_STATE_ACTIVE, CreatedAt: baseTime, LastSettledAt: baseTime,
				MinLeaseDurationAtCreation: 1, Reservation: &types.LeaseReservation{},
			}
			require.NoError(t, k.SetLease(f.Ctx, lease))
			account := types.CreditAccount{Tenant: tenant.String(), CreditAddress: creditAddress.String(), ActiveLeaseCount: 1}
			var protected types.Lease
			if protectOtherLease {
				protected = lease
				protected.Uuid = "01912345-6789-7abc-8def-0123456789ac"
				protected.State = types.LEASE_STATE_PENDING
				protected.MinLeaseDurationAtCreation = 100
				protected.Reservation = &types.LeaseReservation{RemainingAmounts: sdk.NewCoins(sdk.NewInt64Coin(testDenom, 100))}
				require.NoError(t, k.SetLease(f.Ctx, protected))
				account.PendingLeaseCount = 1
				account.ReservedAmounts = sdk.NewCoins(sdk.NewInt64Coin(testDenom, 100))
				f.fundAccount(t, creditAddress, account.ReservedAmounts)
			}
			require.NoError(t, k.SetCreditAccount(f.Ctx, account))
			require.NoError(t, k.LeaseSequence.Set(f.Ctx, account.ActiveLeaseCount+account.PendingLeaseCount))
			ctx := f.Ctx.WithBlockTime(baseTime.Add(10 * time.Second)).WithEventManager(sdk.NewEventManager())
			require.NoError(t, k.ExportGenesis(ctx).ValidateCurrentState())
			creditBefore := f.App.BankKeeper.GetBalance(ctx, creditAddress, testDenom)
			payoutBefore := f.App.BankKeeper.GetBalance(ctx, f.TestAccs[2], testDenom)
			accountBefore := account.String()
			leaseBefore := lease.String()
			for _, settle := range []func() (*keeper.SettlementResult, error){
				func() (*keeper.SettlementResult, error) {
					return k.PerformSettlement(ctx, &lease, &account, ctx.BlockTime())
				},
				func() (*keeper.SettlementResult, error) {
					return k.PerformSettlementSilent(ctx, &lease, &account, ctx.BlockTime())
				},
			} {
				result, err := settle()
				require.NoError(t, err)
				require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 10)), result.AccruedAmounts)
				require.True(t, result.TransferAmounts.IsZero())
				require.Equal(t, ctx.BlockTime(), result.SettledThrough)
				require.Equal(t, accountBefore, account.String())
				require.Equal(t, leaseBefore, lease.String())
				require.Empty(t, ctx.EventManager().Events(), "zero transfer must not emit a bank transfer")
			}

			// The real close handler must finish this terminal exhaustion step,
			// advancing its cursor/count without spending another lease's credit.
			closed, err := ms.CloseLease(ctx, &types.MsgCloseLease{Sender: tenant.String(), LeaseUuids: []string{lease.Uuid}})
			require.NoError(t, err)
			require.Equal(t, uint64(1), closed.ClosedCount)
			require.True(t, closed.TotalSettledAmounts.IsZero())
			stored, err := k.GetLease(ctx, lease.Uuid)
			require.NoError(t, err)
			require.Equal(t, types.LEASE_STATE_CLOSED, stored.State)
			require.Equal(t, types.ClosureReasonCreditExhausted, stored.ClosureReason)
			require.Equal(t, ctx.BlockTime(), stored.LastSettledAt)
			require.Equal(t, creditBefore, f.App.BankKeeper.GetBalance(ctx, creditAddress, testDenom))
			require.Equal(t, payoutBefore, f.App.BankKeeper.GetBalance(ctx, f.TestAccs[2], testDenom))
			if protectOtherLease {
				storedProtected, err := k.GetLease(ctx, protected.Uuid)
				require.NoError(t, err)
				require.Equal(t, protected, storedProtected)
			}
			require.NoError(t, k.ExportGenesis(ctx).ValidateCurrentState())
		})
	}
}

func TestBillingAllowedListCannotPerformProviderOrTenantLifecycleOperations(t *testing.T) {
	s := setupCustomDomain(t)
	k := s.f.App.BillingKeeper
	ctx := s.f.Ctx.WithBlockTime(s.f.Ctx.BlockTime().Add(10 * time.Second))
	created, err := s.msgServer.CreateLeaseForTenant(ctx, &types.MsgCreateLeaseForTenant{
		Authority: s.allowed.String(), Tenant: s.tenant.String(),
		Items: []types.LeaseItemInput{{SkuUuid: s.sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err, "the allowed-list account does have delegated creation rights")
	pendingUUID := created.LeaseUuid
	before := k.ExportGenesis(ctx)
	creditBefore := s.f.App.BankKeeper.GetBalance(ctx, types.DeriveCreditAddress(s.tenant), testDenom)
	payoutBefore := s.f.App.BankKeeper.GetBalance(ctx, s.f.TestAccs[2], testDenom)
	for _, operation := range []struct {
		name string
		run  func() error
	}{
		{"acknowledge", func() error {
			_, err := s.msgServer.AcknowledgeLease(ctx, &types.MsgAcknowledgeLease{Sender: s.allowed.String(), LeaseUuids: []string{pendingUUID}})
			return err
		}},
		{"reject", func() error {
			_, err := s.msgServer.RejectLease(ctx, &types.MsgRejectLease{Sender: s.allowed.String(), LeaseUuids: []string{pendingUUID}})
			return err
		}},
		{"withdraw specific", func() error {
			_, err := s.msgServer.Withdraw(ctx, &types.MsgWithdraw{Sender: s.allowed.String(), LeaseUuids: []string{s.leaseUUID}})
			return err
		}},
		{"withdraw provider", func() error {
			_, err := s.msgServer.Withdraw(ctx, &types.MsgWithdraw{Sender: s.allowed.String(), ProviderUuid: s.provider.Uuid})
			return err
		}},
		{"close", func() error {
			_, err := s.msgServer.CloseLease(ctx, &types.MsgCloseLease{Sender: s.allowed.String(), LeaseUuids: []string{s.leaseUUID}})
			return err
		}},
		{"cancel", func() error {
			_, err := s.msgServer.CancelLease(ctx, &types.MsgCancelLease{Tenant: s.allowed.String(), LeaseUuids: []string{pendingUUID}})
			return err
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			require.ErrorIs(t, operation.run(), types.ErrUnauthorized)
			require.Equal(t, before, k.ExportGenesis(ctx), "authorization failures must not change state")
			require.Equal(t, creditBefore, s.f.App.BankKeeper.GetBalance(ctx, types.DeriveCreditAddress(s.tenant), testDenom))
			require.Equal(t, payoutBefore, s.f.App.BankKeeper.GetBalance(ctx, s.f.TestAccs[2], testDenom))
		})
	}
}

func TestMsgSetItemCustomDomainValidationAuthorizationAndState(t *testing.T) {
	s := setupCustomDomain(t)
	k := s.f.App.BillingKeeper
	ctx := s.f.Ctx.WithEventManager(sdk.NewEventManager())
	before := k.ExportGenesis(ctx)
	for _, invalid := range []types.MsgSetItemCustomDomain{
		{Sender: s.allowed.String(), LeaseUuid: "not-a-uuid", CustomDomain: "api.example.com"},
		{Sender: s.allowed.String(), LeaseUuid: s.leaseUUID, ServiceName: "INVALID", CustomDomain: "api.example.com"},
		{Sender: s.allowed.String(), LeaseUuid: s.leaseUUID, CustomDomain: "Api.Example.COM"},
		{Sender: s.providerAddr.String(), LeaseUuid: s.leaseUUID, CustomDomain: "api.example.com"},
		{Sender: s.stranger.String(), LeaseUuid: s.leaseUUID, CustomDomain: "api.example.com"},
	} {
		_, err := s.msgServer.SetItemCustomDomain(ctx, &invalid)
		require.Error(t, err)
		require.Equal(t, before, k.ExportGenesis(ctx))
		require.Empty(t, ctx.EventManager().Events())
	}

	msg := &types.MsgSetItemCustomDomain{Sender: s.allowed.String(), LeaseUuid: s.leaseUUID, CustomDomain: "api.example.com"}
	_, err := s.msgServer.SetItemCustomDomain(ctx, msg)
	require.NoError(t, err)
	lease, service, found, err := k.GetLeaseByCustomDomain(ctx, msg.CustomDomain)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, s.leaseUUID, lease.Uuid)
	require.Empty(t, service)
	event := findEvent(t, ctx, types.EventTypeLeaseCustomDomainSet)
	require.Equal(t, types.AttributeValueRoleAllowed, attrValue(t, event, types.AttributeKeySetBy))
	require.Equal(t, msg.CustomDomain, attrValue(t, event, types.AttributeKeyCustomDomain))

	ctx = ctx.WithEventManager(sdk.NewEventManager())
	msg.CustomDomain = ""
	_, err = s.msgServer.SetItemCustomDomain(ctx, msg)
	require.NoError(t, err)
	_, _, found, err = k.GetLeaseByCustomDomain(ctx, "api.example.com")
	require.NoError(t, err)
	require.False(t, found)
	event = findEvent(t, ctx, types.EventTypeLeaseCustomDomainCleared)
	require.Equal(t, "api.example.com", attrValue(t, event, types.AttributeKeyCustomDomain))
	require.NoError(t, k.ExportGenesis(ctx).ValidateCurrentState())
}
