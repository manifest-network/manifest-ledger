package scripts_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProtobufContainerUsesHostOwnershipAndWritableHome(t *testing.T) {
	testDir := t.TempDir()
	dockerArguments := filepath.Join(testDir, "docker-arguments")
	fakeDocker := filepath.Join(testDir, "docker")
	fakeGo := filepath.Join(testDir, "go")

	writeExecutable(t, fakeDocker, `#!/bin/sh
set -eu
: "${PROTO_DOCKER_ARGUMENTS:?}"
printf '%s\n' "$@" > "$PROTO_DOCKER_ARGUMENTS"
`)
	writeExecutable(t, fakeGo, `#!/bin/sh
set -eu
if [ "${1-}" = env ] && [ "${2-}" = GOROOT ]; then
	printf '/tmp\n'
	exit 0
fi
if [ "${1-}" = list ]; then
	exit 0
fi
printf '%s\n' "unexpected fake Go invocation: $*" >&2
exit 1
`)

	cmd := exec.Command( //nolint:gosec // The command and arguments are fixed test fixtures.
		"make", "--no-print-directory", "proto-lint",
		"DOCKER="+fakeDocker, "GO="+fakeGo, "LEDGER_ENABLED=false",
	)
	cmd.Dir = filepath.Clean("..")
	cmd.Env = []string{
		"LC_ALL=C",
		"PATH=" + os.Getenv("PATH"),
		"PROTO_DOCKER_ARGUMENTS=" + dockerArguments,
	}
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)

	arguments, err := os.ReadFile(dockerArguments) //nolint:gosec // Fixed test-owned path.
	require.NoError(t, err)
	invocation := "\n" + string(arguments)
	require.Contains(t, invocation, fmt.Sprintf("\n--user\n%d:%d\n", os.Getuid(), os.Getgid()))
	require.Contains(t, invocation, "\n--env\nHOME=/tmp\n")
	require.Less(t, strings.Index(invocation, "\n--user\n"), strings.Index(invocation, "\n-v\n"))
}

func TestProtobufGenerationStartsCleanAndFailsWithoutOutput(t *testing.T) {
	fixtureRoot, scriptPath, binDir := newProtobufGenerationFixture(t)
	require.NoError(t, os.MkdirAll(
		filepath.Join(fixtureRoot, "github.com", "manifest-network", "manifest-ledger", "stale"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(fixtureRoot, "liftedinit", "stale", "module"), 0o700))
	writeExecutable(t, filepath.Join(binDir, "buf"), "#!/bin/sh\nexit 0\n")

	cmd := exec.Command("sh", scriptPath) //nolint:gosec
	cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, err := cmd.CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(output), "gogo protobuf generation produced no repository output")
	require.NoDirExists(t, filepath.Join(fixtureRoot, "github.com"),
		"stale gogo output must not make an empty generation pass")
	require.NoDirExists(t, filepath.Join(fixtureRoot, "liftedinit"),
		"stale pulsar output must not make an empty generation pass")
}

func TestProtobufGenerationCopiesOnlyFreshOutputs(t *testing.T) {
	fixtureRoot, scriptPath, binDir := newProtobufGenerationFixture(t)
	fakeBuf := `#!/bin/sh
set -eu
: "${FIXTURE_ROOT:?}"
case "$*" in
	*buf.gen.gogo.yaml*)
		output="$FIXTURE_ROOT/github.com/manifest-network/manifest-ledger/x/test"
		mkdir -p "$output"
		printf 'package test\n' > "$output/test.pb.go"
		;;
	*buf.gen.pulsar.yaml*)
		output="$FIXTURE_ROOT/liftedinit/test/module"
		mkdir -p "$output"
		printf 'package module\n' > "$output/module.pulsar.go"
		;;
	*)
		printf '%s\n' "unexpected buf invocation: $*" >&2
		exit 1
		;;
esac
`
	writeExecutable(t, filepath.Join(binDir, "buf"), fakeBuf)

	cmd := exec.Command("sh", scriptPath) //nolint:gosec
	cmd.Env = append(os.Environ(),
		"FIXTURE_ROOT="+fixtureRoot,
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.FileExists(t, filepath.Join(fixtureRoot, "x", "test", "test.pb.go"))
	require.FileExists(t, filepath.Join(fixtureRoot, "api", "liftedinit", "test", "module", "module.pulsar.go"))
	require.NoDirExists(t, filepath.Join(fixtureRoot, "github.com"))
	require.NoDirExists(t, filepath.Join(fixtureRoot, "liftedinit"))
}

func newProtobufGenerationFixture(t *testing.T) (fixtureRoot, scriptPath, binDir string) {
	t.Helper()
	fixtureRoot = t.TempDir()
	scriptDir := filepath.Join(fixtureRoot, "scripts")
	binDir = filepath.Join(fixtureRoot, "bin")
	protoDir := filepath.Join(fixtureRoot, "proto", "liftedinit", "test", "v1")
	require.NoError(t, os.MkdirAll(scriptDir, 0o700))
	require.NoError(t, os.MkdirAll(binDir, 0o700))
	require.NoError(t, os.MkdirAll(protoDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(fixtureRoot, "go.mod"), []byte("module example.test/protocgen\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(protoDir, "test.proto"), []byte(
		"syntax = \"proto3\";\noption go_package = \"github.com/manifest-network/manifest-ledger/x/test\";\n"), 0o600))

	script, err := os.ReadFile("protocgen.sh") //nolint:gosec
	require.NoError(t, err)
	scriptPath = filepath.Join(scriptDir, "protocgen.sh")
	require.NoError(t, os.WriteFile(scriptPath, script, 0o600)) //nolint:gosec // G703: both paths are rooted in the test's private temporary directory.
	return fixtureRoot, scriptPath, binDir
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o700)) //nolint:gosec // G306: callers use this helper exclusively to create executable test fixtures.
}
