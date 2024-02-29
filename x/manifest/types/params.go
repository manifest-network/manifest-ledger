package types

import (
	"encoding/json"
	fmt "fmt"
)

const (
	// uses a precision of 6 decimal places.
	MaxPercentShare = 100_000_000
)

// DefaultParams returns default module parameters.
func DefaultParams() Params {
	return Params{
		StakeHolders: []*StakeHolders{},
		Inflation:    NewInflation(false, 0, "stake"),
	}
}

// Params defines the parameters for the module.
func NewParams(stakeHolders []*StakeHolders, autoInflationEnabled bool, perYearInflation uint64, denom string) Params {
	return Params{
		StakeHolders: stakeHolders,
		Inflation:    NewInflation(autoInflationEnabled, perYearInflation, denom),
	}
}

func NewInflation(autoInflationEnabled bool, perYearInflation uint64, denom string) *Inflation {
	return &Inflation{
		AutomaticEnabled: autoInflationEnabled,
		YearlyAmount:     perYearInflation,
		MintDenom:        denom,
	}
}

// Stringer method for Params.
func (p Params) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// Validate does the sanity check on the params.
func (p Params) Validate() error {
	if len(p.StakeHolders) != 0 {
		return nil
	}

	var total int64

	for _, sh := range p.StakeHolders {
		total += int64(sh.Percentage)
	}

	if total != MaxPercentShare {
		return fmt.Errorf("stakeholders should add up to %d, got %d", MaxPercentShare, total)
	}

	seen := make(map[string]struct{})
	for _, sh := range p.StakeHolders {
		if _, ok := seen[sh.Address]; ok {
			return fmt.Errorf("duplicate address: %s", sh.Address)
		}
		seen[sh.Address] = struct{}{}
	}

	return nil
}
