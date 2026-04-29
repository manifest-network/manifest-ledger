package types

import (
	"slices"

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

// MaxLeasesPerTenantUpperBound is the maximum allowed value for max_leases_per_tenant (10,000).
const MaxLeasesPerTenantUpperBound = uint64(10_000)

// MaxPendingLeasesPerTenantUpperBound is the maximum allowed value for max_pending_leases_per_tenant (1,000).
const MaxPendingLeasesPerTenantUpperBound = uint64(1_000)

// MaxMinLeaseDuration is the maximum allowed value for min_lease_duration (30 days).
const MaxMinLeaseDuration = uint64(30 * 24 * 3600)

// DefaultParams returns the default billing module parameters.
// ReservedDomainSuffixes is intentionally empty — operators are expected to
// seed provider wildcard zones via genesis JSON for new chains, or via
// MsgUpdateParams shortly after a v1→v2 upgrade for existing chains. The
// migration itself is a no-op (no hardcoded hostnames in the binary).
func DefaultParams() Params {
	return Params{
		MaxLeasesPerTenant:        DefaultMaxLeasesPerTenant,
		AllowedList:               []string{},
		MaxItemsPerLease:          DefaultMaxItemsPerLease,
		MinLeaseDuration:          DefaultMinLeaseDuration,
		MaxPendingLeasesPerTenant: DefaultMaxPendingLeasesPerTenant,
		PendingTimeout:            DefaultPendingTimeout,
		ReservedDomainSuffixes:    nil,
	}
}

// NewParams creates a new Params instance using the v1 field set only.
// ReservedDomainSuffixes (introduced in v2) is left at its zero value (nil);
// callers who need to populate it should mutate the returned struct directly.
// Kept signature-stable to avoid touching the ~30 existing test callers.
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

	if p.MaxLeasesPerTenant > MaxLeasesPerTenantUpperBound {
		return ErrInvalidParams.Wrapf("max_leases_per_tenant %d exceeds upper bound of %d", p.MaxLeasesPerTenant, MaxLeasesPerTenantUpperBound)
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

	if p.MinLeaseDuration > MaxMinLeaseDuration {
		return ErrInvalidParams.Wrapf("min_lease_duration %d exceeds upper bound of %d seconds (30 days)", p.MinLeaseDuration, MaxMinLeaseDuration)
	}

	if p.MaxPendingLeasesPerTenant == 0 {
		return ErrInvalidParams.Wrap("max_pending_leases_per_tenant must be greater than zero")
	}

	if p.MaxPendingLeasesPerTenant > MaxPendingLeasesPerTenantUpperBound {
		return ErrInvalidParams.Wrapf("max_pending_leases_per_tenant %d exceeds upper bound of %d", p.MaxPendingLeasesPerTenant, MaxPendingLeasesPerTenantUpperBound)
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

	// Validate reserved domain suffixes: each entry must start with '.' and
	// the substring after the dot must be a valid FQDN. Duplicates rejected.
	seenSuffix := make(map[string]bool)
	for _, s := range p.ReservedDomainSuffixes {
		if len(s) < 2 || s[0] != '.' {
			return ErrInvalidParams.Wrapf("reserved domain suffix must begin with '.': %q", s)
		}
		if err := IsValidFQDN(s[1:]); err != nil {
			return ErrInvalidParams.Wrapf("invalid reserved domain suffix %q: %s", s, err)
		}
		if seenSuffix[s] {
			return ErrInvalidParams.Wrapf("duplicate reserved domain suffix: %q", s)
		}
		seenSuffix[s] = true
	}

	return nil
}

// IsAllowed checks if an address is in the allowed list.
func (p Params) IsAllowed(addr string) bool {
	return slices.Contains(p.AllowedList, addr)
}
