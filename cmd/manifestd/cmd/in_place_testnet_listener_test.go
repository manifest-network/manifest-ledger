package cmd

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	cmtcfg "github.com/cometbft/cometbft/config"
)

func TestTestnetPreflightRPCListenerList(t *testing.T) {
	for _, command := range []string{"in-place-testnet", "start"} {
		for _, tc := range []struct {
			name, prefix, suffix string
			unix                 bool
		}{
			{name: "single TCP", prefix: "tcp://127.0.0.1:26657"},
			{name: "multiple TCP", prefix: "tcp://127.0.0.1:26657, tcp://127.0.0.1:26658"},
			{name: "empty entries", prefix: " , tcp://127.0.0.1:26657, , "},
			{name: "single unix", unix: true},
			{name: "unix after TCP", prefix: "tcp://127.0.0.1:26657,", unix: true},
			{name: "unix before TCP", suffix: ",tcp://127.0.0.1:26657", unix: true},
			{name: "leading whitespace", prefix: "  ", unix: true},
			{name: "whitespace after comma", prefix: "tcp://127.0.0.1:26657,  ", suffix: "  , ", unix: true},
		} {
			t.Run(command+"/"+tc.name, func(t *testing.T) {
				f := newTestnetPreflightFixture(t)
				if command == "start" {
					f.completeFork(t)
				}
				outside := t.TempDir()
				listener := tc.prefix
				if tc.unix {
					listener += "unix://" + filepath.Join(outside, "rpc.sock")
				}
				f.config.RPC.ListenAddress = listener + tc.suffix
				cmtcfg.WriteConfigFile(filepath.Join(f.home, "config", "config.toml"), f.config)
				before, outsideBefore := snapshotTestnetFiles(t, f.home), snapshotTestnetFiles(t, outside)
				err := preflightTestnetCommand(f.command(t, command), []string{"fork", f.operator})
				if tc.unix {
					require.ErrorContains(t, err, "unix socket listeners are unsupported")
				} else {
					require.NoError(t, err)
				}
				require.Equal(t, before, snapshotTestnetFiles(t, f.home))
				require.Equal(t, outsideBefore, snapshotTestnetFiles(t, outside))
			})
		}
	}
}
