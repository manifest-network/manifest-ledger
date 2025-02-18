package helpers

import (
	"context"
	"encoding/json"
	"path"
	"strconv"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/cosmos/cosmos-sdk/x/group"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/dockerutil"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/stretchr/testify/require"
)

// SubmitGroupProposal submits a group proposal to the chain.
// TODO: This function should be part of `interchaintest`
// See https://github.com/strangelove-ventures/interchaintest/issues/1138
func SubmitGroupProposal(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, config *ibc.ChainConfig, keyName string, prop *group.MsgSubmitProposal) (string, error) {
	file := "proposal.json"
	propJson, err := json.MarshalIndent(prop, "", " ")
	require.NoError(t, err)

	tn := chain.GetNode()

	fw := dockerutil.NewFileWriter(nil, tn.DockerClient, tn.TestName)
	err = fw.WriteFile(ctx, tn.VolumeName, file, propJson)
	require.NoError(t, err)

	submitCommand := []string{
		"group", "submit-proposal",
		path.Join(tn.HomeDir(), file),
		"--gas", "8000000",
		"--gas-adjustment", "2.0",
	}

	return exec(ctx, chain, config, tn.TxCommand(keyName, submitCommand...))
}

//// QueryGroupProposal queries a group proposal on the chain.
//// TODO: This function should be part of `interchaintest`
//// See https://github.com/strangelove-ventures/interchaintest/issues/1138
//func QueryGroupProposal(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, config *ibc.ChainConfig, proposalId string) (string, error) {
//	query := []string{
//		"group", "proposal", proposalId,
//	}
//
//	tn := chain.GetNode()
//
//	o, _, err := tn.Exec(ctx, tn.QueryCommand(query...), config.Env)
//	if err != nil {
//		return "", errors.WithMessage(err, "failed to query group proposal")
//	}
//
//	var data interface{}
//	if err := json.Unmarshal([]byte(o), &data); err != nil {
//		return "", errors.WithMessage(err, "failed to unmarshal group proposal")
//
//	}
//
//	prettyJSON, err := json.MarshalIndent(data, "", "  ")
//	if err != nil {
//		return "", errors.WithMessage(err, "failed to marshal group proposal")
//	}
//
//	return string(prettyJSON), nil
//}

// VoteGroupProposal votes on a group proposal on the chain.
// TODO: This function should be part of `interchaintest`
// See https://github.com/strangelove-ventures/interchaintest/issues/1138
func VoteGroupProposal(ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, proposalId, accAddr, vote, metadata string) (string, error) {
	voteCommand := []string{
		"group", "vote", proposalId, accAddr, vote, metadata,
		"--gas", "1000000",
		"--gas-adjustment", "2.0",
	}
	return exec(ctx, chain, config, chain.GetNode().TxCommand(accAddr, voteCommand...))
}

func ExecGroupProposal(ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr, proposalId string) (string, error) {
	tn := chain.GetNode()

	execCommand := []string{
		"group", "exec", proposalId,
		"--gas", "20000000",
		"--gas-adjustment", "2.0",
	}
	return exec(ctx, chain, config, tn.TxCommand(accAddr, execCommand...))
}

func exec(ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, command []string) (string, error) {
	tn := chain.GetNode()

	o, _, err := tn.Exec(ctx, command, config.Env)
	if err != nil {
		return "", errors.WithMessage(err, "failed to execute group proposal")
	}

	output := cosmos.CosmosTx{}
	if err := json.Unmarshal([]byte(o), &output); err != nil {
		return "", errors.WithMessage(err, "failed to unmarshal group proposal")
	}

	if err := testutil.WaitForBlocks(ctx, 3, tn); err != nil {
		return "", errors.WithMessage(err, "failed to wait for blocks")
	}

	txResp, err := chain.GetTransaction(output.TxHash)
	if err != nil {
		return "", errors.WithMessage(err, "failed to get transaction")
	}

	if txResp.Code != 0 {
		return "", errors.Errorf("failed to execute group proposal: %s", txResp.RawLog)
	}

	// The transaction itself can be successful but the proposal can fail
	// Check for proposal execution failure
	var logs string
	success := true
	expectedProposalResult := strconv.Quote(group.PROPOSAL_EXECUTOR_RESULT_SUCCESS.String())
	for _, event := range txResp.Events {
		if event.GetType() != "cosmos.group.v1.EventExec" {
			continue
		}
		for _, attr := range event.GetAttributes() {
			switch attr.Key {
			case "logs":
				logs = attr.Value
			case "result":
				if attr.Value != expectedProposalResult {
					success = false
				}
			}
		}
	}

	// The proposal failed, return the logs
	if !success {
		return "", errors.Newf("failed to execute group proposal: %s", logs)
	}

	return txResp.TxHash, nil
}
