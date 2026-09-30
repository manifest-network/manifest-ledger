package cmd

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	cmtdb "github.com/cometbft/cometbft-db"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtstateproto "github.com/cometbft/cometbft/proto/tendermint/state"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmtstore "github.com/cometbft/cometbft/store"
)

func (f *testnetPreflightFixture) writeRecord(t *testing.T, name, key string, value []byte) {
	t.Helper()
	db, err := cmtdb.NewGoLevelDB(name, f.config.DBDir())
	require.NoError(t, err)
	if value == nil {
		require.NoError(t, db.DeleteSync([]byte(key)))
	} else {
		require.NoError(t, db.SetSync([]byte(key), value))
	}
	require.NoError(t, db.Close())
}

func TestTestnetPreflightSourceRecoveryRejectsWithoutWrites(t *testing.T) {
	for _, tc := range []struct {
		name, message string
		appAhead      bool
		mutate        func(*testing.T, *testnetPreflightFixture)
	}{
		{"aligned last block ID", "source last block ID disagrees", false, func(t *testing.T, f *testnetPreflightFixture) {
			f.state.LastBlockID.Hash = bytes.Repeat([]byte{0x42}, 32)
			f.saveState(t)
		}},
		{"aligned metadata hash", "disagrees with Comet state or metadata", false, func(t *testing.T, f *testnetPreflightFixture) {
			db, err := cmtdb.NewGoLevelDB("blockstore", f.config.DBDir())
			require.NoError(t, err)
			meta := cmtstore.NewBlockStore(db).LoadBlockMeta(3).ToProto()
			meta.BlockID.Hash = bytes.Repeat([]byte{0x42}, 32)
			value, err := meta.Marshal()
			require.NoError(t, err)
			require.NoError(t, db.SetSync([]byte("H:3"), value))
			require.NoError(t, db.Close())
		}},
		{"aligned missing metadata", "full local block and metadata", false, func(t *testing.T, f *testnetPreflightFixture) {
			f.writeRecord(t, "blockstore", "H:3", nil)
		}},
		{"aligned malformed metadata", "malformed persisted source block", false, func(t *testing.T, f *testnetPreflightFixture) {
			f.writeRecord(t, "blockstore", "H:3", []byte{0xff})
		}},
		{"app ahead missing block part", "full local block and metadata", true, func(t *testing.T, f *testnetPreflightFixture) {
			f.writeRecord(t, "blockstore", "P:3:0", nil)
		}},
		{"app ahead prior block ID", "does not extend Comet state", true, func(t *testing.T, f *testnetPreflightFixture) {
			f.state.LastBlockID.Hash = bytes.Repeat([]byte{0x42}, 32)
			f.saveState(t)
		}},
		{"app ahead prior application hash", "does not extend Comet state", true, func(t *testing.T, f *testnetPreflightFixture) {
			f.state.AppHash = bytes.Repeat([]byte{0x42}, 32)
			f.saveState(t)
		}},
		{"missing recovery response", "no last ABCI response", true, func(t *testing.T, f *testnetPreflightFixture) {
			f.writeRecord(t, "state", "lastABCIResponseKey", nil)
		}},
		{"malformed recovery response", "decode last ABCI response", true, func(t *testing.T, f *testnetPreflightFixture) {
			f.writeRecord(t, "state", "lastABCIResponseKey", []byte{0xff})
		}},
		{"empty recovery payload", "contains no response", true, func(t *testing.T, f *testnetPreflightFixture) {
			info := cmtstateproto.ABCIResponsesInfo{Height: 3}
			value, err := info.Marshal()
			require.NoError(t, err)
			f.writeRecord(t, "state", "lastABCIResponseKey", value)
		}},
		{"wrong response height", "expected height 3", true, func(t *testing.T, f *testnetPreflightFixture) {
			f.saveResponse(t, 2, &abci.ResponseFinalizeBlock{TxResults: []*abci.ExecTxResult{{}}})
		}},
		{"wrong transaction result count", "invalid committed source block response", true, func(t *testing.T, f *testnetPreflightFixture) {
			f.saveResponse(t, 3, &abci.ResponseFinalizeBlock{})
		}},
		{"wrong committed application hash", "response hash disagrees", true, func(t *testing.T, f *testnetPreflightFixture) {
			f.saveResponse(t, 3, &abci.ResponseFinalizeBlock{AppHash: bytes.Repeat([]byte{0x42}, 32), TxResults: []*abci.ExecTxResult{{}}})
		}},
		{"invalid consensus params", "invalid committed source consensus params", true, func(t *testing.T, f *testnetPreflightFixture) {
			f.saveResponse(t, 3, &abci.ResponseFinalizeBlock{TxResults: []*abci.ExecTxResult{{}}, ConsensusParamUpdates: &cmtproto.ConsensusParams{
				Block: &cmtproto.BlockParams{MaxBytes: -2},
			}})
		}},
		{"invalid consensus transition", "invalid committed source consensus parameter update", true, func(t *testing.T, f *testnetPreflightFixture) {
			f.saveResponse(t, 3, &abci.ResponseFinalizeBlock{TxResults: []*abci.ExecTxResult{{}}, ConsensusParamUpdates: &cmtproto.ConsensusParams{
				Abci: &cmtproto.ABCIParams{VoteExtensionsEnableHeight: 2},
			}})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newTestnetPreflightFixture(t)
			if tc.appAhead {
				f.state.LastBlockHeight = 2
				f.state.LastBlockID = f.blockIDs[2]
				f.state.AppHash = testnetPreflightCommitInfo(2).Hash()
				f.saveState(t)
			}
			tc.mutate(t, f)
			before := snapshotTestnetFiles(t, f.home)
			cmd := f.command(t, inPlaceTestnetCommandName)
			err := preflightTestnetCommand(cmd, []string{"fork", f.operator})
			require.ErrorContains(t, err, tc.message)
			require.Nil(t, cmd.Context().Value(testnetPreflightContextKey{}))
			require.Equal(t, before, snapshotTestnetFiles(t, f.home))
			require.NoFileExists(t, filepath.Join(f.home, inPlaceTestnetMarker))
		})
	}
}

func TestTestnetPreflightLegacyRecoveryResponse(t *testing.T) {
	f := newTestnetPreflightFixture(t)
	f.state.LastBlockHeight = 2
	f.state.LastBlockID = f.blockIDs[2]
	f.state.AppHash = testnetPreflightCommitInfo(2).Hash()
	f.saveState(t)
	// Comet v0.37 responses have no AppHash and may omit BeginBlock/EndBlock.
	info := cmtstateproto.ABCIResponsesInfo{Height: 3, LegacyAbciResponses: &cmtstateproto.LegacyABCIResponses{DeliverTxs: []*abci.ExecTxResult{{Code: 0}}}}
	value, err := info.Marshal()
	require.NoError(t, err)
	f.writeRecord(t, "state", "lastABCIResponseKey", value)
	before := snapshotTestnetFiles(t, f.home)
	cmd := f.command(t, inPlaceTestnetCommandName)
	require.NoError(t, preflightTestnetCommand(cmd, []string{"fork", f.operator}))
	require.NotNil(t, cmd.Context().Value(testnetPreflightContextKey{}))
	require.Equal(t, before, snapshotTestnetFiles(t, f.home))
	require.NoFileExists(t, filepath.Join(f.home, inPlaceTestnetMarker))
}
