package types

import (
	"cosmossdk.io/math"
)

// DefaultDenom is the default billing denomination.
const DefaultDenom = "factory/manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj/upwr"

// DefaultMinCreditBalance is the default minimum credit balance (5 PWR = 5000000 upwr).
var DefaultMinCreditBalance = math.NewInt(5_000_000)

// DefaultMaxLeasesPerTenant is the default maximum number of leases per tenant.
const DefaultMaxLeasesPerTenant = uint64(100)

// DefaultSettlementBatchSize is the default number of leases to settle per EndBlock.
const DefaultSettlementBatchSize = uint64(10)

// DefaultParams returns the default billing module parameters.
func DefaultParams() Params {
	return Params{
		Denom:               DefaultDenom,
		MinCreditBalance:    DefaultMinCreditBalance,
		MaxLeasesPerTenant:  DefaultMaxLeasesPerTenant,
		SettlementBatchSize: DefaultSettlementBatchSize,
	}
}

// NewParams creates a new Params instance.
func NewParams(denom string, minCreditBalance math.Int, maxLeasesPerTenant, settlementBatchSize uint64) Params {
	return Params{
		Denom:               denom,
		MinCreditBalance:    minCreditBalance,
		MaxLeasesPerTenant:  maxLeasesPerTenant,
		SettlementBatchSize: settlementBatchSize,
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

	if p.SettlementBatchSize == 0 {
		return ErrInvalidParams.Wrap("settlement_batch_size must be greater than zero")
	}

	return nil
}
