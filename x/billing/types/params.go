package types

import (
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

// DefaultMaxPendingLeasesPerTenant is the default maximum number of pending leases per tenant.
// Set to 10 to prevent spam attacks while allowing reasonable concurrent lease requests.
const DefaultMaxPendingLeasesPerTenant = uint64(10)

// DefaultPendingTimeout is the default duration in seconds that a lease can remain in PENDING state.
// Set to 1800 (30 minutes) - providers have 30 minutes to acknowledge or reject a lease.
const DefaultPendingTimeout = uint64(1800)

// MinPendingTimeout is the minimum allowed pending timeout (1 minute).
const MinPendingTimeout = uint64(60)

// MaxPendingTimeout is the maximum allowed pending timeout (24 hours).
const MaxPendingTimeout = uint64(86400)

// DefaultParams returns the default billing module parameters.
func DefaultParams() Params {
	return Params{
		MaxLeasesPerTenant:        DefaultMaxLeasesPerTenant,
		AllowedList:               []string{},
		MaxItemsPerLease:          DefaultMaxItemsPerLease,
		MinLeaseDuration:          DefaultMinLeaseDuration,
		MaxPendingLeasesPerTenant: DefaultMaxPendingLeasesPerTenant,
		PendingTimeout:            DefaultPendingTimeout,
	}
}

// NewParams creates a new Params instance.
func NewParams(maxLeasesPerTenant uint64, allowedList []string, maxItemsPerLease uint64, minLeaseDuration uint64, maxPendingLeasesPerTenant uint64, pendingTimeout uint64) Params {
	return Params{
		MaxLeasesPerTenant:        maxLeasesPerTenant,
		AllowedList:               allowedList,
		MaxItemsPerLease:          maxItemsPerLease,
		MinLeaseDuration:          minLeaseDuration,
		MaxPendingLeasesPerTenant: maxPendingLeasesPerTenant,
		PendingTimeout:            pendingTimeout,
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

	if p.MaxItemsPerLease > MaxItemsPerLeaseHardLimit {
		return ErrInvalidParams.Wrapf("max_items_per_lease %d exceeds hard limit of %d", p.MaxItemsPerLease, MaxItemsPerLeaseHardLimit)
	}

	if p.MinLeaseDuration == 0 {
		return ErrInvalidParams.Wrap("min_lease_duration must be greater than zero")
	}

	if p.MaxPendingLeasesPerTenant == 0 {
		return ErrInvalidParams.Wrap("max_pending_leases_per_tenant must be greater than zero")
	}

	if p.PendingTimeout < MinPendingTimeout {
		return ErrInvalidParams.Wrapf("pending_timeout must be at least %d seconds (1 minute)", MinPendingTimeout)
	}

	if p.PendingTimeout > MaxPendingTimeout {
		return ErrInvalidParams.Wrapf("pending_timeout must be at most %d seconds (24 hours)", MaxPendingTimeout)
	}

	// Validate allowed list addresses
	seen := make(map[string]bool)
	for _, addr := range p.AllowedList {
		if _, err := sdk.AccAddressFromBech32(addr); err != nil {
			return ErrInvalidParams.Wrapf("invalid address in allowed list: %s", addr)
		}
		if seen[addr] {
			return ErrInvalidParams.Wrapf("duplicate address in allowed list: %s", addr)
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
