package interchaintest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/collections"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	grouptypes "github.com/cosmos/cosmos-sdk/x/group"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"

	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/manifest-network/manifest-ledger/interchaintest/helpers"
	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

const (
	haltHeightDelta    = int64(15) // will propose upgrade this many blocks in the future
	blocksAfterUpgrade = int64(7)

	billingParamsStoragePrefix        = "\x00billing/params/v1"
	billingLeaseStoragePrefix         = "\x00billing/lease/v1"
	billingCreditAccountStoragePrefix = "\x00billing/credit-account/v1"
)

var (
	// baseChain is the current version of the chain that will be upgraded from
	baseChain = ibc.DockerImage{
		Repository: "ghcr.io/manifest-network/manifest-ledger",
		Version:    "2.3.0",
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
	user2Wallet, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, user2, acc1Mnemonic, DefaultGenesisAmt, chain)
	require.NoError(t, err)

	requireBillingModuleVersion(t, ctx, chain, "2")
	billingState := populateBillingMigrationState(t, ctx, chain, &cfg, user1Wallet, user2Wallet)
	requireLegacyBillingStorage(t, ctx, chain, billingState)

	// Get current height and calculate halt height
	height, err := chain.Height(ctx)
	require.NoError(t, err, "error fetching height before submit upgrade proposal")

	haltHeight := height + haltHeightDelta

	// The upgrade name must match app.Version() in the new binary
	upgradeName := "v2.3.1"

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

	requireBillingModuleVersion(t, ctx, chain, "3")
	requireBillingMigrationState(t, ctx, chain, billingState)
	requireMigratedBillingStorage(t, ctx, chain, billingState)

	// Test CosmWasm functionality after upgrade
	t.Log("Testing CosmWasm functionality after upgrade")
	StoreAndInstantiateContract(t, ctx, chain, user1Wallet, accAddr)
}

type billingMigrationState struct {
	tenant            string
	tenantAddress     sdk.AccAddress
	creditAddress     string
	creditAddressRaw  sdk.AccAddress
	lease             helpers.LeaseJSON
	creditAccount     billingtypes.CreditAccount
	balances          sdk.Coins
	availableBalances sdk.Coins
	paramsKey         []byte
	leaseKey          []byte
	creditAccountKey  []byte
}

func populateBillingMigrationState(
	t *testing.T,
	ctx context.Context,
	chain *cosmos.CosmosChain,
	cfg *ibc.ChainConfig,
	tenantWallet ibc.Wallet,
	providerWallet ibc.Wallet,
) billingMigrationState {
	t.Helper()
	t.Log("Populating v2 billing state before upgrade")

	providerAddress := providerWallet.FormattedAddress()
	createProviderMsg := createSKUCreateProviderProposal(groupAddr, providerAddress, providerAddress, nil)
	createAndRunProposalSuccess(t, ctx, chain, cfg, accAddr, []*types.Any{createAny(t, &createProviderMsg)})

	providers, err := helpers.SKUQueryProviders(ctx, chain)
	require.NoError(t, err)
	require.Len(t, providers.Providers, 1)
	providerUUID := providers.Providers[0].Uuid
	require.Equal(t, providerAddress, providers.Providers[0].Address)
	require.True(t, providers.Providers[0].Active)

	createSKUMsg := createSKUCreateSKUProposal(
		groupAddr,
		providerUUID,
		"Billing Migration SKU",
		skutypes.Unit_UNIT_PER_HOUR,
		sdk.NewInt64Coin(Denom, 3600),
		nil,
	)
	createAndRunProposalSuccess(t, ctx, chain, cfg, accAddr, []*types.Any{createAny(t, &createSKUMsg)})

	skus, err := helpers.SKUQuerySKUsByProvider(ctx, chain, providerUUID)
	require.NoError(t, err)
	require.Len(t, skus.Skus, 1)
	skuUUID := skus.Skus[0].Uuid
	require.Equal(t, sdk.NewInt64Coin(Denom, 3600), skus.Skus[0].BasePrice)
	require.True(t, skus.Skus[0].Active)

	tenant := tenantWallet.FormattedAddress()
	fundRes, err := helpers.BillingFundCredit(ctx, chain, tenantWallet, tenant, "1000000"+Denom)
	require.NoError(t, err)
	requireSuccessfulUpgradeTestTx(t, chain, fundRes.TxHash, "fund billing credit")

	createLeaseRes, err := helpers.BillingCreateLease(ctx, chain, tenantWallet, []string{skuUUID + ":1"})
	require.NoError(t, err)
	leaseUUID, err := helpers.GetLeaseIDFromTxHash(ctx, chain, createLeaseRes.TxHash)
	require.NoError(t, err)

	ackRes, err := helpers.BillingAcknowledgeLease(ctx, chain, providerWallet, leaseUUID)
	require.NoError(t, err)
	requireSuccessfulUpgradeTestTx(t, chain, ackRes.TxHash, "acknowledge billing lease")

	leaseRes, err := helpers.BillingQueryLease(ctx, chain, leaseUUID)
	require.NoError(t, err)
	require.Equal(t, tenant, leaseRes.Lease.Tenant)
	require.Equal(t, providerUUID, leaseRes.Lease.ProviderUuid)
	require.Equal(t, billingtypes.LEASE_STATE_ACTIVE, leaseRes.Lease.GetState())
	require.Len(t, leaseRes.Lease.Items, 1)
	require.Equal(t, sdk.NewInt64Coin(Denom, 1), leaseRes.Lease.Items[0].LockedPrice)
	require.Equal(t, "3600", leaseRes.Lease.MinLeaseDurationAtCreation)

	creditRes, err := helpers.BillingQueryCreditAccount(ctx, chain, tenant)
	require.NoError(t, err)
	require.Equal(t, uint64(1), creditRes.CreditAccount.ActiveLeaseCount)
	require.Zero(t, creditRes.CreditAccount.PendingLeaseCount)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(Denom, 3600)), creditRes.CreditAccount.ReservedAmounts)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(Denom, 1_000_000)), creditRes.Balances)

	tenantAddress, err := sdk.AccAddressFromBech32(tenant)
	require.NoError(t, err)
	creditAddress, err := sdk.AccAddressFromBech32(creditRes.CreditAccount.CreditAddress)
	require.NoError(t, err)
	leaseKey, err := collections.EncodeKeyWithPrefix(billingtypes.LeaseKey.Bytes(), collections.StringKey, leaseUUID)
	require.NoError(t, err)
	creditAccountKey, err := collections.EncodeKeyWithPrefix(billingtypes.CreditAccountKey.Bytes(), sdk.AccAddressKey, tenantAddress)
	require.NoError(t, err)

	return billingMigrationState{
		tenant:            tenant,
		tenantAddress:     tenantAddress,
		creditAddress:     creditRes.CreditAccount.CreditAddress,
		creditAddressRaw:  creditAddress,
		lease:             leaseRes.Lease,
		creditAccount:     creditRes.CreditAccount,
		balances:          creditRes.Balances,
		availableBalances: creditRes.AvailableBalances,
		paramsKey:         bytes.Clone(billingtypes.ParamsKey.Bytes()),
		leaseKey:          leaseKey,
		creditAccountKey:  creditAccountKey,
	}
}

func requireBillingMigrationState(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, expected billingMigrationState) {
	t.Helper()

	leaseRes, err := helpers.BillingQueryLease(ctx, chain, expected.lease.Uuid)
	require.NoError(t, err)
	require.Equal(t, expected.lease, leaseRes.Lease)

	activeLeases, err := helpers.BillingQueryLeasesByTenant(ctx, chain, expected.tenant, "active")
	require.NoError(t, err)
	require.Len(t, activeLeases.Leases, 1)
	require.Equal(t, expected.lease.Uuid, activeLeases.Leases[0].Uuid)

	creditRes, err := helpers.BillingQueryCreditAccount(ctx, chain, expected.tenant)
	require.NoError(t, err)
	require.Equal(t, expected.creditAccount, creditRes.CreditAccount)
	require.Equal(t, expected.balances, creditRes.Balances)
	require.Equal(t, expected.availableBalances, creditRes.AvailableBalances)
}

func requireLegacyBillingStorage(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, state billingMigrationState) {
	t.Helper()

	rawParams := queryBillingStoreValue(t, ctx, chain, state.paramsKey)
	rawLease := queryBillingStoreValue(t, ctx, chain, state.leaseKey)
	rawCreditAccount := queryBillingStoreValue(t, ctx, chain, state.creditAccountKey)

	require.False(t, bytes.HasPrefix(rawParams, []byte(billingParamsStoragePrefix)))
	require.False(t, bytes.HasPrefix(rawLease, []byte(billingLeaseStoragePrefix)))
	require.False(t, bytes.HasPrefix(rawCreditAccount, []byte(billingCreditAccountStoragePrefix)))
	require.True(t, bytes.Contains(rawLease, []byte(state.tenant)))
	require.True(t, bytes.Contains(rawCreditAccount, []byte(state.tenant)))
	require.True(t, bytes.Contains(rawCreditAccount, []byte(state.creditAddress)))
}

func requireMigratedBillingStorage(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, state billingMigrationState) {
	t.Helper()

	rawParams := queryBillingStoreValue(t, ctx, chain, state.paramsKey)
	rawLease := queryBillingStoreValue(t, ctx, chain, state.leaseKey)
	rawCreditAccount := queryBillingStoreValue(t, ctx, chain, state.creditAccountKey)

	require.True(t, bytes.HasPrefix(rawParams, []byte(billingParamsStoragePrefix)))
	require.True(t, bytes.HasPrefix(rawLease, []byte(billingLeaseStoragePrefix)))
	require.True(t, bytes.HasPrefix(rawCreditAccount, []byte(billingCreditAccountStoragePrefix)))
	require.True(t, bytes.Contains(rawLease, state.tenantAddress.Bytes()))
	require.False(t, bytes.Contains(rawLease, []byte(state.tenant)))
	require.True(t, bytes.Contains(rawCreditAccount, state.tenantAddress.Bytes()))
	require.True(t, bytes.Contains(rawCreditAccount, state.creditAddressRaw.Bytes()))
	require.False(t, bytes.Contains(rawCreditAccount, []byte(state.tenant)))
	require.False(t, bytes.Contains(rawCreditAccount, []byte(state.creditAddress)))
}

func queryBillingStoreValue(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, key []byte) []byte {
	t.Helper()

	res, err := chain.GetNode().Client.ABCIQuery(ctx, "/store/billing/key", key)
	require.NoError(t, err)
	require.Equal(t, uint32(0), res.Response.Code, res.Response.Log)
	require.NotEmpty(t, res.Response.Value)
	return bytes.Clone(res.Response.Value)
}

func requireBillingModuleVersion(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, expected string) {
	t.Helper()

	stdout, _, err := chain.GetNode().ExecQuery(ctx, "upgrade", "module-versions", billingtypes.ModuleName)
	require.NoError(t, err)
	var response struct {
		ModuleVersions []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"module_versions"`
	}
	require.NoError(t, json.Unmarshal(stdout, &response))
	require.Len(t, response.ModuleVersions, 1)
	require.Equal(t, billingtypes.ModuleName, response.ModuleVersions[0].Name)
	require.Equal(t, expected, response.ModuleVersions[0].Version)
}

func requireSuccessfulUpgradeTestTx(t *testing.T, chain *cosmos.CosmosChain, txHash, operation string) {
	t.Helper()

	txRes, err := chain.GetTransaction(txHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "%s failed: %s", operation, txRes.RawLog)
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
