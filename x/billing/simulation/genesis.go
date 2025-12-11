package simulation

import (
	"encoding/json"
	"math/rand"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// RandomizedGenState generates a random GenesisState for the billing module.
func RandomizedGenState(simState *module.SimulationState) {
	// For simulation, we start with default params and empty state.
	// Leases and credit accounts are created via simulation operations.
	genesisState := types.GenesisState{
		Params:         randomParams(simState.Rand),
		Leases:         []types.Lease{},
		CreditAccounts: []types.CreditAccount{},
		NextLeaseId:    1,
	}

	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&genesisState)
}

// randomParams returns randomized billing module parameters.
func randomParams(r *rand.Rand) types.Params {
	// NOTE: For simulation, we use the staking bond denom ("stake" via sdk.DefaultBondDenom)
	// instead of the production PWR factory denom
	// (factory/manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj/upwr).
	// This is because factory denoms require the TokenFactory module to be set up with
	// specific creator addresses, which is not available during simulation. The simulation
	// genesis funds accounts with the bond denom via the bank module's RandomGenesisBalances.
	denom := sdk.DefaultBondDenom

	// Random min credit balance between 1_000_000 and 10_000_000 (1-10 tokens)
	minCreditBalance := math.NewInt(int64(r.Intn(9_000_000) + 1_000_000))

	// Random max leases per tenant between 10 and 200
	maxLeasesPerTenant := uint64(r.Intn(190) + 10) //nolint:gosec

	// Random max items per lease: 5-50
	maxItemsPerLease := uint64(r.Intn(45) + 5) //nolint:gosec

	// Empty allowed list for simulation (only authority can create leases for tenants)
	allowedList := []string{}

	return types.NewParams(denom, minCreditBalance, maxLeasesPerTenant, allowedList, maxItemsPerLease)
}

// GetGenesisStateFromAppState returns the billing module GenesisState from app state.
func GetGenesisStateFromAppState(cdc codec.JSONCodec, appState map[string]json.RawMessage) types.GenesisState {
	var genesisState types.GenesisState

	if appState[types.ModuleName] != nil {
		cdc.MustUnmarshalJSON(appState[types.ModuleName], &genesisState)
	}

	return genesisState
}

// RandomAccounts returns a slice of random simulation accounts.
func RandomAccounts(r *rand.Rand, accs []simtypes.Account, n int) []simtypes.Account {
	if n > len(accs) {
		n = len(accs)
	}

	result := make([]simtypes.Account, n)
	perm := r.Perm(len(accs))
	for i := 0; i < n; i++ {
		result[i] = accs[perm[i]]
	}

	return result
}
