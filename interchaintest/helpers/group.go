package helpers

import (
	"context"
	"encoding/json"
	"path"
	"strconv"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/dockerutil"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/x/group"
)

const groupModule = "group"

// SubmitGroupProposal submits a group proposal to the chain.
// TODO: This function should be part of `interchaintest`
// See https://github.com/strangelove-ventures/interchaintest/issues/1138
func SubmitGroupProposal(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, config *ibc.ChainConfig, keyName string, prop *group.MsgSubmitProposal) (string, error) {
	file := "proposal.json"
	propJSON, err := json.MarshalIndent(prop, "", " ")
	require.NoError(t, err)

	tn := chain.GetNode()

	fw := dockerutil.NewFileWriter(nil, tn.DockerClient, tn.TestName)
	err = fw.WriteFile(ctx, tn.VolumeName, file, propJSON)
	require.NoError(t, err)

	submitCommand := []string{
		groupModule, "submit-proposal",
		path.Join(tn.HomeDir(), file),
		cliGasFlag, "8000000",
		gasAdjustmentFlag, gasAdjustment,
	}

	return exec(ctx, chain, config, tn.TxCommand(keyName, submitCommand...))
}

// CreateGroupWithMetadata creates a single-member group with the supplied metadata.
func CreateGroupWithMetadata(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, keyName, metadata string) (string, error) {
	file := "members.json"

	type MembersWrapper struct {
		Members []group.MemberRequest `json:"members"`
	}

	members := MembersWrapper{
		Members: []group.MemberRequest{
			{
				Address:  keyName,
				Weight:   "1",
				Metadata: "user",
			},
		},
	}
	membersJSON, err := json.MarshalIndent(members, "", " ")
	require.NoError(t, err)

	tn := chain.GetNode()

	fw := dockerutil.NewFileWriter(nil, tn.DockerClient, tn.TestName)
	err = fw.WriteFile(ctx, tn.VolumeName, file, membersJSON)
	require.NoError(t, err)

	createCommand := []string{
		groupModule, "create-group",
		keyName, metadata,
		path.Join(tn.HomeDir(), file),
		cliGasFlag, "20000000",
		gasAdjustmentFlag, gasAdjustment,
	}

	return tn.ExecTx(ctx, keyName, createCommand...)
}

// VoteGroupProposal votes on a group proposal on the chain.
// TODO: This function should be part of `interchaintest`
// See https://github.com/strangelove-ventures/interchaintest/issues/1138
func VoteGroupProposal(ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, proposalID, accAddr, vote, metadata string) (string, error) {
	voteCommand := []string{
		groupModule, "vote", proposalID, accAddr, vote, metadata,
		cliGasFlag, "1000000",
		gasAdjustmentFlag, gasAdjustment,
	}
	return exec(ctx, chain, config, chain.GetNode().TxCommand(accAddr, voteCommand...))
}

// ExecGroupProposal executes an accepted group proposal on the chain.
func ExecGroupProposal(ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr, proposalID string) (string, error) {
	tn := chain.GetNode()

	execCommand := []string{
		groupModule, "exec", proposalID,
		cliGasFlag, "20000000",
		gasAdjustmentFlag, gasAdjustment,
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
	if err := json.Unmarshal(o, &output); err != nil {
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
