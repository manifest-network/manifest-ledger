package main

import (
	"slices"
	"strings"
	"testing"
)

func TestStatementsOwnTokensExcludeNestedBodiesAndPadding(t *testing.T) {
	source := `package fixture
func Example(value int) int {
 result := value +
  // padding
  2
 if result > 0 {
  result++
 } else if result < 0 {
  result--
 }
 closure := func() int {
  return result
 }
 return closure()
}
`
	statements, err := sourceStatements(sourceFile{Path: "fixture.go", Source: source})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]int{{3, 5}, {6}, {7}, {8}, {9}, {11}, {12}, {14}}
	if len(statements) != len(want) {
		t.Fatalf("got %d statements: %+v", len(statements), statements)
	}
	for index, lines := range want {
		if !slices.Equal(lines, statements[index].Lines) {
			t.Errorf("statement %d: lines %v, want %v", index, statements[index].Lines, lines)
		}
	}
}

func TestStatementsLabelsCasesAndCommunicationBodies(t *testing.T) {
	source := `package fixture
func Example(ch chan int, value any) {
label:
 for number := range ch {
  switch number {
  case 1:
   continue label
  default:
   break
  }
 }
 switch value.(type) {
 case int:
  println(value)
 }
 select {
 case number := <-ch:
  println(number)
 default:
  return
 }
}
`
	statements, err := sourceStatements(sourceFile{Path: "fixture.go", Source: source})
	if err != nil {
		t.Fatal(err)
	}
	want := []int{4, 5, 7, 9, 12, 14, 16, 18, 20}
	var got []int
	for _, statement := range statements {
		got = append(got, statement.Line)
		if !slices.Equal(statement.Lines, []int{statement.Line}) {
			t.Errorf("header includes nested body: %+v", statement)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("statement lines: got %v, want %v", got, want)
	}
}

func TestStatementsFunctionLiteralsDeclarationsAndRawStrings(t *testing.T) {
	for _, source := range []string{
		"package fixture\nconst Answer = 42\ntype Reader interface { Read() }\n",
		"package fixture\nfunc Empty() {}\nfunc External()\nfunc _() { panic(42) }\n",
		"package fixture\nfunc Empty() { {}; ; }\n",
	} {
		statements, err := sourceStatements(sourceFile{Path: "fixture.go", Source: source})
		if err != nil || len(statements) != 0 {
			t.Fatalf("declaration or empty source: statements=%+v, error=%v", statements, err)
		}
	}
	source := "package fixture\nvar Answer = func() int {\n return 42\n}()\nfunc Example() {\n _ = `first\r\n\nlast`\n}\n"
	statements, err := sourceStatements(sourceFile{Path: "fixture.go", Source: source})
	if err != nil {
		t.Fatal(err)
	}
	if len(statements) != 2 || statements[0].Line != 3 || !slices.Equal(statements[1].Lines, []int{6, 7, 8}) {
		t.Fatalf("function literal and one multiline literal statement: %+v", statements)
	}
}

func TestStatementsRejectInvalidSourceAndLineDirectives(t *testing.T) {
	for _, source := range []string{
		"package fixture\nfunc Broken( {\n",
		"package fixture\n//line other.go:12\nfunc Example() { println(1) }\n",
		"package fixture\n/*line other.go:12*/func Example() { println(1) }\n",
	} {
		if _, err := sourceStatements(sourceFile{Path: "fixture.go", Source: source}); err == nil {
			t.Fatalf("accepted unsupported source %q", source)
		}
	}
	source := "package fixture\nfunc Example() { println(`//line other.go:12`) }\n"
	if _, err := sourceStatements(sourceFile{Path: "fixture.go", Source: source}); err != nil {
		t.Fatalf("directive text in a literal is ordinary data: %v", err)
	}
}

func TestStatementsControlHeaderExcludesFunctionLiteralBody(t *testing.T) {
	source := `package fixture
func Example() {
 if value := func() int {
  return 42
 }(); value > 0 {
  println(value)
 }
}
`
	statements, err := sourceStatements(sourceFile{Path: "fixture.go", Source: source})
	if err != nil {
		t.Fatal(err)
	}
	if len(statements) != 3 || !slices.Equal(statements[0].Lines, []int{3, 5}) || statements[1].Line != 4 || statements[2].Line != 6 {
		t.Fatalf("control header and literal body must be independent: %+v", statements)
	}
	for _, statement := range statements {
		if len(statement.Lines) == 0 {
			t.Fatal("executable statement has no source tokens")
		}
	}
}

func TestScannerErrorsAreReported(t *testing.T) {
	for _, source := range []string{"\x00", "`unterminated", "\"unterminated"} {
		if _, err := scanTokens(source); err == nil {
			t.Fatalf("accepted invalid token input %q", source)
		}
	}
	if tokens, err := scanTokens("// padding\n/* more padding */\n"); err != nil || len(tokens) != 0 {
		t.Fatalf("comments are not executable token lines: %v, %v", tokens, err)
	}
	if _, err := sourceStatements(sourceFile{Path: "fixture.go", Source: strings.Repeat("\n", 3) + "package fixture\n"}); err != nil {
		t.Fatal(err)
	}
}
