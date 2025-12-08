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
	OpWeightMsgCreateSKU     = "op_weight_msg_sku_create"     //nolint:gosec
	OpWeightMsgUpdateSKU     = "op_weight_msg_sku_update"     //nolint:gosec
	OpWeightMsgDeactivateSKU = "op_weight_msg_sku_deactivate" //nolint:gosec

	DefaultWeightMsgCreateSKU     = 50
	DefaultWeightMsgUpdateSKU     = 30
	DefaultWeightMsgDeactivateSKU = 20
)

var (
	providers = []string{"provider1", "provider2", "provider3", "provider4", "provider5"}
	skuNames  = []string{"Compute Small", "Compute Medium", "Compute Large", "Storage 100GB", "Storage 1TB", "Bandwidth 1Gbps"}
	units     = []types.Unit{types.Unit_UNIT_PER_HOUR, types.Unit_UNIT_PER_DAY}
)

// WeightedOperations returns the all the sku module operations with their respective weights.
func WeightedOperations(
	appParams simtypes.AppParams,
	_ codec.JSONCodec,
	txGen client.TxConfig,
	k keeper.Keeper,
) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0)

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

// SimulateMsgCreateSKU generates a MsgCreateSKU with random values.
func SimulateMsgCreateSKU(txGen client.TxConfig, k keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgCreateSKU{})

		simAccount, found := findAuthority(accs, k.GetAuthority())
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "authority not found in accounts"), nil, nil
		}

		provider := providers[r.Intn(len(providers))]
		name := skuNames[r.Intn(len(skuNames))]
		unit := units[r.Intn(len(units))]
		basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(int64(r.Intn(10000)+1)))

		msg := &types.MsgCreateSKU{
			Authority: simAccount.Address.String(),
			Provider:  provider,
			Name:      name,
			Unit:      unit,
			BasePrice: basePrice,
			MetaHash:  generateRandomBytes(r, 32),
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
		basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(int64(r.Intn(10000)+1)))
		active := r.Float32() > 0.3

		msg := &types.MsgUpdateSKU{
			Authority: simAccount.Address.String(),
			Provider:  sku.Provider,
			Id:        sku.Id,
			Name:      name,
			Unit:      unit,
			BasePrice: basePrice,
			MetaHash:  generateRandomBytes(r, 32),
			Active:    active,
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
			Provider:  sku.Provider,
			Id:        sku.Id,
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

func generateRandomBytes(r *rand.Rand, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Intn(256)) //nolint:gosec
	}
	return b
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
