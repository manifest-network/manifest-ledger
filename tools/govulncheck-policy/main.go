// Command govulncheck-policy runs govulncheck with the repository's narrow,
// fail-closed vulnerability policy.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
)

const (
	protocolVersion           = "v1.0.0"
	defaultScanPattern        = "./..."
	testScanFlag              = "-test"
	interchaintestScanPattern = "./interchaintest/..."
	interchaintestProfile     = "interchaintest"
	dockerClientVersion       = "v27.5.1+incompatible"
)

type exception struct {
	id      string
	module  string
	version string
	profile string
	// packagePrefixes restricts an exception to explicitly reviewed package
	// families. An empty list permits every package in the exact module version.
	packagePrefixes []string
	reason          string
	url             string
}

var dockerClientPackagePrefixes = []string{
	"github.com/docker/docker/api",
	"github.com/docker/docker/errdefs",
	"github.com/docker/docker/internal/multierror",
	"github.com/moby/moby/client",
	"github.com/moby/moby/errdefs",
	"github.com/moby/moby/pkg/stdcopy",
}

var exceptions = []exception{
	{
		id:      "GO-2026-4513",
		module:  "github.com/shamaton/msgpack/v2",
		version: "v2.4.2",
		reason:  "upstream fixed this stale database record in v2.4.1",
		url:     "https://github.com/golang/vulndb/issues/5034",
	},
	{
		id:      "GO-2026-4740",
		module:  "github.com/shamaton/msgpack/v2",
		version: "v2.4.2",
		reason:  "upstream fixed this stale database record in v2.4.1",
		url:     "https://github.com/golang/vulndb/issues/5034",
	},
	{
		id:              "GO-2026-4883",
		module:          "github.com/docker/docker",
		version:         dockerClientVersion,
		profile:         interchaintestProfile,
		packagePrefixes: dockerClientPackagePrefixes,
		reason:          "advisory affects Docker Engine plugin validation; this graph imports only reviewed client packages, not the daemon",
		url:             "https://github.com/moby/moby/security/advisories/GHSA-pxq6-2prw-chj9",
	},
	{
		id:              "GO-2026-4883",
		module:          "github.com/moby/moby",
		version:         dockerClientVersion,
		profile:         interchaintestProfile,
		packagePrefixes: dockerClientPackagePrefixes,
		reason:          "advisory affects Docker Engine plugin validation; this graph imports only reviewed client packages, not the daemon",
		url:             "https://github.com/moby/moby/security/advisories/GHSA-pxq6-2prw-chj9",
	},
	{
		id:              "GO-2026-4887",
		module:          "github.com/docker/docker",
		version:         dockerClientVersion,
		profile:         interchaintestProfile,
		packagePrefixes: dockerClientPackagePrefixes,
		reason:          "advisory affects Docker Engine AuthZ plugins; this graph imports only reviewed client packages, not the daemon",
		url:             "https://github.com/moby/moby/security/advisories/GHSA-x744-4wpc-v9h2",
	},
	{
		id:              "GO-2026-4887",
		module:          "github.com/moby/moby",
		version:         dockerClientVersion,
		profile:         interchaintestProfile,
		packagePrefixes: dockerClientPackagePrefixes,
		reason:          "advisory affects Docker Engine AuthZ plugins; this graph imports only reviewed client packages, not the daemon",
		url:             "https://github.com/moby/moby/security/advisories/GHSA-x744-4wpc-v9h2",
	},
}

type message struct {
	Config  *config  `json:"config,omitempty"`
	Finding *finding `json:"finding,omitempty"`
}

type config struct {
	ProtocolVersion string `json:"protocol_version"`
	ScanLevel       string `json:"scan_level"`
}

type finding struct {
	OSV   string  `json:"osv"`
	Trace []frame `json:"trace"`
}

type frame struct {
	Module   string `json:"module"`
	Version  string `json:"version"`
	Package  string `json:"package"`
	Function string `json:"function"`
	Receiver string `json:"receiver"`
}

type result struct {
	allowed []acceptedFinding
	blocked []finding
}

type acceptedFinding struct {
	finding   finding
	exception exception
}

func main() {
	govulncheck := flag.String("govulncheck", "govulncheck", "path to the govulncheck binary")
	scanGOOS := flag.String("goos", "", "GOOS used by the scanner (defaults to the host value)")
	scanGOARCH := flag.String("goarch", "", "GOARCH used by the scanner (defaults to the host value)")
	profile := flag.String("profile", "", "narrow exception profile for the selected build graph")
	flag.Parse()

	patterns := flag.Args()
	if len(patterns) == 0 {
		patterns = []string{defaultScanPattern}
	}

	var scanEnv []string
	if *scanGOOS != "" {
		scanEnv = append(scanEnv, "GOOS="+*scanGOOS)
	}
	if *scanGOARCH != "" {
		scanEnv = append(scanEnv, "GOARCH="+*scanGOARCH)
	}

	if err := run(*govulncheck, patterns, scanEnv, *profile, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "govulncheck policy: %v\n", err)
		os.Exit(1)
	}
}

func run(govulncheck string, patterns, scanEnv []string, profile string, stdout, stderr io.Writer) error {
	if err := validateProfile(profile, patterns); err != nil {
		return err
	}

	var report bytes.Buffer
	scanArgs := append([]string{"-format=json"}, patterns...)
	scan := exec.Command(govulncheck, scanArgs...) // #nosec G204 -- the caller explicitly selects the installed scanner binary.
	if len(scanEnv) > 0 {
		scan.Env = append(scan.Environ(), scanEnv...)
	}
	scan.Stdout = &report
	scan.Stderr = stderr
	if err := scan.Run(); err != nil {
		return fmt.Errorf("run scanner: %w", err)
	}

	reportBytes := report.Bytes()
	policyResult, err := evaluateWithProfile(bytes.NewReader(reportBytes), profile)
	if err != nil {
		return fmt.Errorf("evaluate scanner report: %w", err)
	}

	convert := exec.Command(govulncheck, "-mode=convert") // #nosec G204 -- see the scanner invocation above.
	convert.Stdin = bytes.NewReader(reportBytes)
	convert.Stdout = stdout
	convert.Stderr = stderr
	if err := convert.Run(); err != nil && !isFindingsExit(err) {
		return fmt.Errorf("render scanner report: %w", err)
	}

	if len(policyResult.allowed) > 0 {
		if err := writeAccepted(stdout, policyResult.allowed); err != nil {
			return fmt.Errorf("write accepted vulnerability exceptions: %w", err)
		}
	}

	if len(policyResult.blocked) > 0 {
		return fmt.Errorf("actionable vulnerabilities found: %s", strings.Join(summarize(policyResult.blocked), ", "))
	}

	return nil
}

func validateProfile(profile string, patterns []string) error {
	switch profile {
	case "":
		return nil
	case interchaintestProfile:
		required := []string{testScanFlag, interchaintestScanPattern}
		if !slices.Equal(patterns, required) {
			return fmt.Errorf(
				"profile %q requires exact scanner arguments %q, got %q",
				profile,
				required,
				patterns,
			)
		}
		return nil
	default:
		return fmt.Errorf("unknown vulnerability exception profile %q", profile)
	}
}

func writeAccepted(output io.Writer, findings []acceptedFinding) error {
	if _, err := fmt.Fprintln(output, "\nAccepted exact-version vulnerability exceptions:"); err != nil {
		return err
	}
	for _, summary := range summarizeAccepted(findings) {
		if _, err := fmt.Fprintf(output, "  %s\n", summary); err != nil {
			return err
		}
	}

	return nil
}

func evaluate(input io.Reader) (result, error) {
	return evaluateWithProfile(input, "")
}

func evaluateWithProfile(input io.Reader, profile string) (result, error) {
	decoder := json.NewDecoder(input)
	var (
		gotConfig bool
		out       result
	)

	for {
		var msg message
		if err := decoder.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return result{}, fmt.Errorf("decode JSON stream: %w", err)
		}

		if msg.Config != nil {
			if gotConfig {
				return result{}, errors.New("multiple scanner configuration messages")
			}
			gotConfig = true
			if msg.Config.ProtocolVersion != protocolVersion {
				return result{}, fmt.Errorf("unsupported protocol version %q", msg.Config.ProtocolVersion)
			}
			if msg.Config.ScanLevel != "symbol" {
				return result{}, fmt.Errorf("scanner must use symbol scan level, got %q", msg.Config.ScanLevel)
			}
		}

		if msg.Finding == nil || !msg.Finding.isSymbolFinding() {
			continue
		}
		if msg.Finding.OSV == "" {
			return result{}, errors.New("symbol finding has no vulnerability ID")
		}

		if accepted := msg.Finding.acceptedException(profile); accepted != nil {
			out.allowed = append(out.allowed, acceptedFinding{finding: *msg.Finding, exception: *accepted})
		} else {
			out.blocked = append(out.blocked, *msg.Finding)
		}
	}

	if !gotConfig {
		return result{}, errors.New("scanner report has no configuration message")
	}

	return out, nil
}

func (f finding) isSymbolFinding() bool {
	return len(f.Trace) > 0 && f.Trace[0].Function != ""
}

func (f finding) acceptedException(profile string) *exception {
	if len(f.Trace) == 0 {
		return nil
	}

	vulnerable := f.Trace[0]
	for i := range exceptions {
		candidate := &exceptions[i]
		if candidate.profile != "" && candidate.profile != profile {
			continue
		}
		if f.OSV == candidate.id &&
			vulnerable.Module == candidate.module &&
			vulnerable.Version == candidate.version &&
			matchesPackagePrefix(vulnerable.Package, candidate.packagePrefixes) {
			return candidate
		}
	}

	return nil
}

func matchesPackagePrefix(packageName string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	for _, prefix := range prefixes {
		if packageName == prefix || strings.HasPrefix(packageName, prefix+"/") {
			return true
		}
	}
	return false
}

func summarize(findings []finding) []string {
	unique := make(map[string]struct{}, len(findings))
	for _, current := range findings {
		vulnerable := current.Trace[0]
		key := fmt.Sprintf("%s in %s@%s", current.OSV, vulnerable.Module, vulnerable.Version)
		unique[key] = struct{}{}
	}

	summaries := make([]string, 0, len(unique))
	for summary := range unique {
		summaries = append(summaries, summary)
	}
	sort.Strings(summaries)

	return summaries
}

func summarizeAccepted(findings []acceptedFinding) []string {
	unique := make(map[string]struct{}, len(findings))
	for _, current := range findings {
		accepted := current.exception
		key := fmt.Sprintf("%s in %s@%s — %s (%s)", accepted.id, accepted.module, accepted.version, accepted.reason, accepted.url)
		unique[key] = struct{}{}
	}

	summaries := make([]string, 0, len(unique))
	for summary := range unique {
		summaries = append(summaries, summary)
	}
	sort.Strings(summaries)

	return summaries
}

func isFindingsExit(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 3
}
