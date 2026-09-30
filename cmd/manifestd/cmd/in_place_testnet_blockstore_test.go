package cmd

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	cmtdb "github.com/cometbft/cometbft-db"
	cmtstoreproto "github.com/cometbft/cometbft/proto/tendermint/store"
	cmtstore "github.com/cometbft/cometbft/store"
)

func TestTestnetSourceHeightBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name                                string
		appHeight, stateHeight, storeHeight int64
		allowed                             bool
	}{
		{"first committed block", 1, 1, 1, true},
		{"last usable aligned block", math.MaxInt64 - 2, math.MaxInt64 - 2, math.MaxInt64 - 2, true},
		{"last usable halt block", math.MaxInt64 - 3, math.MaxInt64 - 3, math.MaxInt64 - 2, true},
		{"last usable app commit", math.MaxInt64 - 2, math.MaxInt64 - 3, math.MaxInt64 - 2, true},
		{"history would overflow", math.MaxInt64 - 1, math.MaxInt64 - 1, math.MaxInt64 - 1, false},
		{"maximal heights", math.MaxInt64, math.MaxInt64, math.MaxInt64, false},
		{"uncommitted state", 1, 0, 1, false},
		{"empty blockstore", 1, 1, 0, false},
		{"negative blockstore", 1, 1, -1, false},
		{"uncommitted application", 0, 1, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTestnetSourceHeights(tc.appHeight, tc.stateHeight, tc.storeHeight)
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "unsupported source application/Comet/blockstore heights")
			}
		})
	}
}

func TestTestnetPreflightRejectsUnavailableBlockStore(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*testing.T, *testnetPreflightFixture)
		message string
	}{
		{"missing database", func(t *testing.T, f *testnetPreflightFixture) {
			require.NoError(t, os.RemoveAll(filepath.Join(f.config.DBDir(), "blockstore.db")))
		}, "read-only blockstore database requires an existing LOCK file"},
		{"missing lock", func(t *testing.T, f *testnetPreflightFixture) {
			require.NoError(t, os.Remove(filepath.Join(f.config.DBDir(), "blockstore.db", "LOCK")))
		}, "read-only blockstore database requires an existing LOCK file"},
		{"missing descriptor", func(t *testing.T, f *testnetPreflightFixture) {
			db, err := cmtdb.NewGoLevelDB("blockstore", f.config.DBDir())
			require.NoError(t, err)
			require.NoError(t, db.DeleteSync([]byte("blockStore")))
			require.NoError(t, db.Close())
		}, "invalid persisted Comet blockstore range"},
		{"malformed descriptor", func(t *testing.T, f *testnetPreflightFixture) {
			db, err := cmtdb.NewGoLevelDB("blockstore", f.config.DBDir())
			require.NoError(t, err)
			require.NoError(t, db.SetSync([]byte("blockStore"), []byte{0xff}))
			require.NoError(t, db.Close())
		}, "malformed persisted Comet blockstore state"},
		{"empty range", func(t *testing.T, f *testnetPreflightFixture) {
			f.saveBlockStore(t, 0)
		}, "invalid persisted Comet blockstore range"},
		{"inverted range", func(t *testing.T, f *testnetPreflightFixture) {
			db, err := cmtdb.NewGoLevelDB("blockstore", f.config.DBDir())
			require.NoError(t, err)
			cmtstore.SaveBlockStoreState(&cmtstoreproto.BlockStoreState{Base: 4, Height: 3}, db)
			require.NoError(t, db.Close())
		}, "invalid persisted Comet blockstore range"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newTestnetPreflightFixture(t)
			tc.mutate(t, f)
			before := snapshotTestnetFiles(t, f.home)
			cmd := f.command(t, "in-place-testnet")
			err := preflightTestnetCommand(cmd, []string{"fork", f.operator})
			require.ErrorContains(t, err, tc.message)
			require.Nil(t, cmd.Context().Value(testnetPreflightContextKey{}))
			require.Equal(t, before, snapshotTestnetFiles(t, f.home))
			require.NoFileExists(t, filepath.Join(f.home, inPlaceTestnetMarker))
		})
	}
}

func TestReadTestnetBlockStoreHeightLegacyDescriptor(t *testing.T) {
	f := newTestnetPreflightFixture(t)
	db, err := cmtdb.NewGoLevelDB("blockstore", f.config.DBDir())
	require.NoError(t, err)
	// Comet accepts descriptors written before the base field existed.
	cmtstore.SaveBlockStoreState(&cmtstoreproto.BlockStoreState{Height: 3}, db)
	require.NoError(t, db.Close())
	before := snapshotTestnetFiles(t, f.home)
	height, err := readTestnetBlockStoreHeight(f.config)
	require.NoError(t, err)
	require.Equal(t, int64(3), height)
	require.Equal(t, before, snapshotTestnetFiles(t, f.home))
}
