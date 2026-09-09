package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"github.com/manifest-network/manifest-ledger/x/sku/keeper"
	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

// Parameter-update simulation operation key and default selection weight.
const (
	OpWeightMsgUpdateParams      = "op_weight_msg_sku_update_params" //nolint:gosec // simulation configuration key
	DefaultWeightMsgUpdateParams = 5
)

// SimulateMsgUpdateParams rotates delegated SKU managers using only the actual
// configured authority. Allowed-list membership alone cannot authorize it.
func SimulateMsgUpdateParams(txGen client.TxConfig, k keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgUpdateParams{})
		signers, err := authorizedSimulationAccounts(accs, k.GetAuthority(), types.Params{})
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "invalid authority address"), nil, err
		}
		if len(signers) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "authority account not found in simulation"), nil, nil
		}
		params, err := k.GetParams(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get params"), nil, err
		}
		params.AllowedList = simulationAllowedList(r, accs)
		msg := &types.MsgUpdateParams{Authority: signers[0].Address.String(), Params: params}
		return genAndDeliverTxWithRandFees(r, app, ctx, txGen, signers[0], msg, k)
	}
}
