package types

import (
	"encoding/json"
)

// DefaultParams returns default module parameters.
func DefaultParams() Params {
	return NewParams()
}

// NewParams defines the parameters for the module.
func NewParams() Params {
	return Params{}
}

// Stringer method for Params.
func (p *Params) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// Validate does the sanity check on the params.
func (p *Params) Validate() error {
	return nil
}
