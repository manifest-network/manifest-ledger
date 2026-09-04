package types

import "cosmossdk.io/collections"

// ParamsKey saves the current module params.
var ParamsKey = collections.NewPrefix(0)

// Module identity constants define the manifest store and legacy query route.
const (
	ModuleName = "manifest"

	StoreKey = ModuleName

	QuerierRoute = ModuleName
)
