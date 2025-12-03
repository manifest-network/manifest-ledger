package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultParams returns the default module parameters.
func DefaultParams() Params {
	return Params{
		AllowedList: []string{},
	}
}

// Validate performs basic validation of the module parameters.
func (p Params) Validate() error {
	seen := make(map[string]bool)
	for _, addr := range p.AllowedList {
		if _, err := sdk.AccAddressFromBech32(addr); err != nil {
			return fmt.Errorf("invalid address in allowed list: %s", addr)
		}
		if seen[addr] {
			return fmt.Errorf("duplicate address in allowed list: %s", addr)
		}
		seen[addr] = true
	}
	return nil
}

// IsAllowed checks if an address is in the allowed list.
func (p Params) IsAllowed(addr string) bool {
	for _, allowed := range p.AllowedList {
		if allowed == addr {
			return true
		}
	}
	return false
}
