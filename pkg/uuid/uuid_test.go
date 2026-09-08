package uuid

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// These vectors were captured from the original manual FNV-1a implementation.
// They pin the consensus encoding independently of the implementation under test.
func TestGenerateUUIDv7GoldenVectors(t *testing.T) {
	blockTime := time.Date(2024, 1, 15, 10, 30, 0, 123456789, time.UTC)
	headerHash, err := hex.DecodeString("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	const chainID = "manifest-1"
	ctx := sdk.Context{}.WithBlockTime(blockTime).WithHeaderHash(headerHash).WithChainID(chainID)

	tests := []struct {
		namespace string
		sequence  uint64
		expected  string
	}{
		{namespace: "sku-provider", sequence: 0, expected: "018d0cab-c4bb-7000-848f-ba8a4a3e3967"},
		{namespace: "sku-provider", sequence: 4095, expected: "018d0cab-c4bb-7fff-845d-bf8a4a149b4d"},
		{namespace: "sku-provider", sequence: 4096, expected: "018d0cab-c4bb-7000-84c6-1a8a4a6c6bf7"},
		{namespace: "sku-provider", sequence: 9223372036854788153, expected: "018d0cab-c4bb-7039-87cf-7d04d32402dc"},
		{namespace: "sku-provider", sequence: 18446744073709551615, expected: "018d0cab-c4bb-7fff-a16b-7ee31ab9bbdf"},
		{namespace: "sku-sku", sequence: 0, expected: "018d0cab-c4bb-7000-8525-31f2ec27db3f"},
		{namespace: "sku-sku", sequence: 4095, expected: "018d0cab-c4bb-7fff-8527-a6f2ec2924e5"},
		{namespace: "sku-sku", sequence: 4096, expected: "018d0cab-c4bb-7000-855b-91f2ec560dcf"},
		{namespace: "sku-sku", sequence: 9223372036854788153, expected: "018d0cab-c4bb-7039-8252-6d78639ee044"},
		{namespace: "sku-sku", sequence: 18446744073709551615, expected: "018d0cab-c4bb-7fff-81c2-98268eb64db7"},
		{namespace: "billing-lease", sequence: 0, expected: "018d0cab-c4bb-7000-b517-741c95665142"},
		{namespace: "billing-lease", sequence: 4095, expected: "018d0cab-c4bb-7fff-b53d-071c95866748"},
		{namespace: "billing-lease", sequence: 4096, expected: "018d0cab-c4bb-7000-b4e1-141c95381eb2"},
		{namespace: "billing-lease", sequence: 9223372036854788153, expected: "018d0cab-c4bb-7039-b1d7-d1a20c80be2d"},
		{namespace: "billing-lease", sequence: 18446744073709551615, expected: "018d0cab-c4bb-7fff-acb2-c4e195c425ba"},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%s/%d", test.namespace, test.sequence), func(t *testing.T) {
			actual := GenerateUUIDv7WithEntropy(blockTime, headerHash, chainID, test.namespace, test.sequence)
			require.Equal(t, test.expected, actual)
			require.Equal(t, test.expected, GenerateUUIDv7(ctx, test.namespace, test.sequence))

			decoded, err := hex.DecodeString(strings.ReplaceAll(actual, "-", ""))
			require.NoError(t, err)
			require.Len(t, decoded, 16)
			timestamp := binary.BigEndian.Uint64(decoded[:8]) >> 16
			require.Equal(t, uint64(blockTime.UnixMilli()), timestamp) //nolint:gosec // fixed positive test timestamp
			require.Equal(t, byte(7), decoded[6]>>4, "UUID version")
			require.Equal(t, byte(2), decoded[8]>>6, "RFC UUID variant")
			require.Equal(t, uint16(test.sequence&0xfff), binary.BigEndian.Uint16(decoded[6:8])&0xfff)
		})
	}
	t.Run("time-only entropy", func(t *testing.T) {
		require.Equal(t, "018d0cab-c4bb-7000-8e7c-30352a6f7a27", GenerateUUIDv7FromTime(blockTime, "billing-lease", 4096))
	})
}

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

	// Decode the first 48 bits instead of only comparing generated strings.
	decoded, err := hex.DecodeString(strings.ReplaceAll(uuid, "-", ""))
	require.NoError(t, err)
	require.Equal(t, uint64(testTime.UnixMilli()), binary.BigEndian.Uint64(decoded[:8])>>16) //nolint:gosec // fixed positive test timestamp

	// Generate at different times (1 hour later to ensure timestamp differs significantly)
	otherTime := time.Date(2024, 6, 15, 13, 0, 0, 0, time.UTC) // 1 hour later
	otherUUID := GenerateUUIDv7FromTime(otherTime, "test", 1)

	// UUIDs should differ
	require.NotEqual(t, uuid, otherUUID, "Different timestamps should produce different UUIDs")
}
