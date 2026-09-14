package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestProductionRootfsRequiresOnlyBinaryAndCertificates(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*testing.T, string) string
		wantError bool
	}{
		{name: "regular executable and nonempty certificate bundle"},
		{
			name: "unexpected regular file", wantError: true,
			mutate: func(t *testing.T, root string) string {
				require.NoError(t, os.WriteFile(filepath.Join(root, "extra"), []byte("unapproved"), 0o600))
				return root
			},
		},
		{
			name: "unexpected symlink", wantError: true,
			mutate: func(t *testing.T, root string) string {
				require.NoError(t, os.Symlink("usr/bin/manifestd", filepath.Join(root, "extra")))
				return root
			},
		},
		{
			name: "special file", wantError: true,
			mutate: func(t *testing.T, root string) string {
				require.NoError(t, unix.Mkfifo(filepath.Join(root, "extra"), 0o600))
				return root
			},
		},
		{
			name: "missing binary", wantError: true,
			mutate: func(t *testing.T, root string) string {
				require.NoError(t, os.Remove(filepath.Join(root, "usr/bin/manifestd")))
				return root
			},
		},
		{
			name: "missing certificates", wantError: true,
			mutate: func(t *testing.T, root string) string {
				require.NoError(t, os.Remove(filepath.Join(root, "etc/ssl/certs/ca-certificates.crt")))
				return root
			},
		},
		{
			name: "nonexecutable binary", wantError: true,
			mutate: func(t *testing.T, root string) string {
				require.NoError(t, os.Chmod(filepath.Join(root, "usr/bin/manifestd"), 0o600))
				return root
			},
		},
		{
			name: "empty certificates", wantError: true,
			mutate: func(t *testing.T, root string) string {
				require.NoError(t, os.Truncate(filepath.Join(root, "etc/ssl/certs/ca-certificates.crt"), 0))
				return root
			},
		},
		{
			name: "binary symlink to executable outside root", wantError: true,
			mutate: func(t *testing.T, root string) string {
				binary := filepath.Join(root, "usr/bin/manifestd")
				outside := filepath.Join(t.TempDir(), "manifestd")
				require.NoError(t, os.Rename(binary, outside))
				require.NoError(t, os.Symlink(outside, binary))
				return root
			},
		},
		{
			name: "certificate symlink to nonempty bundle outside root", wantError: true,
			mutate: func(t *testing.T, root string) string {
				bundle := filepath.Join(root, "etc/ssl/certs/ca-certificates.crt")
				outside := filepath.Join(t.TempDir(), "ca-certificates.crt")
				require.NoError(t, os.Rename(bundle, outside))
				require.NoError(t, os.Symlink(outside, bundle))
				return root
			},
		},
		{
			name: "directory symlink to files outside root", wantError: true,
			mutate: func(t *testing.T, root string) string {
				bin := filepath.Join(root, "usr/bin")
				outside := filepath.Join(t.TempDir(), "bin")
				require.NoError(t, os.Rename(bin, outside))
				require.NoError(t, os.Symlink(outside, bin))
				return root
			},
		},
		{
			name: "root directory symlink", wantError: true,
			mutate: func(t *testing.T, root string) string {
				link := filepath.Join(t.TempDir(), "rootfs")
				require.NoError(t, os.Symlink(root, link))
				return link
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(root, "usr/bin"), 0o700))
			require.NoError(t, os.MkdirAll(filepath.Join(root, "etc/ssl/certs"), 0o700))
			// The verifier inspects the filesystem shape. Neither fixture file is
			// interpreted or executed, and this check does not assert ELF or TLS validity.
			binary := filepath.Join(root, "usr/bin/manifestd")
			require.NoError(t, os.WriteFile(binary, []byte("binary fixture"), 0o600))
			require.NoError(t, os.Chmod(binary, 0o700)) //nolint:gosec // Owner-only execution is the property under test.
			require.NoError(t, os.WriteFile(filepath.Join(root, "etc/ssl/certs/ca-certificates.crt"), []byte("certificate fixture"), 0o600))
			if test.mutate != nil {
				root = test.mutate(t, root)
			}
			cmd := exec.Command("sh", "./verify-production-rootfs.sh", root) //nolint:gosec
			output, err := cmd.CombinedOutput()
			if test.wantError {
				require.Error(t, err, string(output))
			} else {
				require.NoError(t, err, string(output))
			}
		})
	}
}
