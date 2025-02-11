package interchaintest

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	grouptypes "github.com/cosmos/cosmos-sdk/x/group"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"

	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

const (
	// Hardcoding the upgrade name to match what's registered in app.RegisterUpgradeHandlers()
	upgradeName = "v0.0.1-rc.5"

	haltHeightDelta    = int64(15) // will propose upgrade this many blocks in the future
	blocksAfterUpgrade = int64(7)
)

var (
	// baseChain is the current version of the chain that will be upgraded from
	baseChain = ibc.DockerImage{
		Repository: "ghcr.io/liftedinit/manifest-ledger", // GitHub Container Registry path
		Version:    "v0.0.1-rc.4",                        // The version we're upgrading from
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

	chains, err := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t), []*interchaintest.ChainSpec{
		{
			Name:          "manifest-2",
			Version:       baseChain.Version, // Use the base version initially
			ChainName:     cfg.ChainID,
			NumValidators: &vals,
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
	user1Wallet, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, user1, accMnemonic, DefaultGenesisAmt, chain)
	require.NoError(t, err)
	_, err = interchaintest.GetAndFundTestUserWithMnemonic(ctx, user2, acc1Mnemonic, DefaultGenesisAmt, chain)
	require.NoError(t, err)

	// Get current height and calculate halt height
	height, err := chain.Height(ctx)
	require.NoError(t, err, "error fetching height before submit upgrade proposal")

	haltHeight := height + haltHeightDelta

	// Create and submit upgrade proposal through group
	t.Log("Submitting upgrade proposal through group")
	upgradeMsg := createUpgradeProposal(groupAddr, upgradeName, haltHeight)

	// Set the upgrade plan
	createAndRunProposalSuccess(t, ctx, chain, &cfg, accAddr, []*types.Any{createAny(t, &upgradeMsg)})
	verifyUpgradePlan(t, ctx, chain, &upgradetypes.Plan{Name: upgradeName, Height: haltHeight})

	// Wait for chain to halt
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

	// Upgrade nodes
	t.Log("Stopping all nodes...")
	err = chain.StopAllNodes(ctx)
	require.NoError(t, err, "error stopping node(s)")

	t.Log("Waiting for chain to stop...")
	time.Sleep(30 * time.Second)

	// Use local build for upgrade
	t.Log("Using local build for upgrade")
	chain.UpgradeVersion(ctx, client, "manifest", "local")

	t.Log("Starting upgraded nodes...")
	time.Sleep(30 * time.Second)

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
	StoreAndInstantiateContract(t, ctx, chain, user1Wallet, accAddr)
}

func StoreAndInstantiateContract(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, accAddr string) string {
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
