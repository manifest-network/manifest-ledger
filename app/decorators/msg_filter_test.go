package decorators_test

// Adapted from https://github.com/rollchains/spawn/blob/release/v0.50/simapp/app/decorators/msg_filter_test.go @ e332edf

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/cometbft/cometbft/crypto/secp256k1"

	"github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	ibctypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/liftedinit/manifest-ledger/app/decorators"
)

type AnteTestSuite struct {
	suite.Suite

	ctx sdk.Context
}

func TestAnteTestSuite(t *testing.T) {
	suite.Run(t, new(AnteTestSuite))
}

func (s *AnteTestSuite) TestAnteMsgFilterLogic() {
	acc := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())

	// test blocking any MsgTransfer Messages
	ante := decorators.FilterDecorator(&types.MsgTransfer{})
	msg := types.NewMsgTransfer(
		"transfer/channel-0",
		"transfer/channel-0",
		sdk.NewCoin("umfl", sdkmath.NewInt(1)),
		acc.String(),
		acc.String(),
		ibctypes.Height{
			RevisionNumber: 0,
			RevisionHeight: 0,
		},
		0,
		"memo",
	)
	_, err := ante.AnteHandle(s.ctx, decorators.NewMockTx(msg), false, decorators.EmptyAnte)
	s.Require().Error(err)
	s.Require().ErrorContains(err, "tx contains unsupported message types")

	// validate other messages go through still
	msgMultiSend := banktypes.NewMsgMultiSend(
		banktypes.NewInput(acc, sdk.NewCoins(sdk.NewCoin("umfx", sdkmath.NewInt(1)))),
		[]banktypes.Output{banktypes.NewOutput(acc, sdk.NewCoins(sdk.NewCoin("umfx", sdkmath.NewInt(1))))},
	)
	_, err = ante.AnteHandle(s.ctx, decorators.NewMockTx(msgMultiSend), false, decorators.EmptyAnte)
	s.Require().NoError(err)
}
