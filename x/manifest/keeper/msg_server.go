package keeper

import (
	"context"
	"fmt"

	"github.com/liftedinit/manifest-ledger/x/manifest/types"
)

type msgServer struct {
	k Keeper
}

var _ types.MsgServer = msgServer{}

// NewMsgServerImpl returns an implementation of the module MsgServer interface.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{k: keeper}
}

func (ms msgServer) UpdateParams(ctx context.Context, req *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if ms.k.authority != req.Authority {
		return nil, fmt.Errorf("invalid authority; expected %s, got %s", ms.k.authority, req.Authority)
	}

	if err := req.Params.Validate(); err != nil {
		return nil, err
	}

	return nil, ms.k.Params.Set(ctx, req.Params)
}
