package interchaintest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path"
	"testing"
	"time"

	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	grouptypes "github.com/cosmos/cosmos-sdk/x/group"
	"github.com/manifest-network/manifest-ledger/interchaintest/helpers"
	"github.com/strangelove-ventures/interchaintest/v8/dockerutil"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	poatypes "github.com/strangelove-ventures/poa"

	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

const (
	upgradeName = "v1.0.9"

	haltHeightDelta    = int64(15) // will propose upgrade this many blocks in the future
	blocksAfterUpgrade = int64(7)
	val1               = "val1"
)

var (
	// baseChain is the current version of the chain that will be upgraded from
	baseChain = ibc.DockerImage{
		Repository: "ghcr.io/liftedinit/manifest-ledger", // GitHub Container Registry path
		Version:    "v1.0.5",                             // The version we're upgrading from
		UIDGID:     "1025:1025",
	}

	// Initialize group policy with decision policy
	_ = func() error {
		err := groupPolicy.SetDecisionPolicy(createThresholdDecisionPolicy("1", 10*time.Second, 0*time.Second))
		if err != nil {
			panic(err)
		}
		return nil
	}()

	// Initialize codec for proper group policy serialization
	_ = func() error {
		enc := AppEncoding()
		grouptypes.RegisterInterfaces(enc.InterfaceRegistry)
		cdc := codec.NewProtoCodec(enc.InterfaceRegistry)
		_, err := cdc.MarshalJSON(groupPolicy)
		if err != nil {
			panic(err)
		}
		return nil
	}()
)

func TestBasicManifestUpgrade(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// Setup chain with group-based governance
	previousVersionGenesis := append(DefaultGenesis,
		cosmos.NewGenesisKV("app_state.group.group_seq", "1"),
		cosmos.NewGenesisKV("app_state.group.groups", []grouptypes.GroupInfo{groupInfo}),
		cosmos.NewGenesisKV("app_state.group.group_members", []grouptypes.GroupMember{groupMember1, groupMember2}),
		cosmos.NewGenesisKV("app_state.group.group_policy_seq", "1"),
		cosmos.NewGenesisKV("app_state.group.group_policies", []*grouptypes.GroupPolicyInfo{groupPolicy}),
	)

	cfg := LocalChainConfig
	cfg.ModifyGenesis = cosmos.ModifyGenesis(previousVersionGenesis)
	cfg.Images = []ibc.DockerImage{baseChain}
	cfg.Env = []string{
		fmt.Sprintf("POA_ADMIN_ADDRESS=%s", groupAddr), // Set group address as POA admin
	}

	v := 12
	chains, err := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t), []*interchaintest.ChainSpec{
		{
			Name:          "manifest-2",
			Version:       baseChain.Version, // Use the base version initially
			ChainName:     cfg.ChainID,
			NumValidators: &v,
			NumFullNodes:  &fullNodes,
			ChainConfig:   cfg,
		},
	}).Chains(t.Name())
	require.NoError(t, err)

	chain := chains[0].(*cosmos.CosmosChain)

	ctx := context.Background()

	client, network := interchaintest.DockerSetup(t)

	ic := interchaintest.NewInterchain().
		AddChain(chain)

	rep := testreporter.NewNopReporter()
	eRep := rep.RelayerExecReporter(t)

	// Build interchain
	require.NoError(t, ic.Build(ctx, eRep, interchaintest.InterchainBuildOptions{
		TestName:         t.Name(),
		Client:           client,
		NetworkID:        network,
		SkipPathCreation: true,
	}))

	// Get test users
	_, err = interchaintest.GetAndFundTestUserWithMnemonic(ctx, user1, accMnemonic, DefaultGenesisAmt, chain)
	require.NoError(t, err)
	_, err = interchaintest.GetAndFundTestUserWithMnemonic(ctx, user2, acc1Mnemonic, DefaultGenesisAmt, chain)
	require.NoError(t, err)

	// We'll add this validator after the upgrade
	val1Wallet, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, val1, val1Mnemonic, DefaultGenesisAmt, chain)
	require.NoError(t, err)

	// Checking current unbounding time
	sparams, err := chain.StakingQueryParams(ctx)
	require.NoError(t, err)
	require.Equal(t, 504*time.Hour, sparams.UnbondingTime)

	// Update unbonding time to 6 seconds for faster test
	// ********** WARNING **********
	// Removing a validator, upgrading the chain while the validator is UNBOUNDING, then adding a new validator will CRASH THE CHAIN.
	// See https://github.com/strangelove-ventures/poa/issues/245
	// TODO: Fix POA to handle this case.
	// ********** WARNING **********
	updateStaking := createSetStakingParamsProposal(groupAddr, 6*time.Second, sparams.MaxValidators, sparams.MaxEntries, sparams.HistoricalEntries, Denom)
	createAndRunProposalSuccess(t, ctx, chain, &cfg, accAddr, []*types.Any{createAny(t, &updateStaking)})

	// Verify the staking param change
	sparams, err = chain.StakingQueryParams(ctx)
	require.NoError(t, err)
	require.Equal(t, 6*time.Second, sparams.UnbondingTime)

	beforeVals, err := chain.StakingQueryValidators(ctx, "")
	require.NoError(t, err)
	valToRemove := beforeVals[0]
	t.Log("Removing validator", valToRemove.Description.Moniker, "with operator address", valToRemove.OperatorAddress)
	p := createRemoveValidatorProposal(groupAddr, valToRemove.OperatorAddress)
	createAndRunProposalSuccess(t, ctx, chain, &cfg, accAddr, []*types.Any{createAny(t, &p)})

	// Get current height and calculate halt height
	height, err := chain.Height(ctx)
	require.NoError(t, err, "error fetching height before submit upgrade proposal")

	haltHeight := height + haltHeightDelta

	t.Log("Upgrade name:", upgradeName)
	t.Log("Submitting upgrade proposal through group")
	upgradeMsg := createUpgradeProposal(groupAddr, upgradeName, haltHeight)

	createAndRunProposalSuccess(t, ctx, chain, &cfg, accAddr, []*types.Any{createAny(t, &upgradeMsg)})
	verifyUpgradePlan(t, ctx, chain, &upgradetypes.Plan{Name: upgradeName, Height: haltHeight})

	t.Log("Waiting for chain to halt at upgrade height")
	timeoutCtx, timeoutCtxCancel := context.WithTimeout(ctx, time.Second*45)
	defer timeoutCtxCancel()

	height, err = chain.Height(ctx)
	require.NoError(t, err, "error fetching height before upgrade")

	// this should timeout due to chain halt at upgrade height
	_ = testutil.WaitForBlocks(timeoutCtx, int(haltHeight-height), chain)

	height, err = chain.Height(ctx)
	require.NoError(t, err, "error fetching height after chain should have halted")

	// make sure that chain is halted
	require.Equal(t, haltHeight, height, "height is not equal to halt height")

	time.Sleep(10 * time.Second)

	// Upgrade nodes
	t.Log("Stopping all nodes...")
	err = chain.StopAllNodes(ctx)
	require.NoError(t, err, "error stopping node(s)")

	t.Log("Waiting for chain to stop...")

	// Use local build for upgrade
	t.Log("Using local build for upgrade")
	chain.UpgradeVersion(ctx, client, "manifest", "local")

	t.Log("Starting upgraded nodes...")

	// Make sure we have the v2 upgrade handler in the local build
	err = chain.StartAllNodes(ctx)
	require.NoError(t, err, "error starting upgraded node(s)")

	timeoutCtx, timeoutCtxCancel = context.WithTimeout(ctx, time.Second*60)
	defer timeoutCtxCancel()

	err = testutil.WaitForBlocks(timeoutCtx, int(blocksAfterUpgrade), chain)
	require.NoError(t, err, "chain did not produce blocks after upgrade")

	height, err = chain.Height(ctx)
	require.NoError(t, err, "error fetching height after upgrade")

	require.GreaterOrEqual(t, height, haltHeight+blocksAfterUpgrade, "height did not increment enough after upgrade")

	// Test CosmWasm functionality after upgrade
	t.Log("Testing CosmWasm functionality after upgrade")
	storeAndInstantiateContract(t, ctx, chain, accAddr)

	// Test inducting a new validator after upgrade
	t.Log("Testing adding a new validator after upgrade")
	createAndInductValidator(t, ctx, chain, val1Wallet)
}

func storeAndInstantiateContract(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, accAddr string) string {
	// Get the current chain config
	chainConfig := chain.Config()

	// Store contract
	wasmFile := "../scripts/cw_template.wasm"
	wasmStoreProposal = createWasmStoreProposal(groupAddr, wasmFile)
	createAndRunProposalSuccess(t, ctx, chain, &chainConfig, accAddr, []*types.Any{createAny(t, &wasmStoreProposal)})

	// Query the code ID
	codeId := queryLatestCodeId(t, ctx, chain)
	require.Equal(t, uint64(1), codeId)

	// Instantiate the contract
	initMsg := map[string]interface{}{
		"count": 0,
	}
	initMsgBz, err := json.Marshal(initMsg)
	require.NoError(t, err)

	wasmInstantiateProposal := createWasmInstantiateProposal(groupAddr, codeId, string(initMsgBz))
	createAndRunProposalSuccess(t, ctx, chain, &chainConfig, accAddr, []*types.Any{createAny(t, &wasmInstantiateProposal)})

	// Query the contract address
	contractAddr := queryLatestContractAddress(t, ctx, chain, codeId)
	require.NotEmpty(t, contractAddr)

	// Query contract state to verify instantiation
	var resp struct {
		Count int `json:"count"`
	}
	queryMsg := map[string]interface{}{
		"get_count": struct{}{},
	}
	queryMsgBz, err := json.Marshal(queryMsg)
	require.NoError(t, err)

	err = chain.QueryContract(ctx, contractAddr, string(queryMsgBz), &resp)
	require.NoError(t, err)
	require.Equal(t, 0, resp.Count)

	return contractAddr
}

func createAndInductValidator(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, val ibc.Wallet) {
	chainConfig := chain.Config()
	enc := chainConfig.EncodingConfig

	beforeVals, err := chain.StakingQueryValidators(ctx, "")
	require.NoError(t, err)
	numBefore := len(beforeVals)

	bz, err := base64.StdEncoding.DecodeString(val1Pubkey)
	require.NoError(t, err)

	if len(bz) != 32 {
		t.Fatalf("invalid pubkey length: expected 32, got %d", len(bz))
	}

	tmpPk := ed25519.PubKey(bz)
	pk, err := cryptocodec.FromCmtPubKeyInterface(tmpPk)
	require.NoError(t, err)

	anyPk, err := types.NewAnyWithValue(pk)
	require.NoError(t, err)

	// Encode MsgCreateValidator to valid JSON using the chain's codec
	msgCreateValidator := createValidator("my-test-validator", val.FormattedAddress(), anyPk)
	pkJSON, err := enc.Codec.MarshalJSON(msgCreateValidator.Pubkey)
	require.NoError(t, err)

	payload := map[string]any{
		"pubkey":                     json.RawMessage(pkJSON),
		"amount":                     "1000000upoa",
		"moniker":                    msgCreateValidator.Description.Moniker,
		"identity":                   msgCreateValidator.Description.Identity,
		"website":                    msgCreateValidator.Description.Website,
		"security-contact":           msgCreateValidator.Description.SecurityContact,
		"details":                    msgCreateValidator.Description.Details,
		"commission-rate":            msgCreateValidator.Commission.Rate.String(),
		"commission-max-rate":        msgCreateValidator.Commission.MaxRate.String(),
		"commission-max-change-rate": msgCreateValidator.Commission.MaxChangeRate.String(),
		"min-self-delegation":        msgCreateValidator.MinSelfDelegation.String(),
	}

	out, err := json.MarshalIndent(payload, "", "  ")
	require.NoError(t, err)

	tn := chain.GetNode()

	file := "validator_json.json"
	fw := dockerutil.NewFileWriter(nil, tn.DockerClient, tn.TestName)
	err = fw.WriteFile(ctx, tn.VolumeName, file, out)
	require.NoError(t, err)

	resp, err := helpers.POACreateValidator(ctx, chain, val, path.Join(tn.HomeDir(), file), "--gas", "auto", "--gas-adjustment", "2.0", "--gas-prices", "1.0umfx")
	require.NoError(t, err)
	t.Log("Create validator response:", resp.String())
	require.Equal(t, resp.Code, uint32(0))

	r, err := poatypes.NewQueryClient(tn.GrpcConn).PendingValidators(ctx, &poatypes.QueryPendingValidatorsRequest{})
	require.NoError(t, err)
	require.Equal(t, 1, len(r.GetPending()))

	t.Log("Pending validator before induction", r.GetPending())

	setPowerProposal := createSetPowerProposal(groupAddr, val1Addr, 5000000000000, false)
	createAndRunProposalSuccess(t, ctx, chain, &chainConfig, accAddr, []*types.Any{createAny(t, &setPowerProposal)})

	afterVals, err := chain.StakingQueryValidators(ctx, "")
	require.NoError(t, err)
	numAfter := len(afterVals)

	r, err = poatypes.NewQueryClient(tn.GrpcConn).PendingValidators(ctx, &poatypes.QueryPendingValidatorsRequest{})
	require.NoError(t, err)
	require.Equal(t, 0, len(r.GetPending()))

	t.Log("Pending validator after induction", r.GetPending())

	// Should have one more validator after inducting
	require.Equal(t, numBefore+1, numAfter)
}
