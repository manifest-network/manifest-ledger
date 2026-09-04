package types

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/gogoproto/jsonpb"
	"github.com/cosmos/gogoproto/proto"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/migrations/legacytx"
)

const (
	legacyUpdateProviderWireHex = "0a126d616e696665737431617574686f72697479122430313931323334352d363738392d376162632d386465662d3031323334353637383961621a116d616e69666573743170726f7669646572220f6d616e6966657374317061796f75742a0301020330013a1c68747470733a2f2f6170692e70726f76696465722e6578616d706c65"
	legacyUpdateProviderJSON    = `{"type":"lifted/sku/MsgUpdateProvider","value":{"authority":"manifest1authority","uuid":"01912345-6789-7abc-8def-0123456789ab","address":"manifest1provider","payout_address":"manifest1payout","meta_hash":"AQID","active":true,"api_url":"https://api.provider.example"}}`
	legacyUpdateProviderSign    = `{"account_number":"1","chain_id":"manifest-test","fee":{"amount":[],"gas":"0"},"memo":"","msgs":[{"type":"lifted/sku/MsgUpdateProvider","value":{"active":true,"address":"manifest1provider","api_url":"https://api.provider.example","authority":"manifest1authority","meta_hash":"AQID","payout_address":"manifest1payout","uuid":"01912345-6789-7abc-8def-0123456789ab"}}],"sequence":"2"}`
)

func legacyUpdateProviderMessage() *MsgUpdateProvider {
	return &MsgUpdateProvider{
		Authority:     "manifest1authority",
		Uuid:          "01912345-6789-7abc-8def-0123456789ab",
		Address:       "manifest1provider",
		PayoutAddress: "manifest1payout",
		MetaHash:      []byte{1, 2, 3},
		Active:        true,
		ApiUrl:        "https://api.provider.example",
	}
}

func TestMsgUpdateProviderAdditiveWireCompatibility(t *testing.T) {
	legacyWire, err := hex.DecodeString(legacyUpdateProviderWireHex)
	require.NoError(t, err)

	legacyMessage := legacyUpdateProviderMessage()
	encoded, err := proto.Marshal(legacyMessage)
	require.NoError(t, err)
	require.Equal(t, legacyWire, encoded)

	var decoded MsgUpdateProvider
	require.NoError(t, proto.Unmarshal(legacyWire, &decoded))
	require.Equal(t, legacyMessage, &decoded)
	require.False(t, decoded.ClearApiUrl)

	clearWire, err := proto.Marshal(&MsgUpdateProvider{ClearApiUrl: true})
	require.NoError(t, err)
	require.Equal(t, []byte{0x40, 0x01}, clearWire)
	var clearDecoded MsgUpdateProvider
	require.NoError(t, proto.Unmarshal(clearWire, &clearDecoded))
	require.True(t, clearDecoded.ClearApiUrl)
}

func TestMsgUpdateProviderJSONCompatibility(t *testing.T) {
	message := &MsgUpdateProvider{ClearApiUrl: true}

	protoJSON, err := codec.ProtoMarshalJSON(message, nil)
	require.NoError(t, err)
	require.Contains(t, string(protoJSON), `"clear_api_url":true`)
	require.NotContains(t, string(protoJSON), `"clearApiUrl"`)
	var protoDecoded MsgUpdateProvider
	require.NoError(t, jsonpb.Unmarshal(strings.NewReader(string(protoJSON)), &protoDecoded))
	require.True(t, protoDecoded.ClearApiUrl)

	gateway := &runtime.JSONPb{OrigName: true}
	gatewayJSON, err := gateway.Marshal(message)
	require.NoError(t, err)
	require.Contains(t, string(gatewayJSON), `"clear_api_url":true`)
	require.NotContains(t, string(gatewayJSON), `"clearApiUrl"`)
	var gatewayDecoded MsgUpdateProvider
	require.NoError(t, gateway.Unmarshal(gatewayJSON, &gatewayDecoded))
	require.True(t, gatewayDecoded.ClearApiUrl)
}

func TestMsgUpdateProviderLegacyAminoCompatibility(t *testing.T) {
	amino := codec.NewLegacyAmino()
	RegisterLegacyAminoCodec(amino)
	legacyMessage := legacyUpdateProviderMessage()

	legacyJSON, err := amino.MarshalJSON(legacyMessage)
	require.NoError(t, err)
	require.JSONEq(t, legacyUpdateProviderJSON, string(legacyJSON))

	previousCodec := legacytx.RegressionTestingAminoCodec
	legacytx.RegressionTestingAminoCodec = amino
	t.Cleanup(func() { legacytx.RegressionTestingAminoCodec = previousCodec })
	legacySignBytes := legacytx.StdSignBytes(
		"manifest-test", 1, 2, 0,
		legacytx.StdFee{}, []sdk.Msg{legacyMessage}, "",
	)
	require.Equal(t, legacyUpdateProviderSign, string(legacySignBytes))

	clearMessage := *legacyMessage
	clearMessage.ApiUrl = ""
	clearMessage.ClearApiUrl = true
	clearJSON, err := amino.MarshalJSON(&clearMessage)
	require.NoError(t, err)
	require.Contains(t, string(clearJSON), `"clear_api_url":true`)
	require.NotContains(t, string(clearJSON), `"api_url"`)
	var clearJSONDecoded MsgUpdateProvider
	require.NoError(t, amino.UnmarshalJSON(clearJSON, &clearJSONDecoded))
	require.True(t, clearJSONDecoded.ClearApiUrl)
	require.Empty(t, clearJSONDecoded.ApiUrl)

	clearBinary, err := amino.Marshal(&clearMessage)
	require.NoError(t, err)
	var clearBinaryDecoded MsgUpdateProvider
	require.NoError(t, amino.Unmarshal(clearBinary, &clearBinaryDecoded))
	require.True(t, clearBinaryDecoded.ClearApiUrl)
	require.Empty(t, clearBinaryDecoded.ApiUrl)

	require.NotPanics(t, func() {
		clearSignBytes := legacytx.StdSignBytes(
			"manifest-test", 1, 2, 0,
			legacytx.StdFee{}, []sdk.Msg{&clearMessage}, "",
		)
		require.Contains(t, string(clearSignBytes), `"clear_api_url":true`)
		require.NotContains(t, string(clearSignBytes), `"api_url"`)
	})
}
