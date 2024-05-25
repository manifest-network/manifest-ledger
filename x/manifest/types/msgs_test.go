package types

import (
	"testing"

	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestMsgBurn(t *testing.T) {
	_, _, acc := testdata.KeyTestPubAddr()

	type tc struct {
		name    string
		msg     *MsgBurnHeldBalance
		success bool
	}

	for _, c := range []tc{
		{
			name: "fail; no coins",
			msg:  NewMsgBurnHeldBalance(acc, sdk.NewCoins()),
		},
		{
			name: "fail; invalid coin: 0stake",
			msg:  NewMsgBurnHeldBalance(acc, sdk.NewCoins(sdk.NewCoin("stake", sdkmath.NewInt(0)))),
		},
		{
			name: "fail; invalid address",
			msg:  NewMsgBurnHeldBalance(sdk.AccAddress{}, sdk.NewCoins(sdk.NewCoin("stake", sdkmath.NewInt(5)))),
		},
		{
			name:    "success; valid burn",
			msg:     NewMsgBurnHeldBalance(acc, sdk.NewCoins(sdk.NewCoin("stake", sdkmath.NewInt(5)))),
			success: true,
		},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if c.success {
				require.NoError(t, c.msg.Validate())
			} else {
				require.Error(t, c.msg.Validate())
			}
		})
	}
}
