package interchaintest

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/stretchr/testify/require"

	rpchttp "github.com/cometbft/cometbft/rpc/client/http"

	sdkmath "cosmossdk.io/math"
	circuittypes "cosmossdk.io/x/circuit/types"

	sdked25519 "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	poa "github.com/strangelove-ventures/poa"
)

// The upstream WriteFile recursively chowns the entire volume, racing live
// database compaction. Set ownership on this archive entry only. Use the stable
// container name because the initial fork has a separate container lifecycle.
func writeInPlaceTestnetLiveFile(ctx context.Context, node *cosmos.ChainNode, name string, content []byte) error {
	if name == "" || name == "." || name == ".." || path.Base(name) != name {
		return fmt.Errorf("live upload requires a file name, got %q", name)
	}
	uidText, gidText, ok := strings.Cut(node.Image.UIDGID, ":")
	if !ok {
		return fmt.Errorf("live upload requires numeric UID:GID, got %q", node.Image.UIDGID)
	}
	uid, err := strconv.Atoi(uidText)
	if err != nil || uid < 0 {
		return fmt.Errorf("invalid upload UID %q", uidText)
	}
	gid, err := strconv.Atoi(gidText)
	if err != nil || gid < 0 {
		return fmt.Errorf("invalid upload GID %q", gidText)
	}
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(content)), Uid: uid, Gid: gid}); err != nil {
		return err
	}
	if _, err := writer.Write(content); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return node.DockerClient.CopyToContainer(ctx, node.Name(), node.HomeDir(), &archive, container.CopyToContainerOptions{})
}

// The snapshot excludes Go coverage output, which each CLI process may emit even
// when preflight rejects the command. All application, consensus, and key files
// must remain byte-for-byte unchanged.
func inPlaceTestnetFileSnapshot(t *testing.T, ctx context.Context, node *cosmos.ChainNode) string {
	t.Helper()
	stdout, stderr, err := node.Exec(ctx, []string{"sh", "-c", `find "$1" -type f ! -name 'cov*' -exec sha256sum {} + | sort`, "sh", node.HomeDir()}, node.Chain.Config().Env)
	require.NoError(t, err, "%s", stderr)
	require.NotEmpty(t, stdout)
	return string(stdout)
}

func assertInPlaceTestnetPreflight(t *testing.T, ctx context.Context, node *cosmos.ChainNode, chainID, operator string, sourceKey, freshKey []byte) {
	t.Helper()
	reject := func(name, requestedChainID, expectedError string) {
		t.Helper()
		before := inPlaceTestnetFileSnapshot(t, ctx, node)
		rejectCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		_, stderr, err := node.Exec(rejectCtx, node.BinCommand("in-place-testnet", requestedChainID, operator, "--skip-confirmation"), node.Chain.Config().Env)
		require.Error(t, err, "%s must fail before conversion", name)
		require.NotContains(t, err.Error(), "context deadline exceeded", "%s unexpectedly started: %s", name, stderr)
		// The pinned interchaintest Exec embeds stdout/stderr in the error and
		// returns empty output buffers when a process exits unsuccessfully.
		require.Contains(t, err.Error(), expectedError, "%s: unexpected failure: %s", name, stderr)
		require.Equal(t, before, inPlaceTestnetFileSnapshot(t, ctx, node), "%s modified the copied home", name)
	}
	reject("source chain ID", node.Chain.Config().ChainID, "requires a new chain ID different from source chain")
	require.NoError(t, node.OverwritePrivValFile(ctx, sourceKey))
	reject("source consensus key", chainID, "reuses a source last/current/next consensus key")
	require.NoError(t, node.OverwritePrivValFile(ctx, freshKey))
	// The exact on-disk TOML is restored after each isolated configuration case.
	config, err := node.ReadFile(ctx, "config/config.toml")
	require.NoError(t, err)
	for _, tc := range []struct{ name, expression, expectedError string }{
		{"peer discovery", `s/pex = false/pex = true/`, "fork isolation requires"},
		{"external address book", `s#addr_book_file = .*#addr_book_file = "/tmp/in-place-testnet-outside-home.json"#`, "inside the fork home"},
	} {
		_, stderr, err := node.Exec(ctx, []string{"sed", "-i", tc.expression, path.Join(node.HomeDir(), "config/config.toml")}, node.Chain.Config().Env)
		require.NoError(t, err, "%s", stderr)
		modified, err := node.ReadFile(ctx, "config/config.toml")
		require.NoError(t, err)
		require.NotEqual(t, string(config), string(modified), "%s fixture did not alter the config", tc.name)
		reject(tc.name, chainID, tc.expectedError)
		require.NoError(t, node.WriteFile(ctx, config, "config/config.toml"))
	}
}

func inPlaceTestnetDisabledMessages() []string {
	return []string{
		sdk.MsgTypeURL(&poa.MsgSetPower{}), sdk.MsgTypeURL(&poa.MsgRemoveValidator{}),
		sdk.MsgTypeURL(&poa.MsgCreateValidator{}), sdk.MsgTypeURL(&circuittypes.MsgResetCircuitBreaker{}),
		sdk.MsgTypeURL(&stakingtypes.MsgCreateValidator{}), sdk.MsgTypeURL(&stakingtypes.MsgDelegate{}),
		sdk.MsgTypeURL(&stakingtypes.MsgUndelegate{}), sdk.MsgTypeURL(&stakingtypes.MsgBeginRedelegate{}),
		sdk.MsgTypeURL(&stakingtypes.MsgCancelUnbondingDelegation{}), sdk.MsgTypeURL(&stakingtypes.MsgUpdateParams{}),
	}
}

func assertInPlaceTestnetCircuit(t *testing.T, ctx context.Context, node *cosmos.ChainNode) {
	t.Helper()
	var response struct {
		DisabledList []string `json:"disabled_list"`
	}
	require.NoError(t, json.Unmarshal(queryInPlaceTestnet(t, ctx, node, "circuit", "disabled-list"), &response))
	require.ElementsMatch(t, inPlaceTestnetDisabledMessages(), response.DisabledList)
}

type inPlaceTestnetTxResponse struct {
	Height int64  `json:"height,string"`
	Code   uint32 `json:"code"`
	TxHash string `json:"txhash"`
	RawLog string `json:"raw_log"`
}

// Signing explicit messages permits exercising authority-gated upgrade and
// circuit messages without wrapping them in a governance proposal.
func broadcastInPlaceTestnetMessage(t *testing.T, ctx context.Context, node *cosmos.ChainNode, keyName, chainID string, msg sdk.Msg) inPlaceTestnetTxResponse {
	t.Helper()
	txConfig := node.Chain.Config().EncodingConfig.TxConfig
	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msg))
	builder.SetGasLimit(400_000)
	unsigned, err := txConfig.TxJSONEncoder()(builder.GetTx())
	require.NoError(t, err)
	require.NoError(t, writeInPlaceTestnetLiveFile(ctx, node, "testnet-unsigned.json", unsigned))
	_, stderr, err := node.Exec(ctx, node.NodeCommand("tx", "sign", path.Join(node.HomeDir(), "testnet-unsigned.json"),
		"--from", keyName, "--chain-id", chainID, "--keyring-backend", "test", "--sign-mode", "direct",
		"--output-document", path.Join(node.HomeDir(), "testnet-signed.json")), node.Chain.Config().Env)
	require.NoError(t, err, "%s", stderr)
	stdout, stderr, err := node.Exec(ctx, node.NodeCommand("tx", "broadcast", path.Join(node.HomeDir(), "testnet-signed.json"),
		"--chain-id", chainID, "--broadcast-mode", "sync", "--output", "json"), node.Chain.Config().Env)
	require.NoError(t, err, "%s", stderr)
	var response inPlaceTestnetTxResponse
	require.NoError(t, json.Unmarshal(stdout, &response))
	require.NotEmpty(t, response.TxHash)
	return response
}

func assertInPlaceTestnetLifecycleBlocked(t *testing.T, ctx context.Context, node *cosmos.ChainNode, operator ibc.Wallet, validator, chainID string, rpc *rpchttp.HTTP) {
	t.Helper()
	create, err := poa.NewMsgCreateValidator(validator, sdked25519.GenPrivKey().PubKey(), poa.Description{Moniker: "blocked"},
		poa.CommissionRates{Rate: sdkmath.LegacyZeroDec(), MaxRate: sdkmath.LegacyOneDec(), MaxChangeRate: sdkmath.LegacyOneDec()}, sdkmath.OneInt())
	require.NoError(t, err)
	messages := []sdk.Msg{
		&poa.MsgSetPower{Sender: operator.FormattedAddress(), ValidatorAddress: validator, Power: 1, Unsafe: true},
		&poa.MsgRemoveValidator{Sender: operator.FormattedAddress(), ValidatorAddress: validator},
		create,
		&circuittypes.MsgResetCircuitBreaker{Authority: operator.FormattedAddress(), MsgTypeUrls: inPlaceTestnetDisabledMessages()},
	}
	for _, msg := range messages {
		response := broadcastInPlaceTestnetMessage(t, ctx, node, operator.KeyName(), chainID, msg)
		if response.Code == 0 {
			status, err := rpc.Status(ctx)
			require.NoError(t, err)
			waitForInPlaceTestnetHeight(t, ctx, rpc, chainID, status.SyncInfo.LatestBlockHeight+2)
			require.NoError(t, json.Unmarshal(queryInPlaceTestnet(t, ctx, node, "tx", response.TxHash), &response))
		}
		require.NotZero(t, response.Code, "%s bypassed the fixed-validator circuit", sdk.MsgTypeURL(msg))
		require.True(t, strings.Contains(response.RawLog, "tx type not allowed") || strings.Contains(response.RawLog, "circuit breaker disables execution of this message"), "%s was rejected for an unrelated reason: %s", sdk.MsgTypeURL(msg), response.RawLog)
	}
	assertInPlaceTestnetCircuit(t, ctx, node)
}
