package keeper

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	collcodec "cosmossdk.io/collections/codec"

	sdkcodec "github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"

	appparams "github.com/manifest-network/manifest-ledger/app/params"
	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

// Exercise both disk-only codecs through real current and legacy protobuf
// decoders. Accepted inputs must canonicalize to deterministic storage bytes;
// malformed envelopes and address encodings may fail but must not panic.
func FuzzSKUStorageValueCodecs(f *testing.F) {
	appparams.SetAddressPrefixes()
	cfg := moduletestutil.MakeTestEncodingConfig()
	manager := sdk.AccAddress(bytes.Repeat([]byte{0x44}, 20))
	payout := sdk.AccAddress(bytes.Repeat([]byte{0x55}, 20))
	params := types.Params{AllowedList: []string{strings.ToUpper(manager.String())}}
	provider := types.Provider{
		Uuid:    "01912345-6789-7abc-8def-0123456789ab",
		Address: strings.ToUpper(manager.String()), PayoutAddress: payout.String(),
		MetaHash: []byte{0xaa, 0xbb}, Active: true, ApiUrl: "https://provider.example",
	}
	paramsCodec, providerCodec := newParamsValueCodec(cfg.Codec), newProviderValueCodec(cfg.Codec)
	seedSKUStorageCodec(f, 0, paramsCodec, sdkcodec.CollValue[types.Params](cfg.Codec), params)
	seedSKUStorageCodec(f, 1, providerCodec, sdkcodec.CollValue[types.Provider](cfg.Codec), provider)
	for kind, prefix := range []string{paramsStoragePrefix, providerStoragePrefix} {
		for length := range len(prefix) + 1 {
			f.Add(uint8(kind), []byte(prefix[:length]))
		}
		f.Add(uint8(kind), append([]byte(prefix), 0xff))
		f.Add(uint8(kind), append([]byte(prefix), 0x0a, 0xff)) // truncated protobuf length
		f.Add(uint8(kind), []byte("\x00sku/unknown/v9"))
		f.Add(uint8(kind), []byte(prefix+"0")) // unsupported multi-digit version
	}
	f.Fuzz(func(t *testing.T, kind uint8, encoded []byte) {
		if len(encoded) > 32*1024 {
			t.Skip()
		}
		if kind%2 == 0 {
			checkSKUStorageCodecFixedPoint(t, paramsCodec, encoded)
		} else {
			checkSKUStorageCodecFixedPoint(t, providerCodec, encoded)
		}
	})
}

func seedSKUStorageCodec[T any](f *testing.F, kind uint8, current, legacy collcodec.ValueCodec[T], value T) {
	f.Helper()
	for _, valueCodec := range []collcodec.ValueCodec[T]{current, legacy} {
		encoded, err := valueCodec.Encode(value)
		require.NoError(f, err)
		f.Add(kind, encoded)
	}
}

func checkSKUStorageCodecFixedPoint[T any](t *testing.T, valueCodec collcodec.ValueCodec[T], encoded []byte) {
	t.Helper()
	snapshot := bytes.Clone(encoded)
	value, err := valueCodec.Decode(encoded)
	require.Equal(t, snapshot, encoded, "decoding must not mutate its input")
	if err != nil {
		return
	}
	canonical, err := valueCodec.Encode(value)
	require.NoError(t, err)
	repeated, err := valueCodec.Encode(value)
	require.NoError(t, err)
	require.Equal(t, canonical, repeated, "encoding the same value must be deterministic")
	roundTrip, err := valueCodec.Decode(canonical)
	require.NoError(t, err)
	reencoded, err := valueCodec.Encode(roundTrip)
	require.NoError(t, err)
	require.Equal(t, canonical, reencoded, "legacy and current encodings must reach a stable canonical form")
}
