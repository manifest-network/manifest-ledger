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
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/privval"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"

	sdkmath "cosmossdk.io/math"
	circuittypes "cosmossdk.io/x/circuit/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
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
			testInPlaceTestnet(t, triggerUpgrade, false)
		})
	}
	t.Run("released_source_upgrade", func(t *testing.T) { testInPlaceTestnet(t, false, true) })
}

func testInPlaceTestnet(t *testing.T, triggerUpgrade, releasedSource bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	logger := zaptest.NewLogger(t)
	cfg := LocalChainConfig
	cfg.Name = "in-place-testnet"
	cfg.Env = append([]string(nil), cfg.Env...)
	targetImage := inPlaceTestnetImage(t, "IN_PLACE_TESTNET_IMAGE", "manifest:local")
	cfg.Images = []ibc.DockerImage{targetImage}
	cfg.EncodingConfig = AppEncoding()
	circuittypes.RegisterInterfaces(cfg.EncodingConfig.InterfaceRegistry)
	cfg.WithCodeCoverage()
	if releasedSource {
		// This is synthetic state produced by the published release, not a mainnet snapshot.
		cfg.Images[0] = ibc.DockerImage{Repository: "ghcr.io/manifest-network/manifest-ledger", Version: "2.3.1", UIDGID: "1025:1025"}
		cfg.Env[len(cfg.Env)-1] = "GOCOVERDIR=" // Do not mix released-binary counters into PR coverage.
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
	_, err = authtypes.NewQueryClient(node.GrpcConn).Account(ctx, &authtypes.QueryAccountRequest{Address: operator.FormattedAddress()})
	require.Equal(t, codes.NotFound, grpcstatus.Code(err))
	require.Equal(t, "account "+operator.FormattedAddress()+" not found", grpcstatus.Convert(err).Message())
	var released *inPlaceTestnetReleasedState
	if releasedSource {
		released = seedInPlaceTestnetReleasedState(t, ctx, chain, existingUser)
	}
	validators, err := chain.StakingQueryValidators(ctx, stakingtypes.Bonded.String())
	require.NoError(t, err)
	require.Len(t, validators, 2)
	sourceValidators, err := node.Client.Validators(ctx, nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 2, sourceValidators.Total)
	sourceConsensus, err := node.Client.ConsensusParams(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, int64(1), sourceConsensus.ConsensusParams.ABCI.VoteExtensionsEnableHeight)
	sourceHeight, err := chain.Height(ctx)
	require.NoError(t, err)
	require.Greater(t, sourceHeight, int64(1))
	version, stderr, err := node.ExecBin(ctx, "version")
	require.NoError(t, err, "%s", stderr)
	upgradeName := strings.TrimSpace(string(version))
	require.NotEmpty(t, upgradeName)

	require.NoError(t, chain.StopAllNodes(ctx))
	if !releasedSource {
		for _, sourceNode := range chain.Validators {
			dockerutil.CopyCoverageFromContainer(ctx, t, client, sourceNode.ContainerID(), sourceNode.HomeDir(), ExternalGoCoverDir)
		}
	}
	require.NoError(t, node.RemoveContainer(ctx))
	node.Image = targetImage
	chain.Config().Images[0] = targetImage
	// Config returns the environment slice used by CreateNodeContainer as well,
	// so persist the local authority for both this boot and ordinary restarts.
	forkEnv := chain.Config().Env
	require.Len(t, forkEnv, 2)
	forkEnv[0] = "POA_ADMIN_ADDRESS=" + operator.FormattedAddress()
	forkEnv[1] = "GOCOVERDIR=" + node.HomeDir()
	// Use only the stopped local test chain's data volume. Replace consensus
	// signing identity/state and remove peer discovery. Keep the copied WAL so
	// negative preflight snapshots cover it and successful conversion cleans it.
	sourceKeyJSON, err := node.ReadFile(ctx, "config/priv_validator_key.json")
	require.NoError(t, err)
	freshPV := privval.NewFilePV(ed25519.GenPrivKey(), "", "")
	keyJSON, err := cmtjson.Marshal(freshPV.Key)
	require.NoError(t, err)
	require.NoError(t, node.OverwritePrivValFile(ctx, keyJSON))
	require.NoError(t, node.WriteFile(ctx, []byte(`{"height":"0","round":0,"step":0}`), "data/priv_validator_state.json"))
	_, stderr, err = node.Exec(ctx, []string{
		"rm", "-rf", path.Join(node.HomeDir(), "config/addrbook.json"),
		path.Join(node.HomeDir(), "config/node_key.json"),
	}, cfg.Env)
	require.NoError(t, err, "%s", stderr)
	require.NoError(t, testutil.ModifyTomlConfigFile(ctx, logger, client, t.Name(), node.VolumeName, "config/config.toml", testutil.Toml{
		"p2p": testutil.Toml{
			"persistent_peers": "", "seeds": "", "pex": false,
		},
		"statesync": testutil.Toml{"enable": false},
	}))

	forkChainID := "manifest-in-place-testnet"
	assertInPlaceTestnetPreflight(t, ctx, node, forkChainID, operator.FormattedAddress(), sourceKeyJSON, keyJSON)
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
		dockerutil.CopyCoverageFromContainer(cleanupCtx, t, client, fork.ContainerID(), node.HomeDir(), ExternalGoCoverDir)
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
	const expectedPower int64 = 900_000_000_000_000
	expectedOperatorFunds := sdkmath.NewInt(1_000_000_000_000)
	assertForkState := func(rpc *rpchttp.HTTP) {
		t.Helper()
		genesis, err := rpc.Genesis(ctx)
		require.NoError(t, err)
		require.Equal(t, forkChainID, genesis.Genesis.ChainID, "CometBFT's cached genesis still names the source chain")
		validatorJSON := queryInPlaceTestnet(t, ctx, node, "staking", "validators")
		// AutoCLI uses aminojson: consensus keys use type/value, and the auth
		// account below exposes its address under account.value.
		var stakingValidators struct {
			Validators []struct {
				OperatorAddress string            `json:"operator_address"`
				Status          string            `json:"status"`
				Jailed          bool              `json:"jailed"`
				Tokens          sdkmath.Int       `json:"tokens"`
				DelegatorShares sdkmath.LegacyDec `json:"delegator_shares"`
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
		require.True(t, validator.DelegatorShares.Equal(sdkmath.LegacyNewDecFromInt(validator.Tokens)))
		poolResponse, err := decodeInPlaceTestnetPool(queryInPlaceTestnet(t, ctx, node, "staking", "pool"))
		require.NoError(t, err)
		require.True(t, poolResponse.Pool.BondedTokens.Equal(validator.Tokens), "bonded pool must back the seeded validator")
		require.True(t, poolResponse.Pool.NotBondedTokens.IsZero(), "source unbonded stake remains")
		delegationsResponse, err := decodeInPlaceTestnetDelegations(queryInPlaceTestnet(t, ctx, node, "staking", "delegations", operator.FormattedAddress()))
		require.NoError(t, err)
		require.Len(t, delegationsResponse.DelegationResponses, 1)
		delegation := delegationsResponse.DelegationResponses[0]
		require.Equal(t, operator.FormattedAddress(), delegation.Delegation.DelegatorAddress)
		require.Equal(t, operatorValAddress, delegation.Delegation.ValidatorAddress)
		require.True(t, delegation.Delegation.Shares.Equal(validator.DelegatorShares))
		require.True(t, delegation.Balance.Amount.Equal(validator.Tokens))
		var authResponse struct {
			Account struct {
				Value struct {
					Address string `json:"address"`
				} `json:"value"`
			} `json:"account"`
		}
		require.NoError(t, json.Unmarshal(queryInPlaceTestnet(t, ctx, node, "auth", "account", operator.FormattedAddress()), &authResponse))
		require.Equal(t, operator.FormattedAddress(), authResponse.Account.Value.Address)
		require.True(t, expectedOperatorFunds.Equal(inPlaceTestnetBalance(t, ctx, node, operator.FormattedAddress(), cfg.Denom)), "operator funding changed unexpectedly")
		assertInPlaceTestnetCircuit(t, ctx, node)
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
	if released != nil {
		released.assertPreserved(t, ctx, node, false)
	}
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

	expectedOperatorFunds = expectedOperatorFunds.SubRaw(1)
	assertForkState(rpc)

	require.NoError(t, fork.StopContainer(ctx))
	dockerutil.CopyCoverageFromContainer(ctx, t, client, fork.ContainerID(), node.HomeDir(), ExternalGoCoverDir)
	require.NoError(t, fork.RemoveContainer(ctx))
	forkRemoved = true
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = node.StopContainer(cleanupCtx)
		dockerutil.CopyCoverageFromContainer(cleanupCtx, t, client, node.ContainerID(), node.HomeDir(), ExternalGoCoverDir)
	})
	require.NoError(t, node.CreateNodeContainer(ctx))
	require.NoError(t, node.StartContainer(ctx))
	rpc, err = rpchttp.NewWithClient(chain.GetHostRPCAddress(), "/websocket", &http.Client{Timeout: 5 * time.Second})
	require.NoError(t, err)
	waitForInPlaceTestnetHeight(t, ctx, rpc, forkChainID, forkHeight+5)
	assertForkState(rpc)
	require.Equal(t, preservedBalance.AddRaw(1), inPlaceTestnetBalance(t, ctx, node, existingUser.FormattedAddress(), cfg.Denom))

	// Payout checks the manifest keeper's authority, so a committed payout proves
	// that the replacement operator retains admin access after ordinary restart.
	payoutHash := sendInPlaceTestnetTx(t, ctx, node, operator.KeyName(), forkChainID,
		"manifest", "payout", existingUser.FormattedAddress()+":1"+cfg.Denom)
	status, err = rpc.Status(ctx)
	require.NoError(t, err)
	forkHeight = waitForInPlaceTestnetHeight(t, ctx, rpc, forkChainID, status.SyncInfo.LatestBlockHeight+3)
	var payoutResponse struct {
		Code   uint32 `json:"code"`
		RawLog string `json:"raw_log"`
	}
	require.NoError(t, json.Unmarshal(queryInPlaceTestnet(t, ctx, node, "tx", payoutHash), &payoutResponse))
	require.Zero(t, payoutResponse.Code, "%s", payoutResponse.RawLog)
	require.Equal(t, preservedBalance.AddRaw(2), inPlaceTestnetBalance(t, ctx, node, existingUser.FormattedAddress(), cfg.Denom))
	assertForkState(rpc)
	assertInPlaceTestnetLifecycleBlocked(t, ctx, node, operator, operatorValAddress, forkChainID, rpc)
	assertForkState(rpc)
	if released != nil {
		released.assertPreserved(t, ctx, node, false)
		runInPlaceTestnetReleasedUpgrade(t, ctx, chain, client, operator, forkChainID, released, assertForkState)
	}
}

func inPlaceTestnetImage(t *testing.T, variable, fallback string) ibc.DockerImage {
	t.Helper()
	ref := os.Getenv(variable)
	if ref == "" {
		ref = fallback
	}
	separator := strings.LastIndex(ref, ":")
	require.Greater(t, separator, strings.LastIndex(ref, "/"), "%s must be repository:tag", variable)
	require.Less(t, separator, len(ref)-1, "%s requires a tag", variable)
	return ibc.DockerImage{Repository: ref[:separator], Version: ref[separator+1:], UIDGID: "1025:1025"}
}

// inPlaceTestnetPoolResponse projects the staking pool's quoted token amounts.
type inPlaceTestnetPoolResponse struct {
	Pool struct {
		BondedTokens    sdkmath.Int `json:"bonded_tokens"`
		NotBondedTokens sdkmath.Int `json:"not_bonded_tokens"`
	} `json:"pool"`
}

// inPlaceTestnetDelegationsResponse projects the fields asserted by this test
// without coupling the query decoder to unused pagination or generated JSON tags.
type inPlaceTestnetDelegationsResponse struct {
	DelegationResponses []struct {
		Delegation struct {
			DelegatorAddress string            `json:"delegator_address"`
			ValidatorAddress string            `json:"validator_address"`
			Shares           sdkmath.LegacyDec `json:"shares"`
		} `json:"delegation"`
		Balance sdk.Coin `json:"balance"`
	} `json:"delegation_responses"`
}

func decodeInPlaceTestnetPool(data []byte) (inPlaceTestnetPoolResponse, error) {
	var response inPlaceTestnetPoolResponse
	err := json.Unmarshal(data, &response)
	return response, err
}

func decodeInPlaceTestnetDelegations(data []byte) (inPlaceTestnetDelegationsResponse, error) {
	var response inPlaceTestnetDelegationsResponse
	err := json.Unmarshal(data, &response)
	return response, err
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

func sendInPlaceTestnetTx(t *testing.T, ctx context.Context, node *cosmos.ChainNode, keyName, chainID string, command ...string) string {
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
	return response.TxHash
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
