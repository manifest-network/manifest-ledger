package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReleaseTagValidation(t *testing.T) {
	valid := []string{
		"v0.0.0",
		"v2.4.0",
		"v2.4.0-rc.1",
		"v12.345.6789-0.alpha-1",
	}
	for _, tag := range valid {
		t.Run("valid_"+tag, func(t *testing.T) {
			_, err := runBuildInputValidator(t, nil, "release-tag", tag)
			require.NoError(t, err)
		})
	}

	invalid := []string{
		"2.4.0",
		"v02.4.0",
		"v2.04.0",
		"v2.4.00",
		"v2.4.0-01",
		"v2.4.0-",
		"v2.4.0-rc..1",
		"v2.4.0+metadata",
		"v2.4.0-rc.1+metadata",
		"v2.4.0/extra",
	}
	for _, tag := range invalid {
		t.Run("invalid_"+tag, func(t *testing.T) {
			_, err := runBuildInputValidator(t, nil, "release-tag", tag)
			require.Error(t, err)
		})
	}
}

func TestReleaseRefValidation(t *testing.T) {
	validEnvironment := []string{
		"RELEASE_REF=refs/tags/v2.4.0-rc.1",
		"RELEASE_REF_NAME=v2.4.0-rc.1",
		"RELEASE_REF_TYPE=tag",
	}
	_, err := runBuildInputValidator(t, validEnvironment, "release-ref")
	require.NoError(t, err)

	tests := []struct {
		name        string
		environment []string
	}{
		{
			name: "non-tag ref",
			environment: []string{
				"RELEASE_REF=refs/heads/v2.4.0",
				"RELEASE_REF_NAME=v2.4.0",
				"RELEASE_REF_TYPE=branch",
			},
		},
		{
			name: "ref name mismatch",
			environment: []string{
				"RELEASE_REF=refs/tags/v2.4.0",
				"RELEASE_REF_NAME=v2.4.1",
				"RELEASE_REF_TYPE=tag",
			},
		},
		{
			name: "noncanonical tag",
			environment: []string{
				"RELEASE_REF=refs/tags/v2.4.0+metadata",
				"RELEASE_REF_NAME=v2.4.0+metadata",
				"RELEASE_REF_TYPE=tag",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := runBuildInputValidator(t, tt.environment, "release-ref")
			require.Error(t, err)
		})
	}
}

func TestReleaseAndBuildCommandPayloadsRemainData(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "payload-executed")
	payload := "v2.4.0';touch " + marker + ";#"

	_, err := runBuildInputValidator(t, nil, "release-tag", payload)
	require.Error(t, err)
	require.NoFileExists(t, marker)

	_, err = runBuildInputValidator(t, nil, "build-command", "build;touch "+marker)
	require.Error(t, err)
	require.NoFileExists(t, marker)

	for _, command := range []string{"build", "build-cover"} {
		_, err = runBuildInputValidator(t, nil, "build-command", command)
		require.NoError(t, err)
	}
}

func TestMakeRejectsMetadataBeforeShellOrMakeExpansion(t *testing.T) {
	tests := []struct {
		name    string
		version func(string) string
		commit  func(string) string
		e2e     func(string) string
	}{
		{
			name:    "version shell syntax",
			version: shellPayload,
		},
		{
			name: "commit shell syntax",
			commit: func(marker string) string {
				return "abcdef0';touch " + marker + ";#"
			},
		},
		{
			name: "e2e shell syntax",
			e2e:  shellPayload,
		},
		{
			name: "version make function",
			version: func(marker string) string {
				return "$(shell touch " + marker + ")"
			},
		},
		{
			name: "commit make function",
			commit: func(marker string) string {
				return "$(shell touch " + marker + ")"
			},
		},
		{
			name: "e2e make function",
			e2e: func(marker string) string {
				return "$(shell touch " + marker + ")"
			},
		},
	}
	for _, tt := range tests {
		for _, source := range []struct {
			name        string
			commandLine bool
		}{
			{name: "command_line", commandLine: true},
			{name: "environment"},
		} {
			t.Run(tt.name+"_"+source.name, func(t *testing.T) {
				testDir := t.TempDir()
				marker := filepath.Join(testDir, "payload-executed")
				version := "v2.4.0-3-gabcdef0-dirty"
				commit := strings.Repeat("a", 40)
				e2eVersion := "e2e-" + strings.Repeat("b", 40)
				if tt.version != nil {
					version = tt.version(marker)
				}
				if tt.commit != nil {
					commit = tt.commit(marker)
				}
				if tt.e2e != nil {
					e2eVersion = tt.e2e(marker)
				}

				_, err := runMakeBuild(t, testDir, version, commit, e2eVersion, source.commandLine)
				require.Error(t, err)
				require.NoFileExists(t, marker)
			})
		}
	}
}

func TestMakePassesValidatedMetadataAsSingleLinkerArgument(t *testing.T) {
	testDir := t.TempDir()
	version := "v2.4.0-3-gabcdef0-dirty"
	commit := strings.Repeat("a", 40)
	e2eVersion := "e2e-" + strings.Repeat("b", 40)

	argsFile, err := runMakeBuild(t, testDir, version, commit, e2eVersion, false)
	require.NoError(t, err)
	// The file path is created by this test's fake Go command inside t.TempDir.
	contents, err := os.ReadFile(argsFile) //nolint:gosec
	require.NoError(t, err)
	args := strings.Split(strings.TrimSpace(string(contents)), "\n")
	ldflagsIndex := slices.Index(args, "-ldflags")
	require.NotEqual(t, -1, ldflagsIndex)
	require.Less(t, ldflagsIndex+1, len(args))
	ldflags := args[ldflagsIndex+1]
	require.Contains(t, ldflags, "version.Version="+version)
	require.Contains(t, ldflags, "version.Commit="+commit)
	require.NotContains(t, args, version,
		"validated version must remain inside the single ldflags argument")
}

func TestReleaseWorkflowAndDockerfileKeepValidationGates(t *testing.T) {
	repoRoot := filepath.Clean("..")
	// Both paths are fixed repository files, not user-controlled input.
	workflow, err := os.ReadFile(filepath.Join(repoRoot, ".github/workflows/release-bin.yaml")) //nolint:gosec
	require.NoError(t, err)
	workflowText := string(workflow)
	for _, job := range []string{
		"validate-build",
		"validate-unit-tests",
		"validate-simulations",
		"validate-e2e",
		"validate-codeql",
		"prepare",
		"publish-image",
		"publish-release",
	} {
		block := workflowJobBlock(t, workflowText, job)
		require.Contains(t, block, "needs:")
		require.Contains(t, block, "validate-release-ref")
	}

	dockerfile, err := os.ReadFile(filepath.Join(repoRoot, "Dockerfile")) //nolint:gosec
	require.NoError(t, err)
	dockerfileText := string(dockerfile)
	require.Contains(t, dockerfileText,
		`sh ./scripts/validate-build-inputs.sh build-command "${BUILD_CMD}"`)
	require.Contains(t, dockerfileText, `make "${BUILD_CMD}"`)
	require.NotContains(t, dockerfileText, `make "${BUILD_CMD}" "COMMIT=${COMMIT}"`)
}

func TestChainUpgradeTargetsBindTheTestedImageToSource(t *testing.T) {
	repoRoot := filepath.Clean("..")
	makefile, err := os.ReadFile(filepath.Join(repoRoot, "Makefile")) //nolint:gosec
	require.NoError(t, err)
	makefileText := string(makefile)

	require.Contains(t, makefileText, "ictest-chain-upgrade-local: local-image")
	require.Contains(t, makefileText, `test "$${CI:-}" = "true"`)
	require.Contains(t, makefileText, `actual_version="$$(printf "%s\n" "$$metadata" | jq -er .version)"`)
	require.Contains(t, makefileText, `actual_commit="$$(printf "%s\n" "$$metadata" | jq -er .commit)"`)
	require.Contains(t, makefileText, `--env "EXPECTED_VERSION=$${MANIFEST_E2E_IMAGE_VERSION}"`)
	require.Contains(t, makefileText, `--env "EXPECTED_COMMIT=$${MANIFEST_BUILD_COMMIT}"`)
	require.NotContains(t, makefileText, `$(MAKE) --no-print-directory verify-chain-upgrade-image`,
		"recursive Make drops the sanitized metadata variables")

	workflow, err := os.ReadFile(filepath.Join(repoRoot, ".github/workflows/e2e.yml")) //nolint:gosec
	require.NoError(t, err)
	workflowText := string(workflow)
	require.Contains(t, workflowText, "e2e-tests:\n    needs: build-docker")
	require.Contains(t, workflowText, `- "ictest-chain-upgrade"`)
}

func TestChainUpgradeImageVerificationPreservesEnvironmentMetadata(t *testing.T) {
	testDir := t.TempDir()
	dockerArgsFile := filepath.Join(testDir, "docker-args")
	upgradeVersionFile := filepath.Join(testDir, "upgrade-version")
	fakeDocker := filepath.Join(testDir, "docker")
	fakeGo := filepath.Join(testDir, "fake-go.sh")

	const fakeDockerSource = `#!/bin/sh
set -eu
: "${DOCKER_ARGS_FILE:?}"
printf '%s\n' "$@" > "$DOCKER_ARGS_FILE"
`
	const fakeGoSource = `#!/bin/sh
set -eu
if [ "${1-}" = env ] && [ "${2-}" = GOROOT ]; then
	printf '/tmp\n'
	exit 0
fi
if [ "${1-}" = list ]; then
	exit 0
fi
if [ "${1-}" = test ]; then
	: "${UPGRADE_VERSION_FILE:?}"
	printf '%s\n' "${MANIFEST_UPGRADE_VERSION:?}" > "$UPGRADE_VERSION_FILE"
	exit 0
fi
printf '%s\n' "unexpected fake Go invocation: $*" >&2
exit 1
`
	require.NoError(t, os.WriteFile(fakeDocker, []byte(fakeDockerSource), 0o700))
	require.NoError(t, os.WriteFile(fakeGo, []byte(fakeGoSource), 0o600))

	version := "v2.4.0-3-gabcdef0"
	commit := strings.Repeat("a", 40)
	e2eVersion := "e2e-" + strings.Repeat("b", 40)
	cmd := exec.Command("make", "--no-print-directory", "ictest-chain-upgrade", //nolint:gosec
		"GO=sh "+fakeGo, "LEDGER_ENABLED=false")
	cmd.Dir = filepath.Clean("..")
	cmd.Env = []string{
		"CI=true",
		"COMMIT=" + commit,
		"DOCKER_ARGS_FILE=" + dockerArgsFile,
		"E2E_IMAGE_VERSION=" + e2eVersion,
		"LC_ALL=C",
		"PATH=" + testDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"UPGRADE_VERSION_FILE=" + upgradeVersionFile,
		"VERSION=" + version,
	}
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)

	dockerArgs, err := os.ReadFile(dockerArgsFile) //nolint:gosec
	require.NoError(t, err)
	dockerArgList := strings.Split(strings.TrimSpace(string(dockerArgs)), "\n")
	require.Contains(t, dockerArgList, "EXPECTED_VERSION="+e2eVersion)
	require.Contains(t, dockerArgList, "EXPECTED_COMMIT="+commit)
	upgradeVersion, err := os.ReadFile(upgradeVersionFile) //nolint:gosec
	require.NoError(t, err)
	require.Equal(t, e2eVersion, strings.TrimSpace(string(upgradeVersion)))
}

func shellPayload(marker string) string {
	return "v2.4.0';touch " + marker + ";#"
}

func runBuildInputValidator(t *testing.T, environment []string, args ...string) ([]byte, error) {
	t.Helper()
	commandArgs := append([]string{"./validate-build-inputs.sh"}, args...)
	// The command is the checked-in validator; dynamic args are intentionally
	// adversarial test data and are never passed through a shell.
	cmd := exec.Command("sh", commandArgs...) //nolint:gosec
	cmd.Dir = "."
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "LC_ALL=C"}, environment...)
	return cmd.CombinedOutput()
}

func runMakeBuild(
	t *testing.T,
	testDir, version, commit, e2eVersion string,
	metadataOnCommandLine bool,
) (string, error) {
	t.Helper()
	repoRoot := filepath.Clean("..")
	fakeGo := filepath.Join(testDir, "fake-go.sh")
	argsFile := filepath.Join(testDir, "go-args")
	const fakeGoSource = `#!/bin/sh
set -eu
if [ "${VERSION+x}" = x ] || [ "${COMMIT+x}" = x ] || [ "${E2E_IMAGE_VERSION+x}" = x ]; then
	printf '%s\n' 'public metadata variables leaked to a recipe child' >&2
	exit 97
fi
if [ "${1-}" = env ] && [ "${2-}" = GOROOT ]; then
	printf '/tmp\n'
	exit 0
fi
if [ "${1-}" = list ]; then
	exit 0
fi
: "${ARGS_FILE:?}"
: > "$ARGS_FILE"
for argument do
	printf '%s\n' "$argument" >> "$ARGS_FILE"
done
`
	require.NoError(t, os.WriteFile(fakeGo, []byte(fakeGoSource), 0o600))

	makeArgs := []string{
		"--no-print-directory",
		"build",
		"GO=sh " + fakeGo,
		"LEDGER_ENABLED=false",
		"BUILD_DIR=" + filepath.Join(testDir, "build"),
	}
	metadata := []string{
		"VERSION=" + version,
		"COMMIT=" + commit,
		"E2E_IMAGE_VERSION=" + e2eVersion,
	}
	if metadataOnCommandLine {
		makeArgs = append(makeArgs, metadata...)
	}
	// The test deliberately launches Make with hostile variable values to prove
	// they remain data across the real Makefile boundary.
	cmd := exec.Command("make", makeArgs...) //nolint:gosec
	cmd.Dir = repoRoot
	cmd.Env = []string{
		"ARGS_FILE=" + argsFile,
		"PATH=" + os.Getenv("PATH"),
		"LC_ALL=C",
	}
	if !metadataOnCommandLine {
		cmd.Env = append(cmd.Env, metadata...)
	}
	_, err := cmd.CombinedOutput()
	return argsFile, err
}

func workflowJobBlock(t *testing.T, workflow, job string) string {
	t.Helper()
	startMarker := "\n  " + job + ":\n"
	start := strings.Index(workflow, startMarker)
	require.NotEqual(t, -1, start, "missing workflow job %s", job)
	start += len(startMarker)
	remainder := workflow[start:]
	end := len(remainder)
	for index, line := range strings.Split(remainder, "\n") {
		if index == 0 {
			continue
		}
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && strings.HasSuffix(line, ":") {
			lineOffset := strings.Index(remainder, "\n"+line+"\n")
			if lineOffset >= 0 {
				end = lineOffset
			}
			break
		}
	}
	return remainder[:end]
}
