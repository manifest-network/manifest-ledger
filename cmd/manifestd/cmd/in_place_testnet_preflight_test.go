package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	cmtdb "github.com/cometbft/cometbft-db"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/cometbft/cometbft/crypto/secp256k1"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/privval"
	cmtstate "github.com/cometbft/cometbft/state"
	cmttypes "github.com/cometbft/cometbft/types"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/version"
	gogotypes "github.com/cosmos/gogoproto/types"
)

type testnetPreflightFixture struct {
	home      string
	config    *cmtcfg.Config
	key       *privval.FilePV
	sourceKey ed25519.PrivKey
	operator  string
	state     cmtstate.State
}

func newTestnetPreflightFixture(t *testing.T) *testnetPreflightFixture {
	t.Helper()
	home := t.TempDir()
	cfg := cmtcfg.DefaultConfig().SetRoot(home)
	cfg.P2P.PexReactor = false
	require.NoError(t, os.MkdirAll(filepath.Join(home, "config"), 0700))
	require.NoError(t, os.MkdirAll(filepath.Join(home, "data", "cs.wal"), 0700))
	cmtcfg.WriteConfigFile(filepath.Join(home, "config", "config.toml"), cfg)
	require.NoError(t, os.WriteFile(filepath.Join(home, "config", "app.toml"), []byte("minimum-gas-prices = '0umfx'\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(home, "config", "client.toml"), []byte("chain-id = 'source'\nkeyring-backend = 'test'\n"), 0600))
	require.NoError(t, os.WriteFile(cfg.P2P.AddrBookFile(), []byte("{\"source\":true}"), 0600))
	require.NoError(t, os.WriteFile(cfg.Consensus.WalFile(), []byte("source consensus WAL"), 0600))
	require.NoError(t, os.WriteFile(cfg.Consensus.WalFile()+".000", []byte("source rotated WAL"), 0600))
	key := privval.NewFilePV(ed25519.GenPrivKey(), cfg.PrivValidatorKeyFile(), cfg.PrivValidatorStateFile())
	key.Save()
	sourceKey := ed25519.GenPrivKey()
	genesis := &cmttypes.GenesisDoc{ChainID: "source", InitialHeight: 1, ConsensusParams: cmttypes.DefaultConsensusParams(), Validators: []cmttypes.GenesisValidator{{Address: sourceKey.PubKey().Address(), PubKey: sourceKey.PubKey(), Power: 1}}}
	require.NoError(t, genesis.SaveAs(cfg.GenesisFile()))
	state, err := cmtstate.MakeGenesisState(genesis)
	require.NoError(t, err)
	state.LastBlockHeight = 3
	state.LastValidators = state.Validators.Copy()
	info := storetypes.CommitInfo{Version: 3, StoreInfos: []storetypes.StoreInfo{{Name: "bank", CommitId: storetypes.CommitID{Version: 3, Hash: bytes.Repeat([]byte{1}, 32)}}}}
	state.AppHash = info.Hash()
	db, err := cmtdb.NewGoLevelDB("application", filepath.Join(home, "data"))
	require.NoError(t, err)
	latest, err := gogotypes.StdInt64Marshal(3)
	require.NoError(t, err)
	require.NoError(t, db.SetSync([]byte("s/latest"), latest))
	commit, err := info.Marshal()
	require.NoError(t, err)
	require.NoError(t, db.SetSync([]byte("s/3"), commit))
	require.NoError(t, db.Close())
	f := &testnetPreflightFixture{home: home, config: cfg, key: key, sourceKey: sourceKey, operator: sdk.AccAddress(bytes.Repeat([]byte{7}, 20)).String(), state: state}
	f.saveState(t)
	t.Setenv("POA_ADMIN_ADDRESS", f.operator)
	t.Setenv(poaSimulationBypass, "")
	return f
}

func (f *testnetPreflightFixture) saveState(t *testing.T) {
	t.Helper()
	db, err := cmtdb.NewGoLevelDB("state", f.config.DBDir())
	require.NoError(t, err)
	proto, err := f.state.ToProto()
	require.NoError(t, err)
	bz, err := proto.Marshal()
	require.NoError(t, err)
	require.NoError(t, db.SetSync([]byte("stateKey"), bz))
	genesis, err := os.ReadFile(f.config.GenesisFile())
	require.NoError(t, err)
	require.NoError(t, db.SetSync([]byte("genesisDoc"), genesis))
	require.NoError(t, db.Close())
}

func (f *testnetPreflightFixture) command(t *testing.T, name string) *cobra.Command {
	t.Helper()
	cmd := server.InPlaceTestnetCreator(nil)
	cmd.Use = name
	if cmd.Flags().Lookup(flags.FlagHome) == nil {
		cmd.Flags().String(flags.FlagHome, f.home, "")
	}
	require.NoError(t, cmd.Flags().Set(flags.FlagHome, f.home))
	cmd.SetContext(context.Background())
	return cmd
}

func snapshotTestnetFiles(t *testing.T, home string) map[string][32]byte {
	t.Helper()
	result := map[string][32]byte{}
	require.NoError(t, filepath.WalkDir(home, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			result[path] = sha256.Sum256([]byte("directory"))
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			result[path] = sha256.Sum256([]byte("symlink:" + target))
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[path] = sha256.Sum256(data)
		return nil
	}))
	return result
}

func TestTestnetCommandPreflightRejectsWithoutWrites(t *testing.T) {
	cases := []struct {
		name, message string
		change        func(*testing.T, *testnetPreflightFixture, *cobra.Command, []string)
	}{
		{"same chain", "different from source chain", func(_ *testing.T, _ *testnetPreflightFixture, _ *cobra.Command, args []string) { args[0] = "source" }},
		{"source key", "reuses a source", func(_ *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			privval.NewFilePV(f.sourceKey, f.config.PrivValidatorKeyFile(), f.config.PrivValidatorStateFile()).Save()
		}},
		{"next source key", "reuses a source", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			f.state.NextValidators = cmttypes.NewValidatorSet([]*cmttypes.Validator{cmttypes.NewValidator(f.key.Key.PubKey, 1)})
			f.saveState(t)
		}},
		{"last source key", "reuses a source", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			f.state.LastValidators = cmttypes.NewValidatorSet([]*cmttypes.Validator{cmttypes.NewValidator(f.key.Key.PubKey, 1)})
			f.saveState(t)
		}},
		{"key type", "complete ed25519", func(_ *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			privval.NewFilePV(secp256k1.GenPrivKey(), f.config.PrivValidatorKeyFile(), f.config.PrivValidatorStateFile()).Save()
		}},
		{"disallowed key", "not allowed", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			f.state.ConsensusParams.Validator.PubKeyTypes = []string{"secp256k1"}
			f.saveState(t)
		}},
		{"signed source", "must be reset", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			require.NoError(t, os.WriteFile(f.config.PrivValidatorStateFile(), []byte(`{"height":"3","round":0,"step":2}`), 0600))
		}},
		{"empty signing state", "explicit non-null", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			require.NoError(t, os.WriteFile(f.config.PrivValidatorStateFile(), []byte(`{}`), 0600))
		}},
		{"null signing height", "explicit non-null", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			require.NoError(t, os.WriteFile(f.config.PrivValidatorStateFile(), []byte(`{"height":null,"round":0,"step":0}`), 0600))
		}},
		{"external address book", "inside the fork home", func(_ *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			f.config.P2P.AddrBook = filepath.Join(filepath.Dir(f.home), "external-addrbook.json")
		}},
		{"genesis collision", "collides", func(_ *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			f.config.P2P.AddrBook = f.config.GenesisFile()
		}},
		{"database collision", "collides", func(_ *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			f.config.P2P.AddrBook = filepath.Join(f.home, "data", "application.db", "extra")
		}},
		{"WAL rotation collision", "WAL rotation", func(_ *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			f.config.P2P.AddrBook = f.config.Consensus.WalFile() + ".123"
		}},
		{"external database", "database directory", func(_ *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			f.config.DBPath = filepath.Dir(f.home)
		}},
		{"external trace", "inside the fork home", func(t *testing.T, f *testnetPreflightFixture, cmd *cobra.Command, _ []string) {
			require.NoError(t, cmd.Flags().Set("trace-store", filepath.Join(filepath.Dir(f.home), "trace.log")))
		}},
		{"PEX", "fork isolation requires", func(_ *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			f.config.P2P.PexReactor = true
		}},
		{"peers", "fork isolation requires", func(_ *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			f.config.P2P.PersistentPeers = "source@127.0.0.1:26656"
		}},
		{"seeds", "fork isolation requires", func(_ *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			f.config.P2P.Seeds = "source@127.0.0.1:26656"
		}},
		{"state sync", "fork isolation requires", func(_ *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			f.config.StateSync.Enable = true
		}},
		{"external signer", "local validator key", func(_ *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			f.config.PrivValidatorListenAddr = "tcp://127.0.0.1:1234"
		}},
		{"wrong authority", "does not match", func(t *testing.T, _ *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			t.Setenv("POA_ADMIN_ADDRESS", sdk.AccAddress(bytes.Repeat([]byte{8}, 20)).String())
		}},
		{"missing authority", "decode configured POA admin", func(t *testing.T, _ *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			t.Setenv("POA_ADMIN_ADDRESS", "")
		}},
		{"unknown trigger", "not registered", func(t *testing.T, _ *testnetPreflightFixture, cmd *cobra.Command, _ []string) {
			require.NoError(t, cmd.Flags().Set(server.KeyTriggerTestnetUpgrade, "no-such-handler"))
		}},
		{"skip trigger", "unsafe-skip-upgrades", func(t *testing.T, _ *testnetPreflightFixture, cmd *cobra.Command, _ []string) {
			if version.Version == "" {
				t.Skip("unversioned binary")
			}
			require.NoError(t, cmd.Flags().Set(server.KeyTriggerTestnetUpgrade, version.Version))
			require.NoError(t, cmd.Flags().Set(server.FlagUnsafeSkipUpgrades, "4"))
		}},
		{"missing client config", "existing client.toml", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			require.NoError(t, os.Remove(filepath.Join(f.home, "config", "client.toml")))
		}},
		{"missing DB lock", "existing LOCK", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			require.NoError(t, os.Remove(filepath.Join(f.home, "data", "state.db", "LOCK")))
		}},
		{"home symlink", "without symlinks", func(t *testing.T, f *testnetPreflightFixture, cmd *cobra.Command, _ []string) {
			alias := filepath.Join(t.TempDir(), "alias")
			require.NoError(t, os.Symlink(f.home, alias))
			require.NoError(t, cmd.Flags().Set(flags.FlagHome, alias))
		}},
		{"Wasm symlink", "contains symlink", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(f.home, "wasm")))
		}},
		{"hard link", "hard-linked", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			require.NoError(t, os.Link(f.config.GenesisFile(), filepath.Join(t.TempDir(), "genesis.json")))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newTestnetPreflightFixture(t)
			cmd := f.command(t, "in-place-testnet")
			args := []string{"fork", f.operator}
			tc.change(t, f, cmd, args)
			cmtcfg.WriteConfigFile(filepath.Join(f.home, "config", "config.toml"), f.config)
			before := snapshotTestnetFiles(t, f.home)
			err := preflightTestnetCommand(cmd, args)
			require.ErrorContains(t, err, tc.message)
			require.Equal(t, before, snapshotTestnetFiles(t, f.home))
			require.Nil(t, cmd.Context().Value(testnetPreflightContextKey{}))
		})
	}
}

func TestTestnetCommandPreflightAndJournalRestart(t *testing.T) {
	f := newTestnetPreflightFixture(t)
	before := snapshotTestnetFiles(t, f.home)
	cmd := f.command(t, "in-place-testnet")
	require.NoError(t, preflightTestnetCommand(cmd, []string{"fork", f.operator}))
	require.Equal(t, before, snapshotTestnetFiles(t, f.home))
	preflight := cmd.Context().Value(testnetPreflightContextKey{}).(*testnetPreflight)
	require.NoError(t, writeTestnetJournal(f.home, preflight.journal, true))
	require.ErrorContains(t, preflightTestnetCommand(f.command(t, "start"), nil), "incomplete")
	journal := preflight.journal
	journal.Complete = true
	journal.FirstCommitHeight = 4
	require.NoError(t, writeTestnetJournal(f.home, journal, false))
	require.ErrorContains(t, preflightTestnetCommand(f.command(t, "start"), nil), "disagrees")

	f.state.ChainID = "fork"
	f.state.LastBlockHeight = 4
	f.state.Validators = cmttypes.NewValidatorSet([]*cmttypes.Validator{cmttypes.NewValidator(f.key.Key.PubKey, 1)})
	genesis := cmttypes.GenesisDoc{ChainID: "fork", InitialHeight: 1, ConsensusParams: cmttypes.DefaultConsensusParams()}
	require.NoError(t, genesis.SaveAs(f.config.GenesisFile()))
	f.saveState(t)
	// A completed app journal alone must never make a partial cross-DB conversion restartable.
	require.ErrorContains(t, preflightTestnetCommand(f.command(t, "start"), nil), "inconsistent")
	info := storetypes.CommitInfo{Version: 4, StoreInfos: []storetypes.StoreInfo{{Name: "bank", CommitId: storetypes.CommitID{Version: 4, Hash: bytes.Repeat([]byte{2}, 32)}}}}
	db, err := cmtdb.NewGoLevelDB("application", filepath.Join(f.home, "data"))
	require.NoError(t, err)
	latest, err := gogotypes.StdInt64Marshal(4)
	require.NoError(t, err)
	require.NoError(t, db.SetSync([]byte("s/latest"), latest))
	bz, err := info.Marshal()
	require.NoError(t, err)
	require.NoError(t, db.SetSync([]byte("s/4"), bz))
	require.NoError(t, db.Close())
	f.state.AppHash = info.Hash()
	f.saveState(t)
	before = snapshotTestnetFiles(t, f.home)
	require.NoError(t, preflightTestnetCommand(f.command(t, "start"), nil))
	require.Equal(t, before, snapshotTestnetFiles(t, f.home))
	t.Setenv("POA_ADMIN_ADDRESS", "")
	require.ErrorContains(t, preflightTestnetCommand(f.command(t, "start"), nil), "POA_ADMIN_ADDRESS=")
	require.Equal(t, before, snapshotTestnetFiles(t, f.home))
}

func TestTestnetCosmovisorBinaryLink(t *testing.T) {
	f := newTestnetPreflightFixture(t)
	dir := filepath.Join(f.home, "cosmovisor", "genesis", "bin")
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.Symlink("genesis", filepath.Join(f.home, "cosmovisor", "current")))
	require.NoError(t, preflightTestnetCommand(f.command(t, "in-place-testnet"), []string{"fork", f.operator}))
	f.config.P2P.AddrBook = filepath.Join(f.home, "cosmovisor", "current", "addrbook.json")
	cmtcfg.WriteConfigFile(filepath.Join(f.home, "config", "config.toml"), f.config)
	require.ErrorContains(t, preflightTestnetCommand(f.command(t, "in-place-testnet"), []string{"fork", f.operator}), "traverses a symlink")
}

func TestTestnetBypassRejectedBeforeRootConstruction(t *testing.T) {
	t.Setenv(poaSimulationBypass, "not_for-production")
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"start", "--home", t.TempDir()})
	require.ErrorContains(t, cmd.Execute(), poaSimulationBypass+" must be unset")
	for _, name := range []string{"export", "snapshots", "init"} {
		cmd.SetArgs([]string{name})
		require.ErrorContains(t, cmd.Execute(), poaSimulationBypass+" must be unset")
	}
}

type testnetCommitRecorder struct {
	servertypes.Application
	height  int64
	commits int
	err     error
}

func (app *testnetCommitRecorder) Commit() (*abci.ResponseCommit, error) {
	app.commits++
	return &abci.ResponseCommit{}, app.err
}
func (app *testnetCommitRecorder) Info(*abci.RequestInfo) (*abci.ResponseInfo, error) {
	return &abci.ResponseInfo{LastBlockHeight: app.height}, nil
}

func TestTestnetJournalCommitAndConfirmation(t *testing.T) {
	f := newTestnetPreflightFixture(t)
	cmd := f.command(t, "in-place-testnet")
	require.NoError(t, preflightTestnetCommand(cmd, []string{"fork", f.operator}))
	preflight := cmd.Context().Value(testnetPreflightContextKey{}).(*testnetPreflight)
	root := &cobra.Command{Use: "manifestd"}
	root.AddCommand(cmd)
	serverContext := server.NewDefaultContext()
	serverContext.Config = f.config
	serverContext.Viper = preflight.options
	cmd.SetContext(context.WithValue(cmd.Context(), server.ServerContextKey, serverContext))
	called := false
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		called = true
		journal, err := readTestnetJournal(f.home)
		require.NoError(t, err)
		require.False(t, journal.Complete)
		return nil
	}
	protectInPlaceTestnetCommand(root)
	before := snapshotTestnetFiles(t, f.home)
	cmd.SetIn(strings.NewReader("no\n"))
	require.NoError(t, cmd.RunE(cmd, []string{"fork", f.operator}))
	require.False(t, called)
	require.Equal(t, before, snapshotTestnetFiles(t, f.home))
	cmd.SetIn(strings.NewReader("yes\n"))
	require.NoError(t, cmd.RunE(cmd, []string{"fork", f.operator}))
	require.True(t, called)
	fake := &testnetCommitRecorder{height: 4, err: fmt.Errorf("commit failed")}
	app := &journaledTestnetApp{Application: fake, home: f.home, journal: preflight.journal}
	_, err := app.Commit()
	require.ErrorContains(t, err, "commit failed")
	journal, err := readTestnetJournal(f.home)
	require.NoError(t, err)
	require.False(t, journal.Complete)
	fake.err = nil
	_, err = app.Commit()
	require.NoError(t, err)
	journal, err = readTestnetJournal(f.home)
	require.NoError(t, err)
	require.True(t, journal.Complete)
	require.EqualValues(t, 4, journal.FirstCommitHeight)
	_, err = app.Commit()
	require.NoError(t, err)
	require.Equal(t, 3, fake.commits)
}

func TestTestnetHomeMutationPaths(t *testing.T) {
	f := newTestnetPreflightFixture(t)
	v := viper.New()
	v.Set("with-comet", true)
	f.config.P2P.AddrBook = filepath.Join(f.home, "custom", "nested", "addrbook.json")
	require.NoError(t, validateTestnetHome(f.home, f.config, v))
	for _, target := range []string{"cpu-profile", "trace-store"} {
		v.Set(target, filepath.Join(f.home, target+".log"))
		require.NoError(t, validateTestnetHome(f.home, f.config, v))
		v.Set(target, f.config.GenesisFile())
		require.ErrorContains(t, validateTestnetHome(f.home, f.config, v), "collides")
		v.Set(target, "")
	}
	v.Set("streaming.abci.plugin", "external-plugin")
	require.ErrorContains(t, validateTestnetHome(f.home, f.config, v), "streaming must be disabled")
}

func TestTestnetSigningStateAndJournalMalformed(t *testing.T) {
	f := newTestnetPreflightFixture(t)
	for _, data := range []string{"{", `{"height":"0","round":0,"step":0,"signature":"AQ=="}`} {
		require.NoError(t, os.WriteFile(f.config.PrivValidatorStateFile(), []byte(data), 0600))
		require.Error(t, validateTestnetSigningState(f.config.PrivValidatorStateFile()))
	}
	require.NoError(t, os.WriteFile(filepath.Join(f.home, inPlaceTestnetMarker), []byte(`{}`), 0600))
	_, err := readTestnetJournal(f.home)
	require.ErrorContains(t, err, "invalid")
	keyJSON, err := cmtjson.Marshal(f.key.Key)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(keyJSON, &fields))
	delete(fields, "pub_key")
	keyJSON, err = json.Marshal(fields)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(f.config.PrivValidatorKeyFile(), keyJSON, 0600))
	_, err = readTestnetKey(f.config.PrivValidatorKeyFile())
	require.ErrorContains(t, err, "complete ed25519")
}

func TestTestnetConfigCannotOverrideDefaultHome(t *testing.T) {
	for _, name := range []string{"config.toml", "app.toml"} {
		t.Run(name, func(t *testing.T) {
			f := newTestnetPreflightFixture(t)
			cmd := f.command(t, "in-place-testnet")
			cmd.Flags().Lookup(flags.FlagHome).Changed = false
			path := filepath.Join(f.home, "config", name)
			original, err := os.ReadFile(path)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, append([]byte("home = '/outside-fork-home'\n"), original...), 0600))
			before := snapshotTestnetFiles(t, f.home)
			require.ErrorContains(t, preflightTestnetCommand(cmd, []string{"fork", f.operator}), "overrides the selected fork home")
			require.Equal(t, before, snapshotTestnetFiles(t, f.home))
		})
	}
}

func TestTestnetApplicationCommandsProtected(t *testing.T) {
	for _, name := range []string{"start", "export", "prune", "rollback", "bootstrap-state", "module-hash-by-height"} {
		t.Run(name, func(t *testing.T) {
			f := newTestnetPreflightFixture(t)
			journal := testnetJournal{Version: 1, SourceChainID: "source", ChainID: "fork", Operator: f.operator, ConsensusAddress: f.key.Key.Address, SourceHeight: 3}
			require.NoError(t, writeTestnetJournal(f.home, journal, true))
			before := snapshotTestnetFiles(t, f.home)
			require.ErrorContains(t, preflightTestnetCommand(f.command(t, name), nil), "incomplete")
			require.Equal(t, before, snapshotTestnetFiles(t, f.home))
		})
	}
	f := newTestnetPreflightFixture(t)
	parent := &cobra.Command{Use: "snapshots"}
	child := f.command(t, "restore")
	parent.AddCommand(child)
	require.True(t, testnetAppCommand(child))
}

func TestTestnetSDKEnvironmentBindingsCannotEscapeHome(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)
	prefix := strings.NewReplacer(".", "_", "-", "_").Replace(filepath.Base(exe))
	for _, flag := range []string{"trace-store", "cpu-profile", flags.FlagHome} {
		t.Run(flag, func(t *testing.T) {
			f := newTestnetPreflightFixture(t)
			cmd := f.command(t, "in-place-testnet")
			cmd.Flags().Lookup(flags.FlagHome).Changed = false
			t.Setenv(prefix+"_"+strings.ToUpper(strings.ReplaceAll(flag, "-", "_")), filepath.Join(filepath.Dir(f.home), "outside"))
			before := snapshotTestnetFiles(t, f.home)
			err := preflightTestnetCommand(cmd, []string{"fork", f.operator})
			if flag == flags.FlagHome {
				require.ErrorContains(t, err, "overrides the selected fork home")
			} else {
				require.ErrorContains(t, err, "inside the fork home")
			}
			require.Equal(t, before, snapshotTestnetFiles(t, f.home))
		})
	}
}

func TestTestnetRestartRedirectCannotSkipJournal(t *testing.T) {
	for _, source := range []string{"environment", "config.toml", "app.toml"} {
		t.Run(source, func(t *testing.T) {
			ordinary := newTestnetPreflightFixture(t)
			fork := newTestnetPreflightFixture(t)
			journal := testnetJournal{Version: 1, SourceChainID: "source", ChainID: "fork", Operator: fork.operator, ConsensusAddress: fork.key.Key.Address, SourceHeight: 3}
			require.NoError(t, writeTestnetJournal(fork.home, journal, true))
			cmd := ordinary.command(t, "start")
			cmd.Flags().Lookup(flags.FlagHome).Changed = false
			if source == "environment" {
				exe, err := os.Executable()
				require.NoError(t, err)
				prefix := strings.NewReplacer(".", "_", "-", "_").Replace(filepath.Base(exe))
				t.Setenv(prefix+"_HOME", fork.home)
			} else {
				path := filepath.Join(ordinary.home, "config", source)
				config, err := os.ReadFile(path)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(path, append([]byte(fmt.Sprintf("home = %q\n", fork.home)), config...), 0600))
			}
			before := snapshotTestnetFiles(t, fork.home)
			ordinaryBefore := snapshotTestnetFiles(t, ordinary.home)
			require.ErrorContains(t, preflightTestnetCommand(cmd, nil), "overrides the selected fork home")
			require.Equal(t, before, snapshotTestnetFiles(t, fork.home))
			require.Equal(t, ordinaryBefore, snapshotTestnetFiles(t, ordinary.home))
		})
	}
}

func TestTestnetVerifiedChainIDOverridesMergedGenesis(t *testing.T) {
	f := newTestnetPreflightFixture(t)
	other := cmttypes.GenesisDoc{ChainID: "other-source", InitialHeight: 1, ConsensusParams: cmttypes.DefaultConsensusParams()}
	require.NoError(t, other.SaveAs(filepath.Join(f.home, "config", "other-genesis.json")))
	require.NoError(t, os.WriteFile(filepath.Join(f.home, "config", "app.toml"), []byte("genesis_file = 'config/other-genesis.json'\n"), 0600))
	cmd := f.command(t, "in-place-testnet")
	require.NoError(t, preflightTestnetCommand(cmd, []string{"fork", f.operator}))
	preflight := cmd.Context().Value(testnetPreflightContextKey{}).(*testnetPreflight)
	serverContext, err := server.InterceptConfigsAndCreateContext(cmd, "", nil, initCometBFTConfig())
	require.NoError(t, err)
	cmd.SetContext(context.WithValue(cmd.Context(), server.ServerContextKey, serverContext))
	require.Empty(t, serverContext.Viper.GetString(flags.FlagChainID))
	require.Equal(t, "config/other-genesis.json", serverContext.Viper.GetString("genesis_file"))
	before := snapshotTestnetFiles(t, f.home)
	require.NoError(t, configureTestnetServerContext(cmd))
	require.Equal(t, before, snapshotTestnetFiles(t, f.home))
	application := baseapp.NewBaseApp("fork-identity-test", log.NewNopLogger(), dbm.NewMemDB(), nil, server.DefaultBaseappOptions(serverContext.Viper)...)
	require.Equal(t, preflight.journal.ChainID, application.ChainID())
	require.Equal(t, "fork", application.ChainID())
	require.NoError(t, application.Close())
}

func TestTestnetJournalUpdateFailureRemainsIncomplete(t *testing.T) {
	f := newTestnetPreflightFixture(t)
	cmd := f.command(t, "in-place-testnet")
	require.NoError(t, preflightTestnetCommand(cmd, []string{"fork", f.operator}))
	preflight := cmd.Context().Value(testnetPreflightContextKey{}).(*testnetPreflight)
	require.NoError(t, writeTestnetJournal(f.home, preflight.journal, true))
	require.NoError(t, os.WriteFile(filepath.Join(f.home, inPlaceTestnetMarker+".next"), []byte("interrupted journal update"), 0600))
	application := &journaledTestnetApp{Application: &testnetCommitRecorder{height: 4}, home: f.home, journal: preflight.journal}
	for attempt := 0; attempt < 2; attempt++ {
		_, err := application.Commit()
		require.ErrorContains(t, err, "complete conversion journal")
		require.False(t, application.journal.Complete)
		persisted, err := readTestnetJournal(f.home)
		require.NoError(t, err)
		require.False(t, persisted.Complete)
	}
}

func TestTestnetIncompleteJournalPrecedesMixedStateDecoding(t *testing.T) {
	f := newTestnetPreflightFixture(t)
	journal := testnetJournal{Version: 1, SourceChainID: "source", ChainID: "fork", Operator: f.operator, ConsensusAddress: f.key.Key.Address, SourceHeight: 3}
	require.NoError(t, writeTestnetJournal(f.home, journal, true))
	genesis := cmttypes.GenesisDoc{ChainID: "fork", InitialHeight: 1, ConsensusParams: cmttypes.DefaultConsensusParams()}
	require.NoError(t, genesis.SaveAs(f.config.GenesisFile()))
	before := snapshotTestnetFiles(t, f.home)
	require.ErrorContains(t, preflightTestnetCommand(f.command(t, "start"), nil), "conversion is incomplete; create a fresh disposable copy")
	require.Equal(t, before, snapshotTestnetFiles(t, f.home))
}
