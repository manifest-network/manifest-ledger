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
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/spf13/cast"
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

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/version"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
)

const (
	inPlaceTestnetCommandName = "in-place-testnet"
	startCommandName          = "start"
	inPlaceTestnetMarker      = "in-place-testnet.json"
	poaSimulationEnvVar       = "POA_BYPASS_ADMIN_CHECK_FOR_SIMULATION_TESTING_ONLY"
	// Server-context option carrying the checked journal to the app creator.
	testnetJournalOption = "manifest-in-place-testnet-journal"
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

// The command's path below the root. A command without a parent is its own path.
func testnetCommandPath(cmd *cobra.Command) []string {
	path := []string{cmd.Name()}
	for current := cmd.Parent(); current != nil && current.HasParent(); current = current.Parent() {
		path = append([]string{current.Name()}, path...)
	}
	return path
}

// Match full paths: client commands such as `keys export`, `tx feegrant prune`
// and `testnet start` share names with the application commands.
func testnetAppCommand(cmd *cobra.Command) bool {
	path := testnetCommandPath(cmd)
	switch path[0] {
	case startCommandName, "export", "prune", "rollback", "module-hash-by-height":
		return len(path) == 1
	case "snapshots":
		return true
	case "comet":
		return len(path) == 2 && path[1] == "bootstrap-state"
	}
	return false
}

// RejectSimulationAdminBypass refuses the PoA simulation-only admin bypass.
func RejectSimulationAdminBypass() error {
	if os.Getenv(poaSimulationEnvVar) != "" {
		return fmt.Errorf("%s must be unset when running manifestd", poaSimulationEnvVar)
	}
	return nil
}

// Run this before client configuration, keyrings, SDK configuration interception,
// profiling, tracing, or any database opener can mutate the selected home.
func preflightTestnetCommand(cmd *cobra.Command, args []string) error {
	if err := RejectSimulationAdminBypass(); err != nil {
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
		// Rolling back the first fork block restores the unmodified source state.
		if path := testnetCommandPath(cmd); len(path) == 1 && path[0] == "rollback" && state.LastBlockHeight <= journal.FirstCommitHeight {
			return fmt.Errorf("rollback would remove the fork's first block %d and restore unmodified source state; make a fresh disposable copy instead", journal.FirstCommitHeight)
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
	// Completing the journal creates this file exclusively after the first commit.
	if _, err := os.Lstat(filepath.Join(home, inPlaceTestnetMarker+".next")); !os.IsNotExist(err) {
		return fmt.Errorf("stale in-place-testnet journal update %s exists; make a fresh disposable copy", inPlaceTestnetMarker+".next")
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
	storeHeight, err := readTestnetBlockStoreHeight(cfg)
	if err != nil {
		return err
	}
	if err := validateTestnetSourceHeights(appHeight, state.LastBlockHeight, storeHeight); err != nil {
		return err
	}
	if err := validateTestnetSourceCommit(cfg, state, appHeight, appHash); err != nil {
		return err
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
	if err := validateTestnetHaltSettings(v, appHeight+1); err != nil {
		return err
	}
	if err := probeTestnetListeners(cfg, v); err != nil {
		return err
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
			if resolved, err := filepath.EvalSymlinks(home); err == nil && resolved != home {
				return fmt.Errorf("fork home ancestor %q must be a real directory, without symlinks; use the resolved home %q", path, resolved)
			}
			return fmt.Errorf("fork home ancestor %q must be a real directory, without symlinks", path)
		}
		if path == filepath.Dir(path) {
			break
		}
	}
	return filepath.WalkDir(home, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			// A directory the node user can neither list nor enter, such as a
			// root-owned lost+found at a volume root, is equally out of the
			// node's reach, so nothing conversion or startup uses can be there.
			if entry != nil && entry.IsDir() && path != home && errors.Is(err, fs.ErrPermission) && syscall.Access(path, 1 /* X_OK */) != nil {
				return fs.SkipDir
			}
			return fmt.Errorf("inspect fork home: %w", err)
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
				return testnetCosmovisorLinkError(home, path)
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

// Cosmovisor records an absolute target under the original DAEMON_HOME, so a
// copy's link keeps selecting the source's binaries until it is re-pointed.
func testnetCosmovisorLinkError(home, path string) error {
	cosmovisor := filepath.Join(home, "cosmovisor")
	target, err := os.Readlink(path)
	if err != nil {
		return fmt.Errorf("cosmovisor/current must select a directory inside %s: %w", cosmovisor, err)
	}
	marker := string(filepath.Separator) + "cosmovisor" + string(filepath.Separator)
	if index := strings.LastIndex(target, marker); index >= 0 {
		candidate := filepath.Join(cosmovisor, target[index+len(marker):])
		if info, err := os.Stat(candidate); err == nil && info.IsDir() && testnetPathInside(cosmovisor, candidate) {
			return fmt.Errorf("cosmovisor/current points to %q outside this copy; re-point it with: ln -sfn %s %s", target, candidate, path)
		}
	}
	return fmt.Errorf("cosmovisor/current points to %q; re-point it to a directory inside %s", target, cosmovisor)
}

// BaseApp loads a plugin for every [streaming.<service>] table, not only abci.
func testnetStreamingEnabled(v *viper.Viper) bool {
	if strings.TrimSpace(v.GetString("streaming.abci.plugin")) != "" {
		return true
	}
	for _, service := range slices.Sorted(maps.Keys(cast.ToStringMap(v.Get(baseapp.StreamingTomlKey)))) {
		key := fmt.Sprintf("%s.%s.%s", baseapp.StreamingTomlKey, service, baseapp.StreamingABCIPluginTomlKey)
		if strings.TrimSpace(cast.ToString(v.Get(key))) != "" {
			return true
		}
	}
	return false
}

// The SDK finds config/config and config/app by name, trying viper's extensions
// in order and parsing the first match as TOML, so a file whose extension sorts
// before toml would silently replace the configuration checked here.
func rejectTestnetShadowConfig(home string) error {
	for _, ext := range viper.SupportedExts {
		if ext == "toml" {
			return nil
		}
		for _, name := range []string{"config", "app"} {
			path := filepath.Join(home, "config", name+"."+ext)
			if _, err := os.Lstat(path); err == nil {
				return fmt.Errorf("fork home contains %q, which would replace %s.toml; remove it", path, name)
			} else if !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
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
	if testnetStreamingEnabled(v) || len(v.GetStringSlice("store.streamers")) != 0 {
		return fmt.Errorf("external store streaming must be disabled for an isolated fork")
	}
	if err := rejectTestnetShadowConfig(home); err != nil {
		return err
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
	// Comet splits rpc.laddr into trimmed, comma-separated endpoints and opens
	// each listener separately. The remaining listener settings are single-valued.
	listeners := append(strings.Split(cfg.RPC.ListenAddress, ","), cfg.RPC.GRPCListenAddress, cfg.ProxyApp, cfg.P2P.ListenAddress, v.GetString("grpc.address"), v.GetString("api.address"))
	for _, listener := range listeners {
		network, _, _ := strings.Cut(strings.TrimSpace(listener), ":")
		switch network {
		case "unix", "unixpacket", "unixgram":
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
	// Push sinks would deliver fork metrics, labelled as the source chain, to
	// production collectors. The in-memory sink only serves local scrapes.
	if v.GetBool("telemetry.enabled") {
		switch sink := v.GetString("telemetry.metrics-sink"); sink {
		case telemetry.MetricSinkStatsd, telemetry.MetricSinkDogsStatsd:
			return fmt.Errorf("fork isolation forbids the %s telemetry sink; use the in-memory sink", sink)
		}
	}
	return nil
}

// BaseApp refuses to finalize any block at or above a nonzero halt-height and
// any block at or after a nonzero halt-time. A copied setting would stop the
// fork before its first commit and leave the conversion incomplete.
func validateTestnetHaltSettings(opts servertypes.AppOptions, firstHeight int64) error {
	// The SDK reads halt-height as uint64; firstHeight is a positive block height.
	if height := cast.ToUint64(opts.Get(server.FlagHaltHeight)); height != 0 && firstHeight > 0 && height <= uint64(firstHeight) { //nolint:gosec // guarded above
		return fmt.Errorf("halt-height %d would stop the fork before its first block %d commits; clear it before conversion", height, firstHeight)
	}
	if cast.ToUint64(opts.Get(server.FlagHaltTime)) != 0 {
		return fmt.Errorf("halt-time must be unset for conversion; set it only after the fork commits its first block")
	}
	return nil
}

// CometBFT binds its listeners, and the SDK its gRPC and API servers, only
// after testnetify has rewritten the copy. Probe them first so an address in
// use cannot strand a converted home before its first commit.
func probeTestnetListeners(cfg *cmtcfg.Config, v *viper.Viper) (err error) {
	addresses := append(strings.Split(cfg.RPC.ListenAddress, ","), cfg.P2P.ListenAddress, cfg.RPC.GRPCListenAddress)
	for _, service := range []string{"grpc", "api"} {
		if v.GetBool(service + ".enable") {
			addresses = append(addresses, v.GetString(service+".address"))
		}
	}
	// Hold every probe until all are checked: endpoints that share a port each
	// bind alone, but the node fails on the second once it holds the first.
	var listeners []net.Listener
	defer func() {
		for _, listener := range listeners {
			err = errors.Join(err, listener.Close())
		}
	}()
	for _, address := range addresses {
		address = strings.TrimSpace(address)
		if address == "" {
			continue
		}
		network, host, ok := strings.Cut(address, "://")
		if !ok {
			network, host = "tcp", address
		}
		listener, listenErr := net.Listen(network, host)
		if listenErr != nil {
			return fmt.Errorf("fork listener %s is unavailable: %w", address, listenErr)
		}
		listeners = append(listeners, listener)
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
	// The SDK locates its configuration files itself. It must resolve the same
	// identity, genesis and databases that preflight checked and journals.
	actual, checked := serverContext.Config, preflight.config
	for _, paths := range [][3]string{
		{"validator key", actual.PrivValidatorKeyFile(), checked.PrivValidatorKeyFile()},
		{"validator signing state", actual.PrivValidatorStateFile(), checked.PrivValidatorStateFile()},
		{"node key", actual.NodeKeyFile(), checked.NodeKeyFile()},
		{"genesis", actual.GenesisFile(), checked.GenesisFile()},
		{"database directory", actual.DBDir(), checked.DBDir()},
		{"address book", actual.P2P.AddrBookFile(), checked.P2P.AddrBookFile()},
		{"consensus WAL", actual.Consensus.WalFile(), checked.Consensus.WalFile()},
	} {
		if paths[1] != paths[2] {
			return fmt.Errorf("SDK configuration resolves the %s to %q, not the checked %q", paths[0], paths[1], paths[2])
		}
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

// Tests replace this to exercise failed writes.
var writeTestnetJournalData = func(file *os.File, data []byte) error {
	_, err := file.Write(data)
	return err
}

func writeTestnetJournal(home string, journal testnetJournal, create bool) (err error) {
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
	// Never leave a partial record: an incomplete marker would refuse retries of
	// a conversion that never started, and a stale update blocks the next one.
	defer func() {
		if err != nil {
			_ = os.Remove(name)
		}
	}()
	writeErr := writeTestnetJournalData(file, bz)
	syncErr := file.Sync()
	closeErr := file.Close()
	if joined := errors.Join(writeErr, syncErr, closeErr); joined != nil {
		return joined
	}
	if !create {
		if err := os.Rename(name, filepath.Join(home, inPlaceTestnetMarker)); err != nil {
			return err
		}
	}
	return syncTestnetDirectory(home)
}

// Like goleveldb, tolerate filesystems that cannot sync a directory.
func syncTestnetDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil && !errors.Is(err, syscall.EINVAL) {
		return err
	}
	return nil
}

// Keep the SDK confirmation in front of conversion and hand the checked journal
// to the application creator, which records it before testnetify's first write.
func protectInPlaceTestnetCommand(root *cobra.Command) error {
	protected := 0
	for _, cmd := range root.Commands() {
		if cmd.Name() != inPlaceTestnetCommandName {
			continue
		}
		run := cmd.RunE
		if run == nil {
			return fmt.Errorf("%s has no RunE to protect", inPlaceTestnetCommandName)
		}
		protected++
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
			serverContext.Viper.Set(testnetJournalOption, preflight.journal)
			if err := cmd.Flags().Set("skip-confirmation", "true"); err != nil {
				return err
			}
			return run(cmd, args)
		}
	}
	if protected != 1 {
		return fmt.Errorf("found %d %s commands to protect, want 1", protected, inPlaceTestnetCommandName)
	}
	return nil
}

type journaledTestnetApp struct {
	servertypes.Application
	home    string
	journal testnetJournal
}

func newJournaledTestnetApp(logger log.Logger, db dbm.DB, trace io.Writer, opts servertypes.AppOptions) servertypes.Application {
	home, _ := opts.Get(flags.FlagHome).(string)
	journal, ok := opts.Get(testnetJournalOption).(testnetJournal)
	if !ok || journal.Complete {
		panic("in-place-testnet conversion journal was not prepared by its read-only preflight")
	}
	application := newTestnetApp(logger, db, trace, opts)
	// Every check that can reject this copy without changing it has now passed:
	// the SDK's configuration, pruning, profiling and tracing checks, testnetify's
	// checks before this call, and the application rewrite, which stays in memory
	// until the first commit. Preflight mirrors testnetify's final reconciliation.
	// Record the conversion before testnetify's first write.
	if err := writeTestnetJournal(home, journal, true); err != nil {
		panic(fmt.Errorf("create incomplete conversion journal: %w", err))
	}
	return &journaledTestnetApp{Application: application, home: home, journal: journal}
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
