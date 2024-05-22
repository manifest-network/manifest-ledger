package interchaintest

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"testing"
	"time"

	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/x/group"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/stretchr/testify/require"

	"github.com/liftedinit/manifest-ledger/interchaintest/helpers"
	manifesttypes "github.com/liftedinit/manifest-ledger/x/manifest/types"
)

const (
	groupAddr        = "manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj"
	planName         = "foobar"
	planHeight int64 = 200
	metadata         = "AQ=="
)

var (
	groupInfo = group.GroupInfo{
		Id:          1,
		Admin:       groupAddr, // The Group Policy is the admin of the Group (--group-policy-as-admin)
		Metadata:    metadata,
		Version:     2,
		TotalWeight: "2",
		CreatedAt:   time.Now(),
	}

	member1 = group.Member{
		Address:  accAddr,
		Weight:   "1",
		Metadata: "user1",
		AddedAt:  time.Now(),
	}

	member2 = group.Member{
		Address:  acc2Addr,
		Weight:   "1",
		Metadata: "user2",
		AddedAt:  time.Now(),
	}

	groupMember1 = group.GroupMember{
		GroupId: 1,
		Member:  &member1,
	}

	groupMember2 = group.GroupMember{
		GroupId: 1,
		Member:  &member2,
	}

	groupPolicy = &group.GroupPolicyInfo{
		Address:  groupAddr,
		GroupId:  1,
		Admin:    groupAddr,
		Version:  1,
		Metadata: "policy metadata",
	}

	upgradeProposal = &upgradetypes.MsgSoftwareUpgrade{
		Authority: groupAddr,
		Plan: upgradetypes.Plan{
			Name:   planName,
			Height: planHeight,
			Info:   "{}",
		},
	}

	manifestUpdateProposal = &manifesttypes.MsgUpdateParams{
		Authority: groupAddr,
		Params: manifesttypes.Params{
			StakeHolders: []*manifesttypes.StakeHolders{
				{
					Address:    accAddr,
					Percentage: 50_000_000,
				},
				{
					Address:    acc2Addr,
					Percentage: 50_000_000,
				},
			},
		},
	}

	proposalId = 1
)

func TestGroupPOA(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// Same as ChainNode.HomeDir() but we need it before the chain is created
	// The node volume is always mounted at /var/cosmos-chain/[chain-name]
	// This is a hackish way to get the coverage files from the ephemeral containers
	name := "group-poa"
	internalGoCoverDir := path.Join("/var/cosmos-chain", name)

	err := groupPolicy.SetDecisionPolicy(&group.ThresholdDecisionPolicy{
		Threshold: "1",
		Windows: &group.DecisionPolicyWindows{
			VotingPeriod:       10 * time.Second,
			MinExecutionPeriod: 0 * time.Second,
		},
	})
	require.NoError(t, err)

	// TODO: The following block is needed in order for the GroupPolicy to get properly serialized in the ModifyGenesis function
	// https://github.com/strangelove-ventures/interchaintest/issues/1138
	enc := AppEncoding()
	group.RegisterInterfaces(enc.InterfaceRegistry)
	cdc := codec.NewProtoCodec(enc.InterfaceRegistry)
	_, err = cdc.MarshalJSON(groupPolicy)
	require.NoError(t, err)

	groupGenesis := DefaultGenesis
	// Define the new Group and Group Policy to be used as the POA Admin
	groupGenesis = append(groupGenesis, cosmos.NewGenesisKV("app_state.group.group_seq", "1"))
	groupGenesis = append(groupGenesis, cosmos.NewGenesisKV("app_state.group.groups", []group.GroupInfo{groupInfo}))
	groupGenesis = append(groupGenesis, cosmos.NewGenesisKV("app_state.group.group_members", []group.GroupMember{groupMember1, groupMember2}))
	groupGenesis = append(groupGenesis, cosmos.NewGenesisKV("app_state.group.group_policy_seq", "1"))
	groupGenesis = append(groupGenesis, cosmos.NewGenesisKV("app_state.group.group_policies", []*group.GroupPolicyInfo{groupPolicy}))

	// Set the POA Admin as the new Group
	groupGenesis = append(groupGenesis, cosmos.NewGenesisKV("app_state.poa.params.admins", []string{groupAddr}))

	// Disable automatic inflation
	groupGenesis = append(groupGenesis, cosmos.NewGenesisKV("app_state.manifest.params.inflation.automatic_enabled", false))
	groupGenesis = append(groupGenesis, cosmos.NewGenesisKV("app_state.manifest.params.inflation.yearly_amount", "0"))

	cfgA := LocalChainConfig
	cfgA.ModifyGenesis = cosmos.ModifyGenesis(groupGenesis)
	cfgA.Env = []string{
		fmt.Sprintf("GOCOVERDIR=%s", internalGoCoverDir),
		fmt.Sprintf("POA_ADMIN_ADDRESS=%s", groupAddr), // This is required in order for GetPoAAdmin to return the Group address
	}

	// setup base chain
	chains := interchaintest.CreateChainWithConfig(t, numVals, numNodes, name, "", cfgA)
	chain := chains[0].(*cosmos.CosmosChain)

	ctx, _, client, _ := interchaintest.BuildInitialChain(t, chains, false)

	_, err = interchaintest.GetAndFundTestUserWithMnemonic(ctx, "user1", accMnemonic, DefaultGenesisAmt, chain)
	require.NoError(t, err)
	_, err = interchaintest.GetAndFundTestUserWithMnemonic(ctx, "user2", acc1Mnemonic, DefaultGenesisAmt, chain)
	require.NoError(t, err)

	// Make sure the chain's HomeDir and the GOCOVERDIR are the same
	require.Equal(t, internalGoCoverDir, chain.GetNode().HomeDir())

	testSoftwareUpgrade(t, ctx, chain, &cfgA, accAddr)
	testManifestParamsUpdate(t, ctx, chain, &cfgA, accAddr)
	testManifestParamsUpdateWithInflation(t, ctx, chain, &cfgA, accAddr)

	t.Cleanup(func() {
		// Copy coverage files from the container
		CopyCoverageFromContainer(ctx, t, client, chain.GetNode().ContainerID(), chain.HomeDir())
	})
}

// testSoftwareUpgrade tests the submission, voting, and execution of a software upgrade proposal
func testSoftwareUpgrade(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string) {
	t.Log("\n===== TEST GROUP SOFTWARE UPGRADE =====")

	// Verify the Upgrade module authority is the Group address
	upgradeAuth, err := chain.UpgradeQueryAuthority(ctx)
	require.NoError(t, err)
	require.Equal(t, upgradeAuth, groupAddr)

	upgradeProposalAny, err := types.NewAnyWithValue(upgradeProposal)
	require.NoError(t, err)

	prop := createProposal(groupAddr, []string{accAddr}, []*types.Any{upgradeProposalAny}, "Software Upgrade Proposal", "Upgrade the software to the latest version")
	submitVoteAndExecProposal(ctx, t, chain, config, accAddr, prop)

	plan, err := chain.UpgradeQueryPlan(ctx)
	require.NoError(t, err)

	require.Equal(t, planName, plan.Name)
	require.Equal(t, planHeight, plan.Height)
}

// testManifestParamsUpdate tests the submission, voting, and execution of a manifest params update proposal
// This proposal tests for https://github.com/liftedinit/manifest-ledger/issues/61
func testManifestParamsUpdate(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string) {
	t.Log("\n===== TEST GROUP MANIFEST PARAMS UPDATE =====")
	t.Log("\n===== TEST FIX FOR https://github.com/liftedinit/manifest-ledger/issues/61 =====")
	manifestUpdateProposalAny, err := types.NewAnyWithValue(manifestUpdateProposal)
	require.NoError(t, err)

	prop := createProposal(groupAddr, []string{accAddr}, []*types.Any{manifestUpdateProposalAny}, "Manifest Params Update Proposal (without Inflation param)", "Update the manifest params (without Inflation param). https://github.com/liftedinit/manifest-ledger/issues/61")
	submitVoteAndExecProposal(ctx, t, chain, config, accAddr, prop)

	pc, err := chain.QueryParam(ctx, "manifest", "stakeholders")
	require.NoError(t, err)
	t.Log(pc)
}

func testManifestParamsUpdateWithInflation(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string) {
	t.Log("\n===== TEST GROUP MANIFEST PARAMS UPDATE (WITH INFLATION) =====")
	manifestUpdateProposal2 := manifestUpdateProposal
	manifestUpdateProposal2.Params.Inflation = &manifesttypes.Inflation{
		AutomaticEnabled: false,
		YearlyAmount:     200_000_000,
		MintDenom:        "umfx",
	}

	manifestUpdateProposalAny, err := types.NewAnyWithValue(manifestUpdateProposal)
	require.NoError(t, err)

	prop := createProposal(groupAddr, []string{accAddr}, []*types.Any{manifestUpdateProposalAny}, "Manifest Params Update Proposal (with Inflation param)", "Update the manifest params (with Inflation param)")
	submitVoteAndExecProposal(ctx, t, chain, config, accAddr, prop)

	pc, err := chain.QueryParam(ctx, "manifest", "stakeholders")
	require.NoError(t, err)
	t.Log(pc)
}

func submitVoteAndExecProposal(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string, prop *group.MsgSubmitProposal) {
	pid := strconv.Itoa(proposalId)

	marshalProposal(t, prop)

	helpers.SubmitGroupProposal(ctx, t, chain, config, accAddr, prop)
	helpers.VoteGroupProposal(ctx, t, chain, config, pid, accAddr, group.VOTE_OPTION_YES.String(), metadata)
	helpers.ExecGroupProposal(ctx, t, chain, config, accAddr, pid)

	proposalId = proposalId + 1
}

func createProposal(groupPolicyAddress string, proposers []string, messages []*types.Any, title string, summary string) *group.MsgSubmitProposal {
	return &group.MsgSubmitProposal{
		GroupPolicyAddress: groupPolicyAddress,
		Proposers:          proposers,
		Metadata:           metadata,
		Messages:           messages,
		Exec:               0,
		Title:              title,
		Summary:            summary,
	}
}

// marshalProposal is a hackish way to ensure the prop is properly serialized
// TODO: The following block is needed in order for the prop to get properly serialized
// https://github.com/strangelove-ventures/interchaintest/issues/1138
func marshalProposal(t *testing.T, prop *group.MsgSubmitProposal) {
	enc := AppEncoding()
	group.RegisterInterfaces(enc.InterfaceRegistry)
	cdc := codec.NewProtoCodec(enc.InterfaceRegistry)
	_, err := cdc.MarshalJSON(prop)
	require.NoError(t, err)
}
