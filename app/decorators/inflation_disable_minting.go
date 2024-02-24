package decorators

import (
	"context"

	manifestkeeper "github.com/liftedinit/manifest-ledger/x/manifest/keeper"
	manifesttypes "github.com/liftedinit/manifest-ledger/x/manifest/types"
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
	if err := mfd.hasInvalidMsgFromPoAAdmin(ctx, tx.GetMsgs()); err != nil {
		return ctx, err
	}

	return next(ctx, tx, simulate)
}

func (mfd MsgManualMintFilterDecorator) hasInvalidMsgFromPoAAdmin(ctx sdk.Context, msgs []sdk.Msg) error {
	for _, msg := range msgs {
		// only payout stakeholders manually if inflation is 0% & the sender is the admin.
		if m, ok := msg.(*manifesttypes.MsgPayoutStakeholders); ok {
			return mfd.senderAdminOnMintWithInflation(ctx, m.Authority)
		}

		// if the sender is not the admin, continue as normal
		// if they are the admin, check if inflation is 0%. if it is, allow. Else, error.
		if m, ok := msg.(*tokenfactorytypes.MsgMint); ok {
			return mfd.senderAdminOnMintWithInflation(ctx, m.Sender)
		}
	}

	return nil
}

func (mfd MsgManualMintFilterDecorator) senderAdminOnMintWithInflation(ctx context.Context, sender string) error {
	if mfd.isSudoAdminFunc(ctx, sender) {
		if !mfd.mk.IsManualMintingEnabled(ctx) {
			return manifesttypes.ErrManualMintingDisabled
		}
	}

	return nil
}
