package keeper_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

const reservationRuntimeSKUUUID2 = "01912345-6789-7abc-8def-0123456789af"

func requireReservationRuntimeCoinsEqual(t *testing.T, want, got sdk.Coins) {
	t.Helper()
	require.True(t, want.Equal(got), "expected %s, got %s", want, got)
}

// TestReservationSettlementOwnFirstProtectsSiblingLease exercises the complete
// message path with two leases sharing one tenant account. The first lease may
// spend its own reservation plus genuinely unreserved credit, but it must not
// consume either denomination owned by the sibling lease.
func TestReservationSettlementOwnFirstProtectsSiblingLease(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	msgServer := keeper.NewMsgServerImpl(k)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	creditAddr := types.DeriveCreditAddress(tenant)

	// Each lease owns 100 umfx and 200 upwr. The account also has 50 umfx and
	// 70 upwr of unreserved credit. After 150 seconds, lease A has accrued 150
	// umfx and 300 upwr, so its transfer is capped at 150/270 while lease B's
	// 100/200 allocation remains fully backed.
	leaseAllocation := sdk.NewCoins(
		sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
		sdk.NewCoin(testDenom2, sdkmath.NewInt(200)),
	)
	totalReserved := sdk.NewCoins(
		sdk.NewCoin(testDenom, sdkmath.NewInt(200)),
		sdk.NewCoin(testDenom2, sdkmath.NewInt(400)),
	)
	creditBalance := sdk.NewCoins(
		sdk.NewCoin(testDenom, sdkmath.NewInt(250)),
		sdk.NewCoin(testDenom2, sdkmath.NewInt(470)),
	)
	f.fundAccount(t, creditAddr, creditBalance)

	now := f.Ctx.BlockTime()
	items := []types.LeaseItem{
		{
			SkuUuid:     testSKUUUID,
			Quantity:    1,
			LockedPrice: sdk.NewCoin(testDenom, sdkmath.OneInt()),
		},
		{
			SkuUuid:     reservationRuntimeSKUUUID2,
			Quantity:    1,
			LockedPrice: sdk.NewCoin(testDenom2, sdkmath.NewInt(2)),
		},
	}
	leaseA := types.Lease{
		Uuid:                       testLeaseUUID1,
		Tenant:                     tenant.String(),
		ProviderUuid:               provider.Uuid,
		Items:                      items,
		State:                      types.LEASE_STATE_ACTIVE,
		CreatedAt:                  now,
		LastSettledAt:              now,
		MinLeaseDurationAtCreation: 100,
		Reservation: &types.LeaseReservation{
			RemainingAmounts: append(sdk.Coins(nil), leaseAllocation...),
		},
	}
	leaseB := leaseA
	leaseB.Uuid = testLeaseUUID2
	leaseB.Reservation = &types.LeaseReservation{
		RemainingAmounts: append(sdk.Coins(nil), leaseAllocation...),
	}
	require.NoError(t, k.SetLease(f.Ctx, leaseA))
	require.NoError(t, k.SetLease(f.Ctx, leaseB))
	require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:           tenant.String(),
		CreditAddress:    creditAddr.String(),
		ActiveLeaseCount: 2,
		ReservedAmounts:  totalReserved,
	}))

	settleCtx := f.Ctx.WithBlockTime(now.Add(150 * time.Second))
	response, err := msgServer.Withdraw(settleCtx, &types.MsgWithdraw{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{leaseA.Uuid},
	})
	require.NoError(t, err)
	require.Equal(t, uint64(1), response.WithdrawalCount)
	requireReservationRuntimeCoinsEqual(t, sdk.NewCoins(
		sdk.NewCoin(testDenom, sdkmath.NewInt(150)),
		sdk.NewCoin(testDenom2, sdkmath.NewInt(270)),
	), response.TotalAmounts)

	account, err := k.GetCreditAccount(settleCtx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(1), account.ActiveLeaseCount)
	requireReservationRuntimeCoinsEqual(t, leaseAllocation, account.ReservedAmounts)
	require.True(t, account.UnattributedReservedAmounts.IsZero())

	storedA, err := k.GetLease(settleCtx, leaseA.Uuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, storedA.State)
	require.NotNil(t, storedA.Reservation)
	require.True(t, storedA.Reservation.RemainingAmounts.IsZero())

	storedB, err := k.GetLease(settleCtx, leaseB.Uuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, storedB.State)
	require.NotNil(t, storedB.Reservation)
	requireReservationRuntimeCoinsEqual(t, leaseAllocation, storedB.Reservation.RemainingAmounts)

	requireReservationRuntimeCoinsEqual(t, leaseAllocation,
		f.App.BankKeeper.GetAllBalances(settleCtx, creditAddr))
	requireReservationRuntimeCoinsEqual(t, response.TotalAmounts,
		f.App.BankKeeper.GetAllBalances(settleCtx, payoutAddr))
}

func TestReservationSettlementPartiallyReducesAggregateAndLeaseTranche(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	creditAddr := types.DeriveCreditAddress(tenant)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(200))))

	now := f.Ctx.BlockTime()
	lease := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{{
			SkuUuid:     testSKUUUID,
			Quantity:    1,
			LockedPrice: sdk.NewCoin(testDenom, sdkmath.OneInt()),
		}},
		State:                      types.LEASE_STATE_ACTIVE,
		CreatedAt:                  now,
		LastSettledAt:              now,
		MinLeaseDurationAtCreation: 100,
		Reservation: &types.LeaseReservation{RemainingAmounts: sdk.NewCoins(
			sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
		)},
	}
	account := types.CreditAccount{
		Tenant:           tenant.String(),
		CreditAddress:    creditAddr.String(),
		ActiveLeaseCount: 2,
		ReservedAmounts: sdk.NewCoins(
			sdk.NewCoin(testDenom, sdkmath.NewInt(200)),
		),
	}

	result, err := k.PerformSettlement(f.Ctx, &lease, &account, now.Add(40*time.Second))
	require.NoError(t, err)
	requireReservationRuntimeCoinsEqual(t,
		sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(40))),
		result.TransferAmounts,
	)
	requireReservationRuntimeCoinsEqual(t,
		sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(160))),
		account.ReservedAmounts,
	)
	requireReservationRuntimeCoinsEqual(t,
		sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(60))),
		lease.Reservation.RemainingAmounts,
	)
	require.Equal(t, sdkmath.NewInt(160),
		f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom).Amount)
}

func TestLegacyReservationSettlementConsumesSharedCohortWithoutChangingMembership(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	creditAddr := types.DeriveCreditAddress(tenant)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 150)))

	now := f.Ctx.BlockTime()
	lease := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{{
			SkuUuid:     testSKUUUID,
			Quantity:    1,
			LockedPrice: sdk.NewInt64Coin(testDenom, 1),
		}},
		State:         types.LEASE_STATE_ACTIVE,
		CreatedAt:     now,
		LastSettledAt: now,
		// A zero creation duration identifies a historical lease. Its wrapper is
		// present but empty; the account owns the shared compatibility cohort.
		Reservation: &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
	}
	cohort := sdk.NewCoins(sdk.NewInt64Coin(testDenom, 100))
	account := types.CreditAccount{
		Tenant:                      tenant.String(),
		CreditAddress:               creditAddr.String(),
		ActiveLeaseCount:            2,
		ReservedAmounts:             append(sdk.Coins(nil), cohort...),
		UnattributedReservedAmounts: append(sdk.Coins(nil), cohort...),
		UnattributedLeaseCount:      2,
	}

	result, err := k.PerformSettlement(f.Ctx, &lease, &account, now.Add(40*time.Second))
	require.NoError(t, err)
	wantRemaining := sdk.NewCoins(sdk.NewInt64Coin(testDenom, 60))
	requireReservationRuntimeCoinsEqual(t,
		sdk.NewCoins(sdk.NewInt64Coin(testDenom, 40)),
		result.TransferAmounts,
	)
	requireReservationRuntimeCoinsEqual(t, wantRemaining, account.ReservedAmounts)
	requireReservationRuntimeCoinsEqual(t, wantRemaining, account.UnattributedReservedAmounts)
	require.Equal(t, uint64(2), account.UnattributedLeaseCount,
		"settlement consumes cohort value but must not change live cohort membership")
	require.NotNil(t, lease.Reservation)
	require.True(t, lease.Reservation.RemainingAmounts.IsZero())
	require.Equal(t, sdkmath.NewInt(110),
		f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom).Amount)
	require.Equal(t, sdkmath.NewInt(40),
		f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom).Amount)
}

func TestModernSettlementCannotConsumeUnattributedReservation(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	creditAddr := types.DeriveCreditAddress(tenant)
	reservation := sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100)))
	f.fundAccount(t, creditAddr, reservation)

	now := f.Ctx.BlockTime()
	lease := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{{
			SkuUuid:     testSKUUUID,
			Quantity:    1,
			LockedPrice: sdk.NewCoin(testDenom, sdkmath.OneInt()),
		}},
		State:                      types.LEASE_STATE_ACTIVE,
		CreatedAt:                  now,
		LastSettledAt:              now,
		MinLeaseDurationAtCreation: 100,
		Reservation: &types.LeaseReservation{
			RemainingAmounts: append(sdk.Coins(nil), reservation...),
		},
	}
	account := types.CreditAccount{
		Tenant:                      tenant.String(),
		CreditAddress:               creditAddr.String(),
		ActiveLeaseCount:            1,
		ReservedAmounts:             append(sdk.Coins(nil), reservation...),
		UnattributedReservedAmounts: append(sdk.Coins(nil), reservation...),
		UnattributedLeaseCount:      1,
	}

	_, err := k.PerformSettlement(f.Ctx, &lease, &account, now.Add(40*time.Second))
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrReservationInvariant)
	requireReservationRuntimeCoinsEqual(t, reservation, account.ReservedAmounts)
	requireReservationRuntimeCoinsEqual(t, reservation, account.UnattributedReservedAmounts)
	requireReservationRuntimeCoinsEqual(t, reservation, lease.Reservation.RemainingAmounts)
	require.Equal(t, sdkmath.NewInt(100),
		f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom).Amount)
	require.True(t, f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom).Amount.IsZero())
}

func TestModernReleaseCannotSubtractUnattributedReservation(t *testing.T) {
	f := initFixture(t)
	tenant := f.TestAccs[0]
	reservation := sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100)))
	account := types.CreditAccount{
		Tenant:                      tenant.String(),
		CreditAddress:               types.DeriveCreditAddress(tenant).String(),
		ReservedAmounts:             append(sdk.Coins(nil), reservation...),
		UnattributedReservedAmounts: append(sdk.Coins(nil), reservation...),
		UnattributedLeaseCount:      1,
	}
	lease := types.Lease{
		Uuid:                       testLeaseUUID1,
		Tenant:                     tenant.String(),
		State:                      types.LEASE_STATE_CLOSED,
		MinLeaseDurationAtCreation: 100,
		Reservation: &types.LeaseReservation{
			RemainingAmounts: append(sdk.Coins(nil), reservation...),
		},
	}

	err := f.App.BillingKeeper.ReleaseLeaseReservation(f.Ctx, &account, &lease)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrReservationInvariant)
	requireReservationRuntimeCoinsEqual(t, reservation, account.ReservedAmounts)
	requireReservationRuntimeCoinsEqual(t, reservation, account.UnattributedReservedAmounts)
	requireReservationRuntimeCoinsEqual(t, reservation, lease.Reservation.RemainingAmounts)
}

func TestReservationSpecificBatchCarriesForwardSameTenantMutations(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	msgServer := keeper.NewMsgServerImpl(k)
	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	creditAddr := types.DeriveCreditAddress(tenant)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(250))))

	now := f.Ctx.BlockTime()
	initialAllocation := sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100)))
	lease := func(uuid string) types.Lease {
		return types.Lease{
			Uuid:         uuid,
			Tenant:       tenant.String(),
			ProviderUuid: provider.Uuid,
			Items: []types.LeaseItem{{
				SkuUuid:     testSKUUUID,
				Quantity:    1,
				LockedPrice: sdk.NewCoin(testDenom, sdkmath.OneInt()),
			}},
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  now,
			LastSettledAt:              now,
			MinLeaseDurationAtCreation: 100,
			Reservation: &types.LeaseReservation{
				RemainingAmounts: append(sdk.Coins(nil), initialAllocation...),
			},
		}
	}
	leaseA := lease(testLeaseUUID1)
	leaseB := lease(testLeaseUUID2)
	require.NoError(t, k.SetLease(f.Ctx, leaseA))
	require.NoError(t, k.SetLease(f.Ctx, leaseB))
	require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:           tenant.String(),
		CreditAddress:    creditAddr.String(),
		ActiveLeaseCount: 2,
		ReservedAmounts: sdk.NewCoins(
			sdk.NewCoin(testDenom, sdkmath.NewInt(200)),
		),
	}))

	settleCtx := f.Ctx.WithBlockTime(now.Add(60 * time.Second))
	response, err := msgServer.Withdraw(settleCtx, &types.MsgWithdraw{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{leaseA.Uuid, leaseB.Uuid},
	})
	require.NoError(t, err)
	require.Equal(t, uint64(2), response.WithdrawalCount)
	requireReservationRuntimeCoinsEqual(t,
		sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(120))),
		response.TotalAmounts,
	)

	account, err := k.GetCreditAccount(settleCtx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(2), account.ActiveLeaseCount)
	requireReservationRuntimeCoinsEqual(t,
		sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(80))),
		account.ReservedAmounts,
	)
	for _, uuid := range []string{leaseA.Uuid, leaseB.Uuid} {
		stored, err := k.GetLease(settleCtx, uuid)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_ACTIVE, stored.State)
		require.Equal(t, settleCtx.BlockTime(), stored.LastSettledAt)
		require.NotNil(t, stored.Reservation)
		requireReservationRuntimeCoinsEqual(t,
			sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(40))),
			stored.Reservation.RemainingAmounts,
		)
	}
	require.Equal(t, sdkmath.NewInt(130),
		f.App.BankKeeper.GetBalance(settleCtx, creditAddr, testDenom).Amount)
	require.Equal(t, sdkmath.NewInt(120),
		f.App.BankKeeper.GetBalance(settleCtx, payoutAddr, testDenom).Amount)
}

func TestReservationLifecycleReleasesExactRemainingTranche(t *testing.T) {
	tests := []struct {
		name                string
		transition          string
		wantState           types.LeaseState
		wantCreditBalance   int64
		wantProviderBalance int64
	}{
		{
			name:                "close after partial accrual",
			transition:          "close",
			wantState:           types.LEASE_STATE_CLOSED,
			wantCreditBalance:   210,
			wantProviderBalance: 40,
		},
		{
			name:                "provider rejection",
			transition:          "reject",
			wantState:           types.LEASE_STATE_REJECTED,
			wantCreditBalance:   250,
			wantProviderBalance: 0,
		},
		{
			name:                "tenant cancellation",
			transition:          "cancel",
			wantState:           types.LEASE_STATE_REJECTED,
			wantCreditBalance:   250,
			wantProviderBalance: 0,
		},
		{
			name:                "pending expiration",
			transition:          "expire",
			wantState:           types.LEASE_STATE_EXPIRED,
			wantCreditBalance:   250,
			wantProviderBalance: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			k := f.App.BillingKeeper
			msgServer := keeper.NewMsgServerImpl(k)
			tenant := f.TestAccs[0]
			providerAddr := f.TestAccs[1]
			payoutAddr := f.TestAccs[2]

			params := types.DefaultParams()
			params.MinLeaseDuration = 100
			params.PendingTimeout = 60
			require.NoError(t, k.SetParams(f.Ctx, params))

			provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
			sku := f.createTestSKU(t, provider.Uuid, 3600)
			fundAmount := sdk.NewCoin(testDenom, sdkmath.NewInt(250))
			f.fundAccount(t, tenant, sdk.NewCoins(fundAmount))
			_, err := msgServer.FundCredit(f.Ctx, &types.MsgFundCredit{
				Sender: tenant.String(),
				Tenant: tenant.String(),
				Amount: fundAmount,
			})
			require.NoError(t, err)

			created, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
				Tenant: tenant.String(),
				Items: []types.LeaseItemInput{{
					SkuUuid:  sku.Uuid,
					Quantity: 1,
				}},
			})
			require.NoError(t, err)

			createdLease, err := k.GetLease(f.Ctx, created.LeaseUuid)
			require.NoError(t, err)
			require.NotNil(t, createdLease.Reservation)
			requireReservationRuntimeCoinsEqual(t,
				sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100))),
				createdLease.Reservation.RemainingAmounts,
			)

			transitionCtx := f.Ctx
			switch tc.transition {
			case "close":
				_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
					Sender:     providerAddr.String(),
					LeaseUuids: []string{created.LeaseUuid},
				})
				require.NoError(t, err)
				transitionCtx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(40 * time.Second))
				_, err = msgServer.CloseLease(transitionCtx, &types.MsgCloseLease{
					Sender:     tenant.String(),
					LeaseUuids: []string{created.LeaseUuid},
				})
			case "reject":
				_, err = msgServer.RejectLease(transitionCtx, &types.MsgRejectLease{
					Sender:     providerAddr.String(),
					LeaseUuids: []string{created.LeaseUuid},
					Reason:     "capacity unavailable",
				})
			case "cancel":
				_, err = msgServer.CancelLease(transitionCtx, &types.MsgCancelLease{
					Tenant:     tenant.String(),
					LeaseUuids: []string{created.LeaseUuid},
				})
			case "expire":
				transitionCtx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(61 * time.Second))
				err = k.EndBlocker(transitionCtx)
			default:
				t.Fatalf("unknown transition %q", tc.transition)
			}
			require.NoError(t, err)

			account, err := k.GetCreditAccount(transitionCtx, tenant.String())
			require.NoError(t, err)
			require.True(t, account.ReservedAmounts.IsZero())
			require.True(t, account.UnattributedReservedAmounts.IsZero())

			stored, err := k.GetLease(transitionCtx, created.LeaseUuid)
			require.NoError(t, err)
			require.Equal(t, tc.wantState, stored.State)
			require.NotNil(t, stored.Reservation)
			require.True(t, stored.Reservation.RemainingAmounts.IsZero())

			creditAddr := types.DeriveCreditAddress(tenant)
			require.Equal(t, sdkmath.NewInt(tc.wantCreditBalance),
				f.App.BankKeeper.GetBalance(transitionCtx, creditAddr, testDenom).Amount)
			require.Equal(t, sdkmath.NewInt(tc.wantProviderBalance),
				f.App.BankKeeper.GetBalance(transitionCtx, payoutAddr, testDenom).Amount)
		})
	}
}

func TestLegacyUnattributedReservationReleasedOnlyAfterLastLiveLease(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	creditAddr := types.DeriveCreditAddress(tenant)
	cohort := sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100)))
	f.fundAccount(t, creditAddr, cohort)
	now := f.Ctx.BlockTime()

	legacyLease := func(uuid string) types.Lease {
		return types.Lease{
			Uuid:         uuid,
			Tenant:       tenant.String(),
			ProviderUuid: provider.Uuid,
			Items: []types.LeaseItem{{
				SkuUuid:     testSKUUUID,
				Quantity:    1,
				LockedPrice: sdk.NewCoin(testDenom, sdkmath.OneInt()),
			}},
			State:         types.LEASE_STATE_ACTIVE,
			CreatedAt:     now,
			LastSettledAt: now,
			Reservation:   &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
		}
	}
	leaseA := legacyLease(testLeaseUUID1)
	leaseB := legacyLease(testLeaseUUID2)
	require.NoError(t, k.SetLease(f.Ctx, leaseA))
	require.NoError(t, k.SetLease(f.Ctx, leaseB))
	account := types.CreditAccount{
		Tenant:                      tenant.String(),
		CreditAddress:               creditAddr.String(),
		ActiveLeaseCount:            2,
		ReservedAmounts:             append(sdk.Coins(nil), cohort...),
		UnattributedReservedAmounts: append(sdk.Coins(nil), cohort...),
		UnattributedLeaseCount:      2,
	}
	require.NoError(t, k.SetCreditAccount(f.Ctx, account))

	leaseA.State = types.LEASE_STATE_CLOSED
	require.NoError(t, k.ReleaseLeaseReservation(f.Ctx, &account, &leaseA))
	requireReservationRuntimeCoinsEqual(t, cohort, account.ReservedAmounts)
	requireReservationRuntimeCoinsEqual(t, cohort, account.UnattributedReservedAmounts)
	require.Equal(t, uint64(1), account.UnattributedLeaseCount)
	require.NoError(t, k.SetLease(f.Ctx, leaseA))

	leaseB.State = types.LEASE_STATE_CLOSED
	require.NoError(t, k.ReleaseLeaseReservation(f.Ctx, &account, &leaseB))
	require.True(t, account.ReservedAmounts.IsZero())
	require.True(t, account.UnattributedReservedAmounts.IsZero())
	require.Zero(t, account.UnattributedLeaseCount)
}

func TestLegacyUnattributedLeaseCountTracksEmptyCohort(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	tenant := f.TestAccs[0]
	account := types.CreditAccount{
		Tenant:                 tenant.String(),
		CreditAddress:          types.DeriveCreditAddress(tenant).String(),
		UnattributedLeaseCount: 2,
	}
	legacyLease := func(uuid string) types.Lease {
		return types.Lease{
			Uuid:        uuid,
			Tenant:      tenant.String(),
			State:       types.LEASE_STATE_CLOSED,
			Reservation: &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
		}
	}

	first := legacyLease(testLeaseUUID1)
	require.NoError(t, k.ReleaseLeaseReservation(f.Ctx, &account, &first))
	require.Equal(t, uint64(1), account.UnattributedLeaseCount)
	require.True(t, account.ReservedAmounts.IsZero())
	require.True(t, account.UnattributedReservedAmounts.IsZero())

	second := legacyLease(testLeaseUUID2)
	require.NoError(t, k.ReleaseLeaseReservation(f.Ctx, &account, &second))
	require.Zero(t, account.UnattributedLeaseCount)
}

func TestLegacyReservationReleaseRejectsMissingCohortCount(t *testing.T) {
	f := initFixture(t)
	tenant := f.TestAccs[0]
	account := types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: types.DeriveCreditAddress(tenant).String(),
	}
	lease := types.Lease{
		Uuid:        testLeaseUUID1,
		Tenant:      tenant.String(),
		State:       types.LEASE_STATE_CLOSED,
		Reservation: &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
	}

	err := f.App.BillingKeeper.ReleaseLeaseReservation(f.Ctx, &account, &lease)
	require.ErrorIs(t, err, types.ErrReservationInvariant)
	require.ErrorContains(t, err, "not represented in the unattributed lease count")
	require.Zero(t, account.UnattributedLeaseCount)
}

func TestReservationSettlementBankFailureRollsBackState(t *testing.T) {
	f := initFixture(t)
	k := f.App.BillingKeeper
	msgServer := keeper.NewMsgServerImpl(k)
	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	creditAddr := types.DeriveCreditAddress(tenant)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(200))))

	now := f.Ctx.BlockTime()
	reservation := sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100)))
	lease := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{{
			SkuUuid:     testSKUUUID,
			Quantity:    1,
			LockedPrice: sdk.NewCoin(testDenom, sdkmath.OneInt()),
		}},
		State:                      types.LEASE_STATE_ACTIVE,
		CreatedAt:                  now,
		LastSettledAt:              now,
		MinLeaseDurationAtCreation: 100,
		Reservation: &types.LeaseReservation{
			RemainingAmounts: append(sdk.Coins(nil), reservation...),
		},
	}
	require.NoError(t, k.SetLease(f.Ctx, lease))
	require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:           tenant.String(),
		CreditAddress:    creditAddr.String(),
		ActiveLeaseCount: 1,
		ReservedAmounts:  append(sdk.Coins(nil), reservation...),
	}))

	// Install the failure only after arranging the fixture so the test reaches
	// the bank write with otherwise-valid settlement state.
	f.App.BankKeeper.AppendSendRestriction(func(
		_ context.Context,
		fromAddr, toAddr sdk.AccAddress,
		_ sdk.Coins,
	) (sdk.AccAddress, error) {
		if fromAddr.Equals(creditAddr) && toAddr.Equals(payoutAddr) {
			return toAddr, errors.New("forced settlement send failure")
		}
		return toAddr, nil
	})
	defer f.App.BankKeeper.ClearSendRestriction()

	settleCtx := f.Ctx.WithBlockTime(now.Add(40 * time.Second))
	_, err := msgServer.Withdraw(settleCtx, &types.MsgWithdraw{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{lease.Uuid},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidCreditOperation)

	storedAccount, err := k.GetCreditAccount(settleCtx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(1), storedAccount.ActiveLeaseCount)
	requireReservationRuntimeCoinsEqual(t, reservation, storedAccount.ReservedAmounts)

	storedLease, err := k.GetLease(settleCtx, lease.Uuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, storedLease.State)
	require.Equal(t, now, storedLease.LastSettledAt)
	require.NotNil(t, storedLease.Reservation)
	requireReservationRuntimeCoinsEqual(t, reservation, storedLease.Reservation.RemainingAmounts)
	require.Equal(t, sdkmath.NewInt(200),
		f.App.BankKeeper.GetBalance(settleCtx, creditAddr, testDenom).Amount)
	require.True(t,
		f.App.BankKeeper.GetBalance(settleCtx, payoutAddr, testDenom).Amount.IsZero())
}
