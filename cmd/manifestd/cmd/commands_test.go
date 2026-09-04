package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewRootCmdRemovesTemporaryApplicationHome(t *testing.T) {
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
