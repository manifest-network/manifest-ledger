package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/cometbft/cometbft/crypto/ed25519"

	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/version"

	appparams "github.com/manifest-network/manifest-ledger/app/params"
)

func TestTestnetRestartGuards(t *testing.T) {
	t.Run("configured chain ID", func(t *testing.T) {
		f := newTestnetPreflightFixture(t)
		f.completeFork(t)
		cmd := f.command(t, startCommandName)
		if cmd.Flags().Lookup(flags.FlagChainID) == nil {
			cmd.Flags().String(flags.FlagChainID, "", "")
		}
		require.NoError(t, cmd.Flags().Set(flags.FlagChainID, "source"))
		require.ErrorContains(t, preflightTestnetCommand(cmd, nil), "configured chain ID disagrees")
	})
	t.Run("rollback of the first fork block", func(t *testing.T) {
		f := newTestnetPreflightFixture(t)
		f.completeFork(t)
		before := snapshotTestnetFiles(t, f.home)
		require.ErrorContains(t, preflightTestnetCommand(f.nestedCommand(t, "rollback"), nil), "rollback would remove the fork's first block 3")
		require.Equal(t, before, snapshotTestnetFiles(t, f.home))
		require.NoError(t, preflightTestnetCommand(f.nestedCommand(t, startCommandName), nil))
	})
	t.Run("rollback of a later fork block", func(t *testing.T) {
		f := newTestnetPreflightFixture(t)
		f.completeFork(t)
		require.NoError(t, os.Remove(filepath.Join(f.home, inPlaceTestnetMarker)))
		journal := testnetJournal{Version: 1, Complete: true, SourceChainID: "source", ChainID: "fork", Operator: f.operator, ConsensusAddress: f.key.Key.Address, SourceHeight: 1, FirstCommitHeight: 2}
		require.NoError(t, writeTestnetJournal(f.home, journal, true))
		require.NoError(t, preflightTestnetCommand(f.nestedCommand(t, "rollback"), nil))
	})
}

func TestTestnetServerContextMustMatchPreflight(t *testing.T) {
	for _, tc := range []struct {
		name, message string
		change        func(*server.Context)
	}{
		{name: "matching"},
		{name: "validator key", message: "resolves the validator key", change: func(ctx *server.Context) { ctx.Config.PrivValidatorKey = "config/source_key.json" }},
		{name: "database", message: "resolves the database directory", change: func(ctx *server.Context) { ctx.Config.DBPath = "data/other" }},
		{name: "home", message: "SDK startup home differs", change: func(ctx *server.Context) { ctx.Config.RootDir = filepath.Dir(ctx.Config.RootDir) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newTestnetPreflightFixture(t)
			cmd := f.command(t, inPlaceTestnetCommandName)
			require.NoError(t, preflightTestnetCommand(cmd, []string{"fork", f.operator}))
			serverContext, err := server.InterceptConfigsAndCreateContext(cmd, "", nil, initCometBFTConfig())
			require.NoError(t, err)
			cmd.SetContext(context.WithValue(cmd.Context(), server.ServerContextKey, serverContext))
			if tc.change == nil {
				require.NoError(t, configureTestnetServerContext(cmd))
				return
			}
			tc.change(serverContext)
			require.ErrorContains(t, configureTestnetServerContext(cmd), tc.message)
		})
	}
}

func TestTestnetConversionWrapper(t *testing.T) {
	require.ErrorContains(t, protectInPlaceTestnetCommand(&cobra.Command{Use: "manifestd"}), "found 0 in-place-testnet commands")
	withoutRun := &cobra.Command{Use: "manifestd"}
	withoutRun.AddCommand(&cobra.Command{Use: inPlaceTestnetCommandName})
	require.ErrorContains(t, protectInPlaceTestnetCommand(withoutRun), "has no RunE")

	root := &cobra.Command{Use: "manifestd"}
	server.AddTestnetCreatorCommand(root, newJournaledTestnetApp, addModuleInitFlags)
	require.NoError(t, protectInPlaceTestnetCommand(root))
	cmd, _, err := root.Find([]string{inPlaceTestnetCommandName})
	require.NoError(t, err)
	cmd.SetContext(context.Background())
	require.ErrorContains(t, cmd.RunE(cmd, []string{"fork", "operator"}), "read-only preflight was not performed")

	f := newTestnetPreflightFixture(t)
	checked := f.command(t, inPlaceTestnetCommandName)
	require.NoError(t, preflightTestnetCommand(checked, []string{"fork", f.operator}))
	serverContext := server.NewDefaultContext()
	config := *f.config
	config.RootDir = filepath.Dir(f.home)
	serverContext.Config = &config
	cmd.SetContext(context.WithValue(checked.Context(), server.ServerContextKey, serverContext))
	require.ErrorContains(t, cmd.RunE(cmd, []string{"fork", f.operator}), "SDK startup home differs")
	require.NoFileExists(t, filepath.Join(f.home, inPlaceTestnetMarker))
}

func TestNewJournaledTestnetAppRecordsAfterChecks(t *testing.T) {
	previousVersion := version.Version
	version.Version = "callback-upgrade"
	t.Cleanup(func() { version.Version = previousVersion })
	db := dbm.NewMemDB()
	sourceCtx, source := setupInPlaceTestnetWithDB(t, db, t.TempDir(), appparams.BondDenom)
	operator := sdk.MustAccAddressFromBech32(source.POAKeeper.GetAdmin(sourceCtx))
	pubKey := ed25519.GenPrivKey().PubKey()
	journal := testnetJournal{Version: 1, SourceChainID: "source", ChainID: "fork", Operator: operator.String(), ConsensusAddress: pubKey.Address(), SourceHeight: source.LastBlockHeight()}

	missing := inPlaceTestnetAppOptions(t.TempDir(), pubKey, operator)
	require.PanicsWithValue(t, "in-place-testnet conversion journal was not prepared by its read-only preflight", func() {
		newJournaledTestnetApp(log.NewNopLogger(), db, nil, missing)
	})

	// A rejected rewrite leaves the copy retryable.
	rejectedHome := t.TempDir()
	rejected := inPlaceTestnetAppOptions(rejectedHome, pubKey, operator)
	rejected[testnetJournalOption] = journal
	first := source.LastBlockHeight() + 1
	rejected[server.FlagHaltHeight] = first
	message := fmt.Sprintf("initialize in-place testnet: halt-height %d would stop the fork before its first block %d commits; clear it before conversion", first, first)
	require.PanicsWithError(t, message, func() { newJournaledTestnetApp(log.NewNopLogger(), db, nil, rejected) })
	require.NoFileExists(t, filepath.Join(rejectedHome, inPlaceTestnetMarker))

	home := t.TempDir()
	options := inPlaceTestnetAppOptions(home, pubKey, operator)
	options[testnetJournalOption] = journal
	options[server.FlagHaltHeight] = first + 1 // Halting after the first commit is allowed.
	application := newJournaledTestnetApp(log.NewNopLogger(), db, nil, options).(*journaledTestnetApp)
	t.Cleanup(func() { require.NoError(t, application.Close()) })
	recorded, err := readTestnetJournal(home)
	require.NoError(t, err)
	require.Equal(t, journal, recorded)
	require.Equal(t, journal, application.journal)
}

func TestTestnetJournalCommitRequiresNewBlock(t *testing.T) {
	f := newTestnetPreflightFixture(t)
	journal := testnetJournal{Version: 1, SourceChainID: "source", ChainID: "fork", Operator: f.operator, ConsensusAddress: f.key.Key.Address, SourceHeight: 3}
	require.NoError(t, writeTestnetJournal(f.home, journal, true))
	application := &journaledTestnetApp{Application: &testnetCommitRecorder{height: 3}, home: f.home, journal: journal}
	_, err := application.Commit()
	require.ErrorContains(t, err, "fork did not commit its first new block")
	recorded, err := readTestnetJournal(f.home)
	require.NoError(t, err)
	require.False(t, recorded.Complete)
}

func TestTestnetJournalWriteFailureLeavesNoRecord(t *testing.T) {
	f := newTestnetPreflightFixture(t)
	journal := testnetJournal{Version: 1, SourceChainID: "source", ChainID: "fork", Operator: f.operator, ConsensusAddress: f.key.Key.Address, SourceHeight: 3}
	write := writeTestnetJournalData
	t.Cleanup(func() { writeTestnetJournalData = write })
	fail := func(file *os.File, data []byte) error {
		_, err := file.Write(data[:len(data)/2])
		return errors.Join(err, fmt.Errorf("disk full"))
	}

	writeTestnetJournalData = fail
	require.ErrorContains(t, writeTestnetJournal(f.home, journal, true), "disk full")
	require.NoFileExists(t, filepath.Join(f.home, inPlaceTestnetMarker))

	writeTestnetJournalData = write
	require.NoError(t, writeTestnetJournal(f.home, journal, true))
	complete := journal
	complete.Complete, complete.FirstCommitHeight = true, 4
	writeTestnetJournalData = fail
	require.ErrorContains(t, writeTestnetJournal(f.home, complete, false), "disk full")
	require.NoFileExists(t, filepath.Join(f.home, inPlaceTestnetMarker+".next"))
	recorded, err := readTestnetJournal(f.home)
	require.NoError(t, err)
	require.Equal(t, journal, recorded)
}

func TestTestnetHomeWalkSkipsOnlyInaccessibleDirectories(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("directory permissions do not apply to root")
	}
	f := newTestnetPreflightFixture(t)
	// A root-owned lost+found at a volume root: neither listable nor searchable.
	lostFound := filepath.Join(f.home, "lost+found")
	require.NoError(t, os.Mkdir(lostFound, 0o700))
	require.NoError(t, os.Chmod(lostFound, 0))
	t.Cleanup(func() { _ = os.Chmod(lostFound, 0o700) })
	require.NoError(t, preflightTestnetCommand(f.command(t, inPlaceTestnetCommandName), []string{"fork", f.operator}))

	// Searchable but unlistable directories could still hide links the node follows.
	require.NoError(t, os.Chmod(lostFound, 0o100))
	require.ErrorContains(t, preflightTestnetCommand(f.command(t, inPlaceTestnetCommandName), []string{"fork", f.operator}), "inspect fork home")
}

func TestTestnetAncestorSymlinkNamesResolvedHome(t *testing.T) {
	f := newTestnetPreflightFixture(t)
	parent := filepath.Join(t.TempDir(), "alias")
	require.NoError(t, os.Symlink(filepath.Dir(f.home), parent))
	cmd := f.command(t, inPlaceTestnetCommandName)
	require.NoError(t, cmd.Flags().Set(flags.FlagHome, filepath.Join(parent, filepath.Base(f.home))))
	require.ErrorContains(t, preflightTestnetCommand(cmd, []string{"fork", f.operator}), fmt.Sprintf("use the resolved home %q", f.home))
}

func TestTestnetClientChainGuard(t *testing.T) {
	fork := newTestnetPreflightFixture(t)
	fork.completeFork(t)
	ordinary := newTestnetPreflightFixture(t)
	for _, tc := range []struct {
		name, home, chainID, message string
		path                         []string
	}{
		{name: "source transaction", home: fork.home, chainID: "source", path: []string{"tx", "bank", "send"}, message: `client chain ID "source" is not this fork's "fork"`},
		{name: "source query", home: fork.home, chainID: "source", path: []string{"query", "bank", "balances"}},
		{name: "source offline signing", home: fork.home, chainID: "source", path: []string{"tx", "sign"}, message: "is not this fork's"},
		{name: "fork transaction", home: fork.home, chainID: "fork", path: []string{"tx", "bank", "send"}},
		{name: "no chain ID", home: fork.home, path: []string{"tx", "bank", "send"}},
		{name: "keys", home: fork.home, chainID: "source", path: []string{"keys", "list"}},
		{name: "ordinary home", home: ordinary.home, chainID: "source", path: []string{"tx", "bank", "send"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := &cobra.Command{Use: "manifestd"}
			var cmd *cobra.Command
			for _, name := range tc.path {
				cmd = &cobra.Command{Use: name}
				parent.AddCommand(cmd)
				parent = cmd
			}
			cmd.SetContext(context.Background())
			require.NoError(t, client.SetCmdClientContext(cmd, client.Context{}.WithHomeDir(tc.home).WithChainID(tc.chainID)))
			err := rejectTestnetClientChain(cmd)
			if tc.message == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.message)
			}
		})
	}
}

// The SDK's configuration interceptor applies app.toml and environment values to
// unset flags after client.toml is read; transactions then sign with them.
func TestTestnetClientChainGuardSeesSDKConfiguration(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)
	prefix := strings.NewReplacer(".", "_", "-", "_").Replace(filepath.Base(exe))
	for _, tc := range []struct {
		name, explicit, message string
		configure               func(*testing.T, *testnetPreflightFixture)
	}{
		{name: "environment", message: `client chain ID "source" is not this fork's "fork"`, configure: func(t *testing.T, _ *testnetPreflightFixture) {
			t.Setenv(prefix+"_CHAIN_ID", "source")
		}},
		{name: "app.toml", message: `client chain ID "source" is not this fork's "fork"`, configure: func(t *testing.T, f *testnetPreflightFixture) {
			f.appendAppConfig(t, "chain-id = 'source'\n")
		}},
		{name: "explicit flag wins", explicit: "fork", configure: func(t *testing.T, _ *testnetPreflightFixture) {
			t.Setenv(prefix+"_CHAIN_ID", "source")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newTestnetPreflightFixture(t)
			f.completeFork(t)
			root := &cobra.Command{Use: "manifestd"}
			tx := &cobra.Command{Use: "tx"}
			send := &cobra.Command{Use: "send"}
			root.AddCommand(tx)
			tx.AddCommand(send)
			flags.AddTxFlagsToCmd(send)
			send.Flags().String(flags.FlagHome, "", "")
			require.NoError(t, send.Flags().Set(flags.FlagHome, f.home))
			if tc.explicit != "" {
				require.NoError(t, send.Flags().Set(flags.FlagChainID, tc.explicit))
			}
			send.SetContext(context.Background())
			// client.toml already names the fork.
			require.NoError(t, client.SetCmdClientContext(send, client.Context{}.WithHomeDir(f.home).WithChainID("fork")))
			require.NoError(t, rejectTestnetClientChain(send))
			tc.configure(t, f)
			_, err := server.InterceptConfigsAndCreateContext(send, "", nil, initCometBFTConfig())
			require.NoError(t, err)
			if tc.message == "" {
				require.NoError(t, rejectTestnetClientChain(send))
				return
			}
			require.ErrorContains(t, rejectTestnetClientChain(send), tc.message)
		})
	}
}

func TestTestnetOutputFormat(t *testing.T) {
	f := newTestnetPreflightFixture(t)
	cmd := &cobra.Command{Use: "module-hash-by-height"}
	flags.AddQueryFlagsToCmd(cmd)
	require.Equal(t, flags.OutputFormatText, testnetOutputFormat(cmd, f.home))
	f.appendClientConfig(t, "output = 'json'\n")
	require.Equal(t, flags.OutputFormatJSON, testnetOutputFormat(cmd, f.home))
	require.NoError(t, cmd.Flags().Set(flags.FlagOutput, flags.OutputFormatText))
	require.Equal(t, flags.OutputFormatText, testnetOutputFormat(cmd, f.home))
}

func (f *testnetPreflightFixture) appendClientConfig(t *testing.T, content string) {
	t.Helper()
	file, err := os.OpenFile(filepath.Join(f.home, "config", "client.toml"), os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = file.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, file.Close())
}

func TestInitAppForTestnetMirrorsFirstBlockUpgradeChecks(t *testing.T) {
	for _, tc := range []struct {
		name, completed, trigger, message string
		pending                           *upgradetypes.Plan
		skipFirst                         bool
	}{
		{name: "no upgrade history"},
		{name: "completed upgrade with handler", completed: "rehearsal-upgrade"},
		{name: "completed upgrade without handler", completed: "retired-upgrade", message: `no handler for the source's last completed upgrade "retired-upgrade"`},
		{name: "trigger replaces the completed upgrade check", completed: "retired-upgrade", trigger: "rehearsal-upgrade"},
		{name: "due plan with handler", pending: &upgradetypes.Plan{Name: "rehearsal-upgrade", Height: 2}},
		{name: "due plan without handler", pending: &upgradetypes.Plan{Name: "unregistered-upgrade", Height: 2}, message: `source upgrade "unregistered-upgrade" is due at the fork's first block 2`},
		{name: "skipped due plan", pending: &upgradetypes.Plan{Name: "unregistered-upgrade", Height: 2}, skipFirst: true},
		{name: "skipped due plan still checks history", completed: "retired-upgrade", pending: &upgradetypes.Plan{Name: "unregistered-upgrade", Height: 2}, skipFirst: true, message: "last completed upgrade"},
		{name: "handler registered early", pending: &upgradetypes.Plan{Name: "rehearsal-upgrade", Height: 100}, message: `registers the pending source upgrade "rehearsal-upgrade" before its height 100`},
		{name: "pending plan without handler", pending: &upgradetypes.Plan{Name: "future-upgrade", Height: 100}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := simtestutil.AppOptionsMap{}
			if tc.skipFirst {
				options[server.FlagUnsafeSkipUpgrades] = []int{2}
			}
			ctx, chainApp := setupInPlaceTestnetWithOptions(t, dbm.NewMemDB(), t.TempDir(), appparams.BondDenom, options)
			if tc.completed != "" {
				key := append([]byte{upgradetypes.DoneByte}, sdk.Uint64ToBigEndian(1)...)
				ctx.KVStore(chainApp.GetKey(upgradetypes.StoreKey)).Set(append(key, tc.completed...), []byte{1})
			}
			if tc.pending != nil {
				require.NoError(t, chainApp.UpgradeKeeper.ScheduleUpgrade(ctx, *tc.pending))
			}
			before := snapshotInPlaceTestnetStores(t, ctx, chainApp)
			key := ed25519.GenPrivKey().PubKey()
			err := initAppForTestnet(chainApp, key.Address(), key, chainApp.POAKeeper.GetAdmin(ctx), tc.trigger)
			if tc.message == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.message)
			require.Equal(t, before, snapshotInPlaceTestnetStores(t, ctx, chainApp))
		})
	}
}
