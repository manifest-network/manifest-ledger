// Package sanitize provides string sanitization utilities for safe logging and event emission.
package sanitize

import (
	"strings"
	"unicode"
)

// EventAttribute sanitizes a string for safe inclusion in event attributes.
// This prevents log injection attacks by removing control characters using Go's
// standard library unicode.IsGraphic function, which allows letters, marks, numbers,
// punctuation, symbols, and spaces while filtering out control characters.
func EventAttribute(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsGraphic(r) {
			return r
		}
		return -1
	}, s)
}
