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
	OpWeightMsgPayoutStakeholders      = "op_weight_msg_payout_stakeholders" // nolint: gosec
	DefaultWeightMsgPayoutStakeholders = 100
)

// WeightedOperations returns the all the gov module operations with their respective weights.
func WeightedOperations(appParams simtypes.AppParams,
	_ codec.JSONCodec,
	txGen client.TxConfig,
	k keeper.Keeper,
) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0)

	var weightMsgPayoutStakeholders int
	appParams.GetOrGenerate(OpWeightMsgPayoutStakeholders, &weightMsgPayoutStakeholders, nil, func(_ *rand.Rand) {
		weightMsgPayoutStakeholders = DefaultWeightMsgPayoutStakeholders
	})

	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgPayoutStakeholders,
		SimulateMsgPayout(txGen, k),
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
