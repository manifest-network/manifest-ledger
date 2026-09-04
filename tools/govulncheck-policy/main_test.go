package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const validConfig = `{"config":{"protocol_version":"v1.0.0","scan_level":"symbol"}}`

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestEvaluate(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		messages    []string
		profile     string
		wantAllowed int
		wantBlocked int
		wantErr     string
	}{
		{
			name:     "clean report",
			messages: []string{validConfig, `{"progress":{"message":"done"}}`},
		},
		{
			name: "exact fixed msgpack version is allowed",
			messages: []string{
				validConfig,
				`{"finding":{"osv":"GO-2026-4740","trace":[{"module":"github.com/shamaton/msgpack/v2","version":"v2.4.2","function":"init"}]}}`,
			},
			wantAllowed: 1,
		},
		{
			name: "package-only finding is not actionable",
			messages: []string{
				validConfig,
				`{"finding":{"osv":"GO-2026-4513","trace":[{"module":"github.com/shamaton/msgpack/v2","version":"v2.4.2","package":"github.com/shamaton/msgpack/v2"}]}}`,
			},
		},
		{
			name: "msgpack version drift fails closed",
			messages: []string{
				validConfig,
				`{"finding":{"osv":"GO-2026-4740","trace":[{"module":"github.com/shamaton/msgpack/v2","version":"v2.4.3","function":"init"}]}}`,
			},
			wantBlocked: 1,
		},
		{
			name: "new Docker symbol finding fails closed",
			messages: []string{
				validConfig,
				`{"finding":{"osv":"GO-2026-4887","trace":[{"module":"github.com/docker/docker","version":"v27.5.1+incompatible","function":"ContainerList"}]}}`,
			},
			wantBlocked: 1,
		},
		{
			name:    "Docker Engine advisory is allowed only for the client-only interchaintest graph",
			profile: "interchaintest",
			messages: []string{
				validConfig,
				`{"finding":{"osv":"GO-2026-4887","trace":[{"module":"github.com/docker/docker","version":"v27.5.1+incompatible","package":"github.com/docker/docker/api/types/container","function":"ContainerList"}]}}`,
			},
			wantAllowed: 1,
		},
		{
			name:    "Docker plugin daemon package fails closed in interchaintest profile",
			profile: "interchaintest",
			messages: []string{
				validConfig,
				`{"finding":{"osv":"GO-2026-4883","trace":[{"module":"github.com/docker/docker","version":"v27.5.1+incompatible","package":"github.com/docker/docker/daemon/pkg/plugin","function":"validatePrivileges"}]}}`,
			},
			wantBlocked: 1,
		},
		{
			name:    "Moby authorization daemon package fails closed in interchaintest profile",
			profile: "interchaintest",
			messages: []string{
				validConfig,
				`{"finding":{"osv":"GO-2026-4887","trace":[{"module":"github.com/moby/moby","version":"v27.5.1+incompatible","package":"github.com/moby/moby/pkg/authorization","function":"AuthZRequest"}]}}`,
			},
			wantBlocked: 1,
		},
		{
			name:    "Docker finding without package metadata fails closed",
			profile: "interchaintest",
			messages: []string{
				validConfig,
				`{"finding":{"osv":"GO-2026-4887","trace":[{"module":"github.com/docker/docker","version":"v27.5.1+incompatible","function":"ContainerList"}]}}`,
			},
			wantBlocked: 1,
		},
		{
			name:    "Docker client version drift fails closed in the interchaintest graph",
			profile: "interchaintest",
			messages: []string{
				validConfig,
				`{"finding":{"osv":"GO-2026-4887","trace":[{"module":"github.com/docker/docker","version":"v29.3.1+incompatible","package":"github.com/docker/docker/api/types/container","function":"ContainerList"}]}}`,
			},
			wantBlocked: 1,
		},
		{
			name: "new vulnerability fails closed",
			messages: []string{
				validConfig,
				`{"finding":{"osv":"GO-2099-0001","trace":[{"module":"example.com/module","version":"v1.0.0","function":"Vulnerable"}]}}`,
			},
			wantBlocked: 1,
		},
		{
			name:     "protocol drift fails closed",
			messages: []string{`{"config":{"protocol_version":"v2.0.0","scan_level":"symbol"}}`},
			wantErr:  "unsupported protocol version",
		},
		{
			name:     "non-symbol scan fails closed",
			messages: []string{`{"config":{"protocol_version":"v1.0.0","scan_level":"package"}}`},
			wantErr:  "scanner must use symbol scan level",
		},
		{
			name:     "missing config fails closed",
			messages: []string{`{"progress":{"message":"done"}}`},
			wantErr:  "no configuration message",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := evaluateWithProfile(strings.NewReader(strings.Join(tc.messages, "\n")), tc.profile)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}

			require.NoError(t, err)
			require.Len(t, got.allowed, tc.wantAllowed)
			require.Len(t, got.blocked, tc.wantBlocked)
		})
	}
}

func TestValidateProfile(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateProfile("", []string{defaultScanPattern}))
	require.NoError(t, validateProfile(interchaintestProfile, []string{testScanFlag, interchaintestScanPattern}))
	require.ErrorContains(t, validateProfile(interchaintestProfile, []string{testScanFlag, defaultScanPattern}), "requires exact scanner arguments")
	require.ErrorContains(t, validateProfile(interchaintestProfile, []string{testScanFlag, interchaintestScanPattern, defaultScanPattern}), "requires exact scanner arguments")
	require.ErrorContains(t, validateProfile("unknown", []string{defaultScanPattern}), "unknown vulnerability exception profile")
}

func TestSummarizeIsDeterministicAndDeduplicated(t *testing.T) {
	t.Parallel()

	findings := []finding{
		{OSV: "GO-2", Trace: []frame{{Module: "example.com/b", Version: "v2", Function: "B"}}},
		{OSV: "GO-1", Trace: []frame{{Module: "example.com/a", Version: "v1", Function: "A"}}},
		{OSV: "GO-2", Trace: []frame{{Module: "example.com/b", Version: "v2", Function: "OtherTrace"}}},
	}

	require.Equal(t, []string{
		"GO-1 in example.com/a@v1",
		"GO-2 in example.com/b@v2",
	}, summarize(findings))
}

func TestWriteAccepted(t *testing.T) {
	t.Parallel()

	accepted := []acceptedFinding{
		{exception: exception{id: "GO-2", module: "example.com/b", version: "v2", reason: "reason B", url: "https://example.com/b"}},
		{exception: exception{id: "GO-1", module: "example.com/a", version: "v1", reason: "reason A", url: "https://example.com/a"}},
		{exception: exception{id: "GO-2", module: "example.com/b", version: "v2", reason: "reason B", url: "https://example.com/b"}},
	}

	var output strings.Builder
	require.NoError(t, writeAccepted(&output, accepted))
	require.Equal(t, "\nAccepted exact-version vulnerability exceptions:\n"+
		"  GO-1 in example.com/a@v1 — reason A (https://example.com/a)\n"+
		"  GO-2 in example.com/b@v2 — reason B (https://example.com/b)\n", output.String())
	require.EqualError(t, writeAccepted(errorWriter{}, accepted), "write failed")
}

func TestRunAppliesPolicyWhenScannerReportsFindings(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the fake scanner is a POSIX shell script")
	}

	scanner := filepath.Join(t.TempDir(), "govulncheck")
	script := `#!/bin/sh
case "$1" in
  -format=json)
	[ "$GOOS" = linux ] || exit 4
	[ "$GOARCH" = arm64 ] || exit 5
    printf '%s\n' \
      '{"config":{"protocol_version":"v1.0.0","scan_level":"symbol"}}' \
      '{"finding":{"osv":"GO-2026-4740","trace":[{"module":"github.com/shamaton/msgpack/v2","version":"v2.4.2","function":"init"}]}}'
    exit 0
    ;;
  -mode=convert)
    cat
    exit 3
    ;;
esac
exit 2
`
	// The scanner fixture must be executable so run can invoke it directly.
	require.NoError(t, os.WriteFile(scanner, []byte(script), 0o700)) //nolint:gosec

	var stdout bytes.Buffer
	require.NoError(t, run(scanner, []string{defaultScanPattern}, []string{"GOOS=linux", "GOARCH=arm64"}, "", &stdout, &bytes.Buffer{}))
	require.Contains(t, stdout.String(), "Accepted exact-version vulnerability exceptions:")
}
