package simulation

import (
	"encoding/json"
	"fmt"
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

	// Create random providers
	numProviders := simState.Rand.Intn(5) + 1
	for i := 0; i < numProviders; i++ {
		providerUUID := generateSimUUID(simState.Rand, i)
		provider := generateRandomProvider(simState.Rand, simState.Accounts, providerUUID)
		providers = append(providers, provider)
	}

	// Create random SKUs for each provider
	numSKUs := simState.Rand.Intn(10) + 1
	for i := 0; i < numSKUs; i++ {
		// Pick a random provider
		providerIdx := simState.Rand.Intn(len(providers))
		providerUUID := providers[providerIdx].Uuid
		skuUUID := generateSimUUID(simState.Rand, 1000+i) // offset to avoid collision with provider UUIDs
		sku := generateRandomSKU(simState.Rand, providerUUID, skuUUID)
		skus = append(skus, sku)
	}

	genesisState := types.GenesisState{
		Params:    types.DefaultParams(),
		Providers: providers,
		Skus:      skus,
	}

	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(&genesisState)
}

// generateSimUUID generates a deterministic UUID-like string for simulation.
// This is not a real UUIDv7 but provides unique identifiers for testing.
func generateSimUUID(r *rand.Rand, seed int) string {
	return fmt.Sprintf("%08x-%04x-7%03x-%04x-%012x",
		r.Uint32(),
		r.Uint32()&0xFFFF,
		(r.Uint32()&0x0FFF)|uint32(seed&0xF00), //nolint:gosec // seed is small and within int32 range
		(r.Uint32()&0x3FFF)|0x8000,
		r.Uint64()&0xFFFFFFFFFFFF,
	)
}

func generateRandomProvider(r *rand.Rand, accs []simtypes.Account, uuid string) types.Provider {
	// Pick random accounts for address and payout address
	acc, _ := simtypes.RandomAcc(r, accs)
	payoutAcc, _ := simtypes.RandomAcc(r, accs)
	active := r.Float32() > 0.2

	return types.Provider{
		Uuid:          uuid,
		Address:       acc.Address.String(),
		PayoutAddress: payoutAcc.Address.String(),
		MetaHash:      generateRandomBytes(r),
		Active:        active,
		ApiUrl:        generateRandomAPIURL(r),
	}
}

func generateRandomSKU(r *rand.Rand, providerUUID string, uuid string) types.SKU {
	name := skuNames[r.Intn(len(skuNames))]
	unit := units[r.Intn(len(units))]
	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(int64(r.Intn(10000)+1)))
	active := r.Float32() > 0.2

	return types.SKU{
		Uuid:         uuid,
		ProviderUuid: providerUUID,
		Name:         name,
		Unit:         unit,
		BasePrice:    basePrice,
		MetaHash:     generateRandomBytes(r),
		Active:       active,
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
