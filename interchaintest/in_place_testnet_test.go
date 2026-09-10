package interchaintest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/dockerutil"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/privval"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// TestInPlaceTestnet exercises the command against committed state with vote
// extensions enabled. It starts with two validators and verifies that both the
// staking and CometBFT sets are replaced while the isolated fork produces blocks.
func TestInPlaceTestnet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	for _, triggerUpgrade := range []bool{false, true} {
		t.Run(fmt.Sprintf("trigger_upgrade=%t", triggerUpgrade), func(t *testing.T) {
			testInPlaceTestnet(t, triggerUpgrade)
		})
	}
}

func testInPlaceTestnet(t *testing.T, triggerUpgrade bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	logger := zaptest.NewLogger(t)
	cfg := LocalChainConfig
	cfg.Name = "in-place-testnet"
	cfg.Env = append([]string(nil), cfg.Env...)
	if ref := os.Getenv("IN_PLACE_TESTNET_IMAGE"); ref != "" {
		separator := strings.LastIndex(ref, ":")
		require.Greater(t, separator, strings.LastIndex(ref, "/"), "IN_PLACE_TESTNET_IMAGE must be repository:tag")
		require.Less(t, separator, len(ref)-1, "IN_PLACE_TESTNET_IMAGE requires a tag")
		cfg.Images = []ibc.DockerImage{{Repository: ref[:separator], Version: ref[separator+1:], UIDGID: "1025:1025"}}
	}
	validatorCount, fullNodeCount := 2, 0
	chains, err := interchaintest.NewBuiltinChainFactory(logger, []*interchaintest.ChainSpec{{
		Name:          cfg.Name,
		Version:       cfg.Images[0].Version,
		ChainName:     cfg.ChainID,
		NumValidators: &validatorCount,
		NumFullNodes:  &fullNodeCount,
		ChainConfig:   cfg,
	}}).Chains(t.Name())
	require.NoError(t, err)
	chain := chains[0].(*cosmos.CosmosChain)
	client, network := interchaintest.DockerSetup(t)
	ic := interchaintest.NewInterchain().AddChain(chain)
	t.Cleanup(func() { _ = ic.Close() })
	require.NoError(t, ic.Build(ctx, testreporter.NewNopReporter().RelayerExecReporter(t), interchaintest.InterchainBuildOptions{
		TestName: t.Name(), Client: client, NetworkID: network, SkipPathCreation: true,
	}))

	node := chain.Validators[0]
	existingUser := interchaintest.GetAndFundTestUsers(t, ctx, "preserved", DefaultGenesisAmt, chain)[0]
	preservedBalance, err := chain.GetBalance(ctx, existingUser.FormattedAddress(), cfg.Denom)
	require.NoError(t, err)
	operator, err := chain.BuildWallet(ctx, "fork-operator", "")
	require.NoError(t, err)
	// Building the key must not create or fund an on-chain auth account. The fork
	// is responsible for making this new account usable.
	_, _, err = node.ExecQuery(ctx, "auth", "account", operator.FormattedAddress())
	require.Error(t, err)
	validators, err := chain.StakingQueryValidators(ctx, stakingtypes.Bonded.String())
	require.NoError(t, err)
	require.Len(t, validators, 2)
	sourceHeight, err := chain.Height(ctx)
	require.NoError(t, err)
	require.Greater(t, sourceHeight, int64(1))
	version, stderr, err := node.ExecBin(ctx, "version")
	require.NoError(t, err, "%s", stderr)
	upgradeName := strings.TrimSpace(string(version))
	require.NotEmpty(t, upgradeName)

	require.NoError(t, chain.StopAllNodes(ctx))
	require.NoError(t, node.RemoveContainer(ctx))
	// Config returns the environment slice used by CreateNodeContainer as well,
	// so persist the local authority for both this boot and ordinary restarts.
	forkEnv := chain.Config().Env
	require.Len(t, forkEnv, 1)
	forkEnv[0] = "POA_ADMIN_ADDRESS=" + operator.FormattedAddress()
	// Use only the stopped local test chain's data volume. Replace consensus
	// signing identity/state and remove peer discovery and the old consensus WAL
	// before the SDK rewrites the application and CometBFT databases.
	freshPV := privval.NewFilePV(ed25519.GenPrivKey(), "", "")
	keyJSON, err := cmtjson.Marshal(freshPV.Key)
	require.NoError(t, err)
	require.NoError(t, node.OverwritePrivValFile(ctx, keyJSON))
	require.NoError(t, node.WriteFile(ctx, []byte(`{"height":"0","round":0,"step":0}`), "data/priv_validator_state.json"))
	_, stderr, err = node.Exec(ctx, []string{
		"rm", "-rf", path.Join(node.HomeDir(), "config/addrbook.json"),
		path.Join(node.HomeDir(), "config/node_key.json"), path.Join(node.HomeDir(), "data/cs.wal"),
	}, cfg.Env)
	require.NoError(t, err, "%s", stderr)
	require.NoError(t, testutil.ModifyTomlConfigFile(ctx, logger, client, t.Name(), node.VolumeName, "config/config.toml", testutil.Toml{
		"p2p": testutil.Toml{
			"persistent_peers": "", "seeds": "", "pex": false,
		},
		"statesync": testutil.Toml{"enable": false},
	}))

	forkChainID := "manifest-in-place-testnet"
	command := node.BinCommand("in-place-testnet", forkChainID, operator.FormattedAddress(), "--skip-confirmation")
	if triggerUpgrade {
		command = append(command, "--trigger-testnet-upgrade", upgradeName)
	}
	// A separate lifecycle lets this first boot run the actual long-running CLI
	// command; the original ChainNode will later recreate its normal start command.
	fork := dockerutil.NewContainerLifecycle(logger, client, node.Name())
	require.NoError(t, fork.CreateContainer(ctx, t.Name(), network, node.Image,
		nat.PortMap{"26657/tcp": {}}, "", node.Bind(), nil, node.HostName(), command, forkEnv, nil))
	forkRemoved := false
	t.Cleanup(func() {
		if forkRemoved {
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if t.Failed() {
			logs, err := client.ContainerLogs(cleanupCtx, fork.ContainerID(), container.LogsOptions{ShowStdout: true, ShowStderr: true, Tail: "100"})
			if err == nil {
				defer logs.Close()
				content, _ := io.ReadAll(logs)
				t.Logf("in-place-testnet logs:\n%s", content)
			}
		}
		_ = fork.StopContainer(cleanupCtx)
		_ = fork.RemoveContainer(cleanupCtx)
	})
	require.NoError(t, fork.StartContainer(ctx))
	ports, err := fork.GetHostPorts(ctx, "26657/tcp")
	require.NoError(t, err)
	rpc, err := rpchttp.NewWithClient("http://"+ports[0], "/websocket", &http.Client{Timeout: 5 * time.Second})
	require.NoError(t, err)
	forkHeight := waitForInPlaceTestnetHeight(t, ctx, rpc, forkChainID, sourceHeight+5)

	operatorValAddress, err := sdk.Bech32ifyAddressBytes(cfg.Bech32Prefix+"valoper", operator.Address())
	require.NoError(t, err)
	expectedPower := int64(900_000_000_000_000)
	assertForkState := func(rpc *rpchttp.HTTP) {
		t.Helper()
		genesis, err := rpc.Genesis(ctx)
		require.NoError(t, err)
		require.Equal(t, forkChainID, genesis.Genesis.ChainID, "CometBFT's cached genesis still names the source chain")
		validatorJSON := queryInPlaceTestnet(t, ctx, node, "staking", "validators")
		// The staking CLI renders consensus keys using CometBFT's type/value
		// JSON representation rather than protobuf Any's @type/key fields.
		var stakingValidators struct {
			Validators []struct {
				OperatorAddress string      `json:"operator_address"`
				Status          string      `json:"status"`
				Jailed          bool        `json:"jailed"`
				Tokens          sdkmath.Int `json:"tokens"`
				ConsensusPubkey struct {
					Type  string `json:"type"`
					Value string `json:"value"`
				} `json:"consensus_pubkey"`
			} `json:"validators"`
		}
		require.NoError(t, json.Unmarshal(validatorJSON, &stakingValidators))
		require.Len(t, stakingValidators.Validators, 1, "source validators remain in staking state")
		validator := stakingValidators.Validators[0]
		require.Equal(t, operatorValAddress, validator.OperatorAddress)
		require.Equal(t, stakingtypes.Bonded.String(), validator.Status)
		require.False(t, validator.Jailed)
		require.Equal(t, "tendermint/PubKeyEd25519", validator.ConsensusPubkey.Type)
		pubKey, err := base64.StdEncoding.DecodeString(validator.ConsensusPubkey.Value)
		require.NoError(t, err)
		require.Equal(t, freshPV.Key.PubKey.Bytes(), pubKey)
		cometValidators, err := rpc.Validators(ctx, nil, nil, nil)
		require.NoError(t, err)
		require.Equal(t, 1, cometValidators.Total)
		require.Len(t, cometValidators.Validators, 1)
		require.Equal(t, freshPV.Key.PubKey.Address(), cometValidators.Validators[0].Address)
		require.Equal(t, expectedPower, cometValidators.Validators[0].VotingPower)
		require.Equal(t, validator.Tokens.Quo(sdk.DefaultPowerReduction).Int64(), cometValidators.Validators[0].VotingPower)
		var authResponse struct {
			Account struct {
				Value struct {
					Address string `json:"address"`
				} `json:"value"`
			} `json:"account"`
		}
		require.NoError(t, json.Unmarshal(queryInPlaceTestnet(t, ctx, node, "auth", "account", operator.FormattedAddress()), &authResponse))
		require.Equal(t, operator.FormattedAddress(), authResponse.Account.Value.Address)
		require.True(t, inPlaceTestnetBalance(t, ctx, node, operator.FormattedAddress(), cfg.Denom).IsPositive())
		var authorityResponse struct {
			Address string `json:"address"`
		}
		require.NoError(t, json.Unmarshal(queryInPlaceTestnet(t, ctx, node, "upgrade", "authority"), &authorityResponse))
		require.Equal(t, operator.FormattedAddress(), authorityResponse.Address)
		if triggerUpgrade {
			var applied struct {
				Height int64 `json:"height,string"`
			}
			require.NoError(t, json.Unmarshal(queryInPlaceTestnet(t, ctx, node, "upgrade", "applied", upgradeName), &applied))
			require.Greater(t, applied.Height, sourceHeight)
			require.LessOrEqual(t, applied.Height, forkHeight)
		}
	}
	assertForkState(rpc)
	require.Equal(t, preservedBalance, inPlaceTestnetBalance(t, ctx, node, existingUser.FormattedAddress(), cfg.Denom))
	genesisJSON, err := node.GenesisFileContent(ctx)
	require.NoError(t, err)
	var genesis struct {
		ChainID string `json:"chain_id"`
	}
	require.NoError(t, json.Unmarshal(genesisJSON, &genesis))
	require.Equal(t, forkChainID, genesis.ChainID)

	// Prove that the replacement account can sign and spend its balance on the
	// new chain ID. Use explicit flags because ChainConfig still names the source.
	sendInPlaceTestnetTx(t, ctx, node, operator.KeyName(), forkChainID,
		"bank", "send", operator.KeyName(), existingUser.FormattedAddress(), "1"+cfg.Denom)
	status, err := rpc.Status(ctx)
	require.NoError(t, err)
	forkHeight = waitForInPlaceTestnetHeight(t, ctx, rpc, forkChainID, status.SyncInfo.LatestBlockHeight+3)
	require.Equal(t, preservedBalance.AddRaw(1), inPlaceTestnetBalance(t, ctx, node, existingUser.FormattedAddress(), cfg.Denom))

	// The initial power matches SDK testnetify. A local PoA authority must be
	// able to reduce it to one consensus power before adding more validators.
	sendInPlaceTestnetTx(t, ctx, node, operator.KeyName(), forkChainID,
		"poa", "set-power", operatorValAddress, "1000000", "--unsafe")
	status, err = rpc.Status(ctx)
	require.NoError(t, err)
	forkHeight = waitForInPlaceTestnetHeight(t, ctx, rpc, forkChainID, status.SyncInfo.LatestBlockHeight+5)
	expectedPower = 1
	assertForkState(rpc)

	require.NoError(t, fork.StopContainer(ctx))
	require.NoError(t, fork.RemoveContainer(ctx))
	forkRemoved = true
	require.NoError(t, node.CreateNodeContainer(ctx))
	require.NoError(t, node.StartContainer(ctx))
	rpc, err = rpchttp.NewWithClient(chain.GetHostRPCAddress(), "/websocket", &http.Client{Timeout: 5 * time.Second})
	require.NoError(t, err)
	waitForInPlaceTestnetHeight(t, ctx, rpc, forkChainID, forkHeight+5)
	assertForkState(rpc)
	require.Equal(t, preservedBalance.AddRaw(1), inPlaceTestnetBalance(t, ctx, node, existingUser.FormattedAddress(), cfg.Denom))
}

func waitForInPlaceTestnetHeight(t *testing.T, ctx context.Context, rpc *rpchttp.HTTP, chainID string, target int64) int64 {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var height int64
	for {
		status, err := rpc.Status(waitCtx)
		if err == nil {
			height = status.SyncInfo.LatestBlockHeight
			if status.NodeInfo.Network == chainID && height >= target {
				return height
			}
		}
		select {
		case <-waitCtx.Done():
			require.FailNow(t, "fork did not advance", "target height %d, last height %d, RPC error %v", target, height, err)
			return height
		case <-ticker.C:
		}
	}
}

func sendInPlaceTestnetTx(t *testing.T, ctx context.Context, node *cosmos.ChainNode, keyName, chainID string, command ...string) {
	t.Helper()
	command = append([]string{"tx"}, command...)
	command = append(command, "--from", keyName, "--chain-id", chainID, "--keyring-backend", "test",
		"--fees", "0"+node.Chain.Config().Denom, "--gas", "200000", "--broadcast-mode", "sync", "--output", "json", "--yes")
	stdout, stderr, err := node.Exec(ctx, node.NodeCommand(command...), node.Chain.Config().Env)
	require.NoError(t, err, "%s", stderr)
	var response struct {
		Code   uint32 `json:"code"`
		TxHash string `json:"txhash"`
		RawLog string `json:"raw_log"`
	}
	require.NoError(t, json.Unmarshal(stdout, &response))
	require.Zero(t, response.Code, "%s", response.RawLog)
	require.NotEmpty(t, response.TxHash)
}

func queryInPlaceTestnet(t *testing.T, ctx context.Context, node *cosmos.ChainNode, command ...string) []byte {
	t.Helper()
	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	stdout, stderr, err := node.ExecQuery(queryCtx, command...)
	require.NoError(t, err, "%s", stderr)
	return stdout
}

func inPlaceTestnetBalance(t *testing.T, ctx context.Context, node *cosmos.ChainNode, address, denom string) sdkmath.Int {
	t.Helper()
	var response struct {
		Balances sdk.Coins `json:"balances"`
	}
	require.NoError(t, json.Unmarshal(queryInPlaceTestnet(t, ctx, node, "bank", "balances", address), &response))
	return response.Balances.AmountOf(denom)
}
