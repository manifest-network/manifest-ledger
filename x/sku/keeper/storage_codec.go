package keeper

import (
	"fmt"

	"github.com/cosmos/gogoproto/proto"

	collcodec "cosmossdk.io/collections/codec"

	sdkcodec "github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/internal/storagecodec"
	skustorage "github.com/manifest-network/manifest-ledger/x/sku/internal/types"
	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

// SKU API protobufs use Bech32 strings at the wire boundary. These value
// codecs retain those public Go types while persisting account identities as
// raw bytes. A tag-zero prefix is an unambiguous format discriminator: field
// number zero is invalid protobuf, so no legacy CollValue payload can begin
// with it.
const (
	paramsStoragePrefix   = "\x00sku/params/v1"
	providerStoragePrefix = "\x00sku/provider/v1"
)

func newParamsValueCodec(cdc sdkcodec.BinaryCodec) collcodec.ValueCodec[types.Params] {
	wire := sdkcodec.CollValue[types.Params](cdc)
	return storagecodec.New(storagecodec.Config[types.Params]{
		Wire:   wire,
		Module: "SKU",
		Prefix: paramsStoragePrefix,
		EncodeStorage: func(params types.Params) ([]byte, error) {
			allowedAddresses, err := storagecodec.DecodeAddressStrings(params.AllowedList)
			if err != nil {
				return nil, fmt.Errorf("invalid allowed-list address: %w", err)
			}

			return storagecodec.Marshal(&skustorage.Params{
				AllowedAddresses: allowedAddresses,
			})
		},
		DecodeStorage: func(encoded []byte) (types.Params, error) {
			var stored skustorage.Params
			if err := proto.Unmarshal(encoded, &stored); err != nil {
				return types.Params{}, err
			}

			allowedList, err := storagecodec.EncodeAddressBytes(stored.AllowedAddresses)
			if err != nil {
				return types.Params{}, fmt.Errorf("invalid stored allowed-list address: %w", err)
			}
			return types.Params{AllowedList: allowedList}, nil
		},
		NormalizeLegacy: normalizeParamsAddresses,
	})
}

func newProviderValueCodec(cdc sdkcodec.BinaryCodec) collcodec.ValueCodec[types.Provider] {
	wire := sdkcodec.CollValue[types.Provider](cdc)
	return storagecodec.New(storagecodec.Config[types.Provider]{
		Wire:   wire,
		Module: "SKU",
		Prefix: providerStoragePrefix,
		EncodeStorage: func(provider types.Provider) ([]byte, error) {
			address, err := sdk.AccAddressFromBech32(provider.Address)
			if err != nil {
				return nil, fmt.Errorf("invalid provider address: %w", err)
			}
			payoutAddress, err := sdk.AccAddressFromBech32(provider.PayoutAddress)
			if err != nil {
				return nil, fmt.Errorf("invalid provider payout address: %w", err)
			}

			return storagecodec.Marshal(&skustorage.Provider{
				Uuid:          provider.Uuid,
				Address:       append([]byte(nil), address.Bytes()...),
				PayoutAddress: append([]byte(nil), payoutAddress.Bytes()...),
				MetaHash:      append([]byte(nil), provider.MetaHash...),
				Active:        provider.Active,
				ApiUrl:        provider.ApiUrl,
			})
		},
		DecodeStorage: func(encoded []byte) (types.Provider, error) {
			var stored skustorage.Provider
			if err := proto.Unmarshal(encoded, &stored); err != nil {
				return types.Provider{}, err
			}

			address, err := storagecodec.AccountAddressString(stored.Address)
			if err != nil {
				return types.Provider{}, fmt.Errorf("invalid stored provider address: %w", err)
			}
			payoutAddress, err := storagecodec.AccountAddressString(stored.PayoutAddress)
			if err != nil {
				return types.Provider{}, fmt.Errorf("invalid stored provider payout address: %w", err)
			}

			return types.Provider{
				Uuid:          stored.Uuid,
				Address:       address,
				PayoutAddress: payoutAddress,
				MetaHash:      append([]byte(nil), stored.MetaHash...),
				Active:        stored.Active,
				ApiUrl:        stored.ApiUrl,
			}, nil
		},
		NormalizeLegacy: normalizeProviderAddresses,
	})
}

func normalizeParamsAddresses(params types.Params) (types.Params, error) {
	allowedAddresses, err := storagecodec.DecodeAddressStrings(params.AllowedList)
	if err != nil {
		return types.Params{}, fmt.Errorf("invalid legacy allowed-list address: %w", err)
	}
	params.AllowedList, err = storagecodec.EncodeAddressBytes(allowedAddresses)
	return params, err
}

func normalizeProviderAddresses(provider types.Provider) (types.Provider, error) {
	address, err := sdk.AccAddressFromBech32(provider.Address)
	if err != nil {
		return types.Provider{}, fmt.Errorf("invalid legacy provider address: %w", err)
	}
	payoutAddress, err := sdk.AccAddressFromBech32(provider.PayoutAddress)
	if err != nil {
		return types.Provider{}, fmt.Errorf("invalid legacy provider payout address: %w", err)
	}
	provider.Address = address.String()
	provider.PayoutAddress = payoutAddress.String()
	return provider, nil
}
