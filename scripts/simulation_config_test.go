package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	simcli "github.com/cosmos/cosmos-sdk/x/simulation/client/cli"
)

const (
	releaseSimulationSeed     int64 = 2507940531156952020
	releaseSimulationBlocks   int   = 100
	determinismReplaysPerSeed int   = 3
	coverageTestTimeout             = "2h"
)

func TestReleaseSimulationDefaultsArePinned(t *testing.T) {
	makefile, err := os.ReadFile(filepath.Join("..", "Makefile")) //nolint:gosec
	require.NoError(t, err)

	for _, testCase := range []struct {
		name     string
		variable string
		want     string
	}{
		{name: "block count", variable: "SIM_NUM_BLOCKS", want: strconv.Itoa(releaseSimulationBlocks)},
		{name: "enabled", variable: "SIM_ENABLED", want: "true"},
		{name: "commit", variable: "SIM_COMMIT", want: "true"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			assignment := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(testCase.variable) + `\s*\?=\s*(\S+)\s*$`)
			matches := assignment.FindAllSubmatch(makefile, -1)
			require.Len(t, matches, 1, "Makefile must define exactly one default for %s", testCase.variable)
			require.Equal(t, testCase.want, string(matches[0][1]),
				"release simulations must keep %s=%s", testCase.variable, testCase.want)
		})
	}
}

func TestReleaseSimulationSeedIsReproducible(t *testing.T) {
	makefile, err := os.ReadFile(filepath.Join("..", "Makefile")) //nolint:gosec
	require.NoError(t, err)

	seedAssignment := regexp.MustCompile(`(?m)^SIM_SEED\s*\?=\s*(-?[0-9]+)\s*$`)
	matches := seedAssignment.FindAllSubmatch(makefile, -1)
	require.Len(t, matches, 1, "Makefile must define exactly one default simulation seed")

	seed, err := strconv.ParseInt(string(matches[0][1]), 10, 64)
	require.NoError(t, err)
	require.Equal(t, releaseSimulationSeed, seed,
		"changing the release seed requires a complete three-replay, 100-block validation")
	require.NotEqual(t, int64(simcli.DefaultSeedValue), seed,
		"Cosmos SDK's default seed is a sentinel that TestAppStateDeterminism randomizes")

	makefileText := string(makefile)
	require.Regexp(t, `(?m)^COV_SIM_COMMON\s*=.*-Seed=\$\{SIM_SEED\}(?:\s|$)`, makefileText,
		"coverage simulations must use the reproducible release seed")
	require.NotContains(t, makefileText, `${COV_SIM_CMD} -test.run TestAppSimulationAfterImport ${COV_SIM_COMMON} > /dev/null`,
		"coverage simulation failures must preserve their diagnostic output")
	require.Contains(t, makefileText, `cat "$$log_file" >&2`,
		"coverage simulation failures must print their captured output")
}

func TestCoverageTimeoutAccommodatesInstrumentedE2ESuite(t *testing.T) {
	makefile, err := os.ReadFile(filepath.Join("..", "Makefile")) //nolint:gosec
	require.NoError(t, err)

	timeoutAssignment := regexp.MustCompile(`(?m)^COV_TEST_TIMEOUT\s*:=\s*(\S+)\s*$`)
	matches := timeoutAssignment.FindAllSubmatch(makefile, -1)
	require.Len(t, matches, 1, "Makefile must define exactly one coverage test timeout")
	require.Equal(t, coverageTestTimeout, string(matches[0][1]),
		"race- and coverage-instrumented e2e tests need a two-hour package timeout")
	require.Contains(t, string(makefile), `test -p 1 -timeout ${COV_TEST_TIMEOUT}`,
		"coverage must apply the dedicated timeout to its combined unit and e2e test command")
}

func TestRandomSimulationTargetsOverrideTheFixedSeed(t *testing.T) {
	makefile, err := os.ReadFile(filepath.Join("..", "Makefile")) //nolint:gosec
	require.NoError(t, err)

	for _, target := range []string{
		"sim-full-app",
		"sim-import-export",
		"sim-after-import",
		"sim-app-determinism",
	} {
		pattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(target) +
			`-random:\n\t\$\(MAKE\) ` + regexp.QuoteMeta(target) + ` SIM_SEED=\$\(SIM_RANDOM_SEED\)$`)
		require.Regexp(t, pattern, string(makefile), "%s-random must override the fixed seed", target)

		cmd := exec.Command("make", "--no-print-directory", "-n", target+"-random") //nolint:gosec
		cmd.Dir = ".."
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s: %s", target, output)

		seedArgument := regexp.MustCompile(`-Seed=([0-9]+)`).FindStringSubmatch(string(output))
		require.Len(t, seedArgument, 2, "%s-random must expand to a numeric seed: %s", target, output)
		_, err = strconv.ParseUint(seedArgument[1], 10, 32)
		require.NoError(t, err, "%s-random seed must fit in 32 bits", target)
	}
}

func TestDeterminismRunsThreeReplaysPerSeed(t *testing.T) {
	simulationTest, err := os.ReadFile(filepath.Join("..", "app", "sim_test.go")) //nolint:gosec
	require.NoError(t, err)

	replayAssignment := regexp.MustCompile(`(?m)^\s*numTimesToRunPerSeed\s*:=\s*([0-9]+)(?:\s*//.*)?$`)
	matches := replayAssignment.FindAllSubmatch(simulationTest, -1)
	require.Len(t, matches, 1,
		"app/sim_test.go must define exactly one determinism replay count")

	replays, err := strconv.Atoi(string(matches[0][1]))
	require.NoError(t, err)
	require.Equal(t, determinismReplaysPerSeed, replays,
		"release determinism validation must replay each seed three times")
}
