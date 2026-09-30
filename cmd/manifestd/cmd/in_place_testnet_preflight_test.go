package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	cmtstoreproto "github.com/cometbft/cometbft/proto/tendermint/store"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmtstate "github.com/cometbft/cometbft/state"
	cmtstore "github.com/cometbft/cometbft/store"
	cmttypes "github.com/cometbft/cometbft/types"

	dbm "github.com/cosmos/cosmos-db"
	gogotypes "github.com/cosmos/gogoproto/types"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/version"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
)

type testnetPreflightFixture struct {
	home      string
	config    *cmtcfg.Config
	key       *privval.FilePV
	sourceKey ed25519.PrivKey
	operator  string
	state     cmtstate.State
	blockIDs  map[int64]cmttypes.BlockID
}

func testnetPreflightCommitInfo(height int64) storetypes.CommitInfo {
	return storetypes.CommitInfo{Version: height, StoreInfos: []storetypes.StoreInfo{{Name: "bank", CommitId: storetypes.CommitID{Version: height, Hash: bytes.Repeat([]byte{byte(height)}, 32)}}}}
}

func newTestnetPreflightFixture(t *testing.T) *testnetPreflightFixture {
	t.Helper()
	// Fork homes must not traverse symlinks; macOS temporary directories do.
	home, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	cfg := cmtcfg.DefaultConfig().SetRoot(home)
	cfg.P2P.PexReactor = false
	// Conversion probes its listeners, so avoid ports a local node may hold.
	cfg.RPC.ListenAddress = "tcp://127.0.0.1:0"
	cfg.P2P.ListenAddress = "tcp://127.0.0.1:0"
	require.NoError(t, os.MkdirAll(filepath.Join(home, "config"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(home, "data", "cs.wal"), 0o700))
	cmtcfg.WriteConfigFile(filepath.Join(home, "config", "config.toml"), cfg)
	require.NoError(t, os.WriteFile(filepath.Join(home, "config", "app.toml"), []byte("minimum-gas-prices = '0umfx'\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(home, "config", "client.toml"), []byte("chain-id = 'source'\nkeyring-backend = 'test'\n"), 0o600))
	require.NoError(t, os.WriteFile(cfg.P2P.AddrBookFile(), []byte("{\"source\":true}"), 0o600))
	require.NoError(t, os.WriteFile(cfg.Consensus.WalFile(), []byte("source consensus WAL"), 0o600))
	require.NoError(t, os.WriteFile(cfg.Consensus.WalFile()+".000", []byte("source rotated WAL"), 0o600))
	key := privval.NewFilePV(ed25519.GenPrivKey(), cfg.PrivValidatorKeyFile(), cfg.PrivValidatorStateFile())
	key.Save()
	sourceKey := ed25519.GenPrivKey()
	genesis := &cmttypes.GenesisDoc{ChainID: "source", InitialHeight: 1, ConsensusParams: cmttypes.DefaultConsensusParams(), Validators: []cmttypes.GenesisValidator{{Address: sourceKey.PubKey().Address(), PubKey: sourceKey.PubKey(), Power: 1}}}
	require.NoError(t, genesis.SaveAs(cfg.GenesisFile()))
	state, err := cmtstate.MakeGenesisState(genesis)
	require.NoError(t, err)
	state.LastBlockHeight = 3
	state.LastValidators = state.Validators.Copy()
	info := testnetPreflightCommitInfo(3)
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
	f := &testnetPreflightFixture{home: home, config: cfg, key: key, sourceKey: sourceKey, operator: sdk.AccAddress(bytes.Repeat([]byte{7}, 20)).String(), state: state, blockIDs: map[int64]cmttypes.BlockID{}}
	f.saveBlocks(t, 3)
	f.state.LastBlockID = f.blockIDs[3]
	f.saveState(t)
	f.saveResponse(t, 3, &abci.ResponseFinalizeBlock{AppHash: info.Hash(), TxResults: []*abci.ExecTxResult{{Code: 0}}})
	t.Setenv("POA_ADMIN_ADDRESS", f.operator)
	t.Setenv(poaSimulationEnvVar, "")
	return f
}

func (f *testnetPreflightFixture) saveBlocks(t *testing.T, height int64) {
	t.Helper()
	db, err := cmtdb.NewGoLevelDB("blockstore", f.config.DBDir())
	require.NoError(t, err)
	store := cmtstore.NewBlockStore(db)
	lastCommit := &cmttypes.Commit{}
	if store.Height() > 0 {
		lastCommit = store.LoadSeenCommit(store.Height())
	}
	for h := store.Height() + 1; h <= height; h++ {
		block := cmttypes.MakeBlock(h, []cmttypes.Tx{[]byte("source transaction")}, lastCommit, nil)
		block.Populate(f.state.Version.Consensus, f.state.ChainID, time.Unix(1700000000+h, 0), f.blockIDs[h-1],
			f.state.Validators.Hash(), f.state.NextValidators.Hash(), f.state.ConsensusParams.Hash(),
			testnetPreflightCommitInfo(h-1).Hash(), nil, f.sourceKey.PubKey().Address())
		parts, err := block.MakePartSet(cmttypes.BlockPartSizeBytes)
		require.NoError(t, err)
		id := cmttypes.BlockID{Hash: block.Hash(), PartSetHeader: parts.Header()}
		vote := cmttypes.Vote{Type: cmtproto.PrecommitType, Height: h, BlockID: id, Timestamp: block.Time, ValidatorAddress: f.sourceKey.PubKey().Address()}
		vote.Signature, err = f.sourceKey.Sign(cmttypes.VoteSignBytes(f.state.ChainID, vote.ToProto()))
		require.NoError(t, err)
		commit := &cmttypes.Commit{Height: h, BlockID: id, Signatures: []cmttypes.CommitSig{vote.CommitSig()}}
		store.SaveBlock(block, parts, commit)
		f.blockIDs[h] = id
		lastCommit = commit
	}
	require.NoError(t, db.Close())
}

func (f *testnetPreflightFixture) saveResponse(t *testing.T, height int64, response *abci.ResponseFinalizeBlock) {
	t.Helper()
	db, err := cmtdb.NewGoLevelDB("state", f.config.DBDir())
	require.NoError(t, err)
	require.NoError(t, cmtstate.NewStore(db, cmtstate.StoreOptions{}).SaveFinalizeBlockResponse(height, response))
	require.NoError(t, db.Close())
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

func (f *testnetPreflightFixture) appendAppConfig(t *testing.T, content string) {
	t.Helper()
	file, err := os.OpenFile(filepath.Join(f.home, "config", "app.toml"), os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = file.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, file.Close())
}

func (f *testnetPreflightFixture) saveBlockStore(t *testing.T, height int64) {
	t.Helper()
	db, err := cmtdb.NewGoLevelDB("blockstore", f.config.DBDir())
	require.NoError(t, err)
	cmtstore.SaveBlockStoreState(&cmtstoreproto.BlockStoreState{Base: 1, Height: height}, db)
	require.NoError(t, db.Close())
}

func (f *testnetPreflightFixture) completeFork(t *testing.T) {
	t.Helper()
	f.state.ChainID = "fork"
	f.state.Validators = cmttypes.NewValidatorSet([]*cmttypes.Validator{cmttypes.NewValidator(f.key.Key.PubKey, 1)})
	f.state.LastValidators = f.state.Validators.Copy()
	f.state.NextValidators = f.state.Validators.Copy()
	genesis := cmttypes.GenesisDoc{ChainID: "fork", InitialHeight: 1, ConsensusParams: cmttypes.DefaultConsensusParams()}
	require.NoError(t, genesis.SaveAs(f.config.GenesisFile()))
	f.saveState(t)
	journal := testnetJournal{
		Version: 1, Complete: true, SourceChainID: "source", ChainID: "fork", Operator: f.operator,
		ConsensusAddress: f.key.Key.Address, SourceHeight: 2, FirstCommitHeight: 3,
	}
	require.NoError(t, writeTestnetJournal(f.home, journal, true))
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
			require.NoError(t, os.WriteFile(f.config.PrivValidatorStateFile(), []byte(`{"height":"3","round":0,"step":2}`), 0o600))
		}},
		{"empty signing state", "explicit non-null", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			require.NoError(t, os.WriteFile(f.config.PrivValidatorStateFile(), []byte(`{}`), 0o600))
		}},
		{"null signing height", "explicit non-null", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			require.NoError(t, os.WriteFile(f.config.PrivValidatorStateFile(), []byte(`{"height":null,"round":0,"step":0}`), 0o600))
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
			previousVersion := version.Version
			version.Version = "preflight-upgrade"
			t.Cleanup(func() { version.Version = previousVersion })
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
		{"completed conversion", "journal already exists", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			f.completeFork(t)
		}},
		{"stale journal update", "stale in-place-testnet journal update", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			require.NoError(t, os.WriteFile(filepath.Join(f.home, inPlaceTestnetMarker+".next"), []byte("{}"), 0o600))
		}},
		{"halt height at first block", "halt-height 4 would stop the fork", func(t *testing.T, _ *testnetPreflightFixture, cmd *cobra.Command, _ []string) {
			require.NoError(t, cmd.Flags().Set(server.FlagHaltHeight, "4"))
		}},
		{"halt height before source", "halt-height 2 would stop the fork", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			f.appendAppConfig(t, "halt-height = 2\n")
		}},
		{"halt time", "halt-time must be unset", func(t *testing.T, _ *testnetPreflightFixture, cmd *cobra.Command, _ []string) {
			require.NoError(t, cmd.Flags().Set(server.FlagHaltTime, "1"))
		}},
		{"listener in use", "is unavailable", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			t.Cleanup(func() { _ = listener.Close() })
			f.config.RPC.ListenAddress = "tcp://127.0.0.1:0,tcp://" + listener.Addr().String()
		}},
		{"gRPC server in use", "is unavailable", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			t.Cleanup(func() { _ = listener.Close() })
			f.appendAppConfig(t, fmt.Sprintf("[grpc]\nenable = true\naddress = %q\n", listener.Addr().String()))
		}},
		{"named streaming service", "streaming must be disabled", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			f.appendAppConfig(t, "[streaming.indexer]\nplugin = \"abci\"\n")
		}},
		{"statsd telemetry", "telemetry sink", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			f.appendAppConfig(t, "[telemetry]\nenabled = true\nmetrics-sink = \"statsd\"\nstatsd-addr = \"collector.invalid:8125\"\n")
		}},
		{"shadowing Comet config", "would replace config.toml", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			require.NoError(t, os.WriteFile(filepath.Join(f.home, "config", "config.json"), []byte("priv_validator_key_file = 'config/source.json'\n"), 0o600))
		}},
		{"shadowing app config", "would replace app.toml", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			require.NoError(t, os.WriteFile(filepath.Join(f.home, "config", "app.json"), []byte("halt-height = 4\n"), 0o600))
		}},
		{"other database backend", "requires goleveldb", func(_ *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			f.config.DBBackend = "pebbledb"
		}},
		{"standalone ABCI", "requires CometBFT enabled", func(t *testing.T, _ *testnetPreflightFixture, cmd *cobra.Command, _ []string) {
			require.NoError(t, cmd.Flags().Set("with-comet", "false"))
		}},
		{"inconsistent key", "must agree", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			key := f.key.Key
			key.Address = ed25519.GenPrivKey().PubKey().Address()
			bz, err := cmtjson.Marshal(key)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(f.config.PrivValidatorKeyFile(), bz, 0o600))
		}},
		{"genesis chain ID", "genesis and persisted Comet chain IDs disagree", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			genesis := cmttypes.GenesisDoc{ChainID: "other-source", InitialHeight: 1, ConsensusParams: cmttypes.DefaultConsensusParams()}
			require.NoError(t, genesis.SaveAs(f.config.GenesisFile()))
		}},
		{"cached genesis chain ID", "cached genesis and persisted Comet chain IDs disagree", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			genesis := cmttypes.GenesisDoc{ChainID: "other-source", InitialHeight: 1, ConsensusParams: cmttypes.DefaultConsensusParams()}
			bz, err := cmtjson.Marshal(genesis)
			require.NoError(t, err)
			f.writeRecord(t, "state", "genesisDoc", bz)
		}},
		{"application commit info", "invalid persisted application commit info", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			info := testnetPreflightCommitInfo(2)
			bz, err := info.Marshal()
			require.NoError(t, err)
			f.writeRecord(t, "application", "s/3", bz)
		}},
		{"block from another chain", "disagrees with Comet state or metadata", func(t *testing.T, f *testnetPreflightFixture, _ *cobra.Command, _ []string) {
			// Every hash link is consistent; only the block's chain ID differs.
			require.NoError(t, os.RemoveAll(filepath.Join(f.config.DBDir(), "blockstore.db")))
			f.blockIDs = map[int64]cmttypes.BlockID{}
			f.state.ChainID = "other-source"
			f.saveBlocks(t, 3)
			f.state.ChainID = "source"
			f.state.LastBlockID = f.blockIDs[3]
			f.saveState(t)
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

func TestTestnetPreflightExternalServices(t *testing.T) {
	for _, command := range []string{"in-place-testnet", "start"} {
		for _, tc := range []struct {
			name, indexer, listener, message string
		}{
			{name: "local index", indexer: "kv"},
			{name: "no index", indexer: "null"},
			{name: "external index", indexer: "psql", message: "external transaction indexing is unsupported"},
			{name: "unknown index", indexer: "other", message: "tx_index.indexer to be kv or null"},
			{name: "TCP broadcast", indexer: "kv", listener: "tcp://127.0.0.1:26658"},
			{name: "unix broadcast", indexer: "kv", listener: "unix://", message: "unix socket listeners are unsupported"},
		} {
			t.Run(command+"/"+tc.name, func(t *testing.T) {
				f := newTestnetPreflightFixture(t)
				if command == startCommandName {
					f.completeFork(t)
				}
				f.config.TxIndex.Indexer = tc.indexer
				f.config.TxIndex.PsqlConn = "postgresql://source.invalid/source"
				f.config.RPC.GRPCListenAddress = tc.listener
				outside := t.TempDir()
				if tc.listener == "unix://" {
					f.config.RPC.GRPCListenAddress += filepath.Join(outside, "grpc.sock")
				}
				cmtcfg.WriteConfigFile(filepath.Join(f.home, "config", "config.toml"), f.config)
				before, outsideBefore := snapshotTestnetFiles(t, f.home), snapshotTestnetFiles(t, outside)
				err := preflightTestnetCommand(f.command(t, command), []string{"fork", f.operator})
				if tc.message == "" {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, tc.message)
				}
				require.Equal(t, before, snapshotTestnetFiles(t, f.home))
				require.Equal(t, outsideBefore, snapshotTestnetFiles(t, outside))
			})
		}
	}
}

func TestTestnetPreflightSourceHeights(t *testing.T) {
	for _, tc := range []struct {
		name        string
		cometHeight int64
		storeHeight int64
		allowed     bool
	}{
		{"matching commits", 3, 3, true},
		{"stored uncommitted halt block", 3, 4, true},
		{"application one ahead", 2, 3, true},
		{"interrupted hard rollback", 2, 2, false},
		{"application behind", 4, 4, false},
		{"application two ahead", 1, 3, false},
		{"blockstore behind", 3, 2, false},
		{"blockstore two ahead", 3, 5, false},
		{"blockstore height overflow", 3, math.MaxInt64 - 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newTestnetPreflightFixture(t)
			f.state.LastBlockHeight = tc.cometHeight
			f.state.LastBlockID = f.blockIDs[tc.cometHeight]
			f.state.AppHash = testnetPreflightCommitInfo(tc.cometHeight).Hash()
			f.saveState(t)
			if tc.allowed && tc.storeHeight > 3 {
				f.saveBlocks(t, tc.storeHeight)
			}
			f.saveBlockStore(t, tc.storeHeight)
			before := snapshotTestnetFiles(t, f.home)
			cmd := f.command(t, "in-place-testnet")
			err := preflightTestnetCommand(cmd, []string{"fork", f.operator})
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "unsupported source application/Comet/blockstore heights")
				require.Nil(t, cmd.Context().Value(testnetPreflightContextKey{}))
			}
			require.Equal(t, before, snapshotTestnetFiles(t, f.home))
			require.NoFileExists(t, filepath.Join(f.home, inPlaceTestnetMarker))
		})
	}
}

func TestTestnetRestartRejectsUnverifiedCrashRecovery(t *testing.T) {
	f := newTestnetPreflightFixture(t)
	journal := testnetJournal{ChainID: f.state.ChainID, ConsensusAddress: f.key.Key.Address, FirstCommitHeight: 2}
	require.NoError(t, validateTestnetRestart(journal, f.key.Key, &f.state, 3, f.state.AppHash))
	// A matching old hash cannot excuse the height mismatch; a new hash cannot
	// be trusted without verifying the block and saved ABCI response for it.
	for _, hash := range [][]byte{f.state.AppHash, bytes.Repeat([]byte{4}, 32)} {
		require.ErrorContains(t, validateTestnetRestart(journal, f.key.Key, &f.state, 4, hash), "create a fresh disposable copy")
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
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.Symlink("genesis", filepath.Join(f.home, "cosmovisor", "current")))
	require.NoError(t, preflightTestnetCommand(f.command(t, "in-place-testnet"), []string{"fork", f.operator}))
	f.config.P2P.AddrBook = filepath.Join(f.home, "cosmovisor", "current", "addrbook.json")
	cmtcfg.WriteConfigFile(filepath.Join(f.home, "config", "config.toml"), f.config)
	require.ErrorContains(t, preflightTestnetCommand(f.command(t, "in-place-testnet"), []string{"fork", f.operator}), "traverses a symlink")
}

// Cosmovisor writes absolute links under its original DAEMON_HOME.
func TestTestnetCosmovisorAbsoluteBinaryLink(t *testing.T) {
	f := newTestnetPreflightFixture(t)
	upgrade := filepath.Join(f.home, "cosmovisor", "upgrades", "v2.3.1")
	require.NoError(t, os.MkdirAll(filepath.Join(upgrade, "bin"), 0o700))
	link := filepath.Join(f.home, "cosmovisor", "current")
	// Copied from another DAEMON_HOME: rejected, with the re-point command.
	source := filepath.Join(t.TempDir(), "source-home")
	require.NoError(t, os.MkdirAll(filepath.Join(source, "cosmovisor", "upgrades", "v2.3.1"), 0o700))
	require.NoError(t, os.Symlink(filepath.Join(source, "cosmovisor", "upgrades", "v2.3.1"), link))
	before := snapshotTestnetFiles(t, f.home)
	err := preflightTestnetCommand(f.command(t, "in-place-testnet"), []string{"fork", f.operator})
	require.ErrorContains(t, err, "outside this copy; re-point it with: ln -sfn "+upgrade+" "+link)
	require.Equal(t, before, snapshotTestnetFiles(t, f.home))
	// Re-pointed with an absolute path inside the copy: accepted.
	require.NoError(t, os.Remove(link))
	require.NoError(t, os.Symlink(upgrade, link))
	require.NoError(t, preflightTestnetCommand(f.command(t, "in-place-testnet"), []string{"fork", f.operator}))
	// A target the copy lacks gets the generic instruction.
	require.NoError(t, os.Remove(link))
	require.NoError(t, os.Symlink(t.TempDir(), link))
	require.ErrorContains(t, preflightTestnetCommand(f.command(t, "in-place-testnet"), []string{"fork", f.operator}), "re-point it to a directory inside")
}

func TestTestnetBypassRejectedBeforeRootConstruction(t *testing.T) {
	t.Setenv(poaSimulationEnvVar, "not_for-production")
	require.ErrorContains(t, RejectSimulationAdminBypass(), poaSimulationEnvVar+" must be unset")
	cmd := NewRootCmd()
	// The caller prints the returned error; cobra must not print it first.
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"start", "--home", t.TempDir()})
	require.ErrorContains(t, cmd.Execute(), poaSimulationEnvVar+" must be unset")
	require.Empty(t, output.String())
	for _, name := range []string{"export", "snapshots", "init"} {
		cmd.SetArgs([]string{name})
		require.ErrorContains(t, cmd.Execute(), poaSimulationEnvVar+" must be unset")
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
		// The SDK still has to validate its own configuration, so nothing is
		// recorded yet; the application creator receives the checked journal.
		require.NoFileExists(t, filepath.Join(f.home, inPlaceTestnetMarker))
		require.Equal(t, preflight.journal, serverContext.Viper.Get(testnetJournalOption))
		return nil
	}
	require.NoError(t, protectInPlaceTestnetCommand(root))
	before := snapshotTestnetFiles(t, f.home)
	cmd.SetIn(strings.NewReader("no\n"))
	require.NoError(t, cmd.RunE(cmd, []string{"fork", f.operator}))
	require.False(t, called)
	require.Equal(t, before, snapshotTestnetFiles(t, f.home))
	cmd.SetIn(strings.NewReader("yes\n"))
	require.NoError(t, cmd.RunE(cmd, []string{"fork", f.operator}))
	require.True(t, called)
	require.Equal(t, before, snapshotTestnetFiles(t, f.home))
	// newJournaledTestnetApp records the journal once the application is built.
	require.NoError(t, writeTestnetJournal(f.home, preflight.journal, true))
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
		require.NoError(t, os.WriteFile(f.config.PrivValidatorStateFile(), []byte(data), 0o600))
		require.Error(t, validateTestnetSigningState(f.config.PrivValidatorStateFile()))
	}
	require.NoError(t, os.WriteFile(filepath.Join(f.home, inPlaceTestnetMarker), []byte(`{}`), 0o600))
	_, err := readTestnetJournal(f.home)
	require.ErrorContains(t, err, "invalid")
	keyJSON, err := cmtjson.Marshal(f.key.Key)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(keyJSON, &fields))
	delete(fields, "pub_key")
	keyJSON, err = json.Marshal(fields)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(f.config.PrivValidatorKeyFile(), keyJSON, 0o600))
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
			require.NoError(t, os.WriteFile(path, append([]byte("home = '/outside-fork-home'\n"), original...), 0o600))
			before := snapshotTestnetFiles(t, f.home)
			require.ErrorContains(t, preflightTestnetCommand(cmd, []string{"fork", f.operator}), "overrides the selected fork home")
			require.Equal(t, before, snapshotTestnetFiles(t, f.home))
		})
	}
}

// Attach the fixture command below a root, as the real CLI does.
func (f *testnetPreflightFixture) nestedCommand(t *testing.T, path ...string) *cobra.Command {
	t.Helper()
	parent := &cobra.Command{Use: "manifestd"}
	for _, name := range path[:len(path)-1] {
		child := &cobra.Command{Use: name}
		parent.AddCommand(child)
		parent = child
	}
	cmd := f.command(t, path[len(path)-1])
	parent.AddCommand(cmd)
	return cmd
}

func TestTestnetApplicationCommandsProtected(t *testing.T) {
	for _, path := range [][]string{{"start"}, {"export"}, {"prune"}, {"rollback"}, {"module-hash-by-height"}, {"comet", "bootstrap-state"}, {"snapshots", "restore"}} {
		t.Run(strings.Join(path, " "), func(t *testing.T) {
			f := newTestnetPreflightFixture(t)
			journal := testnetJournal{Version: 1, SourceChainID: "source", ChainID: "fork", Operator: f.operator, ConsensusAddress: f.key.Key.Address, SourceHeight: 3}
			require.NoError(t, writeTestnetJournal(f.home, journal, true))
			cmd := f.nestedCommand(t, path...)
			require.True(t, testnetAppCommand(cmd))
			before := snapshotTestnetFiles(t, f.home)
			require.ErrorContains(t, preflightTestnetCommand(cmd, nil), "incomplete")
			require.Equal(t, before, snapshotTestnetFiles(t, f.home))
		})
	}
	// Client commands share these names but never open the application databases.
	for _, path := range [][]string{{"keys", "export"}, {"tx", "feegrant", "prune"}, {"testnet", "start"}, {"comet", "show-node-id"}} {
		t.Run(strings.Join(path, " "), func(t *testing.T) {
			f := newTestnetPreflightFixture(t)
			journal := testnetJournal{Version: 1, SourceChainID: "source", ChainID: "fork", Operator: f.operator, ConsensusAddress: f.key.Key.Address, SourceHeight: 3}
			require.NoError(t, writeTestnetJournal(f.home, journal, true))
			cmd := f.nestedCommand(t, path...)
			require.False(t, testnetAppCommand(cmd))
			require.NoError(t, preflightTestnetCommand(cmd, nil))
			require.Nil(t, cmd.Context().Value(testnetPreflightContextKey{}))
		})
	}
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
				require.NoError(t, os.WriteFile(path, append([]byte(fmt.Sprintf("home = %q\n", fork.home)), config...), 0o600))
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
	require.NoError(t, os.WriteFile(filepath.Join(f.home, "config", "app.toml"), []byte("genesis_file = 'config/other-genesis.json'\n"), 0o600))
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
	require.NoError(t, os.WriteFile(filepath.Join(f.home, inPlaceTestnetMarker+".next"), []byte("interrupted journal update"), 0o600))
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

func TestTestnetPreflightSupportsSDKAndCometGenesisFiles(t *testing.T) {
	for _, format := range []string{"sdk_app_genesis", "legacy_comet_genesis"} {
		t.Run(format, func(t *testing.T) {
			f := newTestnetPreflightFixture(t)
			if format == "sdk_app_genesis" {
				genesis, err := genutiltypes.AppGenesisFromFile(f.config.GenesisFile())
				require.NoError(t, err)
				genesis.AppName = "manifestd"
				genesis.AppVersion = "v2.3.1"
				genesis.AppState = json.RawMessage(`{"bank":{"balances":[],"supply":[]}}`)
				require.NoError(t, genesis.SaveAs(f.config.GenesisFile()))
				contents, err := os.ReadFile(f.config.GenesisFile())
				require.NoError(t, err)
				var fields map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(contents, &fields))
				require.JSONEq(t, `1`, string(fields["initial_height"]))
				require.Contains(t, fields, "consensus")
			}
			// The cached record retains Comet's encoding in both cases, just as
			// it does after ordinary SDK init followed by Comet startup.
			before := snapshotTestnetFiles(t, f.home)
			require.ErrorContains(t, preflightTestnetCommand(f.command(t, "in-place-testnet"), []string{"source", f.operator}), "requires a new chain ID different from source chain")
			require.Equal(t, before, snapshotTestnetFiles(t, f.home))
			require.NoError(t, preflightTestnetCommand(f.command(t, "in-place-testnet"), []string{"fork", f.operator}))
			require.Equal(t, before, snapshotTestnetFiles(t, f.home))
		})
	}
}
