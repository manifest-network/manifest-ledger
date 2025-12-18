package uuid

import (
	"encoding/hex"
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

func TestGenerateUUIDv7WithEntropy(t *testing.T) {
	testTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	headerHash, _ := hex.DecodeString("abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	chainID := "manifest-1"

	tests := []struct {
		name       string
		headerHash []byte
		chainID    string
		moduleName string
		sequence   uint64
	}{
		{
			name:       "with all entropy sources",
			headerHash: headerHash,
			chainID:    chainID,
			moduleName: "sku",
			sequence:   1,
		},
		{
			name:       "with nil header hash",
			headerHash: nil,
			chainID:    chainID,
			moduleName: "sku",
			sequence:   1,
		},
		{
			name:       "with empty chain ID",
			headerHash: headerHash,
			chainID:    "",
			moduleName: "billing",
			sequence:   1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			uuid1 := GenerateUUIDv7WithEntropy(testTime, tc.headerHash, tc.chainID, tc.moduleName, tc.sequence)
			uuid2 := GenerateUUIDv7WithEntropy(testTime, tc.headerHash, tc.chainID, tc.moduleName, tc.sequence)

			// Should be deterministic
			require.Equal(t, uuid1, uuid2, "UUIDs should be deterministic")

			// Should be valid UUIDv7
			require.True(t, IsValidUUIDv7(uuid1), "UUID should be valid: %s", uuid1)
		})
	}
}

func TestCrossChainUniqueness(t *testing.T) {
	testTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	headerHash1, _ := hex.DecodeString("1111111111111111111111111111111111111111111111111111111111111111")
	headerHash2, _ := hex.DecodeString("2222222222222222222222222222222222222222222222222222222222222222")

	// Same time, sequence, module but different chain IDs
	uuid1 := GenerateUUIDv7WithEntropy(testTime, headerHash1, "chain-1", "sku", 1)
	uuid2 := GenerateUUIDv7WithEntropy(testTime, headerHash1, "chain-2", "sku", 1)
	require.NotEqual(t, uuid1, uuid2, "Different chain IDs should produce different UUIDs")

	// Same time, sequence, module, chain ID but different header hashes
	uuid3 := GenerateUUIDv7WithEntropy(testTime, headerHash1, "chain-1", "sku", 1)
	uuid4 := GenerateUUIDv7WithEntropy(testTime, headerHash2, "chain-1", "sku", 1)
	require.NotEqual(t, uuid3, uuid4, "Different header hashes should produce different UUIDs")

	// Verify uuid1 and uuid3 are the same (same inputs)
	require.Equal(t, uuid1, uuid3, "Same inputs should produce same UUIDs")
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

func TestMultiChainDeploymentScenario(t *testing.T) {
	// Simulate a scenario where two chains are deployed at the exact same time
	// with the same genesis and both create their first provider
	deployTime := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	// Chain 1's first block after genesis
	chain1HeaderHash, _ := hex.DecodeString("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	chain1ID := "manifest-mainnet"

	// Chain 2's first block after genesis (testnet deployed same time)
	chain2HeaderHash, _ := hex.DecodeString("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	chain2ID := "manifest-testnet"

	// Both chains create first provider (sequence 1) at the same block time
	uuid1 := GenerateUUIDv7WithEntropy(deployTime, chain1HeaderHash, chain1ID, "sku-provider", 1)
	uuid2 := GenerateUUIDv7WithEntropy(deployTime, chain2HeaderHash, chain2ID, "sku-provider", 1)

	require.NotEqual(t, uuid1, uuid2,
		"UUIDs from different chains should be unique even with same timestamp and sequence")

	// Both should be valid
	require.True(t, IsValidUUIDv7(uuid1))
	require.True(t, IsValidUUIDv7(uuid2))
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
