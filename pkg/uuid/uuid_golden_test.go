package uuid_test

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/pkg/uuid"
	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

// These vectors were captured from the manual FNV-1a implementation at 85d1602.
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
		{namespace: skutypes.ModuleName + "-provider", sequence: 0, expected: "018d0cab-c4bb-7000-848f-ba8a4a3e3967"},
		{namespace: skutypes.ModuleName + "-provider", sequence: 4095, expected: "018d0cab-c4bb-7fff-845d-bf8a4a149b4d"},
		{namespace: skutypes.ModuleName + "-provider", sequence: 4096, expected: "018d0cab-c4bb-7000-84c6-1a8a4a6c6bf7"},
		{namespace: skutypes.ModuleName + "-provider", sequence: 9223372036854788153, expected: "018d0cab-c4bb-7039-87cf-7d04d32402dc"},
		{namespace: skutypes.ModuleName + "-provider", sequence: 18446744073709551615, expected: "018d0cab-c4bb-7fff-a16b-7ee31ab9bbdf"},
		{namespace: skutypes.ModuleName + "-sku", sequence: 0, expected: "018d0cab-c4bb-7000-8525-31f2ec27db3f"},
		{namespace: skutypes.ModuleName + "-sku", sequence: 4095, expected: "018d0cab-c4bb-7fff-8527-a6f2ec2924e5"},
		{namespace: skutypes.ModuleName + "-sku", sequence: 4096, expected: "018d0cab-c4bb-7000-855b-91f2ec560dcf"},
		{namespace: skutypes.ModuleName + "-sku", sequence: 9223372036854788153, expected: "018d0cab-c4bb-7039-8252-6d78639ee044"},
		{namespace: skutypes.ModuleName + "-sku", sequence: 18446744073709551615, expected: "018d0cab-c4bb-7fff-81c2-98268eb64db7"},
		{namespace: billingtypes.ModuleName, sequence: 0, expected: "018d0cab-c4bb-7000-977c-dbcb522b14e1"},
		{namespace: billingtypes.ModuleName, sequence: 4095, expected: "018d0cab-c4bb-7fff-9773-a4cb522415e7"},
		{namespace: billingtypes.ModuleName, sequence: 4096, expected: "018d0cab-c4bb-7000-9746-7bcb51fce251"},
		{namespace: billingtypes.ModuleName, sequence: 9223372036854788153, expected: "018d0cab-c4bb-7039-9976-5e45d9fbaef6"},
		{namespace: billingtypes.ModuleName, sequence: 18446744073709551615, expected: "018d0cab-c4bb-7fff-b1bf-f8a198517159"},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%s/%d", test.namespace, test.sequence), func(t *testing.T) {
			actual := uuid.GenerateUUIDv7WithEntropy(blockTime, headerHash, chainID, test.namespace, test.sequence)
			require.Equal(t, test.expected, actual)
			require.Equal(t, test.expected, uuid.GenerateUUIDv7(ctx, test.namespace, test.sequence))

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
		require.Equal(t, "018d0cab-c4bb-7000-866f-1d331a2e834c", uuid.GenerateUUIDv7FromTime(blockTime, billingtypes.ModuleName, 4096))
	})
}
