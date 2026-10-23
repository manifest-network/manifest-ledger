package app

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"

	sdk "github.com/cosmos/cosmos-sdk/types"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
)

// TestSnapshotRestoresWasmCode checks that a state-sync snapshot carries contract
// bytecode, which lives under wasm/ rather than in the IAVL stores.
func TestSnapshotRestoresWasmCode(t *testing.T) {
	_, src := Setup(t)
	code, err := os.ReadFile("../scripts/cw_template.wasm")
	require.NoError(t, err)

	// Setup commits without the genesis state, so write the Wasm params and
	// code directly and commit them as the next version.
	ctx := src.NewUncachedContext(false, tmproto.Header{ChainID: SimAppChainID, Height: src.LastBlockHeight() + 1, Time: time.Now().UTC()})
	require.NoError(t, src.WasmKeeper.SetParams(ctx, wasmtypes.DefaultParams()))
	creator := sdk.AccAddress("creator_____________")
	codeID, checksum, err := wasmkeeper.NewGovPermissionKeeper(&src.WasmKeeper).Create(ctx, creator, code, nil)
	require.NoError(t, err)
	commit := src.CommitMultiStore().Commit()
	require.Positive(t, commit.Version)

	snapshot, err := src.SnapshotManager().Create(uint64(commit.Version)) // #nosec G115 -- checked positive above
	require.NoError(t, err)

	dst := newTestApp(t)
	require.NoError(t, dst.SnapshotManager().Restore(*snapshot))
	for i := uint32(0); i < snapshot.Chunks; i++ {
		chunk, err := src.SnapshotManager().LoadChunk(snapshot.Height, snapshot.Format, i)
		require.NoError(t, err)
		done, err := dst.SnapshotManager().RestoreChunk(chunk)
		require.NoError(t, err)
		if done {
			break
		}
	}
	require.Equal(t, src.LastBlockHeight(), dst.LastBlockHeight())

	dstCtx := dst.NewUncachedContext(false, tmproto.Header{ChainID: SimAppChainID, Height: dst.LastBlockHeight() + 1})
	info := dst.WasmKeeper.GetCodeInfo(dstCtx, codeID)
	require.NotNil(t, info)
	require.Equal(t, checksum, info.CodeHash)
	restored, err := dst.WasmKeeper.GetByteCode(dstCtx, codeID)
	require.NoError(t, err)
	require.Equal(t, code, restored)
}
