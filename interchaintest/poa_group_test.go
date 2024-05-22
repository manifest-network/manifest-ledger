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
	grouptypes "github.com/cosmos/cosmos-sdk/x/group"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	poatypes "github.com/strangelove-ventures/poa"
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
	groupInfo = grouptypes.GroupInfo{
		Id:          1,
		Admin:       groupAddr, // The Group Policy is the admin of the Group (--group-policy-as-admin)
		Metadata:    metadata,
		Version:     2,
		TotalWeight: "2",
		CreatedAt:   time.Now(),
	}

	member1 = grouptypes.Member{
		Address:  accAddr,
		Weight:   "1",
		Metadata: "user1",
		AddedAt:  time.Now(),
	}

	member2 = grouptypes.Member{
		Address:  acc2Addr,
		Weight:   "1",
		Metadata: "user2",
		AddedAt:  time.Now(),
	}

	groupMember1 = grouptypes.GroupMember{
		GroupId: 1,
		Member:  &member1,
	}

	groupMember2 = grouptypes.GroupMember{
		GroupId: 1,
		Member:  &member2,
	}

	groupPolicy = &grouptypes.GroupPolicyInfo{
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

	cancelUpgradeProposal = &upgradetypes.MsgCancelUpgrade{
		Authority: groupAddr,
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

	manifestDefaultProposal = &manifesttypes.MsgUpdateParams{
		Authority: groupAddr,
		Params: manifesttypes.Params{
			StakeHolders: []*manifesttypes.StakeHolders{
				{
					Address:    acc2Addr,
					Percentage: 100_000_000,
				},
			},
			Inflation: manifesttypes.NewInflation(false, 0, Denom),
		},
	}

	poaDefaultParams = &poatypes.Params{
		Admins:                 []string{groupAddr},
		AllowValidatorSelfExit: true,
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

	err := groupPolicy.SetDecisionPolicy(&grouptypes.ThresholdDecisionPolicy{
		Threshold: "1",
		Windows: &grouptypes.DecisionPolicyWindows{
			VotingPeriod:       10 * time.Second,
			MinExecutionPeriod: 0 * time.Second,
		},
	})
	require.NoError(t, err)

	// TODO: The following block is needed in order for the GroupPolicy to get properly serialized in the ModifyGenesis function
	// https://github.com/strangelove-ventures/interchaintest/issues/1138
	enc := AppEncoding()
	grouptypes.RegisterInterfaces(enc.InterfaceRegistry)
	cdc := codec.NewProtoCodec(enc.InterfaceRegistry)
	_, err = cdc.MarshalJSON(groupPolicy)
	require.NoError(t, err)

	groupGenesis := DefaultGenesis
	// Define the new Group and Group Policy to be used as the POA Admin
	groupGenesis = append(groupGenesis, cosmos.NewGenesisKV("app_state.group.group_seq", "1"))
	groupGenesis = append(groupGenesis, cosmos.NewGenesisKV("app_state.group.groups", []grouptypes.GroupInfo{groupInfo}))
	groupGenesis = append(groupGenesis, cosmos.NewGenesisKV("app_state.group.group_members", []grouptypes.GroupMember{groupMember1, groupMember2}))
	groupGenesis = append(groupGenesis, cosmos.NewGenesisKV("app_state.group.group_policy_seq", "1"))
	groupGenesis = append(groupGenesis, cosmos.NewGenesisKV("app_state.group.group_policies", []*grouptypes.GroupPolicyInfo{groupPolicy}))

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

	user1, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "user1", accMnemonic, DefaultGenesisAmt, chain)
	require.NoError(t, err)
	_, err = interchaintest.GetAndFundTestUserWithMnemonic(ctx, "user2", acc1Mnemonic, DefaultGenesisAmt, chain)
	require.NoError(t, err)

	// Make sure the chain's HomeDir and the GOCOVERDIR are the same
	require.Equal(t, internalGoCoverDir, chain.GetNode().HomeDir())

	// Software Upgrade
	testSoftwareUpgrade(t, ctx, chain, &cfgA, accAddr)
	// Manifest Params Update
	testManifestParamsUpdate(t, ctx, chain, &cfgA, accAddr)
	testManifestParamsUpdateWithInflation(t, ctx, chain, &cfgA, accAddr)
	testManifestParamsUpdateEmpty(t, ctx, chain, &cfgA, accAddr)
	// POA Update
	testPOAParamsUpdateEmpty(t, ctx, chain, &cfgA, accAddr)
	testPOAParamsUpdate(t, ctx, chain, &cfgA, accAddr, user1)

	t.Cleanup(func() {
		// Copy coverage files from the container
		CopyCoverageFromContainer(ctx, t, client, chain.GetNode().ContainerID(), chain.HomeDir())
	})
}

// testSoftwareUpgrade tests the submission, voting, and execution of a software upgrade proposal
func testSoftwareUpgrade(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string) {
	t.Log("\n===== TEST GROUP SOFTWARE UPGRADE =====")

	// Verify there is no upgrade plan
	plan, err := chain.UpgradeQueryPlan(ctx)
	require.NoError(t, err)
	require.Nil(t, plan)

	// Verify the Upgrade module authority is the Group address
	upgradeAuth, err := chain.UpgradeQueryAuthority(ctx)
	require.NoError(t, err)
	require.Equal(t, upgradeAuth, groupAddr)

	upgradeProposalAny, err := types.NewAnyWithValue(upgradeProposal)
	require.NoError(t, err)

	prop := createProposal(groupAddr, []string{accAddr}, []*types.Any{upgradeProposalAny}, "Software Upgrade Proposal", "Upgrade the software to the latest version")
	submitVoteAndExecProposal(ctx, t, chain, config, accAddr, prop)

	// Verify the upgrade plan is set
	plan, err = chain.UpgradeQueryPlan(ctx)
	require.NoError(t, err)
	require.Equal(t, planName, plan.Name)
	require.Equal(t, planHeight, plan.Height)

	// Cancel the upgrade
	cancelUpgradeProposalAny, err := types.NewAnyWithValue(cancelUpgradeProposal)
	require.NoError(t, err)

	prop = createProposal(groupAddr, []string{accAddr}, []*types.Any{cancelUpgradeProposalAny}, "Cancel Upgrade Proposal", "Cancel the software upgrade")
	submitVoteAndExecProposal(ctx, t, chain, config, accAddr, prop)

	// Verify the upgrade plan is cancelled
	plan, err = chain.UpgradeQueryPlan(ctx)
	require.NoError(t, err)
	require.Nil(t, plan)
}

// testManifestParamsUpdate tests the submission, voting, and execution of a manifest params update proposal
// This proposal tests for https://github.com/liftedinit/manifest-ledger/issues/61
func testManifestParamsUpdate(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string) {
	t.Log("\n===== TEST GROUP MANIFEST PARAMS UPDATE =====")
	t.Log("\n===== TEST FIX FOR https://github.com/liftedinit/manifest-ledger/issues/61 =====")

	// Verify the initial manifest params
	checkManifestParams(ctx, t, chain, &manifestDefaultProposal.Params)

	manifestUpdateProposalAny, err := types.NewAnyWithValue(manifestUpdateProposal)
	require.NoError(t, err)

	prop := createProposal(groupAddr, []string{accAddr}, []*types.Any{manifestUpdateProposalAny}, "Manifest Params Update Proposal (without Inflation param)", "Update the manifest params (without Inflation param). https://github.com/liftedinit/manifest-ledger/issues/61")
	submitVoteAndExecProposal(ctx, t, chain, config, accAddr, prop)

	// Verify the updated manifest params
	checkManifestParams(ctx, t, chain, &manifestUpdateProposal.Params)

	// Reset the manifest params back to the default
	resetManifestParams(t, ctx, chain, config, accAddr)
}

func testManifestParamsUpdateWithInflation(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string) {
	t.Log("\n===== TEST GROUP MANIFEST PARAMS UPDATE (WITH INFLATION) =====")
	// Verify the initial manifest params
	checkManifestParams(ctx, t, chain, &manifestDefaultProposal.Params)

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

	// Verify the updated manifest params
	checkManifestParams(ctx, t, chain, &manifestUpdateProposal2.Params)

	// Reset the manifest params back to the default
	resetManifestParams(t, ctx, chain, config, accAddr)
}

func testManifestParamsUpdateEmpty(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string) {
	t.Log("\n===== TEST GROUP MANIFEST PARAMS UPDATE (EMPTY) =====")
	// Verify the initial manifest params
	checkManifestParams(ctx, t, chain, &manifestDefaultProposal.Params)

	manifestUpdateEmptyProposal := &manifesttypes.MsgUpdateParams{
		Authority: groupAddr,
		Params:    manifesttypes.Params{},
	}
	manifestUpdateProposalAny, err := types.NewAnyWithValue(manifestUpdateEmptyProposal)
	require.NoError(t, err)

	prop := createProposal(groupAddr, []string{accAddr}, []*types.Any{manifestUpdateProposalAny}, "Manifest Params Update Proposal (empty)", "Update the manifest params (empty)")
	submitVoteAndExecProposal(ctx, t, chain, config, accAddr, prop)

	// Verify the updated manifest params
	checkManifestParams(ctx, t, chain, &manifestUpdateEmptyProposal.Params)

	// Reset the manifest params back to the default
	resetManifestParams(t, ctx, chain, config, accAddr)
}

// testPOAParamsUpdateEmpty tests the submission, voting, and execution of an empty POA params update proposal
// This proposal tests that the Admins field cannot be empty
func testPOAParamsUpdateEmpty(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string) {
	t.Log("\n===== TEST GROUP POA PARAMS UPDATE (EMPTY ADMINS) =====")
	poaUpdateProposal := &poatypes.MsgUpdateParams{
		Sender: groupAddr,
		Params: poatypes.Params{
			Admins:                 nil,
			AllowValidatorSelfExit: false,
		},
	}
	poaUpdateProposalAny, err := types.NewAnyWithValue(poaUpdateProposal)
	require.NoError(t, err)

	prop := createProposal(groupAddr, []string{accAddr}, []*types.Any{poaUpdateProposalAny}, "POA Params Update Proposal", "Update the POA params")
	submitVoteAndExecProposal(ctx, t, chain, config, accAddr, prop)

	// Verify the POA params are unchanged
	checkPOAParams(ctx, t, chain, poaDefaultParams)
}

// testPOAParamsUpdate tests the submission, voting, and execution of a POA params update proposal
func testPOAParamsUpdate(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string, user ibc.Wallet) {
	t.Log("\n===== TEST GROUP POA PARAMS UPDATE =====")
	poaUpdateProposal := &poatypes.MsgUpdateParams{
		Sender: groupAddr,
		Params: poatypes.Params{
			Admins:                 []string{accAddr},
			AllowValidatorSelfExit: false,
		},
	}
	poaUpdateProposalAny, err := types.NewAnyWithValue(poaUpdateProposal)
	require.NoError(t, err)

	prop := createProposal(groupAddr, []string{accAddr}, []*types.Any{poaUpdateProposalAny}, "POA Params Update Proposal", "Update the POA params")
	submitVoteAndExecProposal(ctx, t, chain, config, accAddr, prop)

	checkPOAParams(ctx, t, chain, &poaUpdateProposal.Params)

	// NOTE:
	// At this point, the POA_ADMIN_ADDRESS is still the Group address, but the POA module admin field is now `accAddr`
	// What this means is that the POA_ADMIN_ADDRESS is the authority for all CosmosSDK modules, including the Group module,
	// but the POA module admin field is the authority for the POA module itself.

	// Reset the POA Admin back to the Group address using the POA module admin field, i.e., `accAddr`
	// Resetting the POA Admin back to the Group address using a group proposal will NOT work
	r, err := helpers.POAUpdateParams(t, ctx, chain, user, groupAddr, true)
	require.NoError(t, err)
	require.NotNil(t, r)
	require.Equal(t, uint32(0x0), r.Code)

	// Verify the POA params are reset
	checkPOAParams(ctx, t, chain, poaDefaultParams)
}

// resetManifestParams resets the manifest params back to the default
func resetManifestParams(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string) {
	manifestDefaultProposalAny, err := types.NewAnyWithValue(manifestDefaultProposal)
	require.NoError(t, err)

	prop := createProposal(groupAddr, []string{accAddr}, []*types.Any{manifestDefaultProposalAny}, "Manifest Params Update Proposal (reset)", "Reset the manifest params to the default")
	submitVoteAndExecProposal(ctx, t, chain, config, accAddr, prop)

	checkManifestParams(ctx, t, chain, &manifestDefaultProposal.Params)
}

// checkManifestParams checks the manifest params against the expected params
func checkManifestParams(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, expectedParams *manifesttypes.Params) {
	resp, err := manifesttypes.NewQueryClient(chain.GetNode().GrpcConn).Params(ctx, &manifesttypes.QueryParamsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Params)
	if expectedParams.Inflation != nil {
		require.Equal(t, expectedParams.Inflation.MintDenom, resp.Params.Inflation.MintDenom)
		require.Equal(t, expectedParams.Inflation.YearlyAmount, resp.Params.Inflation.YearlyAmount)
		require.Equal(t, expectedParams.Inflation.AutomaticEnabled, resp.Params.Inflation.AutomaticEnabled)
	}
	require.Len(t, expectedParams.StakeHolders, len(resp.Params.StakeHolders))
	for i, sh := range expectedParams.StakeHolders {
		require.Equal(t, sh.Address, resp.Params.StakeHolders[i].Address)
		require.Equal(t, sh.Percentage, resp.Params.StakeHolders[i].Percentage)
	}
}

// checkPOAParams checks the POA params against the expected params
func checkPOAParams(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, expectedParams *poatypes.Params) {
	resp, err := poatypes.NewQueryClient(chain.GetNode().GrpcConn).Params(ctx, &poatypes.QueryParamsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Params)
	require.Len(t, resp.Params.Admins, len(expectedParams.Admins))
	for i, admin := range expectedParams.Admins {
		require.Equal(t, admin, resp.Params.Admins[i])
	}
	require.Equal(t, expectedParams.AllowValidatorSelfExit, resp.Params.AllowValidatorSelfExit)
}

// submitVoteAndExecProposal submits, votes, and executes a group proposal
func submitVoteAndExecProposal(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string, prop *grouptypes.MsgSubmitProposal) {
	pid := strconv.Itoa(proposalId)

	marshalProposal(t, prop)

	helpers.SubmitGroupProposal(ctx, t, chain, config, accAddr, prop)
	helpers.VoteGroupProposal(ctx, t, chain, config, pid, accAddr, grouptypes.VOTE_OPTION_YES.String(), metadata)
	helpers.ExecGroupProposal(ctx, t, chain, config, accAddr, pid)

	proposalId = proposalId + 1
}

// createProposal creates a group proposal
func createProposal(groupPolicyAddress string, proposers []string, messages []*types.Any, title string, summary string) *grouptypes.MsgSubmitProposal {
	return &grouptypes.MsgSubmitProposal{
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
func marshalProposal(t *testing.T, prop *grouptypes.MsgSubmitProposal) {
	enc := AppEncoding()
	grouptypes.RegisterInterfaces(enc.InterfaceRegistry)
	cdc := codec.NewProtoCodec(enc.InterfaceRegistry)
	_, err := cdc.MarshalJSON(prop)
	require.NoError(t, err)
}
