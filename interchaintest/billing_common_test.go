// Package interchaintest contains end-to-end tests for the billing module.
// This file contains shared setup and test context for billing e2e tests.
package interchaintest

import (
	"context"
	"fmt"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/dockerutil"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/stretchr/testify/require"

	"github.com/manifest-network/manifest-ledger/interchaintest/helpers"
)

// Package-level variables for backward compatibility with test functions
// that reference these globals. Each test file sets these from billingTestContext.
var (
	testPWRDenom     string
	testProviderUUID string
	testSKUUUID      string
	testSKUUUID2     string
)

// billingTestContext holds shared test state for billing e2e tests.
type billingTestContext struct {
	chain            *cosmos.CosmosChain
	authority        ibc.Wallet
	providerWallet   ibc.Wallet
	tenant1          ibc.Wallet
	tenant2          ibc.Wallet
	unauthorizedUser ibc.Wallet
	pwrDenom         string
	providerUUID     string
	skuUUID          string  // per-hour pricing SKU
	skuUUID2         string  // per-day pricing SKU
}

// setupBillingTest creates chain and infrastructure for billing tests.
// Returns context, test context, and cleanup function.
func setupBillingTest(t *testing.T, testName string) (context.Context, *billingTestContext, func()) {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// Setup chain
	cfgA := LocalChainConfig
	cfgA.Name = testName
	cfgA.WithCodeCoverage()

	chains := interchaintest.CreateChainWithConfig(t, vals, fullNodes, testName, "", cfgA)
	chain := chains[0].(*cosmos.CosmosChain)

	enableBlockDB := false
	ctx, ic, client, _ := interchaintest.BuildInitialChain(t, chains, enableBlockDB)

	// Setup accounts
	authority, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "authority", accMnemonic, DefaultGenesisAmt, chain)
	require.NoError(t, err)

	// Regular users (tenants)
	users := interchaintest.GetAndFundTestUsers(t, ctx, t.Name(), DefaultGenesisAmt, chain, chain, chain, chain)
	tenant1 := users[0]
	tenant2 := users[1]
	providerWallet := users[2]
	unauthorizedUser := users[3]

	tc := &billingTestContext{
		chain:            chain,
		authority:        authority,
		providerWallet:   providerWallet,
		tenant1:          tenant1,
		tenant2:          tenant2,
		unauthorizedUser: unauthorizedUser,
	}

	// Setup test infrastructure (PWR denom, provider, SKUs)
	setupBillingInfrastructure(t, ctx, tc)

	// Return cleanup function
	cleanup := func() {
		dockerutil.CopyCoverageFromContainer(ctx, t, client, chain.GetNode().ContainerID(), chain.HomeDir(), ExternalGoCoverDir)
		_ = ic.Close()
	}

	return ctx, tc, cleanup
}

// setupBillingInfrastructure creates the PWR denom, provider, and SKUs needed for tests.
func setupBillingInfrastructure(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Helper()
	t.Log("=== Setting up Billing Test Infrastructure ===")

	node := tc.chain.GetNode()
	var err error

	// Create PWR denom via tokenfactory
	t.Log("Creating PWR denom...")
	tc.pwrDenom, _, err = node.TokenFactoryCreateDenom(ctx, tc.authority, "upwr", 2_500_00)
	require.NoError(t, err, "failed to create PWR denom")
	t.Logf("Created PWR denom: %s", tc.pwrDenom)

	// Mint PWR tokens to authority for distribution
	t.Log("Minting PWR tokens to authority...")
	_, err = node.TokenFactoryMintDenom(ctx, tc.authority.FormattedAddress(), tc.pwrDenom, 1_000_000_000_000)
	require.NoError(t, err, "failed to mint PWR tokens")

	balance, err := tc.chain.GetBalance(ctx, tc.authority.FormattedAddress(), tc.pwrDenom)
	require.NoError(t, err)
	require.True(t, balance.GT(sdkmath.ZeroInt()), "authority should have PWR balance")
	t.Logf("Authority PWR balance: %s", balance)

	// Create provider
	t.Log("Creating provider...")
	res, err := helpers.SKUCreateProvider(ctx, tc.chain, tc.authority, tc.providerWallet.FormattedAddress(), tc.providerWallet.FormattedAddress(), "")
	require.NoError(t, err, "failed to create provider")

	txRes, err := tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "provider creation should succeed: %s", txRes.RawLog)

	tc.providerUUID, err = helpers.GetProviderUUIDFromTxHash(ctx, tc.chain, res.TxHash)
	require.NoError(t, err)
	t.Logf("Created provider UUID: %s", tc.providerUUID)

	// Create SKU with per-hour pricing (Unit = 1)
	// Price: 3600000 upwr per hour = 1000 per second
	t.Log("Creating SKU (per-hour)...")
	res, err = helpers.SKUCreateSKU(ctx, tc.chain, tc.authority, tc.providerUUID, "Compute Small", 1, fmt.Sprintf("3600000%s", tc.pwrDenom), "")
	require.NoError(t, err, "failed to create per-hour SKU")

	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "SKU creation should succeed: %s", txRes.RawLog)

	tc.skuUUID, err = helpers.GetSKUUUIDFromTxHash(ctx, tc.chain, res.TxHash)
	require.NoError(t, err)
	t.Logf("Created SKU UUID (per-hour): %s", tc.skuUUID)

	// Create SKU with per-day pricing (Unit = 2)
	// Price: 86400000 upwr per day = 1000 per second
	t.Log("Creating SKU (per-day)...")
	res, err = helpers.SKUCreateSKU(ctx, tc.chain, tc.authority, tc.providerUUID, "Storage Large", 2, fmt.Sprintf("86400000%s", tc.pwrDenom), "")
	require.NoError(t, err, "failed to create per-day SKU")

	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "SKU creation should succeed: %s", txRes.RawLog)

	tc.skuUUID2, err = helpers.GetSKUUUIDFromTxHash(ctx, tc.chain, res.TxHash)
	require.NoError(t, err)
	t.Logf("Created SKU UUID (per-day): %s", tc.skuUUID2)

	// Wait for blocks to settle
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))
}

// fundTenantCredit is a helper to fund a tenant's credit account.
func fundTenantCredit(t *testing.T, ctx context.Context, tc *billingTestContext, tenant ibc.Wallet, amount int64) {
	t.Helper()

	// First send PWR to tenant
	err := tc.chain.GetNode().SendFunds(ctx, tc.authority.KeyName(), ibc.WalletAmount{
		Address: tenant.FormattedAddress(),
		Denom:   tc.pwrDenom,
		Amount:  sdkmath.NewInt(amount),
	})
	require.NoError(t, err)
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	// Fund credit account
	fundAmount := fmt.Sprintf("%d%s", amount, tc.pwrDenom)
	res, err := helpers.BillingFundCredit(ctx, tc.chain, tenant, tenant.FormattedAddress(), fundAmount)
	require.NoError(t, err)

	txRes, err := tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "fund credit should succeed: %s", txRes.RawLog)
}
