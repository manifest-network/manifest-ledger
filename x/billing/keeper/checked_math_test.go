package keeper_test

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func maxBillingTestInt() sdkmath.Int {
	value := new(big.Int).Lsh(big.NewInt(1), 256)
	value.Sub(value, big.NewInt(1))
	return sdkmath.NewIntFromBigInt(value)
}

func highBitBillingTestInt() sdkmath.Int {
	return sdkmath.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 255))
}

func calculateLeaseReservation(t *testing.T, items []types.LeaseItem, duration uint64) sdk.Coins {
	t.Helper()
	reservation, err := types.CalculateLeaseReservation(items, duration)
	require.NoError(t, err)
	return reservation
}

func addTestCoins(t *testing.T, left, right sdk.Coins) sdk.Coins {
	t.Helper()
	total, err := types.SafeAddCoins(left, right)
	require.NoError(t, err)
	return total
}

func addTestReservation(t *testing.T, reserved, addition sdk.Coins) sdk.Coins {
	t.Helper()
	total, err := types.AddReservation(reserved, addition)
	require.NoError(t, err)
	return total
}

func getLeaseReservationAmount(t *testing.T, lease *types.Lease, fallbackDuration uint64) sdk.Coins {
	t.Helper()
	reservation, err := types.GetLeaseReservationAmount(lease, fallbackDuration)
	require.NoError(t, err)
	return reservation
}

func calculateExpectedReservationsByTenant(
	t *testing.T,
	leases []types.Lease,
	fallbackDuration uint64,
) map[string]sdk.Coins {
	t.Helper()
	expected, err := types.CalculateExpectedReservationsByTenant(leases, fallbackDuration)
	require.NoError(t, err)
	return expected
}
