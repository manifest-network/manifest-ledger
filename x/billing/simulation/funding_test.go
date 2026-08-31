package simulation

import (
	"math/big"
	"math/rand"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/require"
)

func TestRandomFundingAmount_LargeRangeDoesNotNarrowOrPanic(t *testing.T) {
	minimum := sdkmath.NewInt(1_000_000)
	maximum := sdkmath.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 255))
	r := rand.New(rand.NewSource(1)) //nolint:gosec

	var amount sdkmath.Int
	require.NotPanics(t, func() {
		var err error
		amount, err = randomFundingAmount(r, minimum, maximum)
		require.NoError(t, err)
	})
	require.True(t, amount.GTE(minimum))
	require.True(t, amount.LT(maximum))
}

func TestRandomFundingAmount_EmptyRangeUsesMinimum(t *testing.T) {
	minimum := sdkmath.NewInt(1_000_000)
	r := rand.New(rand.NewSource(1)) //nolint:gosec

	amount, err := randomFundingAmount(r, minimum, minimum)
	require.NoError(t, err)
	require.Equal(t, minimum, amount)
}
