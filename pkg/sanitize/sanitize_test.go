package sanitize_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/manifest-network/manifest-ledger/pkg/sanitize"
)

func TestEventAttribute(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal text unchanged",
			input:    "Resource unavailable",
			expected: "Resource unavailable",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "removes newline",
			input:    "line1\nline2",
			expected: "line1line2",
		},
		{
			name:     "removes carriage return",
			input:    "line1\rline2",
			expected: "line1line2",
		},
		{
			name:     "removes tab",
			input:    "col1\tcol2",
			expected: "col1col2",
		},
		{
			name:     "removes null byte",
			input:    "text\x00null",
			expected: "textnull",
		},
		{
			name:     "removes escape sequence",
			input:    "text\x1b[31mred",
			expected: "text[31mred",
		},
		{
			name:     "preserves space",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "preserves numbers and punctuation",
			input:    "Rate: 100.5% - OK!",
			expected: "Rate: 100.5% - OK!",
		},
		{
			name:     "preserves UTF-8 characters",
			input:    "日本語テスト",
			expected: "日本語テスト",
		},
		{
			name:     "preserves emojis",
			input:    "Status: ✓ OK 🚀",
			expected: "Status: ✓ OK 🚀",
		},
		{
			name:     "mixed control chars and valid text",
			input:    "start\x00\n\r\tmiddle\x1b[0mend",
			expected: "startmiddle[0mend",
		},
		{
			name:     "removes DEL character",
			input:    "text\x7fmore",
			expected: "textmore",
		},
		{
			name:     "log injection attempt with fake log entry",
			input:    "reason\n2024-01-01 CRITICAL: hacked",
			expected: "reason2024-01-01 CRITICAL: hacked",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitize.EventAttribute(tc.input)
			require.Equal(t, tc.expected, result)
		})
	}
}
