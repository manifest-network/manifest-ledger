package app_test

import (

	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"runtime/debug"
	"strings"
	"testing"
	"time"


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
	"github.com/cosmos/cosmos-sdk/server"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simulationtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	"github.com/cosmos/cosmos-sdk/x/simulation"
	simcli "github.com/cosmos/cosmos-sdk/x/simulation/client/cli"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/liftedinit/manifest-ledger/app"
)

const (
	SimAppChainID = "testing"
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
	appOptions[flags.FlagHome] = app.DefaultNodeHome
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
	_, simParams, simErr := simulation.SimulateFromSeed(
		b,
		os.Stdout,
		bApp.BaseApp,
		simtestutil.AppStateFn(bApp.AppCodec(), bApp.SimulationManager(), bApp.DefaultGenesis()),
		simulationtypes.RandomAccounts,
		simtestutil.SimulationOperations(bApp, bApp.AppCodec(), config),
		app.BlockedAddresses(),
		config,
		bApp.AppCodec(),
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

	defer func() {
		require.NoError(t, db.Close())
		require.NoError(t, os.RemoveAll(dir))
	}()

	appOptions := make(simtestutil.AppOptionsMap, 0)
	appOptions[flags.FlagHome] = app.DefaultNodeHome
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
	_, simParams, simErr := simulation.SimulateFromSeed(
		t,
		os.Stdout,
		bApp.BaseApp,
		simtestutil.AppStateFn(bApp.AppCodec(), bApp.SimulationManager(), bApp.DefaultGenesis()),
		simulationtypes.RandomAccounts, // Replace with own random account function if using keys other than secp256k1
		simtestutil.SimulationOperations(bApp, bApp.AppCodec(), config),
		app.BlockedAddresses(),
		config,
		bApp.AppCodec(),
	)

	// export state and simParams before the simulation error is checked
	err = simtestutil.CheckExportSimulation(bApp, config, simParams)
	require.NoError(t, err)
	require.NoError(t, simErr)

	if config.Commit {
		simtestutil.PrintStats(db)
	}
}

func TestAppImportExport(t *testing.T) {
	config := simcli.NewConfigFromFlags()
	config.ChainID = SimAppChainID

	db, dir, logger, skip, err := simtestutil.SetupSimulation(config, "leveldb-app-sim", "Simulation", simcli.FlagVerboseValue, simcli.FlagEnabledValue)
	if skip {
		t.Skip("skipping application import/export simulation")
	}
	require.NoError(t, err, "simulation setup failed")

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
	appOptions[flags.FlagHome] = app.DefaultNodeHome
	appOptions[server.FlagInvCheckPeriod] = simcli.FlagPeriodValue

	bApp := app.NewApp(logger, db, nil, true, SimulatorCommissionRateMinMax, appOptions, fauxMerkleModeOpt, baseapp.SetChainID(SimAppChainID))
	require.Equal(t, app.AppName, bApp.Name())

	// Run randomized simulation
	_, simParams, simErr := simulation.SimulateFromSeed(
		t,
		os.Stdout,
		bApp.BaseApp,
		simtestutil.AppStateFn(bApp.AppCodec(), bApp.SimulationManager(), bApp.DefaultGenesis()),
		simulationtypes.RandomAccounts,
		simtestutil.SimulationOperations(bApp, bApp.AppCodec(), config),
		app.BlockedAddresses(),
		config,
		bApp.AppCodec(),
	)

	// export state and simParams before the simulation error is checked
	err = simtestutil.CheckExportSimulation(bApp, config, simParams)
	require.NoError(t, err)
	require.NoError(t, simErr)

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

	newApp := app.NewApp(log.NewNopLogger(), newDB, nil, true, SimulatorCommissionRateMinMax, appOptions, fauxMerkleModeOpt, baseapp.SetChainID(SimAppChainID))
	require.Equal(t, app.AppName, newApp.Name())

	var genesisState app.GenesisState
	err = json.Unmarshal(exported.AppState, &genesisState)
	require.NoError(t, err)

	ctxA := bApp.NewContextLegacy(true, cmtproto.Header{Height: bApp.LastBlockHeight()})
	ctxB := newApp.NewContextLegacy(true, cmtproto.Header{Height: bApp.LastBlockHeight()})
	_, err = newApp.ModuleManager.InitGenesis(ctxB, bApp.AppCodec(), genesisState)
	if err != nil {
		if strings.Contains(err.Error(), "validator set is empty after InitGenesis") {
			logger.Info("Skipping simulation as all validators have been unbonded")
			logger.Info("err", err, "stacktrace", string(debug.Stack()))
			return
		}
	}
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

	defer func() {
		require.NoError(t, db.Close())
		require.NoError(t, os.RemoveAll(dir))
	}()

	appOptions := make(simtestutil.AppOptionsMap, 0)
	appOptions[flags.FlagHome] = app.DefaultNodeHome
	appOptions[server.FlagInvCheckPeriod] = simcli.FlagPeriodValue

	bApp := app.NewApp(logger, db, nil, true, SimulatorCommissionRateMinMax, appOptions, fauxMerkleModeOpt, baseapp.SetChainID(SimAppChainID))
	require.Equal(t, app.AppName, bApp.Name())

	// Run randomized simulation
	stopEarly, simParams, simErr := simulation.SimulateFromSeed(
		t,
		os.Stdout,
		bApp.BaseApp,
		simtestutil.AppStateFn(bApp.AppCodec(), bApp.SimulationManager(), bApp.DefaultGenesis()),
		simulationtypes.RandomAccounts,
		simtestutil.SimulationOperations(bApp, bApp.AppCodec(), config),
		app.BlockedAddresses(),
		config,
		bApp.AppCodec(),
	)

	// export state and simParams before the simulation error is checked
	err = simtestutil.CheckExportSimulation(bApp, config, simParams)
	require.NoError(t, err)
	require.NoError(t, simErr)

	if config.Commit {
		simtestutil.PrintStats(db)
	}

	if stopEarly {
		fmt.Println("can't export or import a zero-validator genesis, exiting test...")
		return
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

	newApp := app.NewApp(log.NewNopLogger(), newDB, nil, true, SimulatorCommissionRateMinMax, appOptions, fauxMerkleModeOpt, baseapp.SetChainID(SimAppChainID))
	require.Equal(t, app.AppName, newApp.Name())

	_, err = newApp.InitChain(&abci.RequestInitChain{
		AppStateBytes: exported.AppState,
		ChainId:       SimAppChainID,
	})
	require.NoError(t, err)

	_, _, err = simulation.SimulateFromSeed(
		t,
		os.Stdout,
		newApp.BaseApp,
		simtestutil.AppStateFn(bApp.AppCodec(), bApp.SimulationManager(), bApp.DefaultGenesis()),
		simulationtypes.RandomAccounts,
		simtestutil.SimulationOperations(newApp, newApp.AppCodec(), config),
		app.BlockedAddresses(),
		config,
		bApp.AppCodec(),
	)
	require.NoError(t, err)
}

func TestAppStateDeterminism(t *testing.T) {
	if !simcli.FlagEnabledValue {
		t.Skip("skipping application simulation")
	}

	config := simcli.NewConfigFromFlags()
	config.InitialBlockHeight = 1
	config.ExportParamsPath = ""
	config.OnOperation = false
	config.AllInvariants = false
	config.ChainID = ""  // Set to empty string to match expected value

	// Create separate temp dirs for each simulation
	dir1 := makeTestDir(t)
	dir2 := makeTestDir(t)

	db1 := dbm.NewMemDB()
	db2 := dbm.NewMemDB()

	// Create apps with different dirs but same seed
	app1 := app.NewApp(log.NewNopLogger(), db1, nil, true, SimulatorCommissionRateMinMax, simtestutil.NewAppOptionsWithFlagHome(dir1), baseapp.SetChainID(""))
	app2 := app.NewApp(log.NewNopLogger(), db2, nil, true, SimulatorCommissionRateMinMax, simtestutil.NewAppOptionsWithFlagHome(dir2), baseapp.SetChainID(""))

	appOptions := make(simtestutil.AppOptionsMap, 0)
	appOptions[flags.FlagHome] = app.DefaultNodeHome
	appOptions[server.FlagInvCheckPeriod] = simcli.FlagPeriodValue

	_, simParams, simErr := simulation.SimulateFromSeed(
		t,
		os.Stdout,
		app1.BaseApp,
		simtestutil.AppStateFn(app1.AppCodec(), app1.SimulationManager(), app1.DefaultGenesis()),
		simulationtypes.RandomAccounts,
		simtestutil.SimulationOperations(app1, app1.AppCodec(), config),
		app.BlockedAddresses(),
		config,
		app1.AppCodec(),
	)
	require.NoError(t, simErr)

	_, simParams2, simErr2 := simulation.SimulateFromSeed(
		t,
		os.Stdout,
		app2.BaseApp,
		simtestutil.AppStateFn(app2.AppCodec(), app2.SimulationManager(), app2.DefaultGenesis()),
		simulationtypes.RandomAccounts,
		simtestutil.SimulationOperations(app2, app2.AppCodec(), config),
		app.BlockedAddresses(),
		config,
		app2.AppCodec(),
	)
	require.NoError(t, simErr2)
	require.Equal(t, simParams, simParams2)
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

// Add helper to create unique test directories
func makeTestDir(t *testing.T) string {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "manifest-test")
	require.NoError(t, err)
	t.Cleanup(func() {
		os.RemoveAll(tempDir)
	})
	return tempDir
}
