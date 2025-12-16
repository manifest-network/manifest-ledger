package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultMaxLeasesPerTenant is the default maximum number of leases per tenant.
const DefaultMaxLeasesPerTenant = uint64(100)

// DefaultMaxItemsPerLease is the default maximum number of items per lease.
// Set to 20 to balance flexibility with gas consumption.
const DefaultMaxItemsPerLease = uint64(20)

// DefaultMinLeaseDuration is the default minimum lease duration in seconds.
// Set to 3600 (1 hour) - tenants must have enough credit to cover at least 1 hour of lease charges.
const DefaultMinLeaseDuration = uint64(3600)

// DefaultParams returns the default billing module parameters.
func DefaultParams() Params {
	return Params{
		MaxLeasesPerTenant: DefaultMaxLeasesPerTenant,
		AllowedList:        []string{},
		MaxItemsPerLease:   DefaultMaxItemsPerLease,
		MinLeaseDuration:   DefaultMinLeaseDuration,
	}
}

// NewParams creates a new Params instance.
func NewParams(maxLeasesPerTenant uint64, allowedList []string, maxItemsPerLease uint64, minLeaseDuration uint64) Params {
	return Params{
		MaxLeasesPerTenant: maxLeasesPerTenant,
		AllowedList:        allowedList,
		MaxItemsPerLease:   maxItemsPerLease,
		MinLeaseDuration:   minLeaseDuration,
	}
}

// Validate performs validation on billing parameters.
func (p *Params) Validate() error {
	if p.MaxLeasesPerTenant == 0 {
		return ErrInvalidParams.Wrap("max_leases_per_tenant must be greater than zero")
	}

	if p.MaxItemsPerLease == 0 {
		return ErrInvalidParams.Wrap("max_items_per_lease must be greater than zero")
	}

	if p.MinLeaseDuration == 0 {
		return ErrInvalidParams.Wrap("min_lease_duration must be greater than zero")
	}

	// Validate allowed list addresses
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
