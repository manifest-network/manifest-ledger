package types

import (
	"fmt"

	pkguuid "github.com/manifest-network/manifest-ledger/pkg/uuid"
)

// DefaultGenesis returns the default genesis state.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:    DefaultParams(),
		Providers: []Provider{},
		Skus:      []SKU{},
	}
}

// NewGenesisState creates a new genesis state with the given parameters.
func NewGenesisState(params Params, providers []Provider, skus []SKU) *GenesisState {
	return &GenesisState{
		Params:    params,
		Providers: providers,
		Skus:      skus,
	}
}

// Validate performs basic genesis state validation.
func (gs *GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}

	// Validate providers
	seenProviderUUIDs := make(map[string]bool)
	for _, provider := range gs.Providers {
		if err := pkguuid.ValidateUUIDv7(provider.Uuid); err != nil {
			return fmt.Errorf("invalid provider uuid %s: %w", provider.Uuid, err)
		}

		if seenProviderUUIDs[provider.Uuid] {
			return fmt.Errorf("duplicate provider uuid: %s", provider.Uuid)
		}
		seenProviderUUIDs[provider.Uuid] = true

		if provider.Address == "" {
			return fmt.Errorf("provider %s has empty address", provider.Uuid)
		}

		if provider.PayoutAddress == "" {
			return fmt.Errorf("provider %s has empty payout address", provider.Uuid)
		}
	}

	// Validate SKUs
	seenSKUUUIDs := make(map[string]bool)
	for _, sku := range gs.Skus {
		if err := pkguuid.ValidateUUIDv7(sku.Uuid); err != nil {
			return fmt.Errorf("invalid sku uuid %s: %w", sku.Uuid, err)
		}

		if seenSKUUUIDs[sku.Uuid] {
			return fmt.Errorf("duplicate sku uuid: %s", sku.Uuid)
		}
		seenSKUUUIDs[sku.Uuid] = true

		if err := pkguuid.ValidateUUIDv7(sku.ProviderUuid); err != nil {
			return fmt.Errorf("sku %s has invalid provider_uuid: %w", sku.Uuid, err)
		}

		// Check that provider exists
		if !seenProviderUUIDs[sku.ProviderUuid] {
			return fmt.Errorf("sku %s references non-existent provider %s", sku.Uuid, sku.ProviderUuid)
		}

		if sku.Name == "" {
			return fmt.Errorf("sku %s has empty name", sku.Uuid)
		}

		if len(sku.Name) > MaxSKUNameLength {
			return fmt.Errorf("sku %s name exceeds maximum length of %d characters", sku.Uuid, MaxSKUNameLength)
		}

		if sku.Unit == Unit_UNIT_UNSPECIFIED {
			return fmt.Errorf("sku %s has unspecified unit", sku.Uuid)
		}

		if !sku.BasePrice.IsValid() || sku.BasePrice.IsZero() {
			return fmt.Errorf("sku %s has invalid or zero base price", sku.Uuid)
		}
	}

	return nil
}
