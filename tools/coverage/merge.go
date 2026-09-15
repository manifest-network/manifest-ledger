package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"slices"
	"strings"

	"golang.org/x/tools/cover"
)

type blockKey struct {
	file             string
	startLine, start int
	endLine, end     int
}

type mergedBlock struct {
	key        blockKey
	statements int
	hit        bool
}

// mergeProfiles preserves Go's ranges and statement counts, but normalizes
// execution counts to zero or one. Only hit/miss coverage is claimed: the unit
// text profile and covdata may contain observations from the same test runs.
func mergeProfiles(paths []string) ([]byte, error) {
	blocks := make(map[blockKey]mergedBlock)
	mode := ""
	for _, source := range paths {
		data, err := os.ReadFile(source) //nolint:gosec // Reading operator-selected local profiles is this command's purpose.
		if err != nil {
			return nil, err
		}
		header, _, _ := strings.Cut(string(data), "\n")
		current := strings.TrimPrefix(header, "mode: ")
		if header == current || (current != "set" && current != "count" && current != "atomic") {
			return nil, fmt.Errorf("invalid coverage mode in %s", source)
		}
		if mode != "" && mode != current {
			return nil, errors.New("cannot merge different coverage modes")
		}
		mode = current
		profiles, err := cover.ParseProfilesFromReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", source, err)
		}
		for _, profile := range profiles {
			if err := validateProfile(profile); err != nil {
				return nil, fmt.Errorf("%s: %w", source, err)
			}
			for _, block := range profile.Blocks {
				key := blockKey{profile.FileName, block.StartLine, block.StartCol, block.EndLine, block.EndCol}
				old, exists := blocks[key]
				if exists && old.statements != block.NumStmt {
					return nil, fmt.Errorf("inconsistent statement count for %s:%d.%d", key.file, key.startLine, key.start)
				}
				blocks[key] = mergedBlock{key: key, statements: block.NumStmt, hit: old.hit || block.Count > 0}
			}
		}
	}
	if len(blocks) == 0 {
		return nil, errors.New("coverage profiles contain no blocks")
	}
	ordered := make([]mergedBlock, 0, len(blocks))
	for _, block := range blocks {
		ordered = append(ordered, block)
	}
	slices.SortFunc(ordered, func(a, b mergedBlock) int {
		if a.key.file != b.key.file {
			return strings.Compare(a.key.file, b.key.file)
		}
		if a.key.startLine != b.key.startLine {
			return a.key.startLine - b.key.startLine
		}
		if a.key.start != b.key.start {
			return a.key.start - b.key.start
		}
		if a.key.endLine != b.key.endLine {
			return a.key.endLine - b.key.endLine
		}
		return a.key.end - b.key.end
	})
	var result bytes.Buffer
	fmt.Fprintf(&result, "mode: %s\n", mode)
	for index, block := range ordered {
		key := block.key
		if index > 0 {
			previous := ordered[index-1].key
			if previous.file == key.file && before(key.startLine, key.start, previous.endLine, previous.end) {
				return nil, fmt.Errorf("overlapping merged profile ranges in %s", key.file)
			}
		}
		count := 0
		if block.hit {
			count = 1
		}
		fmt.Fprintf(&result, "%s:%d.%d,%d.%d %d %d\n", key.file, key.startLine, key.start, key.endLine, key.end, block.statements, count)
	}
	return result.Bytes(), nil
}

func validateProfile(profile *cover.Profile) error {
	name := profile.FileName
	if name == "" || path.IsAbs(name) || path.Clean(name) != name || strings.HasPrefix(name, "../") ||
		strings.ContainsAny(name, "\x00\r\n") || !strings.HasSuffix(name, ".go") {
		return fmt.Errorf("invalid coverage source path %q", name)
	}
	for index, block := range profile.Blocks {
		if block.StartLine <= 0 || block.StartCol <= 0 || block.EndLine <= 0 || block.EndCol <= 0 ||
			before(block.EndLine, block.EndCol, block.StartLine, block.StartCol) || block.NumStmt < 0 || block.Count < 0 ||
			(block.StartLine == block.EndLine && block.StartCol == block.EndCol && block.NumStmt != 0) ||
			(profile.Mode == "set" && block.Count > 1) {
			return fmt.Errorf("invalid coverage range or count in %s", name)
		}
		if index > 0 {
			previous := profile.Blocks[index-1]
			if before(block.StartLine, block.StartCol, previous.EndLine, previous.EndCol) {
				return fmt.Errorf("overlapping coverage ranges in %s", name)
			}
		}
	}
	return nil
}

func before(line, column, otherLine, otherColumn int) bool {
	return line < otherLine || (line == otherLine && column < otherColumn)
}
