package cmd

import (
	"bufio"
	"bytes"
	"context"
	stded25519 "crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/syndtr/goleveldb/leveldb/opt"

	cmtdb "github.com/cometbft/cometbft-db"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/crypto"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/privval"
	cmtstateproto "github.com/cometbft/cometbft/proto/tendermint/state"
	cmtstate "github.com/cometbft/cometbft/state"
	cmttypes "github.com/cometbft/cometbft/types"

	dbm "github.com/cosmos/cosmos-db"
	gogotypes "github.com/cosmos/gogoproto/types"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/version"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
)

const (
	inPlaceTestnetCommandName = "in-place-testnet"
	inPlaceTestnetMarker      = "in-place-testnet.json"
	poaSimulationEnvVar       = "POA_BYPASS_ADMIN_CHECK_FOR_SIMULATION_TESTING_ONLY"
)

type testnetPreflightContextKey struct{}

// The marker is a fail-closed journal, not a transaction across the application
// and Comet databases. A completed application commit is checked against Comet's
// persisted state again on every ordinary start.
type testnetJournal struct {
	Version           int    `json:"version"`
	Complete          bool   `json:"complete"`
	SourceChainID     string `json:"source_chain_id"`
	ChainID           string `json:"chain_id"`
	Operator          string `json:"operator"`
	ConsensusAddress  []byte `json:"consensus_address"`
	SourceHeight      int64  `json:"source_height"`
	FirstCommitHeight int64  `json:"first_commit_height,omitempty"`
}

type testnetPreflight struct {
	home    string
	config  *cmtcfg.Config
	options *viper.Viper
	journal testnetJournal
}

func testnetAppCommand(cmd *cobra.Command) bool {
	for current := cmd; current != nil; current = current.Parent() {
		switch current.Name() {
		case "start", "export", "snapshots", "prune", "rollback", "bootstrap-state", "module-hash-by-height":
			return true
		}
	}
	return false
}

func rejectSimulationAdminBypass() error {
	if os.Getenv(poaSimulationEnvVar) != "" {
		return fmt.Errorf("%s must be unset when running manifestd", poaSimulationEnvVar)
	}
	return nil
}

// Run this before client configuration, keyrings, SDK configuration interception,
// profiling, tracing, or any database opener can mutate the selected home.
func preflightTestnetCommand(cmd *cobra.Command, args []string) error {
	if err := rejectSimulationAdminBypass(); err != nil {
		return err
	}
	conversion := cmd.Name() == inPlaceTestnetCommandName
	if !conversion && !testnetAppCommand(cmd) {
		return nil
	}
	v, cfg, err := readTestnetConfig(cmd)
	if err != nil {
		return err
	}
	home := cfg.RootDir
	if !conversion {
		if _, err := os.Lstat(filepath.Join(home, inPlaceTestnetMarker)); os.IsNotExist(err) {
			return nil
		} else if err != nil {
			return err
		}
	}
	if err := validateTestnetHome(home, cfg, v); err != nil {
		return fmt.Errorf("in-place-testnet home preflight: %w", err)
	}
	var journal testnetJournal
	if !conversion {
		journal, err = readTestnetJournal(home)
		if err != nil {
			return err
		}
		// Conversion can stop between the genesis rewrite and Comet Bootstrap.
		// Explain that recovery boundary before trying to decode mixed state.
		if err := validateTestnetJournalReady(journal); err != nil {
			return err
		}
	}
	if err := validateTestnetIsolation(cfg, v); err != nil {
		return err
	}
	key, err := readTestnetKey(cfg.PrivValidatorKeyFile())
	if err != nil {
		return err
	}
	state, err := readTestnetCometState(cfg)
	if err != nil {
		return err
	}
	appHeight, appHash, err := readTestnetApplicationCommit(home)
	if err != nil {
		return err
	}
	if !conversion {
		if err := validateTestnetRestart(journal, key, state, appHeight, appHash); err != nil {
			return err
		}
		if chainID := v.GetString("chain-id"); chainID != "" && chainID != state.ChainID {
			return fmt.Errorf("configured chain ID disagrees with the fork conversion journal")
		}
		cmd.SetContext(context.WithValue(cmd.Context(), testnetPreflightContextKey{}, &testnetPreflight{home: home, config: cfg, options: v, journal: journal}))
		return nil
	}
	if len(args) != 2 {
		return fmt.Errorf("in-place-testnet requires a new chain ID and operator address")
	}
	if _, err := os.Lstat(filepath.Join(home, inPlaceTestnetMarker)); !os.IsNotExist(err) {
		return fmt.Errorf("in-place-testnet conversion journal already exists; use normal start for a completed fork or make a fresh disposable copy")
	}
	if args[0] == "" || len(args[0]) > cmttypes.MaxChainIDLen || args[0] == state.ChainID {
		return fmt.Errorf("in-place-testnet requires a new chain ID different from source chain %q", state.ChainID)
	}
	operator, err := sdk.AccAddressFromBech32(args[1])
	if err != nil {
		return fmt.Errorf("invalid fork operator: %w", err)
	}
	if err := validateTestnetAuthority(operator, os.Getenv("POA_ADMIN_ADDRESS")); err != nil {
		return err
	}
	if err := validateTestnetSigningState(cfg.PrivValidatorStateFile()); err != nil {
		return err
	}
	if err := validateTestnetFreshKey(key, state); err != nil {
		return err
	}
	if appHeight < 1 || (appHeight != state.LastBlockHeight && appHeight != state.LastBlockHeight+1) {
		return fmt.Errorf("unsupported source application/Comet heights %d/%d", appHeight, state.LastBlockHeight)
	}
	if trigger := v.GetString(server.KeyTriggerTestnetUpgrade); trigger != "" {
		if trigger != version.Version {
			return fmt.Errorf("testnet upgrade handler %q is not registered in this binary (%s)", trigger, version.Version)
		}
		for _, height := range v.GetIntSlice(server.FlagUnsafeSkipUpgrades) {
			if int64(height) == appHeight+testnetUpgradeDelay {
				return fmt.Errorf("testnet upgrade height %d is in --unsafe-skip-upgrades", height)
			}
		}
	}
	preflight := &testnetPreflight{home: home, config: cfg, options: v, journal: testnetJournal{
		Version: 1, SourceChainID: state.ChainID, ChainID: args[0], Operator: operator.String(),
		ConsensusAddress: key.Address, SourceHeight: appHeight,
	}}
	cmd.SetContext(context.WithValue(cmd.Context(), testnetPreflightContextKey{}, preflight))
	return nil
}

// Mirror the SDK's flags/env/config precedence without creating config files.
func readTestnetConfig(cmd *cobra.Command) (*viper.Viper, *cmtcfg.Config, error) {
	v := viper.New()
	if err := v.BindPFlags(cmd.Flags()); err != nil {
		return nil, nil, err
	}
	if err := v.BindPFlags(cmd.PersistentFlags()); err != nil {
		return nil, nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, nil, err
	}
	basename := filepath.Base(exe)
	v.SetEnvPrefix(basename)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()
	// The SDK selects Comet's root before reading either configuration file.
	home := v.GetString(flags.FlagHome)
	protected := cmd.Name() == inPlaceTestnetCommandName || testnetMarkerExists(home)
	if protected {
		if err := inspectTestnetTree(home); err != nil {
			return nil, nil, err
		}
		if err := requireTestnetConfigFiles(home); err != nil {
			return nil, nil, err
		}
	}
	cfg := initCometBFTConfig()
	v.SetConfigFile(filepath.Join(home, "config", "config.toml"))
	if err := v.ReadInConfig(); err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, nil, err
	}
	cfg.SetRoot(home)
	v.SetConfigFile(filepath.Join(home, "config", "app.toml"))
	if err := v.MergeInConfig(); err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}
	// SDK bindFlags adds these case-sensitive env bindings only AFTER config
	// loading. AutomaticEnv alone checks a different, fully uppercase prefix.
	var bindingErr error
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		if bindingErr == nil {
			bindingErr = v.BindEnv(flag.Name, basename+"_"+strings.ToUpper(strings.ReplaceAll(flag.Name, "-", "_")))
		}
	})
	if bindingErr != nil {
		return nil, nil, bindingErr
	}
	runtimeHome := v.GetString(flags.FlagHome)
	if protected || testnetMarkerExists(runtimeHome) {
		if runtimeHome != home {
			return nil, nil, fmt.Errorf("configuration overrides the selected fork home; remove home overrides from config files and environment")
		}
	}
	return v, cfg, nil
}

func testnetMarkerExists(home string) bool {
	_, err := os.Lstat(filepath.Join(home, inPlaceTestnetMarker))
	// An unreadable marker must fail closed too; the later read supplies the error.
	return !os.IsNotExist(err)
}

func requireTestnetConfigFiles(home string) error {
	for _, name := range []string{"config.toml", "app.toml", "client.toml"} {
		info, err := os.Stat(filepath.Join(home, "config", name))
		if err != nil {
			return fmt.Errorf("existing %s is required before conversion: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s must be a regular file", name)
		}
	}
	return nil
}

func inspectTestnetTree(home string) error {
	if home == "" || !filepath.IsAbs(home) || filepath.Clean(home) != home {
		return fmt.Errorf("fork home must be an absolute, clean directory path")
	}
	for path := home; ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("fork home ancestor %q must be a real directory, without symlinks", path)
		}
		if path == filepath.Dir(path) {
			break
		}
	}
	return filepath.WalkDir(home, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if path == filepath.Join(home, "cosmovisor", "current") {
				resolved, err := filepath.EvalSymlinks(path)
				if err == nil && testnetPathInside(filepath.Join(home, "cosmovisor"), resolved) {
					info, statErr := os.Stat(resolved)
					if statErr == nil && info.IsDir() {
						return nil // Cosmovisor's read-only binary selection link.
					}
				}
			}
			return fmt.Errorf("fork home contains symlink %q; use an independent copy", path)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("fork home contains non-regular file %q", path)
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Nlink > 1 {
			return fmt.Errorf("fork home contains hard-linked file %q; use an independent copy", path)
		}
		return nil
	})
}

func testnetPathInside(home, path string) bool {
	rel, err := filepath.Rel(home, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func rejectTestnetPathSymlinks(home, path string) error {
	for current := path; current != home; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("writable fork path %q traverses a symlink", path)
		}
	}
	return nil
}

func validateTestnetHome(home string, cfg *cmtcfg.Config, v *viper.Viper) error {
	if cfg.P2P.AddrBook == "" {
		return fmt.Errorf("p2p.addr_book_file must name a file")
	}
	files := map[string]string{
		"genesis": cfg.GenesisFile(), "validator key": cfg.PrivValidatorKeyFile(), "validator state": cfg.PrivValidatorStateFile(),
		"node key": cfg.NodeKeyFile(), "address book": cfg.P2P.AddrBookFile(), "consensus WAL": cfg.Consensus.WalFile(),
		"fork journal": filepath.Join(home, inPlaceTestnetMarker), "config": filepath.Join(home, "config", "config.toml"),
		"journal update": filepath.Join(home, inPlaceTestnetMarker+".next"),
		"app config":     filepath.Join(home, "config", "app.toml"), "client config": filepath.Join(home, "config", "client.toml"),
	}
	for _, key := range []string{"trace-store", "cpu-profile"} {
		if path := v.GetString(key); path != "" {
			absolute, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			files[key] = absolute
		}
	}
	if v.GetString("streaming.abci.plugin") != "" || len(v.GetStringSlice("store.streamers")) != 0 {
		return fmt.Errorf("external store streaming must be disabled for an isolated fork")
	}
	dirs := map[string]string{
		"application DB": filepath.Join(home, "data", "application.db"), "snapshots": filepath.Join(home, "data", "snapshots"),
		"Wasm": filepath.Join(home, "wasm"), "blockstore": filepath.Join(cfg.DBDir(), "blockstore.db"),
		"Comet state": filepath.Join(cfg.DBDir(), "state.db"), "evidence": filepath.Join(cfg.DBDir(), "evidence.db"),
		"transaction index": filepath.Join(cfg.DBDir(), "tx_index.db"),
	}
	if cfg.Mempool.WalEnabled() {
		dirs["mempool WAL"] = cfg.Mempool.WalDir()
	}
	if cfg.StateSync.TempDir != "" {
		dirs["state sync temporary directory"] = cfg.StateSync.TempDir
	}
	if !testnetPathInside(home, cfg.DBDir()) || filepath.Clean(cfg.DBDir()) != cfg.DBDir() {
		return fmt.Errorf("database directory must remain inside the fork home")
	}
	for label, path := range files {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || !testnetPathInside(home, path) {
			return fmt.Errorf("%s path %q must be a clean file path inside the fork home", label, path)
		}
		if err := rejectTestnetPathSymlinks(home, path); err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("inspect %s: %w", label, err)
		}
		if err == nil && !info.Mode().IsRegular() {
			return fmt.Errorf("%s must name a regular file", label)
		}
		for other, otherPath := range files {
			if other != label && (path == otherPath || testnetPathInside(path, otherPath)) {
				return fmt.Errorf("%s path collides with %s", label, other)
			}
			if other != "consensus WAL" && isTestnetWALRotation(cfg.Consensus.WalFile(), otherPath) {
				return fmt.Errorf("%s path collides with a consensus WAL rotation", other)
			}
		}
		for dirLabel, dir := range dirs {
			if path == dir || testnetPathInside(dir, path) || testnetPathInside(path, dir) {
				return fmt.Errorf("%s path collides with %s", label, dirLabel)
			}
		}
	}
	for label, path := range dirs {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || !testnetPathInside(home, path) {
			return fmt.Errorf("%s path must remain inside the fork home", label)
		}
		if err := rejectTestnetPathSymlinks(home, path); err != nil {
			return err
		}
		for other, otherPath := range dirs {
			if other != label && (path == otherPath || testnetPathInside(path, otherPath)) {
				return fmt.Errorf("%s directory collides with %s", label, other)
			}
		}
	}
	for _, listener := range []string{cfg.RPC.ListenAddress, cfg.RPC.GRPCListenAddress, cfg.ProxyApp, cfg.P2P.ListenAddress, v.GetString("grpc.address"), v.GetString("api.address")} {
		if strings.HasPrefix(listener, "unix:") {
			return fmt.Errorf("unix socket listeners are unsupported for in-place testnet homes")
		}
	}
	return nil
}

func isTestnetWALRotation(wal, path string) bool {
	if !strings.HasPrefix(path, wal+".") {
		return false
	}
	suffix := strings.TrimPrefix(path, wal+".")
	if suffix == "" {
		return false
	}
	for _, c := range suffix {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func validateTestnetIsolation(cfg *cmtcfg.Config, v *viper.Viper) error {
	if cfg.P2P.PersistentPeers != "" || cfg.P2P.Seeds != "" || cfg.P2P.PexReactor || cfg.StateSync.Enable {
		return fmt.Errorf("fork isolation requires empty persistent_peers and seeds, pex=false and statesync.enable=false")
	}
	if cfg.PrivValidatorListenAddr != "" {
		return fmt.Errorf("fork isolation requires a local validator key, without priv_validator_laddr")
	}
	if cfg.TxIndex.Indexer != "kv" && cfg.TxIndex.Indexer != "null" {
		return fmt.Errorf("fork isolation requires tx_index.indexer to be kv or null; external transaction indexing is unsupported")
	}
	if cfg.DBBackend != "goleveldb" || (v.GetString("app-db-backend") != "" && v.GetString("app-db-backend") != "goleveldb") {
		return fmt.Errorf("read-only fork preflight currently requires goleveldb application and Comet databases")
	}
	if (v.IsSet("with-comet") && !v.GetBool("with-comet")) || v.GetBool("grpc-only") {
		return fmt.Errorf("fork startup requires CometBFT enabled")
	}
	return nil
}

func readTestnetKey(path string) (key privval.FilePVKey, err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("invalid validator key file")
		}
	}()
	bz, err := os.ReadFile(path)
	if err != nil {
		return key, err
	}
	if err := cmtjson.Unmarshal(bz, &key); err != nil {
		return key, fmt.Errorf("decode validator key: %w", err)
	}
	if key.PubKey == nil || key.PrivKey == nil || key.PubKey.Type() != ed25519.KeyType || key.PrivKey.Type() != ed25519.KeyType || len(key.PrivKey.Bytes()) != ed25519.PrivateKeySize || len(key.PubKey.Bytes()) != ed25519.PubKeySize {
		return key, fmt.Errorf("in-place-testnet requires a complete ed25519 validator key")
	}
	if !bytes.Equal(key.PubKey.Bytes(), key.PrivKey.PubKey().Bytes()) || !bytes.Equal(key.Address, key.PubKey.Address()) ||
		!bytes.Equal(stded25519.NewKeyFromSeed(key.PrivKey.Bytes()[:stded25519.SeedSize]), key.PrivKey.Bytes()) {
		return key, fmt.Errorf("validator key address, public key and private key must agree")
	}
	return key, nil
}

func validateTestnetSigningState(path string) error {
	bz, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read reset validator signing state: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(bz, &fields); err != nil {
		return fmt.Errorf("decode validator signing state: %w", err)
	}
	for _, field := range []string{"height", "round", "step"} {
		if value := bytes.TrimSpace(fields[field]); len(value) == 0 || bytes.Equal(value, []byte("null")) {
			return fmt.Errorf("validator signing state must contain explicit non-null height, round and step")
		}
	}
	var state privval.FilePVLastSignState
	if err := cmtjson.Unmarshal(bz, &state); err != nil {
		return fmt.Errorf("decode validator signing state: %w", err)
	}
	if state.Height != 0 || state.Round != 0 || state.Step != 0 || len(state.Signature) != 0 || len(state.SignBytes) != 0 {
		return fmt.Errorf("validator signing state must be reset to height/round/step zero without signatures")
	}
	return nil
}

func openTestnetReadOnlyDB(name, dir string) (*cmtdb.GoLevelDB, error) {
	// goleveldb's read-only opener still creates a missing LOCK file. Require
	// that file first so even a rejected preflight leaves copied bytes alone.
	info, err := os.Stat(filepath.Join(dir, name+".db", "LOCK"))
	if err != nil {
		return nil, fmt.Errorf("read-only %s database requires an existing LOCK file: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("invalid %s database LOCK file", name)
	}
	return cmtdb.NewGoLevelDBWithOpts(name, dir, &opt.Options{ReadOnly: true})
}

func readTestnetCometState(cfg *cmtcfg.Config) (_ *cmtstate.State, err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("malformed persisted Comet state")
		}
	}()
	db, err := openTestnetReadOnlyDB("state", cfg.DBDir())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	bz, err := db.Get([]byte("stateKey"))
	if err != nil {
		return nil, err
	}
	var proto cmtstateproto.State
	if err := proto.Unmarshal(bz); err != nil {
		return nil, err
	}
	state, err := cmtstate.FromProto(&proto)
	if err != nil {
		return nil, err
	}
	if state.LastBlockHeight < 1 || state.Validators == nil || state.NextValidators == nil || state.LastValidators == nil {
		return nil, fmt.Errorf("in-place-testnet requires committed Comet state")
	}
	genesis, err := os.ReadFile(cfg.GenesisFile())
	if err != nil {
		return nil, err
	}
	// SDK genesis files use AppGenesis (numeric initial_height and a nested
	// consensus object). Its reader also supports legacy Comet genesis files.
	// The cached genesis below remains Comet's own JSON representation.
	doc, err := genutiltypes.AppGenesisFromReader(bytes.NewReader(genesis))
	if err != nil {
		return nil, fmt.Errorf("read application genesis: %w", err)
	}
	if doc.ChainID != state.ChainID {
		return nil, fmt.Errorf("genesis and persisted Comet chain IDs disagree")
	}
	cached, err := db.Get([]byte("genesisDoc"))
	if err != nil {
		return nil, err
	}
	if len(cached) != 0 {
		var cachedDoc cmttypes.GenesisDoc
		if err := cmtjson.Unmarshal(cached, &cachedDoc); err != nil {
			return nil, err
		}
		if cachedDoc.ChainID != state.ChainID {
			return nil, fmt.Errorf("cached genesis and persisted Comet chain IDs disagree")
		}
	}
	return state, nil
}

func readTestnetApplicationCommit(home string) (int64, []byte, error) {
	db, err := openTestnetReadOnlyDB("application", filepath.Join(home, "data"))
	if err != nil {
		return 0, nil, err
	}
	defer db.Close()
	bz, err := db.Get([]byte("s/latest"))
	if err != nil {
		return 0, nil, err
	}
	var height int64
	if err := gogotypes.StdInt64Unmarshal(&height, bz); err != nil {
		return 0, nil, err
	}
	if height < 1 {
		return 0, nil, fmt.Errorf("in-place-testnet requires committed application state")
	}
	bz, err = db.Get([]byte(fmt.Sprintf("s/%d", height)))
	if err != nil {
		return 0, nil, err
	}
	var info storetypes.CommitInfo
	if err := info.Unmarshal(bz); err != nil {
		return 0, nil, err
	}
	if info.Version != height || len(info.StoreInfos) == 0 {
		return 0, nil, fmt.Errorf("invalid persisted application commit info")
	}
	return height, info.Hash(), nil
}

func validateTestnetFreshKey(key privval.FilePVKey, state *cmtstate.State) error {
	allowed := false
	for _, kind := range state.ConsensusParams.Validator.PubKeyTypes {
		if kind == key.PubKey.Type() {
			allowed = true
		}
	}
	if !allowed {
		return fmt.Errorf("fork validator key type is not allowed by source consensus parameters")
	}
	for _, set := range []*cmttypes.ValidatorSet{state.LastValidators, state.Validators, state.NextValidators} {
		if set != nil && set.HasAddress(key.Address) {
			return fmt.Errorf("fork validator key reuses a source last/current/next consensus key; generate a fresh key")
		}
	}
	return nil
}

func validateTestnetJournalReady(journal testnetJournal) error {
	if !journal.Complete || journal.FirstCommitHeight <= journal.SourceHeight {
		return fmt.Errorf("in-place-testnet conversion is incomplete; create a fresh disposable copy")
	}
	if os.Getenv("POA_ADMIN_ADDRESS") != journal.Operator {
		return fmt.Errorf("fork restart requires POA_ADMIN_ADDRESS=%s", journal.Operator)
	}
	return nil
}

func validateTestnetRestart(journal testnetJournal, key privval.FilePVKey, state *cmtstate.State, appHeight int64, appHash []byte) error {
	if state.ChainID != journal.ChainID || !bytes.Equal(key.Address, journal.ConsensusAddress) {
		return fmt.Errorf("fork chain ID or validator key disagrees with its conversion journal")
	}
	// Comet can recover an app-ahead crash using the stored block and ABCI
	// response. This guard does not verify those recovery records, so it requires
	// matching persisted commits even after a previously completed conversion.
	if state.LastBlockHeight < journal.FirstCommitHeight || appHeight != state.LastBlockHeight || !bytes.Equal(appHash, state.AppHash) {
		return fmt.Errorf("fork application and Comet commit are inconsistent with the conversion journal; create a fresh disposable copy")
	}
	return nil
}

// Validate the actual SDK context after its config/env-to-flag bindings. Pin
// BaseApp's chain ID so its separate merged genesis_file fallback cannot select
// another genesis document on an otherwise valid fork restart.
func configureTestnetServerContext(cmd *cobra.Command) error {
	preflight, ok := cmd.Context().Value(testnetPreflightContextKey{}).(*testnetPreflight)
	if !ok {
		return nil
	}
	serverContext := server.GetServerContextFromCmd(cmd)
	if serverContext.Config.RootDir != preflight.home || serverContext.Viper.GetString(flags.FlagHome) != preflight.home {
		return fmt.Errorf("SDK startup home differs from the checked fork home")
	}
	if err := validateTestnetHome(preflight.home, serverContext.Config, serverContext.Viper); err != nil {
		return err
	}
	if err := validateTestnetIsolation(serverContext.Config, serverContext.Viper); err != nil {
		return err
	}
	serverContext.Viper.Set(flags.FlagChainID, preflight.journal.ChainID)
	return nil
}

func readTestnetJournal(home string) (testnetJournal, error) {
	var journal testnetJournal
	bz, err := os.ReadFile(filepath.Join(home, inPlaceTestnetMarker))
	if err != nil {
		return journal, err
	}
	if err := json.Unmarshal(bz, &journal); err != nil {
		return journal, fmt.Errorf("invalid in-place-testnet conversion journal: %w", err)
	}
	if journal.Version != 1 || journal.ChainID == "" || journal.SourceChainID == journal.ChainID || journal.Operator == "" || len(journal.ConsensusAddress) != crypto.AddressSize || journal.SourceHeight < 1 {
		return journal, fmt.Errorf("invalid in-place-testnet conversion journal")
	}
	return journal, nil
}

func writeTestnetJournal(home string, journal testnetJournal, create bool) error {
	bz, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	name := filepath.Join(home, inPlaceTestnetMarker)
	if !create {
		name += ".next"
	}
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(bz)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if !create {
		if err := os.Rename(name, filepath.Join(home, inPlaceTestnetMarker)); err != nil {
			return err
		}
	}
	dir, err := os.Open(home)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// Keep the SDK confirmation before creating the incomplete journal, and then
// delegate conversion/startup to its existing command implementation.
func protectInPlaceTestnetCommand(root *cobra.Command) {
	for _, cmd := range root.Commands() {
		if cmd.Name() != inPlaceTestnetCommandName {
			continue
		}
		run := cmd.RunE
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			preflight, ok := cmd.Context().Value(testnetPreflightContextKey{}).(*testnetPreflight)
			if !ok {
				return fmt.Errorf("in-place-testnet read-only preflight was not performed")
			}
			serverContext := server.GetServerContextFromCmd(cmd)
			if serverContext.Config.RootDir != preflight.home || serverContext.Viper.GetString(flags.FlagHome) != preflight.home {
				return fmt.Errorf("SDK startup home differs from the checked fork home")
			}
			if skip, _ := cmd.Flags().GetBool("skip-confirmation"); !skip {
				fmt.Fprintln(cmd.OutOrStdout(), "This modifies the disposable home and cannot be undone. Continue? (y/n)")
				answer, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if answer = strings.ToLower(strings.TrimSpace(answer)); answer != "y" && answer != "yes" {
					return nil
				}
			}
			if err := writeTestnetJournal(preflight.home, preflight.journal, true); err != nil {
				return fmt.Errorf("create incomplete conversion journal: %w", err)
			}
			if err := cmd.Flags().Set("skip-confirmation", "true"); err != nil {
				return err
			}
			return run(cmd, args)
		}
	}
}

type journaledTestnetApp struct {
	servertypes.Application
	home    string
	journal testnetJournal
}

func newJournaledTestnetApp(logger log.Logger, db dbm.DB, trace io.Writer, opts servertypes.AppOptions) servertypes.Application {
	home, _ := opts.Get(flags.FlagHome).(string)
	journal, err := readTestnetJournal(home)
	if err != nil {
		panic(err)
	}
	if journal.Complete {
		panic("in-place-testnet conversion journal is already complete")
	}
	return &journaledTestnetApp{Application: newTestnetApp(logger, db, trace, opts), home: home, journal: journal}
}

func (app *journaledTestnetApp) Commit() (*abci.ResponseCommit, error) {
	response, err := app.Application.Commit()
	if err != nil || app.journal.Complete {
		return response, err
	}
	info, err := app.Application.Info(&abci.RequestInfo{})
	if err != nil {
		return nil, err
	}
	if info.LastBlockHeight <= app.journal.SourceHeight {
		return nil, fmt.Errorf("fork did not commit its first new block")
	}
	journal := app.journal
	journal.Complete = true
	journal.FirstCommitHeight = info.LastBlockHeight
	if err := writeTestnetJournal(app.home, journal, false); err != nil {
		return nil, fmt.Errorf("complete conversion journal: %w", err)
	}
	app.journal = journal
	return response, nil
}
