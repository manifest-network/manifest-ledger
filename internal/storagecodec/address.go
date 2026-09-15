package storagecodec

import (
	"bytes"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DecodeAddressStrings converts Bech32 accounts to independent address bytes.
func DecodeAddressStrings(addresses []string) ([][]byte, error) {
	decoded := make([][]byte, 0, len(addresses))
	for _, address := range addresses {
		addr, err := sdk.AccAddressFromBech32(address)
		if err != nil {
			return nil, err
		}
		decoded = append(decoded, bytes.Clone(addr.Bytes()))
	}
	return decoded, nil
}

// EncodeAddressBytes validates and formats stored account addresses.
func EncodeAddressBytes(addresses [][]byte) ([]string, error) {
	encoded := make([]string, 0, len(addresses))
	for _, address := range addresses {
		addressString, err := AccountAddressString(address)
		if err != nil {
			return nil, err
		}
		encoded = append(encoded, addressString)
	}
	return encoded, nil
}

// AccountAddressString validates stored bytes before converting to Bech32.
func AccountAddressString(address []byte) (string, error) {
	if err := sdk.VerifyAddressFormat(address); err != nil {
		return "", err
	}
	return sdk.AccAddress(address).String(), nil
}
