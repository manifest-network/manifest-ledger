package helpers

import (
	"context"
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"

	manifesttypes "github.com/liftedinit/manifest-ledger/x/manifest/types"
)

func ManifestUpdateParams(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, poaAdmin ibc.Wallet, addressPairs string, automaticInflation string, coinInflationPerYear string, flags ...string) (sdk.TxResponse, error) {
	txCmd := []string{"tx", "manifest", "update-params", addressPairs, automaticInflation, coinInflationPerYear}
	fmt.Println("ManifestUpdateParams", txCmd)
	cmd := TxCommandBuilder(ctx, chain, txCmd, poaAdmin.KeyName(), flags...)
	return ExecuteTransaction(ctx, chain, cmd)
}

func ManifestStakeholderPayout(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, poaAdmin ibc.Wallet, coinAmount string, flags ...string) (sdk.TxResponse, error) {
	txCmd := []string{"tx", "manifest", "stakeholder-payout", coinAmount}
	fmt.Println("ManifestStakeholderPayout", txCmd)
	cmd := TxCommandBuilder(ctx, chain, txCmd, poaAdmin.KeyName(), flags...)
	return ExecuteTransaction(ctx, chain, cmd)
}

// queries
func ManifestQueryParams(ctx context.Context, node *cosmos.ChainNode) (*manifesttypes.Params, error) {
	res, err := manifesttypes.NewQueryClient(node.GrpcConn).Params(ctx, &manifesttypes.QueryParamsRequest{})
	return res.GetParams(), err
}

func ManifestBurnTokens(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, keyName string, amount string, flags ...string) (sdk.TxResponse, error) {
	txCmd := []string{"tx", "manifest", "burn-coins", amount}
	fmt.Println("ManifestBurnTokens", txCmd)
	cmd := TxCommandBuilder(ctx, chain, txCmd, keyName, flags...)
	return ExecuteTransaction(ctx, chain, cmd)
}
