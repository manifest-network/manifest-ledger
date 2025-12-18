package types

import "cosmossdk.io/collections"

// Storage prefixes for collections.
var (
	// ParamsKey saves the module parameters.
	ParamsKey = collections.NewPrefix(0)

	// SKUKey saves the SKUs.
	SKUKey = collections.NewPrefix(1)

	// SKUSequenceKey saves the next SKU id.
	SKUSequenceKey = collections.NewPrefix(2)

	// SKUByProviderIndexKey saves the provider index for SKUs.
	SKUByProviderIndexKey = collections.NewPrefix(3)

	// ProviderKey saves the Providers.
	ProviderKey = collections.NewPrefix(4)

	// ProviderSequenceKey saves the next Provider id.
	ProviderSequenceKey = collections.NewPrefix(5)
)

const (
	ModuleName = "sku"

	StoreKey = ModuleName

	QuerierRoute = ModuleName
)

// Event types for the sku module.
const (
	EventTypeProviderCreated     = "provider_created"
	EventTypeProviderUpdated     = "provider_updated"
	EventTypeProviderActivated   = "provider_activated"
	EventTypeProviderDeactivated = "provider_deactivated"
	EventTypeSKUCreated          = "sku_created"
	EventTypeSKUUpdated          = "sku_updated"
	EventTypeSKUActivated        = "sku_activated"
	EventTypeSKUDeactivated      = "sku_deactivated"
	EventTypeParamsUpdated       = "params_updated"

	AttributeKeyProviderUUID  = "provider_uuid"
	AttributeKeySKUUUID       = "sku_uuid"
	AttributeKeyName          = "name"
	AttributeKeyAddress       = "address"
	AttributeKeyPayoutAddress = "payout_address"
	AttributeKeyActive        = "active"
	AttributeKeyBasePrice     = "base_price"
	AttributeKeyUnit          = "unit"
	AttributeKeyCreatedBy     = "created_by"
	AttributeKeyDeactivatedBy = "deactivated_by"
)
