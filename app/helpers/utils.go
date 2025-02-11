package helpers

import (
	"fmt"
	"os"

	"github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
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

	return authtypes.NewModuleAddress(govtypes.ModuleName).String()
}
