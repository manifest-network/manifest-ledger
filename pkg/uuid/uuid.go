// Package uuid provides deterministic UUIDv7 generation for blockchain consensus.
// All validators must generate identical UUIDs for the same inputs.
//
// # Why Custom Implementation?
//
// Standard UUID libraries (like google/uuid) use non-deterministic sources:
//   - time.Now() - varies between validators
//   - crypto/rand - produces different values per call
//
// In blockchain consensus, all validators must produce identical state transitions.
// This package uses deterministic inputs available to all validators:
//   - Block timestamp (from consensus)
//   - Block header hash (from previous block, available to all validators)
//   - Chain ID (from genesis)
//   - Module name (constant per module)
//   - Sequence number (from module state)
//
// # Cross-Chain Collision Resistance
//
// Block header hash and chain ID help distinguish otherwise identical timestamps
// and sequences on different chains. The truncated, non-cryptographic hash does
// not guarantee collision resistance against adversarial inputs.
package uuid

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
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

// GenerateUUIDv7 generates a deterministic UUIDv7 based on block context and a sequence number.
// This ensures all validators generate identical UUIDs for consensus.
//
// The UUID incorporates:
//   - Block timestamp (48 bits) - provides time-ordering
//   - Low sequence bits (12 bits) - distinguish nearby counter values
//   - Hash of header, chain ID, namespace, and full sequence (62 bits)
//
// UUIDv7 structure (128 bits):
//   - 48 bits: Unix timestamp in milliseconds
//   - 4 bits: Version (7)
//   - 12 bits: Low sequence bits
//   - 2 bits: Variant (RFC 4122)
//   - 62 bits: Node ID (derived from header hash + chain ID + module + sequence)
func GenerateUUIDv7(ctx sdk.Context, moduleName string, sequence uint64) string {
	blockTime := ctx.BlockTime()
	headerHash := ctx.HeaderHash()
	chainID := ctx.ChainID()
	return GenerateUUIDv7WithEntropy(blockTime, headerHash, chainID, moduleName, sequence)
}

// GenerateUUIDv7WithEntropy generates a deterministic UUIDv7 with explicit entropy sources.
// This is the core implementation that allows full control over all inputs.
//
// Parameters:
//   - t: timestamp for the UUID (typically block time)
//   - headerHash: block header hash to distinguish chains and blocks (can be nil for testing)
//   - chainID: chain identifier to distinguish chains
//   - moduleName: entity namespace ("sku-provider", "sku-sku", or "billing-lease")
//   - sequence: monotonically increasing sequence within the module
func GenerateUUIDv7WithEntropy(t time.Time, headerHash []byte, chainID, moduleName string, sequence uint64) string {
	// Get milliseconds since Unix epoch
	// Note: UnixMilli() returns int64 but blockchain timestamps are always positive
	// and within uint64 range, so this conversion is safe.
	ms := uint64(t.UnixMilli()) //nolint:gosec // blockchain timestamps are always positive

	var uuid [16]byte

	// Bytes 0-5: 48-bit timestamp (big-endian)
	uuid[0] = byte((ms >> 40) & 0xFF)
	uuid[1] = byte((ms >> 32) & 0xFF)
	uuid[2] = byte((ms >> 24) & 0xFF)
	uuid[3] = byte((ms >> 16) & 0xFF)
	uuid[4] = byte((ms >> 8) & 0xFF)
	uuid[5] = byte(ms & 0xFF)

	// Bytes 6-7: version (7) and 12-bit sequence
	// High 4 bits of byte 6 = version 7
	// Low 4 bits of byte 6 + all of byte 7 = 12-bit sequence
	uuid[6] = 0x70 | byte((sequence>>8)&0x0F)
	uuid[7] = byte(sequence & 0xFF)

	// Bytes 8-15: variant (10) and 62-bit node derived from all entropy sources
	// Chain and namespace inputs help distinguish otherwise identical counters.
	nodeHash := hashEntropy(headerHash, chainID, moduleName, sequence)

	// Byte 8: variant (10xx xxxx) + high bits of node
	uuid[8] = 0x80 | byte((nodeHash>>56)&0x3F)

	// Bytes 9-15: remaining node bits
	uuid[9] = byte((nodeHash >> 48) & 0xFF)
	uuid[10] = byte((nodeHash >> 40) & 0xFF)
	uuid[11] = byte((nodeHash >> 32) & 0xFF)
	uuid[12] = byte((nodeHash >> 24) & 0xFF)
	uuid[13] = byte((nodeHash >> 16) & 0xFF)
	uuid[14] = byte((nodeHash >> 8) & 0xFF)
	uuid[15] = byte(nodeHash & 0xFF)

	return formatUUID(uuid)
}

// GenerateUUIDv7FromTime generates a deterministic UUIDv7 from a specific time.
// Useful for testing and migration scenarios where block context is not available.
// Note: This omits header hash and chain ID, so matching time, namespace, and
// sequence inputs collide across chains. Use GenerateUUIDv7 or
// GenerateUUIDv7WithEntropy for production.
func GenerateUUIDv7FromTime(t time.Time, moduleName string, sequence uint64) string {
	return GenerateUUIDv7WithEntropy(t, nil, "", moduleName, sequence)
}

// hashEntropy hashes the existing consensus byte encoding with FNV-1a.
// Inputs are concatenated without delimiters; the sequence is big-endian.
// Changing the algorithm, framing, or input order would change future UUIDs.
//
// The hash incorporates:
//   - Header hash: consensus block context
//   - Chain ID: chain namespace
//   - Module name: entity namespace
//   - Sequence: full entity counter
func hashEntropy(headerHash []byte, chainID, moduleName string, sequence uint64) uint64 {
	hash := fnv.New64a()
	// hash.Hash.Write always returns a nil error.
	_, _ = hash.Write(headerHash)
	_, _ = hash.Write([]byte(chainID))
	_, _ = hash.Write([]byte(moduleName))

	var seqBytes [8]byte
	binary.BigEndian.PutUint64(seqBytes[:], sequence)
	_, _ = hash.Write(seqBytes[:])

	return hash.Sum64()
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
