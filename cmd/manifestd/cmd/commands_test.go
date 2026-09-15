package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewRootCmdRemovesTemporaryApplicationHome(t *testing.T) {
	// Root command construction seals the process-global SDK configuration.
	// Exercise each invocation in a fresh process, as the daemon does, so this
	// test neither depends on package order nor breaks repeated test runs.
	const childProcess = "MANIFEST_ROOT_COMMAND_TEST_PROCESS"
	if os.Getenv(childProcess) != "1" {
		executable, err := os.Executable()
		require.NoError(t, err)
		args := []string{"-test.run=^TestNewRootCmdRemovesTemporaryApplicationHome$"}
		// Preserve this test binary's subprocess counters in coverage runs.
		if directory := flag.Lookup("test.gocoverdir"); directory != nil && directory.Value.String() != "" {
			args = append(args, "-test.gocoverdir="+directory.Value.String())
		}
		command := exec.CommandContext(t.Context(), executable, args...) //nolint:gosec // Relaunch this test binary with a fixed test selection.
		command.Env = append(os.Environ(), childProcess+"=1")
		output, err := command.CombinedOutput()
		require.NoError(t, err, "%s", output)
		return
	}

	originalTempDir := tempDir
	t.Cleanup(func() { tempDir = originalTempDir })

	tempHome := filepath.Join(t.TempDir(), "temporary-app-home")
	require.NoError(t, os.Mkdir(tempHome, 0o700))
	tempDir = func() string { return tempHome }

	rootCmd := NewRootCmd()
	require.NotNil(t, rootCmd)
	preflightCmd, remaining, err := rootCmd.Find([]string{"genesis", "preflight-billing-v4"})
	require.NoError(t, err)
	require.Equal(t, "preflight-billing-v4", preflightCmd.Name())
	require.Empty(t, remaining)
	require.NoDirExists(t, tempHome)
}

func TestTempDirPanicsWhenCreationFails(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing-parent"))

	var panicValue any
	func() {
		defer func() { panicValue = recover() }()
		_ = tempDir()
	}()

	require.Contains(t, fmt.Sprint(panicValue), "failed to create temporary application home:")
}
