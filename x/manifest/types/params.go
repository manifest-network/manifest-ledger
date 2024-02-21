package types

import (
	"encoding/json"
	fmt "fmt"
)

const (
	// uses a precision of 6 decimal places.
	maxPercentShare = 100_000_000
)

// DefaultParams returns default module parameters.
func DefaultParams() Params {
	return Params{
		StakeHolders: []*StakeHolders{},
	}
}

func NewParams(stakeHolders []*StakeHolders) Params {
	return Params{
		StakeHolders: stakeHolders,
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

	// TODO: if stakeholders is empty, then we can allow I assume. Will ignore upsteram
	if len(p.StakeHolders) == 0 {
		return nil
	}

	var total int64

	for _, sh := range p.StakeHolders {
		total += int64(sh.Percentage)
	}

	if total != maxPercentShare {
		return fmt.Errorf("stakeholders should add up to %d, got %d", maxPercentShare, total)
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
