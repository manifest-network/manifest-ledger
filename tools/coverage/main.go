// Command coverage validates and merges Go profiles and identifies changed
// executable statements using Go's parser rather than coverage-block weights.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

const (
	statementsCommand = "statements"
	mergeCommand      = "merge"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, input io.Reader, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: coverage statements | coverage merge -output OUTPUT PROFILE [PROFILE]")
	}
	switch args[0] {
	case statementsCommand:
		if len(args) != 1 {
			return errors.New("statements accepts a JSON array of {path, source} on stdin")
		}
		decoder := json.NewDecoder(input)
		decoder.DisallowUnknownFields()
		var sources []sourceFile
		if err := decoder.Decode(&sources); err != nil {
			return fmt.Errorf("decode sources: %w", err)
		}
		if sources == nil {
			return errors.New("sources must be a JSON array")
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			return errors.New("unexpected trailing JSON after sources")
		}
		results := make([]statementFile, 0, len(sources))
		seen := make(map[string]bool, len(sources))
		for _, source := range sources {
			if source.Path == "" || seen[source.Path] {
				return fmt.Errorf("empty or duplicate source path: %q", source.Path)
			}
			seen[source.Path] = true
			statements, err := sourceStatements(source)
			if err != nil {
				return fmt.Errorf("%s: %w", source.Path, err)
			}
			results = append(results, statementFile{Path: source.Path, Statements: statements})
		}
		return json.NewEncoder(output).Encode(results)
	case mergeCommand:
		flags := flag.NewFlagSet(mergeCommand, flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		var destination string
		flags.StringVar(&destination, "output", "", "merged profile destination")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if destination == "" || flags.NArg() == 0 {
			return errors.New("merge requires -output OUTPUT and at least one coverage profile")
		}
		merged, err := mergeProfiles(flags.Args())
		if err != nil {
			return err
		}
		return os.WriteFile(destination, merged, 0o600)
	default:
		return fmt.Errorf("unknown coverage subcommand %q", args[0])
	}
}
