package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/liftedinit/manifest-ledger/x/manifest/keeper"
	"github.com/liftedinit/manifest-ledger/x/manifest/types"
)

func TestPerformPayout(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, acc := testdata.KeyTestPubAddr()
	_, _, acc2 := testdata.KeyTestPubAddr()
	_, _, acc3 := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.ManifestKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	type testcase struct {
		name       string
		sender     string
		payouts    []types.PayoutPair
		shouldFail bool
	}

	cases := []testcase{
		{
			name:   "success; payout token to 3 stakeholders",
			sender: authority.String(),
			payouts: []types.PayoutPair{
				types.NewPayoutPair(acc, "umfx", 1),
				types.NewPayoutPair(acc2, "umfx", 2),
				types.NewPayoutPair(acc3, "umfx", 3),
			},
		},
		{
			name:   "fail; bad authority",
			sender: acc.String(),
			payouts: []types.PayoutPair{
				types.NewPayoutPair(acc, "umfx", 1),
			},
			shouldFail: true,
		},
		{
			name:   "fail; bad bech32 authority",
			sender: "bad",
			payouts: []types.PayoutPair{
				types.NewPayoutPair(acc, "umfx", 1),
			},
			shouldFail: true,
		},
		{
			name:   "fail; payout to bad address",
			sender: authority.String(),
			payouts: []types.PayoutPair{
				types.NewPayoutPair(acc, "umfx", 1),
				{Address: "badaddr", Coin: sdk.NewCoin("umfx", sdkmath.NewInt(2))},
				types.NewPayoutPair(acc3, "umfx", 3),
			},
			shouldFail: true,
		},
		{
			name:   "fail; payout with a 0 token",
			sender: authority.String(),
			payouts: []types.PayoutPair{
				types.NewPayoutPair(acc, "umfx", 1),
				types.NewPayoutPair(acc2, "umfx", 0),
			},
			shouldFail: true,
		},
	}

	for _, c := range cases {
		c := c

		t.Run(c.name, func(t *testing.T) {
			payoutMsg := &types.MsgPayout{
				Authority:   c.sender,
				PayoutPairs: c.payouts,
			}

			_, err := ms.Payout(f.Ctx, payoutMsg)
			if c.shouldFail {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			for _, p := range c.payouts {
				p := p
				addr := p.Address
				coin := p.Coin

				accAddr, err := sdk.AccAddressFromBech32(addr)
				require.NoError(t, err)

				balance := f.App.BankKeeper.GetBalance(f.Ctx, accAddr, coin.Denom)
				require.EqualValues(t, coin.Amount, balance.Amount, "expected %s, got %s", coin.Amount, balance.Amount)
			}
		})

	}
}

func TestBurnCoins(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.ManifestKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	type tc struct {
		name     string
		initial  sdk.Coins
		burn     sdk.Coins
		expected sdk.Coins
		address  string
		success  bool
	}

	stake := sdk.NewCoin("stake", sdkmath.NewInt(100_000_000))
	mfx := sdk.NewCoin("umfx", sdkmath.NewInt(100_000_000))

	cases := []tc{
		{
			name:     "fail; not enough balance to burn",
			initial:  sdk.NewCoins(),
			burn:     sdk.NewCoins(sdk.NewCoin("stake", sdkmath.NewInt(7))),
			expected: sdk.NewCoins(),
		},
		{
			name:     "fail; bad address",
			initial:  sdk.NewCoins(),
			burn:     sdk.NewCoins(sdk.NewCoin("stake", sdkmath.NewInt(7))),
			expected: sdk.NewCoins(),
			address:  "xyz",
		},
		{
			name:     "success; burn 1 token successfully",
			initial:  sdk.NewCoins(stake, mfx),
			burn:     sdk.NewCoins(sdk.NewCoin("stake", sdkmath.NewInt(7))),
			expected: sdk.NewCoins(mfx, stake.SubAmount(sdkmath.NewInt(7))),
			success:  true,
		},
		{
			name:     "success; burn many tokens successfully",
			initial:  sdk.NewCoins(stake, mfx),
			burn:     sdk.NewCoins(sdk.NewCoin("umfx", sdkmath.NewInt(9)), sdk.NewCoin("stake", sdkmath.NewInt(7))),
			expected: sdk.NewCoins(mfx.SubAmount(sdkmath.NewInt(9)), stake.SubAmount(sdkmath.NewInt(7))),
			success:  true,
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			_, _, acc := testdata.KeyTestPubAddr()
			if c.address == "" {
				c.address = acc.String()
			}

			// setup initial balances for the new account
			if len(c.initial) > 0 {
				require.NoError(t, f.App.BankKeeper.MintCoins(f.Ctx, "mint", c.initial))
				require.NoError(t, f.App.BankKeeper.SendCoinsFromModuleToAccount(f.Ctx, "mint", acc, c.initial))
			}

			// validate initial balance
			require.Equal(t, c.initial, f.App.BankKeeper.GetAllBalances(f.Ctx, acc))

			// burn coins
			_, err := ms.BurnHeldBalance(f.Ctx, &types.MsgBurnHeldBalance{
				Sender:    c.address,
				BurnCoins: c.burn,
			})
			if c.success {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}

			require.Equal(t, c.expected, f.App.BankKeeper.GetAllBalances(f.Ctx, acc))
		})
	}
}
