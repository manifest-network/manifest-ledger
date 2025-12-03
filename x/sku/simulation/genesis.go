package simulation

import (
	"encoding/json"
	"math/rand"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

// RandomizedGenState generates a random GenesisState for the sku module.
func RandomizedGenState(simState *module.SimulationState) {
	var skus []types.SKU
	var nextID uint64 = 1

	numSKUs := simState.Rand.Intn(10) + 1

	for i := 0; i < numSKUs; i++ {
		sku := generateRandomSKU(simState.Rand, nextID)
		skus = append(skus, sku)
		nextID++
	}

	genesisState := types.GenesisState{
		Skus:   skus,
		NextId: nextID,
	}

	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&genesisState)
}

func generateRandomSKU(r *rand.Rand, id uint64) types.SKU {
	provider := providers[r.Intn(len(providers))]
	name := skuNames[r.Intn(len(skuNames))]
	unit := units[r.Intn(len(units))]
	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(int64(r.Intn(10000)+1)))
	active := r.Float32() > 0.2

	return types.SKU{
		Id:        id,
		Provider:  provider,
		Name:      name,
		Unit:      unit,
		BasePrice: basePrice,
		MetaHash:  generateRandomBytes(r, 32),
		Active:    active,
	}
}

// GetGenesisStateFromAppState returns the sku module GenesisState from app state.
func GetGenesisStateFromAppState(
	cdc interface {
		UnmarshalJSON([]byte, interface{}) error
	},
	appState map[string]json.RawMessage,
) types.GenesisState {
	var genesisState types.GenesisState

	if appState[types.ModuleName] != nil {
		if err := cdc.UnmarshalJSON(appState[types.ModuleName], &genesisState); err != nil {
			panic(err)
		}
	}

	return genesisState
}
