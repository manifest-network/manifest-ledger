// Command release-notes renders deterministic, release-specific upgrade notes.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	billingmodule "github.com/manifest-network/manifest-ledger/x/billing"
	skumodule "github.com/manifest-network/manifest-ledger/x/sku"
)

const prereleaseIdentifier = `(0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)`

var (
	releaseTagPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-` + prereleaseIdentifier + `(\.` + prereleaseIdentifier + `)*)?$`)
	commitPattern     = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)
)

type releaseConfig struct {
	tag                  string
	commit               string
	targetRelease        string
	sourceRelease        string
	billingSourceVersion uint64
	skuSourceVersion     uint64
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string, output io.Writer) error {
	flags := flag.NewFlagSet("release-notes", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	var config releaseConfig
	flags.StringVar(&config.tag, "tag", "", "canonical release tag")
	flags.StringVar(&config.commit, "commit", "", "source commit object ID")
	flags.StringVar(&config.targetRelease, "target-release", "", "stable target release")
	flags.StringVar(&config.sourceRelease, "source-release", "", "live source release")
	flags.Uint64Var(&config.billingSourceVersion, "billing-source-version", 0, "live billing module version")
	flags.Uint64Var(&config.skuSourceVersion, "sku-source-version", 0, "live SKU module version")
	if err := flags.Parse(arguments); err != nil {
		return fmt.Errorf("parse release-note arguments: %w", err)
	}
	if flags.NArg() != 0 {
		return errors.New("release-notes does not accept positional arguments")
	}
	if err := config.validate(); err != nil {
		return err
	}

	return render(output, config)
}

func (config releaseConfig) validate() error {
	if !releaseTagPattern.MatchString(config.tag) {
		return fmt.Errorf("release tag is not canonical SemVer: %q", config.tag)
	}
	if !commitPattern.MatchString(config.commit) {
		return errors.New("source commit must be a 7-64 character hexadecimal object ID")
	}
	if !releaseTagPattern.MatchString(config.targetRelease) || strings.Contains(config.targetRelease, "-") {
		return fmt.Errorf("target release must be a canonical stable SemVer tag: %q", config.targetRelease)
	}
	if config.tag != config.targetRelease && !strings.HasPrefix(config.tag, config.targetRelease+"-") {
		return fmt.Errorf("release tag %q does not match configured target %q", config.tag, config.targetRelease)
	}
	if !releaseTagPattern.MatchString(config.sourceRelease) {
		return fmt.Errorf("source release is not canonical SemVer: %q", config.sourceRelease)
	}
	if config.billingSourceVersion == 0 || config.billingSourceVersion > billingmodule.ConsensusVersion {
		return fmt.Errorf(
			"billing source version must be between 1 and target version %d",
			billingmodule.ConsensusVersion,
		)
	}
	if config.skuSourceVersion == 0 || config.skuSourceVersion > skumodule.ConsensusVersion {
		return fmt.Errorf(
			"SKU source version must be between 1 and target version %d",
			skumodule.ConsensusVersion,
		)
	}
	return nil
}

func render(output io.Writer, config releaseConfig) error {
	guideURL := fmt.Sprintf(
		"https://github.com/manifest-network/manifest-ledger/blob/%s/network/manifest-1/UPGRADES.md",
		config.tag,
	)
	_, err := fmt.Fprintf(output, `## Upgrade Details

- **Upgrade Handler Name:** %s
- **Source Commit:** %s
- **Application Handler:** module-migration-only via Cosmos SDK RunMigrations
- **Target Module Versions:** billing %d; SKU %d
- **Expected Live Baseline:** %s with billing %d and SKU %d
- **Registered Migration Path:** billing %s; SKU %s

The on-chain software-upgrade plan name must match the handler name above byte-for-byte. RunMigrations executes every registered intermediate migration; no version is skipped when moving from the live baseline to this binary.

Before scheduling the upgrade height, complete the [operator migration checklist](%s) against a copy of current network state.
`,
		code(config.tag),
		code(config.commit),
		billingmodule.ConsensusVersion,
		skumodule.ConsensusVersion,
		code(config.sourceRelease),
		config.billingSourceVersion,
		config.skuSourceVersion,
		code(versionPath(config.billingSourceVersion, billingmodule.ConsensusVersion)),
		code(versionPath(config.skuSourceVersion, skumodule.ConsensusVersion)),
		guideURL,
	)
	if err != nil {
		return fmt.Errorf("write release notes: %w", err)
	}
	return nil
}

func versionPath(source, target uint64) string {
	versions := make([]string, 0, target-source+1)
	for version := source; ; version++ {
		versions = append(versions, strconv.FormatUint(version, 10))
		if version == target {
			break
		}
	}
	return strings.Join(versions, " → ")
}

func code(value string) string {
	return "`" + value + "`"
}
