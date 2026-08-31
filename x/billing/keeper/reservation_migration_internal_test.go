package keeper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestAllocateReservationClaimsSparseDenominations(t *testing.T) {
	newClaims := func() []reservationMigrationClaim {
		return []reservationMigrationClaim{
			{
				uuid: "a",
				nominal: sdk.NewCoins(
					sdk.NewInt64Coin("alpha", 3),
					sdk.NewInt64Coin("gamma", 5),
				),
				allocation: sdk.NewCoins(),
			},
			{
				uuid:       "b",
				nominal:    sdk.NewCoins(sdk.NewInt64Coin("beta", 4)),
				allocation: sdk.NewCoins(),
			},
			{
				uuid: "c",
				nominal: sdk.NewCoins(
					sdk.NewInt64Coin("alpha", 3),
					sdk.NewInt64Coin("delta", 2),
				),
				allocation: sdk.NewCoins(),
			},
		}
	}
	budget := sdk.NewCoins(
		sdk.NewInt64Coin("alpha", 5),
		sdk.NewInt64Coin("beta", 3),
		sdk.NewInt64Coin("delta", 1),
		sdk.NewInt64Coin("gamma", 4),
	)
	legacyClaim := sdk.NewCoins(
		sdk.NewInt64Coin("alpha", 2),
		sdk.NewInt64Coin("beta", 1),
		sdk.NewInt64Coin("gamma", 3),
	)

	claims := newClaims()
	legacyAllocation, err := allocateReservationClaims(budget, claims, legacyClaim)
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(
		sdk.NewInt64Coin("alpha", 2),
		sdk.NewInt64Coin("gamma", 3),
	), claims[0].allocation)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("beta", 2)), claims[1].allocation)
	require.Equal(t, sdk.NewCoins(
		sdk.NewInt64Coin("alpha", 2),
		sdk.NewInt64Coin("delta", 1),
	), claims[2].allocation)
	require.Equal(t, sdk.NewCoins(
		sdk.NewInt64Coin("alpha", 1),
		sdk.NewInt64Coin("beta", 1),
		sdk.NewInt64Coin("gamma", 1),
	), legacyAllocation)

	// Repeating the same plan must produce byte-for-byte equivalent canonical
	// coin slices, including equal-remainder claimant tie-breaks.
	repeatedClaims := newClaims()
	repeatedLegacyAllocation, err := allocateReservationClaims(budget, repeatedClaims, legacyClaim)
	require.NoError(t, err)
	require.Equal(t, claims, repeatedClaims)
	require.Equal(t, legacyAllocation, repeatedLegacyAllocation)
}

func TestAllocateReservationClaimsManyDisjointDenominations(t *testing.T) {
	const claimCount = 2_048

	claims := make([]reservationMigrationClaim, 0, claimCount)
	budget := make(sdk.Coins, 0, claimCount)
	for index := range claimCount {
		denom := fmt.Sprintf("unit%05d", index)
		coin := sdk.Coin{Denom: denom, Amount: sdkmath.OneInt()}
		claims = append(claims, reservationMigrationClaim{
			uuid:       fmt.Sprintf("%05d", index),
			nominal:    sdk.Coins{coin},
			allocation: sdk.NewCoins(),
		})
		budget = append(budget, coin)
	}

	legacyAllocation, err := allocateReservationClaims(budget, claims, sdk.NewCoins())
	require.NoError(t, err)
	require.Empty(t, legacyAllocation)
	for index := range claims {
		require.Equal(t, claims[index].nominal, claims[index].allocation)
	}
}
