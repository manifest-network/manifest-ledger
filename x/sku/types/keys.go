package types

import "cosmossdk.io/collections"

// ParamsKey saves the current module params.
var ParamsKey = collections.NewPrefix(0)

// SKUKey saves the SKUs.
var SKUKey = collections.NewPrefix(1)

// SequenceKey saves the next SKU id.
var SequenceKey = collections.NewPrefix(2)

// SKUByProviderPrefix is the prefix for indexing SKUs by provider.
var SKUByProviderPrefix = collections.NewPrefix(3)

const (
	ModuleName = "sku"

	StoreKey = ModuleName

	QuerierRoute = ModuleName
)
