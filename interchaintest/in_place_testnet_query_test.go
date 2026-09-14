package interchaintest

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func TestInPlaceTestnetStakingQueryJSON(t *testing.T) {
	// Captured from the fork's AutoCLI queries.
	const poolJSON = `{
		"pool": {
			"not_bonded_tokens": "0",
			"bonded_tokens": "900000000000000000000"
		}
	}`
	// Keep the quoted pagination total: it reproduces the generated decoder failure.
	const delegationsJSON = `{
		"delegation_responses": [{
			"delegation": {
				"delegator_address": "manifest1wvukf0r4neq257fvl52jphhfus5mj4m7vxadtj",
				"validator_address": "manifestvaloper1wvukf0r4neq257fvl52jphhfus5mj4m7sxqe8q",
				"shares": "900000000000000000000.000000000000000000"
			},
			"balance": {"denom": "upoa", "amount": "900000000000000000000"}
		}],
		"pagination": {"total": "1"}
	}`
	tokens, ok := sdkmath.NewIntFromString("900000000000000000000")
	require.True(t, ok)
	pool, err := decodeInPlaceTestnetPool([]byte(poolJSON))
	require.NoError(t, err)
	require.True(t, pool.Pool.BondedTokens.Equal(tokens))
	require.True(t, pool.Pool.NotBondedTokens.IsZero())

	var generated stakingtypes.QueryDelegatorDelegationsResponse
	err = json.Unmarshal([]byte(delegationsJSON), &generated)
	var typeError *json.UnmarshalTypeError
	require.ErrorAs(t, err, &typeError)
	require.Equal(t, "pagination.total", typeError.Field)
	require.Equal(t, "string", typeError.Value)
	require.Equal(t, "uint64", typeError.Type.String())

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
