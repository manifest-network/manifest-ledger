package simulation

import (
	"encoding/json"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

const maxSimulationBillingAdmins = 3

// RandomizedGenState generates a random GenesisState for the billing module.
func RandomizedGenState(simState *module.SimulationState) {
	// For simulation, we start with default params and empty state.
	// Leases and credit accounts are created via simulation operations.
	params := randomParams(simState.Rand)
	params.AllowedList = simulationBillingAllowedList(simState.Rand, simState.Accounts)
	genesisState := types.GenesisState{
		Params:         params,
		Leases:         []types.Lease{},
		CreditAccounts: []types.CreditAccount{},
	}

	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&genesisState)
}

// randomParams returns randomized billing module parameters.
func randomParams(r *rand.Rand) types.Params {
	// Random max leases per tenant between 10 and 200
	maxLeasesPerTenant := uint64(r.Intn(191) + 10) //nolint:gosec

	// Random max items per lease: 5-50
	maxItemsPerLease := uint64(r.Intn(46) + 5) //nolint:gosec

	// Random min lease duration: 1-24 hours (in seconds)
	minLeaseDuration := uint64((r.Intn(24) + 1) * 3600) //nolint:gosec

	// Random max pending leases per tenant: 5-20
	maxPendingLeasesPerTenant := uint64(r.Intn(16) + 5) //nolint:gosec

	// Random pending timeout: 1-60 minutes (60-3600 seconds)
	pendingTimeout := uint64(r.Intn(3541) + 60) //nolint:gosec

	// Empty allowed list for simulation (only authority can create leases for tenants)
	allowedList := []string{}

	params := types.NewParams(maxLeasesPerTenant, allowedList, maxItemsPerLease, minLeaseDuration, maxPendingLeasesPerTenant, pendingTimeout)
	reservedSuffixes := []string{".simulation.example", ".simulation.test", ".simulation.invalid"}
	count := r.Intn(3)
	permutation := r.Perm(len(reservedSuffixes))
	params.ReservedDomainSuffixes = make([]string, count)
	for i := range count {
		params.ReservedDomainSuffixes[i] = reservedSuffixes[permutation[i]]
	}
	return params
}

func simulationBillingAllowedList(r *rand.Rand, accs []simtypes.Account) []string {
	if len(accs) == 0 {
		return nil
	}

	target := min(maxSimulationBillingAdmins, len(accs))
	target = r.Intn(target) + 1
	permutation := r.Perm(len(accs))
	allowed := make([]string, 0, target)
	allowedAddresses := make([]sdk.AccAddress, 0, target)
	for _, index := range permutation {
		candidate := accs[index].Address
		duplicate := false
		for _, existing := range allowedAddresses {
			if candidate.Equals(existing) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		allowed = append(allowed, candidate.String())
		allowedAddresses = append(allowedAddresses, candidate)
		if len(allowed) == target {
			break
		}
	}
	return allowed
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
	n = min(n, len(accs))

	result := make([]simtypes.Account, n)
	perm := r.Perm(len(accs))
	for i := range n {
		result[i] = accs[perm[i]]
	}

	return result
}
