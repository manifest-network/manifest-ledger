package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/liftedinit/manifest-ledger/x/manifest/types"
)

const (
	keyInflation    = "inflation"
	keyStakeHolders = "stake_holders"
)

// genInflation returns a randomly generated Inflation object.
func genInflation(r *rand.Rand) types.Inflation {
	return types.Inflation{
		AutomaticEnabled: r.Intn(100) > 50,
		YearlyAmount:     uint64(r.Intn(100)),
		MintDenom:        "stake",
	}
}

// genStakeHolders returns a randomly generated StakeHolders object.
// TODO: Randomize
func genStakeHolders(r *rand.Rand) []*types.StakeHolders {
	return []*types.StakeHolders{
		{
			Address:    "manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct",
			Percentage: 100000000,
		},
	}
}

// RandomizedGenState generates a random GenesisState for manifest.
func RandomizedGenState(simState *module.SimulationState) {
	var (
		inflation    types.Inflation
		stakeHolders []*types.StakeHolders
	)

	simState.AppParams.GetOrGenerate(keyInflation, &inflation, simState.Rand, func(r *rand.Rand) {
		inflation = genInflation(r)
	})

	simState.AppParams.GetOrGenerate(keyStakeHolders, &stakeHolders, simState.Rand, func(r *rand.Rand) {
		stakeHolders = genStakeHolders(r)
	})
}
