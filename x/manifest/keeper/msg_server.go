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

func (ms msgServer) Payout(ctx context.Context, req *types.MsgPayout) (*types.MsgPayoutResponse, error) {
	if ms.k.authority != req.Authority {
		return nil, fmt.Errorf("invalid authority; expected %s, got %s", ms.k.authority, req.Authority)
	}

	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid payout message: %w", err)
	}

	return nil, ms.k.Payout(ctx, req.PayoutPairs)
}

func (ms msgServer) BurnHeldBalance(ctx context.Context, req *types.MsgBurnHeldBalance) (*types.MsgBurnHeldBalanceResponse, error) {
	if ms.k.authority != req.Authority {
		return nil, fmt.Errorf("invalid authority; expected %s, got %s", ms.k.authority, req.Authority)
	}
	addr, err := sdk.AccAddressFromBech32(req.Authority)
	if err != nil {
		return nil, err
	}

	if err := ms.k.bankKeeper.SendCoinsFromAccountToModule(ctx, addr, types.ModuleName, req.BurnCoins); err != nil {
		return nil, fmt.Errorf("not enough balance to burn %s: %w", req.BurnCoins, err)
	}

	return &types.MsgBurnHeldBalanceResponse{}, ms.k.bankKeeper.BurnCoins(ctx, types.ModuleName, req.BurnCoins)
}
