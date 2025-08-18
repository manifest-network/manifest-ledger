package keeper

import (
	"github.com/manifest-network/manifest-ledger/x/manifest/types"
)

var _ types.QueryServer = Querier{}

type Querier struct {
	Keeper
}

func NewQuerier(keeper Keeper) Querier {
	return Querier{Keeper: keeper}
}
