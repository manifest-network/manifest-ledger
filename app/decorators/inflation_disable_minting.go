package decorators

import (
	"context"

	manifestkeeper "github.com/liftedinit/manifest-ledger/x/manifest/keeper"
	tokenfactorytypes "github.com/reecepbcups/tokenfactory/x/tokenfactory/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

type IsSudoAdminFunc func(ctx context.Context, fromAddr string) bool

type MsgManualMintFilterDecorator struct {
	mk              *manifestkeeper.Keeper
	isSudoAdminFunc IsSudoAdminFunc
}

func NewMsgManualMintFilterDecorator(mk *manifestkeeper.Keeper, isSudoAdminFunc IsSudoAdminFunc) MsgManualMintFilterDecorator {
	return MsgManualMintFilterDecorator{
		mk:              mk,
		isSudoAdminFunc: isSudoAdminFunc,
	}
}

func (mfd MsgManualMintFilterDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	// iterate all messages, see if any are a tokenfactory message from a sudo admin.
	// if there is and inflation is current >0%, return an error
	if err := mfd.hasInvalidTokenFactoryMsg(ctx, tx.GetMsgs()); err != nil {
		return ctx, err
	}

	return next(ctx, tx, simulate)
}

func (mfd MsgManualMintFilterDecorator) hasInvalidTokenFactoryMsg(ctx sdk.Context, msgs []sdk.Msg) error {
	for _, msg := range msgs {
		if m, ok := msg.(*tokenfactorytypes.MsgMint); ok {
			if mfd.isSudoAdminFunc(ctx, m.Sender) {
				isInflationEnabled := mfd.mk.IsManualMintingEnabled(ctx)
				if isInflationEnabled != nil {
					return isInflationEnabled
				}
			}

			return nil
		}
	}

	return nil
}
