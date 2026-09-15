package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// Parameter-update simulation operation key and default selection weight.
const (
	OpWeightMsgUpdateParams      = "op_weight_msg_billing_update_params" //nolint:gosec // simulation configuration key
	DefaultWeightMsgUpdateParams = 5
)

// SimulateMsgUpdateParams exercises whole-parameter replacement using the real
// configured authority. Keep domain policy and delegated admins while varying
// lifecycle limits, durations and timeouts across leases created earlier.
func SimulateMsgUpdateParams(txGen client.TxConfig, k keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgUpdateParams{})
		signer, found, err := simulationAccountForAddress(accs, k.GetAuthority())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "invalid authority address"), nil, err
		}
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "authority account not found in simulation"), nil, nil
		}
		current, err := k.GetParams(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get params"), nil, err
		}
		params := randomParams(r)
		params.AllowedList = current.AllowedList
		params.ReservedDomainSuffixes = current.ReservedDomainSuffixes
		msg := &types.MsgUpdateParams{Authority: signer.Address.String(), Params: params}
		return genAndDeliverTxWithRandFees(r, app, ctx, txGen, signer, msg, k)
	}
}
