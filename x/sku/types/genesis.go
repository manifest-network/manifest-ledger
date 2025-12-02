package types

import "fmt"

// DefaultGenesis returns the default genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Skus:   []SKU{},
		NextId: 1,
	}
}

// NewGenesisState creates a new genesis state with the given parameters.
func NewGenesisState(skus []SKU, nextID uint64) *GenesisState {
	return &GenesisState{
		Skus:   skus,
		NextId: nextID,
	}
}

// Validate performs basic genesis state validation.
func (gs *GenesisState) Validate() error {
	seenIDs := make(map[uint64]bool)
	for _, sku := range gs.Skus {
		if seenIDs[sku.Id] {
			return fmt.Errorf("duplicate sku id: %d", sku.Id)
		}
		seenIDs[sku.Id] = true

		if sku.Id >= gs.NextId {
			return fmt.Errorf("sku id %d is greater than or equal to next_id %d", sku.Id, gs.NextId)
		}

		if sku.Provider == "" {
			return fmt.Errorf("sku %d has empty provider", sku.Id)
		}

		if sku.Name == "" {
			return fmt.Errorf("sku %d has empty name", sku.Id)
		}

		if !sku.BasePrice.IsValid() {
			return fmt.Errorf("sku %d has invalid base price", sku.Id)
		}
	}

	return nil
}
