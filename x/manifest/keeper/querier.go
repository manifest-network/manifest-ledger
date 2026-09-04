package keeper

import (
	"github.com/manifest-network/manifest-ledger/x/manifest/types"
)

var _ types.QueryServer = Querier{}

// Querier implements the manifest module's gRPC query service.
type Querier struct {
	Keeper
}

// NewQuerier constructs a manifest query service.
func NewQuerier(keeper Keeper) Querier {
	return Querier{Keeper: keeper}
}
