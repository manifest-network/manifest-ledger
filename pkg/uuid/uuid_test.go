package uuid

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGenerateUUIDv7FromTime(t *testing.T) {
	testTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name       string
		moduleName string
		sequence   uint64
	}{
		{
			name:       "sku provider",
			moduleName: "sku",
			sequence:   1,
		},
		{
			name:       "sku sku",
			moduleName: "sku",
			sequence:   2,
		},
		{
			name:       "billing lease",
			moduleName: "billing",
			sequence:   1,
		},
		{
			name:       "high sequence",
			moduleName: "sku",
			sequence:   999999,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			uuid1 := GenerateUUIDv7FromTime(testTime, tc.moduleName, tc.sequence)
			uuid2 := GenerateUUIDv7FromTime(testTime, tc.moduleName, tc.sequence)

			// Should be deterministic
			require.Equal(t, uuid1, uuid2, "UUIDs should be deterministic")

			// Should be valid UUIDv7
			require.True(t, IsValidUUIDv7(uuid1), "UUID should be valid: %s", uuid1)

			// Check length
			require.Len(t, uuid1, 36, "UUID should be 36 characters")

			// Check version (7)
			require.Equal(t, byte('7'), uuid1[14], "Version should be 7")

			// Check variant (8, 9, a, or b)
			variant := uuid1[19]
			require.True(t, variant == '8' || variant == '9' || variant == 'a' || variant == 'b',
				"Variant should be 8, 9, a, or b, got %c", variant)
		})
	}
}

func TestUUIDUniqueness(t *testing.T) {
	testTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	// Same module, different sequences
	uuid1 := GenerateUUIDv7FromTime(testTime, "sku", 1)
	uuid2 := GenerateUUIDv7FromTime(testTime, "sku", 2)
	require.NotEqual(t, uuid1, uuid2, "Different sequences should produce different UUIDs")

	// Same sequence, different modules
	uuid3 := GenerateUUIDv7FromTime(testTime, "billing", 1)
	require.NotEqual(t, uuid1, uuid3, "Different modules should produce different UUIDs")

	// Same module and sequence, different times
	otherTime := time.Date(2024, 1, 15, 10, 30, 1, 0, time.UTC)
	uuid4 := GenerateUUIDv7FromTime(otherTime, "sku", 1)
	require.NotEqual(t, uuid1, uuid4, "Different times should produce different UUIDs")
}

func TestIsValidUUIDv7(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid UUIDv7",
			input:    "018d1234-5678-7abc-8def-0123456789ab",
			expected: true,
		},
		{
			name:     "valid UUIDv7 variant 9",
			input:    "018d1234-5678-7abc-9def-0123456789ab",
			expected: true,
		},
		{
			name:     "valid UUIDv7 variant a",
			input:    "018d1234-5678-7abc-adef-0123456789ab",
			expected: true,
		},
		{
			name:     "valid UUIDv7 variant b",
			input:    "018d1234-5678-7abc-bdef-0123456789ab",
			expected: true,
		},
		{
			name:     "invalid version 4",
			input:    "018d1234-5678-4abc-8def-0123456789ab",
			expected: false,
		},
		{
			name:     "invalid variant",
			input:    "018d1234-5678-7abc-0def-0123456789ab",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "too short",
			input:    "018d1234-5678-7abc",
			expected: false,
		},
		{
			name:     "uppercase (invalid)",
			input:    "018D1234-5678-7ABC-8DEF-0123456789AB",
			expected: false,
		},
		{
			name:     "no dashes",
			input:    "018d123456787abc8def0123456789ab",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsValidUUIDv7(tc.input)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestValidateUUIDv7(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expectErr bool
	}{
		{
			name:      "valid UUID",
			input:     "018d1234-5678-7abc-8def-0123456789ab",
			expectErr: false,
		},
		{
			name:      "empty string",
			input:     "",
			expectErr: true,
		},
		{
			name:      "invalid format",
			input:     "not-a-uuid",
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateUUIDv7(tc.input)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestTimestampExtraction(t *testing.T) {
	// Generate UUID at a known time
	testTime := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	uuid := GenerateUUIDv7FromTime(testTime, "test", 1)

	// The first 12 hex characters (48 bits) should encode the timestamp
	// This is a simple sanity check that the timestamp is embedded
	require.True(t, IsValidUUIDv7(uuid))

	// Generate at different times (1 hour later to ensure timestamp differs significantly)
	otherTime := time.Date(2024, 6, 15, 13, 0, 0, 0, time.UTC) // 1 hour later
	otherUUID := GenerateUUIDv7FromTime(otherTime, "test", 1)

	// UUIDs should differ
	require.NotEqual(t, uuid, otherUUID, "Different timestamps should produce different UUIDs")
}
