package simulation

import (
	"math/rand"

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
	OpWeightMsgPayoutStakeholders      = "op_weight_msg_payout_stakeholders"
	DefaultWeightMsgPayoutStakeholders = 100
)

// WeightedOperations returns the all the gov module operations with their respective weights.
func WeightedOperations(appParams simtypes.AppParams,
	cdc codec.JSONCodec,
	txGen client.TxConfig,
	k keeper.Keeper) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0)

	var weightMsgPayoutStakeholders int
	appParams.GetOrGenerate(OpWeightMsgPayoutStakeholders, &weightMsgPayoutStakeholders, nil, func(r *rand.Rand) {
		weightMsgPayoutStakeholders = DefaultWeightMsgPayoutStakeholders
	})

	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgPayoutStakeholders,
		SimulateMsgPayout(k),
	))

	return operations
}

func SimulateMsgPayout(k keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgPayout{})

		//msg := types.NewMsgPayoutStakeholders()
		//return simtypes.NewOperationMsg(msg, true, ""), nil, nil
		return simtypes.NoOpMsg(types.ModuleName, msgType, "placeholder"), nil, nil
	}

}
