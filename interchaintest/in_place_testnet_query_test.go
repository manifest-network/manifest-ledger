package interchaintest

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"
)

func TestInPlaceTestnetStakingQueryJSON(t *testing.T) {
	// Captured from the fork's AutoCLI queries.
	const poolJSON = `{
		"pool": {
			"not_bonded_tokens": "0",
			"bonded_tokens": "900000000000000000000"
		}
	}`
	const delegationsJSON = `{
		"delegation_responses": [{
			"delegation": {
				"delegator_address": "manifest1wvukf0r4neq257fvl52jphhfus5mj4m7vxadtj",
				"validator_address": "manifestvaloper1wvukf0r4neq257fvl52jphhfus5mj4m7sxqe8q",
				"shares": "900000000000000000000.000000000000000000"
			},
			"balance": {"denom": "upoa", "amount": "900000000000000000000"}
		}]
	}`
	tokens, ok := sdkmath.NewIntFromString("900000000000000000000")
	require.True(t, ok)
	pool, err := decodeInPlaceTestnetPool([]byte(poolJSON))
	require.NoError(t, err)
	require.True(t, pool.Pool.BondedTokens.Equal(tokens))
	require.True(t, pool.Pool.NotBondedTokens.IsZero())

	delegations, err := decodeInPlaceTestnetDelegations([]byte(delegationsJSON))
	require.NoError(t, err)
	require.Len(t, delegations.DelegationResponses, 1)
	delegation := delegations.DelegationResponses[0]
	require.Equal(t, "manifest1wvukf0r4neq257fvl52jphhfus5mj4m7vxadtj", delegation.Delegation.DelegatorAddress)
	require.Equal(t, "manifestvaloper1wvukf0r4neq257fvl52jphhfus5mj4m7sxqe8q", delegation.Delegation.ValidatorAddress)
	require.True(t, delegation.Delegation.Shares.Equal(sdkmath.LegacyNewDecFromInt(tokens)))
	require.Equal(t, "upoa", delegation.Balance.Denom)
	require.True(t, delegation.Balance.Amount.Equal(tokens))
}
