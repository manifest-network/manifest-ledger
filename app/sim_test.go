package app_test

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	"cosmossdk.io/store"
	storetypes "cosmossdk.io/store/types"
	"cosmossdk.io/x/feegrant"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/server"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simulationtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/simulation"
	simcli "github.com/cosmos/cosmos-sdk/x/simulation/client/cli"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/manifest-network/manifest-ledger/app"
	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
)

const (
	SimAppChainID = "manifest-ledger-simapp"
)

var FlagEnableStreamingValue bool

var SimulatorCommissionRateMinMax = app.RateMinMax{
	Floor: sdkmath.LegacyMustNewDecFromStr("0.1"),
	Ceil:  sdkmath.LegacyMustNewDecFromStr("0.5"),
}

// Get flags every time the simulator is run
func init() {
	simcli.GetSimulatorFlags()
	flag.BoolVar(&FlagEnableStreamingValue, "EnableStreaming", false, "Enable streaming service")
}

// fauxMerkleModeOpt returns a BaseApp option to use a dbStoreAdapter instead of
// an IAVLStore for faster simulation speed.
func fauxMerkleModeOpt(bapp *baseapp.BaseApp) {
	bapp.SetFauxMerkleMode()
}

// interBlockCacheOpt returns a BaseApp option function that sets the persistent
// inter-block write-through cache.
func interBlockCacheOpt() func(*baseapp.BaseApp) {
	return baseapp.SetInterBlockCache(store.NewCommitKVStoreCacheManager())
}

// BenchmarkSimulation run the chain simulation
// Running using starport command:
// `ignite chain simulate -v --numBlocks 200 --blockSize 50`
// Running as go benchmark test:
// `go test -benchmem -run=^$ -bench ^BenchmarkSimulation ./app -NumBlocks=200 -BlockSize 50 -Commit=true -Verbose=true -Enabled=true`
func BenchmarkSimulation(b *testing.B) {
	simcli.FlagSeedValue = time.Now().Unix()
	simcli.FlagVerboseValue = true
	simcli.FlagCommitValue = true
	simcli.FlagEnabledValue = true
	nodeHome := b.TempDir()

	config := simcli.NewConfigFromFlags()
	config.ChainID = SimAppChainID

	db, dir, logger, skip, err := simtestutil.SetupSimulation(config, "leveldb-app-sim", "Simulation", simcli.FlagVerboseValue, simcli.FlagEnabledValue)
	if skip {
		b.Skip("skipping application simulation")
	}
	require.NoError(b, err, "simulation setup failed")

	defer func() {
		require.NoError(b, db.Close())
		require.NoError(b, os.RemoveAll(dir))
	}()

	appOptions := make(simtestutil.AppOptionsMap, 0)
	appOptions[flags.FlagHome] = nodeHome
	appOptions[server.FlagInvCheckPeriod] = simcli.FlagPeriodValue

	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount(app.Bech32PrefixAccAddr, app.Bech32PrefixAccPub)
	cfg.SetBech32PrefixForValidator(app.Bech32PrefixValAddr, app.Bech32PrefixValPub)
	cfg.SetBech32PrefixForConsensusNode(app.Bech32PrefixConsAddr, app.Bech32PrefixConsPub)
	cfg.Seal()

	err = setPOAAdmin(config)
	require.NoError(b, err)

	bApp := app.NewApp(logger, db, nil, true, SimulatorCommissionRateMinMax, appOptions, fauxMerkleModeOpt, baseapp.SetChainID(SimAppChainID))
	require.Equal(b, app.AppName, bApp.Name())

	// run randomized simulation
	_, simParams, simErr := simulateWithBillingCoverage(
		b,
		bApp,
		simulationAppStateFn(bApp),
		config,
	)

	// export state and simParams before the simulation error is checked
	err = simtestutil.CheckExportSimulation(bApp, config, simParams)
	require.NoError(b, err)
	require.NoError(b, simErr)

	if config.Commit {
		simtestutil.PrintStats(db)
	}
}

func TestFullAppSimulation(t *testing.T) {
	config := simcli.NewConfigFromFlags()
	config.ChainID = SimAppChainID

	db, dir, logger, skip, err := simtestutil.SetupSimulation(config, "leveldb-app-sim", "Simulation", simcli.FlagVerboseValue, simcli.FlagEnabledValue)
	if skip {
		t.Skip("skipping application simulation")
	}
	require.NoError(t, err, "simulation setup failed")
	markSimulationComplete := requireSimulationCompletion(t)

	defer func() {
		require.NoError(t, db.Close())
		require.NoError(t, os.RemoveAll(dir))
	}()

	appOptions := make(simtestutil.AppOptionsMap, 0)
	appOptions[flags.FlagHome] = t.TempDir()
	appOptions[server.FlagInvCheckPeriod] = simcli.FlagPeriodValue

	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount(app.Bech32PrefixAccAddr, app.Bech32PrefixAccPub)
	cfg.SetBech32PrefixForValidator(app.Bech32PrefixValAddr, app.Bech32PrefixValPub)
	cfg.SetBech32PrefixForConsensusNode(app.Bech32PrefixConsAddr, app.Bech32PrefixConsPub)
	cfg.Seal()

	err = setPOAAdmin(config)
	require.NoError(t, err)

	bApp := app.NewApp(logger, db, nil, true, SimulatorCommissionRateMinMax, appOptions, fauxMerkleModeOpt, baseapp.SetChainID(SimAppChainID))
	require.Equal(t, app.AppName, bApp.Name())

	// run randomized simulation
	stopEarly, simParams, simErr := simulateWithBillingCoverage(
		t,
		bApp,
		simulationAppStateFn(bApp),
		config,
	)

	// export state and simParams before the simulation error is checked
	err = simtestutil.CheckExportSimulation(bApp, config, simParams)
	require.NoError(t, err)
	require.NoError(t, simErr)
	require.False(t, stopEarly, "simulation stopped before all configured blocks completed")

	if config.Commit {
		simtestutil.PrintStats(db)
	}
	markSimulationComplete()
}

func TestAppImportExport(t *testing.T) {
	config := simcli.NewConfigFromFlags()
	config.ChainID = SimAppChainID

	db, dir, logger, skip, err := simtestutil.SetupSimulation(config, "leveldb-app-sim", "Simulation", simcli.FlagVerboseValue, simcli.FlagEnabledValue)
	if skip {
		t.Skip("skipping application import/export simulation")
	}
	require.NoError(t, err, "simulation setup failed")
	markSimulationComplete := requireSimulationCompletion(t)

	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount(app.Bech32PrefixAccAddr, app.Bech32PrefixAccPub)
	cfg.SetBech32PrefixForValidator(app.Bech32PrefixValAddr, app.Bech32PrefixValPub)
	cfg.SetBech32PrefixForConsensusNode(app.Bech32PrefixConsAddr, app.Bech32PrefixConsPub)
	cfg.Seal()

	err = setPOAAdmin(config)
	require.NoError(t, err)

	defer func() {
		require.NoError(t, db.Close())
		require.NoError(t, os.RemoveAll(dir))
	}()

	appOptions := make(simtestutil.AppOptionsMap, 0)
	appOptions[flags.FlagHome] = t.TempDir()
	appOptions[server.FlagInvCheckPeriod] = simcli.FlagPeriodValue

	bApp := app.NewApp(logger, db, nil, true, SimulatorCommissionRateMinMax, appOptions, fauxMerkleModeOpt, baseapp.SetChainID(SimAppChainID))
	require.Equal(t, app.AppName, bApp.Name())

	// Run randomized simulation
	stopEarly, simParams, simErr := simulateWithBillingCoverage(
		t,
		bApp,
		simulationAppStateFn(bApp),
		config,
	)

	// export state and simParams before the simulation error is checked
	err = simtestutil.CheckExportSimulation(bApp, config, simParams)
	require.NoError(t, err)
	require.NoError(t, simErr)
	require.False(t, stopEarly, "simulation stopped before all configured blocks completed")

	if config.Commit {
		simtestutil.PrintStats(db)
	}

	fmt.Printf("exporting genesis...\n")

	exported, err := bApp.ExportAppStateAndValidators(false, []string{}, []string{})
	require.NoError(t, err)

	fmt.Printf("importing genesis...\n")

	newDB, newDir, _, _, err := simtestutil.SetupSimulation(config, "leveldb-app-sim-2", "Simulation-2", simcli.FlagVerboseValue, simcli.FlagEnabledValue)
	require.NoError(t, err, "simulation setup failed")

	defer func() {
		require.NoError(t, newDB.Close())
		require.NoError(t, os.RemoveAll(newDir))
	}()

	appOptions[flags.FlagHome] = t.TempDir()
	newApp := app.NewApp(log.NewNopLogger(), newDB, nil, true, SimulatorCommissionRateMinMax, appOptions, fauxMerkleModeOpt, baseapp.SetChainID(SimAppChainID))
	require.Equal(t, app.AppName, newApp.Name())

	ctxA := bApp.NewContextLegacy(true, cmtproto.Header{Height: bApp.LastBlockHeight()})
	ctxB := newApp.NewContextLegacy(true, cmtproto.Header{Height: bApp.LastBlockHeight()})
	_, err = newApp.InitChainer(ctxB, &abci.RequestInitChain{AppStateBytes: exported.AppState})
	require.NoError(t, err)
	err = newApp.StoreConsensusParams(ctxB, exported.ConsensusParams)
	require.NoError(t, err)
	fmt.Printf("comparing stores...\n")

	// skip certain prefixes
	skipPrefixes := map[string][][]byte{
		stakingtypes.StoreKey: {
			stakingtypes.UnbondingQueueKey, stakingtypes.RedelegationQueueKey, stakingtypes.ValidatorQueueKey,
			stakingtypes.HistoricalInfoKey, stakingtypes.UnbondingIDKey, stakingtypes.UnbondingIndexKey,
			stakingtypes.UnbondingTypeKey, stakingtypes.ValidatorUpdatesKey,
		},
		authzkeeper.StoreKey:   {authzkeeper.GrantQueuePrefix},
		feegrant.StoreKey:      {feegrant.FeeAllowanceQueueKeyPrefix},
		slashingtypes.StoreKey: {slashingtypes.ValidatorMissedBlockBitmapKeyPrefix},
	}

	storeKeys := bApp.GetStoreKeys()
	require.NotEmpty(t, storeKeys)

	for _, appKeyA := range storeKeys {
		// only compare kvstores
		if _, ok := appKeyA.(*storetypes.KVStoreKey); !ok {
			continue
		}

		keyName := appKeyA.Name()
		appKeyB := newApp.GetKey(keyName)

		storeA := ctxA.KVStore(appKeyA)
		storeB := ctxB.KVStore(appKeyB)

		failedKVAs, failedKVBs := simtestutil.DiffKVStores(storeA, storeB, skipPrefixes[keyName])
		require.Equal(t, len(failedKVAs), len(failedKVBs), "unequal sets of key-values to compare %s", keyName)

		fmt.Printf("compared %d different key/value pairs between %s and %s\n", len(failedKVAs), appKeyA, appKeyB)

		require.Equal(t, 0, len(failedKVAs), simtestutil.GetSimulationLog(keyName, bApp.SimulationManager().StoreDecoders, failedKVAs, failedKVBs))
	}
	markSimulationComplete()
}

func TestAppSimulationAfterImport(t *testing.T) {
	config := simcli.NewConfigFromFlags()
	config.ChainID = SimAppChainID

	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount(app.Bech32PrefixAccAddr, app.Bech32PrefixAccPub)
	cfg.SetBech32PrefixForValidator(app.Bech32PrefixValAddr, app.Bech32PrefixValPub)
	cfg.SetBech32PrefixForConsensusNode(app.Bech32PrefixConsAddr, app.Bech32PrefixConsPub)
	cfg.Seal()

	err := setPOAAdmin(config)
	require.NoError(t, err)

	db, dir, logger, skip, err := simtestutil.SetupSimulation(config, "leveldb-app-sim", "Simulation", simcli.FlagVerboseValue, simcli.FlagEnabledValue)
	if skip {
		t.Skip("skipping application simulation after import")
	}
	require.NoError(t, err, "simulation setup failed")
	markSimulationComplete := requireSimulationCompletion(t)

	defer func() {
		require.NoError(t, db.Close())
		require.NoError(t, os.RemoveAll(dir))
	}()

	appOptions := make(simtestutil.AppOptionsMap, 0)
	appOptions[flags.FlagHome] = t.TempDir()
	appOptions[server.FlagInvCheckPeriod] = simcli.FlagPeriodValue

	bApp := app.NewApp(logger, db, nil, true, SimulatorCommissionRateMinMax, appOptions, fauxMerkleModeOpt, baseapp.SetChainID(SimAppChainID))
	require.Equal(t, app.AppName, bApp.Name())

	var (
		simulationAccounts    []simulationtypes.Account
		simulationChainID     string
		simulationGenesisTime time.Time
	)
	newGenesisState := simulationAppStateFn(bApp)
	recordGenesisState := func(
		r *rand.Rand,
		accounts []simulationtypes.Account,
		config simulationtypes.Config,
	) (json.RawMessage, []simulationtypes.Account, string, time.Time) {
		appState, accounts, chainID, genesisTime := newGenesisState(r, accounts, config)
		simulationAccounts = slices.Clone(accounts)
		simulationChainID = chainID
		simulationGenesisTime = genesisTime
		return appState, accounts, chainID, genesisTime
	}

	// Run randomized simulation
	stopEarly, simParams, simErr := simulateWithBillingCoverage(
		t,
		bApp,
		recordGenesisState,
		config,
	)

	// export state and simParams before the simulation error is checked
	err = simtestutil.CheckExportSimulation(bApp, config, simParams)
	require.NoError(t, err)
	require.NoError(t, simErr)
	require.False(t, stopEarly, "simulation stopped before all configured blocks completed")
	require.NotEmpty(t, simulationAccounts, "simulation genesis did not return signing accounts")
	require.NotEmpty(t, simulationChainID, "simulation genesis did not return a chain ID")
	require.False(t, simulationGenesisTime.IsZero(), "simulation genesis did not return a timestamp")

	if config.Commit {
		simtestutil.PrintStats(db)
	}

	fmt.Printf("exporting genesis...\n")

	exported, err := bApp.ExportAppStateAndValidators(true, []string{}, []string{})
	require.NoError(t, err)

	fmt.Printf("importing genesis...\n")

	newDB, newDir, _, _, err := simtestutil.SetupSimulation(config, "leveldb-app-sim-2", "Simulation-2", simcli.FlagVerboseValue, simcli.FlagEnabledValue)
	require.NoError(t, err, "simulation setup failed")

	defer func() {
		require.NoError(t, newDB.Close())
		require.NoError(t, os.RemoveAll(newDir))
	}()

	appOptions[flags.FlagHome] = t.TempDir()
	newApp := app.NewApp(log.NewNopLogger(), newDB, nil, true, SimulatorCommissionRateMinMax, appOptions, fauxMerkleModeOpt, baseapp.SetChainID(SimAppChainID))
	require.Equal(t, app.AppName, newApp.Name())

	importGenesisCalls := 0
	importGenesisState := func(
		_ *rand.Rand,
		_ []simulationtypes.Account,
		_ simulationtypes.Config,
	) (json.RawMessage, []simulationtypes.Account, string, time.Time) {
		importGenesisCalls++
		return slices.Clone(exported.AppState), slices.Clone(simulationAccounts), simulationChainID, simulationGenesisTime
	}

	stopEarlyAfterImport, _, err := simulateWithBillingCoverage(
		t,
		newApp,
		importGenesisState,
		config,
	)
	require.NoError(t, err)
	require.False(t, stopEarlyAfterImport, "post-import simulation stopped before all configured blocks completed")
	require.Equal(t, 1, importGenesisCalls, "post-import simulation must initialize exactly once from exported state")
	markSimulationComplete()
}

func TestAppStateDeterminism(t *testing.T) {
	if !simcli.FlagEnabledValue {
		t.Skip("skipping application simulation")
	}
	markSimulationComplete := requireSimulationCompletion(t)

	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount(app.Bech32PrefixAccAddr, app.Bech32PrefixAccPub)
	cfg.SetBech32PrefixForValidator(app.Bech32PrefixValAddr, app.Bech32PrefixValPub)
	cfg.SetBech32PrefixForConsensusNode(app.Bech32PrefixConsAddr, app.Bech32PrefixConsPub)
	cfg.Seal()

	config := simcli.NewConfigFromFlags()
	config.InitialBlockHeight = 1
	config.ExportParamsPath = ""
	config.OnOperation = true
	config.AllInvariants = true
	config.ChainID = SimAppChainID

	numSeeds := 3
	numTimesToRunPerSeed := 3 // This used to be set to 5, but we've temporarily reduced it to 3 for the sake of faster CI.
	appHashList := make([]json.RawMessage, numTimesToRunPerSeed)

	// We will be overriding the random seed and just run a single simulation on the provided seed value
	if config.Seed != simcli.DefaultSeedValue {
		numSeeds = 1
	}

	appOptions := viper.New()
	if FlagEnableStreamingValue {
		m := make(map[string]interface{})
		m["streaming.abci.keys"] = []string{"*"}
		m["streaming.abci.plugin"] = "abci_v1"
		m["streaming.abci.stop-node-on-err"] = true
		for key, value := range m {
			appOptions.SetDefault(key, value)
		}
	}
	appOptions.SetDefault(flags.FlagHome, t.TempDir())
	appOptions.SetDefault(server.FlagInvCheckPeriod, simcli.FlagPeriodValue)
	if simcli.FlagVerboseValue {
		appOptions.SetDefault(flags.FlagLogLevel, "debug")
	}

	for i := 0; i < numSeeds; i++ {
		if config.Seed == simcli.DefaultSeedValue {
			config.Seed = rand.Int63()
		}
		fmt.Println("config.Seed: ", config.Seed)

		for j := 0; j < numTimesToRunPerSeed; j++ {
			var logger log.Logger
			if simcli.FlagVerboseValue {
				logger = log.NewTestLogger(t)
			} else {
				logger = log.NewNopLogger()
			}

			err := setPOAAdmin(config)
			require.NoError(t, err)

			appOptions.SetDefault(flags.FlagHome, t.TempDir())

			db := dbm.NewMemDB()
			bApp := app.NewApp(
				logger,
				db,
				nil,
				true,
				SimulatorCommissionRateMinMax,
				appOptions,
				interBlockCacheOpt(),
				baseapp.SetChainID(SimAppChainID),
			)

			fmt.Printf(
				"running non-determinism simulation; seed %d: %d/%d, attempt: %d/%d\n",
				config.Seed, i+1, numSeeds, j+1, numTimesToRunPerSeed,
			)

			stopEarly, _, err := simulateWithBillingCoverage(
				t,
				bApp,
				simulationAppStateFn(bApp),
				config,
			)
			require.NoError(t, err)
			require.False(t, stopEarly, "determinism replay stopped before all configured blocks completed")

			if config.Commit {
				simtestutil.PrintStats(db)
			}

			appHash := bApp.LastCommitID().Hash
			appHashList[j] = appHash

			if j != 0 {
				require.Equal(
					t, hex.EncodeToString(appHashList[0]), hex.EncodeToString(appHashList[j]),
					"non-determinism in seed %d: %d/%d, attempt: %d/%d\n", config.Seed, i+1, numSeeds, j+1, numTimesToRunPerSeed,
				)
			}
		}
	}
	markSimulationComplete()
}

// simulationAppStateFn preserves the SDK's randomized genesis except for the
// explicit send policy needed by SKU prices and billing deposits. A random bank
// deny otherwise turns every billing operation into a no-op for the entire run.
func simulationAppStateFn(bApp *app.ManifestApp) simulationtypes.AppStateFn {
	return simtestutil.AppStateFnWithExtendedCb(
		bApp.AppCodec(), bApp.SimulationManager(), bApp.DefaultGenesis(),
		func(state map[string]json.RawMessage) {
			enableSimulationBillingTransfers(bApp.AppCodec(), state)
		},
	)
}

func enableSimulationBillingTransfers(cdc codec.JSONCodec, state map[string]json.RawMessage) {
	var bankGenesis banktypes.GenesisState
	cdc.MustUnmarshalJSON(state[banktypes.ModuleName], &bankGenesis)

	// SKU simulation prices and billing funding both use DefaultBondDenom.
	// Set its override explicitly: DefaultSendEnabled does not override a
	// denomination-specific deny. Leave all other randomized bank policy intact.
	found := false
	for i := range bankGenesis.SendEnabled {
		if bankGenesis.SendEnabled[i].Denom == sdk.DefaultBondDenom {
			bankGenesis.SendEnabled[i].Enabled = true
			found = true
		}
	}
	if !found {
		bankGenesis.SendEnabled = append(bankGenesis.SendEnabled, banktypes.SendEnabled{
			Denom: sdk.DefaultBondDenom, Enabled: true,
		})
	}
	state[banktypes.ModuleName] = cdc.MustMarshalJSON(&bankGenesis)
}

// simulateWithBillingCoverage checks actual delivery statistics after every
// run, including imported-state runs and determinism replays. Invariants over
// empty billing state must not make a starved simulation look healthy.
func simulateWithBillingCoverage(
	tb testing.TB,
	bApp *app.ManifestApp,
	appStateFn simulationtypes.AppStateFn,
	config simulationtypes.Config,
) (bool, simulationtypes.Params, error) {
	tb.Helper()
	config, statsOutput := simulationStatisticsOutput(tb, config)
	stopEarly, params, err := simulation.SimulateFromSeed(
		tb, os.Stdout, bApp.BaseApp, appStateFn, simulationtypes.RandomAccounts,
		simtestutil.SimulationOperations(bApp, bApp.AppCodec(), config),
		app.BlockedAddresses(), config, bApp.AppCodec(),
	)
	return stopEarly, params, checkBillingSimulationResult(statsOutput, config, stopEarly, err)
}

func simulationStatisticsOutput(tb testing.TB, config simulationtypes.Config) (simulationtypes.Config, io.Writer) {
	tb.Helper()
	var statsOutput io.Writer
	if config.ExportStatsPath == "" {
		config.ExportStatsPath = filepath.Join(tb.TempDir(), "simulation-stats.json")
		statsOutput = os.Stdout
	}
	return config, statsOutput
}

func checkBillingSimulationResult(output io.Writer, config simulationtypes.Config, stopEarly bool, simulationErr error) error {
	// A signal can return an error along with exported partial statistics.
	// Read available diagnostics without masking the original simulator error.
	statsJSON, err := os.ReadFile(config.ExportStatsPath)
	if err != nil {
		if simulationErr != nil {
			return simulationErr
		}
		return fmt.Errorf("read simulation delivery statistics: %w", err)
	}
	var stats simulation.EventStats
	if err := json.Unmarshal(statsJSON, &stats); err != nil {
		if simulationErr != nil {
			return simulationErr
		}
		return fmt.Errorf("decode simulation delivery statistics: %w", err)
	}
	if output != nil {
		stats.Print(output)
	}
	// The SDK exports statistics even when a run stops early. Keep those
	// diagnostics, but only require billing coverage from completed runs.
	if simulationErr != nil || stopEarly {
		return simulationErr
	}
	if err := billingSimulationCoverageError(stats); err != nil {
		return fmt.Errorf("billing coverage at seed %d: %w", config.Seed, err)
	}
	return nil
}

func billingSimulationCoverageError(stats simulation.EventStats) error {
	// Check actual selections, not configured block counts: small smoke runs and
	// intentionally disabled operation weights need not exercise billing. Twenty
	// funding/creation selections give normal runs multiple chances to progress.
	const minimumAttempts = 20
	funding := stats[billingtypes.ModuleName][sdk.MsgTypeURL(&billingtypes.MsgFundCredit{})]
	if funding["ok"] == 0 && funding["failure"] >= minimumAttempts {
		return fmt.Errorf("billing simulation delivered no credit deposits after %d attempts", funding["failure"])
	}
	creation := stats[billingtypes.ModuleName][sdk.MsgTypeURL(&billingtypes.MsgCreateLease{})]
	if funding["ok"] > 0 && creation["ok"] == 0 && creation["failure"] >= minimumAttempts {
		return fmt.Errorf("billing simulation delivered no lease creations after %d attempts despite successful funding", creation["failure"])
	}
	return nil
}

// requireSimulationCompletion turns the SDK simulator's zero-validator Skip
// into a failure after a simulation has been explicitly enabled. Release gates
// must not report success when they did not execute every configured block.
func requireSimulationCompletion(t *testing.T) func() {
	t.Helper()
	completed := false
	t.Cleanup(func() {
		if !completed && !t.Failed() {
			t.Error("simulation exited before completing its configured validation")
		}
	})
	return func() {
		completed = true
	}
}

// setPOAAdmin sets the POA admin address in the environment variable POA_ADMIN_ADDRESS
func setPOAAdmin(config simulationtypes.Config) error {
	r := rand.New(rand.NewSource(config.Seed))
	params := simulation.RandomParams(r)
	accs := simulationtypes.RandomAccounts(r, params.NumKeys())
	poaAdminAddr := accs[0]
	err := os.Setenv("POA_ADMIN_ADDRESS", poaAdminAddr.Address.String())
	if err != nil {
		return err
	}
	return nil
}
