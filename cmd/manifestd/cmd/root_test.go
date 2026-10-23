package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"

	"github.com/manifest-network/manifest-ledger/app"
)

// rootTempHomeChildEnv selects the child half of TestRootCmdTempHome.
const rootTempHomeChildEnv = "MANIFEST_ROOT_TEMP_HOME_CHILD"

// TestRootCmdTempHome builds the root command in a child process whose HOME is
// a scratch directory. app.DefaultNodeHome is fixed at package init, so only a
// child can point it away from the developer's real node home, which a
// regression here would delete.
func TestRootCmdTempHome(t *testing.T) {
	switch os.Getenv(rootTempHomeChildEnv) {
	case "unusable":
		require.Equal(t, os.Getenv("HOME"), filepath.Dir(app.DefaultNodeHome))
		root := NewRootCmd()
		root.SetArgs([]string{"version", "--home", t.TempDir()})
		require.ErrorContains(t, svrcmd.Execute(root, "MANIFESTD", app.DefaultNodeHome), "create a temporary home for the encoding app")
		return
	case "usable":
		require.Equal(t, os.Getenv("HOME"), filepath.Dir(app.DefaultNodeHome))
		NewRootCmd()
		entries, err := os.ReadDir(os.Getenv("TMPDIR"))
		require.NoError(t, err)
		require.Empty(t, entries, "the encoding app's temporary home outlived NewRootCmd")
		return
	}

	for _, tc := range []struct {
		name, mode string
		tmpdir     func(t *testing.T, fakeHome string) string
	}{
		{"missing TMPDIR", "unusable", func(_ *testing.T, fakeHome string) string {
			return filepath.Join(fakeHome, "missing")
		}},
		{"TMPDIR is a file", "unusable", func(t *testing.T, fakeHome string) string {
			file := filepath.Join(fakeHome, "file")
			require.NoError(t, os.WriteFile(file, nil, 0o600))
			return file
		}},
		{"usable TMPDIR", "usable", func(t *testing.T, fakeHome string) string {
			dir := filepath.Join(fakeHome, "tmp")
			require.NoError(t, os.Mkdir(dir, 0o700))
			return dir
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeHome := t.TempDir()
			nodeHome := filepath.Join(fakeHome, filepath.Base(app.DefaultNodeHome))
			files := map[string][]byte{
				filepath.Join(nodeHome, "config", "priv_validator_key.json"): []byte(`{"key":"kept"}`),
				filepath.Join(nodeHome, "data", "priv_validator_state.json"): []byte(`{"height":"9","round":0,"step":0}`),
			}
			for path, content := range files {
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
				require.NoError(t, os.WriteFile(path, content, 0o600))
			}

			child := exec.Command(os.Args[0], "-test.run=^TestRootCmdTempHome$", "-test.count=1") // #nosec G204 -- re-runs this test binary
			// Later duplicates win, so these replace the inherited values.
			child.Env = append(os.Environ(),
				"HOME="+fakeHome, "TMPDIR="+tc.tmpdir(t, fakeHome), rootTempHomeChildEnv+"="+tc.mode)
			output, err := child.CombinedOutput()
			// Report the child's failure, then still check what it left behind.
			assert.NoError(t, err, "%s", output)

			for path, content := range files {
				got, err := os.ReadFile(path)
				require.NoError(t, err, "the default node home lost %s", path)
				require.Equal(t, content, got)
			}
		})
	}
}
