package helpers

import (
	"fmt"
	"os"

	"github.com/cosmos/cosmos-sdk/types"
	types2 "github.com/cosmos/cosmos-sdk/x/auth/types"
	types3 "github.com/cosmos/cosmos-sdk/x/gov/types"
)

// GetPoAAdmin returns the address of the PoA admin.
// The default PoA admin is the governance module account.
func GetPoAAdmin() string {
	if addr := os.Getenv("POA_ADMIN_ADDRESS"); addr != "" {
		// Panic if the address is invalid
		_, err := types.AccAddressFromBech32(addr)
		if err != nil {
			panic(fmt.Sprintf("invalid POA_ADMIN_ADDRESS: %s", addr))
		}
		return addr
	}

	return types2.NewModuleAddress(types3.ModuleName).String()
}
