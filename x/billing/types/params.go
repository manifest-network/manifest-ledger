package types

import (
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultDenom is the default billing denomination.
const DefaultDenom = "factory/manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj/upwr"

// DefaultMinCreditBalance is the default minimum credit balance (5 PWR = 5000000 upwr).
var DefaultMinCreditBalance = math.NewInt(5_000_000)

// DefaultMaxLeasesPerTenant is the default maximum number of leases per tenant.
const DefaultMaxLeasesPerTenant = uint64(100)

// DefaultParams returns the default billing module parameters.
func DefaultParams() Params {
	return Params{
		Denom:              DefaultDenom,
		MinCreditBalance:   DefaultMinCreditBalance,
		MaxLeasesPerTenant: DefaultMaxLeasesPerTenant,
		AllowedList:        []string{},
	}
}

// NewParams creates a new Params instance.
func NewParams(denom string, minCreditBalance math.Int, maxLeasesPerTenant uint64, allowedList []string) Params {
	return Params{
		Denom:              denom,
		MinCreditBalance:   minCreditBalance,
		MaxLeasesPerTenant: maxLeasesPerTenant,
		AllowedList:        allowedList,
	}
}

// Validate performs validation on billing parameters.
func (p *Params) Validate() error {
	if p.Denom == "" {
		return ErrInvalidParams.Wrap("denom cannot be empty")
	}

	if p.MinCreditBalance.IsNil() || p.MinCreditBalance.IsNegative() {
		return ErrInvalidParams.Wrap("min_credit_balance cannot be nil or negative")
	}

	if p.MaxLeasesPerTenant == 0 {
		return ErrInvalidParams.Wrap("max_leases_per_tenant must be greater than zero")
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
