package keeper

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
)

func TestCreditCoinAfterLocksBoundsMalformedBackingPerDenom(t *testing.T) {
	// Unlike SDK SpendableCoin's panicking negative subtraction, a lock above
	// the balance yields no usable credit. A deficit in another denomination
	// cannot reduce a healthy denomination's usable funds.
	locked := sdk.NewCoins(sdk.NewInt64Coin("ualpha", 6), sdk.NewInt64Coin("ubeta", 99))
	for _, tc := range []struct {
		balance sdk.Coin
		want    sdk.Coin
	}{
		{sdk.NewInt64Coin("ualpha", 5), sdk.NewInt64Coin("ualpha", 0)},
		{sdk.NewInt64Coin("ualpha", 6), sdk.NewInt64Coin("ualpha", 0)},
		{sdk.NewInt64Coin("ualpha", 10), sdk.NewInt64Coin("ualpha", 4)},
		{sdk.NewInt64Coin("ugamma", 10), sdk.NewInt64Coin("ugamma", 10)},
	} {
		require.NotPanics(t, func() {
			require.True(t, tc.want.IsEqual(creditCoinAfterLocks(tc.balance, locked)))
		})
	}
}

func TestReservationPreflightAuthLocksRejectsMissingAccountStructure(t *testing.T) {
	accounts := authtypes.GenesisAccounts{nil, (*vestingtypes.PermanentLockedAccount)(nil), &vestingtypes.PermanentLockedAccount{}}
	for _, account := range accounts {
		require.NotPanics(t, func() {
			locks, err := reservationPreflightAuthLocks(authtypes.GenesisAccounts{account}, time.Unix(1, 0))
			require.ErrorContains(t, err, "auth account")
			require.Nil(t, locks)
		})
	}
}
