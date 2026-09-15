package storagecodec

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestAddressConversionPreservesOrderAndIndependentBytes(t *testing.T) {
	first := sdk.AccAddress([]byte("12345678901234567890")).String()
	second := sdk.AccAddress([]byte("09876543210987654321")).String()
	input := []string{first, strings.ToUpper(second), first}
	decoded, err := DecodeAddressStrings(input)
	require.NoError(t, err)
	encoded, err := EncodeAddressBytes(decoded)
	require.NoError(t, err)
	require.Equal(t, []string{first, second, first}, encoded)
	decoded[0][0] ^= 0xff
	require.NotEqual(t, decoded[0], decoded[2], "repeated addresses must not alias")
	_, err = DecodeAddressStrings([]string{"invalid"})
	require.Error(t, err)
	_, err = EncodeAddressBytes([][]byte{nil})
	require.Error(t, err)
	_, err = AccountAddressString(bytes.Repeat([]byte{1}, 300))
	require.Error(t, err)
	decoded, err = DecodeAddressStrings(nil)
	require.NoError(t, err)
	require.NotNil(t, decoded)
	encoded, err = EncodeAddressBytes(nil)
	require.NoError(t, err)
	require.NotNil(t, encoded)
}

func TestMarshalPreservesGeneratedBytesAndErrors(t *testing.T) {
	message := &testStorageMessage{payload: []byte{0x0a, 0x01, 'x'}}
	encoded, err := Marshal(message)
	require.NoError(t, err)
	require.Equal(t, message.payload, encoded)
	message.payload[2] = 'y'
	require.Equal(t, byte('x'), encoded[2])
	fault := errors.New("marshal failed")
	message.err = fault
	encoded, err = Marshal(message)
	require.ErrorIs(t, err, fault)
	require.Nil(t, encoded)
}

type testStorageMessage struct {
	payload []byte
	err     error
}

func (*testStorageMessage) Reset()                     {}
func (*testStorageMessage) String() string             { return "test" }
func (*testStorageMessage) ProtoMessage()              {}
func (m *testStorageMessage) Marshal() ([]byte, error) { return m.payload, m.err }
