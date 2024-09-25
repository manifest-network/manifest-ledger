package interchaintest

import (
	"context"
	"fmt"
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
	"github.com/cosmos/gogoproto/proto"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/dockerutil"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	tokenfactorytypes "github.com/strangelove-ventures/tokenfactory/x/tokenfactory/types"
	"github.com/stretchr/testify/require"

	"github.com/liftedinit/manifest-ledger/interchaintest/helpers"
	manifesttypes "github.com/liftedinit/manifest-ledger/x/manifest/types"
)

const (
	groupAddr        = "manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj"
	planName         = "foobar"
	planHeight int64 = 200
	metadata         = "AQ=="
	tfDenom          = "foo"
	tfTicker         = "FOO"
	user1            = "user1"
	user2            = "user2"
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

	member1 = createMember(accAddr, "1", user1)
	member2 = createMember(acc2Addr, "1", user2)

	groupMember1 = createGroupMember(1, &member1)
	groupMember2 = createGroupMember(1, &member2)

	groupPolicy = createGroupPolicyInfo(groupAddr, 1, "policy metadata")

	tfFullDenom = fmt.Sprintf("factory/%s/%s", groupAddr, tfDenom)

	upgradeProposal       = createUpgradeProposal(groupAddr, planName, planHeight)
	cancelUpgradeProposal = createCancelUpgradeProposal(groupAddr)

	manifestBurnProposal    = createManifestBurnProposal(groupAddr, sdk.NewCoins(sdk.NewInt64Coin(Denom, 50)))
	bankSendProposal        = createBankSendProposal(groupAddr, accAddr, sdk.NewInt64Coin(Denom, 1))
	tfCreateProposal        = createTfCreateDenomProposal(groupAddr, tfDenom)
	tfMintProposal          = createTfMintProposal(groupAddr, sdk.NewInt64Coin(tfFullDenom, 1234), "")
	tfMintToProposal        = createTfMintProposal(groupAddr, sdk.NewInt64Coin(tfFullDenom, 4321), accAddr)
	tfBurnProposal          = createTfBurnProposal(groupAddr, sdk.NewInt64Coin(tfFullDenom, 1234), "")
	tfBurnFromProposal      = createTfBurnProposal(groupAddr, sdk.NewInt64Coin(tfFullDenom, 4321), accAddr)
	tfForceTransferProposal = createTfForceTransferProposal(groupAddr, sdk.NewInt64Coin(tfFullDenom, 1), accAddr, acc2Addr)
	tfChangeAdminProposal   = createTfChangeAdminProposal(groupAddr, tfFullDenom, accAddr)
	tfModifyProposal        = createTfModifyMetadataProposal(groupAddr, tfFullDenom, tfFullDenom, tfTicker, tfFullDenom, tfTicker, "The foo token description")
	proposalId              = 1
)

func TestGroupPOA(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	name := "group-poa"

	err := groupPolicy.SetDecisionPolicy(createThresholdDecisionPolicy("1", 10*time.Second, 0*time.Second))
	require.NoError(t, err)

	// TODO: The following block is needed in order for the GroupPolicy to get properly serialized in the ModifyGenesis function
	// https://github.com/strangelove-ventures/interchaintest/issues/1138
	enc := AppEncoding()
	grouptypes.RegisterInterfaces(enc.InterfaceRegistry)
	cdc := codec.NewProtoCodec(enc.InterfaceRegistry)
	_, err = cdc.MarshalJSON(groupPolicy)
	require.NoError(t, err)

	groupGenesis := createGroupGenesis()

	cfgA := LocalChainConfig
	cfgA.Name = name
	cfgA.ModifyGenesis = cosmos.ModifyGenesis(groupGenesis)
	cfgA.Env = []string{
		fmt.Sprintf("POA_ADMIN_ADDRESS=%s", groupAddr), // This is required in order for GetPoAAdmin to return the Group address
	}
	cfgA.WithCodeCoverage()

	// setup base chain
	chains := interchaintest.CreateChainWithConfig(t, numVals, numNodes, name, "", cfgA)
	chain := chains[0].(*cosmos.CosmosChain)

	ctx, ic, client, _ := interchaintest.BuildInitialChain(t, chains, false)

	_, err = interchaintest.GetAndFundTestUserWithMnemonic(ctx, user1, accMnemonic, DefaultGenesisAmt, chain)
	require.NoError(t, err)
	_, err = interchaintest.GetAndFundTestUserWithMnemonic(ctx, user2, acc1Mnemonic, DefaultGenesisAmt, chain)
	require.NoError(t, err)

	// Software Upgrade
	testSoftwareUpgrade(t, ctx, chain, &cfgA, accAddr)
	// Manifest module
	testManifestStakeholdersPayout(t, ctx, chain, &cfgA, accAddr)
	// TokenFactory
	testTokenCreate(t, ctx, chain, &cfgA, accAddr)
	// Bank
	testBankSend(t, ctx, chain, &cfgA, accAddr)
	testBankSendIllegal(t, ctx, chain, &cfgA, accAddr)
	t.Cleanup(func() {
		dockerutil.CopyCoverageFromContainer(ctx, t, client, chain.GetNode().ContainerID(), chain.HomeDir(), ExternalGoCoverDir)
		_ = ic.Close()
	})
}

// testSoftwareUpgrade tests the submission, voting, and execution of a software upgrade proposal
// The software upgrade plan is set and then cancelled
func testSoftwareUpgrade(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string) {
	t.Log("\n===== TEST GROUP SOFTWARE UPGRADE =====")
	verifyUpgradePlanIsNil(t, ctx, chain)
	verifyUpgradeAuthority(t, ctx, chain, groupAddr)

	// Set the upgrade plan
	createAndRunProposalSuccess(t, ctx, chain, config, accAddr, []*types.Any{createAny(t, &upgradeProposal)})
	verifyUpgradePlan(t, ctx, chain, &upgradetypes.Plan{Name: planName, Height: planHeight})

	// Cancel the upgrade
	createAndRunProposalSuccess(t, ctx, chain, config, accAddr, []*types.Any{createAny(t, &cancelUpgradeProposal)})
	verifyUpgradePlanIsNil(t, ctx, chain)
}

// testManifestStakeholdersPayout tests the submission, voting, and execution of a manifest stakeholders payout proposal.
// The stakeholders are paid out and the newly minted tokens are burned
func testManifestStakeholdersPayout(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string) {
	t.Log("\n===== TEST GROUP MANIFEST STAKEHOLDERS PAYOUT (MINT) & BURN =====")
	// Verify the initial balances
	verifyBalance(t, ctx, chain, accAddr, Denom, DefaultGenesisAmt)
	verifyBalance(t, ctx, chain, groupAddr, Denom, sdkmath.ZeroInt())

	// Stakeholders payout
	manifestPayoutProposal := createManifestPayoutProposal(groupAddr, []manifesttypes.PayoutPair{
		manifesttypes.NewPayoutPair(sdk.MustAccAddressFromBech32(acc3Addr), "umfx", 25),
		manifesttypes.NewPayoutPair(sdk.MustAccAddressFromBech32(acc4Addr), "umfx", 25),
	})
	createAndRunProposalSuccess(t, ctx, chain, config, accAddr, []*types.Any{createAny(t, &manifestPayoutProposal)})
	verifyBalance(t, ctx, chain, acc3Addr, Denom, sdkmath.NewInt(25))
	verifyBalance(t, ctx, chain, acc4Addr, Denom, sdkmath.NewInt(25))

	buildWallet(t, ctx, chain, acc3Addr, acc3Mnemonic)
	buildWallet(t, ctx, chain, acc4Addr, acc4Mnemonic)

	// Send back the funds to the Group address
	sendFunds(t, ctx, chain, acc3Addr, groupAddr, Denom, sdkmath.NewInt(25))
	verifyBalance(t, ctx, chain, acc3Addr, Denom, sdkmath.ZeroInt())

	sendFunds(t, ctx, chain, acc4Addr, groupAddr, Denom, sdkmath.NewInt(25))
	verifyBalance(t, ctx, chain, acc4Addr, Denom, sdkmath.ZeroInt())

	// Burn the newly minted tokens using a Group Proposal
	createAndRunProposalSuccess(t, ctx, chain, config, accAddr, []*types.Any{createAny(t, &manifestBurnProposal)})
	verifyBalance(t, ctx, chain, accAddr, Denom, DefaultGenesisAmt)
	verifyBalance(t, ctx, chain, acc3Addr, Denom, sdkmath.ZeroInt())
	verifyBalance(t, ctx, chain, acc4Addr, Denom, sdkmath.ZeroInt())
	verifyBalance(t, ctx, chain, groupAddr, Denom, sdkmath.ZeroInt())
}

// testBankSend tests the sending of funds from one account to another using a group proposal
func testBankSend(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string) {
	t.Log("\n===== TEST GROUP BANK SEND =====")

	// Verify the initial balances
	verifyBalance(t, ctx, chain, accAddr, Denom, DefaultGenesisAmt)
	verifyBalance(t, ctx, chain, groupAddr, Denom, sdkmath.ZeroInt())

	// Send funds from accAddr to groupAddr
	sendFunds(t, ctx, chain, accAddr, groupAddr, Denom, sdkmath.NewInt(1))
	verifyBalance(t, ctx, chain, accAddr, Denom, sdkmath.NewInt(DefaultGenesisAmt.Int64()-1))
	verifyBalance(t, ctx, chain, groupAddr, Denom, sdkmath.OneInt())

	// Send funds from groupAddr back to accAddr using a Group Proposal
	createAndRunProposalSuccess(t, ctx, chain, config, accAddr, []*types.Any{createAny(t, &bankSendProposal)})
	verifyBalance(t, ctx, chain, accAddr, Denom, DefaultGenesisAmt)
	verifyBalance(t, ctx, chain, groupAddr, Denom, sdkmath.ZeroInt())
}

func testBankSendIllegal(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string) {
	t.Log("\n===== TEST GROUP BANK SEND (INVALID SENDER - FAIL) =====")
	newProp := bankSendProposal
	newProp.FromAddress = accAddr
	newProp.ToAddress = acc2Addr

	// Verify initial balances
	verifyBalance(t, ctx, chain, accAddr, Denom, DefaultGenesisAmt)
	verifyBalance(t, ctx, chain, acc2Addr, Denom, DefaultGenesisAmt)

	// Send funds from groupAddr back to accAddr using a Group Proposal
	createAndRunProposalFailure(t, ctx, chain, config, accAddr, []*types.Any{createAny(t, &newProp)}, "msg does not have group policy authorization")

	// Verify the funds were not sent
	verifyBalance(t, ctx, chain, accAddr, Denom, DefaultGenesisAmt)
	verifyBalance(t, ctx, chain, acc2Addr, Denom, DefaultGenesisAmt)
}

// testTokenCreate tests the creation, modification, and admin transfer of a token using a group proposal
func testTokenCreate(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string) {
	t.Log("\n===== TEST GROUP TOKEN CREATION, MODIFICATION, MINT (-TO), BURN (-FROM), FORCE TRANSFER AND ADMIN CHANGE =====")
	createAndRunProposalSuccess(t, ctx, chain, config, accAddr, []*types.Any{createAny(t, &tfCreateProposal)})
	verifyTfAdmin(t, ctx, chain, tfFullDenom, groupAddr)

	// Modify token metadata
	createAndRunProposalSuccess(t, ctx, chain, config, accAddr, []*types.Any{createAny(t, &tfModifyProposal)})
	verifyBankDenomMetadata(t, ctx, chain, tfModifyProposal.Metadata)

	// Mint some token to groupAddr
	createAndRunProposalSuccess(t, ctx, chain, config, accAddr, []*types.Any{createAny(t, &tfMintProposal)})
	verifyBalance(t, ctx, chain, groupAddr, tfFullDenom, tfMintProposal.Amount.Amount)

	// Burn the token using a Group Proposal
	createAndRunProposalSuccess(t, ctx, chain, config, accAddr, []*types.Any{createAny(t, &tfBurnProposal)})
	verifyBalance(t, ctx, chain, groupAddr, tfFullDenom, sdkmath.ZeroInt())

	// Mint some token to accAddr
	createAndRunProposalSuccess(t, ctx, chain, config, accAddr, []*types.Any{createAny(t, &tfMintToProposal)})
	verifyBalance(t, ctx, chain, accAddr, tfFullDenom, tfMintToProposal.Amount.Amount)

	// Force transfer the token from accAddr to acc2Addr using a Group Proposal
	createAndRunProposalSuccess(t, ctx, chain, config, accAddr, []*types.Any{createAny(t, &tfForceTransferProposal)})
	verifyBalance(t, ctx, chain, accAddr, tfFullDenom, sdkmath.NewInt(4320))
	verifyBalance(t, ctx, chain, acc2Addr, tfFullDenom, tfForceTransferProposal.Amount.Amount)

	// Send the token from acc2Addr to accAddr
	sendFunds(t, ctx, chain, acc2Addr, accAddr, tfFullDenom, sdkmath.OneInt())

	// Verify the token was sent
	verifyBalance(t, ctx, chain, accAddr, tfFullDenom, sdkmath.NewInt(4321))
	verifyBalance(t, ctx, chain, acc2Addr, tfFullDenom, sdkmath.ZeroInt())

	// Burn the token from accAddr using a Group Proposal
	createAndRunProposalSuccess(t, ctx, chain, config, accAddr, []*types.Any{createAny(t, &tfBurnFromProposal)})
	verifyBalance(t, ctx, chain, accAddr, tfFullDenom, sdkmath.ZeroInt())

	// Transfer the token to accAddr
	createAndRunProposalSuccess(t, ctx, chain, config, accAddr, []*types.Any{createAny(t, &tfChangeAdminProposal)})
	verifyTfAdmin(t, ctx, chain, tfFullDenom, accAddr)
}

func _createAndRunProposal(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, proposer string, proposalAny []*types.Any) error {
	prop := createProposal(groupAddr, []string{proposer}, proposalAny, "Proposal", "Proposal")
	return submitVoteAndExecProposal(ctx, t, chain, config, accAddr, prop)
}

func createAndRunProposalSuccess(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, proposer string, proposalAny []*types.Any) {
	err := _createAndRunProposal(t, ctx, chain, config, proposer, proposalAny)
	require.NoError(t, err)
}

func createAndRunProposalFailure(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, proposer string, proposalAny []*types.Any, expectedErr string) {
	err := _createAndRunProposal(t, ctx, chain, config, proposer, proposalAny)
	require.Error(t, err)
	require.ErrorContains(t, err, expectedErr)
}

// submitVoteAndExecProposal submits, votes, and executes a group proposal
func submitVoteAndExecProposal(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, config *ibc.ChainConfig, keyName string, prop *grouptypes.MsgSubmitProposal) error {
	// Increment the proposal ID regardless of the outcome
	defer func() { proposalId++ }()

	pid := strconv.Itoa(proposalId)

	marshalProposal(t, prop)

	_, err := helpers.SubmitGroupProposal(ctx, t, chain, config, keyName, prop)
	if err != nil {
		return err
	}
	_, err = helpers.VoteGroupProposal(ctx, chain, config, pid, keyName, grouptypes.VOTE_OPTION_YES.String(), metadata)
	if err != nil {
		return err
	}
	_, err = helpers.ExecGroupProposal(ctx, chain, config, keyName, pid)
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

// createAny creates a types.Any from a proto.Message
func createAny(t *testing.T, msg proto.Message) *types.Any {
	anyV, err := types.NewAnyWithValue(msg)
	require.NoError(t, err)
	return anyV
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

func createGroupGenesis() []cosmos.GenesisKV {
	return append(DefaultGenesis,
		cosmos.NewGenesisKV("app_state.group.group_seq", "1"),
		cosmos.NewGenesisKV("app_state.group.groups", []grouptypes.GroupInfo{groupInfo}),
		cosmos.NewGenesisKV("app_state.group.group_members", []grouptypes.GroupMember{groupMember1, groupMember2}),
		cosmos.NewGenesisKV("app_state.group.group_policy_seq", "1"),
		cosmos.NewGenesisKV("app_state.group.group_policies", []*grouptypes.GroupPolicyInfo{groupPolicy}),
	)
}

func createMember(address, weight, metadata string) grouptypes.Member {
	return grouptypes.Member{
		Address:  address,
		Weight:   weight,
		Metadata: metadata,
		AddedAt:  time.Now(),
	}
}

func createGroupMember(groupID uint64, member *grouptypes.Member) grouptypes.GroupMember {
	return grouptypes.GroupMember{
		GroupId: groupID,
		Member:  member,
	}
}

func createGroupPolicyInfo(address string, groupID uint64, metadata string) *grouptypes.GroupPolicyInfo {
	return &grouptypes.GroupPolicyInfo{
		Address:  address,
		GroupId:  groupID,
		Admin:    address,
		Version:  1,
		Metadata: metadata,
	}
}

func createThresholdDecisionPolicy(threshold string, votingPeriod, minExecutionPeriod time.Duration) *grouptypes.ThresholdDecisionPolicy {
	return &grouptypes.ThresholdDecisionPolicy{
		Threshold: threshold,
		Windows: &grouptypes.DecisionPolicyWindows{
			VotingPeriod:       votingPeriod,
			MinExecutionPeriod: minExecutionPeriod,
		},
	}
}

func createUpgradeProposal(authority, planName string, planHeight int64) upgradetypes.MsgSoftwareUpgrade {
	return upgradetypes.MsgSoftwareUpgrade{
		Authority: authority,
		Plan: upgradetypes.Plan{
			Name:   planName,
			Height: planHeight,
			Info:   "{}",
		},
	}
}

func createCancelUpgradeProposal(authority string) upgradetypes.MsgCancelUpgrade {
	return upgradetypes.MsgCancelUpgrade{
		Authority: authority,
	}
}

func createManifestPayoutProposal(authority string, payouts []manifesttypes.PayoutPair) manifesttypes.MsgPayout {
	return manifesttypes.MsgPayout{
		Authority:   authority,
		PayoutPairs: payouts,
	}
}

func createManifestBurnProposal(sender string, amounts sdk.Coins) manifesttypes.MsgBurnHeldBalance {
	return manifesttypes.MsgBurnHeldBalance{
		Authority: sender,
		BurnCoins: amounts,
	}
}

func createBankSendProposal(from, to string, amount sdk.Coin) banktypes.MsgSend {
	return banktypes.MsgSend{
		FromAddress: from,
		ToAddress:   to,
		Amount:      sdk.Coins{amount},
	}
}

func createTfCreateDenomProposal(sender, subdenom string) tokenfactorytypes.MsgCreateDenom {
	return tokenfactorytypes.MsgCreateDenom{
		Sender:   sender,
		Subdenom: subdenom,
	}
}

func createTfMintProposal(sender string, amount sdk.Coin, mintTo string) tokenfactorytypes.MsgMint {
	return tokenfactorytypes.MsgMint{
		Sender:        sender,
		Amount:        amount,
		MintToAddress: mintTo,
	}
}

func createTfBurnProposal(sender string, amount sdk.Coin, burnFrom string) tokenfactorytypes.MsgBurn {
	return tokenfactorytypes.MsgBurn{
		Sender:          sender,
		Amount:          amount,
		BurnFromAddress: burnFrom,
	}
}

func createTfForceTransferProposal(sender string, amount sdk.Coin, from, to string) tokenfactorytypes.MsgForceTransfer {
	return tokenfactorytypes.MsgForceTransfer{
		Sender:              sender,
		Amount:              amount,
		TransferFromAddress: from,
		TransferToAddress:   to,
	}
}

func createTfChangeAdminProposal(sender, denom, newAdmin string) tokenfactorytypes.MsgChangeAdmin {
	return tokenfactorytypes.MsgChangeAdmin{
		Sender:   sender,
		Denom:    denom,
		NewAdmin: newAdmin,
	}
}

func createTfMetadata(base, denom, display, name, symbol, description string) banktypes.Metadata {
	return banktypes.Metadata{
		Base:        base,
		Display:     display,
		Name:        name,
		Symbol:      symbol,
		Description: description,
		DenomUnits: []*banktypes.DenomUnit{
			{
				Denom:    denom,
				Exponent: 0,
				Aliases:  []string{symbol},
			},
			{
				Denom:    symbol,
				Exponent: 6,
				Aliases:  []string{denom},
			},
		},
	}
}

func createTfModifyMetadataProposal(sender, denom, name, symbol, base, display, description string) tokenfactorytypes.MsgSetDenomMetadata {
	return tokenfactorytypes.MsgSetDenomMetadata{
		Sender:   sender,
		Metadata: createTfMetadata(base, denom, display, name, symbol, description),
	}
}

func verifyBalance(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, address, denom string, expected sdkmath.Int) {
	bal, err := chain.BankQueryBalance(ctx, address, denom)
	require.NoError(t, err)
	require.Equal(t, expected, bal, fmt.Sprintf("expected balance %s to be %s, got %s", address, expected, bal))
}

func buildWallet(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, address, mnemonic string) {
	_, err := chain.BuildWallet(ctx, address, mnemonic)
	require.NoError(t, err)
}

func _verifyUpgradePlan(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain) *upgradetypes.Plan {
	plan, err := chain.UpgradeQueryPlan(ctx)
	require.NoError(t, err)
	return plan
}

func verifyUpgradePlanIsNil(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain) {
	plan := _verifyUpgradePlan(t, ctx, chain)
	require.Nil(t, plan)
}

func verifyUpgradePlan(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, expectedPlan *upgradetypes.Plan) {
	plan := _verifyUpgradePlan(t, ctx, chain)
	require.NotNil(t, plan)
	require.Equal(t, expectedPlan.Name, plan.Name)
	require.Equal(t, expectedPlan.Height, plan.Height)
}

func verifyUpgradeAuthority(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, expectedAuthority string) {
	authority, err := chain.UpgradeQueryAuthority(ctx)
	require.NoError(t, err)
	require.Equal(t, expectedAuthority, authority)
}

func sendFunds(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, from, to, denom string, amount sdkmath.Int) {
	err := chain.SendFunds(ctx, from, ibc.WalletAmount{
		Address: to,
		Denom:   denom,
		Amount:  amount,
	})
	require.NoError(t, err)
}

func updatePOAParams(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, groupAddr string, allowValidatorSelfExit bool) {
	r, err := helpers.POAUpdateParams(ctx, chain, user, groupAddr, allowValidatorSelfExit)
	require.NoError(t, err)
	require.NotNil(t, r)
	require.Equal(t, uint32(0x0), r.Code)
}

func verifyBankDenomMetadata(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, expectedMetadata banktypes.Metadata) {
	meta, err := chain.BankQueryDenomMetadata(ctx, tfFullDenom)
	require.NoError(t, err)
	require.Equal(t, expectedMetadata, *meta)
}

func verifyTfAdmin(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, denom, expectedAdmin string) {
	resp, err := chain.TokenFactoryQueryAdmin(ctx, denom)
	require.NoError(t, err)
	require.Equal(t, expectedAdmin, resp.AuthorityMetadata.Admin)
}
