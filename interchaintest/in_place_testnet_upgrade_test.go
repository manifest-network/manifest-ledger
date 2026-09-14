package interchaintest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	upgradetypes "cosmossdk.io/x/upgrade/types"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	"github.com/docker/docker/api/types/container"
	dockerclient "github.com/moby/moby/client"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/dockerutil"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/stretchr/testify/require"
)

const inPlaceTestnetFixtureVersion = "eng879-test-upgrade"

// The source is synthetic state produced by the released v2.3.1 image. The target
// fixture changes one billing parameter after RunMigrations; normal builds never
// include this handler. This proves execution without inventing a production
// schema migration for a release that already has the current module versions.
type inPlaceTestnetReleasedState struct {
	codeID, contract                                      string
	codeInfo, contractInfo, contractState, moduleVersions []byte
	billingParams                                         map[string]any
	maxLeases                                             uint64
	completedName                                         string
	completedHeight                                       int64
	pendingPlan                                           []byte
}

func seedInPlaceTestnetReleasedState(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet) *inPlaceTestnetReleasedState {
	t.Helper()
	node := chain.GetNode()
	version, stderr, err := node.ExecBin(ctx, "version")
	require.NoError(t, err, "%s", stderr)
	require.Equal(t, "v2.3.1", strings.TrimSpace(string(version)))
	codeID, err := node.StoreContract(ctx, user.KeyName(), "../scripts/cw_template.wasm")
	require.NoError(t, err)
	contract, err := node.InstantiateContract(ctx, user.KeyName(), codeID, `{"count":41}`, true)
	require.NoError(t, err)
	execution, err := node.ExecuteContract(ctx, user.KeyName(), contract, `{"increment":{}}`)
	require.NoError(t, err)
	require.Zero(t, execution.Code, "%s", execution.RawLog)
	authority, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "source-authority", accMnemonic, DefaultGenesisAmt, chain)
	require.NoError(t, err)
	require.Equal(t, accAddr, authority.FormattedAddress())
	rpc, err := rpchttp.NewWithClient(chain.GetHostRPCAddress(), "/websocket", &http.Client{Timeout: 5 * time.Second})
	require.NoError(t, err)
	height, err := chain.Height(ctx)
	require.NoError(t, err)
	completedHeight := height + 15
	scheduleInPlaceTestnetUpgrade(t, ctx, node, authority, chain.Config().ChainID, "v2.3.1", completedHeight, rpc)
	waitForInPlaceTestnetHeight(t, ctx, rpc, chain.Config().ChainID, completedHeight+3)
	require.Equal(t, completedHeight, inPlaceTestnetAppliedHeight(t, ctx, node, "v2.3.1"))
	// Keep a genuinely pending source plan across conversion. It is rescheduled
	// near the fork tip only after ordinary restart and authority checks succeed.
	height, err = chain.Height(ctx)
	require.NoError(t, err)
	scheduleInPlaceTestnetUpgrade(t, ctx, node, authority, chain.Config().ChainID, inPlaceTestnetFixtureVersion, height+1000, rpc)
	state := &inPlaceTestnetReleasedState{
		codeID: codeID, contract: contract, completedName: "v2.3.1", completedHeight: completedHeight,
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
	state.assertPreserved(t, ctx, node, false)
	t.Logf("Released source image ghcr.io/manifest-network/manifest-ledger:2.3.1, synthetic two-validator state: contract %s count=42, completed upgrade at %d, pending target %s", contract, completedHeight, inPlaceTestnetFixtureVersion)
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
	require.JSONEq(t, string(state.moduleVersions), string(queryInPlaceTestnet(t, ctx, node, "upgrade", "module-versions")))
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
	assertForkState(rpc)
	require.NoError(t, node.StopContainer(ctx))
	dockerutil.CopyCoverageFromContainer(ctx, t, client, node.ContainerID(), node.HomeDir(), ExternalGoCoverDir)
	require.NoError(t, node.RemoveContainer(ctx))
	require.NoError(t, node.CreateNodeContainer(ctx))
	require.NoError(t, node.StartContainer(ctx))
	rpc, err = rpchttp.NewWithClient(chain.GetHostRPCAddress(), "/websocket", &http.Client{Timeout: 5 * time.Second})
	require.NoError(t, err)
	waitForInPlaceTestnetHeight(t, ctx, rpc, chainID, height+5)
	require.Equal(t, upgradeHeight, inPlaceTestnetAppliedHeight(t, ctx, node, inPlaceTestnetFixtureVersion))
	state.assertPreserved(t, ctx, node, true)
	assertForkState(rpc)
	t.Logf("Scheduled upgrade halted at %d; fixture image %s:%s applied the +1 billing parameter migration exactly once across ordinary restart", upgradeHeight, image.Repository, image.Version)
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
