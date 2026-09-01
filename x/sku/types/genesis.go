package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

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
func NewGenesisState(params Params, providers []Provider, skus []SKU, providerSequence, skuSequence uint64) *GenesisState {
	return &GenesisState{
		Params:           params,
		Providers:        providers,
		Skus:             skus,
		ProviderSequence: providerSequence,
		SkuSequence:      skuSequence,
	}
}

// PrepareForImport returns a copy of genesis state with historical Bech32
// aliases normalized. Public genesis remains string-based, while keeper state
// is written through raw-byte storage codecs.
func (gs *GenesisState) PrepareForImport() (*GenesisState, error) {
	if gs == nil {
		return nil, ErrInvalidConfig.Wrap("genesis state cannot be nil")
	}

	prepared := *gs
	params, err := gs.Params.CanonicalizeAllowedList()
	if err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	prepared.Params = params
	prepared.Providers = append([]Provider(nil), gs.Providers...)
	for i := range prepared.Providers {
		provider := &prepared.Providers[i]
		address, err := sdk.AccAddressFromBech32(provider.Address)
		if err != nil {
			return nil, ErrInvalidProvider.Wrapf("provider %s has invalid address: %s", provider.Uuid, err)
		}
		payoutAddress, err := sdk.AccAddressFromBech32(provider.PayoutAddress)
		if err != nil {
			return nil, ErrInvalidProvider.Wrapf("provider %s has invalid payout address: %s", provider.Uuid, err)
		}
		provider.Address = address.String()
		provider.PayoutAddress = payoutAddress.String()
	}
	prepared.Skus = append([]SKU(nil), gs.Skus...)
	if err := prepared.validate(); err != nil {
		return nil, err
	}
	return &prepared, nil
}

// Validate performs import-safe genesis state validation. Equivalent historical
// Bech32 spellings are normalized before applying current structural checks.
func (gs *GenesisState) Validate() error {
	_, err := gs.PrepareForImport()
	return err
}

func (gs *GenesisState) validate() error {
	if err := gs.Params.Validate(); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}

	// Validate providers
	seenProviderUUIDs := make(map[string]bool)
	for _, provider := range gs.Providers {
		if err := pkguuid.ValidateUUIDv7(provider.Uuid); err != nil {
			return ErrInvalidProvider.Wrapf("invalid provider uuid %s: %s", provider.Uuid, err)
		}

		if seenProviderUUIDs[provider.Uuid] {
			return ErrInvalidProvider.Wrapf("duplicate provider uuid: %s", provider.Uuid)
		}
		seenProviderUUIDs[provider.Uuid] = true

		if provider.Address == "" {
			return ErrInvalidProvider.Wrapf("provider %s has empty address", provider.Uuid)
		}
		if _, err := sdk.AccAddressFromBech32(provider.Address); err != nil {
			return ErrInvalidProvider.Wrapf("provider %s has invalid address: %s", provider.Uuid, err)
		}

		if provider.PayoutAddress == "" {
			return ErrInvalidProvider.Wrapf("provider %s has empty payout address", provider.Uuid)
		}
		if _, err := sdk.AccAddressFromBech32(provider.PayoutAddress); err != nil {
			return ErrInvalidProvider.Wrapf("provider %s has invalid payout address: %s", provider.Uuid, err)
		}

		// Validate API URL if provided
		if provider.ApiUrl != "" {
			if err := ValidateAPIURL(provider.ApiUrl); err != nil {
				return ErrInvalidProvider.Wrapf("provider %s has invalid api_url: %s", provider.Uuid, err)
			}
		}

		if len(provider.MetaHash) > MaxMetaHashLength {
			return ErrInvalidProvider.Wrapf("provider %s meta_hash exceeds maximum length of %d bytes", provider.Uuid, MaxMetaHashLength)
		}
	}

	// Validate SKUs
	seenSKUUUIDs := make(map[string]bool)
	for _, sku := range gs.Skus {
		if err := pkguuid.ValidateUUIDv7(sku.Uuid); err != nil {
			return ErrInvalidSKU.Wrapf("invalid sku uuid %s: %s", sku.Uuid, err)
		}

		if seenSKUUUIDs[sku.Uuid] {
			return ErrInvalidSKU.Wrapf("duplicate sku uuid: %s", sku.Uuid)
		}
		seenSKUUUIDs[sku.Uuid] = true

		if err := pkguuid.ValidateUUIDv7(sku.ProviderUuid); err != nil {
			return ErrInvalidSKU.Wrapf("sku %s has invalid provider_uuid: %s", sku.Uuid, err)
		}

		// Check that provider exists
		if !seenProviderUUIDs[sku.ProviderUuid] {
			return ErrInvalidSKU.Wrapf("sku %s references non-existent provider %s", sku.Uuid, sku.ProviderUuid)
		}

		if sku.Name == "" {
			return ErrInvalidSKU.Wrapf("sku %s has empty name", sku.Uuid)
		}

		if len(sku.Name) > MaxSKUNameLength {
			return ErrInvalidSKU.Wrapf("sku %s name exceeds maximum length of %d bytes", sku.Uuid, MaxSKUNameLength)
		}

		if sku.Unit == Unit_UNIT_UNSPECIFIED {
			return ErrInvalidSKU.Wrapf("sku %s has unspecified unit", sku.Uuid)
		}

		if !sku.BasePrice.IsValid() || sku.BasePrice.IsZero() {
			return ErrInvalidSKU.Wrapf("sku %s has invalid or zero base price", sku.Uuid)
		}

		// Validate that price and unit combination produces a valid per-second rate
		if err := ValidatePriceAndUnit(sku.BasePrice, sku.Unit); err != nil {
			return ErrInvalidSKU.Wrapf("sku %s has invalid price/unit combination: %s", sku.Uuid, err)
		}

		if len(sku.MetaHash) > MaxMetaHashLength {
			return ErrInvalidSKU.Wrapf("sku %s meta_hash exceeds maximum length of %d bytes", sku.Uuid, MaxMetaHashLength)
		}
	}

	// Validate sequences: must be >= number of entities to prevent UUID
	// collisions after genesis import. Each Create call advances the
	// sequence, so a valid export always has sequence >= len(entities).
	if uint64(len(gs.Providers)) > gs.ProviderSequence {
		return ErrInvalidProvider.Wrapf(
			"provider_sequence %d is less than number of providers %d",
			gs.ProviderSequence, len(gs.Providers),
		)
	}
	if uint64(len(gs.Skus)) > gs.SkuSequence {
		return ErrInvalidSKU.Wrapf(
			"sku_sequence %d is less than number of skus %d",
			gs.SkuSequence, len(gs.Skus),
		)
	}

	return nil
}
