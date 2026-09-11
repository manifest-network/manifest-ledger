package helpers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"

	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	queryCommand      = "query"
	cliGasFlag        = "--gas"
	gasAdjustmentFlag = "--gas-adjustment"
	gasAdjustment     = "2.0"
	limitFlag         = "--limit"
)

// ExecuteQuery executes a CLI query and decodes its JSON response into result.
func ExecuteQuery(ctx context.Context, chain *cosmos.CosmosChain, cmd []string, i interface{}, extraFlags ...string) {
	flags := []string{
		"--node", chain.GetRPCAddress(),
		"--output=json",
	}
	flags = append(flags, extraFlags...)

	ExecuteExec(ctx, chain, cmd, i, flags...)
}

// ExecuteExec executes a CLI command and decodes its JSON response into result.
func ExecuteExec(ctx context.Context, chain *cosmos.CosmosChain, cmd []string, i interface{}, extraFlags ...string) {
	command := []string{chain.Config().Bin}
	command = append(command, cmd...)
	command = append(command, extraFlags...)
	fmt.Println(command)

	stdout, _, err := chain.Exec(ctx, command, chain.Config().Env)
	if err != nil {
		fmt.Println(err)
	}

	fmt.Println(string(stdout))
	if err := json.Unmarshal(stdout, &i); err != nil {
		fmt.Println(err)
	}
}

// ExecuteTransaction executes a transaction command and waits for it to be included in a block.
func ExecuteTransaction(ctx context.Context, chain *cosmos.CosmosChain, cmd []string) (sdk.TxResponse, error) {
	var err error
	var stdout []byte
	var res sdk.TxResponse

	stdout, _, err = chain.Exec(ctx, cmd, chain.Config().Env)
	if err != nil {
		return sdk.TxResponse{}, err
	}

	if err := testutil.WaitForBlocks(ctx, 2, chain); err != nil {
		return sdk.TxResponse{}, err
	}

	if err := json.Unmarshal(stdout, &res); err != nil {
		return res, nil
	}

	return res, err
}

// TxCommandBuilder builds a transaction command for the chain's default node.
func TxCommandBuilder(_ context.Context, chain *cosmos.CosmosChain, cmd []string, fromUser string, extraFlags ...string) []string {
	return TxCommandBuilderNode(chain.GetNode(), cmd, fromUser, extraFlags...)
}

// TxCommandBuilderNode builds a transaction command for a specific chain node.
func TxCommandBuilderNode(node *cosmos.ChainNode, cmd []string, fromUser string, extraFlags ...string) []string {
	command := []string{node.Chain.Config().Bin}
	command = append(command, cmd...)
	command = append(command, "--node", node.Chain.GetRPCAddress())
	command = append(command, "--home", node.HomeDir())
	command = append(command, "--chain-id", node.Chain.Config().ChainID)
	command = append(command, "--from", fromUser)
	command = append(command, "--keyring-backend", keyring.BackendTest)
	command = append(command, "--output=json")
	command = append(command, "--yes")

	hasGasFlag := false
	for _, flag := range extraFlags {
		if flag == cliGasFlag {
			hasGasFlag = true
		}
	}

	if !hasGasFlag {
		command = append(command, cliGasFlag, "500000")
	}

	command = append(command, extraFlags...)
	return command
}
