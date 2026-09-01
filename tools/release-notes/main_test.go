package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunRendersDeterministicUpgradeDetails(t *testing.T) {
	arguments := []string{
		"--tag", "v2.4.0",
		"--commit", strings.Repeat("a", 40),
		"--target-release", "v2.4.0",
		"--source-release", "v2.3.1",
		"--billing-source-version", "2",
		"--sku-source-version", "1",
	}
	want := `## Upgrade Details

- **Upgrade Handler Name:** ` + "`v2.4.0`" + `
- **Source Commit:** ` + "`aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`" + `
- **Application Handler:** module-migration-only via Cosmos SDK RunMigrations
- **Target Module Versions:** billing 4; SKU 2
- **Expected Live Baseline:** ` + "`v2.3.1`" + ` with billing 2 and SKU 1
- **Registered Migration Path:** billing ` + "`2 → 3 → 4`" + `; SKU ` + "`1 → 2`" + `

The on-chain software-upgrade plan name must match the handler name above byte-for-byte. RunMigrations executes every registered intermediate migration; no version is skipped when moving from the live baseline to this binary.

Before scheduling the upgrade height, complete the [operator migration checklist](https://github.com/manifest-network/manifest-ledger/blob/v2.4.0/network/manifest-1/UPGRADES.md) against a copy of current network state.
`

	var first bytes.Buffer
	require.NoError(t, run(arguments, &first))
	require.Equal(t, want, first.String())

	var second bytes.Buffer
	require.NoError(t, run(arguments, &second))
	require.Equal(t, first.String(), second.String())
}

func TestRunRejectsUnsafeOrInconsistentMetadata(t *testing.T) {
	base := []string{
		"--tag", "v2.4.0",
		"--commit", strings.Repeat("a", 40),
		"--target-release", "v2.4.0",
		"--source-release", "v2.3.1",
		"--billing-source-version", "2",
		"--sku-source-version", "1",
	}
	tests := []struct {
		name      string
		arguments []string
	}{
		{name: "unsafe tag", arguments: replaceArgument(base, "--tag", "v2.4.0\nmalicious")},
		{name: "build metadata", arguments: replaceArgument(base, "--tag", "v2.4.0+rebuilt")},
		{name: "unsafe commit", arguments: replaceArgument(base, "--commit", "not-a-commit")},
		{name: "stale target", arguments: replaceArgument(base, "--tag", "v2.5.0")},
		{name: "prerelease target", arguments: replaceArgument(base, "--target-release", "v2.4.0-rc.1")},
		{name: "missing source version", arguments: replaceArgument(base, "--billing-source-version", "0")},
		{name: "future source version", arguments: replaceArgument(base, "--sku-source-version", "3")},
		{name: "positional argument", arguments: append(append([]string(nil), base...), "extra")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Error(t, run(test.arguments, &bytes.Buffer{}))
		})
	}
}

func replaceArgument(arguments []string, name, value string) []string {
	replaced := append([]string(nil), arguments...)
	for index := range replaced {
		if replaced[index] == name {
			replaced[index+1] = value
			return replaced
		}
	}
	panic("argument not found: " + name)
}
