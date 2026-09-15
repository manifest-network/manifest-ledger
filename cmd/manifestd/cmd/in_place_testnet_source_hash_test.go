package cmd

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTestnetPreflightSourceApplicationHash(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		stateHeight, storeHeight int64
		mismatch, allowed        bool
	}{
		{"aligned commits", 3, 3, false, true},
		{"aligned height with mixed databases", 3, 3, true, false},
		{"stored halt block", 3, 4, false, true},
		{"stored halt block with mixed databases", 3, 4, true, false},
		{"app ahead requires SDK reconciliation", 2, 3, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newTestnetPreflightFixture(t)
			f.state.LastBlockHeight = tc.stateHeight
			if tc.mismatch {
				f.state.AppHash = bytes.Repeat([]byte{0x42}, 32)
			}
			f.saveState(t)
			f.saveBlockStore(t, tc.storeHeight)
			before := snapshotTestnetFiles(t, f.home)
			cmd := f.command(t, inPlaceTestnetCommandName)
			err := preflightTestnetCommand(cmd, []string{"fork", f.operator})
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "source application hash disagrees with Comet state")
				require.Nil(t, cmd.Context().Value(testnetPreflightContextKey{}))
			}
			require.Equal(t, before, snapshotTestnetFiles(t, f.home))
			require.NoFileExists(t, filepath.Join(f.home, inPlaceTestnetMarker))
		})
	}
}
