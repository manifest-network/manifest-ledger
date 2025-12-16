package simulation

import (
	"encoding/json"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/codec"
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
	// Random max leases per tenant between 10 and 200
	maxLeasesPerTenant := uint64(r.Intn(190) + 10) //nolint:gosec

	// Random max items per lease: 5-50
	maxItemsPerLease := uint64(r.Intn(45) + 5) //nolint:gosec

	// Random min lease duration: 1-24 hours (in seconds)
	minLeaseDuration := uint64((r.Intn(23) + 1) * 3600) //nolint:gosec

	// Empty allowed list for simulation (only authority can create leases for tenants)
	allowedList := []string{}

	return types.NewParams(maxLeasesPerTenant, allowedList, maxItemsPerLease, minLeaseDuration)
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
