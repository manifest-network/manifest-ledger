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

// PayoutStakeholders implements types.MsgServer.
func (ms msgServer) PayoutStakeholders(ctx context.Context, req *types.MsgPayoutStakeholders) (*types.MsgPayoutStakeholdersResponse, error) {
	if ms.k.authority != req.Authority {
		return nil, fmt.Errorf("invalid authority; expected %s, got %s", ms.k.authority, req.Authority)
	}

	params, err := ms.k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	if params.Inflation.AutomaticEnabled {
		return nil, types.ErrManualMintingDisabled
	}

	return nil, ms.k.PayoutStakeholders(ctx, req.Payout)
}
