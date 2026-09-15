package scripts_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The interchaintest host client has dependencies and replacements that must
// not change the daemon graph used by ordinary development commands.
func TestRootWorkspaceMatchesShippedDependencyGraph(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	require.NoError(t, err)

	const moduleTemplate = "{{if .Module}}{{.Module.Path}} {{.Module.Version}}{{if .Module.Replace}} => {{.Module.Replace.Path}} {{.Module.Replace.Version}}{{end}}{{end}}"
	modules := func(workspace string) []string {
		t.Helper()
		command := exec.Command("go", "list", "-mod=readonly", "-deps", "-tags=netgo,muslc,ledger", "-f", moduleTemplate, "./cmd/manifestd")
		command.Dir = repoRoot
		command.Env = append(os.Environ(), "GOWORK="+workspace)
		var stderr bytes.Buffer
		command.Stderr = &stderr
		output, err := command.Output()
		require.NoError(t, err, "daemon dependencies with GOWORK=%s: %s", workspace, stderr.String())
		result := strings.Split(strings.TrimSpace(string(output)), "\n")
		result = slices.DeleteFunc(result, func(line string) bool { return line == "" })
		slices.Sort(result)
		result = slices.Compact(result)
		require.NotEmpty(t, result, "daemon dependencies must include module metadata")
		return result
	}

	require.Equal(t, modules("off"), modules(filepath.Join(repoRoot, "go.work")),
		"root workspace must preserve the shipped daemon's module versions and replacements")
}
