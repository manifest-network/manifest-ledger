package keeper

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	sdkcodec "github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"

	appparams "github.com/manifest-network/manifest-ledger/app/params"
	skustorage "github.com/manifest-network/manifest-ledger/x/sku/internal/types"
	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

func TestStorageValueCodecsPersistRawAddressBytes(t *testing.T) {
	appparams.SetAddressPrefixes()
	cfg := moduletestutil.MakeTestEncodingConfig()
	manager := sdk.AccAddress(bytes.Repeat([]byte{0x11}, 20))
	payout := sdk.AccAddress(bytes.Repeat([]byte{0x22}, 20))
	allowed := sdk.AccAddress(bytes.Repeat([]byte{0x33}, 20))

	params := types.Params{AllowedList: []string{allowed.String()}}
	provider := types.Provider{
		Uuid:          "01912345-6789-7abc-8def-0123456789ab",
		Address:       manager.String(),
		PayoutAddress: payout.String(),
		MetaHash:      []byte{0xaa, 0xbb},
		Active:        true,
		ApiUrl:        "https://provider.example",
	}

	t.Run("params", func(t *testing.T) {
		valueCodec := newParamsValueCodec(cfg.Codec)
		encoded, err := valueCodec.Encode(params)
		require.NoError(t, err)
		require.True(t, bytes.HasPrefix(encoded, []byte(paramsStoragePrefix)))
		require.True(t, bytes.Contains(encoded, allowed.Bytes()))
		require.False(t, bytes.Contains(encoded, []byte(allowed.String())))

		decoded, err := valueCodec.Decode(encoded)
		require.NoError(t, err)
		require.Equal(t, params, decoded)
	})

	t.Run("provider", func(t *testing.T) {
		valueCodec := newProviderValueCodec(cfg.Codec)
		encoded, err := valueCodec.Encode(provider)
		require.NoError(t, err)
		require.True(t, bytes.HasPrefix(encoded, []byte(providerStoragePrefix)))
		require.True(t, bytes.Contains(encoded, manager.Bytes()))
		require.True(t, bytes.Contains(encoded, payout.Bytes()))
		require.False(t, bytes.Contains(encoded, []byte(manager.String())))
		require.False(t, bytes.Contains(encoded, []byte(payout.String())))

		decoded, err := valueCodec.Decode(encoded)
		require.NoError(t, err)
		require.Equal(t, provider, decoded)
	})
}

func TestStorageValueCodecsCanonicalizeEquivalentBech32(t *testing.T) {
	appparams.SetAddressPrefixes()
	cfg := moduletestutil.MakeTestEncodingConfig()
	manager := sdk.AccAddress(bytes.Repeat([]byte{0x44}, 20))
	payout := sdk.AccAddress(bytes.Repeat([]byte{0x55}, 20))

	paramsCodec := newParamsValueCodec(cfg.Codec)
	lowerParams, err := paramsCodec.Encode(types.Params{AllowedList: []string{manager.String()}})
	require.NoError(t, err)
	upperParams, err := paramsCodec.Encode(types.Params{AllowedList: []string{strings.ToUpper(manager.String())}})
	require.NoError(t, err)
	require.Equal(t, lowerParams, upperParams)

	providerCodec := newProviderValueCodec(cfg.Codec)
	provider := types.Provider{Address: manager.String(), PayoutAddress: payout.String()}
	lowerProvider, err := providerCodec.Encode(provider)
	require.NoError(t, err)
	provider.Address = strings.ToUpper(provider.Address)
	provider.PayoutAddress = strings.ToUpper(provider.PayoutAddress)
	upperProvider, err := providerCodec.Encode(provider)
	require.NoError(t, err)
	require.Equal(t, lowerProvider, upperProvider)
}

func TestStorageValueCodecsDecodeLegacyAndRejectCorruptVersionedValues(t *testing.T) {
	appparams.SetAddressPrefixes()
	cfg := moduletestutil.MakeTestEncodingConfig()
	manager := sdk.AccAddress(bytes.Repeat([]byte{0x66}, 20))
	payout := sdk.AccAddress(bytes.Repeat([]byte{0x77}, 20))

	t.Run("params", func(t *testing.T) {
		legacyValue := types.Params{AllowedList: []string{strings.ToUpper(manager.String())}}
		legacy, err := sdkcodec.CollValue[types.Params](cfg.Codec).Encode(legacyValue)
		require.NoError(t, err)

		valueCodec := newParamsValueCodec(cfg.Codec)
		decoded, err := valueCodec.Decode(legacy)
		require.NoError(t, err)
		require.Equal(t, []string{manager.String()}, decoded.AllowedList)
		_, err = valueCodec.Decode(append([]byte(paramsStoragePrefix), 0xff))
		require.Error(t, err)
		_, err = valueCodec.Decode([]byte("\x00sku/params/v999"))
		require.ErrorContains(t, err, "unsupported SKU storage encoding")
	})

	t.Run("provider", func(t *testing.T) {
		legacyValue := types.Provider{
			Uuid:          "01912345-6789-7abc-8def-0123456789ab",
			Address:       strings.ToUpper(manager.String()),
			PayoutAddress: strings.ToUpper(payout.String()),
			MetaHash:      []byte{0xaa},
			Active:        true,
			ApiUrl:        "https://provider.example",
		}
		legacy, err := sdkcodec.CollValue[types.Provider](cfg.Codec).Encode(legacyValue)
		require.NoError(t, err)

		valueCodec := newProviderValueCodec(cfg.Codec)
		decoded, err := valueCodec.Decode(legacy)
		require.NoError(t, err)
		require.Equal(t, manager.String(), decoded.Address)
		require.Equal(t, payout.String(), decoded.PayoutAddress)
		require.Equal(t, legacyValue.Uuid, decoded.Uuid)
		require.Equal(t, legacyValue.MetaHash, decoded.MetaHash)
		require.Equal(t, legacyValue.Active, decoded.Active)
		require.Equal(t, legacyValue.ApiUrl, decoded.ApiUrl)
		_, err = valueCodec.Decode(append([]byte(providerStoragePrefix), 0xff))
		require.Error(t, err)
		_, err = valueCodec.Decode([]byte("\x00sku/provider/v999"))
		require.ErrorContains(t, err, "unsupported SKU storage encoding")

		invalidStored, err := (&skustorage.Provider{
			PayoutAddress: payout.Bytes(),
		}).Marshal()
		require.NoError(t, err)
		_, err = valueCodec.Decode(append([]byte(providerStoragePrefix), invalidStored...))
		require.ErrorContains(t, err, "invalid stored provider address")
	})
}
