package types_test

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"cosmossdk.io/x/tx/signing/aminojson"

	skuv1 "github.com/manifest-network/manifest-ledger/api/liftedinit/sku/v1"
)

func TestMsgUpdateProviderAdditivePulsarCompatibility(t *testing.T) {
	legacyWire, err := hex.DecodeString("0a126d616e696665737431617574686f72697479122430313931323334352d363738392d376162632d386465662d3031323334353637383961621a116d616e69666573743170726f7669646572220f6d616e6966657374317061796f75742a0301020330013a1c68747470733a2f2f6170692e70726f76696465722e6578616d706c65")
	require.NoError(t, err)

	var decoded skuv1.MsgUpdateProvider
	require.NoError(t, proto.Unmarshal(legacyWire, &decoded))
	require.Equal(t, "https://api.provider.example", decoded.ApiUrl)
	require.False(t, decoded.ClearApiUrl)
	remarshaled, err := proto.Marshal(&decoded)
	require.NoError(t, err)
	require.Equal(t, legacyWire, remarshaled)

	clearMessage := &skuv1.MsgUpdateProvider{ClearApiUrl: true}
	clearWire, err := proto.Marshal(clearMessage)
	require.NoError(t, err)
	require.Equal(t, []byte{0x40, 0x01}, clearWire)

	protoJSON, err := protojson.Marshal(clearMessage)
	require.NoError(t, err)
	require.JSONEq(t, `{"clearApiUrl":true}`, string(protoJSON))
	var decodedJSON skuv1.MsgUpdateProvider
	require.NoError(t, protojson.Unmarshal(protoJSON, &decodedJSON))
	require.True(t, decodedJSON.ClearApiUrl)

	protoNameJSON, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(clearMessage)
	require.NoError(t, err)
	require.JSONEq(t, `{"clear_api_url":true}`, string(protoNameJSON))

	aminoEncoder := aminojson.NewEncoder(aminojson.EncoderOptions{})
	legacyAminoJSON, err := aminoEncoder.Marshal(&decoded)
	require.NoError(t, err)
	require.NotContains(t, string(legacyAminoJSON), `"clear_api_url"`)
	clearAminoJSON, err := aminoEncoder.Marshal(clearMessage)
	require.NoError(t, err)
	require.Contains(t, string(clearAminoJSON), `"clear_api_url":true`)
}
