package types

import "fmt"

// DefaultGenesis returns the default genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:         DefaultParams(),
		Providers:      []Provider{},
		Skus:           []SKU{},
		NextProviderId: 1,
		NextSkuId:      1,
	}
}

// NewGenesisState creates a new genesis state with the given parameters.
func NewGenesisState(params Params, providers []Provider, skus []SKU, nextProviderID, nextSKUID uint64) *GenesisState {
	return &GenesisState{
		Params:         params,
		Providers:      providers,
		Skus:           skus,
		NextProviderId: nextProviderID,
		NextSkuId:      nextSKUID,
	}
}

// Validate performs basic genesis state validation.
func (gs *GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}

	// NextProviderId must be at least 1
	if gs.NextProviderId == 0 {
		return fmt.Errorf("next_provider_id cannot be zero")
	}

	// NextSkuId must be at least 1
	if gs.NextSkuId == 0 {
		return fmt.Errorf("next_sku_id cannot be zero")
	}

	// Validate providers
	seenProviderIDs := make(map[uint64]bool)
	for _, provider := range gs.Providers {
		if seenProviderIDs[provider.Id] {
			return fmt.Errorf("duplicate provider id: %d", provider.Id)
		}
		seenProviderIDs[provider.Id] = true

		if provider.Id >= gs.NextProviderId {
			return fmt.Errorf("provider id %d is greater than or equal to next_provider_id %d", provider.Id, gs.NextProviderId)
		}

		if provider.Address == "" {
			return fmt.Errorf("provider %d has empty address", provider.Id)
		}

		if provider.PayoutAddress == "" {
			return fmt.Errorf("provider %d has empty payout address", provider.Id)
		}
	}

	// Validate SKUs
	seenSKUIDs := make(map[uint64]bool)
	for _, sku := range gs.Skus {
		if seenSKUIDs[sku.Id] {
			return fmt.Errorf("duplicate sku id: %d", sku.Id)
		}
		seenSKUIDs[sku.Id] = true

		if sku.Id >= gs.NextSkuId {
			return fmt.Errorf("sku id %d is greater than or equal to next_sku_id %d", sku.Id, gs.NextSkuId)
		}

		if sku.ProviderId == 0 {
			return fmt.Errorf("sku %d has zero provider_id", sku.Id)
		}

		// Check that provider exists
		if !seenProviderIDs[sku.ProviderId] {
			return fmt.Errorf("sku %d references non-existent provider %d", sku.Id, sku.ProviderId)
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
