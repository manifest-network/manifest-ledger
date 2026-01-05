package interchaintest

import (
	"context"
	"fmt"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	grouptypes "github.com/cosmos/cosmos-sdk/x/group"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/manifest-network/manifest-ledger/interchaintest/helpers"
)

const (
	// billingUpgradeName is dynamically set from app.Version() in the test
	billingHaltHeightDelta    = int64(15)
	billingBlocksAfterUpgrade = int64(7)
)

var (
	// preBillingChain is the version without sku/billing modules
	preBillingChain = ibc.DockerImage{
		Repository: "ghcr.io/manifest-network/manifest-ledger",
		Version:    "1.0.13",
		UIDGID:     "1025:1025",
	}
)

// TestBillingModuleUpgrade tests upgrading from a pre-billing version to the current version
// with the x/sku and x/billing modules. It verifies:
// 1. Chain starts on old version
// 2. Upgrade proposal is submitted and passes
// 3. Chain halts at upgrade height
// 4. Nodes upgrade to new version
// 5. Chain produces blocks after upgrade
// 6. New billing/sku modules are functional
func TestBillingModuleUpgrade(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// Initialize group policy for pre-upgrade governance
	groupPolicy := &grouptypes.GroupPolicyInfo{
		Address:  groupAddr,
		GroupId:  1,
		Admin:    groupAddr,
		Metadata: "group policy",
		Version:  1,
	}
	err := groupPolicy.SetDecisionPolicy(createThresholdDecisionPolicy("1", 10*time.Second, 0*time.Second))
	require.NoError(t, err)

	// Initialize codec for proper group policy serialization
	enc := AppEncoding()
	grouptypes.RegisterInterfaces(enc.InterfaceRegistry)
	cdc := codec.NewProtoCodec(enc.InterfaceRegistry)
	_, err = cdc.MarshalJSON(groupPolicy)
	require.NoError(t, err)

	// Setup chain with group-based governance (required for upgrade proposals)
	previousVersionGenesis := append(DefaultGenesis,
		cosmos.NewGenesisKV("app_state.group.group_seq", "1"),
		cosmos.NewGenesisKV("app_state.group.groups", []grouptypes.GroupInfo{groupInfo}),
		cosmos.NewGenesisKV("app_state.group.group_members", []grouptypes.GroupMember{groupMember1, groupMember2}),
		cosmos.NewGenesisKV("app_state.group.group_policy_seq", "1"),
		cosmos.NewGenesisKV("app_state.group.group_policies", []*grouptypes.GroupPolicyInfo{groupPolicy}),
	)

	cfg := LocalChainConfig
	cfg.ModifyGenesis = cosmos.ModifyGenesis(previousVersionGenesis)
	cfg.Images = []ibc.DockerImage{preBillingChain}
	cfg.Env = []string{
		fmt.Sprintf("POA_ADMIN_ADDRESS=%s", groupAddr),
	}

	numVals := 2
	chains, err := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t), []*interchaintest.ChainSpec{
		{
			Name:          "manifest-billing-upgrade",
			Version:       preBillingChain.Version,
			ChainName:     cfg.ChainID,
			NumValidators: &numVals,
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

	// Get current height and calculate halt height
	height, err := chain.Height(ctx)
	require.NoError(t, err, "error fetching height before submit upgrade proposal")

	haltHeight := height + billingHaltHeightDelta

	// The upgrade name must match app.Version() in the new binary
	// This must match VERSION in the Makefile (currently v1.1.0)
	upgradeName := "v1.1.0"

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

	// This should timeout due to chain halt at upgrade height
	_ = testutil.WaitForBlocks(timeoutCtx, int(haltHeight-height), chain)

	height, err = chain.Height(ctx)
	require.NoError(t, err, "error fetching height after chain should have halted")

	// Make sure that chain is halted
	require.Equal(t, haltHeight, height, "height is not equal to halt height")

	time.Sleep(10 * time.Second)

	// Upgrade nodes
	t.Log("Stopping all nodes...")
	err = chain.StopAllNodes(ctx)
	require.NoError(t, err, "error stopping node(s)")

	t.Log("Waiting for chain to stop...")

	// Use local build for upgrade (includes sku and billing modules)
	t.Log("Using local build for upgrade with sku and billing modules")
	chain.UpgradeVersion(ctx, client, "manifest", "local")

	t.Log("Starting upgraded nodes...")
	err = chain.StartAllNodes(ctx)
	require.NoError(t, err, "error starting upgraded node(s)")

	timeoutCtx, timeoutCtxCancel = context.WithTimeout(ctx, time.Second*60)
	defer timeoutCtxCancel()

	err = testutil.WaitForBlocks(timeoutCtx, int(billingBlocksAfterUpgrade), chain)
	require.NoError(t, err, "chain did not produce blocks after upgrade")

	height, err = chain.Height(ctx)
	require.NoError(t, err, "error fetching height after upgrade")

	require.GreaterOrEqual(t, height, haltHeight+billingBlocksAfterUpgrade, "height did not increment enough after upgrade")

	t.Log("Chain successfully upgraded, now testing new modules...")

	// Test SKU module functionality after upgrade
	testSKUModuleAfterUpgrade(t, ctx, chain, &cfg, user1Wallet)

	// Test Billing module functionality after upgrade
	testBillingModuleAfterUpgrade(t, ctx, chain, user1Wallet)

	t.Log("Upgrade test completed successfully - sku and billing modules are functional")
}

// testSKUModuleAfterUpgrade verifies that the SKU module works after the upgrade
func testSKUModuleAfterUpgrade(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, cfg *ibc.ChainConfig, _ ibc.Wallet) {
	t.Log("Testing SKU module after upgrade...")

	// Query SKU params - should return default params
	params, err := helpers.SKUQueryParams(ctx, chain)
	require.NoError(t, err)
	require.NotNil(t, params)
	t.Logf("SKU params: %+v", params)

	// Query providers - should be empty initially
	providers, err := helpers.SKUQueryProviders(ctx, chain)
	require.NoError(t, err)
	require.NotNil(t, providers)
	require.Empty(t, providers.Providers, "expected no providers after fresh upgrade")
	t.Log("SKU module queries working correctly")

	// Create a provider through governance
	t.Log("Creating provider through group proposal...")
	createProviderMsg := helpers.CreateProviderMsg(
		groupAddr,       // authority
		accAddr,         // provider address (payout address)
		"test-provider", // metadata hash
		"",              // api_url (optional)
	)

	createAndRunProposalSuccess(t, ctx, chain, cfg, accAddr, []*types.Any{createAny(t, &createProviderMsg)})

	// Verify provider was created
	providers, err = helpers.SKUQueryProviders(ctx, chain)
	require.NoError(t, err)
	require.Len(t, providers.Providers, 1, "expected 1 provider after creation")
	provider := providers.Providers[0]
	require.Equal(t, accAddr, provider.Address)
	require.True(t, provider.Active)
	t.Logf("Provider created with UUID: %s", provider.Uuid)

	// Create a SKU for the provider
	// Base price must be high enough that per-second rate is non-zero
	// For UNIT_PER_HOUR: price / 3600 >= 1, so price >= 3600
	t.Log("Creating SKU through group proposal...")
	basePrice := sdk.NewCoin(Denom, sdkmath.NewInt(3600)) // 3600 umfx per hour = 1 umfx per second
	createSKUMsg := helpers.CreateSKUMsg(
		groupAddr,       // authority
		provider.Uuid,   // provider UUID
		"test-sku",      // name
		basePrice,       // base price
		"UNIT_PER_HOUR", // price model
	)

	createAndRunProposalSuccess(t, ctx, chain, cfg, accAddr, []*types.Any{createAny(t, &createSKUMsg)})

	// Verify SKU was created
	skus, err := helpers.SKUQuerySKUs(ctx, chain)
	require.NoError(t, err)
	require.Len(t, skus.Skus, 1, "expected 1 SKU after creation")
	sku := skus.Skus[0]
	require.Equal(t, provider.Uuid, sku.ProviderUuid)
	require.True(t, sku.Active)
	t.Logf("SKU created with UUID: %s", sku.Uuid)
}

// testBillingModuleAfterUpgrade verifies that the Billing module works after the upgrade
func testBillingModuleAfterUpgrade(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet) {
	t.Log("Testing Billing module after upgrade...")

	// Query billing params - should return default params
	params, err := helpers.BillingQueryParams(ctx, chain)
	require.NoError(t, err)
	require.NotNil(t, params)
	require.Equal(t, uint64(100), params.Params.MaxLeasesPerTenant)
	require.Equal(t, uint64(20), params.Params.MaxItemsPerLease)
	require.Equal(t, uint64(3600), params.Params.MinLeaseDuration) // 1 hour
	require.Equal(t, uint64(10), params.Params.MaxPendingLeasesPerTenant)
	require.Equal(t, uint64(1800), params.Params.PendingTimeout) // 30 minutes
	t.Logf("Billing params: %+v", params)

	// Query leases - should be empty initially
	leases, err := helpers.BillingQueryLeases(ctx, chain, false)
	require.NoError(t, err)
	require.NotNil(t, leases)
	require.Empty(t, leases.Leases, "expected no leases after fresh upgrade")
	t.Log("Billing module queries working correctly")

	// Fund a credit account
	t.Log("Funding credit account...")
	fundAmount := sdk.NewCoin(Denom, sdkmath.NewInt(1_000_000)) // 1 MFX
	txResp, err := helpers.BillingFundCredit(ctx, chain, user, user.FormattedAddress(), fundAmount.String())
	require.NoError(t, err)
	require.Equal(t, uint32(0), txResp.Code, "fund credit tx failed: %s", txResp.RawLog)

	// Query credit account (includes balances)
	creditAccount, err := helpers.BillingQueryCreditAccount(ctx, chain, user.FormattedAddress())
	require.NoError(t, err)
	require.NotNil(t, creditAccount)
	require.Equal(t, user.FormattedAddress(), creditAccount.CreditAccount.Tenant)
	require.NotEmpty(t, creditAccount.CreditAccount.CreditAddress)
	t.Logf("Credit account created: %s with address %s", creditAccount.CreditAccount.Tenant, creditAccount.CreditAccount.CreditAddress)

	// Verify credit balance
	require.NotNil(t, creditAccount.Balances)
	require.True(t, creditAccount.Balances.AmountOf(Denom).Equal(fundAmount.Amount), "credit balance mismatch")
	t.Logf("Credit balance: %s", creditAccount.Balances.String())

	t.Log("Billing module functionality verified successfully")
}
