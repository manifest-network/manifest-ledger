package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"github.com/cometbft/cometbft/p2p"
	"github.com/cometbft/cometbft/privval"

	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/server"
	srvconfig "github.com/cosmos/cosmos-sdk/server/config"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/manifest-network/manifest-ledger/app"
)

const testnetInitFilesSubcommand = "init-files"

func testnetApplicationFixture(t *testing.T) *app.ManifestApp {
	t.Helper()
	// Match the root command's encoding/basic-module construction without
	// app.Setup's additional mutation of the process-global SDK address prefixes.
	application := app.NewApp(log.NewNopLogger(), dbm.NewMemDB(), nil, true,
		app.DefaultCommissionRateMinMax, simtestutil.NewAppOptionsWithFlagHome(t.TempDir()))
	t.Cleanup(func() { require.NoError(t, application.Close()) })
	return application
}

func testnetCommandFixture(t *testing.T, application *app.ManifestApp) (*cobra.Command, client.Context) {
	t.Helper()
	// The SDK exposes no template getter. These sequential fixtures establish
	// and restore its documented default, rather than retaining testnet's
	// package-global replacement for later tests.
	srvconfig.SetConfigTemplate(srvconfig.DefaultConfigTemplate)
	t.Cleanup(func() { srvconfig.SetConfigTemplate(srvconfig.DefaultConfigTemplate) })
	clientCtx := client.Context{}.
		WithCodec(application.AppCodec()).
		WithInterfaceRegistry(application.InterfaceRegistry()).
		WithTxConfig(application.TxConfig()).
		WithLegacyAmino(application.LegacyAmino()).
		WithHomeDir(t.TempDir()).
		WithInput(strings.NewReader("")).
		WithOffline(true)
	command := NewTestnetCmd(application.BasicModuleManager, banktypes.GenesisBalancesIterator{})
	command.SetContext(context.WithValue(context.Background(), server.ServerContextKey, server.NewDefaultContext()))
	require.NoError(t, client.SetCmdClientContext(command, clientCtx))
	command.SilenceErrors = true
	command.SilenceUsage = true
	return command, clientCtx
}

func TestTestnetInitFilesProducesConsistentSignedNetwork(t *testing.T) {
	application := testnetApplicationFixture(t)
	for _, singleHost := range []bool{false, true} {
		t.Run(fmt.Sprintf("single_host_%t", singleHost), func(t *testing.T) {
			command, clientCtx := testnetCommandFixture(t, application)
			outputDir := filepath.Join(t.TempDir(), "network")
			args := []string{
				testnetInitFilesSubcommand, "--v", "2", "--output-dir", outputDir,
				"--keyring-backend", "test", "--node-daemon-home", "daemon",
				"--starting-ip-address", "192.0.2.10", "--commit-timeout", "3s",
				"--minimum-gas-prices", "0.125stake",
			}
			if singleHost {
				// The old --algo spelling must still select the signing algorithm.
				args = append(args, "--single-host", "--algo", "secp256k1")
			} else {
				args = append(args, "--chain-id", "generated-network-test")
			}
			command.SetArgs(args)
			var output, diagnostics bytes.Buffer
			command.SetOut(&output)
			command.SetErr(&diagnostics)
			require.NoError(t, command.Execute())
			require.Empty(t, output.String())
			require.Contains(t, diagnostics.String(), "Successfully initialized 2 node directories")

			var canonicalGenesis []byte
			memos := make([]string, 2)
			configs := make([]*viper.Viper, 2)
			for i := range 2 {
				nodeName := fmt.Sprintf("node%d", i)
				nodeDir := filepath.Join(outputDir, nodeName, "daemon")
				genesisFile := filepath.Join(nodeDir, "config", "genesis.json")
				genesisBytes, err := os.ReadFile(genesisFile) //nolint:gosec // Generated fixture below t.TempDir.
				require.NoError(t, err)
				if i == 0 {
					canonicalGenesis = genesisBytes
				} else {
					require.Equal(t, canonicalGenesis, genesisBytes, "nodes must agree on both genesis state and time")
				}
				genesis, err := genutiltypes.AppGenesisFromFile(genesisFile)
				require.NoError(t, err)
				require.False(t, genesis.GenesisTime.IsZero())
				if singleHost {
					require.Regexp(t, `^chain-[a-zA-Z0-9]{6}$`, genesis.ChainID)
				} else {
					require.Equal(t, "generated-network-test", genesis.ChainID)
				}

				var state map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(genesis.AppState, &state))
				var authGenesis authtypes.GenesisState
				clientCtx.Codec.MustUnmarshalJSON(state[authtypes.ModuleName], &authGenesis)
				accounts, err := authtypes.UnpackAccounts(authGenesis.Accounts)
				require.NoError(t, err)
				require.Len(t, accounts, 2)
				var bankGenesis banktypes.GenesisState
				clientCtx.Codec.MustUnmarshalJSON(state[banktypes.ModuleName], &bankGenesis)
				require.Len(t, bankGenesis.Balances, 2)
				expectedFunds := sdk.NewCoins(
					sdk.NewCoin(sdk.DefaultBondDenom, sdk.TokensFromConsensusPower(500, sdk.DefaultPowerReduction)),
					sdk.NewCoin("testtoken", sdk.TokensFromConsensusPower(1000, sdk.DefaultPowerReduction)),
				)
				require.Equal(t, expectedFunds.Add(expectedFunds...), bankGenesis.Supply)
				accountAddresses := make([]string, 0, len(accounts))
				for _, account := range accounts {
					accountAddresses = append(accountAddresses, account.GetAddress().String())
					var funds sdk.Coins
					for _, balance := range bankGenesis.Balances {
						if balance.Address == account.GetAddress().String() {
							funds = balance.Coins
						}
					}
					require.Equal(t, expectedFunds, funds, "each genesis signer must be funded")
				}

				gentxFile := filepath.Join(outputDir, "gentxs", nodeName+".json")
				gentxBytes, err := os.ReadFile(gentxFile) //nolint:gosec // Generated fixture below t.TempDir.
				require.NoError(t, err)
				var genutilGenesis genutiltypes.GenesisState
				clientCtx.Codec.MustUnmarshalJSON(state[genutiltypes.ModuleName], &genutilGenesis)
				require.Len(t, genutilGenesis.GenTxs, 2)
				require.JSONEq(t, string(gentxBytes), string(genutilGenesis.GenTxs[i]))
				transaction, err := clientCtx.TxConfig.TxJSONDecoder()(gentxBytes)
				require.NoError(t, err)
				signedTx := transaction.(authsigning.Tx)
				require.NoError(t, signedTx.ValidateBasic())
				require.Len(t, signedTx.GetMsgs(), 1)
				createValidator := signedTx.GetMsgs()[0].(*stakingtypes.MsgCreateValidator)
				require.Equal(t, nodeName, createValidator.Description.Moniker)
				require.Equal(t, sdk.NewCoin(sdk.DefaultBondDenom, sdk.TokensFromConsensusPower(100, sdk.DefaultPowerReduction)), createValidator.Value)
				signatures, err := signedTx.GetSignaturesV2()
				require.NoError(t, err)
				require.Len(t, signatures, 1)
				signature := signatures[0]
				require.Zero(t, signature.Sequence)
				signer := sdk.AccAddress(signature.PubKey.Address())
				require.Contains(t, accountAddresses, signer.String())
				require.Equal(t, sdk.ValAddress(signer).String(), createValidator.ValidatorAddress)
				signatureData := signature.Data.(*signing.SingleSignatureData)
				signBytes, err := authsigning.GetSignBytesAdapter(
					t.Context(), clientCtx.TxConfig.SignModeHandler(), signatureData.SignMode,
					authsigning.SignerData{ChainID: genesis.ChainID, Address: signer.String(), PubKey: signature.PubKey}, transaction,
				)
				require.NoError(t, err)
				require.True(t, signature.PubKey.VerifySignature(signBytes, signatureData.Signature), "gentx signature must bind its chain and validator message")

				seedFile := filepath.Join(nodeDir, "key_seed.json")
				seedBytes, err := os.ReadFile(seedFile) //nolint:gosec // Generated fixture below t.TempDir.
				require.NoError(t, err)
				var seed struct{ Secret string }
				require.NoError(t, json.Unmarshal(seedBytes, &seed))
				require.Len(t, strings.Fields(seed.Secret), 24)
				recovered := keyring.NewInMemory(clientCtx.Codec)
				record, err := recovered.NewAccount(nodeName, seed.Secret, "", sdk.FullFundraiserPath, hd.Secp256k1)
				require.NoError(t, err)
				recoveredAddress, err := record.GetAddress()
				require.NoError(t, err)
				require.Equal(t, signer, recoveredAddress, "saved recovery seed must recover the funded gentx signer")
				for _, privateFile := range []string{seedFile, gentxFile} {
					info, err := os.Stat(privateFile)
					require.NoError(t, err)
					require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
				}

				pv := privval.LoadFilePV(filepath.Join(nodeDir, "config", "priv_validator_key.json"), filepath.Join(nodeDir, "data", "priv_validator_state.json"))
				publicKey, err := pv.GetPubKey()
				require.NoError(t, err)
				require.Equal(t, publicKey.Bytes(), createValidator.Pubkey.GetCachedValue().(cryptotypes.PubKey).Bytes())
				nodeKey, err := p2p.LoadNodeKey(filepath.Join(nodeDir, "config", "node_key.json"))
				require.NoError(t, err)
				offset, p2pPort := 0, 26656
				if singleHost {
					offset, p2pPort = i, 16656+i
				}
				memos[i] = fmt.Sprintf("%s@192.0.2.%d:%d", nodeKey.ID(), 10+i, p2pPort)
				require.Equal(t, memos[i], signedTx.GetMemo())
				configs[i] = readTestnetTOML(t, filepath.Join(nodeDir, "config", "config.toml"))
				require.Equal(t, nodeName, configs[i].GetString("moniker"))
				require.Equal(t, fmt.Sprintf("tcp://0.0.0.0:%d", 26657+offset), configs[i].GetString("rpc.laddr"))
				require.Equal(t, fmt.Sprintf("tcp://0.0.0.0:%d", p2pPort), configs[i].GetString("p2p.laddr"))
				require.Equal(t, 3*time.Second, configs[i].GetDuration("consensus.timeout_commit"))
				if singleHost {
					require.False(t, configs[i].GetBool("p2p.addr_book_strict"))
					require.False(t, configs[i].GetBool("p2p.pex"))
					require.True(t, configs[i].GetBool("p2p.allow_duplicate_ip"))
				}
				appConfig := readTestnetTOML(t, filepath.Join(nodeDir, "config", "app.toml"))
				require.Equal(t, uint64(5_000_000), appConfig.GetUint64("query-gas-limit"))
				require.Equal(t, "0.125stake", appConfig.GetString("minimum-gas-prices"))
				require.True(t, appConfig.GetBool("api.enable"))
				require.Equal(t, fmt.Sprintf("tcp://0.0.0.0:%d", 1317+offset), appConfig.GetString("api.address"))
				require.Equal(t, fmt.Sprintf("0.0.0.0:%d", 9090+offset), appConfig.GetString("grpc.address"))
			}
			for i, config := range configs {
				require.Equal(t, memos[1-i], config.GetString("p2p.persistent_peers"), "each node must connect to the other node, without a self-peer")
			}
		})
	}
}

func readTestnetTOML(t *testing.T, filename string) *viper.Viper {
	t.Helper()
	config := viper.New()
	config.SetConfigFile(filename)
	require.NoError(t, config.ReadInConfig())
	return config
}

func TestTestnetInitFilesRejectsInvalidInputs(t *testing.T) {
	application := testnetApplicationFixture(t)
	for _, test := range []struct {
		name  string
		flags []string
		error string
	}{
		{"invalid IP", []string{"--starting-ip-address", "bad-address"}, "non ipv4 address"},
		{"IPv6", []string{"--starting-ip-address", "2001:db8::1"}, "non ipv4 address"},
		{"unsupported backend", []string{"--keyring-backend", "unsupported"}, "unknown keyring backend"},
		{"unsupported algorithm", []string{"--key-type", "unsupported"}, "unsupported signing algo"},
	} {
		t.Run(test.name, func(t *testing.T) {
			command, _ := testnetCommandFixture(t, application)
			outputDir := filepath.Join(t.TempDir(), "network")
			command.SetArgs(append([]string{
				testnetInitFilesSubcommand, "--v", "1", "--output-dir", outputDir,
				"--keyring-backend", "test", "--node-daemon-home", "daemon",
			}, test.flags...))
			var output bytes.Buffer
			command.SetOut(&output)
			command.SetErr(&output)
			require.ErrorContains(t, command.Execute(), test.error)
			require.NotContains(t, output.String(), "Successfully initialized")
			require.NoFileExists(t, filepath.Join(outputDir, "node0", "daemon", "config", "genesis.json"))
		})
	}
	t.Run("output parent is a file", func(t *testing.T) {
		command, _ := testnetCommandFixture(t, application)
		parent := filepath.Join(t.TempDir(), "existing-file")
		require.NoError(t, os.WriteFile(parent, []byte("keep me"), 0o600))
		command.SetArgs([]string{testnetInitFilesSubcommand, "--v", "1", "--output-dir", filepath.Join(parent, "network")})
		require.Error(t, command.Execute())
		contents, err := os.ReadFile(parent) //nolint:gosec // Fixture inside t.TempDir.
		require.NoError(t, err)
		require.Equal(t, "keep me", string(contents), "failure cleanup must preserve the existing parent file")
	})
}

func TestTestnetStartRefusesToOverwriteExistingNetwork(t *testing.T) {
	// Exercise the start command's real admission check without launching nodes.
	// A pre-existing network must remain usable after the rejected invocation.
	outputDir := t.TempDir()
	chainID := "existing-network"
	chainDir := filepath.Join(outputDir, chainID)
	require.NoError(t, os.Mkdir(chainDir, 0o700))
	keyFile := filepath.Join(chainDir, "key_seed.json")
	require.NoError(t, os.WriteFile(keyFile, []byte("existing private data"), 0o600))
	command := testnetStartCmd()
	command.SetContext(context.Background())
	command.SilenceErrors = true
	command.SilenceUsage = true
	command.SetArgs([]string{"--output-dir", outputDir, "--chain-id", chainID, "--v", "1"})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	require.ErrorContains(t, command.Execute(), "directory already exists for chain-id 'existing-network'")
	require.NotContains(t, output.String(), "press the Enter Key")
	contents, err := os.ReadFile(keyFile) //nolint:gosec // Fixture inside t.TempDir.
	require.NoError(t, err)
	require.Equal(t, "existing private data", string(contents))
	info, err := os.Stat(keyFile)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
