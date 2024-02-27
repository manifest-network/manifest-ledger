package helpers

import (
	"context"
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	manifesttypes "github.com/liftedinit/manifest-ledger/x/manifest/types"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
)

func ManifestUpdateParams(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, poaAdmin ibc.Wallet, addressPairs string, automaticInflation bool, coinInflationPerYear sdk.Coin, flags ...string) (sdk.TxResponse, error) {
	txCmd := []string{"tx", "manifest", "update-params", addressPairs, fmt.Sprintf("%v", automaticInflation), coinInflationPerYear.String()}
	fmt.Println("ManifestUpdateParams", txCmd)
	cmd := TxCommandBuilder(ctx, chain, txCmd, poaAdmin.KeyName(), flags...)
	return ExecuteTransaction(ctx, chain, cmd)
}

func ManifestStakeholderPayout(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, poaAdmin ibc.Wallet, coinAmount sdk.Coin, flags ...string) (sdk.TxResponse, error) {
	txCmd := []string{"tx", "manifest", "stakeholder-payout", coinAmount.String()}
	fmt.Println("ManifestStakeholderPayout", txCmd)
	cmd := TxCommandBuilder(ctx, chain, txCmd, poaAdmin.KeyName(), flags...)
	return ExecuteTransaction(ctx, chain, cmd)
}

// queries
func ManifestQueryParams(ctx context.Context, node *cosmos.ChainNode) (*manifesttypes.Params, error) {
	res, err := manifesttypes.NewQueryClient(node.GrpcConn).Params(ctx, &manifesttypes.QueryParamsRequest{})
	return res.GetParams(), err
}
