package interchaintest

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
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

	upgradeProposal = upgradetypes.MsgSoftwareUpgrade{
		Authority: groupAddr,
		Plan: upgradetypes.Plan{
			Name:   planName,
			Height: planHeight,
			Info:   "{}",
		},
	}

	cancelUpgradeProposal = upgradetypes.MsgCancelUpgrade{
		Authority: groupAddr,
	}

	manifestUpdateProposal = manifesttypes.MsgUpdateParams{
		Authority: groupAddr,
		Params: manifesttypes.Params{
			StakeHolders: []*manifesttypes.StakeHolders{
				{
					Address:    acc3Addr,
					Percentage: 50_000_000,
				},
				{
					Address:    acc4Addr,
					Percentage: 50_000_000,
				},
			},
			Inflation: &manifesttypes.Inflation{
				AutomaticEnabled: false,
				YearlyAmount:     200_000_000,
				MintDenom:        "umfx",
			},
		},
	}

	manifestDefaultProposal = manifesttypes.MsgUpdateParams{
		Authority: groupAddr,
		Params: manifesttypes.Params{
			StakeHolders: []*manifesttypes.StakeHolders{
				{
					Address:    acc2Addr,
					Percentage: 100_000_000,
				},
			},
			Inflation: &manifesttypes.Inflation{
				AutomaticEnabled: false,
				YearlyAmount:     0,
				MintDenom:        Denom,
			},
		},
	}

	manifestPayoutProposal = manifesttypes.MsgPayoutStakeholders{
		Authority: groupAddr,
		Payout:    sdk.NewInt64Coin(Denom, 50),
	}

	tfBurnProposal = manifesttypes.MsgBurnHeldBalance{
		Sender:    groupAddr,
		BurnCoins: sdk.NewCoins(sdk.NewInt64Coin(Denom, 50)),
	}

	poaDefaultParams = poatypes.Params{
		Admins:                 []string{groupAddr},
		AllowValidatorSelfExit: true,
	}

	bankSendProposal = banktypes.MsgSend{
		FromAddress: groupAddr,
		ToAddress:   accAddr,
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(Denom, 1)),
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
	// Manifest module
	testManifestParamsUpdate(t, ctx, chain, &cfgA, accAddr)
	testManifestParamsUpdateWithInflation(t, ctx, chain, &cfgA, accAddr)
	testManifestParamsUpdateEmpty(t, ctx, chain, &cfgA, accAddr)
	testManifestStakeholdersPayout(t, ctx, chain, &cfgA, accAddr)
	// POA Update
	testPOAParamsUpdateEmpty(t, ctx, chain, &cfgA, accAddr)
	testPOAParamsUpdate(t, ctx, chain, &cfgA, accAddr, user1)
	// Bank
	testBankSend(t, ctx, chain, &cfgA, accAddr)
	testBankSendIllegal(t, ctx, chain, &cfgA, accAddr)

	t.Cleanup(func() {
		// Copy coverage files from the container
		CopyCoverageFromContainer(ctx, t, client, chain.GetNode().ContainerID(), chain.HomeDir())
	})
}

// testSoftwareUpgrade tests the submission, voting, and execution of a software upgrade proposal
// The software upgrade plan is set and then cancelled
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

	upgradeProposalAny, err := types.NewAnyWithValue(&upgradeProposal)
	require.NoError(t, err)

	prop := createProposal(groupAddr, []string{accAddr}, []*types.Any{upgradeProposalAny}, "Software Upgrade Proposal", "Upgrade the software to the latest version")
	err = submitVoteAndExecProposal(ctx, t, chain, config, accAddr, prop)
	require.NoError(t, err)

	// Verify the upgrade plan is set
	plan, err = chain.UpgradeQueryPlan(ctx)
	require.NoError(t, err)
	require.Equal(t, planName, plan.Name)
	require.Equal(t, planHeight, plan.Height)

	// Cancel the upgrade
	cancelUpgradeProposalAny, err := types.NewAnyWithValue(&cancelUpgradeProposal)
	require.NoError(t, err)

	prop = createProposal(groupAddr, []string{accAddr}, []*types.Any{cancelUpgradeProposalAny}, "Cancel Upgrade Proposal", "Cancel the software upgrade")
	err = submitVoteAndExecProposal(ctx, t, chain, config, accAddr, prop)
	require.NoError(t, err)

	// Verify the upgrade plan is cancelled
	plan, err = chain.UpgradeQueryPlan(ctx)
	require.NoError(t, err)
	require.Nil(t, plan)
}

// testManifestParamsUpdate tests the submission, voting, and execution of a manifest params update proposal
// This proposal tests for https://github.com/liftedinit/manifest-ledger/issues/61
// `nil` inflation parameter is allowed and should be handled correctly
func testManifestParamsUpdate(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string) {
	t.Log("\n===== TEST GROUP MANIFEST PARAMS UPDATE (NIL INFLATION PARAMETER - FAIL) =====")
	t.Log("\n===== TEST FIX FOR https://github.com/liftedinit/manifest-ledger/issues/61 =====")

	newProposal := manifestDefaultProposal
	newProposal.Params.Inflation = nil

	// Verify the initial manifest params
	checkManifestParams(ctx, t, chain, &manifestDefaultProposal.Params)

	manifestUpdateProposalAny, err := types.NewAnyWithValue(&newProposal)
	require.NoError(t, err)

	prop := createProposal(groupAddr, []string{accAddr}, []*types.Any{manifestUpdateProposalAny}, "Manifest Params Update Proposal (nil Inflation param)", "Update the manifest params (nil Inflation param). https://github.com/liftedinit/manifest-ledger/issues/61")
	err = submitVoteAndExecProposal(ctx, t, chain, config, accAddr, prop)
	require.Error(t, err)
	require.ErrorContains(t, err, manifesttypes.ErrInflationParamsNotSet.Error())

	// Verify the manifest params were not changed
	checkManifestParams(ctx, t, chain, &manifestDefaultProposal.Params)
}

// testManifestParamsUpdateWithInflation tests the submission, voting, and execution of a manifest params update proposal
func testManifestParamsUpdateWithInflation(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string) {
	t.Log("\n===== TEST GROUP MANIFEST PARAMS UPDATE =====")
	// Verify the initial manifest params
	checkManifestParams(ctx, t, chain, &manifestDefaultProposal.Params)

	manifestUpdateProposalAny, err := types.NewAnyWithValue(&manifestUpdateProposal)
	require.NoError(t, err)

	prop := createProposal(groupAddr, []string{accAddr}, []*types.Any{manifestUpdateProposalAny}, "Manifest Params Update Proposal", "Update the manifest params")
	err = submitVoteAndExecProposal(ctx, t, chain, config, accAddr, prop)
	require.NoError(t, err)

	// Verify the updated manifest params
	checkManifestParams(ctx, t, chain, &manifestUpdateProposal.Params)

	// Reset the manifest params back to the default
	resetManifestParams(t, ctx, chain, config, accAddr)
}

// testManifestParamsUpdateEmpty tests the submission, voting, and execution of an empty manifest params update proposal
func testManifestParamsUpdateEmpty(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string) {
	t.Log("\n===== TEST GROUP MANIFEST PARAMS UPDATE (EMPTY PARAM - FAIL) =====")
	// Verify the initial manifest params
	checkManifestParams(ctx, t, chain, &manifestDefaultProposal.Params)

	manifestUpdateEmptyProposal := &manifesttypes.MsgUpdateParams{
		Authority: groupAddr,
		Params:    manifesttypes.Params{},
	}
	manifestUpdateProposalAny, err := types.NewAnyWithValue(manifestUpdateEmptyProposal)
	require.NoError(t, err)

	prop := createProposal(groupAddr, []string{accAddr}, []*types.Any{manifestUpdateProposalAny}, "Manifest Params Update Proposal (empty)", "Update the manifest params (empty)")
	err = submitVoteAndExecProposal(ctx, t, chain, config, accAddr, prop)
	require.Error(t, err)
	require.ErrorContains(t, err, manifesttypes.ErrInflationParamsNotSet.Error())

	// Verify the manifest params were not changed
	checkManifestParams(ctx, t, chain, &manifestDefaultProposal.Params)
}

// testManifestStakeholdersPayout tests the submission, voting, and execution of a manifest stakeholders payout proposal.
// The stakeholders are paid out and the newly minted tokens are burned
func testManifestStakeholdersPayout(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string) {
	t.Log("\n===== TEST GROUP MANIFEST STAKEHOLDERS PAYOUT (MINT) & BURN =====")
	// Verify the initial balances
	accAddrInitialBal, err := chain.BankQueryBalance(ctx, accAddr, Denom)
	require.NoError(t, err)
	require.Equal(t, DefaultGenesisAmt, accAddrInitialBal)

	groupAddrInitialBal, err := chain.BankQueryBalance(ctx, groupAddr, Denom)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(0), groupAddrInitialBal)

	manifestUpdateProposalAny, err := types.NewAnyWithValue(&manifestUpdateProposal)
	require.NoError(t, err)

	prop := createProposal(groupAddr, []string{accAddr}, []*types.Any{manifestUpdateProposalAny}, "Manifest Params Update Proposal", "Update the manifest params")
	err = submitVoteAndExecProposal(ctx, t, chain, config, accAddr, prop)
	require.NoError(t, err)

	// Verify the stakeholders
	resp, err := manifesttypes.NewQueryClient(chain.GetNode().GrpcConn).Params(ctx, &manifesttypes.QueryParamsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Params.StakeHolders, 2)
	require.Equal(t, resp.Params.StakeHolders[0].Address, acc3Addr)
	require.Equal(t, resp.Params.StakeHolders[1].Address, acc4Addr)

	// Stakeholders payout
	manifestPayoutProposalAny, err := types.NewAnyWithValue(&manifestPayoutProposal)
	require.NoError(t, err)

	prop = createProposal(groupAddr, []string{accAddr}, []*types.Any{manifestPayoutProposalAny}, "Manifest Stakeholders Payout Proposal", "Payout the stakeholders")
	err = submitVoteAndExecProposal(ctx, t, chain, config, accAddr, prop)
	require.NoError(t, err)

	// Verify the funds were sent
	acc3AddrBal, err := chain.BankQueryBalance(ctx, acc3Addr, Denom)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(25), acc3AddrBal)

	acc4AddrBal, err := chain.BankQueryBalance(ctx, acc4Addr, Denom)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(25), acc4AddrBal)

	_, err = chain.BuildWallet(ctx, acc3Addr, acc3Mnemonic)
	require.NoError(t, err)
	_, err = chain.BuildWallet(ctx, acc4Addr, acc4Mnemonic)
	require.NoError(t, err)

	// Send back the funds to the Group address
	err = chain.SendFunds(ctx, acc3Addr, ibc.WalletAmount{
		Address: groupAddr,
		Denom:   Denom,
		Amount:  sdkmath.NewInt(25),
	})
	require.NoError(t, err)

	err = chain.SendFunds(ctx, acc4Addr, ibc.WalletAmount{
		Address: groupAddr,
		Denom:   Denom,
		Amount:  sdkmath.NewInt(25),
	})

	// Verify the funds were sent
	acc3AddrBal, err = chain.BankQueryBalance(ctx, acc3Addr, Denom)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(0), acc3AddrBal)

	acc4AddrBal, err = chain.BankQueryBalance(ctx, acc4Addr, Denom)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(0), acc4AddrBal)

	// Burn the newly minted tokens using a Group Proposal
	tfBurnProposalAcc3 := tfBurnProposal
	tfBurnProposalAny, err := types.NewAnyWithValue(&tfBurnProposalAcc3)
	require.NoError(t, err)

	prop = createProposal(groupAddr, []string{accAddr}, []*types.Any{tfBurnProposalAny}, "Token Factory Burn Proposal", "Burn the newly minted tokens")
	err = submitVoteAndExecProposal(ctx, t, chain, config, accAddr, prop)
	require.NoError(t, err)

	// Verify the funds
	accAddrBal, err := chain.BankQueryBalance(ctx, accAddr, Denom)
	require.NoError(t, err)
	require.Equal(t, DefaultGenesisAmt, accAddrBal)

	acc3AddrBal, err = chain.BankQueryBalance(ctx, acc3Addr, Denom)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(0), acc3AddrBal)

	acc4AddrBal, err = chain.BankQueryBalance(ctx, acc4Addr, Denom)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(0), acc4AddrBal)

	groupAddrBal, err := chain.BankQueryBalance(ctx, groupAddr, Denom)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(0), groupAddrBal)
}

// testPOAParamsUpdateEmpty tests the submission, voting, and execution of an empty POA params update proposal
// This proposal tests that the Admins field cannot be empty
func testPOAParamsUpdateEmpty(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string) {
	t.Log("\n===== TEST GROUP POA PARAMS UPDATE (EMPTY ADMINS - FAIL) =====")
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
	err = submitVoteAndExecProposal(ctx, t, chain, config, accAddr, prop)
	require.Error(t, err)
	require.ErrorContains(t, err, poatypes.ErrMustProvideAtLeastOneAddress.Error())

	// Verify the POA params are unchanged
	checkPOAParams(ctx, t, chain, &poaDefaultParams)
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
	err = submitVoteAndExecProposal(ctx, t, chain, config, accAddr, prop)
	require.NoError(t, err)

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
	checkPOAParams(ctx, t, chain, &poaDefaultParams)
}

// testBankSend tests the sending of funds from one account to another using a group proposal
func testBankSend(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string) {
	t.Log("\n===== TEST GROUP BANK SEND =====")

	// Verify the initial balances
	accAddrInitialBal, err := chain.BankQueryBalance(ctx, accAddr, Denom)
	require.NoError(t, err)
	require.Equal(t, DefaultGenesisAmt, accAddrInitialBal)

	groupAddrInitialBal, err := chain.BankQueryBalance(ctx, groupAddr, Denom)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(0), groupAddrInitialBal)

	// Send funds from accAddr to groupAddr
	err = chain.SendFunds(ctx, accAddr, ibc.WalletAmount{
		Address: groupAddr,
		Denom:   Denom,
		Amount:  sdkmath.NewInt(1),
	})
	require.NoError(t, err)

	// Verify the funds were sent
	groupAddrBal, err := chain.BankQueryBalance(ctx, groupAddr, Denom)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(1), groupAddrBal)

	// Send funds from groupAddr back to accAddr using a Group Proposal
	bankSendProposalAny, err := types.NewAnyWithValue(&bankSendProposal)
	require.NoError(t, err)

	prop := createProposal(groupAddr, []string{accAddr}, []*types.Any{bankSendProposalAny}, "Bank Send Proposal", "Send funds from groupAddr back to accAddr")
	err = submitVoteAndExecProposal(ctx, t, chain, config, accAddr, prop)
	require.NoError(t, err)

	// Verify the funds were sent
	accAddrBal, err := chain.BankQueryBalance(ctx, accAddr, Denom)
	require.NoError(t, err)
	require.Equal(t, DefaultGenesisAmt, accAddrBal)

	groupAddrBal, err = chain.BankQueryBalance(ctx, groupAddr, Denom)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(0), groupAddrBal)
}

func testBankSendIllegal(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string) {
	t.Log("\n===== TEST GROUP BANK SEND (INVALID SENDER - FAIL) =====")
	newProp := bankSendProposal
	newProp.FromAddress = accAddr
	newProp.ToAddress = acc2Addr

	// Verify initial balances
	accAddrBal, err := chain.BankQueryBalance(ctx, accAddr, Denom)
	require.NoError(t, err)
	require.Equal(t, DefaultGenesisAmt, accAddrBal)

	acc2AddrBal, err := chain.BankQueryBalance(ctx, acc2Addr, Denom)
	require.NoError(t, err)
	require.Equal(t, DefaultGenesisAmt, acc2AddrBal)

	// Send funds from groupAddr back to accAddr using a Group Proposal
	bankSendProposalAny, err := types.NewAnyWithValue(&newProp)
	require.NoError(t, err)

	prop := createProposal(groupAddr, []string{accAddr}, []*types.Any{bankSendProposalAny}, "Bank Send Proposal (invalid sender)", "Should not be executed")
	err = submitVoteAndExecProposal(ctx, t, chain, config, accAddr, prop)
	require.Error(t, err)
	require.ErrorContains(t, err, "msg does not have group policy authorization")

	// Verify the funds were not sent
	accAddrBal, err = chain.BankQueryBalance(ctx, accAddr, Denom)
	require.NoError(t, err)
	require.Equal(t, DefaultGenesisAmt, accAddrBal)

	acc2AddrBal, err = chain.BankQueryBalance(ctx, acc2Addr, Denom)
	require.NoError(t, err)
	require.Equal(t, DefaultGenesisAmt, acc2AddrBal)
}

// resetManifestParams resets the manifest params back to the default
func resetManifestParams(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string) {
	manifestDefaultProposalAny, err := types.NewAnyWithValue(&manifestDefaultProposal)
	require.NoError(t, err)

	prop := createProposal(groupAddr, []string{accAddr}, []*types.Any{manifestDefaultProposalAny}, "Manifest Params Update Proposal (reset)", "Reset the manifest params to the default")
	err = submitVoteAndExecProposal(ctx, t, chain, config, accAddr, prop)
	require.NoError(t, err)

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
func submitVoteAndExecProposal(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string, prop *grouptypes.MsgSubmitProposal) error {
	// Increment the proposal ID regardless of the outcome
	defer func() { proposalId++ }()

	pid := strconv.Itoa(proposalId)

	marshalProposal(t, prop)

	_, err := helpers.SubmitGroupProposal(ctx, t, chain, config, accAddr, prop)
	if err != nil {
		return err
	}
	_, err = helpers.VoteGroupProposal(ctx, t, chain, config, pid, accAddr, grouptypes.VOTE_OPTION_YES.String(), metadata)
	if err != nil {
		return err
	}
	_, err = helpers.ExecGroupProposal(ctx, t, chain, config, accAddr, pid)
	if err != nil {
		return err
	}

	return nil
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
