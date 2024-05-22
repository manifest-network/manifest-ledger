package helpers

import (
	"context"
	"encoding/json"
	"path"
	"testing"

	"github.com/cosmos/cosmos-sdk/x/group"
	"github.com/pkg/errors"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/dockerutil"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/stretchr/testify/require"
)

// SubmitGroupProposal submits a group proposal to the chain.
// TODO: This function should be part of `interchaintest`
// See https://github.com/strangelove-ventures/interchaintest/issues/1138
func SubmitGroupProposal(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string, prop *group.MsgSubmitProposal) string {
	file := "proposal.json"
	propJson, err := json.MarshalIndent(prop, "", " ")
	require.NoError(t, err)

	tn := chain.GetNode()

	fw := dockerutil.NewFileWriter(nil, tn.DockerClient, tn.TestName)
	err = fw.WriteFile(ctx, tn.VolumeName, file, propJson)
	require.NoError(t, err)

	submitCommand := []string{
		"group", "submit-proposal",
		path.Join(tn.HomeDir(), file), "--gas", "auto",
	}

	return exec(ctx, t, chain, config, tn.TxCommand(accAddr, submitCommand...))
}

// QueryGroupProposal queries a group proposal on the chain.
// TODO: This function should be part of `interchaintest`
// See https://github.com/strangelove-ventures/interchaintest/issues/1138
func QueryGroupProposal(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, config *ibc.ChainConfig, proposalId string) (string, error) {
	query := []string{
		"group", "proposal", proposalId,
	}

	tn := chain.GetNode()

	o, _, err := tn.Exec(ctx, tn.QueryCommand(query...), config.Env)
	if err != nil {
		return "", errors.WithMessage(err, "failed to query group proposal")
	}

	var data interface{}
	if err := json.Unmarshal([]byte(o), &data); err != nil {
		return "", errors.WithMessage(err, "failed to unmarshal group proposal")

	}

	prettyJSON, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", errors.WithMessage(err, "failed to marshal group proposal")
	}

	return string(prettyJSON), nil
}

// VoteGroupProposal votes on a group proposal on the chain.
// TODO: This function should be part of `interchaintest`
// See https://github.com/strangelove-ventures/interchaintest/issues/1138
func VoteGroupProposal(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, config *ibc.ChainConfig, proposalId, accAddr, vote, metadata string) string {
	voteCommand := []string{
		"group", "vote", proposalId, accAddr, vote, metadata,
	}
	return exec(ctx, t, chain, config, chain.GetNode().TxCommand(accAddr, voteCommand...))
}

func ExecGroupProposal(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr, proposalId string) string {
	execCommand := []string{
		"group", "exec", proposalId,
	}
	return exec(ctx, t, chain, config, chain.GetNode().TxCommand(accAddr, execCommand...))
}

func exec(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, config *ibc.ChainConfig, command []string) string {
	tn := chain.GetNode()

	o, _, err := tn.Exec(ctx, command, config.Env)
	require.NoError(t, err)

	output := cosmos.CosmosTx{}
	err = json.Unmarshal([]byte(o), &output)
	require.NoError(t, err)

	err = testutil.WaitForBlocks(ctx, 3, tn)
	require.NoError(t, err)

	txResp, err := chain.GetTransaction(output.TxHash)
	require.NoError(t, err)

	// Check the transaction was successful
	require.Equal(t, uint32(0x0), txResp.Code)

	return txResp.TxHash
}
