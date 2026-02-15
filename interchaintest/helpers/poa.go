package helpers

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
)

func POASetPower(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, valoper string, power int64, flags ...string) (sdk.TxResponse, error) {
	cmd := TxCommandBuilder(ctx, chain, []string{"tx", "poa", "set-power", valoper, fmt.Sprintf("%d", power)}, user.KeyName(), flags...)
	return ExecuteTransaction(ctx, chain, cmd)
}

func POACreateValidator(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, jsonPath string, flags ...string) (sdk.TxResponse, error) {
	cmd := TxCommandBuilder(ctx, chain, []string{"tx", "poa", "create-validator", jsonPath}, user.KeyName(), flags...)
	return ExecuteTransaction(ctx, chain, cmd)
}
