package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	acceptedMsgpackFinding = `{"finding":{"osv":"GO-2026-4740","trace":[{"module":"github.com/shamaton/msgpack/v2","version":"v2.4.2","function":"init"}]}}`
	scannerJSONFlag        = "-format=json"
	githubAdvisoryPrefix   = "https://github.com/"
)

// The executable fixture checks both subprocess boundaries: convert must receive
// exactly the scanner's JSON, and invalid reports must prevent conversion.
func policyScannerFixture(t *testing.T, report string, scanStatus, convertStatus int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the scanner fixture is a POSIX shell executable")
	}
	scanner := filepath.Join(t.TempDir(), "govulncheck")
	require.NoError(t, os.WriteFile(scanner+".report", []byte(report), 0o600))
	script := fmt.Sprintf(`#!/bin/sh
set -eu
case "$1" in
  -format=json)
    printf 'scan\n' >> "$0.calls"
    printf '%%s\n' "$@" > "$0.args"
    printf '%%s\n' "${GOOS-}" "${GOARCH-}" > "$0.env"
    cat "$0.report"
    printf 'scanner diagnostic\n' >&2
    exit %d
    ;;
  -mode=convert)
    printf 'convert\n' >> "$0.calls"
    cmp - "$0.report" || exit 7
    printf 'rendered report\n'
    exit %d
    ;;
esac
exit 8
`, scanStatus, convertStatus)
	require.NoError(t, os.WriteFile(scanner, []byte(script), 0o700)) //nolint:gosec // Executable fixture below t.TempDir.
	return scanner
}

func TestMainSelectsScannerGraphAndPlatform(t *testing.T) {
	// main uses package-global CLI state. Keep these cases sequential and restore
	// every substituted global before the package's parallel policy tests run.
	for _, test := range []struct {
		name          string
		flags         []string
		patterns      []string
		platform      string
		finding       string
		wantException string
		wantURL       string
	}{
		{
			name:          "default graph inherits platform",
			patterns:      []string{scannerJSONFlag, "./..."},
			platform:      "inherited-os\ninherited-arch\n",
			finding:       acceptedMsgpackFinding,
			wantException: "GO-2026-4740 in github.com/shamaton/msgpack/v2@v2.4.2",
			wantURL:       githubAdvisoryPrefix + "golang/vulndb/issues/5034",
		},
		{
			name: "explicit reviewed graph overrides platform",
			flags: []string{
				"-goos", "linux", "-goarch", "arm64", "-profile", "interchaintest", "--", "-test", "./interchaintest/...",
			},
			patterns:      []string{scannerJSONFlag, "-test", "./interchaintest/..."},
			platform:      "linux\narm64\n",
			finding:       `{"finding":{"osv":"GO-2026-4887","trace":[{"module":"github.com/docker/docker","version":"v27.5.1+incompatible","package":"github.com/docker/docker/api/types/container","function":"ContainerList"}]}}`,
			wantException: "GO-2026-4887 in github.com/docker/docker@v27.5.1+incompatible",
			wantURL:       githubAdvisoryPrefix + "moby/moby/security/advisories/GHSA-x744-4wpc-v9h2",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			scanner := policyScannerFixture(t, validConfig+"\n"+test.finding+"\n", 0, 3)
			t.Setenv("GOOS", "inherited-os")
			t.Setenv("GOARCH", "inherited-arch")
			originalFlags, originalArgs := flag.CommandLine, os.Args
			originalStdout, originalStderr := os.Stdout, os.Stderr
			t.Cleanup(func() {
				flag.CommandLine, os.Args = originalFlags, originalArgs
				os.Stdout, os.Stderr = originalStdout, originalStderr
			})
			flag.CommandLine = flag.NewFlagSet("govulncheck-policy", flag.ContinueOnError)
			os.Args = append([]string{"govulncheck-policy", "-govulncheck", scanner}, test.flags...)
			stdout, err := os.CreateTemp(t.TempDir(), "stdout-*")
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, stdout.Close()) })
			stderr, err := os.CreateTemp(t.TempDir(), "stderr-*")
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, stderr.Close()) })
			os.Stdout, os.Stderr = stdout, stderr
			main()
			args, err := os.ReadFile(scanner + ".args") //nolint:gosec // Fixture below t.TempDir.
			require.NoError(t, err)
			require.Equal(t, test.patterns, strings.Split(strings.TrimSpace(string(args)), "\n"))
			platform, err := os.ReadFile(scanner + ".env") //nolint:gosec // Fixture below t.TempDir.
			require.NoError(t, err)
			require.Equal(t, test.platform, string(platform))
			_, err = stdout.Seek(0, io.SeekStart)
			require.NoError(t, err)
			output, err := io.ReadAll(stdout)
			require.NoError(t, err)
			require.Contains(t, string(output), "rendered report\n")
			require.Contains(t, string(output), test.wantException)
			require.Contains(t, string(output), test.wantURL)
			_, err = stderr.Seek(0, io.SeekStart)
			require.NoError(t, err)
			diagnostics, err := io.ReadAll(stderr)
			require.NoError(t, err)
			require.Equal(t, "scanner diagnostic\n", string(diagnostics))
		})
	}
}

func TestRunFailsClosedAtSubprocessBoundaries(t *testing.T) {
	for _, test := range []struct {
		name          string
		report        string
		scanStatus    int
		convertStatus int
		wantError     string
		wantCalls     string
		wantOutput    string
	}{
		{"scanner failure", validConfig, 9, 0, "run scanner: exit status 9", "scan\n", ""},
		{"invalid JSON", validConfig + "\n{", 0, 0, "evaluate scanner report: decode JSON stream", "scan\n", ""},
		{"converter failure", validConfig, 0, 2, "render scanner report: exit status 2", "scan\nconvert\n", "rendered report\n"},
		{"actionable report despite findings exit", validConfig + `
{"finding":{"osv":"GO-2099-0001","trace":[{"module":"example.com/unsafe","version":"v1.0.0","function":"Dangerous"}]}}
{"finding":{"osv":"GO-2099-0001","trace":[{"module":"example.com/unsafe","version":"v1.0.0","function":"Other"}]}}
`, 0, 3, "actionable vulnerabilities found: GO-2099-0001 in example.com/unsafe@v1.0.0", "scan\nconvert\n", "rendered report\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			scanner := policyScannerFixture(t, test.report, test.scanStatus, test.convertStatus)
			var output, diagnostics bytes.Buffer
			err := run(scanner, []string{defaultScanPattern}, nil, "", &output, &diagnostics)
			require.ErrorContains(t, err, test.wantError)
			calls, readErr := os.ReadFile(scanner + ".calls") //nolint:gosec // Fixture below t.TempDir.
			require.NoError(t, readErr)
			require.Equal(t, test.wantCalls, string(calls))
			require.Equal(t, test.wantOutput, output.String())
			require.Equal(t, "scanner diagnostic\n", diagnostics.String())
		})
	}
	t.Run("invalid profile rejected before scanner execution", func(t *testing.T) {
		scanner := policyScannerFixture(t, validConfig, 0, 0)
		var output bytes.Buffer
		err := run(scanner, []string{defaultScanPattern}, nil, interchaintestProfile, &output, io.Discard)
		require.ErrorContains(t, err, "requires exact scanner arguments")
		require.NoFileExists(t, scanner+".calls")
		require.Empty(t, output.String())
	})
	t.Run("missing scanner executable", func(t *testing.T) {
		var output bytes.Buffer
		err := run(filepath.Join(t.TempDir(), "missing"), []string{defaultScanPattern}, nil, "", &output, io.Discard)
		require.ErrorContains(t, err, "run scanner:")
		require.ErrorIs(t, err, os.ErrNotExist)
		require.Empty(t, output.String())
	})
	t.Run("accepted exception cannot conceal output failure", func(t *testing.T) {
		scanner := policyScannerFixture(t, validConfig+"\n"+acceptedMsgpackFinding, 0, 3)
		output := &policyLimitedWriter{remaining: len("rendered report\n")}
		err := run(scanner, []string{defaultScanPattern}, nil, "", output, io.Discard)
		require.ErrorIs(t, err, io.ErrClosedPipe)
		require.ErrorContains(t, err, "write accepted vulnerability exceptions:")
	})
	t.Run("renderer output failure is returned", func(t *testing.T) {
		scanner := policyScannerFixture(t, validConfig, 0, 0)
		err := run(scanner, []string{defaultScanPattern}, nil, "", &policyLimitedWriter{}, io.Discard)
		require.ErrorIs(t, err, io.ErrClosedPipe)
		require.ErrorContains(t, err, "render scanner report:")
	})
}

func TestEvaluateDiscardsPartialDecisionsOnMalformedStream(t *testing.T) {
	for _, malformed := range []string{
		`{`,
		validConfig,
		`{"finding":[]}`,
		`{"finding":{"trace":[{"module":"example.com/unsafe","version":"v1","function":"Dangerous"}]}}`,
	} {
		t.Run(malformed, func(t *testing.T) {
			report := validConfig + "\n" + acceptedMsgpackFinding + "\n" + malformed
			out, err := evaluate(strings.NewReader(report))
			require.Error(t, err)
			require.Empty(t, out.allowed, "a malformed report must not expose earlier policy acceptances")
			require.Empty(t, out.blocked)
		})
	}
}

func TestWriteAcceptedReturnsFailureAfterHeading(t *testing.T) {
	result, err := evaluate(strings.NewReader(validConfig + "\n" + acceptedMsgpackFinding))
	require.NoError(t, err)
	output := &policyLimitedWriter{remaining: len("\nAccepted exact-version vulnerability exceptions:\n")}
	require.ErrorIs(t, writeAccepted(output, result.allowed), io.ErrClosedPipe)
}

type policyLimitedWriter struct{ remaining int }

func (w *policyLimitedWriter) Write(data []byte) (int, error) {
	if len(data) > w.remaining {
		return 0, io.ErrClosedPipe
	}
	w.remaining -= len(data)
	return len(data), nil
}

func TestFindingsExitRequiresRealExitStatus(t *testing.T) {
	require.False(t, isFindingsExit(errors.New("exit status 3")))
}
