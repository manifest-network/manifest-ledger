package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

func TestBlockedAddressesAllowsOnlyGovernanceModuleToReceive(t *testing.T) {
	blocked := BlockedAddresses()
	for module := range GetMaccPerms() {
		address := authtypes.NewModuleAddress(module).String()
		if module == govtypes.ModuleName {
			require.NotContains(t, blocked, address)
		} else {
			require.True(t, blocked[address], "module %s must remain blocked", module)
		}
	}
	// Callers receive independent maps; modifying one must not change policy.
	clear(blocked)
	require.NotEmpty(t, BlockedAddresses())
}
