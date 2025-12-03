package types

import "cosmossdk.io/collections"

// Storage prefixes for collections.
var (
	// ParamsKey saves the module parameters.
	ParamsKey = collections.NewPrefix(0)

	// SKUKey saves the SKUs.
	SKUKey = collections.NewPrefix(1)

	// SequenceKey saves the next SKU id.
	SequenceKey = collections.NewPrefix(2)

	// SKUByProviderIndexKey saves the provider index for SKUs.
	SKUByProviderIndexKey = collections.NewPrefix(3)
)

const (
	ModuleName = "sku"

	StoreKey = ModuleName

	QuerierRoute = ModuleName
)

// Event types for the sku module.
const (
	EventTypeSKUCreated    = "sku_created"
	EventTypeSKUUpdated    = "sku_updated"
	EventTypeSKUDeleted    = "sku_deleted"
	EventTypeParamsUpdated = "params_updated"

	AttributeKeySKUID    = "sku_id"
	AttributeKeyProvider = "provider"
	AttributeKeyName     = "name"
)
