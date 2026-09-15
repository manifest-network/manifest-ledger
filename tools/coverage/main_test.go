package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunStatementsProtocol(t *testing.T) {
	var output bytes.Buffer
	if err := run([]string{statementsCommand}, strings.NewReader(`[{"path":"a.go","source":"package a\nfunc A() { println(1) }\n"}]`), &output); err != nil {
		t.Fatal(err)
	}
	if want := "[{\"path\":\"a.go\",\"statements\":[{\"line\":2,\"column\":12,\"lines\":[2]}]}]\n"; output.String() != want {
		t.Fatalf("got %s, want %s", output.String(), want)
	}
	output.Reset()
	if err := run([]string{statementsCommand}, strings.NewReader("[]"), &output); err != nil || output.String() != "[]\n" {
		t.Fatalf("empty file list: %q, %v", output.String(), err)
	}
}

func TestRunRejectsInvalidArgumentsAndInput(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown"}, {statementsCommand, "extra"}, {mergeCommand}, {mergeCommand, "-unknown"}} {
		if err := run(args, strings.NewReader("[]"), io.Discard); err == nil {
			t.Fatalf("accepted arguments %v", args)
		}
	}
	for _, input := range []string{
		"", "null", "{}", "[] []", "[] invalid", `[{"path":"a.go","source":"package a","unknown":1}]`,
		`[{"source":"package a"}]`, `[{"path":"a.go","source":"invalid"}]`,
		`[{"path":"a.go","source":"package a"},{"path":"a.go","source":"package a"}]`,
	} {
		if err := run([]string{statementsCommand}, strings.NewReader(input), io.Discard); err == nil {
			t.Fatalf("accepted input %q", input)
		}
	}
	if err := run([]string{statementsCommand}, strings.NewReader("[]"), errorWriter{}); err == nil {
		t.Fatal("ignored output error")
	}
}

func TestRunMergeWritesOnlyValidatedProfile(t *testing.T) {
	directory := t.TempDir()
	input, output := filepath.Join(directory, "input.out"), filepath.Join(directory, "output.out")
	writeFixture(t, input, "mode: atomic\nexample.com/a/a.go:1.1,1.9 1 7\n")
	if err := run([]string{mergeCommand, "-output", output, input}, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output) //nolint:gosec // Output is inside this test's temporary directory.
	if err != nil || string(data) != "mode: atomic\nexample.com/a/a.go:1.1,1.9 1 1\n" {
		t.Fatalf("merged profile: %s, %v", data, err)
	}
	writeFixture(t, input, "invalid")
	if err := run([]string{mergeCommand, "-output", output, input}, nil, io.Discard); err == nil {
		t.Fatal("invalid profile accepted")
	}
	preserved, err := os.ReadFile(output) //nolint:gosec // Output is inside this test's temporary directory.
	if err != nil || !bytes.Equal(data, preserved) {
		t.Fatal("invalid input modified an existing output")
	}
	writeFixture(t, input, string(data))
	if err := run([]string{mergeCommand, "-output", directory, input}, nil, io.Discard); err == nil {
		t.Fatal("ignored output write failure")
	}
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func writeFixture(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil { //nolint:gosec // Callers supply paths inside their test-owned temporary directories.
		t.Fatal(err)
	}
}
