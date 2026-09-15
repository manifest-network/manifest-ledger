package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/cover"
)

func TestMergePreservesUncoveredBlocksAndDeduplicatesObservations(t *testing.T) {
	directory := t.TempDir()
	first, second := filepath.Join(directory, "unit.out"), filepath.Join(directory, "covdata.out")
	writeFixture(t, first, "mode: atomic\nexample.com/a/a.go:2.1,2.8 3 0\nexample.com/a/a.go:2.1,2.8 3 0\nexample.com/a/b.go:3.1,3.9 2 0\n")
	writeFixture(t, second, "mode: atomic\nexample.com/a/a.go:2.1,2.8 3 42\nexample.com/a/a.go:2.8,2.8 0 2\n")
	merged, err := mergeProfiles([]string{first, second})
	if err != nil {
		t.Fatal(err)
	}
	want := "mode: atomic\nexample.com/a/a.go:2.1,2.8 3 1\nexample.com/a/a.go:2.8,2.8 0 1\nexample.com/a/b.go:3.1,3.9 2 0\n"
	if string(merged) != want {
		t.Fatalf("merged profile:\n%s\nwant:\n%s", merged, want)
	}
}

func TestMergeRejectsInvalidOrIncompatibleProfiles(t *testing.T) {
	directory := t.TempDir()
	first, second := filepath.Join(directory, "first.out"), filepath.Join(directory, "second.out")
	writeFixture(t, first, "mode: atomic\nexample.com/a/a.go:2.1,3.9 1 0\n")
	for _, input := range []string{
		"", "mode: unknown\n", "mode: atomic\ninvalid\n", "mode: set\nexample.com/a/a.go:2.1,3.9 1 1\n",
		"mode: atomic\nexample.com/a/a.go:2.1,3.9 2 1\n",
		"mode: atomic\nexample.com/a/a.go:2.2,4.1 1 1\n",
		"mode: atomic\nexample.com/a/a.go:0.1,3.9 1 1\n",
		"mode: atomic\nexample.com/a/a.go:2.0,3.9 1 1\n",
		"mode: atomic\nexample.com/a/a.go:4.1,3.9 1 1\n",
		"mode: atomic\nexample.com/a/a.go:2.1,2.1 1 1\n",
		"mode: atomic\nexample.com/a/a.go:2.1,3.9 1 -1\n",
		"mode: atomic\n../a.go:2.1,3.9 1 1\n",
		"mode: atomic\n/a.go:2.1,3.9 1 1\n",
		"mode: atomic\nexample.com//a.go:2.1,3.9 1 1\n",
		"mode: atomic\nexample.com/a.txt:2.1,3.9 1 1\n",
		"mode: atomic\nexample.com/a/a.go:2.1,3.9 1 0\nexample.com/a/a.go:2.1,3.9 2 1\n",
		"mode: atomic\nexample.com/a/b.go:1.1,5.1 1 0\nexample.com/a/b.go:3.1,7.1 1 1\n",
	} {
		writeFixture(t, second, input)
		if _, err := mergeProfiles([]string{first, second}); err == nil {
			t.Fatalf("accepted profile %q", input)
		}
	}
	writeFixture(t, first, "mode: set\nexample.com/a/a.go:2.1,3.9 1 2\n")
	if _, err := mergeProfiles([]string{first}); err == nil {
		t.Fatal("accepted non-boolean set count")
	}
	writeFixture(t, first, "mode: atomic\n")
	if _, err := mergeProfiles([]string{first}); err == nil {
		t.Fatal("accepted empty profiles")
	}
	if _, err := mergeProfiles([]string{filepath.Join(directory, "missing")}); err == nil {
		t.Fatal("accepted missing profile")
	}
}

func TestRealGoProfileOmitsUntestedLeafFromCovdataButMergeRestoresIt(t *testing.T) {
	directory := t.TempDir()
	writeFixture(t, filepath.Join(directory, "go.mod"), "module example.com/fixture\n\ngo 1.26.8\n")
	writeFixture(t, filepath.Join(directory, "covered/covered.go"), "package covered\nfunc Answer() int { return 42 }\n")
	writeFixture(t, filepath.Join(directory, "covered/covered_test.go"), "package covered\nimport \"testing\"\nfunc TestAnswer(t *testing.T) { if Answer() != 42 { t.Fatal(\"answer\") } }\n")
	writeFixture(t, filepath.Join(directory, "untested/untested.go"), "package untested\nfunc Missing() int { return 7 }\n")
	covdir := filepath.Join(directory, "covdata")
	if err := os.Mkdir(covdir, 0o700); err != nil {
		t.Fatal(err)
	}
	direct, binary := filepath.Join(directory, "direct.out"), filepath.Join(directory, "binary.out")
	runGo(t, directory, "test", "-count=1", "-covermode=atomic", "-coverpkg=./...", "-coverprofile="+direct, "./...", "-args", "-test.gocoverdir="+covdir)
	runGo(t, directory, "tool", "covdata", "textfmt", "-i="+covdir, "-o="+binary)
	binaryData, err := os.ReadFile(binary) //nolint:gosec // Profile is generated inside this test's temporary directory.
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(binaryData), "untested.go") {
		t.Fatal("Go behavior changed: covdata now contains the untested leaf; reassess the regression")
	}
	merged, err := mergeProfiles([]string{direct, binary})
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := cover.ParseProfilesFromReader(strings.NewReader(string(merged)))
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 || len(profiles[1].Blocks) != 1 || profiles[1].Blocks[0].NumStmt != 1 || profiles[1].Blocks[0].Count != 0 {
		t.Fatalf("missing untested zero-count block after merge: %s", merged)
	}
}

func TestRealGoProfileChangedStatementMetricIsOneOfFortyThree(t *testing.T) {
	directory := t.TempDir()
	writeFixture(t, filepath.Join(directory, "go.mod"), "module example.com/fixture\n\ngo 1.26.8\n")
	var source strings.Builder
	source.WriteString("package fixture\nfunc Example(flag bool) int {\n value := 0\n")
	for range 170 {
		source.WriteString(" value++\n")
	}
	headerLine := strings.Count(source.String(), "\n") + 1
	source.WriteString(" if flag {\n")
	for range 42 {
		source.WriteString("  value++\n")
	}
	source.WriteString(" }\n return value\n}\n")
	writeFixture(t, filepath.Join(directory, "fixture.go"), source.String())
	writeFixture(t, filepath.Join(directory, "fixture_test.go"), "package fixture\nimport \"testing\"\nfunc TestExample(t *testing.T) { if Example(false) != 170 { t.Fatal(\"value\") } }\n")
	profilePath := filepath.Join(directory, "coverage.out")
	runGo(t, directory, "test", "-count=1", "-coverprofile="+profilePath, "./...")
	profiles, err := cover.ParseProfiles(profilePath)
	if err != nil || len(profiles) != 1 {
		t.Fatalf("read real coverage profile: %v", err)
	}
	blocks := profiles[0].Blocks
	if len(blocks) != 3 || blocks[0].NumStmt != 172 || blocks[0].Count != 1 || blocks[1].NumStmt != 42 || blocks[1].Count != 0 {
		t.Fatalf("expected real 172-covered/42-uncovered profile blocks: %+v", blocks)
	}
	statements, err := sourceStatements(sourceFile{Path: "fixture.go", Source: source.String()})
	if err != nil {
		t.Fatal(err)
	}
	changed, covered := 0, 0
	for _, statement := range statements {
		block := mappedBlock(t, statement, blocks)
		if slices.ContainsFunc(statement.Lines, func(line int) bool { return line >= headerLine && line <= headerLine+42 }) {
			changed++
			if block.Count > 0 {
				covered++
			}
		}
	}
	if changed != 43 || covered != 1 {
		t.Fatalf("changed logical statements = %d/%d, want 1/43", covered, changed)
	}
}

func TestRealGoProfileMapsBranchClosureLabelAndMultilineStatements(t *testing.T) {
	directory := t.TempDir()
	writeFixture(t, filepath.Join(directory, "go.mod"), "module example.com/fixture\n\ngo 1.26.8\n")
	source := `package fixture
var PackageValue = func() int { return 3 }()
func Example(flag bool) int {
 value := 1 +
  2
 closure := func() int {
  return value
 }
 if flag {
  value++
 } else if value == 3 {
  value--
 }
label:
 for value < 4 {
  value++
  if value == 4 { break label }
 }
 switch value {
 case 4:
  value++
 }
 return closure()
}
`
	writeFixture(t, filepath.Join(directory, "fixture.go"), source)
	writeFixture(t, filepath.Join(directory, "fixture_test.go"), "package fixture\nimport \"testing\"\nfunc TestExample(t *testing.T) { if Example(false) != 5 { t.Fatal(\"value\") } }\n")
	profilePath := filepath.Join(directory, "coverage.out")
	runGo(t, directory, "test", "-count=1", "-coverprofile="+profilePath, "./...")
	profiles, err := cover.ParseProfiles(profilePath)
	if err != nil || len(profiles) != 1 {
		t.Fatalf("read real coverage profile: %v", err)
	}
	statements, err := sourceStatements(sourceFile{Path: "fixture.go", Source: source})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range statements {
		mappedBlock(t, statement, profiles[0].Blocks)
	}
}

func mappedBlock(t *testing.T, statement statement, blocks []cover.ProfileBlock) cover.ProfileBlock {
	t.Helper()
	var matches []cover.ProfileBlock
	for _, block := range blocks {
		if !before(statement.Line, statement.Column, block.StartLine, block.StartCol) &&
			before(statement.Line, statement.Column, block.EndLine, block.EndCol) && block.NumStmt > 0 {
			matches = append(matches, block)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("statement %+v maps to %d positive profile blocks: %+v", statement, len(matches), blocks)
	}
	return matches[0]
}

func runGo(t *testing.T, directory string, args ...string) {
	t.Helper()
	command := exec.Command("go", args...) //nolint:gosec // Arguments are fixed Go commands for isolated test fixtures.
	command.Dir = directory
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatal(fmt.Errorf("go %s: %w\n%s", strings.Join(args, " "), err, output))
	}
}
