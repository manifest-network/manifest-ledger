package types

import (
	"encoding/json"
	"fmt"
)

// MarshalJSON implements the json.Marshaler interface for Unit.
func (u Unit) MarshalJSON() ([]byte, error) {
	return json.Marshal(u.String())
}

// UnmarshalJSON implements the json.Unmarshaler interface for Unit.
func (u *Unit) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		// Try unmarshaling as int for backward compatibility
		var i int32
		if err := json.Unmarshal(data, &i); err != nil {
			return fmt.Errorf("Unit should be a string or int, got %s", data)
		}
		*u = Unit(i)
		return nil
	}

	value, ok := Unit_value[s]
	if !ok {
		return fmt.Errorf("invalid Unit value: %s", s)
	}
	*u = Unit(value)
	return nil
}
