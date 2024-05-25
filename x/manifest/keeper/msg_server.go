package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
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

// BurnHeldBalance implements types.MsgServer.
func (ms msgServer) BurnHeldBalance(ctx context.Context, msg *types.MsgBurnHeldBalance) (*types.MsgBurnHeldBalanceResponse, error) {
	addr, err := sdk.AccAddressFromBech32(msg.Sender)
	if err != nil {
		return nil, err
	}

	if err := ms.k.bankKeeper.SendCoinsFromAccountToModule(ctx, addr, types.ModuleName, msg.BurnCoins); err != nil {
		return nil, fmt.Errorf("not enough balance to burn %s: %w", msg.BurnCoins, err)
	}

	return &types.MsgBurnHeldBalanceResponse{}, ms.k.bankKeeper.BurnCoins(ctx, types.ModuleName, msg.BurnCoins)
}
