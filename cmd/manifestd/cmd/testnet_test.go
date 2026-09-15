package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteFileAtomicallyReplacesExistingMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key_seed.json")
	require.NoError(t, os.WriteFile(path, []byte("old secret"), 0o644)) //nolint:gosec // Deliberately create the insecure legacy mode that writeFile must replace.
	require.NoError(t, os.Chmod(path, 0o644))                           //nolint:gosec // Deliberately restore the insecure mode even under a restrictive umask.

	require.NoError(t, writeFile(filepath.Base(path), dir, []byte("new secret")))

	contents, err := os.ReadFile(path) //nolint:gosec // path is constructed inside t.TempDir.
	require.NoError(t, err)
	require.Equal(t, []byte("new secret"), contents)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestWriteFileDoesNotFollowExistingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink replacement semantics differ on Windows")
	}

	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target.json")
	require.NoError(t, os.WriteFile(target, []byte("must remain unchanged"), 0o600))
	link := filepath.Join(dir, "key_seed.json")
	require.NoError(t, os.Symlink(target, link))

	require.NoError(t, writeFile(filepath.Base(link), dir, []byte("replacement secret")))

	targetContents, err := os.ReadFile(target) //nolint:gosec // target is constructed inside t.TempDir.
	require.NoError(t, err)
	require.Equal(t, []byte("must remain unchanged"), targetContents)
	linkInfo, err := os.Lstat(link)
	require.NoError(t, err)
	require.Zero(t, linkInfo.Mode()&os.ModeSymlink)
	replacementContents, err := os.ReadFile(link) //nolint:gosec // link is constructed inside t.TempDir.
	require.NoError(t, err)
	require.Equal(t, []byte("replacement secret"), replacementContents)
}
