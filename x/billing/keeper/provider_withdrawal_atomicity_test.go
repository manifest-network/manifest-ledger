package keeper_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestProviderLeaseWithdrawalLateFailurePreservesCaller(t *testing.T) {
	for _, failure := range []string{"lease persistence", "credit account persistence"} {
		t.Run(failure, func(t *testing.T) {
			s, lease := setupReservedProviderWithdrawal(t)
			f, k := s.f, s.f.App.BillingKeeper
			expectedError := types.ErrInternalCorruption
			switch failure {
			case "lease persistence":
				const domain = "withdrawal-atomicity.example.com"
				lease.Items[0].CustomDomain = domain
				require.NoError(t, k.SetLease(f.Ctx, lease))
				key, err := collections.EncodeKeyWithPrefix(types.CustomDomainIndexKey.Bytes(), collections.StringKey, domain)
				require.NoError(t, err)
				f.Ctx.KVStore(f.App.GetKey(types.StoreKey)).Set(key, []byte{0xff})
			case "credit account persistence":
				account := f.creditAccountForLease(t, &lease)
				account.CreditAddress = s.stranger.String()
				// Bypass admission to model a corrupt stored address. Settlement
				// derives its bank address from the tenant and succeeds first;
				// SetCreditAccount rejects this only after the lease write.
				require.NoError(t, k.CreditAccounts.Set(f.Ctx, s.tenant, *account))
				expectedError = types.ErrInvalidCreditOperation
			}
			before := lease.String()
			reservationAlias := lease.Reservation
			reservationBefore := reservationAlias.String()
			storesBefore := snapshotPayoutStores(t, f, f.Ctx)
			creditAddress := types.DeriveCreditAddress(s.tenant)
			balanceBefore := f.App.BankKeeper.GetBalance(f.Ctx, creditAddress, testDenom)
			cacheCtx, _ := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100*time.Second + 500*time.Millisecond)).CacheContext()

			transfer, counted, autoClosed, err := keeper.ExecuteProviderLeaseWithdrawalForTesting(cacheCtx, &k, &lease)
			require.ErrorIs(t, err, expectedError)
			require.ErrorContains(t, err, "persist")
			require.Empty(t, transfer)
			require.False(t, counted)
			require.False(t, autoClosed)
			require.Equal(t, before, lease.String(), "failed persistence must preserve the caller's accrual cursor and remaining reservation")
			require.Same(t, reservationAlias, lease.Reservation)
			require.Equal(t, reservationBefore, reservationAlias.String(), "a shallow lease copy must not mutate the shared reservation")
			require.Equal(t, balanceBefore.Sub(sdk.NewInt64Coin(testDenom, 100)), f.App.BankKeeper.GetBalance(cacheCtx, creditAddress, testDenom), "the failure must follow a real settlement that consumes reserved funds")
			require.Equal(t, storesBefore, snapshotPayoutStores(t, f, f.Ctx), "the caller discards the failed lease cache")
		})
	}
}

func TestProviderLeaseWithdrawalSuccessPreservesLifecycleAndCacheOwnership(t *testing.T) {
	for _, autoClose := range []bool{false, true} {
		name, elapsed := "live settlement", 100*time.Second+500*time.Millisecond
		if autoClose {
			name, elapsed = "auto close", 2*time.Hour
		}
		t.Run(name, func(t *testing.T) {
			s, lease := setupReservedProviderWithdrawal(t)
			f, k := s.f, s.f.App.BillingKeeper
			lease.Tenant = strings.ToUpper(lease.Tenant)
			reservationAlias := lease.Reservation
			reservationBefore := reservationAlias.String()
			initialReservation := reservationAlias.RemainingAmounts
			storesBefore := snapshotPayoutStores(t, f, f.Ctx)
			blockTime := f.Ctx.BlockTime().Add(elapsed)
			cacheCtx, write := f.Ctx.WithBlockTime(blockTime).CacheContext()
			wantedTransfer := sdk.NewCoins(sdk.NewInt64Coin(testDenom, 100))
			wantedCursor := lease.LastSettledAt.Add(100 * time.Second)
			wantedState := types.LEASE_STATE_ACTIVE
			wantedCount := uint64(1)
			if autoClose {
				wantedTransfer, wantedCursor = initialReservation, blockTime
				wantedState, wantedCount = types.LEASE_STATE_CLOSED, 0
			}

			transfer, counted, autoClosed, err := keeper.ExecuteProviderLeaseWithdrawalForTesting(cacheCtx, &k, &lease)
			require.NoError(t, err)
			require.Equal(t, wantedTransfer, transfer)
			require.True(t, counted)
			require.Equal(t, autoClose, autoClosed)
			require.Equal(t, wantedState, lease.State)
			require.Equal(t, wantedCursor, lease.LastSettledAt)
			require.Equal(t, initialReservation.Sub(transfer...).String(), lease.Reservation.RemainingAmounts.String())
			require.Equal(t, strings.ToUpper(s.tenant.String()), lease.Tenant)
			require.Equal(t, reservationBefore, reservationAlias.String())
			require.Equal(t, storesBefore, snapshotPayoutStores(t, f, f.Ctx), "success must not commit the caller's cache")
			if autoClose {
				require.Equal(t, blockTime, *lease.ClosedAt)
				require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason)
			} else {
				require.Nil(t, lease.ClosedAt)
				require.Equal(t, 500*time.Millisecond, blockTime.Sub(lease.LastSettledAt))
			}

			write()
			storedLease, err := k.GetLease(f.Ctx, lease.Uuid)
			require.NoError(t, err)
			lease.Tenant = s.tenant.String()
			require.Equal(t, lease.String(), storedLease.String())
			account, err := k.GetCreditAccount(f.Ctx, s.tenant.String())
			require.NoError(t, err)
			require.Equal(t, wantedCount, account.ActiveLeaseCount)
			require.Equal(t, lease.Reservation.RemainingAmounts.String(), account.ReservedAmounts.String())
			require.NoError(t, k.ExportGenesis(f.Ctx.WithBlockTime(blockTime)).ValidateCurrentState())
		})
	}
}

// Leave only the admitted reservation in the credit address so a live payout
// consumes its reservation, exercising aliases as well as the accrual cursor.
func setupReservedProviderWithdrawal(t *testing.T) (*customDomainSetup, types.Lease) {
	t.Helper()
	s := setupCustomDomain(t)
	lease, err := s.f.App.BillingKeeper.GetLease(s.f.Ctx, s.leaseUUID)
	require.NoError(t, err)
	creditAddress := types.DeriveCreditAddress(s.tenant)
	balance := s.f.App.BankKeeper.GetBalance(s.f.Ctx, creditAddress, testDenom)
	excess := balance.Sub(sdk.NewCoin(testDenom, lease.Reservation.RemainingAmounts.AmountOf(testDenom)))
	require.NoError(t, s.f.App.BankKeeper.SendCoins(s.f.Ctx, creditAddress, s.stranger, sdk.NewCoins(excess)))
	return s, lease
}
