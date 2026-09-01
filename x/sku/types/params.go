package types

import sdk "github.com/cosmos/cosmos-sdk/types"

// DefaultParams returns the default module parameters.
func DefaultParams() Params {
	return Params{
		AllowedList: []string{},
	}
}

// Validate performs basic validation of the module parameters.
func (p Params) Validate() error {
	seen := make(map[string]struct{}, len(p.AllowedList))
	for _, addr := range p.AllowedList {
		decoded, err := sdk.AccAddressFromBech32(addr)
		if err != nil {
			return ErrInvalidConfig.Wrapf("invalid address in allowed list: %s", addr)
		}
		identity := string(decoded.Bytes())
		if _, exists := seen[identity]; exists {
			return ErrInvalidConfig.Wrapf("duplicate address in allowed list: %s", addr)
		}
		seen[identity] = struct{}{}
	}
	return nil
}

// IsAllowed checks if an address is in the allowed list.
func (p Params) IsAllowed(addr string) bool {
	candidate, err := sdk.AccAddressFromBech32(addr)
	if err != nil {
		return false
	}
	for _, allowed := range p.AllowedList {
		allowedAddress, err := sdk.AccAddressFromBech32(allowed)
		if err == nil && candidate.Equals(allowedAddress) {
			return true
		}
	}
	return false
}

// CanonicalizeAllowedList returns params with equivalent Bech32 spellings
// collapsed in first-seen order and all addresses rendered canonically. It is
// used when importing historical state that predates identity-based duplicate
// validation.
func (p Params) CanonicalizeAllowedList() (Params, error) {
	canonical := make([]string, 0, len(p.AllowedList))
	seen := make(map[string]struct{}, len(p.AllowedList))
	for _, address := range p.AllowedList {
		decoded, err := sdk.AccAddressFromBech32(address)
		if err != nil {
			return Params{}, ErrInvalidConfig.Wrapf("invalid address in allowed list: %s", address)
		}
		identity := string(decoded.Bytes())
		if _, exists := seen[identity]; exists {
			continue
		}
		seen[identity] = struct{}{}
		canonical = append(canonical, decoded.String())
	}
	p.AllowedList = canonical
	return p, nil
}
