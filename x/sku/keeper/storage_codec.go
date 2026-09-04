package keeper

import (
	"bytes"
	"fmt"

	"github.com/cosmos/gogoproto/proto"

	collcodec "cosmossdk.io/collections/codec"

	sdkcodec "github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

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

type storageValueCodec[T any] struct {
	wire            collcodec.ValueCodec[T]
	prefix          string
	encodeStorage   func(T) ([]byte, error)
	decodeStorage   func([]byte) (T, error)
	normalizeLegacy func(T) (T, error)
}

func (c storageValueCodec[T]) Encode(value T) ([]byte, error) {
	payload, err := c.encodeStorage(value)
	if err != nil {
		return nil, err
	}

	encoded := make([]byte, len(c.prefix)+len(payload))
	copy(encoded, c.prefix)
	copy(encoded[len(c.prefix):], payload)
	return encoded, nil
}

func (c storageValueCodec[T]) Decode(encoded []byte) (T, error) {
	if bytes.HasPrefix(encoded, []byte(c.prefix)) {
		return c.decodeStorage(encoded[len(c.prefix):])
	}
	if len(encoded) > 0 && encoded[0] == 0 {
		var zero T
		return zero, fmt.Errorf("unsupported SKU storage encoding")
	}

	value, err := c.wire.Decode(encoded)
	if err != nil {
		var zero T
		return zero, err
	}
	return c.normalizeLegacy(value)
}

func (c storageValueCodec[T]) EncodeJSON(value T) ([]byte, error) {
	return c.wire.EncodeJSON(value)
}

func (c storageValueCodec[T]) DecodeJSON(encoded []byte) (T, error) {
	return c.wire.DecodeJSON(encoded)
}

func (c storageValueCodec[T]) Stringify(value T) string {
	return c.wire.Stringify(value)
}

func (c storageValueCodec[T]) ValueType() string {
	return c.wire.ValueType()
}

func newParamsValueCodec(cdc sdkcodec.BinaryCodec) collcodec.ValueCodec[types.Params] {
	wire := sdkcodec.CollValue[types.Params](cdc)
	return storageValueCodec[types.Params]{
		wire:   wire,
		prefix: paramsStoragePrefix,
		encodeStorage: func(params types.Params) ([]byte, error) {
			allowedAddresses, err := decodeAddressStrings(params.AllowedList)
			if err != nil {
				return nil, fmt.Errorf("invalid allowed-list address: %w", err)
			}

			return marshalStorage(&skustorage.Params{
				AllowedAddresses: allowedAddresses,
			})
		},
		decodeStorage: func(encoded []byte) (types.Params, error) {
			var stored skustorage.Params
			if err := proto.Unmarshal(encoded, &stored); err != nil {
				return types.Params{}, err
			}

			allowedList, err := encodeAddressBytes(stored.AllowedAddresses)
			if err != nil {
				return types.Params{}, fmt.Errorf("invalid stored allowed-list address: %w", err)
			}
			return types.Params{AllowedList: allowedList}, nil
		},
		normalizeLegacy: normalizeParamsAddresses,
	}
}

func newProviderValueCodec(cdc sdkcodec.BinaryCodec) collcodec.ValueCodec[types.Provider] {
	wire := sdkcodec.CollValue[types.Provider](cdc)
	return storageValueCodec[types.Provider]{
		wire:   wire,
		prefix: providerStoragePrefix,
		encodeStorage: func(provider types.Provider) ([]byte, error) {
			address, err := sdk.AccAddressFromBech32(provider.Address)
			if err != nil {
				return nil, fmt.Errorf("invalid provider address: %w", err)
			}
			payoutAddress, err := sdk.AccAddressFromBech32(provider.PayoutAddress)
			if err != nil {
				return nil, fmt.Errorf("invalid provider payout address: %w", err)
			}

			return marshalStorage(&skustorage.Provider{
				Uuid:          provider.Uuid,
				Address:       append([]byte(nil), address.Bytes()...),
				PayoutAddress: append([]byte(nil), payoutAddress.Bytes()...),
				MetaHash:      append([]byte(nil), provider.MetaHash...),
				Active:        provider.Active,
				ApiUrl:        provider.ApiUrl,
			})
		},
		decodeStorage: func(encoded []byte) (types.Provider, error) {
			var stored skustorage.Provider
			if err := proto.Unmarshal(encoded, &stored); err != nil {
				return types.Provider{}, err
			}

			address, err := accountAddressString(stored.Address)
			if err != nil {
				return types.Provider{}, fmt.Errorf("invalid stored provider address: %w", err)
			}
			payoutAddress, err := accountAddressString(stored.PayoutAddress)
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
		normalizeLegacy: normalizeProviderAddresses,
	}
}

type storageMessage interface {
	proto.Message
	Marshal() ([]byte, error)
}

// marshalStorage uses generated field-order marshaling. The disk-only messages
// deliberately contain no maps and repeated addresses preserve slice order, so
// the generated encoding is deterministic.
func marshalStorage(message storageMessage) ([]byte, error) {
	encoded, err := message.Marshal()
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), encoded...), nil
}

func normalizeParamsAddresses(params types.Params) (types.Params, error) {
	allowedAddresses, err := decodeAddressStrings(params.AllowedList)
	if err != nil {
		return types.Params{}, fmt.Errorf("invalid legacy allowed-list address: %w", err)
	}
	params.AllowedList, err = encodeAddressBytes(allowedAddresses)
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

func decodeAddressStrings(addresses []string) ([][]byte, error) {
	decoded := make([][]byte, 0, len(addresses))
	for _, address := range addresses {
		addr, err := sdk.AccAddressFromBech32(address)
		if err != nil {
			return nil, err
		}
		decoded = append(decoded, append([]byte(nil), addr.Bytes()...))
	}
	return decoded, nil
}

func encodeAddressBytes(addresses [][]byte) ([]string, error) {
	encoded := make([]string, 0, len(addresses))
	for _, address := range addresses {
		addressString, err := accountAddressString(address)
		if err != nil {
			return nil, err
		}
		encoded = append(encoded, addressString)
	}
	return encoded, nil
}

func accountAddressString(address []byte) (string, error) {
	if err := sdk.VerifyAddressFormat(address); err != nil {
		return "", err
	}
	return sdk.AccAddress(address).String(), nil
}
