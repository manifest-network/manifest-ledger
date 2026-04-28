package interchaintest

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	grouptypes "github.com/cosmos/cosmos-sdk/x/group"
	"github.com/cosmos/interchaintest/v10/testutil"

	"github.com/cosmos/interchaintest/v10"
	"github.com/cosmos/interchaintest/v10/chain/cosmos"
	"github.com/cosmos/interchaintest/v10/ibc"
	"github.com/cosmos/interchaintest/v10/testreporter"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	tokenfactorytypes "github.com/strangelove-ventures/tokenfactory/x/tokenfactory/types"

	"github.com/manifest-network/manifest-ledger/interchaintest/helpers"
)

const (
	haltHeightDelta    = int64(15) // will propose upgrade this many blocks in the future
	blocksAfterUpgrade = int64(7)
)

var (
	// baseChain is the current version of the chain that will be upgraded from
	baseChain = ibc.DockerImage{
		Repository: "ghcr.io/manifest-network/manifest-ledger",
		Version:    "2.0.3",
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
			Version:       baseChain.Version,
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

	t.Cleanup(func() {
		_ = ic.Close()
	})

	// Get test users
	user1Wallet, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, user1, accMnemonic, DefaultGenesisAmt, chain)
	require.NoError(t, err)
	_, err = interchaintest.GetAndFundTestUserWithMnemonic(ctx, user2, acc1Mnemonic, DefaultGenesisAmt, chain)
	require.NoError(t, err)

	// Seed pre-upgrade state across every custom module so the post-upgrade
	// asserts can prove the in-place migration preserves on-chain records.
	t.Log("Seeding pre-upgrade state (tokenfactory, sku, billing)")
	preState := seedPreUpgradeState(t, ctx, chain, &cfg, user1Wallet)

	// Get current height and calculate halt height
	height, err := chain.Height(ctx)
	require.NoError(t, err, "error fetching height before submit upgrade proposal")

	haltHeight := height + haltHeightDelta

	// The upgrade name must match app.Version() in the new binary
	upgradeName := "v3.0.0"

	t.Logf("Upgrade name: %s", upgradeName)
	t.Logf("Current height: %d, halt height: %d", height, haltHeight)
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

	// Make sure we have the upgrade handler in the local build
	err = chain.StartAllNodes(ctx)
	require.NoError(t, err, "error starting upgraded node(s)")

	timeoutCtx, timeoutCtxCancel = context.WithTimeout(ctx, time.Second*60)
	defer timeoutCtxCancel()

	err = testutil.WaitForBlocks(timeoutCtx, int(blocksAfterUpgrade), chain)
	require.NoError(t, err, "chain did not produce blocks after upgrade")

	height, err = chain.Height(ctx)
	require.NoError(t, err, "error fetching height after upgrade")

	require.GreaterOrEqual(t, height, haltHeight+blocksAfterUpgrade, "height did not increment enough after upgrade")

	// Verify pre-upgrade state (tokenfactory, sku, billing) survived the
	// migration. This is the load-bearing check for an in-place upgrade.
	t.Log("Verifying pre-upgrade state survived the upgrade")
	verifyPreUpgradeStateSurvived(t, ctx, chain, preState)

	// Test CosmWasm functionality after upgrade
	t.Log("Testing CosmWasm functionality after upgrade")
	StoreAndInstantiateContract(t, ctx, chain, user1Wallet, accAddr)
}

// preUpgradeState captures state seeded before the v3.0.0 upgrade so we can
// verify it survives the in-place migration intact. Each module's keeper
// state should round-trip identically across the SDK v0.50→v0.53 boundary.
type preUpgradeState struct {
	providerUUID    string
	skuUUID         string
	skuBasePrice    sdk.Coin
	leaseUUID       string
	leaseTenant     string
	creditAddress   string
	creditBalances  sdk.Coins
	tfDenom         string
	tfMintRecipient string
	tfMintBalance   sdkmath.Int
}

// seedPreUpgradeState exercises one create path per custom module so the
// post-upgrade verification has something to query for. Group governance
// is used for any path that requires the PoA admin (the chain's POA admin
// is groupAddr in this test).
func seedPreUpgradeState(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, cfg *ibc.ChainConfig, tenantWallet ibc.Wallet) *preUpgradeState {
	t.Helper()

	state := &preUpgradeState{leaseTenant: tenantWallet.FormattedAddress()}

	// 1. Tokenfactory denom (group proposal — exercises the PoA admin's
	// MsgCreateDenom path).
	subdenom := "uupgradeseed"
	state.tfDenom = fmt.Sprintf("factory/%s/%s", groupAddr, subdenom)
	createDenomMsg := tokenfactorytypes.MsgCreateDenom{Sender: groupAddr, Subdenom: subdenom}
	createAndRunProposalSuccess(t, ctx, chain, cfg, accAddr, []*types.Any{createAny(t, &createDenomMsg)})

	// 2. Sudo-mint to the tenant (exercises EnableSudoMint capability that
	// the manifest-network/tokenfactory fork preserves from strangelove).
	state.tfMintRecipient = tenantWallet.FormattedAddress()
	state.tfMintBalance = sdkmath.NewInt(123_456_789)
	mintMsg := tokenfactorytypes.MsgMint{
		Sender:        groupAddr,
		Amount:        sdk.NewCoin(state.tfDenom, state.tfMintBalance),
		MintToAddress: state.tfMintRecipient,
	}
	createAndRunProposalSuccess(t, ctx, chain, cfg, accAddr, []*types.Any{createAny(t, &mintMsg)})

	// 3. Provider via group proposal (x/sku authority path).
	createProviderMsg := helpers.CreateProviderMsg(groupAddr, accAddr, "upgrade-seed-meta", "")
	createAndRunProposalSuccess(t, ctx, chain, cfg, accAddr, []*types.Any{createAny(t, &createProviderMsg)})
	providers, err := helpers.SKUQueryProviders(ctx, chain)
	require.NoError(t, err)
	require.Len(t, providers.Providers, 1, "expected exactly one provider after seeding")
	state.providerUUID = providers.Providers[0].Uuid

	// 4. SKU via group proposal (per-hour pricing — base price must be
	// evenly divisible by 3600 to give a non-zero per-second rate).
	state.skuBasePrice = sdk.NewCoin(Denom, sdkmath.NewInt(3600))
	createSKUMsg := helpers.CreateSKUMsg(groupAddr, state.providerUUID, "upgrade-seed-sku", state.skuBasePrice, "UNIT_PER_HOUR")
	createAndRunProposalSuccess(t, ctx, chain, cfg, accAddr, []*types.Any{createAny(t, &createSKUMsg)})
	skus, err := helpers.SKUQuerySKUs(ctx, chain)
	require.NoError(t, err)
	require.Len(t, skus.Skus, 1, "expected exactly one SKU after seeding")
	state.skuUUID = skus.Skus[0].Uuid

	// 5. Tenant funds credit (user-driven; deterministic creates a credit
	// account for the tenant).
	fundAmount := sdk.NewCoin(Denom, sdkmath.NewInt(10_000_000))
	fundResp, err := helpers.BillingFundCredit(ctx, chain, tenantWallet, state.leaseTenant, fundAmount.String())
	require.NoError(t, err)
	require.Equal(t, uint32(0), fundResp.Code, "fund credit failed: %s", fundResp.RawLog)
	ca, err := helpers.BillingQueryCreditAccount(ctx, chain, state.leaseTenant)
	require.NoError(t, err)
	state.creditAddress = ca.CreditAccount.CreditAddress
	state.creditBalances = ca.Balances

	// 6. Tenant creates a lease (stays in PENDING — we verify state survives
	// regardless of state-machine progress).
	items := []string{fmt.Sprintf("%s:1", state.skuUUID)}
	leaseResp, err := helpers.BillingCreateLease(ctx, chain, tenantWallet, items)
	require.NoError(t, err)
	require.Equal(t, uint32(0), leaseResp.Code, "create lease failed: %s", leaseResp.RawLog)
	leases, err := helpers.BillingQueryLeasesByTenant(ctx, chain, state.leaseTenant, "")
	require.NoError(t, err)
	require.Len(t, leases.Leases, 1, "expected exactly one lease after seeding")
	state.leaseUUID = leases.Leases[0].Uuid

	t.Logf("Seeded: tfDenom=%s tfMint=%s provider=%s sku=%s lease=%s",
		state.tfDenom, state.tfMintBalance, state.providerUUID, state.skuUUID, state.leaseUUID)
	return state
}

// verifyPreUpgradeStateSurvived asserts every record seeded before the
// upgrade is queryable post-upgrade with byte-identical content.
func verifyPreUpgradeStateSurvived(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, state *preUpgradeState) {
	t.Helper()

	// x/sku: provider survives.
	providerRes, err := helpers.SKUQueryProvider(ctx, chain, state.providerUUID)
	require.NoError(t, err, "provider query after upgrade")
	require.Equal(t, state.providerUUID, providerRes.Provider.Uuid)
	require.Equal(t, accAddr, providerRes.Provider.Address)
	require.True(t, providerRes.Provider.Active, "provider should still be active")

	// x/sku: SKU survives.
	skuRes, err := helpers.SKUQuerySKU(ctx, chain, state.skuUUID)
	require.NoError(t, err, "sku query after upgrade")
	require.Equal(t, state.skuUUID, skuRes.Sku.Uuid)
	require.Equal(t, state.providerUUID, skuRes.Sku.ProviderUuid)
	require.True(t, skuRes.Sku.Active, "sku should still be active")
	require.True(t, skuRes.Sku.BasePrice.Equal(state.skuBasePrice),
		"sku base price changed: pre=%s post=%s", state.skuBasePrice, skuRes.Sku.BasePrice)

	// x/billing: credit account address + balances survive.
	ca, err := helpers.BillingQueryCreditAccount(ctx, chain, state.leaseTenant)
	require.NoError(t, err, "credit account query after upgrade")
	require.Equal(t, state.creditAddress, ca.CreditAccount.CreditAddress,
		"credit account derived address should be deterministic across upgrade")
	require.True(t, ca.Balances.Equal(state.creditBalances),
		"credit balances changed: pre=%s post=%s", state.creditBalances, ca.Balances)

	// x/billing: lease record survives, including tenant + provider linkage.
	leaseRes, err := helpers.BillingQueryLease(ctx, chain, state.leaseUUID)
	require.NoError(t, err, "lease query after upgrade")
	require.Equal(t, state.leaseUUID, leaseRes.Lease.Uuid)
	require.Equal(t, state.leaseTenant, leaseRes.Lease.Tenant)
	require.Equal(t, state.providerUUID, leaseRes.Lease.ProviderUuid)
	require.NotEmpty(t, leaseRes.Lease.Items, "lease items should survive")

	// x/tokenfactory: minted balance survives (proves both the denom record
	// in the tokenfactory store AND the bank balance survive).
	tfBalance, err := chain.GetBalance(ctx, state.tfMintRecipient, state.tfDenom)
	require.NoError(t, err, "tokenfactory balance query after upgrade")
	require.True(t, tfBalance.Equal(state.tfMintBalance),
		"tokenfactory balance changed: pre=%s post=%s", state.tfMintBalance, tfBalance)

	t.Logf("Post-upgrade state survived: provider=%s sku=%s lease=%s tfBalance=%s",
		state.providerUUID, state.skuUUID, state.leaseUUID, tfBalance)
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
