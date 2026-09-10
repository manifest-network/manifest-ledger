package keeper_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestAutoCloseLeaseFailureLeavesCallerUnchanged(t *testing.T) {
	for _, failure := range []string{"settlement", "lease persistence", "credit account persistence"} {
		t.Run(failure, func(t *testing.T) {
			s := setupCustomDomain(t)
			f, k := s.f, s.f.App.BillingKeeper
			const domain = "auto-close-atomicity.example.com"
			_, err := k.SetItemCustomDomain(f.Ctx, s.tenant.String(), s.leaseUUID, "", domain)
			require.NoError(t, err)
			lease, err := k.GetLease(f.Ctx, s.leaseUUID)
			require.NoError(t, err)
			account := f.creditAccountForLease(t, &lease)
			closeTime := f.Ctx.BlockTime().Add(200_000_000 * time.Second)
			expectedError := types.ErrReservationInvariant
			switch failure {
			case "settlement":
				account.ReservedAmounts = sdk.NewCoins()
			case "lease persistence":
				key, err := collections.EncodeKeyWithPrefix(types.CustomDomainIndexKey.Bytes(), collections.StringKey, domain)
				require.NoError(t, err)
				f.Ctx.KVStore(f.App.GetKey(types.StoreKey)).Set(key, []byte{0xff})
				expectedError = types.ErrInternalCorruption
			case "credit account persistence":
				// Settlement derives its bank address from the tenant. This bad
				// snapshot fails only after settlement and lease persistence.
				account.CreditAddress = s.stranger.String()
				expectedError = types.ErrInvalidCreditOperation
			}
			leaseBefore, accountBefore := lease.String(), account.String()
			reservationAlias := lease.Reservation
			reservationBefore := reservationAlias.String()
			accountCoinsAlias := account.ReservedAmounts
			accountCoinsBefore := accountCoinsAlias.String()
			storesBefore := snapshotPayoutStores(t, f, f.Ctx)
			cacheCtx, _ := f.Ctx.WithBlockTime(closeTime).CacheContext()

			result, err := k.AutoCloseLease(cacheCtx, &lease, account, closeTime)
			require.ErrorIs(t, err, expectedError)
			require.Nil(t, result)
			require.Equal(t, leaseBefore, lease.String())
			require.Equal(t, accountBefore, account.String())
			require.Same(t, reservationAlias, lease.Reservation)
			require.Equal(t, reservationBefore, reservationAlias.String(), "a shallow lease copy must not mutate its shared reservation wrapper")
			require.Equal(t, accountCoinsBefore, accountCoinsAlias.String())
			require.Equal(t, storesBefore, snapshotPayoutStores(t, f, f.Ctx), "discarding the caller's cache must preserve committed stores")
			if failure != "settlement" {
				creditAddress := types.DeriveCreditAddress(s.tenant)
				require.True(t, f.App.BankKeeper.GetBalance(cacheCtx, creditAddress, testDenom).IsZero(), "late failure must occur after a real settlement transfer in the discarded cache")
			}
		})
	}
}

func TestAutoCloseLeaseSuccessUpdatesCallerAndPreservesCacheOwnership(t *testing.T) {
	s := setupCustomDomain(t)
	f, k := s.f, s.f.App.BillingKeeper
	lease, err := k.GetLease(f.Ctx, s.leaseUUID)
	require.NoError(t, err)
	account := f.creditAccountForLease(t, &lease)
	// Equivalent SDK spellings remain accepted; SetLease/SetCreditAccount
	// canonicalize stored values without changing the caller's address fields.
	lease.Tenant = strings.ToUpper(lease.Tenant)
	account.Tenant = strings.ToUpper(account.Tenant)
	account.CreditAddress = strings.ToUpper(account.CreditAddress)
	reservationAlias := lease.Reservation
	reservationBefore := reservationAlias.String()
	accountCoinsAlias := account.ReservedAmounts
	accountCoinsBefore := accountCoinsAlias.String()
	storesBefore := snapshotPayoutStores(t, f, f.Ctx)
	closeTime := f.Ctx.BlockTime().Add(200_000_000 * time.Second)
	cacheCtx, write := f.Ctx.WithBlockTime(closeTime).CacheContext()
	shouldClose, checkedCloseTime, err := k.ShouldAutoCloseLease(cacheCtx, &lease, account)
	require.NoError(t, err)
	require.True(t, shouldClose)
	require.Equal(t, closeTime, checkedCloseTime)

	result, err := k.AutoCloseLease(cacheCtx, &lease, account, closeTime)
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 100_000_000)), result.TransferAmounts)
	require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
	require.Equal(t, closeTime, *lease.ClosedAt)
	require.Equal(t, closeTime, lease.LastSettledAt)
	require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason)
	require.Empty(t, lease.Reservation.RemainingAmounts)
	require.Zero(t, account.ActiveLeaseCount)
	require.Empty(t, account.ReservedAmounts)
	require.Equal(t, strings.ToUpper(s.tenant.String()), lease.Tenant)
	require.Equal(t, lease.Tenant, account.Tenant)
	require.Equal(t, strings.ToUpper(types.DeriveCreditAddress(s.tenant).String()), account.CreditAddress)
	require.Equal(t, reservationBefore, reservationAlias.String())
	require.Equal(t, accountCoinsBefore, accountCoinsAlias.String())
	require.Equal(t, storesBefore, snapshotPayoutStores(t, f, f.Ctx), "only the caller commits its cache")

	write()
	storedLease, err := k.GetLease(f.Ctx, lease.Uuid)
	require.NoError(t, err)
	lease.Tenant = s.tenant.String()
	require.Equal(t, lease.String(), storedLease.String())
	storedAccount, err := k.GetCreditAccount(f.Ctx, account.Tenant)
	require.NoError(t, err)
	account.Tenant = s.tenant.String()
	account.CreditAddress = types.DeriveCreditAddress(s.tenant).String()
	require.Equal(t, account.String(), storedAccount.String())
	require.NoError(t, k.ExportGenesis(f.Ctx.WithBlockTime(closeTime)).ValidateCurrentState())
}
