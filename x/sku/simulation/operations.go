package simulation

import (
	"math/rand"

	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"github.com/manifest-network/manifest-ledger/x/sku/keeper"
	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

const (
	OpWeightMsgCreateProvider     = "op_weight_msg_sku_create_provider"     //nolint:gosec
	OpWeightMsgUpdateProvider     = "op_weight_msg_sku_update_provider"     //nolint:gosec
	OpWeightMsgDeactivateProvider = "op_weight_msg_sku_deactivate_provider" //nolint:gosec
	OpWeightMsgCreateSKU          = "op_weight_msg_sku_create"              //nolint:gosec
	OpWeightMsgUpdateSKU          = "op_weight_msg_sku_update"              //nolint:gosec
	OpWeightMsgDeactivateSKU      = "op_weight_msg_sku_deactivate"          //nolint:gosec

	DefaultWeightMsgCreateProvider     = 30
	DefaultWeightMsgUpdateProvider     = 20
	DefaultWeightMsgDeactivateProvider = 10
	DefaultWeightMsgCreateSKU          = 50
	DefaultWeightMsgUpdateSKU          = 30
	DefaultWeightMsgDeactivateSKU      = 20
)

var (
	skuNames = []string{"Compute Small", "Compute Medium", "Compute Large", "Storage 100GB", "Storage 1TB", "Bandwidth 1Gbps"}
	units    = []types.Unit{types.Unit_UNIT_PER_HOUR, types.Unit_UNIT_PER_DAY}
)

// WeightedOperations returns the all the sku module operations with their respective weights.
func WeightedOperations(
	appParams simtypes.AppParams,
	_ codec.JSONCodec,
	txGen client.TxConfig,
	k keeper.Keeper,
) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0)

	var weightMsgCreateProvider int
	appParams.GetOrGenerate(OpWeightMsgCreateProvider, &weightMsgCreateProvider, nil, func(_ *rand.Rand) {
		weightMsgCreateProvider = DefaultWeightMsgCreateProvider
	})

	var weightMsgUpdateProvider int
	appParams.GetOrGenerate(OpWeightMsgUpdateProvider, &weightMsgUpdateProvider, nil, func(_ *rand.Rand) {
		weightMsgUpdateProvider = DefaultWeightMsgUpdateProvider
	})

	var weightMsgDeactivateProvider int
	appParams.GetOrGenerate(OpWeightMsgDeactivateProvider, &weightMsgDeactivateProvider, nil, func(_ *rand.Rand) {
		weightMsgDeactivateProvider = DefaultWeightMsgDeactivateProvider
	})

	var weightMsgCreateSKU int
	appParams.GetOrGenerate(OpWeightMsgCreateSKU, &weightMsgCreateSKU, nil, func(_ *rand.Rand) {
		weightMsgCreateSKU = DefaultWeightMsgCreateSKU
	})

	var weightMsgUpdateSKU int
	appParams.GetOrGenerate(OpWeightMsgUpdateSKU, &weightMsgUpdateSKU, nil, func(_ *rand.Rand) {
		weightMsgUpdateSKU = DefaultWeightMsgUpdateSKU
	})

	var weightMsgDeactivateSKU int
	appParams.GetOrGenerate(OpWeightMsgDeactivateSKU, &weightMsgDeactivateSKU, nil, func(_ *rand.Rand) {
		weightMsgDeactivateSKU = DefaultWeightMsgDeactivateSKU
	})

	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCreateProvider,
		SimulateMsgCreateProvider(txGen, k),
	))

	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUpdateProvider,
		SimulateMsgUpdateProvider(txGen, k),
	))

	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgDeactivateProvider,
		SimulateMsgDeactivateProvider(txGen, k),
	))

	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCreateSKU,
		SimulateMsgCreateSKU(txGen, k),
	))

	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUpdateSKU,
		SimulateMsgUpdateSKU(txGen, k),
	))

	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgDeactivateSKU,
		SimulateMsgDeactivateSKU(txGen, k),
	))

	return operations
}

// SimulateMsgCreateProvider generates a MsgCreateProvider with random values.
func SimulateMsgCreateProvider(txGen client.TxConfig, k keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgCreateProvider{})

		simAccount, found := findAuthority(accs, k.GetAuthority())
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "authority not found in accounts"), nil, nil
		}

		// Select random accounts for address and payout address
		addressAccount, _ := simtypes.RandomAcc(r, accs)
		payoutAccount, _ := simtypes.RandomAcc(r, accs)

		// Generate a random API URL
		apiURL := generateRandomAPIURL(r)

		msg := &types.MsgCreateProvider{
			Authority:     simAccount.Address.String(),
			Address:       addressAccount.Address.String(),
			PayoutAddress: payoutAccount.Address.String(),
			MetaHash:      generateRandomBytes(r),
			ApiUrl:        apiURL,
		}

		return genAndDeliverTxWithRandFees(r, app, ctx, txGen, simAccount, msg, k)
	}
}

// SimulateMsgUpdateProvider generates a MsgUpdateProvider with random values.
func SimulateMsgUpdateProvider(txGen client.TxConfig, k keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgUpdateProvider{})

		simAccount, found := findAuthority(accs, k.GetAuthority())
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "authority not found in accounts"), nil, nil
		}

		allProviders, err := k.GetAllProviders(ctx)
		if err != nil || len(allProviders) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no providers found to update"), nil, nil
		}

		provider := allProviders[r.Intn(len(allProviders))]

		// Select random accounts for address and payout address
		addressAccount, _ := simtypes.RandomAcc(r, accs)
		payoutAccount, _ := simtypes.RandomAcc(r, accs)
		active := r.Float32() > 0.3

		// Generate a random API URL
		apiURL := generateRandomAPIURL(r)

		msg := &types.MsgUpdateProvider{
			Authority:     simAccount.Address.String(),
			Uuid:          provider.Uuid,
			Address:       addressAccount.Address.String(),
			PayoutAddress: payoutAccount.Address.String(),
			MetaHash:      generateRandomBytes(r),
			Active:        active,
			ApiUrl:        apiURL,
		}

		return genAndDeliverTxWithRandFees(r, app, ctx, txGen, simAccount, msg, k)
	}
}

// SimulateMsgDeactivateProvider generates a MsgDeactivateProvider with random values.
func SimulateMsgDeactivateProvider(txGen client.TxConfig, k keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgDeactivateProvider{})

		simAccount, found := findAuthority(accs, k.GetAuthority())
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "authority not found in accounts"), nil, nil
		}

		allProviders, err := k.GetAllProviders(ctx)
		if err != nil || len(allProviders) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no providers found to deactivate"), nil, nil
		}

		// Find an active provider to deactivate
		var activeProviders []types.Provider
		for _, provider := range allProviders {
			if provider.Active {
				activeProviders = append(activeProviders, provider)
			}
		}

		if len(activeProviders) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no active providers found to deactivate"), nil, nil
		}

		provider := activeProviders[r.Intn(len(activeProviders))]

		msg := &types.MsgDeactivateProvider{
			Authority: simAccount.Address.String(),
			Uuid:      provider.Uuid,
		}

		return genAndDeliverTxWithRandFees(r, app, ctx, txGen, simAccount, msg, k)
	}
}

// SimulateMsgCreateSKU generates a MsgCreateSKU with random values.
func SimulateMsgCreateSKU(txGen client.TxConfig, k keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgCreateSKU{})

		simAccount, found := findAuthority(accs, k.GetAuthority())
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "authority not found in accounts"), nil, nil
		}

		allProviders, err := k.GetAllProviders(ctx)
		if err != nil || len(allProviders) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no providers found"), nil, nil
		}

		// Find an active provider
		var activeProviders []types.Provider
		for _, provider := range allProviders {
			if provider.Active {
				activeProviders = append(activeProviders, provider)
			}
		}

		if len(activeProviders) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no active providers found"), nil, nil
		}

		provider := activeProviders[r.Intn(len(activeProviders))]

		name := skuNames[r.Intn(len(skuNames))]
		unit := units[r.Intn(len(units))]

		// Generate a price that is EXACTLY divisible by the unit's seconds
		// to pass the exact division validation.
		// For UNIT_PER_HOUR: price must be a multiple of 3600
		// For UNIT_PER_DAY: price must be a multiple of 86400
		basePrice := generateValidPrice(r, unit)

		msg := &types.MsgCreateSKU{
			Authority:    simAccount.Address.String(),
			ProviderUuid: provider.Uuid,
			Name:         name,
			Unit:         unit,
			BasePrice:    basePrice,
			MetaHash:     generateRandomBytes(r),
		}

		return genAndDeliverTxWithRandFees(r, app, ctx, txGen, simAccount, msg, k)
	}
}

// SimulateMsgUpdateSKU generates a MsgUpdateSKU with random values.
func SimulateMsgUpdateSKU(txGen client.TxConfig, k keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgUpdateSKU{})

		simAccount, found := findAuthority(accs, k.GetAuthority())
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "authority not found in accounts"), nil, nil
		}

		allSKUs, err := k.GetAllSKUs(ctx)
		if err != nil || len(allSKUs) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no SKUs found to update"), nil, nil
		}

		sku := allSKUs[r.Intn(len(allSKUs))]

		name := skuNames[r.Intn(len(skuNames))]
		unit := units[r.Intn(len(units))]

		// Generate a price that is EXACTLY divisible by the unit's seconds
		// to pass the exact division validation.
		basePrice := generateValidPrice(r, unit)
		active := r.Float32() > 0.3

		msg := &types.MsgUpdateSKU{
			Authority:    simAccount.Address.String(),
			Uuid:         sku.Uuid,
			ProviderUuid: sku.ProviderUuid,
			Name:         name,
			Unit:         unit,
			BasePrice:    basePrice,
			MetaHash:     generateRandomBytes(r),
			Active:       active,
		}

		return genAndDeliverTxWithRandFees(r, app, ctx, txGen, simAccount, msg, k)
	}
}

// SimulateMsgDeactivateSKU generates a MsgDeactivateSKU with random values.
func SimulateMsgDeactivateSKU(txGen client.TxConfig, k keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgDeactivateSKU{})

		simAccount, found := findAuthority(accs, k.GetAuthority())
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "authority not found in accounts"), nil, nil
		}

		allSKUs, err := k.GetAllSKUs(ctx)
		if err != nil || len(allSKUs) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no SKUs found to deactivate"), nil, nil
		}

		// Find an active SKU to deactivate
		var activeSKUs []types.SKU
		for _, sku := range allSKUs {
			if sku.Active {
				activeSKUs = append(activeSKUs, sku)
			}
		}

		if len(activeSKUs) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no active SKUs found to deactivate"), nil, nil
		}

		sku := activeSKUs[r.Intn(len(activeSKUs))]

		msg := &types.MsgDeactivateSKU{
			Authority: simAccount.Address.String(),
			Uuid:      sku.Uuid,
		}

		return genAndDeliverTxWithRandFees(r, app, ctx, txGen, simAccount, msg, k)
	}
}

func findAuthority(accs []simtypes.Account, authority string) (simtypes.Account, bool) {
	for _, acc := range accs {
		if acc.Address.String() == authority {
			return acc, true
		}
	}
	return simtypes.Account{}, false
}

func generateRandomBytes(r *rand.Rand) []byte {
	const n = 32 // fixed size for meta hash
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Intn(256)) //nolint:gosec
	}
	return b
}

// generateRandomAPIURL generates a random HTTPS URL for provider API endpoint.
func generateRandomAPIURL(r *rand.Rand) string {
	domains := []string{
		"api.provider1.example.com",
		"api.provider2.example.com",
		"compute.acme-cloud.io",
		"services.cloudprovider.net",
		"api.hosting-service.org",
	}
	return "https://" + domains[r.Intn(len(domains))]
}

// generateValidPrice generates a price that is exactly divisible by the unit's seconds.
// This ensures the per-second rate calculation has no remainder.
func generateValidPrice(r *rand.Rand, unit types.Unit) sdk.Coin {
	var secondsInUnit int64
	switch unit {
	case types.Unit_UNIT_PER_HOUR:
		secondsInUnit = 3600
	case types.Unit_UNIT_PER_DAY:
		secondsInUnit = 86400
	default:
		secondsInUnit = 3600 // fallback to hourly
	}

	// Generate a multiplier between 1 and 100 to create varied but valid prices
	multiplier := int64(r.Intn(100) + 1)

	// Price = multiplier * secondsInUnit, ensuring exact divisibility
	price := multiplier * secondsInUnit

	return sdk.NewCoin("umfx", sdkmath.NewInt(price))
}

func newOperationInput(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, txGen client.TxConfig, simAccount simtypes.Account, msg sdk.Msg, k keeper.Keeper) simulation.OperationInput {
	return simulation.OperationInput{
		R:             r,
		App:           app,
		TxGen:         txGen,
		Cdc:           nil,
		Msg:           msg,
		Context:       ctx,
		SimAccount:    simAccount,
		AccountKeeper: k.GetAccountKeeper(),
		Bankkeeper:    k.GetBankKeeper(),
		ModuleName:    types.ModuleName,
	}
}

func genAndDeliverTxWithRandFees(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, txGen client.TxConfig, simAccount simtypes.Account, msg sdk.Msg, k keeper.Keeper) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	return simulation.GenAndDeliverTxWithRandFees(newOperationInput(r, app, ctx, txGen, simAccount, msg, k))
}
