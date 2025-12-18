// Package uuid provides deterministic UUIDv7 generation for blockchain consensus.
// All validators must generate identical UUIDs for the same inputs.
package uuid

import (
	"encoding/binary"
	"fmt"
	"regexp"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// UUIDv7 format: xxxxxxxx-xxxx-7xxx-yxxx-xxxxxxxxxxxx
	// where x is hex digit, 7 is version, y is variant (8, 9, a, or b)
	uuidPattern = `^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`
)

var uuidRegex = regexp.MustCompile(uuidPattern)

// GenerateUUIDv7 generates a deterministic UUIDv7 based on block time and a sequence number.
// This ensures all validators generate identical UUIDs for consensus.
//
// UUIDv7 structure (128 bits):
// - 48 bits: Unix timestamp in milliseconds
// - 4 bits: Version (7)
// - 12 bits: Sequence/random (we use sequence for determinism)
// - 2 bits: Variant (RFC 4122)
// - 62 bits: Node ID / random (we derive from module name + sequence)
func GenerateUUIDv7(ctx sdk.Context, moduleName string, sequence uint64) string {
	blockTime := ctx.BlockTime()
	return GenerateUUIDv7FromTime(blockTime, moduleName, sequence)
}

// GenerateUUIDv7FromTime generates a deterministic UUIDv7 from a specific time.
// Useful for testing and migration scenarios.
func GenerateUUIDv7FromTime(t time.Time, moduleName string, sequence uint64) string {
	// Get milliseconds since Unix epoch
	// Note: UnixMilli() returns int64 but blockchain timestamps are always positive
	// and within uint64 range, so this conversion is safe.
	ms := uint64(t.UnixMilli()) //nolint:gosec // blockchain timestamps are always positive

	var uuid [16]byte

	// Bytes 0-5: 48-bit timestamp (big-endian)
	uuid[0] = byte(ms >> 40)
	uuid[1] = byte(ms >> 32)
	uuid[2] = byte(ms >> 24)
	uuid[3] = byte(ms >> 16)
	uuid[4] = byte(ms >> 8)
	uuid[5] = byte(ms)

	// Bytes 6-7: version (7) and 12-bit sequence
	// High 4 bits of byte 6 = version 7
	// Low 4 bits of byte 6 + all of byte 7 = 12-bit sequence
	uuid[6] = 0x70 | byte((sequence>>8)&0x0F)
	uuid[7] = byte(sequence & 0xFF)

	// Bytes 8-15: variant (10) and 62-bit node derived from module name and sequence
	// We hash the module name with sequence to get deterministic node bits
	nodeHash := hashModuleSequence(moduleName, sequence)

	// Byte 8: variant (10xx xxxx) + high bits of node
	uuid[8] = 0x80 | byte((nodeHash>>56)&0x3F)

	// Bytes 9-15: remaining node bits
	uuid[9] = byte(nodeHash >> 48)
	uuid[10] = byte(nodeHash >> 40)
	uuid[11] = byte(nodeHash >> 32)
	uuid[12] = byte(nodeHash >> 24)
	uuid[13] = byte(nodeHash >> 16)
	uuid[14] = byte(nodeHash >> 8)
	uuid[15] = byte(nodeHash)

	return formatUUID(uuid)
}

// hashModuleSequence creates a deterministic hash from module name and sequence.
// Uses a simple FNV-1a inspired hash for determinism across all validators.
func hashModuleSequence(moduleName string, sequence uint64) uint64 {
	// FNV-1a 64-bit constants
	const (
		fnvPrime  = 1099511628211
		fnvOffset = 14695981039346656037
	)

	hash := uint64(fnvOffset)

	// Hash module name
	for i := 0; i < len(moduleName); i++ {
		hash ^= uint64(moduleName[i])
		hash *= fnvPrime
	}

	// Hash sequence (as 8 bytes)
	var seqBytes [8]byte
	binary.BigEndian.PutUint64(seqBytes[:], sequence)
	for i := 0; i < 8; i++ {
		hash ^= uint64(seqBytes[i])
		hash *= fnvPrime
	}

	return hash
}

// formatUUID formats 16 bytes as a UUID string.
func formatUUID(uuid [16]byte) string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4],
		uuid[4:6],
		uuid[6:8],
		uuid[8:10],
		uuid[10:16],
	)
}

// IsValidUUIDv7 validates that a string is a properly formatted UUIDv7.
func IsValidUUIDv7(s string) bool {
	return uuidRegex.MatchString(s)
}

// IsValidUUID is an alias for IsValidUUIDv7 for convenience.
func IsValidUUID(s string) bool {
	return IsValidUUIDv7(s)
}

// ValidateUUIDv7 returns an error if the string is not a valid UUIDv7.
func ValidateUUIDv7(s string) error {
	if s == "" {
		return fmt.Errorf("uuid cannot be empty")
	}
	if !IsValidUUIDv7(s) {
		return fmt.Errorf("invalid UUIDv7 format: %s", s)
	}
	return nil
}
