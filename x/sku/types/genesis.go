package types

import "fmt"

// DefaultGenesis returns the default genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params: DefaultParams(),
		Skus:   []SKU{},
		NextId: 1,
	}
}

// NewGenesisState creates a new genesis state with the given parameters.
func NewGenesisState(params Params, skus []SKU, nextID uint64) *GenesisState {
	return &GenesisState{
		Params: params,
		Skus:   skus,
		NextId: nextID,
	}
}

// Validate performs basic genesis state validation.
func (gs *GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}

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

		if sku.Unit == Unit_UNIT_UNSPECIFIED {
			return fmt.Errorf("sku %d has unspecified unit", sku.Id)
		}

		if !sku.BasePrice.IsValid() || sku.BasePrice.IsZero() {
			return fmt.Errorf("sku %d has invalid or zero base price", sku.Id)
		}
	}

	return nil
}
