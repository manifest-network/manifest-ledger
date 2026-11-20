package interchaintest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	dockerclient "github.com/moby/moby/client"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/dockerutil"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/stretchr/testify/require"

	rpchttp "github.com/cometbft/cometbft/rpc/client/http"

	sdkmath "cosmossdk.io/math"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/interchaintest/helpers"
	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
)

const inPlaceTestnetFixtureVersion = "eng879-test-upgrade"

// inPlaceTestnetReleasedVersion is the published release that produces the
// released-source state, and that state's last completed upgrade. The conversion
// base image (`make local-image-in-place-base`) must report the same version.
const inPlaceTestnetReleasedVersion = "v2.4.0"

// x/billing's consensus version in the released source and in the target. The
// upgrade runs the real migration between them (per-lease settlement to
// settlement streams); every other module keeps the version the release has.
const (
	inPlaceTestnetSourceBillingVersion = uint64(2)
	inPlaceTestnetTargetBillingVersion = uint64(3)
)

// The source is synthetic state produced by a released image. The target
// fixture changes one billing parameter after RunMigrations; normal builds never
// include this handler. That parameter proves the handler ran exactly once; the
// billing state seeded by the release proves RunMigrations converted it.
type inPlaceTestnetReleasedState struct {
	codeID, contract                                      string
	codeInfo, contractInfo, contractState, moduleVersions []byte
	billingParams                                         map[string]any
	maxLeases                                             uint64
	completedName                                         string
	completedHeight                                       int64
	pendingPlan                                           []byte
	billing                                               *inPlaceTestnetBilling
}

// inPlaceTestnetBilling is billing state written by the released binary, which
// settles per lease: a funded tenant holding ACTIVE leases with one provider,
// each lease carrying its own last_settled_at. The target's migration must turn
// them into one settlement stream without losing or double-paying any accrual.
type inPlaceTestnetBilling struct {
	provider, tenant ibc.Wallet
	providerUUID     string
	denom            string
	leases           []inPlaceTestnetLease
	// Balances once the leases are ACTIVE. Settlement is lazy, so nothing moves
	// between the two until the migration settles the leases.
	providerBalance, creditBalance sdkmath.Int
}

type inPlaceTestnetLease struct {
	uuid string
	// rate is what the lease accrues per second.
	rate int64
	// settledAt is the lease's last_settled_at as the released binary stored it.
	settledAt time.Time
	// accruesFrom is where the lease's unsettled accrual starts: its last
	// settlement, or its acknowledgement if that is later.
	accruesFrom time.Time
}

// inPlaceTestnetLeaseView is what this test follows of a lease. The pointers
// tell a lease that carries last_settled_at from one that does not: the CLI
// prints an unset timestamp as null.
type inPlaceTestnetLeaseView struct {
	State          string     `json:"state"`
	AcknowledgedAt *time.Time `json:"acknowledged_at"`
	LastSettledAt  *time.Time `json:"last_settled_at"`
}

func queryInPlaceTestnetLease(t *testing.T, ctx context.Context, node *cosmos.ChainNode, uuid string) inPlaceTestnetLeaseView {
	t.Helper()
	var response struct {
		Lease inPlaceTestnetLeaseView `json:"lease"`
	}
	require.NoError(t, json.Unmarshal(queryInPlaceTestnet(t, ctx, node, "billing", "lease", uuid), &response))
	return response.Lease
}

// seedInPlaceTestnetBilling runs on the released source, so it only uses
// commands that release has: per-lease settlement, no stream queries.
func seedInPlaceTestnetBilling(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, authority ibc.Wallet) *inPlaceTestnetBilling {
	t.Helper()
	node := chain.GetNode()
	committed := func(action string, response sdk.TxResponse, err error) string {
		t.Helper()
		require.NoError(t, err, action)
		result, err := chain.GetTransaction(response.TxHash)
		require.NoError(t, err, action)
		require.Zero(t, result.Code, "%s: %s", action, result.RawLog)
		return response.TxHash
	}

	// Dedicated accounts: the test separately asserts the balance of its
	// preserved user, which billing payouts must not disturb.
	users := interchaintest.GetAndFundTestUsers(t, ctx, "billing", DefaultGenesisAmt, chain, chain)
	billing := &inPlaceTestnetBilling{provider: users[0], tenant: users[1], denom: chain.Config().Denom}

	response, txErr := helpers.SKUCreateProvider(ctx, chain, authority, billing.provider.FormattedAddress(), billing.provider.FormattedAddress(), "")
	providerUUID, err := helpers.GetProviderUUIDFromTxHash(ctx, chain, committed("create provider", response, txErr))
	require.NoError(t, err)
	billing.providerUUID = providerUUID
	// 3600 per hour is 1 per second for each unit leased.
	response, txErr = helpers.SKUCreateSKU(ctx, chain, authority, billing.providerUUID, "in-place", 1, "3600"+billing.denom, "")
	skuUUID, err := helpers.GetSKUUUIDFromTxHash(ctx, chain, committed("create SKU", response, txErr))
	require.NoError(t, err)
	response, txErr = helpers.BillingFundCredit(ctx, chain, billing.tenant, billing.tenant.FormattedAddress(), "1000000"+billing.denom)
	committed("fund credit", response, txErr)

	// Two leases of different rates, so the stream has to aggregate them.
	var uuids []string
	for _, quantity := range []int64{1, 2} {
		response, txErr = helpers.BillingCreateLease(ctx, chain, billing.tenant, []string{fmt.Sprintf("%s:%d", skuUUID, quantity)})
		uuid, err := helpers.GetLeaseIDFromTxHash(ctx, chain, committed("create lease", response, txErr))
		require.NoError(t, err)
		uuids = append(uuids, uuid)
		billing.leases = append(billing.leases, inPlaceTestnetLease{uuid: uuid, rate: quantity})
	}
	response, txErr = helpers.BillingAcknowledgeLeases(ctx, chain, billing.provider, uuids)
	committed("acknowledge leases", response, txErr)

	for i := range billing.leases {
		lease := &billing.leases[i]
		view := queryInPlaceTestnetLease(t, ctx, node, lease.uuid)
		require.Equal(t, "LEASE_STATE_ACTIVE", view.State)
		require.NotNil(t, view.AcknowledgedAt)
		require.NotNil(t, view.LastSettledAt, "the released binary settles per lease, so every lease carries last_settled_at")
		lease.settledAt, lease.accruesFrom = *view.LastSettledAt, *view.LastSettledAt
		if view.AcknowledgedAt.After(lease.accruesFrom) {
			lease.accruesFrom = *view.AcknowledgedAt
		}
	}
	billing.providerBalance = inPlaceTestnetBalance(t, ctx, node, billing.provider.FormattedAddress(), billing.denom)
	billing.creditBalance = billing.credit(t, ctx, node).Balances.AmountOf(billing.denom)
	require.True(t, billing.creditBalance.IsPositive())
	return billing
}

func (billing *inPlaceTestnetBilling) credit(t *testing.T, ctx context.Context, node *cosmos.ChainNode) billingtypes.QueryCreditAccountResponse {
	t.Helper()
	var response billingtypes.QueryCreditAccountResponse
	require.NoError(t, json.Unmarshal(queryInPlaceTestnet(t, ctx, node, "billing", "credit-account", billing.tenant.FormattedAddress()), &response))
	return response
}

// rate is the summed per-second rate of the seeded leases.
func (billing *inPlaceTestnetBilling) rate() int64 {
	var rate int64
	for _, lease := range billing.leases {
		rate += lease.rate
	}
	return rate
}

// assertUnmigrated requires the leases to be exactly as the release left them.
// It holds on the source and on the fork until the upgrade: the conversion
// binary must not settle or rewrite billing state it has not migrated.
func (billing *inPlaceTestnetBilling) assertUnmigrated(t *testing.T, ctx context.Context, node *cosmos.ChainNode) {
	t.Helper()
	for _, lease := range billing.leases {
		view := queryInPlaceTestnetLease(t, ctx, node, lease.uuid)
		require.Equal(t, "LEASE_STATE_ACTIVE", view.State)
		require.NotNil(t, view.LastSettledAt, "lease %s lost last_settled_at before the migration", lease.uuid)
		require.True(t, lease.settledAt.Equal(*view.LastSettledAt), "lease %s was settled before the migration", lease.uuid)
	}
	require.Equal(t, billing.providerBalance.String(),
		inPlaceTestnetBalance(t, ctx, node, billing.provider.FormattedAddress(), billing.denom).String(), "provider was paid before the migration")
	require.Equal(t, billing.creditBalance.String(),
		billing.credit(t, ctx, node).Balances.AmountOf(billing.denom).String(), "tenant was charged before the migration")
}

// stream requires the provider to have exactly one stream, the seeded
// tenant's, and returns it. The same stream must be listed for the tenant.
func (billing *inPlaceTestnetBilling) stream(t *testing.T, ctx context.Context, node *cosmos.ChainNode) helpers.StreamStateJSON {
	t.Helper()
	var byProvider, byTenant helpers.StreamsResponseJSON
	require.NoError(t, json.Unmarshal(queryInPlaceTestnet(t, ctx, node, "billing", "streams-by-provider", billing.providerUUID), &byProvider))
	require.Len(t, byProvider.Streams, 1, "one stream per (provider, tenant, denom), whatever the number of leases")
	stream := byProvider.Find(billing.providerUUID, billing.tenant.FormattedAddress(), billing.denom)
	require.NotNil(t, stream, "unexpected stream %+v", byProvider.Streams[0])

	require.NoError(t, json.Unmarshal(queryInPlaceTestnet(t, ctx, node, "billing", "streams-by-tenant", billing.tenant.FormattedAddress()), &byTenant))
	require.Len(t, byTenant.Streams, 1)
	listed := byTenant.Find(billing.providerUUID, billing.tenant.FormattedAddress(), billing.denom)
	require.NotNil(t, listed, "streams-by-tenant does not list the provider's stream")
	require.True(t, stream.State.LastSettledAt.Equal(listed.State.LastSettledAt))
	require.Equal(t, stream.State.Rate.String(), listed.State.Rate.String())
	return stream.State
}

// assertMigrated requires the migration, which ran at the upgrade block's time,
// to have settled every lease up to that time and replaced per-lease settlement
// by one stream. It must run before anything settles the stream again.
func (billing *inPlaceTestnetBilling) assertMigrated(t *testing.T, ctx context.Context, node *cosmos.ChainNode, upgradeTime time.Time) {
	t.Helper()

	// What the leases accrued under per-lease settlement and were never paid.
	// The migration settles each lease the way the release would have: for the
	// whole seconds since its own last settlement. Exact, sub-second accrual
	// only starts with the stream it creates.
	var owed int64
	for _, lease := range billing.leases {
		seconds := int64(upgradeTime.Sub(lease.accruesFrom) / time.Second)
		require.Positive(t, seconds, "lease %s should have accrued before the upgrade", lease.uuid)
		owed += lease.rate * seconds
	}
	paid := inPlaceTestnetBalance(t, ctx, node, billing.provider.FormattedAddress(), billing.denom).Sub(billing.providerBalance)
	require.Equal(t, sdkmath.NewInt(owed).String(), paid.String(), "migration should pay the provider what the leases accrued up to the upgrade")
	credit := billing.credit(t, ctx, node)
	require.Equal(t, billing.creditBalance.SubRaw(owed).String(), credit.Balances.AmountOf(billing.denom).String(),
		"migration should charge the tenant exactly what it paid the provider")
	require.Equal(t, uint64(len(billing.leases)), credit.CreditAccount.ActiveLeaseCount)

	stream := billing.stream(t, ctx, node)
	require.Equal(t, sdkmath.NewInt(billing.rate()).String(), stream.Rate.String(), "stream rate should be the sum of the lease rates")
	require.Equal(t, uint64(len(billing.leases)), stream.LeaseCount)
	require.True(t, upgradeTime.Equal(stream.LastSettledAt), "stream settled at %s, upgrade block time %s", stream.LastSettledAt, upgradeTime)
	require.False(t, stream.Remainder.IsNil(), "the migrated stream has no remainder field")
	require.True(t, stream.Remainder.IsZero(), "a migrated stream starts without a remainder, got %s", stream.Remainder)
	require.False(t, stream.IsExhausted(), "the tenant's credit covers the accrual")

	for _, lease := range billing.leases {
		view := queryInPlaceTestnetLease(t, ctx, node, lease.uuid)
		require.Equal(t, "LEASE_STATE_ACTIVE", view.State, "lease %s", lease.uuid)
		require.Nil(t, view.LastSettledAt, "lease %s still carries the deprecated last_settled_at", lease.uuid)
	}
}

// assertWithdraw requires a provider withdrawal on the migrated state to settle
// the stream at the time of its block and to pay its exact accrual since the
// migration, to the nanosecond:
//
//	total = remainder + rate × (block time − last_settled_at, in nanoseconds)
//
// of which total / 1e9 is paid and total % 1e9 stays as the stream's remainder.
// Nothing more is paid: the accrual before the upgrade was settled by the migration.
func (billing *inPlaceTestnetBilling) assertWithdraw(t *testing.T, ctx context.Context, node *cosmos.ChainNode, rpc *rpchttp.HTTP, chainID string) {
	t.Helper()
	before := billing.stream(t, ctx, node)
	balance := inPlaceTestnetBalance(t, ctx, node, billing.provider.FormattedAddress(), billing.denom)

	response := broadcastInPlaceTestnetMessage(t, ctx, node, billing.provider.KeyName(), chainID, &billingtypes.MsgWithdraw{
		Sender: billing.provider.FormattedAddress(), ProviderUuid: billing.providerUUID,
	})
	require.Zero(t, response.Code, "%s", response.RawLog)
	status, err := rpc.Status(ctx)
	require.NoError(t, err)
	waitForInPlaceTestnetHeight(t, ctx, rpc, chainID, status.SyncInfo.LatestBlockHeight+3)
	require.NoError(t, json.Unmarshal(queryInPlaceTestnet(t, ctx, node, "tx", response.TxHash), &response))
	require.Zero(t, response.Code, "%s", response.RawLog)

	withdrawBlock, err := rpc.Block(ctx, &response.Height)
	require.NoError(t, err)
	withdrawTime := withdrawBlock.Block.Time
	require.True(t, withdrawTime.After(before.LastSettledAt), "the stream accrued for several blocks since the upgrade")

	after := billing.stream(t, ctx, node)
	require.True(t, withdrawTime.Equal(after.LastSettledAt),
		"stream settled at %s, the withdrawal's block %d has time %s", after.LastSettledAt, response.Height, withdrawTime)
	expected, remainder := before.AccrualAt(withdrawTime)
	require.True(t, expected.IsPositive(), "rate %s over %s should pay something", before.Rate, withdrawTime.Sub(before.LastSettledAt))
	paid := inPlaceTestnetBalance(t, ctx, node, billing.provider.FormattedAddress(), billing.denom).Sub(balance)
	require.Equal(t, expected.String(), paid.String(),
		"withdrawal should pay the exact accrual of rate %s over %s", before.Rate, withdrawTime.Sub(before.LastSettledAt))
	require.Equal(t, remainder.String(), after.Remainder.String(), "what did not amount to a whole unit should be carried as the remainder")

	require.Equal(t, before.Rate.String(), after.Rate.String())
	require.Equal(t, before.LeaseCount, after.LeaseCount)
	require.False(t, after.IsExhausted())
	for _, lease := range billing.leases {
		require.Equal(t, "LEASE_STATE_ACTIVE", queryInPlaceTestnetLease(t, ctx, node, lease.uuid).State, "lease %s", lease.uuid)
	}
}

// decodeInPlaceTestnetModuleVersions indexes `query upgrade module-versions` by module name.
func decodeInPlaceTestnetModuleVersions(t *testing.T, data []byte) map[string]uint64 {
	t.Helper()
	var response struct {
		ModuleVersions []struct {
			Name    string `json:"name"`
			Version uint64 `json:"version,string"`
		} `json:"module_versions"`
	}
	require.NoError(t, json.Unmarshal(data, &response), "%s", data)
	require.NotEmpty(t, response.ModuleVersions)
	versions := make(map[string]uint64, len(response.ModuleVersions))
	for _, module := range response.ModuleVersions {
		require.NotContains(t, versions, module.Name, "module listed twice")
		versions[module.Name] = module.Version
	}
	return versions
}

func seedInPlaceTestnetReleasedState(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet) *inPlaceTestnetReleasedState {
	t.Helper()
	node := chain.GetNode()
	version, stderr, err := node.ExecBin(ctx, "version")
	require.NoError(t, err, "%s", stderr)
	require.Equal(t, inPlaceTestnetReleasedVersion, strings.TrimSpace(string(version)))
	contractCode, err := os.ReadFile("../scripts/cw_template.wasm")
	require.NoError(t, err)
	const contractFile = "cw_template.wasm"
	require.NoError(t, helpers.WriteLiveNodeFile(ctx, node, contractFile, contractCode))
	_, err = node.ExecTx(ctx, user.KeyName(), "wasm", "store", path.Join(node.HomeDir(), contractFile), "--gas", "auto")
	require.NoError(t, err)
	require.NoError(t, testutil.WaitForBlocks(ctx, 5, chain))
	var codes cosmos.CodeInfosResponse
	require.NoError(t, json.Unmarshal(queryInPlaceTestnet(t, ctx, node, "wasm", "list-code", "--reverse"), &codes))
	require.Len(t, codes.CodeInfos, 1)
	codeID := codes.CodeInfos[0].CodeID
	contract, err := node.InstantiateContract(ctx, user.KeyName(), codeID, `{"count":41}`, true)
	require.NoError(t, err)
	execution, err := node.ExecuteContract(ctx, user.KeyName(), contract, `{"increment":{}}`)
	require.NoError(t, err)
	require.Zero(t, execution.Code, "%s", execution.RawLog)
	authority, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "source-authority", accMnemonic, DefaultGenesisAmt, chain)
	require.NoError(t, err)
	require.Equal(t, accAddr, authority.FormattedAddress())
	billing := seedInPlaceTestnetBilling(t, ctx, chain, authority)
	rpc, err := rpchttp.NewWithClient(chain.GetHostRPCAddress(), "/websocket", &http.Client{Timeout: 5 * time.Second})
	require.NoError(t, err)
	completedHeight := completeInPlaceTestnetSourceUpgrade(t, ctx, node, authority, chain.Config().ChainID, rpc)
	// Keep a genuinely pending source plan across conversion. It is rescheduled
	// near the fork tip only after ordinary restart and authority checks succeed.
	height, err := chain.Height(ctx)
	require.NoError(t, err)
	scheduleInPlaceTestnetUpgrade(t, ctx, node, authority, chain.Config().ChainID, inPlaceTestnetFixtureVersion, height+1000, rpc)
	state := &inPlaceTestnetReleasedState{
		codeID: codeID, contract: contract, completedName: inPlaceTestnetReleasedVersion, completedHeight: completedHeight, billing: billing,
		codeInfo:       queryInPlaceTestnet(t, ctx, node, "wasm", "code-info", codeID),
		contractInfo:   queryInPlaceTestnet(t, ctx, node, "wasm", "contract", contract),
		contractState:  queryInPlaceTestnet(t, ctx, node, "wasm", "contract-state", "all", contract),
		moduleVersions: queryInPlaceTestnet(t, ctx, node, "upgrade", "module-versions"),
		pendingPlan:    queryInPlaceTestnet(t, ctx, node, "upgrade", "plan"),
	}
	var params struct {
		Params map[string]any `json:"params"`
	}
	require.NoError(t, json.Unmarshal(queryInPlaceTestnet(t, ctx, node, "billing", "params"), &params))
	state.billingParams = params.Params
	maxLeases, ok := params.Params["max_leases_per_tenant"].(string)
	require.True(t, ok)
	state.maxLeases, err = strconv.ParseUint(maxLeases, 10, 64)
	require.NoError(t, err)
	require.Equal(t, inPlaceTestnetSourceBillingVersion, decodeInPlaceTestnetModuleVersions(t, state.moduleVersions)[billingtypes.ModuleName],
		"the released source no longer has the billing consensus version this test migrates from")
	state.assertPreserved(t, ctx, node, false)
	image := chain.Config().Images[0]
	t.Logf("Released source image %s:%s, synthetic two-validator state: contract %s count=42, %d active leases of provider %s, completed upgrade at %d, pending target %s", image.Repository, image.Version, contract, len(billing.leases), billing.providerUUID, completedHeight, inPlaceTestnetFixtureVersion)
	return state
}

func (state *inPlaceTestnetReleasedState) assertPreserved(t *testing.T, ctx context.Context, node *cosmos.ChainNode, migrated bool) {
	t.Helper()
	require.JSONEq(t, string(state.codeInfo), string(queryInPlaceTestnet(t, ctx, node, "wasm", "code-info", state.codeID)))
	require.JSONEq(t, string(state.contractInfo), string(queryInPlaceTestnet(t, ctx, node, "wasm", "contract", state.contract)))
	require.JSONEq(t, string(state.contractState), string(queryInPlaceTestnet(t, ctx, node, "wasm", "contract-state", "all", state.contract)))
	var count GetCountResponse
	require.NoError(t, json.Unmarshal(queryInPlaceTestnet(t, ctx, node, "wasm", "contract-state", "smart", state.contract, `{"get_count":{}}`), &count))
	require.Equal(t, 42, count.Data.Count)
	moduleVersions := queryInPlaceTestnet(t, ctx, node, "upgrade", "module-versions")
	if migrated {
		// The upgrade migrates billing and nothing else: every other module
		// keeps the consensus version of the release.
		expectedVersions := decodeInPlaceTestnetModuleVersions(t, state.moduleVersions)
		require.Equal(t, inPlaceTestnetSourceBillingVersion, expectedVersions[billingtypes.ModuleName])
		expectedVersions[billingtypes.ModuleName] = inPlaceTestnetTargetBillingVersion
		require.Equal(t, expectedVersions, decodeInPlaceTestnetModuleVersions(t, moduleVersions))
	} else {
		require.JSONEq(t, string(state.moduleVersions), string(moduleVersions))
		state.billing.assertUnmigrated(t, ctx, node)
	}
	require.Equal(t, state.completedHeight, inPlaceTestnetAppliedHeight(t, ctx, node, state.completedName))
	var params struct {
		Params map[string]any `json:"params"`
	}
	require.NoError(t, json.Unmarshal(queryInPlaceTestnet(t, ctx, node, "billing", "params"), &params))
	expected := make(map[string]any, len(state.billingParams))
	for key, value := range state.billingParams {
		expected[key] = value
	}
	if migrated {
		expected["max_leases_per_tenant"] = strconv.FormatUint(state.maxLeases+1, 10)
	}
	require.Equal(t, expected, params.Params)
	if !migrated {
		require.JSONEq(t, string(state.pendingPlan), string(queryInPlaceTestnet(t, ctx, node, "upgrade", "plan")))
	}
}

// A binary must not contain a handler for a plan that is still in the future
// at PreBlock. With stored tip H, a plan for H+2 can first be included at H+1;
// its next PreBlock is then due. If CLI latency makes it stale, the keeper rejects
// it without installing a plan. Retry only that explicit rejection.
func completeInPlaceTestnetSourceUpgrade(t *testing.T, ctx context.Context, node *cosmos.ChainNode, authority ibc.Wallet, chainID string, rpc *rpchttp.HTTP) int64 {
	t.Helper()
	for attempt := 0; attempt < 5; attempt++ {
		status, err := rpc.Status(ctx)
		require.NoError(t, err)
		response := broadcastInPlaceTestnetMessage(t, ctx, node, authority.KeyName(), chainID, &upgradetypes.MsgSoftwareUpgrade{
			Authority: authority.FormattedAddress(), Plan: upgradetypes.Plan{Name: inPlaceTestnetReleasedVersion, Height: status.SyncInfo.LatestBlockHeight + 2},
		})
		if response.Code == 0 {
			status, err = rpc.Status(ctx)
			require.NoError(t, err)
			waitForInPlaceTestnetHeight(t, ctx, rpc, chainID, status.SyncInfo.LatestBlockHeight+3)
			require.NoError(t, json.Unmarshal(queryInPlaceTestnet(t, ctx, node, "tx", response.TxHash), &response))
		}
		if response.Code != 0 {
			require.Contains(t, response.RawLog, "upgrade cannot be scheduled in the past")
			continue
		}
		applied := inPlaceTestnetAppliedHeight(t, ctx, node, inPlaceTestnetReleasedVersion)
		require.Equal(t, response.Height+1, applied)
		return applied
	}
	require.FailNow(t, "could not commit source upgrade plan before it became stale")
	return 0
}

func scheduleInPlaceTestnetUpgrade(t *testing.T, ctx context.Context, node *cosmos.ChainNode, authority ibc.Wallet, chainID, name string, height int64, rpc *rpchttp.HTTP) {
	t.Helper()
	response := broadcastInPlaceTestnetMessage(t, ctx, node, authority.KeyName(), chainID, &upgradetypes.MsgSoftwareUpgrade{
		Authority: authority.FormattedAddress(), Plan: upgradetypes.Plan{Name: name, Height: height},
	})
	require.Zero(t, response.Code, "%s", response.RawLog)
	status, err := rpc.Status(ctx)
	require.NoError(t, err)
	waitForInPlaceTestnetHeight(t, ctx, rpc, chainID, status.SyncInfo.LatestBlockHeight+2)
	require.NoError(t, json.Unmarshal(queryInPlaceTestnet(t, ctx, node, "tx", response.TxHash), &response))
	require.Zero(t, response.Code, "%s", response.RawLog)
	var plan struct {
		Plan struct {
			Name   string `json:"name"`
			Height int64  `json:"height,string"`
		} `json:"plan"`
	}
	require.NoError(t, json.Unmarshal(queryInPlaceTestnet(t, ctx, node, "upgrade", "plan"), &plan))
	require.Equal(t, name, plan.Plan.Name)
	require.Equal(t, height, plan.Plan.Height)
}

func inPlaceTestnetAppliedHeight(t *testing.T, ctx context.Context, node *cosmos.ChainNode, name string) int64 {
	t.Helper()
	var applied struct {
		Height int64 `json:"height,string"`
	}
	require.NoError(t, json.Unmarshal(queryInPlaceTestnet(t, ctx, node, "upgrade", "applied", name), &applied))
	return applied.Height
}

func runInPlaceTestnetReleasedUpgrade(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, client *dockerclient.Client, operator ibc.Wallet, chainID string, state *inPlaceTestnetReleasedState, assertForkState func(*rpchttp.HTTP)) {
	t.Helper()
	node := chain.GetNode()
	rpc, err := rpchttp.NewWithClient(chain.GetHostRPCAddress(), "/websocket", &http.Client{Timeout: 5 * time.Second})
	require.NoError(t, err)
	status, err := rpc.Status(ctx)
	require.NoError(t, err)
	upgradeHeight := status.SyncInfo.LatestBlockHeight + 15
	scheduleInPlaceTestnetUpgrade(t, ctx, node, operator, chainID, inPlaceTestnetFixtureVersion, upgradeHeight, rpc)
	waitForInPlaceTestnetUpgradeHalt(t, ctx, client, node, rpc, upgradeHeight)
	require.NoError(t, node.StopContainer(ctx))
	dockerutil.CopyCoverageFromContainer(ctx, t, client, node.ContainerID(), node.HomeDir(), ExternalGoCoverDir)
	require.NoError(t, node.RemoveContainer(ctx))
	image := inPlaceTestnetImage(t, "IN_PLACE_TESTNET_UPGRADE_IMAGE", "manifest-testnet-upgrade:local")
	chain.UpgradeVersion(ctx, client, image.Repository, image.Version)
	require.NoError(t, node.CreateNodeContainer(ctx))
	require.NoError(t, node.StartContainer(ctx))
	rpc, err = rpchttp.NewWithClient(chain.GetHostRPCAddress(), "/websocket", &http.Client{Timeout: 5 * time.Second})
	require.NoError(t, err)
	height := waitForInPlaceTestnetHeight(t, ctx, rpc, chainID, upgradeHeight+5)
	version, stderr, err := node.ExecBin(ctx, "version")
	require.NoError(t, err, "%s", stderr)
	require.Equal(t, inPlaceTestnetFixtureVersion, strings.TrimSpace(string(version)))
	require.Equal(t, upgradeHeight, inPlaceTestnetAppliedHeight(t, ctx, node, inPlaceTestnetFixtureVersion))
	state.assertPreserved(t, ctx, node, true)
	// The migration ran in the upgrade block, at that block's time.
	upgradeBlock, err := rpc.Block(ctx, &upgradeHeight)
	require.NoError(t, err)
	upgradeTime := upgradeBlock.Block.Time
	state.billing.assertMigrated(t, ctx, node, upgradeTime)
	assertForkState(rpc)
	require.NoError(t, node.StopContainer(ctx))
	dockerutil.CopyCoverageFromContainer(ctx, t, client, node.ContainerID(), node.HomeDir(), ExternalGoCoverDir)
	require.NoError(t, node.RemoveContainer(ctx))
	require.NoError(t, node.CreateNodeContainer(ctx))
	require.NoError(t, node.StartContainer(ctx))
	rpc, err = rpchttp.NewWithClient(chain.GetHostRPCAddress(), "/websocket", &http.Client{Timeout: 5 * time.Second})
	require.NoError(t, err)
	restartInitialHeight := waitForInPlaceTestnetAppHeight(t, ctx, rpc, chainID, height)
	restartConfirmedHeight := waitForInPlaceTestnetAppHeight(t, ctx, rpc, chainID, restartInitialHeight+5)
	require.Equal(t, upgradeHeight, inPlaceTestnetAppliedHeight(t, ctx, node, inPlaceTestnetFixtureVersion))
	state.assertPreserved(t, ctx, node, true)
	// The migrated billing state survives the restart untouched, and only then
	// is the stream settled for the first time since the migration.
	state.billing.assertMigrated(t, ctx, node, upgradeTime)
	assertForkState(rpc)
	state.billing.assertWithdraw(t, ctx, node, rpc, chainID)
	assertForkState(rpc)
	t.Logf("Scheduled upgrade halted at %d; fixture image %s:%s migrated billing %d -> %d (%d leases into one stream) and applied the +1 billing parameter exactly once; restarted application committed from %d to %d", upgradeHeight, image.Repository, image.Version, inPlaceTestnetSourceBillingVersion, inPlaceTestnetTargetBillingVersion, len(state.billing.leases), restartInitialHeight, restartConfirmedHeight)
}

// Status can report a stored block before the application commits it. Use
// ABCIInfo for progress after restart and Status only to verify chain identity.
func waitForInPlaceTestnetAppHeight(t *testing.T, ctx context.Context, rpc *rpchttp.HTTP, chainID string, target int64) int64 {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	var height int64
	require.Eventually(t, func() bool {
		status, err := rpc.Status(waitCtx)
		if err != nil || status.NodeInfo.Network != chainID {
			return false
		}
		info, err := rpc.ABCIInfo(waitCtx)
		if err != nil {
			return false
		}
		height = info.Response.LastBlockHeight
		return height >= target
	}, 90*time.Second, time.Second, "fork application did not commit through height %d", target)
	return height
}

func waitForInPlaceTestnetUpgradeHalt(t *testing.T, ctx context.Context, client *dockerclient.Client, node *cosmos.ChainNode, rpc *rpchttp.HTTP, height int64) {
	t.Helper()
	haltCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	expectedLog := fmt.Sprintf("UPGRADE %q NEEDED at height: %d", inPlaceTestnetFixtureVersion, height)
	require.Eventually(t, func() bool {
		logs, err := client.ContainerLogs(haltCtx, node.ContainerID(), container.LogsOptions{ShowStdout: true, ShowStderr: true, Tail: "100"})
		if err != nil {
			return false
		}
		content, err := io.ReadAll(logs)
		_ = logs.Close()
		return err == nil && strings.Contains(string(content), expectedLog)
	}, 80*time.Second, time.Second, "source-compatible binary did not halt for the scheduled target upgrade")
	data, err := node.ReadFile(ctx, "data/upgrade-info.json")
	require.NoError(t, err)
	var info struct {
		Name   string `json:"name"`
		Height int64  `json:"height"`
	}
	require.NoError(t, json.Unmarshal(data, &info))
	require.Equal(t, inPlaceTestnetFixtureVersion, info.Name)
	require.Equal(t, height, info.Height)
	// Depending on shutdown timing RPC is either unavailable or still serves the
	// uncommitted upgrade block. In either case it must never commit past the halt.
	if response, err := rpc.ABCIInfo(ctx); err == nil {
		require.Equal(t, height-1, response.Response.LastBlockHeight)
	}
}
