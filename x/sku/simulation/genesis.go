package simulation

import (
	"encoding/json"
	"math/rand"

	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

// RandomizedGenState generates a random GenesisState for the sku module.
func RandomizedGenState(simState *module.SimulationState) {
	var providers []types.Provider
	var skus []types.SKU
	var nextProviderID uint64 = 1
	var nextSKUID uint64 = 1

	// Create random providers
	numProviders := simState.Rand.Intn(5) + 1
	for i := 0; i < numProviders; i++ {
		provider := generateRandomProvider(simState.Rand, simState.Accounts, nextProviderID)
		providers = append(providers, provider)
		nextProviderID++
	}

	// Create random SKUs for each provider
	numSKUs := simState.Rand.Intn(10) + 1
	for i := 0; i < numSKUs; i++ {
		// Pick a random provider
		providerID := uint64(simState.Rand.Intn(len(providers))) + 1 //nolint:gosec // simulation code, not security-critical
		sku := generateRandomSKU(simState.Rand, providerID, nextSKUID)
		skus = append(skus, sku)
		nextSKUID++
	}

	genesisState := types.GenesisState{
		Params:         types.DefaultParams(),
		Providers:      providers,
		Skus:           skus,
		NextProviderId: nextProviderID,
		NextSkuId:      nextSKUID,
	}

	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&genesisState)
}

func generateRandomProvider(r *rand.Rand, accs []simtypes.Account, id uint64) types.Provider {
	// Pick random accounts for address and payout address
	acc, _ := simtypes.RandomAcc(r, accs)
	payoutAcc, _ := simtypes.RandomAcc(r, accs)
	active := r.Float32() > 0.2

	return types.Provider{
		Id:            id,
		Address:       acc.Address.String(),
		PayoutAddress: payoutAcc.Address.String(),
		MetaHash:      generateRandomBytes(r),
		Active:        active,
	}
}

func generateRandomSKU(r *rand.Rand, providerID uint64, id uint64) types.SKU {
	name := skuNames[r.Intn(len(skuNames))]
	unit := units[r.Intn(len(units))]
	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(int64(r.Intn(10000)+1)))
	active := r.Float32() > 0.2

	return types.SKU{
		Id:         id,
		ProviderId: providerID,
		Name:       name,
		Unit:       unit,
		BasePrice:  basePrice,
		MetaHash:   generateRandomBytes(r),
		Active:     active,
	}
}

// GetGenesisStateFromAppState returns the sku module GenesisState from app state.
func GetGenesisStateFromAppState(cdc codec.JSONCodec, appState map[string]json.RawMessage) types.GenesisState {
	var genesisState types.GenesisState

	if appState[types.ModuleName] != nil {
		cdc.MustUnmarshalJSON(appState[types.ModuleName], &genesisState)
	}

	return genesisState
}
