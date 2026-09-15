package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"slices"
	"strings"
)

type sourceFile struct {
	Path   string `json:"path"`
	Source string `json:"source"`
}

type statementFile struct {
	Path       string      `json:"path"`
	Statements []statement `json:"statements"`
}

// statement is one logical executable AST statement. Lines contains its own
// non-comment token lines, excluding nested bodies and block delimiters. This
// metric is deliberately distinct from Go coverage's NumStmt block weights.
type statement struct {
	Line   int   `json:"line"`
	Column int   `json:"column"`
	Lines  []int `json:"lines"`
}

type sourceToken struct {
	start, end int
	lines      []int
}

type sourceRange struct{ start, end int }

func sourceStatements(source sourceFile) ([]statement, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, source.Path, source.Source, 0)
	if err != nil {
		return nil, err
	}
	tokens, err := scanTokens(source.Source)
	if err != nil {
		return nil, err
	}
	tokenFile := fset.File(file.Pos())
	statements := make([]statement, 0)
	seen := make(map[token.Pos]bool)
	add := func(node ast.Stmt) {
		for {
			label, ok := node.(*ast.LabeledStmt)
			if !ok {
				break
			}
			node = label.Stmt
		}
		switch node.(type) {
		case *ast.BlockStmt, *ast.EmptyStmt, *ast.CaseClause, *ast.CommClause:
			return
		}
		if seen[node.Pos()] {
			return
		}
		seen[node.Pos()] = true
		endPos := statementEnd(node)
		start, end := tokenFile.Offset(node.Pos()), tokenFile.Offset(endPos)
		var excluded []sourceRange
		ast.Inspect(node, func(child ast.Node) bool {
			if child == nil || child.Pos() >= endPos {
				return false
			}
			if literal, ok := child.(*ast.FuncLit); ok {
				excluded = append(excluded, sourceRange{
					tokenFile.Offset(literal.Body.Pos()), tokenFile.Offset(literal.Body.End()),
				})
				return false
			}
			return true
		})
		lines := make(map[int]bool)
		first, _ := slices.BinarySearchFunc(tokens, start, func(tok sourceToken, offset int) int {
			return tok.start - offset
		})
		for _, tok := range tokens[first:] {
			if tok.start >= end {
				break
			}
			if tok.end > end {
				continue
			}
			if slices.ContainsFunc(excluded, func(r sourceRange) bool { return tok.start >= r.start && tok.start < r.end }) {
				continue
			}
			for _, line := range tok.lines {
				lines[line] = true
			}
		}
		ownLines := make([]int, 0, len(lines))
		for line := range lines {
			ownLines = append(ownLines, line)
		}
		slices.Sort(ownLines)
		pos := fset.PositionFor(node.Pos(), false)
		statements = append(statements, statement{Line: pos.Line, Column: pos.Column, Lines: ownLines})
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.FuncDecl:
			return node.Name.Name != "_" && node.Body != nil
		case *ast.BlockStmt:
			for _, child := range node.List {
				add(child)
			}
		case *ast.CaseClause:
			for _, child := range node.Body {
				add(child)
			}
		case *ast.CommClause:
			for _, child := range node.Body {
				add(child)
			}
		case *ast.IfStmt:
			if child, ok := node.Else.(*ast.IfStmt); ok {
				add(child)
			}
		}
		return true
	})
	slices.SortFunc(statements, func(a, b statement) int {
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		return a.Column - b.Column
	})
	return statements, nil
}

func statementEnd(node ast.Stmt) token.Pos {
	switch node := node.(type) {
	case *ast.IfStmt:
		return node.Body.Lbrace
	case *ast.ForStmt:
		return node.Body.Lbrace
	case *ast.RangeStmt:
		return node.Body.Lbrace
	case *ast.SwitchStmt:
		return node.Body.Lbrace
	case *ast.TypeSwitchStmt:
		return node.Body.Lbrace
	case *ast.SelectStmt:
		return node.Body.Lbrace
	default:
		return node.End()
	}
}

func scanTokens(source string) ([]sourceToken, error) {
	file := token.NewFileSet().AddFile("source.go", -1, len(source))
	var lexer scanner.Scanner
	var scanErr error
	lexer.Init(file, []byte(source), func(pos token.Position, message string) {
		if scanErr == nil {
			scanErr = fmt.Errorf("scan source at %s: %s", pos, message)
		}
	}, scanner.ScanComments)
	var tokens []sourceToken
	for {
		pos, kind, literal := lexer.Scan()
		if kind == token.EOF {
			break
		}
		if kind == token.COMMENT {
			if strings.HasPrefix(literal, "//line ") || strings.HasPrefix(literal, "/*line ") {
				return nil, fmt.Errorf("line directives are unsupported: %q", literal)
			}
			continue
		}
		if kind == token.SEMICOLON && literal == "\n" {
			continue
		}
		start := file.Offset(pos)
		width := len(literal)
		if width == 0 {
			width = len(kind.String())
		}
		// The scanner removes carriage returns from raw-string values; source
		// positions must retain the physical byte span, including such returns.
		if kind == token.STRING && source[start] == '`' {
			closing := strings.IndexByte(source[start+1:], '`')
			if closing < 0 {
				return nil, fmt.Errorf("unterminated raw string at byte %d", start)
			}
			width = closing + 2
		}
		if start+width > len(source) {
			return nil, fmt.Errorf("invalid token span at byte %d", start)
		}
		first := file.PositionFor(pos, false).Line
		last := first + strings.Count(source[start:start+width], "\n")
		lines := make([]int, 0, last-first+1)
		for line := first; line <= last; line++ {
			lines = append(lines, line)
		}
		tokens = append(tokens, sourceToken{start: start, end: start + width, lines: lines})
	}
	return tokens, scanErr
}
