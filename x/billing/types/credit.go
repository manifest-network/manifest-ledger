package types

import (
	"crypto/sha256"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/address"
)

// DeriveCreditAddress derives a deterministic credit account address from a tenant address.
// The address is derived by hashing the module name prefix with the tenant address.
func DeriveCreditAddress(tenant sdk.AccAddress) sdk.AccAddress {
	key := append([]byte(CreditAccountAddressPrefix), tenant.Bytes()...)
	hash := sha256.Sum256(key)
	return address.Module(ModuleName, hash[:])
}

// DeriveCreditAddressFromBech32 derives a credit account address from a bech32 tenant address string.
func DeriveCreditAddressFromBech32(tenant string) (sdk.AccAddress, error) {
	tenantAddr, err := sdk.AccAddressFromBech32(tenant)
	if err != nil {
		return nil, err
	}
	return DeriveCreditAddress(tenantAddr), nil
}
