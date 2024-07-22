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

	"github.com/liftedinit/manifest-ledger/x/manifest/keeper"
	"github.com/liftedinit/manifest-ledger/x/manifest/types"
)

const (
	OpWeightMsgPayout                  = "op_weight_msg_manifest_payout"            // nolint: gosec
	OpWeightMsgBurnHeldBalance         = "op_weight_msg_manifest_burn_held_balance" // nolint: gosec
	OpWeightMsgUpdateParams            = "op_weight_msg_manifest_update_params"     // nolint: gosec
	DefaultWeightMsgPayoutStakeholders = 100
	DefaultWeightMsgBurnHeldBalance    = 100
	DefaultWeightMsgUpdateParams       = 100
)

// WeightedOperations returns the all the gov module operations with their respective weights.
func WeightedOperations(appParams simtypes.AppParams,
	_ codec.JSONCodec,
	txGen client.TxConfig,
	k keeper.Keeper,
) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0)

	var weightMsgPayoutStakeholders int
	appParams.GetOrGenerate(OpWeightMsgPayout, &weightMsgPayoutStakeholders, nil, func(_ *rand.Rand) {
		weightMsgPayoutStakeholders = DefaultWeightMsgPayoutStakeholders
	})

	var weightMsgBurnHeldBalance int
	appParams.GetOrGenerate(OpWeightMsgBurnHeldBalance, &weightMsgBurnHeldBalance, nil, func(_ *rand.Rand) {
		weightMsgBurnHeldBalance = DefaultWeightMsgBurnHeldBalance
	})

	var weightMsgUpdateParams int
	appParams.GetOrGenerate(OpWeightMsgUpdateParams, &weightMsgUpdateParams, nil, func(_ *rand.Rand) {
		weightMsgUpdateParams = DefaultWeightMsgUpdateParams
	})

	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgPayoutStakeholders,
		SimulateMsgPayout(txGen, k),
	))

	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgBurnHeldBalance,
		SimulateMsgBurnHeldBalance(txGen, k),
	))

	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgUpdateParams,
		SimulateMsgUpdateParams(txGen, k),
	))

	return operations
}

func SimulateMsgPayout(txGen client.TxConfig, k keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgPayout{})
		simAccount := accs[0]
		if simAccount.Address.String() != k.GetAuthority() {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "invalid authority"), nil, nil
		}

		denoms := k.GetBankKeeper().GetAllDenomMetaData(ctx)
		if len(denoms) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no denom found"), nil, nil
		}

		randomDenomMeta := denoms[r.Intn(len(denoms))]
		randomDenomUnits := randomDenomMeta.DenomUnits
		if len(randomDenomUnits) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no denom units found"), nil, nil
		}

		randomDenomUnit := randomDenomUnits[r.Intn(len(randomDenomUnits))]
		if randomDenomUnit == nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "denom unit is nil"), nil, nil
		}

		// Randomly select a number of stakeholders
		stakeholderNum := simtypes.RandIntBetween(r, 1, min(len(accs), 10))

		// Randomly shuffle the accounts to select stakeholders
		accsCopy := make([]simtypes.Account, len(accs))
		copy(accsCopy, accs)
		r.Shuffle(len(accs), func(i, j int) { accsCopy[i], accsCopy[j] = accsCopy[j], accsCopy[i] })

		// Select the stakeholders
		stakeholders := accsCopy[:stakeholderNum]

		var payoutPairs []types.PayoutPair
		for _, stakeholder := range stakeholders {
			payoutPairs = append(payoutPairs, types.PayoutPair{
				Address: stakeholder.Address.String(),
				Coin:    sdk.NewCoin(randomDenomUnit.Denom, sdkmath.NewInt(int64(r.Intn(1000)+1))),
			})
		}

		msg := types.MsgPayout{
			Authority:   simAccount.Address.String(),
			PayoutPairs: payoutPairs,
		}

		return genAndDeliverTxWithRandFees(r, app, ctx, txGen, simAccount, &msg, k)
	}
}

func SimulateMsgBurnHeldBalance(txGen client.TxConfig, k keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgBurnHeldBalance{})
		simAccount := accs[0]
		if simAccount.Address.String() != k.GetAuthority() {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "invalid authority"), nil, nil
		}

		spendable := k.GetBankKeeper().SpendableCoins(ctx, simAccount.Address)
		coinsToBurn := simtypes.RandSubsetCoins(r, spendable)
		if coinsToBurn.Empty() {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no spendable coin found"), nil, nil
		}

		if err := k.GetBankKeeper().IsSendEnabledCoins(ctx, coinsToBurn...); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, err.Error()), nil, nil
		}

		var fees sdk.Coins
		var err error
		coins, hasNeg := spendable.SafeSub(coinsToBurn...)
		if !hasNeg {
			fees, err = simtypes.RandomFees(r, ctx, coins)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, msgType, "unable to generate fees"), nil, nil
			}
		}

		msg := types.MsgBurnHeldBalance{
			Authority: simAccount.Address.String(),
			BurnCoins: coinsToBurn,
		}

		return genAndDeliverTx(r, app, ctx, txGen, simAccount, &msg, k, fees)
	}
}

func SimulateMsgUpdateParams(txGen client.TxConfig, k keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgUpdateParams{})
		simAccount := accs[0]
		if simAccount.Address.String() != k.GetAuthority() {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "invalid authority"), nil, nil
		}

		msg := types.MsgUpdateParams{
			Authority: simAccount.Address.String(),
			Params:    types.Params{},
		}

		return genAndDeliverTxWithRandFees(r, app, ctx, txGen, simAccount, &msg, k)
	}
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
		AccountKeeper: k.GetTestAccountKeeper(),
		Bankkeeper:    k.GetBankKeeper(),
		ModuleName:    types.ModuleName,
	}
}

func genAndDeliverTxWithRandFees(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, txGen client.TxConfig, simAccount simtypes.Account, msg sdk.Msg, k keeper.Keeper) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	return simulation.GenAndDeliverTxWithRandFees(newOperationInput(r, app, ctx, txGen, simAccount, msg, k))
}

func genAndDeliverTx(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, txGen client.TxConfig, simAccount simtypes.Account, msg sdk.Msg, k keeper.Keeper, fees sdk.Coins) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	return simulation.GenAndDeliverTx(newOperationInput(r, app, ctx, txGen, simAccount, msg, k), fees)
}
