package interchaintest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
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
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	tokenfactorytypes "github.com/strangelove-ventures/tokenfactory/x/tokenfactory/types"
	"github.com/stretchr/testify/require"

	"github.com/manifest-network/manifest-ledger/interchaintest/helpers"
	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
	manifesttypes "github.com/manifest-network/manifest-ledger/x/manifest/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
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

	wasmStoreProposal = createWasmStoreProposal(groupAddr, wasmFile)

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
	wasmGenesis := append(groupGenesis,
		cosmos.NewGenesisKV("app_state.wasm.params.code_upload_access.permission", "AnyOfAddresses"),
		cosmos.NewGenesisKV("app_state.wasm.params.code_upload_access.addresses", []string{groupAddr}), // Only the Group address can upload code
	)

	cfgA := LocalChainConfig
	cfgA.Name = name
	cfgA.ModifyGenesis = cosmos.ModifyGenesis(wasmGenesis)
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

	// CosmWasm store and instantiate
	testWasmContract(t, ctx, chain, &cfgA, accAddr)
	testWasmContractInvalidUploader(t, ctx, chain, accAddr)
	testWasmContractInvalidInstantiater(t, ctx, chain, accAddr)
	// Software Upgrade
	testSoftwareUpgrade(t, ctx, chain, &cfgA, accAddr)
	// Manifest module
	testManifestStakeholdersPayout(t, ctx, chain, &cfgA, accAddr)
	// TokenFactory
	testTokenCreate(t, ctx, chain, &cfgA, accAddr)
	// Bank
	testBankSend(t, ctx, chain, &cfgA, accAddr)
	testBankSendIllegal(t, ctx, chain, &cfgA, accAddr)
	// SKU Module - Provider and SKU Management via Group
	testGroupProviderAndSKU(t, ctx, chain, &cfgA, accAddr)
	// Billing Module - Lease Creation via Group
	testGroupLeaseCreation(t, ctx, chain, &cfgA, accAddr)

	t.Cleanup(func() {
		dockerutil.CopyCoverageFromContainer(ctx, t, client, chain.GetNode().ContainerID(), chain.HomeDir(), ExternalGoCoverDir)
		_ = ic.Close()
	})
}

// testWasmStore tests the submission, voting, and execution of a wasm store proposal
func testWasmContract(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, accAddr string) {
	t.Log("\n===== TEST GROUP WASM STORE AND INSTANTIATE =====")

	// Store the wasm code
	createAndRunProposalSuccess(t, ctx, chain, config, accAddr, []*types.Any{createAny(t, &wasmStoreProposal)})

	// Query the code ID
	codeId := queryLatestCodeId(t, ctx, chain)
	require.Equal(t, uint64(1), codeId)

	wasmInstantiateProposal := createWasmInstantiateProposal(groupAddr, codeId, `{"count":1}`)
	createAndRunProposalSuccess(t, ctx, chain, config, accAddr, []*types.Any{createAny(t, &wasmInstantiateProposal)})

	// Query the contract address
	contractAddr := queryLatestContractAddress(t, ctx, chain, codeId)
	require.NotEmpty(t, contractAddr)

	// Query contract state to verify instantiation
	var resp GetCountResponse
	err := chain.QueryContract(ctx, contractAddr, `{"get_count":{}}`, &resp)
	require.NoError(t, err)
	require.Equal(t, 1, resp.Data.Count)
}

// Only the POA admin should be able to store contracts
func testWasmContractInvalidUploader(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, accAddr string) {
	t.Log("\n===== TEST GROUP WASM STORE AND INSTANTIATE (INVALID UPLOADER) =====")

	_, err := chain.GetNode().StoreContract(ctx, accAddr, wasmFile)
	require.Error(t, err)
	require.ErrorContains(t, err, "can not create code: unauthorized")
}

// Only the POA admin should be able to instantiate contracts
func testWasmContractInvalidInstantiater(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, accAddr string) {
	t.Log("\n===== TEST GROUP WASM STORE AND INSTANTIATE (INVALID INSTANTIATER) =====")
	codeId := queryLatestCodeId(t, ctx, chain)
	require.Equal(t, uint64(1), codeId)

	_, err := chain.InstantiateContract(ctx, accAddr, strconv.FormatUint(codeId, 10), `{"count":1}`, true)
	require.Error(t, err)
	require.ErrorContains(t, err, "can not instantiate: unauthorized")
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
	marshalProposal(t, prop)

	txHash, err := helpers.SubmitGroupProposal(ctx, t, chain, config, keyName, prop)
	if err != nil {
		return err
	}

	// Get the proposal ID from the transaction response
	txResp, err := chain.GetTransaction(txHash)
	if err != nil {
		return err
	}
	var pid string
	for _, ev := range txResp.Events {
		if ev.GetType() != "cosmos.group.v1.EventSubmitProposal" {
			continue
		}
		for _, attr := range ev.GetAttributes() {
			if attr.Key == "proposal_id" {
				pid = attr.Value
			}
		}
	}
	if pid == "" {
		return fmt.Errorf("failed to get proposal ID")
	}
	cleanedPid := strings.ReplaceAll(pid, "\"", "")

	_, err = helpers.VoteGroupProposal(ctx, chain, config, cleanedPid, keyName, grouptypes.VOTE_OPTION_YES.String(), metadata)
	if err != nil {
		return err
	}
	_, err = helpers.ExecGroupProposal(ctx, chain, config, keyName, cleanedPid)
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

func createWasmStoreProposal(sender string, wasmFile string) wasmtypes.MsgStoreCode {
	wasmBytes, err := os.ReadFile(wasmFile)
	if err != nil {
		panic(fmt.Sprintf("failed to read wasm file: %v", err))
	}

	return wasmtypes.MsgStoreCode{
		Sender:       sender,
		WASMByteCode: wasmBytes,
		InstantiatePermission: &wasmtypes.AccessConfig{
			Permission: wasmtypes.AccessTypeAnyOfAddresses,
			Addresses:  []string{groupAddr}, // Only the Group address can instantiate the contract
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

func createWasmInstantiateProposal(sender string, codeId uint64, msg string) wasmtypes.MsgInstantiateContract {
	return wasmtypes.MsgInstantiateContract{
		Sender: sender,
		Admin:  sender, // Set group as admin
		CodeID: codeId,
		Label:  "wasm-contract",
		Msg:    []byte(msg),
		Funds:  sdk.Coins{},
	}
}

func queryLatestCodeId(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain) uint64 {
	stdout, _, err := chain.GetNode().ExecQuery(ctx, "wasm", "list-code", "--reverse")
	require.NoError(t, err)

	var res struct {
		CodeInfos []struct {
			CodeID string `json:"code_id"`
		} `json:"code_infos"`
	}
	err = json.Unmarshal(stdout, &res)
	require.NoError(t, err)
	require.NotEmpty(t, res.CodeInfos)

	codeId, err := strconv.ParseUint(res.CodeInfos[0].CodeID, 10, 64)
	require.NoError(t, err)
	return codeId
}

func queryLatestContractAddress(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, codeId uint64) string {
	stdout, _, err := chain.GetNode().ExecQuery(ctx, "wasm", "list-contract-by-code", fmt.Sprintf("%d", codeId))
	require.NoError(t, err)

	var res struct {
		Contracts []string `json:"contracts"`
	}
	err = json.Unmarshal(stdout, &res)
	require.NoError(t, err)
	require.NotEmpty(t, res.Contracts)

	return res.Contracts[len(res.Contracts)-1]
}

// testGroupProviderAndSKU tests provider and SKU creation/management via group proposals.
func testGroupProviderAndSKU(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, proposerAddr string) {
	t.Log("\n===== TEST GROUP PROVIDER AND SKU MANAGEMENT =====")

	// Create provider via group proposal
	var providerUUID string
	t.Run("create_provider_via_proposal", func(t *testing.T) {
		createProviderMsg := createSKUCreateProviderProposal(groupAddr, groupAddr, groupAddr, nil)
		createAndRunProposalSuccess(t, ctx, chain, config, proposerAddr, []*types.Any{createAny(t, &createProviderMsg)})

		// Query providers to get the created provider ID
		providers, err := helpers.SKUQueryProviders(ctx, chain)
		require.NoError(t, err)
		require.NotEmpty(t, providers.Providers, "should have at least one provider")
		providerUUID = providers.Providers[len(providers.Providers)-1].Uuid
		t.Logf("Created provider ID: %s", providerUUID)

		// Verify provider was created correctly
		provider, err := helpers.SKUQueryProvider(ctx, chain, providerUUID)
		require.NoError(t, err)
		require.Equal(t, groupAddr, provider.Provider.Address)
		require.Equal(t, groupAddr, provider.Provider.PayoutAddress)
		require.True(t, provider.Provider.Active)
	})

	// Create SKU via group proposal
	var skuUUID string
	t.Run("create_sku_via_proposal", func(t *testing.T) {
		// Price: 3600 umfx per hour (evenly divisible by 3600 seconds)
		basePrice := sdk.NewInt64Coin(Denom, 3600)
		createSKUMsg := createSKUCreateSKUProposal(groupAddr, providerUUID, "Compute Small", skutypes.Unit_UNIT_PER_HOUR, basePrice, nil)
		createAndRunProposalSuccess(t, ctx, chain, config, proposerAddr, []*types.Any{createAny(t, &createSKUMsg)})

		// Query SKUs to get the created SKU ID
		skus, err := helpers.SKUQuerySKUs(ctx, chain)
		require.NoError(t, err)
		require.NotEmpty(t, skus.Skus, "should have at least one SKU")
		skuUUID = skus.Skus[len(skus.Skus)-1].Uuid
		t.Logf("Created SKU ID: %s", skuUUID)

		// Verify SKU was created correctly
		sku, err := helpers.SKUQuerySKU(ctx, chain, skuUUID)
		require.NoError(t, err)
		require.Equal(t, providerUUID, sku.Sku.ProviderUuid)
		require.Equal(t, "Compute Small", sku.Sku.Name)
		require.True(t, sku.Sku.Active)
	})

	// Update provider via group proposal
	t.Run("update_provider_via_proposal", func(t *testing.T) {
		updateProviderMsg := createSKUUpdateProviderProposal(groupAddr, providerUUID, groupAddr, acc2Addr, true, []byte("cafebabe"))
		createAndRunProposalSuccess(t, ctx, chain, config, proposerAddr, []*types.Any{createAny(t, &updateProviderMsg)})

		// Verify update
		provider, err := helpers.SKUQueryProvider(ctx, chain, providerUUID)
		require.NoError(t, err)
		require.Equal(t, acc2Addr, provider.Provider.PayoutAddress)
	})

	// Update SKU via group proposal
	t.Run("update_sku_via_proposal", func(t *testing.T) {
		// Updated price: 7200 umfx per hour
		updatedPrice := sdk.NewInt64Coin(Denom, 7200)
		updateSKUMsg := createSKUUpdateSKUProposal(groupAddr, skuUUID, providerUUID, "Compute Medium", skutypes.Unit_UNIT_PER_HOUR, updatedPrice, true, nil)
		createAndRunProposalSuccess(t, ctx, chain, config, proposerAddr, []*types.Any{createAny(t, &updateSKUMsg)})

		// Verify update
		sku, err := helpers.SKUQuerySKU(ctx, chain, skuUUID)
		require.NoError(t, err)
		require.Equal(t, "Compute Medium", sku.Sku.Name)
		require.Equal(t, sdkmath.NewInt(7200), sku.Sku.BasePrice.Amount)
	})

	// Create another SKU for deactivation testing
	var skuToDeactivateUUID string
	t.Run("create_sku_for_deactivation", func(t *testing.T) {
		basePrice := sdk.NewInt64Coin(Denom, 86400) // 86400 umfx per day
		createSKUMsg := createSKUCreateSKUProposal(groupAddr, providerUUID, "Storage Large", skutypes.Unit_UNIT_PER_DAY, basePrice, nil)
		createAndRunProposalSuccess(t, ctx, chain, config, proposerAddr, []*types.Any{createAny(t, &createSKUMsg)})

		// Get the SKU ID
		skus, err := helpers.SKUQuerySKUs(ctx, chain)
		require.NoError(t, err)
		skuToDeactivateUUID = skus.Skus[len(skus.Skus)-1].Uuid
	})

	// Deactivate SKU via group proposal
	t.Run("deactivate_sku_via_proposal", func(t *testing.T) {
		deactivateSKUMsg := createSKUDeactivateSKUProposal(groupAddr, skuToDeactivateUUID)
		createAndRunProposalSuccess(t, ctx, chain, config, proposerAddr, []*types.Any{createAny(t, &deactivateSKUMsg)})

		// Verify deactivation
		sku, err := helpers.SKUQuerySKU(ctx, chain, skuToDeactivateUUID)
		require.NoError(t, err)
		require.False(t, sku.Sku.Active, "SKU should be inactive after deactivation")
	})

	// Create another provider for deactivation testing
	var providerToDeactivateUUID string
	t.Run("create_provider_for_deactivation", func(t *testing.T) {
		createProviderMsg := createSKUCreateProviderProposal(groupAddr, acc3Addr, acc3Addr, nil)
		createAndRunProposalSuccess(t, ctx, chain, config, proposerAddr, []*types.Any{createAny(t, &createProviderMsg)})

		providers, err := helpers.SKUQueryProviders(ctx, chain)
		require.NoError(t, err)
		providerToDeactivateUUID = providers.Providers[len(providers.Providers)-1].Uuid
	})

	// Deactivate provider via group proposal
	t.Run("deactivate_provider_via_proposal", func(t *testing.T) {
		deactivateProviderMsg := createSKUDeactivateProviderProposal(groupAddr, providerToDeactivateUUID)
		createAndRunProposalSuccess(t, ctx, chain, config, proposerAddr, []*types.Any{createAny(t, &deactivateProviderMsg)})

		// Verify deactivation
		provider, err := helpers.SKUQueryProvider(ctx, chain, providerToDeactivateUUID)
		require.NoError(t, err)
		require.False(t, provider.Provider.Active, "Provider should be inactive after deactivation")
	})
}

// testGroupLeaseCreation tests lease creation via group proposals (as POA admin authority).
func testGroupLeaseCreation(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, config *ibc.ChainConfig, proposerAddr string) {
	t.Log("\n===== TEST GROUP LEASE CREATION =====")

	node := chain.GetNode()

	// First, create a PWR denom for billing using tokenfactory via group proposal
	var pwrDenom string
	t.Run("create_pwr_denom_via_proposal", func(t *testing.T) {
		pwrSubdenom := "upwr"
		createDenomMsg := tokenfactorytypes.MsgCreateDenom{
			Sender:   groupAddr,
			Subdenom: pwrSubdenom,
		}
		createAndRunProposalSuccess(t, ctx, chain, config, proposerAddr, []*types.Any{createAny(t, &createDenomMsg)})
		pwrDenom = fmt.Sprintf("factory/%s/%s", groupAddr, pwrSubdenom)
		t.Logf("Created PWR denom: %s", pwrDenom)
	})

	// Mint PWR tokens to group address for distribution
	t.Run("mint_pwr_tokens_via_proposal", func(t *testing.T) {
		mintMsg := tokenfactorytypes.MsgMint{
			Sender:        groupAddr,
			Amount:        sdk.NewInt64Coin(pwrDenom, 1_000_000_000_000),
			MintToAddress: groupAddr,
		}
		createAndRunProposalSuccess(t, ctx, chain, config, proposerAddr, []*types.Any{createAny(t, &mintMsg)})

		// Verify minting
		balance, err := chain.GetBalance(ctx, groupAddr, pwrDenom)
		require.NoError(t, err)
		require.True(t, balance.GT(sdkmath.ZeroInt()), "group should have PWR balance")
	})

	// Update billing params via group proposal
	t.Run("update_billing_params_via_proposal", func(t *testing.T) {
		// minLeaseDuration = 10 seconds for testing (quick lease tests)
		updateParamsMsg := billingtypes.MsgUpdateParams{
			Authority: groupAddr,
			Params: billingtypes.Params{
				MaxLeasesPerTenant:        100,
				MaxItemsPerLease:          20,
				MinLeaseDuration:          10,
				MaxPendingLeasesPerTenant: 10,
				PendingTimeout:            1800,
				AllowedList:               nil, // Empty allowed list - only authority can create leases for tenants
			},
		}
		createAndRunProposalSuccess(t, ctx, chain, config, proposerAddr, []*types.Any{createAny(t, &updateParamsMsg)})

		// Verify params
		params, err := helpers.BillingQueryParams(ctx, chain)
		require.NoError(t, err)
		require.Equal(t, uint64(10), params.Params.MinLeaseDuration)
	})

	// Get the first provider created in the previous test
	providers, err := helpers.SKUQueryProviders(ctx, chain)
	require.NoError(t, err)
	require.NotEmpty(t, providers.Providers)
	providerUUID := providers.Providers[0].Uuid

	// Create a new SKU priced in PWR for lease testing
	// (existing SKUs are priced in umfx, but we fund tenant credit with PWR)
	var activeSKUUUID string
	t.Run("create_pwr_sku_via_proposal", func(t *testing.T) {
		// Price: 7200 upwr per hour (2 upwr per second)
		basePrice := sdk.NewInt64Coin(pwrDenom, 7200)
		createSKUMsg := createSKUCreateSKUProposal(groupAddr, providerUUID, "Compute PWR", skutypes.Unit_UNIT_PER_HOUR, basePrice, nil)
		createAndRunProposalSuccess(t, ctx, chain, config, proposerAddr, []*types.Any{createAny(t, &createSKUMsg)})

		// Query SKUs to get the created SKU ID
		skus, err := helpers.SKUQuerySKUsByProvider(ctx, chain, providerUUID)
		require.NoError(t, err)
		require.NotEmpty(t, skus.Skus, "should have at least one SKU")
		// Find the PWR-priced SKU we just created
		for _, sku := range skus.Skus {
			if sku.Active && sku.BasePrice.Denom == pwrDenom {
				activeSKUUUID = sku.Uuid
				break
			}
		}
		require.NotZero(t, activeSKUUUID, "should have created a PWR-priced SKU")
		t.Logf("Created PWR SKU ID: %s with denom: %s", activeSKUUUID, pwrDenom)
	})

	// Create a tenant user for lease testing
	tenantUsers := interchaintest.GetAndFundTestUsers(t, ctx, "lease-tenant", DefaultGenesisAmt, chain)
	tenant := tenantUsers[0]
	tenantAddr := tenant.FormattedAddress()

	// Fund tenant's credit account via group proposal
	t.Run("fund_tenant_credit_via_proposal", func(t *testing.T) {
		// First send PWR from group to fund the credit
		// We need to fund credit using MsgFundCredit
		fundCreditMsg := billingtypes.MsgFundCredit{
			Sender: groupAddr,
			Tenant: tenantAddr,
			Amount: sdk.NewInt64Coin(pwrDenom, 100_000_000), // 100 PWR
		}

		// But first, we need to send PWR to the group address's bank account
		// so the FundCredit can transfer from there to the credit account
		// The group already has PWR from minting, so we can directly fund

		createAndRunProposalSuccess(t, ctx, chain, config, proposerAddr, []*types.Any{createAny(t, &fundCreditMsg)})

		// Verify credit account
		creditRes, err := helpers.BillingQueryCreditAccount(ctx, chain, tenantAddr)
		require.NoError(t, err)
		require.True(t, len(creditRes.Balances) > 0 && creditRes.Balances[0].Amount.IsPositive(), "tenant should have credit balance")
		t.Logf("Tenant credit balance: %s", creditRes.Balances)
	})

	// Create lease for tenant via group proposal (authority creates lease for tenant)
	var leaseID string
	t.Run("create_lease_for_tenant_via_proposal", func(t *testing.T) {
		createLeaseForTenantMsg := billingtypes.MsgCreateLeaseForTenant{
			Authority: groupAddr,
			Tenant:    tenantAddr,
			Items: []billingtypes.LeaseItemInput{
				{SkuUuid: activeSKUUUID, Quantity: 1},
			},
		}
		createAndRunProposalSuccess(t, ctx, chain, config, proposerAddr, []*types.Any{createAny(t, &createLeaseForTenantMsg)})

		// Query leases to get the created lease ID (query all, not just active, since lease is PENDING)
		leases, err := helpers.BillingQueryLeasesByTenant(ctx, chain, tenantAddr, "")
		require.NoError(t, err)
		require.NotEmpty(t, leases.Leases, "tenant should have at least one lease")
		leaseID = leases.Leases[len(leases.Leases)-1].Uuid
		t.Logf("Created lease UUID: %s for tenant: %s", leaseID, tenantAddr)

		// Verify lease is in PENDING state
		lease, err := helpers.BillingQueryLease(ctx, chain, leaseID)
		require.NoError(t, err)
		require.Equal(t, tenantAddr, lease.Lease.Tenant)
		require.Equal(t, billingtypes.LEASE_STATE_PENDING, lease.Lease.GetState())
	})

	// Acknowledge the lease via group proposal (provider is the group)
	t.Run("acknowledge_lease_via_proposal", func(t *testing.T) {
		ackLeaseMsg := billingtypes.MsgAcknowledgeLease{
			Sender:     groupAddr,
			LeaseUuids: []string{leaseID},
		}
		createAndRunProposalSuccess(t, ctx, chain, config, proposerAddr, []*types.Any{createAny(t, &ackLeaseMsg)})

		// Verify lease is now ACTIVE
		lease, err := helpers.BillingQueryLease(ctx, chain, leaseID)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_ACTIVE, lease.Lease.GetState())
	})

	// Tenant can close their own lease (even though authority created it)
	t.Run("tenant_closes_own_lease", func(t *testing.T) {
		res, err := helpers.BillingCloseLease(ctx, chain, tenant, leaseID)
		require.NoError(t, err)

		txRes, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "tenant should be able to close lease: %s", txRes.RawLog)

		// Verify lease is closed
		lease, err := helpers.BillingQueryLease(ctx, chain, leaseID)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_CLOSED, lease.Lease.GetState())
	})

	// Create another lease for withdrawal testing
	var withdrawLeaseID string
	t.Run("create_another_lease_for_tenant", func(t *testing.T) {
		createLeaseForTenantMsg := billingtypes.MsgCreateLeaseForTenant{
			Authority: groupAddr,
			Tenant:    tenantAddr,
			Items: []billingtypes.LeaseItemInput{
				{SkuUuid: activeSKUUUID, Quantity: 2},
			},
		}
		createAndRunProposalSuccess(t, ctx, chain, config, proposerAddr, []*types.Any{createAny(t, &createLeaseForTenantMsg)})

		// Query all leases (not just active since this one is PENDING)
		leases, err := helpers.BillingQueryLeasesByTenant(ctx, chain, tenantAddr, "")
		require.NoError(t, err)
		require.NotEmpty(t, leases.Leases)
		// Get the most recent PENDING lease
		for i := len(leases.Leases) - 1; i >= 0; i-- {
			if leases.Leases[i].GetState() == billingtypes.LEASE_STATE_PENDING {
				withdrawLeaseID = leases.Leases[i].Uuid
				break
			}
		}
		require.NotEmpty(t, withdrawLeaseID, "should find a pending lease")
	})

	// Acknowledge the withdrawal lease via group proposal
	t.Run("acknowledge_withdrawal_lease_via_proposal", func(t *testing.T) {
		ackLeaseMsg := billingtypes.MsgAcknowledgeLease{
			Sender:     groupAddr,
			LeaseUuids: []string{withdrawLeaseID},
		}
		createAndRunProposalSuccess(t, ctx, chain, config, proposerAddr, []*types.Any{createAny(t, &ackLeaseMsg)})

		// Verify lease is now ACTIVE
		lease, err := helpers.BillingQueryLease(ctx, chain, withdrawLeaseID)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_ACTIVE, lease.Lease.GetState())
	})

	// Wait for some accrual
	require.NoError(t, testutil.WaitForBlocks(ctx, 5, chain))

	// Withdraw via group proposal
	t.Run("withdraw_via_proposal", func(t *testing.T) {
		// Update provider payout address to acc2Addr (done in previous test)
		// so we verify withdrawal goes to the payout address
		provider, err := helpers.SKUQueryProvider(ctx, chain, providerUUID)
		require.NoError(t, err)
		payoutAddr := provider.Provider.PayoutAddress

		// Get initial balance
		initialBalance, err := chain.GetBalance(ctx, payoutAddr, pwrDenom)
		require.NoError(t, err)

		withdrawMsg := billingtypes.MsgWithdraw{
			Sender:    groupAddr,
			LeaseUuid: withdrawLeaseID,
		}
		createAndRunProposalSuccess(t, ctx, chain, config, proposerAddr, []*types.Any{createAny(t, &withdrawMsg)})

		// Verify provider received funds
		newBalance, err := chain.GetBalance(ctx, payoutAddr, pwrDenom)
		require.NoError(t, err)
		require.True(t, newBalance.GTE(initialBalance), "provider should have received funds")
		t.Logf("Provider payout address balance changed: %s -> %s", initialBalance, newBalance)
	})

	// Close lease via group proposal (authority can close any lease)
	t.Run("close_lease_via_proposal", func(t *testing.T) {
		closeLeaseMsg := billingtypes.MsgCloseLease{
			Sender:    groupAddr,
			LeaseUuid: withdrawLeaseID,
		}
		createAndRunProposalSuccess(t, ctx, chain, config, proposerAddr, []*types.Any{createAny(t, &closeLeaseMsg)})

		// Verify lease is closed
		lease, err := helpers.BillingQueryLease(ctx, chain, withdrawLeaseID)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_CLOSED, lease.Lease.GetState())
	})

	// Test withdraw all via group proposal
	t.Run("withdraw_all_via_proposal", func(t *testing.T) {
		// First create another lease for the tenant
		createLeaseForTenantMsg := billingtypes.MsgCreateLeaseForTenant{
			Authority: groupAddr,
			Tenant:    tenantAddr,
			Items: []billingtypes.LeaseItemInput{
				{SkuUuid: activeSKUUUID, Quantity: 1},
			},
		}
		createAndRunProposalSuccess(t, ctx, chain, config, proposerAddr, []*types.Any{createAny(t, &createLeaseForTenantMsg)})

		// Query to get the new lease ID (PENDING state)
		leases, err := helpers.BillingQueryLeasesByTenant(ctx, chain, tenantAddr, "")
		require.NoError(t, err)
		var withdrawAllLeaseID string
		for i := len(leases.Leases) - 1; i >= 0; i-- {
			if leases.Leases[i].GetState() == billingtypes.LEASE_STATE_PENDING {
				withdrawAllLeaseID = leases.Leases[i].Uuid
				break
			}
		}
		require.NotEmpty(t, withdrawAllLeaseID, "should find a pending lease")

		// Acknowledge the lease
		ackLeaseMsg := billingtypes.MsgAcknowledgeLease{
			Sender:     groupAddr,
			LeaseUuids: []string{withdrawAllLeaseID},
		}
		createAndRunProposalSuccess(t, ctx, chain, config, proposerAddr, []*types.Any{createAny(t, &ackLeaseMsg)})

		// Wait for some accrual
		require.NoError(t, testutil.WaitForBlocks(ctx, 3, chain))

		// Withdraw all for provider
		withdrawAllMsg := billingtypes.MsgWithdrawAll{
			Sender:       groupAddr,
			ProviderUuid: providerUUID,
			Limit:        0, // Use default limit
		}
		createAndRunProposalSuccess(t, ctx, chain, config, proposerAddr, []*types.Any{createAny(t, &withdrawAllMsg)})
	})

	// Test multi-SKU lease creation
	t.Run("create_multi_sku_lease", func(t *testing.T) {
		// Create another SKU for multi-item lease (priced in PWR to match tenant credit)
		basePrice := sdk.NewInt64Coin(pwrDenom, 86400) // 86400 upwr per day (1 upwr per second)
		createSKUMsg := createSKUCreateSKUProposal(groupAddr, providerUUID, "Storage Small", skutypes.Unit_UNIT_PER_DAY, basePrice, nil)
		createAndRunProposalSuccess(t, ctx, chain, config, proposerAddr, []*types.Any{createAny(t, &createSKUMsg)})

		// Get the new SKU ID (find a PWR-priced SKU that's not activeSKUUUID)
		skus, err := helpers.SKUQuerySKUsByProvider(ctx, chain, providerUUID)
		require.NoError(t, err)
		var secondSKUUUID string
		for _, sku := range skus.Skus {
			if sku.Active && sku.Uuid != activeSKUUUID && sku.BasePrice.Denom == pwrDenom {
				secondSKUUUID = sku.Uuid
				break
			}
		}
		require.NotZero(t, secondSKUUUID, "should have a second active PWR-priced SKU")

		// Create multi-SKU lease
		createMultiLeaseMsg := billingtypes.MsgCreateLeaseForTenant{
			Authority: groupAddr,
			Tenant:    tenantAddr,
			Items: []billingtypes.LeaseItemInput{
				{SkuUuid: activeSKUUUID, Quantity: 2},
				{SkuUuid: secondSKUUUID, Quantity: 1},
			},
		}
		createAndRunProposalSuccess(t, ctx, chain, config, proposerAddr, []*types.Any{createAny(t, &createMultiLeaseMsg)})

		// Get the newly created PENDING lease
		leases, err := helpers.BillingQueryLeasesByTenant(ctx, chain, tenantAddr, "")
		require.NoError(t, err)
		var multiLeaseID string
		for i := len(leases.Leases) - 1; i >= 0; i-- {
			if leases.Leases[i].GetState() == billingtypes.LEASE_STATE_PENDING && len(leases.Leases[i].Items) == 2 {
				multiLeaseID = leases.Leases[i].Uuid
				break
			}
		}
		require.NotEmpty(t, multiLeaseID, "should find a pending multi-item lease")

		// Acknowledge the multi-SKU lease
		ackLeaseMsg := billingtypes.MsgAcknowledgeLease{
			Sender:     groupAddr,
			LeaseUuids: []string{multiLeaseID},
		}
		createAndRunProposalSuccess(t, ctx, chain, config, proposerAddr, []*types.Any{createAny(t, &ackLeaseMsg)})

		// Verify multi-SKU lease is now active
		leases, err = helpers.BillingQueryLeasesByTenant(ctx, chain, tenantAddr, "active")
		require.NoError(t, err)
		require.NotEmpty(t, leases.Leases)

		// Find the lease with 2 items
		var multiItemLease *helpers.LeaseJSON
		for i := range leases.Leases {
			if len(leases.Leases[i].Items) == 2 {
				multiItemLease = &leases.Leases[i]
				break
			}
		}
		require.NotNil(t, multiItemLease, "should have a multi-item lease")
		require.Len(t, multiItemLease.Items, 2, "lease should have 2 items")
	})

	_ = node // avoid unused variable
}

// SKU proposal creation helpers

func createSKUCreateProviderProposal(authority, address, payoutAddress string, metaHash []byte) skutypes.MsgCreateProvider {
	return skutypes.MsgCreateProvider{
		Authority:     authority,
		Address:       address,
		PayoutAddress: payoutAddress,
		MetaHash:      metaHash,
	}
}

func createSKUUpdateProviderProposal(authority string, uuid string, address, payoutAddress string, active bool, metaHash []byte) skutypes.MsgUpdateProvider {
	return skutypes.MsgUpdateProvider{
		Authority:     authority,
		Uuid:          uuid,
		Address:       address,
		PayoutAddress: payoutAddress,
		Active:        active,
		MetaHash:      metaHash,
	}
}

func createSKUDeactivateProviderProposal(authority string, uuid string) skutypes.MsgDeactivateProvider {
	return skutypes.MsgDeactivateProvider{
		Authority: authority,
		Uuid:      uuid,
	}
}

func createSKUCreateSKUProposal(authority string, providerUUID string, name string, unit skutypes.Unit, basePrice sdk.Coin, metaHash []byte) skutypes.MsgCreateSKU {
	return skutypes.MsgCreateSKU{
		Authority:    authority,
		ProviderUuid: providerUUID,
		Name:         name,
		Unit:         unit,
		BasePrice:    basePrice,
		MetaHash:     metaHash,
	}
}

func createSKUUpdateSKUProposal(authority string, uuid, providerUUID string, name string, unit skutypes.Unit, basePrice sdk.Coin, active bool, metaHash []byte) skutypes.MsgUpdateSKU {
	return skutypes.MsgUpdateSKU{
		Authority:    authority,
		Uuid:         uuid,
		ProviderUuid: providerUUID,
		Name:         name,
		Unit:         unit,
		BasePrice:    basePrice,
		Active:       active,
		MetaHash:     metaHash,
	}
}

func createSKUDeactivateSKUProposal(authority string, uuid string) skutypes.MsgDeactivateSKU {
	return skutypes.MsgDeactivateSKU{
		Authority: authority,
		Uuid:      uuid,
	}
}
