package interchaintest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	grouptypes "github.com/cosmos/cosmos-sdk/x/group"

	"github.com/manifest-network/manifest-ledger/interchaintest/helpers"
	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

const (
	haltHeightDelta    = int64(15) // will propose upgrade this many blocks in the future
	blocksAfterUpgrade = int64(7)
	upgradeVersionEnv  = "MANIFEST_UPGRADE_VERSION"
	baseBinaryVersion  = "v2.3.1"
	baseImageVersion   = "2.3.1"
	baseImageDigest    = "sha256:181e6e0255c8be2678b91fcc3799f91cb5669d1a03f417a5b151c8c1b8a0b8c0"
	baseWasmVMVersion  = "2.2.4"
	nextWasmVMVersion  = "2.2.8"

	legacyProviderUUID      = "01912345-6789-7abc-8def-0123456789a1"
	legacySKUUUID           = "01912345-6789-7abc-8def-0123456789a2"
	inactiveSKUUUID         = "01912345-6789-7abc-8def-0123456789a3"
	legacyLeaseUUID         = "01912345-6789-7abc-8def-0123456789a4"
	legacyCustomDomain      = "legacy-upgrade.example.com"
	expiringCustomDomain    = "expired-upgrade.example.com"
	retainedCustomDomain    = "retained-upgrade.example.com"
	legacyReservationAmount = int64(3600)
	underbackedFundAmount   = int64(2_104_000)
	underbackedPendingUnits = uint64(35_000)
	underbackedPendingFloor = int64(2_100_000)

	billingParamsStoragePrefix        = "\x00billing/params/v1"
	billingLeaseStoragePrefix         = "\x00billing/lease/v1"
	billingCreditAccountStoragePrefix = "\x00billing/credit-account/v1"
	skuParamsStoragePrefix            = "\x00sku/params/v1"
	skuProviderStoragePrefix          = "\x00sku/provider/v1"
)

// These exact v2 JSON shapes deliberately omit every v4-only storage field.
// The pinned v2.3.1 binary validates and imports them through its normal
// InitGenesis path, so the rehearsal covers a real opaque historical lease
// without writing fabricated KV state after the chain starts.
type v231GenesisLeaseItem struct {
	SKUUUID      string   `json:"sku_uuid"`
	Quantity     uint64   `json:"quantity,string"`
	LockedPrice  sdk.Coin `json:"locked_price"`
	CustomDomain string   `json:"custom_domain,omitempty"`
}

type v231GenesisLease struct {
	UUID          string                 `json:"uuid"`
	Tenant        string                 `json:"tenant"`
	ProviderUUID  string                 `json:"provider_uuid"`
	Items         []v231GenesisLeaseItem `json:"items"`
	State         string                 `json:"state"`
	CreatedAt     time.Time              `json:"created_at"`
	LastSettledAt time.Time              `json:"last_settled_at"`
	Acknowledged  *time.Time             `json:"acknowledged_at,omitempty"`
	MetaHash      []byte                 `json:"meta_hash,omitempty"`
}

type v231GenesisCreditAccount struct {
	Tenant            string    `json:"tenant"`
	CreditAddress     string    `json:"credit_address"`
	ActiveLeaseCount  uint64    `json:"active_lease_count,string"`
	PendingLeaseCount uint64    `json:"pending_lease_count,string"`
	ReservedAmounts   sdk.Coins `json:"reserved_amounts"`
}

func v231MigrationGenesisKVs(t *testing.T) []cosmos.GenesisKV {
	t.Helper()

	legacyTenant, err := sdk.AccAddressFromBech32(acc3Addr)
	require.NoError(t, err)
	legacyTime := time.Date(2020, time.January, 2, 3, 4, 5, 0, time.UTC)

	provider := skutypes.Provider{
		Uuid:          legacyProviderUUID,
		Address:       acc2Addr,
		PayoutAddress: accAddr,
		Active:        true,
		ApiUrl:        "https://legacy-upgrade.example.com",
	}
	activeSKU := skutypes.SKU{
		Uuid:         legacySKUUUID,
		ProviderUuid: legacyProviderUUID,
		Name:         "Genesis migration SKU",
		Unit:         skutypes.Unit_UNIT_PER_HOUR,
		BasePrice:    sdk.NewInt64Coin(Denom, 3600),
		Active:       true,
	}
	inactiveSKU := skutypes.SKU{
		Uuid:         inactiveSKUUUID,
		ProviderUuid: legacyProviderUUID,
		Name:         "Inactive genesis migration SKU",
		Unit:         skutypes.Unit_UNIT_PER_HOUR,
		BasePrice:    sdk.NewInt64Coin(Denom, 7200),
		Active:       false,
	}
	legacyLease := v231GenesisLease{
		UUID:         legacyLeaseUUID,
		Tenant:       acc3Addr,
		ProviderUUID: legacyProviderUUID,
		Items: []v231GenesisLeaseItem{{
			SKUUUID:      legacySKUUUID,
			Quantity:     1,
			LockedPrice:  sdk.NewInt64Coin(Denom, 1),
			CustomDomain: legacyCustomDomain,
		}},
		State:         billingtypes.LEASE_STATE_ACTIVE.String(),
		CreatedAt:     legacyTime,
		LastSettledAt: legacyTime,
		Acknowledged:  &legacyTime,
	}
	legacyAccount := v231GenesisCreditAccount{
		Tenant:           acc3Addr,
		CreditAddress:    billingtypes.DeriveCreditAddress(legacyTenant).String(),
		ActiveLeaseCount: 1,
		ReservedAmounts: sdk.NewCoins(
			sdk.NewInt64Coin(Denom, legacyReservationAmount),
		),
	}

	return []cosmos.GenesisKV{
		cosmos.NewGenesisKV("app_state.sku.params.allowed_list", []string{acc2Addr, accAddr}),
		cosmos.NewGenesisKV("app_state.sku.providers", []skutypes.Provider{provider}),
		cosmos.NewGenesisKV("app_state.sku.skus", []skutypes.SKU{activeSKU, inactiveSKU}),
		cosmos.NewGenesisKV("app_state.sku.provider_sequence", "1"),
		cosmos.NewGenesisKV("app_state.sku.sku_sequence", "2"),
		cosmos.NewGenesisKV("app_state.billing.params.allowed_list", []string{accAddr, acc2Addr}),
		cosmos.NewGenesisKV("app_state.billing.leases", []v231GenesisLease{legacyLease}),
		cosmos.NewGenesisKV("app_state.billing.credit_accounts", []v231GenesisCreditAccount{legacyAccount}),
		cosmos.NewGenesisKV("app_state.billing.lease_sequence", "1"),
	}
}

// baseChain is the current version of the chain that will be upgraded from.
var baseChain = ibc.DockerImage{
	Repository: "ghcr.io/manifest-network/manifest-ledger",
	Version:    baseImageVersion + "@" + baseImageDigest,
	UIDGID:     "1025:1025",
}

func init() {
	if err := groupPolicy.SetDecisionPolicy(createThresholdDecisionPolicy("1", 10*time.Second, 0*time.Second)); err != nil {
		panic(err)
	}

	// Initialize the codec so ModifyGenesis can serialize the group policy.
	enc := AppEncoding()
	grouptypes.RegisterInterfaces(enc.InterfaceRegistry)
	cdc := codec.NewProtoCodec(enc.InterfaceRegistry)
	if _, err := cdc.MarshalJSON(groupPolicy); err != nil {
		panic(err)
	}
}

func TestBasicManifestUpgrade(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	upgradeName := os.Getenv(upgradeVersionEnv)
	if upgradeName == "" {
		t.Skipf("%s is unset; run the dedicated chain-upgrade Make target", upgradeVersionEnv)
	}

	// Setup chain with group-based governance
	previousVersionGenesis := append([]cosmos.GenesisKV(nil), DefaultGenesis...)
	previousVersionGenesis = append(previousVersionGenesis, v231MigrationGenesisKVs(t)...)
	previousVersionGenesis = append(previousVersionGenesis,
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

	requireBinaryVersion(ctx, t, chain, baseBinaryVersion)
	requireLibwasmvmVersion(ctx, t, chain, baseWasmVMVersion)

	// Get test users
	user1Wallet, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, user1, accMnemonic, DefaultGenesisAmt, chain)
	require.NoError(t, err)
	user2Wallet, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, user2, acc1Mnemonic, DefaultGenesisAmt, chain)
	require.NoError(t, err)
	denomWallet, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "upgrade-denom-admin", acc3Mnemonic, DefaultGenesisAmt, chain)
	require.NoError(t, err)

	// Persist both contract state and a v2.2.4-compiled module before replacing
	// the binary. Executing this same contract after the upgrade proves that the
	// v2.2.8 VM can restore historical code and rebuild its versioned cache.
	preUpgradeContractAddr := StoreAndInstantiateContract(ctx, t, chain, accAddr)
	incrementContractAndRequireCount(ctx, t, chain, user1Wallet.KeyName(), preUpgradeContractAddr, 1)

	requireModuleVersion(ctx, t, chain, billingtypes.ModuleName, "2")
	requireModuleVersion(ctx, t, chain, skutypes.ModuleName, "1")
	migrationState := populateMigrationState(ctx, t, chain, &cfg, user1Wallet, user2Wallet, denomWallet)
	requireLegacyBillingStorage(ctx, t, chain, migrationState)
	requireLegacySKUStorage(ctx, t, chain, migrationState)

	// Get current height and calculate halt height
	height, err := chain.Height(ctx)
	require.NoError(t, err, "error fetching height before submit upgrade proposal")

	haltHeight := height + haltHeightDelta

	// The upgrade name must match app.Version() in the new binary. The Make
	// target verifies the image-reported version before this test starts.
	require.NotEmpty(t, upgradeName, "%s must name the local upgrade binary", upgradeVersionEnv)
	require.NotEqual(t, baseBinaryVersion, upgradeName,
		"the target must differ from the running version or old nodes can execute the plan")

	t.Logf("Upgrade name: %s", upgradeName)
	t.Logf("Current height: %d, halt height: %d", height, haltHeight)
	t.Log("Submitting upgrade proposal through group")
	upgradeMsg := createUpgradeProposal(groupAddr, upgradeName, haltHeight)

	createAndRunProposalSuccess(ctx, t, chain, &cfg, accAddr, []*types.Any{createAny(t, &upgradeMsg)})
	verifyUpgradePlan(ctx, t, chain, &upgradetypes.Plan{Name: upgradeName, Height: haltHeight})

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
	requireBinaryVersion(ctx, t, chain, upgradeName)
	requireLibwasmvmVersion(ctx, t, chain, nextWasmVMVersion)

	timeoutCtx, timeoutCtxCancel = context.WithTimeout(ctx, time.Second*60)
	defer timeoutCtxCancel()

	err = testutil.WaitForBlocks(timeoutCtx, int(blocksAfterUpgrade), chain)
	require.NoError(t, err, "chain did not produce blocks after upgrade")

	height, err = chain.Height(ctx)
	require.NoError(t, err, "error fetching height after upgrade")

	require.GreaterOrEqual(t, height, haltHeight+blocksAfterUpgrade, "height did not increment enough after upgrade")

	requireModuleVersion(ctx, t, chain, billingtypes.ModuleName, "4")
	requireModuleVersion(ctx, t, chain, skutypes.ModuleName, "2")
	requireBillingMigrationState(ctx, t, chain, migrationState)
	requireSKUMigrationState(ctx, t, chain, migrationState)
	requireMigratedBillingStorage(ctx, t, chain, migrationState)
	requireMigratedSKUStorage(ctx, t, chain, migrationState)
	requirePostUpgradeBillingFunctionality(ctx, t, chain, migrationState, user1Wallet, user2Wallet, denomWallet)
	requirePostUpgradeSKUFunctionality(ctx, t, chain, &cfg, migrationState)

	// The first query executes code that was compiled and cached by v2.2.4.
	// CosmWasm v2.2.9 changes the module cache discriminator from v20 to v21,
	// so v2.2.8 must recompile the original bytecode instead of loading the old
	// artifact. The subsequent write also proves contract state continuity.
	t.Log("Testing pre-upgrade CosmWasm contract after upgrade")
	requireContractCount(ctx, t, chain, preUpgradeContractAddr, 1)
	incrementContractAndRequireCount(ctx, t, chain, user1Wallet.KeyName(), preUpgradeContractAddr, 2)

	// Also exercise code upload and instantiation under the new validation and
	// gas rules rather than covering only historical contracts.
	t.Log("Testing new CosmWasm contract after upgrade")
	StoreAndInstantiateContract(ctx, t, chain, accAddr)
}

type upgradeMigrationState struct {
	billingCohorts          []billingMigrationCohort
	billingParams           billingtypes.Params
	billingAllowedAddresses []sdk.AccAddress
	paramsKey               []byte
	provider                skutypes.Provider
	providerAddress         sdk.AccAddress
	payoutAddress           sdk.AccAddress
	skuParams               skutypes.Params
	sku                     skutypes.SKU
	inactiveSKU             skutypes.SKU
	secondSKU               skutypes.SKU
	activeSKU               skutypes.SKU
	skuParamsKey            []byte
	skuProviderKey          []byte
	legacyLeaseUUID         string
	underbackedActiveUUID   string
	retainedPendingUUID     string
}

type billingMigrationCohort struct {
	name                 string
	tenant               string
	tenantAddress        sdk.AccAddress
	creditAddress        string
	creditAddressRaw     sdk.AccAddress
	leases               []billingMigrationLease
	creditAccount        billingtypes.CreditAccount
	balances             sdk.Coins
	migratedReservation  sdk.Coins
	migratedUnattributed sdk.Coins
	migratedActiveCount  uint64
	migratedPendingCount uint64
	migratedLegacyCount  uint64
	creditAccountKey     []byte
}

type billingMigrationLease struct {
	lease               helpers.LeaseJSON
	migratedState       string
	migratedReservation sdk.Coins
	key                 []byte
}

func populateMigrationState(
	ctx context.Context,
	t *testing.T,
	chain *cosmos.CosmosChain,
	cfg *ibc.ChainConfig,
	tenantWallet ibc.Wallet,
	providerWallet ibc.Wallet,
	denomWallet ibc.Wallet,
) upgradeMigrationState {
	t.Helper()
	t.Log("Populating v2 billing and v1 SKU state before upgrade")

	providerAddress := providerWallet.FormattedAddress()
	payoutAddress := tenantWallet.FormattedAddress()
	skuParams := skutypes.Params{AllowedList: []string{providerAddress, payoutAddress}}
	paramsRes, err := helpers.SKUQueryParams(ctx, chain)
	require.NoError(t, err)
	require.Equal(t, skuParams, paramsRes.Params)

	providers, err := helpers.SKUQueryProviders(ctx, chain)
	require.NoError(t, err)
	require.Len(t, providers.Providers, 1)
	provider := providers.Providers[0]
	providerUUID := provider.Uuid
	require.Equal(t, legacyProviderUUID, providerUUID)
	require.Equal(t, providerAddress, provider.Address)
	require.Equal(t, payoutAddress, provider.PayoutAddress)
	require.True(t, provider.Active)

	skus, err := helpers.SKUQuerySKUsByProvider(ctx, chain, providerUUID)
	require.NoError(t, err)
	require.Len(t, skus.Skus, 2)
	var sku, inactiveSKU skutypes.SKU
	for _, candidate := range skus.Skus {
		switch candidate.Uuid {
		case legacySKUUUID:
			sku = candidate
		case inactiveSKUUUID:
			inactiveSKU = candidate
		}
	}
	require.NotEmpty(t, sku.Uuid)
	require.NotEmpty(t, inactiveSKU.Uuid)
	skuUUID := sku.Uuid
	require.Equal(t, sdk.NewInt64Coin(Denom, 3600), sku.BasePrice)
	require.True(t, sku.Active)
	require.False(t, inactiveSKU.Active)

	legacyFundRes, err := helpers.BillingFundCredit(
		ctx,
		chain,
		denomWallet,
		denomWallet.FormattedAddress(),
		fmt.Sprintf("%d%s", legacyReservationAmount, Denom),
	)
	require.NoError(t, err)
	requireSuccessfulUpgradeTestTx(t, chain, legacyFundRes.TxHash, "fund opaque legacy billing cohort")
	legacyLeaseRes, err := helpers.BillingQueryLease(ctx, chain, legacyLeaseUUID)
	require.NoError(t, err)
	require.Equal(t, billingtypes.LEASE_STATE_ACTIVE, legacyLeaseRes.Lease.GetState())
	require.Empty(t, legacyLeaseRes.Lease.MinLeaseDurationAtCreation)
	legacyDomainRes, err := helpers.BillingQueryLeaseByCustomDomain(ctx, chain, legacyCustomDomain)
	require.NoError(t, err)
	require.Equal(t, legacyLeaseUUID, legacyDomainRes.Lease.UUID)
	legacyCreditRes, err := helpers.BillingQueryCreditAccount(ctx, chain, denomWallet.FormattedAddress())
	require.NoError(t, err)
	legacyReservation := sdk.NewCoins(sdk.NewInt64Coin(Denom, legacyReservationAmount))
	require.True(t, legacyReservation.Equal(legacyCreditRes.Balances))
	require.True(t, legacyReservation.Equal(legacyCreditRes.CreditAccount.ReservedAmounts))
	legacyCohort := captureBillingMigrationCohort(
		t,
		"opaque zero-duration ACTIVE legacy lease",
		denomWallet.FormattedAddress(),
		[]billingMigrationLease{{
			lease:               legacyLeaseRes.Lease,
			migratedState:       "LEASE_STATE_ACTIVE",
			migratedReservation: sdk.NewCoins(),
		}},
		legacyCreditRes,
		legacyReservation,
		legacyReservation,
		1,
		0,
		1,
	)

	// Create a second denom through the public v2.3.1 tokenfactory API. A third
	// wallet owns the denom so its factory path cannot make the billing raw-value
	// checks mistake a denom component for a serialized tenant address.
	secondDenom, _, err := chain.GetNode().TokenFactoryCreateDenom(ctx, denomWallet, "upgrade-migration", 250_000)
	require.NoError(t, err)
	_, err = chain.GetNode().TokenFactoryMintDenom(ctx, denomWallet.KeyName(), secondDenom, 10_000_000)
	require.NoError(t, err)

	createSecondSKUMsg := createSKUCreateSKUProposal(
		providerUUID,
		"Billing Migration Secondary SKU",
		skutypes.Unit_UNIT_PER_HOUR,
		sdk.NewInt64Coin(secondDenom, 3600),
	)
	createAndRunProposalSuccess(ctx, t, chain, cfg, accAddr, []*types.Any{createAny(t, &createSecondSKUMsg)})

	skus, err = helpers.SKUQuerySKUsByProvider(ctx, chain, providerUUID)
	require.NoError(t, err)
	require.Len(t, skus.Skus, 3)
	var secondSKU skutypes.SKU
	for _, candidate := range skus.Skus {
		if candidate.BasePrice.Denom == secondDenom {
			secondSKU = candidate
			break
		}
	}
	require.NotEmpty(t, secondSKU.Uuid)
	require.Equal(t, sdk.NewInt64Coin(secondDenom, 3600), secondSKU.BasePrice)
	require.True(t, secondSKU.Active)

	createActiveSKUMsg := createSKUCreateSKUProposal(
		providerUUID,
		"Billing Migration High-rate SKU",
		skutypes.Unit_UNIT_PER_HOUR,
		sdk.NewInt64Coin(Denom, 360_000),
	)
	createAndRunProposalSuccess(ctx, t, chain, cfg, accAddr, []*types.Any{createAny(t, &createActiveSKUMsg)})

	skus, err = helpers.SKUQuerySKUsByProvider(ctx, chain, providerUUID)
	require.NoError(t, err)
	require.Len(t, skus.Skus, 4)
	var activeSKU skutypes.SKU
	for _, candidate := range skus.Skus {
		if candidate.BasePrice.Equal(sdk.NewInt64Coin(Denom, 360_000)) {
			activeSKU = candidate
			break
		}
	}
	require.NotEmpty(t, activeSKU.Uuid)
	require.True(t, activeSKU.Active)

	// Lowering the minimum duration through governance is a normal v2.3.1
	// lifecycle operation. Thirty seconds leaves a wide observation window in
	// which an ACTIVE withdrawal can consume beyond its own historical claim
	// without exhausting the account.
	billingParamsRes, err := helpers.BillingQueryParams(ctx, chain)
	require.NoError(t, err)
	require.Equal(t,
		[]string{payoutAddress, providerAddress},
		billingParamsRes.Params.AllowedList,
		"v2.3.1 billing AllowedList fixture must remain nonempty",
	)
	migrationBillingParams := billingParamsRes.Params
	migrationBillingParams.MinLeaseDuration = 30
	updateBillingParamsMsg := billingtypes.MsgUpdateParams{
		Authority: groupAddr,
		Params:    migrationBillingParams,
	}
	createAndRunProposalSuccess(ctx, t, chain, cfg, accAddr, []*types.Any{createAny(t, &updateBillingParamsMsg)})
	billingParamsRes, err = helpers.BillingQueryParams(ctx, chain)
	require.NoError(t, err)
	require.Equal(t, uint64(30), billingParamsRes.Params.MinLeaseDuration)

	underbackedTenant := tenantWallet.FormattedAddress()
	err = chain.GetNode().BankSend(ctx, denomWallet.KeyName(), ibc.WalletAmount{
		Address: underbackedTenant,
		Denom:   secondDenom,
		Amount:  sdkmath.NewInt(1000),
	})
	require.NoError(t, err)

	fundRes, err := helpers.BillingFundCredit(
		ctx,
		chain,
		tenantWallet,
		underbackedTenant,
		fmt.Sprintf("%d%s", underbackedFundAmount, Denom),
	)
	require.NoError(t, err)
	requireSuccessfulUpgradeTestTx(t, chain, fundRes.TxHash, "fund under-backed billing cohort in native denom")
	fundRes, err = helpers.BillingFundCredit(ctx, chain, tenantWallet, underbackedTenant, "1000"+secondDenom)
	require.NoError(t, err)
	requireSuccessfulUpgradeTestTx(t, chain, fundRes.TxHash, "fund under-backed billing cohort in tokenfactory denom")

	createLeaseRes, err := helpers.BillingCreateLease(ctx, chain, tenantWallet, []string{
		activeSKU.Uuid + ":1",
		secondSKU.Uuid + ":1",
	})
	require.NoError(t, err)
	activeLeaseUUID, err := helpers.GetLeaseIDFromTxHash(ctx, chain, createLeaseRes.TxHash)
	require.NoError(t, err)

	ackRes, err := helpers.BillingAcknowledgeLease(ctx, chain, providerWallet, activeLeaseUUID)
	require.NoError(t, err)
	requireSuccessfulUpgradeTestTx(t, chain, ackRes.TxHash, "acknowledge billing lease")

	pendingLeaseSnapshots := make([]helpers.LeaseJSON, 0, 2)
	for pendingIndex := 0; pendingIndex < 2; pendingIndex++ {
		pendingCreateRes, err := helpers.BillingCreateLease(
			ctx,
			chain,
			tenantWallet,
			[]string{fmt.Sprintf("%s:%d", skuUUID, underbackedPendingUnits)},
		)
		require.NoError(t, err)
		pendingLeaseUUID, err := helpers.GetLeaseIDFromTxHash(ctx, chain, pendingCreateRes.TxHash)
		require.NoError(t, err)
		if pendingIndex == 0 {
			domainRes, err := helpers.BillingSetItemCustomDomain(
				ctx,
				chain,
				tenantWallet,
				pendingLeaseUUID,
				"",
				expiringCustomDomain,
			)
			require.NoError(t, err)
			requireSuccessfulUpgradeTestTx(t, chain, domainRes.TxHash, "claim domain on under-backed PENDING lease")
			claimed, err := helpers.BillingQueryLeaseByCustomDomain(ctx, chain, expiringCustomDomain)
			require.NoError(t, err)
			require.Equal(t, pendingLeaseUUID, claimed.Lease.UUID)
		}
		pendingLeaseRes, err := helpers.BillingQueryLease(ctx, chain, pendingLeaseUUID)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_PENDING, pendingLeaseRes.Lease.GetState())
		require.Equal(t, "30", pendingLeaseRes.Lease.MinLeaseDurationAtCreation)
		pendingLeaseSnapshots = append(pendingLeaseSnapshots, pendingLeaseRes.Lease)
	}

	// Native reservations are 3,000 for ACTIVE and 2,100,000 across the two
	// PENDING leases. Once the ACTIVE lease consumes more than the 4,000 native
	// balance above the PENDING floor, the bank can no longer back that cohort.
	// Almost all remaining credit is then unreserved after migration, leaving
	// hours rather than seconds for the post-upgrade lifecycle assertions.
	underbackingObserved := false
	for observation := 0; observation < 120; observation++ {
		withdrawable, err := helpers.BillingQueryWithdrawable(ctx, chain, activeLeaseUUID)
		require.NoError(t, err)
		nativeWithdrawable := withdrawable.Amounts.AmountOf(Denom)
		if nativeWithdrawable.GT(sdkmath.NewInt(underbackedFundAmount - underbackedPendingFloor)) {
			underbackingObserved = true
			break
		}
		require.NoError(t, testutil.WaitForBlocks(ctx, 1, chain))
	}
	require.True(t, underbackingObserved, "failed to observe ACTIVE consumption beyond its reservation")

	withdrawRes, err := helpers.BillingWithdraw(ctx, chain, providerWallet, activeLeaseUUID)
	require.NoError(t, err)
	requireSuccessfulUpgradeTestTx(t, chain, withdrawRes.TxHash, "partially consume active billing credit")

	activeLeaseRes, err := helpers.BillingQueryLease(ctx, chain, activeLeaseUUID)
	require.NoError(t, err)
	require.Equal(t, underbackedTenant, activeLeaseRes.Lease.Tenant)
	require.Equal(t, providerUUID, activeLeaseRes.Lease.ProviderUUID)
	require.Equal(t, billingtypes.LEASE_STATE_ACTIVE, activeLeaseRes.Lease.GetState())
	require.Len(t, activeLeaseRes.Lease.Items, 2)
	require.Equal(t, "30", activeLeaseRes.Lease.MinLeaseDurationAtCreation)

	activeNominalReservation := sdk.NewCoins(
		sdk.NewInt64Coin(Denom, 3000),
		sdk.NewInt64Coin(secondDenom, 30),
	)
	pendingNominalReservation := sdk.NewCoins(sdk.NewInt64Coin(Denom, underbackedPendingFloor))
	oldAggregateReservation := activeNominalReservation.Add(pendingNominalReservation...)
	activeCreditRes, err := helpers.BillingQueryCreditAccount(ctx, chain, underbackedTenant)
	require.NoError(t, err)
	require.Equal(t, uint64(1), activeCreditRes.CreditAccount.ActiveLeaseCount)
	require.Equal(t, uint64(2), activeCreditRes.CreditAccount.PendingLeaseCount)
	require.True(t, oldAggregateReservation.Equal(activeCreditRes.CreditAccount.ReservedAmounts))
	nativeBalance := activeCreditRes.Balances.AmountOf(Denom)
	require.True(t, nativeBalance.IsPositive())
	require.True(t, nativeBalance.LT(pendingNominalReservation.AmountOf(Denom)), "PENDING native claims must be under-backed")
	require.True(t, nativeBalance.GTE(activeNominalReservation.AmountOf(Denom)), "ACTIVE native claim must remain fully backed")
	secondBalance := activeCreditRes.Balances.AmountOf(secondDenom)
	require.True(t, secondBalance.GTE(activeNominalReservation.AmountOf(secondDenom)))

	migratedActiveReservation := sdk.NewCoins(
		sdk.NewInt64Coin(Denom, 3000),
		sdk.NewInt64Coin(secondDenom, 30),
	)
	underbackedLeases := make([]billingMigrationLease, 0, 1+len(pendingLeaseSnapshots))
	underbackedLeases = append(underbackedLeases, billingMigrationLease{
		lease:               activeLeaseRes.Lease,
		migratedState:       "LEASE_STATE_ACTIVE",
		migratedReservation: migratedActiveReservation,
	})
	for _, pendingLease := range pendingLeaseSnapshots {
		underbackedLeases = append(underbackedLeases, billingMigrationLease{
			lease:               pendingLease,
			migratedState:       "LEASE_STATE_EXPIRED",
			migratedReservation: sdk.NewCoins(),
		})
	}
	underbackedCohort := captureBillingMigrationCohort(
		t,
		"same-tenant ACTIVE plus under-backed PENDING leases",
		underbackedTenant,
		underbackedLeases,
		activeCreditRes,
		migratedActiveReservation,
		sdk.NewCoins(),
		1,
		0,
		0,
	)

	controlTenant := providerWallet.FormattedAddress()
	fundRes, err = helpers.BillingFundCredit(ctx, chain, providerWallet, controlTenant, "30"+Denom)
	require.NoError(t, err)
	requireSuccessfulUpgradeTestTx(t, chain, fundRes.TxHash, "fund fully-backed control credit")
	pendingCreateRes, err := helpers.BillingCreateLease(ctx, chain, providerWallet, []string{skuUUID + ":1"})
	require.NoError(t, err)
	pendingLeaseUUID, err := helpers.GetLeaseIDFromTxHash(ctx, chain, pendingCreateRes.TxHash)
	require.NoError(t, err)
	domainRes, err := helpers.BillingSetItemCustomDomain(
		ctx,
		chain,
		providerWallet,
		pendingLeaseUUID,
		"",
		retainedCustomDomain,
	)
	require.NoError(t, err)
	requireSuccessfulUpgradeTestTx(t, chain, domainRes.TxHash, "claim domain on retained PENDING lease")
	claimed, err := helpers.BillingQueryLeaseByCustomDomain(ctx, chain, retainedCustomDomain)
	require.NoError(t, err)
	require.Equal(t, pendingLeaseUUID, claimed.Lease.UUID)
	pendingLeaseRes, err := helpers.BillingQueryLease(ctx, chain, pendingLeaseUUID)
	require.NoError(t, err)
	require.Equal(t, billingtypes.LEASE_STATE_PENDING, pendingLeaseRes.Lease.GetState())
	require.Equal(t, "30", pendingLeaseRes.Lease.MinLeaseDurationAtCreation)

	controlReservation := sdk.NewCoins(sdk.NewInt64Coin(Denom, 30))
	controlCreditRes, err := helpers.BillingQueryCreditAccount(ctx, chain, controlTenant)
	require.NoError(t, err)
	require.Zero(t, controlCreditRes.CreditAccount.ActiveLeaseCount)
	require.Equal(t, uint64(1), controlCreditRes.CreditAccount.PendingLeaseCount)
	require.True(t, controlReservation.Equal(controlCreditRes.CreditAccount.ReservedAmounts))
	require.True(t, controlReservation.Equal(controlCreditRes.Balances))
	require.True(t, controlCreditRes.AvailableBalances.IsZero())
	controlCohort := captureBillingMigrationCohort(
		t,
		"fully backed separate-tenant PENDING control",
		controlTenant,
		[]billingMigrationLease{{
			lease:               pendingLeaseRes.Lease,
			migratedState:       "LEASE_STATE_PENDING",
			migratedReservation: controlReservation,
		}},
		controlCreditRes,
		controlReservation,
		sdk.NewCoins(),
		0,
		1,
		0,
	)

	providerAddressRaw, err := sdk.AccAddressFromBech32(providerAddress)
	require.NoError(t, err)
	payoutAddressRaw, err := sdk.AccAddressFromBech32(payoutAddress)
	require.NoError(t, err)
	billingAllowedAddresses := make([]sdk.AccAddress, 0, len(billingParamsRes.Params.AllowedList))
	for _, address := range billingParamsRes.Params.AllowedList {
		decoded, err := sdk.AccAddressFromBech32(address)
		require.NoError(t, err)
		billingAllowedAddresses = append(billingAllowedAddresses, decoded)
	}
	skuProviderKey, err := collections.EncodeKeyWithPrefix(skutypes.ProviderKey.Bytes(), collections.StringKey, providerUUID)
	require.NoError(t, err)

	return upgradeMigrationState{
		billingCohorts:          []billingMigrationCohort{legacyCohort, underbackedCohort, controlCohort},
		billingParams:           billingParamsRes.Params,
		billingAllowedAddresses: billingAllowedAddresses,
		paramsKey:               bytes.Clone(billingtypes.ParamsKey.Bytes()),
		skuParams:               skuParams,
		provider:                provider,
		providerAddress:         providerAddressRaw,
		payoutAddress:           payoutAddressRaw,
		sku:                     sku,
		inactiveSKU:             inactiveSKU,
		secondSKU:               secondSKU,
		activeSKU:               activeSKU,
		skuParamsKey:            bytes.Clone(skutypes.ParamsKey.Bytes()),
		skuProviderKey:          skuProviderKey,
		legacyLeaseUUID:         legacyLeaseUUID,
		underbackedActiveUUID:   activeLeaseUUID,
		retainedPendingUUID:     pendingLeaseUUID,
	}
}

func captureBillingMigrationCohort(
	t *testing.T,
	name string,
	tenant string,
	leases []billingMigrationLease,
	creditRes *helpers.CreditAccountResponseJSON,
	migratedReservation sdk.Coins,
	migratedUnattributed sdk.Coins,
	migratedActiveCount,
	migratedPendingCount,
	migratedLegacyCount uint64,
) billingMigrationCohort {
	t.Helper()

	tenantAddress, err := sdk.AccAddressFromBech32(tenant)
	require.NoError(t, err)
	creditAddress, err := sdk.AccAddressFromBech32(creditRes.CreditAccount.CreditAddress)
	require.NoError(t, err)
	leases = append([]billingMigrationLease(nil), leases...)
	for index := range leases {
		leaseKey, err := collections.EncodeKeyWithPrefix(
			billingtypes.LeaseKey.Bytes(),
			collections.StringKey,
			leases[index].lease.UUID,
		)
		require.NoError(t, err)
		leases[index].key = leaseKey
	}
	creditAccountKey, err := collections.EncodeKeyWithPrefix(
		billingtypes.CreditAccountKey.Bytes(),
		sdk.AccAddressKey,
		tenantAddress,
	)
	require.NoError(t, err)

	return billingMigrationCohort{
		name:                 name,
		tenant:               tenant,
		tenantAddress:        tenantAddress,
		creditAddress:        creditRes.CreditAccount.CreditAddress,
		creditAddressRaw:     creditAddress,
		leases:               leases,
		creditAccount:        creditRes.CreditAccount,
		balances:             creditRes.Balances,
		migratedReservation:  migratedReservation,
		migratedUnattributed: migratedUnattributed,
		migratedActiveCount:  migratedActiveCount,
		migratedPendingCount: migratedPendingCount,
		migratedLegacyCount:  migratedLegacyCount,
		creditAccountKey:     creditAccountKey,
	}
}

func requireBillingMigrationState(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, expected upgradeMigrationState) {
	t.Helper()
	paramsRes, err := helpers.BillingQueryParams(ctx, chain)
	require.NoError(t, err)
	require.Equal(t, expected.billingParams, paramsRes.Params)

	for _, cohort := range expected.billingCohorts {
		for _, expectedLeaseState := range cohort.leases {
			leaseRes, err := helpers.BillingQueryLease(ctx, chain, expectedLeaseState.lease.UUID)
			require.NoError(t, err, cohort.name)
			expectedLease := expectedLeaseState.lease
			expectedLease.State = expectedLeaseState.migratedState
			require.NotNil(t, leaseRes.Lease.Reservation, cohort.name)
			require.True(t,
				expectedLeaseState.migratedReservation.Equal(leaseRes.Lease.Reservation.RemainingAmounts),
				cohort.name,
			)
			expectedLease.Reservation = leaseRes.Lease.Reservation
			if expectedLeaseState.migratedState == "LEASE_STATE_EXPIRED" {
				require.NotNil(t, leaseRes.Lease.ExpiredAt, cohort.name)
				require.False(t, leaseRes.Lease.ExpiredAt.IsZero(), cohort.name)
				expectedLease.ExpiredAt = leaseRes.Lease.ExpiredAt
			}
			require.Equal(t, expectedLease, leaseRes.Lease, cohort.name)
		}

		stateQueries := []struct {
			query      string
			protoState string
		}{
			{query: "active", protoState: "LEASE_STATE_ACTIVE"},
			{query: "pending", protoState: "LEASE_STATE_PENDING"},
			{query: "expired", protoState: "LEASE_STATE_EXPIRED"},
		}
		for _, stateQuery := range stateQueries {
			expectedUUIDs := make([]string, 0)
			for _, lease := range cohort.leases {
				if lease.migratedState == stateQuery.protoState {
					expectedUUIDs = append(expectedUUIDs, lease.lease.UUID)
				}
			}
			leasesByState, err := helpers.BillingQueryLeasesByTenant(ctx, chain, cohort.tenant, stateQuery.query)
			require.NoError(t, err, cohort.name)
			actualUUIDs := make([]string, 0, len(leasesByState.Leases))
			for _, lease := range leasesByState.Leases {
				actualUUIDs = append(actualUUIDs, lease.UUID)
			}
			slices.Sort(expectedUUIDs)
			slices.Sort(actualUUIDs)
			require.Equal(t, expectedUUIDs, actualUUIDs, "%s %s index", cohort.name, stateQuery.query)
		}

		creditRes, err := helpers.BillingQueryCreditAccount(ctx, chain, cohort.tenant)
		require.NoError(t, err, cohort.name)
		require.Equal(t, cohort.creditAccount.Tenant, creditRes.CreditAccount.Tenant, cohort.name)
		require.Equal(t, cohort.creditAccount.CreditAddress, creditRes.CreditAccount.CreditAddress, cohort.name)
		require.Equal(t, cohort.migratedActiveCount, creditRes.CreditAccount.ActiveLeaseCount, cohort.name)
		require.Equal(t, cohort.migratedPendingCount, creditRes.CreditAccount.PendingLeaseCount, cohort.name)
		require.True(t, cohort.migratedReservation.Equal(creditRes.CreditAccount.ReservedAmounts), cohort.name)
		require.True(t,
			cohort.migratedUnattributed.Equal(creditRes.CreditAccount.UnattributedReservedAmounts),
			cohort.name,
		)
		require.Equal(t, cohort.migratedLegacyCount, creditRes.CreditAccount.UnattributedLeaseCount, cohort.name)
		require.Equal(t, cohort.balances, creditRes.Balances, cohort.name)
		expectedAvailable := billingtypes.GetAvailableCredit(cohort.balances, cohort.migratedReservation)
		require.True(t, expectedAvailable.Equal(creditRes.AvailableBalances), cohort.name)
	}

	requireBillingMigrationStateIndexes(ctx, t, chain, expected)
	legacyDomain, err := helpers.BillingQueryLeaseByCustomDomain(ctx, chain, legacyCustomDomain)
	require.NoError(t, err)
	require.Equal(t, expected.legacyLeaseUUID, legacyDomain.Lease.UUID)
	_, err = helpers.BillingQueryLeaseByCustomDomain(ctx, chain, expiringCustomDomain)
	require.Error(t, err, "expired migration lease must release its custom-domain index")
	retainedDomain, err := helpers.BillingQueryLeaseByCustomDomain(ctx, chain, retainedCustomDomain)
	require.NoError(t, err)
	require.Equal(t, expected.retainedPendingUUID, retainedDomain.Lease.UUID)
}

func requireBillingMigrationStateIndexes(
	ctx context.Context,
	t *testing.T,
	chain *cosmos.CosmosChain,
	expected upgradeMigrationState,
) {
	t.Helper()

	stateQueries := []struct {
		query      string
		protoState string
	}{
		{query: "active", protoState: "LEASE_STATE_ACTIVE"},
		{query: "pending", protoState: "LEASE_STATE_PENDING"},
		{query: "expired", protoState: "LEASE_STATE_EXPIRED"},
	}
	providerUUIDs := make([]string, 0)
	for _, cohort := range expected.billingCohorts {
		for _, lease := range cohort.leases {
			providerUUID := lease.lease.ProviderUUID
			if !slices.Contains(providerUUIDs, providerUUID) {
				providerUUIDs = append(providerUUIDs, providerUUID)
			}
		}
	}
	slices.Sort(providerUUIDs)

	for _, stateQuery := range stateQueries {
		expectedGlobalUUIDs := billingMigrationLeaseUUIDs(expected, stateQuery.protoState, "")
		globalLeases, err := helpers.BillingQueryLeases(ctx, chain, stateQuery.query)
		require.NoError(t, err)
		require.Equal(t, expectedGlobalUUIDs, sortedBillingLeaseUUIDs(globalLeases.Leases),
			"global %s index", stateQuery.query)

		for _, providerUUID := range providerUUIDs {
			expectedProviderUUIDs := billingMigrationLeaseUUIDs(expected, stateQuery.protoState, providerUUID)
			providerLeases, err := helpers.BillingQueryLeasesByProvider(ctx, chain, providerUUID, stateQuery.query)
			require.NoError(t, err)
			require.Equal(t, expectedProviderUUIDs, sortedBillingLeaseUUIDs(providerLeases.Leases),
				"provider %s %s index", providerUUID, stateQuery.query)
		}
	}
}

func billingMigrationLeaseUUIDs(expected upgradeMigrationState, state, providerUUID string) []string {
	uuids := make([]string, 0)
	for _, cohort := range expected.billingCohorts {
		for _, lease := range cohort.leases {
			if lease.migratedState == state && (providerUUID == "" || lease.lease.ProviderUUID == providerUUID) {
				uuids = append(uuids, lease.lease.UUID)
			}
		}
	}
	slices.Sort(uuids)
	return uuids
}

func sortedBillingLeaseUUIDs(leases []helpers.LeaseJSON) []string {
	uuids := make([]string, 0, len(leases))
	for _, lease := range leases {
		uuids = append(uuids, lease.UUID)
	}
	slices.Sort(uuids)
	return uuids
}

func requireSKUMigrationState(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, expected upgradeMigrationState) {
	t.Helper()

	paramsRes, err := helpers.SKUQueryParams(ctx, chain)
	require.NoError(t, err)
	require.Equal(t, expected.skuParams, paramsRes.Params)

	providerRes, err := helpers.SKUQueryProvider(ctx, chain, expected.provider.Uuid)
	require.NoError(t, err)
	require.Equal(t, expected.provider, providerRes.Provider)

	providersByAddress, err := helpers.SKUQueryProviderByAddress(ctx, chain, expected.provider.Address)
	require.NoError(t, err)
	require.Len(t, providersByAddress.Providers, 1)
	require.Equal(t, expected.provider, providersByAddress.Providers[0])
	activeProviders, err := helpers.SKUQueryActiveProviders(ctx, chain)
	require.NoError(t, err)
	require.Equal(t, []skutypes.Provider{expected.provider}, activeProviders.Providers)
	activeProvidersByAddress, err := helpers.SKUQueryActiveProviderByAddress(ctx, chain, expected.provider.Address)
	require.NoError(t, err)
	require.Equal(t, []skutypes.Provider{expected.provider}, activeProvidersByAddress.Providers)

	skuRes, err := helpers.SKUQuerySKU(ctx, chain, expected.sku.Uuid)
	require.NoError(t, err)
	require.Equal(t, expected.sku, skuRes.Sku)
	inactiveSKURes, err := helpers.SKUQuerySKU(ctx, chain, expected.inactiveSKU.Uuid)
	require.NoError(t, err)
	require.Equal(t, expected.inactiveSKU, inactiveSKURes.Sku)
	secondSKURes, err := helpers.SKUQuerySKU(ctx, chain, expected.secondSKU.Uuid)
	require.NoError(t, err)
	require.Equal(t, expected.secondSKU, secondSKURes.Sku)
	activeSKURes, err := helpers.SKUQuerySKU(ctx, chain, expected.activeSKU.Uuid)
	require.NoError(t, err)
	require.Equal(t, expected.activeSKU, activeSKURes.Sku)

	skusByProvider, err := helpers.SKUQuerySKUsByProvider(ctx, chain, expected.provider.Uuid)
	require.NoError(t, err)
	expectedSKUs := sortedSKUs([]skutypes.SKU{expected.sku, expected.inactiveSKU, expected.secondSKU, expected.activeSKU})
	require.Equal(t, expectedSKUs, sortedSKUs(skusByProvider.Skus), "SKUsByProvider migrated set")

	activeSKUs, err := helpers.SKUQueryActiveSKUs(ctx, chain)
	require.NoError(t, err)
	expectedActiveSKUs := sortedSKUs([]skutypes.SKU{expected.sku, expected.secondSKU, expected.activeSKU})
	require.Equal(t, expectedActiveSKUs, sortedSKUs(activeSKUs.Skus), "active SKU index")
	activeSKUsByProvider, err := helpers.SKUQueryActiveSKUsByProvider(ctx, chain, expected.provider.Uuid)
	require.NoError(t, err)
	require.Equal(t, expectedActiveSKUs, sortedSKUs(activeSKUsByProvider.Skus), "provider-active SKU index")
}

func sortedSKUs(input []skutypes.SKU) []skutypes.SKU {
	result := append([]skutypes.SKU(nil), input...)
	slices.SortFunc(result, func(a, b skutypes.SKU) int {
		return strings.Compare(a.Uuid, b.Uuid)
	})
	return result
}

func requirePostUpgradeBillingFunctionality(
	ctx context.Context,
	t *testing.T,
	chain *cosmos.CosmosChain,
	state upgradeMigrationState,
	tenantWallet,
	providerWallet,
	legacyTenantWallet ibc.Wallet,
) {
	t.Helper()
	t.Log("Exercising migrated billing state after upgrade")

	// A migrated opaque lease must remain operable. Closing it consumes the
	// shared legacy allocation, clears the legacy counter, and removes its live
	// custom-domain index while retaining the domain on the audit record.
	legacyPayoutBefore, err := chain.GetBalance(ctx, tenantWallet.FormattedAddress(), Denom)
	require.NoError(t, err)
	closeRes, err := helpers.BillingCloseLease(ctx, chain, legacyTenantWallet, state.legacyLeaseUUID)
	require.NoError(t, err)
	requireSuccessfulUpgradeTestTx(t, chain, closeRes.TxHash, "close migrated opaque legacy lease")
	closedLegacy, err := helpers.BillingQueryLease(ctx, chain, state.legacyLeaseUUID)
	require.NoError(t, err)
	require.Equal(t, billingtypes.LEASE_STATE_CLOSED, closedLegacy.Lease.GetState())
	require.Equal(t, legacyCustomDomain, closedLegacy.Lease.Items[0].CustomDomain)
	legacyAccount, err := helpers.BillingQueryCreditAccount(ctx, chain, legacyTenantWallet.FormattedAddress())
	require.NoError(t, err)
	require.Zero(t, legacyAccount.CreditAccount.ActiveLeaseCount)
	require.Zero(t, legacyAccount.CreditAccount.UnattributedLeaseCount)
	require.True(t, legacyAccount.CreditAccount.ReservedAmounts.IsZero())
	require.True(t, legacyAccount.CreditAccount.UnattributedReservedAmounts.IsZero())
	require.True(t, legacyAccount.Balances.IsZero())
	legacyPayoutAfter, err := chain.GetBalance(ctx, tenantWallet.FormattedAddress(), Denom)
	require.NoError(t, err)
	require.Equal(t,
		sdkmath.NewInt(legacyReservationAmount),
		legacyPayoutAfter.Sub(legacyPayoutBefore),
		"legacy close must transfer exactly its migrated allocation",
	)
	_, err = helpers.BillingQueryLeaseByCustomDomain(ctx, chain, legacyCustomDomain)
	require.Error(t, err, "closing the legacy lease must release its domain")

	// A fully-backed PENDING lease retained by migration must still activate
	// through the current acknowledgement gates without changing its allocation.
	ackRes, err := helpers.BillingAcknowledgeLease(ctx, chain, providerWallet, state.retainedPendingUUID)
	require.NoError(t, err)
	requireSuccessfulUpgradeTestTx(t, chain, ackRes.TxHash, "acknowledge retained migrated lease")
	retainedLease, err := helpers.BillingQueryLease(ctx, chain, state.retainedPendingUUID)
	require.NoError(t, err)
	require.Equal(t, billingtypes.LEASE_STATE_ACTIVE, retainedLease.Lease.GetState())
	require.NotNil(t, retainedLease.Lease.AcknowledgedAt)
	require.NotNil(t, retainedLease.Lease.Reservation)
	require.Equal(t, sdkmath.NewInt(30), retainedLease.Lease.Reservation.RemainingAmounts.AmountOf(Denom))
	retainedAccount, err := helpers.BillingQueryCreditAccount(ctx, chain, providerWallet.FormattedAddress())
	require.NoError(t, err)
	require.Equal(t, uint64(1), retainedAccount.CreditAccount.ActiveLeaseCount)
	require.Zero(t, retainedAccount.CreditAccount.PendingLeaseCount)
	require.Equal(t, sdkmath.NewInt(30), retainedAccount.CreditAccount.ReservedAmounts.AmountOf(Denom))
	retainedDomain, err := helpers.BillingQueryLeaseByCustomDomain(ctx, chain, retainedCustomDomain)
	require.NoError(t, err)
	require.Equal(t, state.retainedPendingUUID, retainedDomain.Lease.UUID)

	// Close the migrated modern ACTIVE lease and prove both the bank balances
	// and consumable reservation move together for every locked-price denom.
	// A terminal transition makes the assertion independent of how much block
	// time elapsed while Docker replaced the binary.
	activeBefore, err := helpers.BillingQueryLease(ctx, chain, state.underbackedActiveUUID)
	require.NoError(t, err)
	require.NotNil(t, activeBefore.Lease.Reservation)
	accountBefore, err := helpers.BillingQueryCreditAccount(ctx, chain, tenantWallet.FormattedAddress())
	require.NoError(t, err)
	secondDenom := state.secondSKU.BasePrice.Denom
	payoutNativeBefore, err := chain.GetBalance(ctx, tenantWallet.FormattedAddress(), Denom)
	require.NoError(t, err)
	payoutSecondBefore, err := chain.GetBalance(ctx, tenantWallet.FormattedAddress(), secondDenom)
	require.NoError(t, err)
	closeRes, err = helpers.BillingCloseLease(ctx, chain, providerWallet, state.underbackedActiveUUID)
	require.NoError(t, err)
	requireSuccessfulUpgradeTestTx(t, chain, closeRes.TxHash, "close migrated modern ACTIVE lease")
	activeAfter, err := helpers.BillingQueryLease(ctx, chain, state.underbackedActiveUUID)
	require.NoError(t, err)
	require.Equal(t, billingtypes.LEASE_STATE_CLOSED, activeAfter.Lease.GetState())
	require.NotNil(t, activeAfter.Lease.Reservation)
	require.True(t, activeAfter.Lease.Reservation.RemainingAmounts.IsZero())
	accountAfter, err := helpers.BillingQueryCreditAccount(ctx, chain, tenantWallet.FormattedAddress())
	require.NoError(t, err)
	require.Zero(t, accountAfter.CreditAccount.ActiveLeaseCount)
	require.Zero(t, accountAfter.CreditAccount.PendingLeaseCount)
	require.True(t, accountAfter.CreditAccount.ReservedAmounts.IsZero())
	payoutNativeAfter, err := chain.GetBalance(ctx, tenantWallet.FormattedAddress(), Denom)
	require.NoError(t, err)
	payoutSecondAfter, err := chain.GetBalance(ctx, tenantWallet.FormattedAddress(), secondDenom)
	require.NoError(t, err)

	for _, denom := range []string{Denom, secondDenom} {
		beforeAllocation := activeBefore.Lease.Reservation.RemainingAmounts.AmountOf(denom)
		afterAllocation := activeAfter.Lease.Reservation.RemainingAmounts.AmountOf(denom)
		require.True(t, beforeAllocation.IsPositive(), "%s lease allocation must start positive", denom)
		require.True(t, afterAllocation.IsZero(), "%s lease allocation must be released", denom)
		beforeAggregate := accountBefore.CreditAccount.ReservedAmounts.AmountOf(denom)
		afterAggregate := accountAfter.CreditAccount.ReservedAmounts.AmountOf(denom)
		require.True(t, beforeAggregate.IsPositive(), "%s aggregate reservation must start positive", denom)
		require.True(t, afterAggregate.IsZero(), "%s aggregate reservation must be released", denom)
	}
	nativeDebit := accountBefore.Balances.AmountOf(Denom).Sub(accountAfter.Balances.AmountOf(Denom))
	secondDebit := accountBefore.Balances.AmountOf(secondDenom).Sub(accountAfter.Balances.AmountOf(secondDenom))
	require.True(t, nativeDebit.IsPositive())
	require.True(t, secondDebit.IsPositive())
	require.Equal(t, nativeDebit, payoutNativeAfter.Sub(payoutNativeBefore))
	require.Equal(t, secondDebit, payoutSecondAfter.Sub(payoutSecondBefore))

	// Finally create and cancel a fresh v4 lease. This covers current writes,
	// indexes, per-lease reservation initialization, and terminal release after
	// the migrated state has already been mutated.
	createRes, err := helpers.BillingCreateLease(ctx, chain, tenantWallet, []string{state.sku.Uuid + ":1"})
	require.NoError(t, err)
	requireSuccessfulUpgradeTestTx(t, chain, createRes.TxHash, "create post-upgrade billing lease")
	newLeaseUUID, err := helpers.GetLeaseIDFromTxHash(ctx, chain, createRes.TxHash)
	require.NoError(t, err)
	newLease, err := helpers.BillingQueryLease(ctx, chain, newLeaseUUID)
	require.NoError(t, err)
	require.Equal(t, billingtypes.LEASE_STATE_PENDING, newLease.Lease.GetState())
	require.NotNil(t, newLease.Lease.Reservation)
	require.Equal(t, sdkmath.NewInt(30), newLease.Lease.Reservation.RemainingAmounts.AmountOf(Denom))
	createdAccount, err := helpers.BillingQueryCreditAccount(ctx, chain, tenantWallet.FormattedAddress())
	require.NoError(t, err)
	require.Zero(t, createdAccount.CreditAccount.ActiveLeaseCount)
	require.Equal(t, uint64(1), createdAccount.CreditAccount.PendingLeaseCount)
	require.Equal(t, sdkmath.NewInt(30), createdAccount.CreditAccount.ReservedAmounts.AmountOf(Denom))
	require.Equal(t, accountAfter.Balances, createdAccount.Balances)
	pendingLeases, err := helpers.BillingQueryLeasesByTenant(
		ctx,
		chain,
		tenantWallet.FormattedAddress(),
		"pending",
	)
	require.NoError(t, err)
	require.Equal(t, []string{newLeaseUUID}, sortedBillingLeaseUUIDs(pendingLeases.Leases))
	cancelRes, err := helpers.BillingCancelLease(ctx, chain, tenantWallet, newLeaseUUID)
	require.NoError(t, err)
	requireSuccessfulUpgradeTestTx(t, chain, cancelRes.TxHash, "cancel post-upgrade billing lease")
	cancelledLease, err := helpers.BillingQueryLease(ctx, chain, newLeaseUUID)
	require.NoError(t, err)
	require.Equal(t, billingtypes.LEASE_STATE_REJECTED, cancelledLease.Lease.GetState())
	require.True(t, cancelledLease.Lease.Reservation.RemainingAmounts.IsZero())
	cancelledAccount, err := helpers.BillingQueryCreditAccount(ctx, chain, tenantWallet.FormattedAddress())
	require.NoError(t, err)
	require.Zero(t, cancelledAccount.CreditAccount.ActiveLeaseCount)
	require.Zero(t, cancelledAccount.CreditAccount.PendingLeaseCount)
	require.True(t, cancelledAccount.CreditAccount.ReservedAmounts.IsZero())
	require.Equal(t, accountAfter.Balances, cancelledAccount.Balances)
	rejectedLeases, err := helpers.BillingQueryLeasesByTenant(
		ctx,
		chain,
		tenantWallet.FormattedAddress(),
		"rejected",
	)
	require.NoError(t, err)
	require.Equal(t, []string{newLeaseUUID}, sortedBillingLeaseUUIDs(rejectedLeases.Leases))
}

func requirePostUpgradeSKUFunctionality(
	ctx context.Context,
	t *testing.T,
	chain *cosmos.CosmosChain,
	cfg *ibc.ChainConfig,
	state upgradeMigrationState,
) {
	t.Helper()
	t.Log("Exercising migrated SKU state after upgrade")

	updatedProvider := createSKUUpdateProviderProposal(
		groupAddr,
		state.provider.Uuid,
		state.provider.Address,
		state.provider.PayoutAddress,
		true,
		[]byte("post-upgrade-provider"),
	)
	updatedProvider.ApiUrl = "https://post-upgrade.example.com"
	updatedSKU := createSKUUpdateSKUProposal(
		groupAddr,
		state.inactiveSKU.Uuid,
		state.provider.Uuid,
		"Reactivated after upgrade",
		skutypes.Unit_UNIT_PER_HOUR,
		sdk.NewInt64Coin(Denom, 10_800),
		true,
		[]byte("post-upgrade-sku"),
	)
	createAndRunProposalSuccess(ctx, t, chain, cfg, accAddr, []*types.Any{
		createAny(t, &updatedProvider),
		createAny(t, &updatedSKU),
	})

	providerRes, err := helpers.SKUQueryProvider(ctx, chain, state.provider.Uuid)
	require.NoError(t, err)
	expectedProvider := state.provider
	expectedProvider.Address = updatedProvider.Address
	expectedProvider.PayoutAddress = updatedProvider.PayoutAddress
	expectedProvider.MetaHash = updatedProvider.MetaHash
	expectedProvider.Active = updatedProvider.Active
	expectedProvider.ApiUrl = updatedProvider.ApiUrl
	require.Equal(t, expectedProvider, providerRes.Provider)
	skuRes, err := helpers.SKUQuerySKU(ctx, chain, state.inactiveSKU.Uuid)
	require.NoError(t, err)
	expectedSKU := state.inactiveSKU
	expectedSKU.ProviderUuid = updatedSKU.ProviderUuid
	expectedSKU.Name = updatedSKU.Name
	expectedSKU.Unit = updatedSKU.Unit
	expectedSKU.BasePrice = updatedSKU.BasePrice
	expectedSKU.Active = updatedSKU.Active
	expectedSKU.MetaHash = updatedSKU.MetaHash
	require.Equal(t, expectedSKU, skuRes.Sku)

	activeProviders, err := helpers.SKUQueryActiveProviders(ctx, chain)
	require.NoError(t, err)
	require.Equal(t, []skutypes.Provider{expectedProvider}, activeProviders.Providers)
	activeSKUs, err := helpers.SKUQueryActiveSKUs(ctx, chain)
	require.NoError(t, err)
	expectedActiveSKUs := sortedSKUs([]skutypes.SKU{state.sku, expectedSKU, state.secondSKU, state.activeSKU})
	require.Equal(t, expectedActiveSKUs, sortedSKUs(activeSKUs.Skus))
	activeSKUsByProvider, err := helpers.SKUQueryActiveSKUsByProvider(ctx, chain, state.provider.Uuid)
	require.NoError(t, err)
	require.Equal(t, expectedActiveSKUs, sortedSKUs(activeSKUsByProvider.Skus))
}

func requireLegacyBillingStorage(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, state upgradeMigrationState) {
	t.Helper()

	rawParams := queryBillingStoreValue(ctx, t, chain, state.paramsKey)
	require.False(t, bytes.HasPrefix(rawParams, []byte(billingParamsStoragePrefix)))
	for _, address := range state.billingParams.AllowedList {
		require.True(t, bytes.Contains(rawParams, []byte(address)))
	}
	for _, cohort := range state.billingCohorts {
		for _, lease := range cohort.leases {
			rawLease := queryBillingStoreValue(ctx, t, chain, lease.key)
			require.False(t, bytes.HasPrefix(rawLease, []byte(billingLeaseStoragePrefix)), cohort.name)
			require.True(t, bytes.Contains(rawLease, []byte(cohort.tenant)), cohort.name)
		}
		rawCreditAccount := queryBillingStoreValue(ctx, t, chain, cohort.creditAccountKey)
		require.False(t, bytes.HasPrefix(rawCreditAccount, []byte(billingCreditAccountStoragePrefix)), cohort.name)
		require.True(t, bytes.Contains(rawCreditAccount, []byte(cohort.tenant)), cohort.name)
		require.True(t, bytes.Contains(rawCreditAccount, []byte(cohort.creditAddress)), cohort.name)
	}
}

func requireMigratedBillingStorage(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, state upgradeMigrationState) {
	t.Helper()

	rawParams := queryBillingStoreValue(ctx, t, chain, state.paramsKey)
	require.True(t, bytes.HasPrefix(rawParams, []byte(billingParamsStoragePrefix)))
	require.Len(t, state.billingAllowedAddresses, len(state.billingParams.AllowedList))
	for index, address := range state.billingAllowedAddresses {
		require.True(t, bytes.Contains(rawParams, address.Bytes()))
		require.False(t, bytes.Contains(rawParams, []byte(state.billingParams.AllowedList[index])))
	}
	for _, cohort := range state.billingCohorts {
		for _, lease := range cohort.leases {
			rawLease := queryBillingStoreValue(ctx, t, chain, lease.key)
			require.True(t, bytes.HasPrefix(rawLease, []byte(billingLeaseStoragePrefix)), cohort.name)
			require.True(t, bytes.Contains(rawLease, cohort.tenantAddress.Bytes()), cohort.name)
			require.False(t, bytes.Contains(rawLease, []byte(cohort.tenant)), cohort.name)
		}
		rawCreditAccount := queryBillingStoreValue(ctx, t, chain, cohort.creditAccountKey)
		require.True(t, bytes.HasPrefix(rawCreditAccount, []byte(billingCreditAccountStoragePrefix)), cohort.name)
		require.True(t, bytes.Contains(rawCreditAccount, cohort.tenantAddress.Bytes()), cohort.name)
		require.True(t, bytes.Contains(rawCreditAccount, cohort.creditAddressRaw.Bytes()), cohort.name)
		require.False(t, bytes.Contains(rawCreditAccount, []byte(cohort.tenant)), cohort.name)
		require.False(t, bytes.Contains(rawCreditAccount, []byte(cohort.creditAddress)), cohort.name)
	}
}

func requireLegacySKUStorage(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, state upgradeMigrationState) {
	t.Helper()

	rawParams := queryModuleStoreValue(ctx, t, chain, skutypes.ModuleName, state.skuParamsKey)
	rawProvider := queryModuleStoreValue(ctx, t, chain, skutypes.ModuleName, state.skuProviderKey)

	require.False(t, bytes.HasPrefix(rawParams, []byte(skuParamsStoragePrefix)))
	require.False(t, bytes.HasPrefix(rawProvider, []byte(skuProviderStoragePrefix)))
	for _, address := range state.skuParams.AllowedList {
		require.True(t, bytes.Contains(rawParams, []byte(address)))
	}
	require.True(t, bytes.Contains(rawProvider, []byte(state.provider.Address)))
	require.True(t, bytes.Contains(rawProvider, []byte(state.provider.PayoutAddress)))
}

func requireMigratedSKUStorage(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, state upgradeMigrationState) {
	t.Helper()

	rawParams := queryModuleStoreValue(ctx, t, chain, skutypes.ModuleName, state.skuParamsKey)
	rawProvider := queryModuleStoreValue(ctx, t, chain, skutypes.ModuleName, state.skuProviderKey)

	require.True(t, bytes.HasPrefix(rawParams, []byte(skuParamsStoragePrefix)))
	require.True(t, bytes.HasPrefix(rawProvider, []byte(skuProviderStoragePrefix)))
	require.True(t, bytes.Contains(rawParams, state.providerAddress.Bytes()))
	require.True(t, bytes.Contains(rawParams, state.payoutAddress.Bytes()))
	require.True(t, bytes.Contains(rawProvider, state.providerAddress.Bytes()))
	require.True(t, bytes.Contains(rawProvider, state.payoutAddress.Bytes()))
	for _, address := range state.skuParams.AllowedList {
		require.False(t, bytes.Contains(rawParams, []byte(address)))
	}
	require.False(t, bytes.Contains(rawProvider, []byte(state.provider.Address)))
	require.False(t, bytes.Contains(rawProvider, []byte(state.provider.PayoutAddress)))
}

func queryBillingStoreValue(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, key []byte) []byte {
	t.Helper()
	return queryModuleStoreValue(ctx, t, chain, billingtypes.ModuleName, key)
}

func queryModuleStoreValue(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, moduleName string, key []byte) []byte {
	t.Helper()

	res, err := chain.GetNode().Client.ABCIQuery(ctx, "/store/"+moduleName+"/key", key)
	require.NoError(t, err)
	require.Equal(t, uint32(0), res.Response.Code, res.Response.Log)
	require.NotEmpty(t, res.Response.Value)
	return bytes.Clone(res.Response.Value)
}

func requireModuleVersion(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, moduleName, expected string) {
	t.Helper()

	stdout, _, err := chain.GetNode().ExecQuery(ctx, "upgrade", "module-versions", moduleName)
	require.NoError(t, err)
	var response struct {
		ModuleVersions []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"module_versions"`
	}
	require.NoError(t, json.Unmarshal(stdout, &response))
	require.Len(t, response.ModuleVersions, 1)
	require.Equal(t, moduleName, response.ModuleVersions[0].Name)
	require.Equal(t, expected, response.ModuleVersions[0].Version)
}

func requireBinaryVersion(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, expected string) {
	t.Helper()

	stdout, stderr, err := chain.Exec(ctx, []string{chain.Config().Bin, "version"}, chain.Config().Env)
	require.NoError(t, err, "query binary version: %s", stderr)
	require.Equal(t, expected, strings.TrimSpace(string(stdout)))
}

func requireLibwasmvmVersion(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, expected string) {
	t.Helper()

	stdout, stderr, err := chain.Exec(
		ctx,
		[]string{chain.Config().Bin, "query", "wasm", "libwasmvm-version"},
		chain.Config().Env,
	)
	require.NoError(t, err, "query libwasmvm version: %s", stderr)
	require.Equal(t, expected, strings.TrimSpace(string(stdout)))
}

func requireSuccessfulUpgradeTestTx(t *testing.T, chain *cosmos.CosmosChain, txHash, operation string) {
	t.Helper()

	txRes, err := chain.GetTransaction(txHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "%s failed: %s", operation, txRes.RawLog)
}

func StoreAndInstantiateContract(ctx context.Context, t *testing.T, chain *cosmos.CosmosChain, accAddr string) string {
	t.Helper()

	// Get the current chain config
	chainConfig := chain.Config()

	// Store contract
	wasmFile := "../scripts/cw_template.wasm"
	wasmStoreProposal := createWasmStoreProposal(groupAddr, wasmFile)
	createAndRunProposalSuccess(ctx, t, chain, &chainConfig, accAddr, []*types.Any{createAny(t, &wasmStoreProposal)})

	// Query the code ID
	codeID := queryLatestCodeID(ctx, t, chain)
	require.NotZero(t, codeID)

	// Instantiate the contract
	initMsg := map[string]interface{}{
		"count": 0,
	}
	initMsgBz, err := json.Marshal(initMsg)
	require.NoError(t, err)

	wasmInstantiateProposal := createWasmInstantiateProposal(groupAddr, codeID, string(initMsgBz))
	createAndRunProposalSuccess(ctx, t, chain, &chainConfig, accAddr, []*types.Any{createAny(t, &wasmInstantiateProposal)})

	// Query the contract address
	contractAddr := queryLatestContractAddress(ctx, t, chain, codeID)
	require.NotEmpty(t, contractAddr)

	// Query contract state to verify instantiation.
	requireContractCount(ctx, t, chain, contractAddr, 0)

	return contractAddr
}

func incrementContractAndRequireCount(
	ctx context.Context,
	t *testing.T,
	chain *cosmos.CosmosChain,
	keyName string,
	contractAddr string,
	expected int,
) {
	t.Helper()

	_, err := chain.ExecuteContract(ctx, keyName, contractAddr, `{"increment":{}}`)
	require.NoError(t, err)
	requireContractCount(ctx, t, chain, contractAddr, expected)
}

func requireContractCount(
	ctx context.Context,
	t *testing.T,
	chain *cosmos.CosmosChain,
	contractAddr string,
	expected int,
) {
	t.Helper()

	var response struct {
		Count int `json:"count"`
	}
	require.NoError(t, chain.QueryContract(ctx, contractAddr, `{"get_count":{}}`, &response))
	require.Equal(t, expected, response.Count)
}
