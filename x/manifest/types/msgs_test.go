package types

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
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

func TestMsgPayout(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, acc := testdata.KeyTestPubAddr()

	type tc struct {
		name    string
		msg     *MsgPayout
		success bool
	}

	for _, c := range []tc{
		{
			name: "fail; no payouts",
			msg:  NewMsgPayout(authority, []PayoutPair{}),
		},
		{
			name: "fail; bad payout address",
			msg: NewMsgPayout(authority, []PayoutPair{
				{
					Address: "bad",
					Coin:    sdk.NewCoin("stake", sdkmath.NewInt(5)),
				},
			}),
		},
		{
			name: "fail; duplicate address",
			msg: NewMsgPayout(authority, []PayoutPair{
				NewPayoutPair(acc, "stake", 1),
				NewPayoutPair(acc, "stake", 2),
			}),
		},
		{
			name: "fail; 0 payout coins",
			msg: NewMsgPayout(authority, []PayoutPair{
				NewPayoutPair(acc, "stake", 0),
			}),
		},
		{
			name: "success; payout",
			msg: NewMsgPayout(authority, []PayoutPair{
				NewPayoutPair(acc, "stake", 5),
			}),
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
