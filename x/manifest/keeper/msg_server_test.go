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

func TestMsgServerPayoutStakeholdersLogic(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, acc := testdata.KeyTestPubAddr()
	_, _, acc2 := testdata.KeyTestPubAddr()
	_, _, acc3 := testdata.KeyTestPubAddr()
	_, _, acc4 := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.ManifestKeeper

	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	sh := []*types.StakeHolders{
		{
			Address:    acc.String(),
			Percentage: 50_000_000, // 50%
		},
		{
			Address:    acc2.String(),
			Percentage: 49_000_000,
		},
		{
			Address:    acc3.String(),
			Percentage: 500_001, // 0.5%
		},
		{
			Address:    acc4.String(),
			Percentage: 499_999,
		},
	}
	_, err := ms.UpdateParams(f.Ctx, &types.MsgUpdateParams{
		Authority: authority.String(),
		Params:    types.NewParams(sh, false, 0, "umfx"),
	})
	require.NoError(t, err)

	// wrong acc
	_, err = ms.PayoutStakeholders(f.Ctx, &types.MsgPayoutStakeholders{
		Authority: acc.String(),
		Payout:    sdk.NewCoin("stake", sdkmath.NewInt(100_000_000)),
	})
	require.Error(t, err)

	// success
	_, err = ms.PayoutStakeholders(f.Ctx, &types.MsgPayoutStakeholders{
		Authority: authority.String(),
		Payout:    sdk.NewCoin("stake", sdkmath.NewInt(100_000_000)),
	})
	require.NoError(t, err)

	for _, s := range sh {
		addr := sdk.MustAccAddressFromBech32(s.Address)

		accBal := f.App.BankKeeper.GetBalance(f.Ctx, addr, "stake")
		require.EqualValues(t, s.Percentage, accBal.Amount.Int64())
	}
}

func TestUpdateParams(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, acc := testdata.KeyTestPubAddr()
	_, _, acc2 := testdata.KeyTestPubAddr()

	f := initFixture(t)

	f.App.ManifestKeeper.SetAuthority(authority.String())

	ms := keeper.NewMsgServerImpl(f.App.ManifestKeeper)

	for _, tc := range []struct {
		desc    string
		sender  string
		p       types.Params
		success bool
	}{
		{
			desc:   "invalid authority",
			sender: acc.String(),
			p: types.NewParams([]*types.StakeHolders{
				{
					Address:    acc.String(),
					Percentage: 100_000_000,
				},
			}, false, 0, "umfx"),
			success: false,
		},
		{
			desc:   "invalid percent",
			sender: authority.String(),
			p: types.NewParams([]*types.StakeHolders{
				{
					Address:    acc.String(),
					Percentage: 7,
				},
			}, false, 0, "umfx"),
			success: false,
		},
		{
			desc:   "invalid stakeholder address",
			sender: authority.String(),
			p: types.NewParams([]*types.StakeHolders{
				{
					Address:    "invalid",
					Percentage: 100_000_000,
				},
			}, false, 0, "umfx"),
			success: false,
		},
		{
			desc:   "duplicate address",
			sender: authority.String(),
			p: types.NewParams([]*types.StakeHolders{
				{
					Address:    acc.String(),
					Percentage: 50_000_000,
				},
				{
					Address:    acc.String(),
					Percentage: 50_000_000,
				},
			}, false, 0, "umfx"),
			success: false,
		},
		{
			desc:    "success none",
			sender:  authority.String(),
			p:       types.NewParams([]*types.StakeHolders{}, false, 0, "umfx"),
			success: true,
		},
		{
			desc:   "success many stake holders",
			sender: authority.String(),
			p: types.NewParams([]*types.StakeHolders{
				{
					Address:    acc.String(),
					Percentage: 1_000_000,
				},
				{
					Address:    acc2.String(),
					Percentage: 1_000_000,
				},
				{
					Address:    authority.String(),
					Percentage: 98_000_000,
				},
			}, false, 0, "umfx"),
			success: true,
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			// Set the params
			_, err := ms.UpdateParams(f.Ctx, &types.MsgUpdateParams{
				Authority: tc.sender,
				Params:    tc.p,
			})
			require.Equal(t, tc.success, err == nil, err)

			// Ensure they are set the same as the expected
			if tc.success && len(tc.p.StakeHolders) > 0 {
				params, err := f.App.ManifestKeeper.Params.Get(f.Ctx)
				require.NoError(t, err)

				require.Equal(t, tc.p.StakeHolders, params.StakeHolders)
			}
		})
	}
}

func TestCalculatePayoutLogic(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, acc := testdata.KeyTestPubAddr()
	_, _, acc2 := testdata.KeyTestPubAddr()
	_, _, acc3 := testdata.KeyTestPubAddr()
	_, _, acc4 := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.ManifestKeeper

	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	sh := []*types.StakeHolders{
		{
			Address:    acc.String(),
			Percentage: 50_000_000, // 50%
		},
		{
			Address:    acc2.String(),
			Percentage: 49_000_000,
		},
		{
			Address:    acc3.String(),
			Percentage: 500_001, // 0.5%
		},
		{
			Address:    acc4.String(),
			Percentage: 499_999,
		},
	}

	_, err := ms.UpdateParams(f.Ctx, &types.MsgUpdateParams{
		Authority: authority.String(),
		Params:    types.NewParams(sh, false, 0, "umfx"),
	})
	require.NoError(t, err)

	// validate the full payout of 100 tokens got split up between all fractional shares as expected
	res, err := k.CalculateShareHolderTokenPayout(f.Ctx, sdk.NewCoin("stake", sdkmath.NewInt(100_000_000)))
	require.NoError(t, err)
	for _, s := range sh {
		for w, shp := range res {
			if s.Address == shp.Address {
				require.EqualValues(t, s.Percentage, shp.Coin.Amount.Int64(), "stakeholder %d", w)
			}
		}
	}
}

func TestBurnCoins(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.ManifestKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)
	_, _, acc := testdata.KeyTestPubAddr()

	type tc struct {
		name     string
		initial  sdk.Coins
		burn     sdk.Coins
		expected sdk.Coins
		address  sdk.AccAddress
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
			address:  authority,
		},
		{
			name:     "fail; bad address",
			initial:  sdk.NewCoins(),
			burn:     sdk.NewCoins(sdk.NewCoin("stake", sdkmath.NewInt(7))),
			expected: sdk.NewCoins(),
			address:  sdk.AccAddress{0x0},
		},
		{
			name:     "success; burn tokens successfully",
			initial:  sdk.NewCoins(stake, mfx),
			burn:     sdk.NewCoins(sdk.NewCoin("stake", sdkmath.NewInt(7))),
			expected: sdk.NewCoins(mfx, stake.SubAmount(sdkmath.NewInt(7))),
			address:  authority,
			success:  true,
		},
		{
			name:     "success; burn many tokens successfully",
			initial:  sdk.NewCoins(stake, mfx),
			burn:     sdk.NewCoins(sdk.NewCoin("umfx", sdkmath.NewInt(9)), sdk.NewCoin("stake", sdkmath.NewInt(7))),
			expected: sdk.NewCoins(mfx.SubAmount(sdkmath.NewInt(9)), stake.SubAmount(sdkmath.NewInt(7))),
			address:  authority,
			success:  true,
		},
		{
			name:     "fail; invalid authority",
			initial:  sdk.NewCoins(stake, mfx),
			burn:     sdk.NewCoins(sdk.NewCoin("stake", sdkmath.NewInt(7))),
			expected: sdk.NewCoins(stake, mfx),
			address:  acc,
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			// setup initial balances for the new account
			if len(c.initial) > 0 {
				require.NoError(t, f.App.BankKeeper.MintCoins(f.Ctx, "mint", c.initial))
				require.NoError(t, f.App.BankKeeper.SendCoinsFromModuleToAccount(f.Ctx, "mint", c.address, c.initial))
			}

			// validate initial balance
			require.Equal(t, c.initial, f.App.BankKeeper.GetAllBalances(f.Ctx, c.address))

			// burn coins
			_, err := ms.BurnHeldBalance(f.Ctx, &types.MsgBurnHeldBalance{
				Authority: c.address.String(),
				BurnCoins: c.burn,
			})
			if c.success {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}

			allBalance := f.App.BankKeeper.GetAllBalances(f.Ctx, c.address)
			require.Equal(t, c.expected, allBalance)

			// burn the rest of the coins to reset the balance to 0 for the next test if the test was successful
			if c.success {
				_, err = ms.BurnHeldBalance(f.Ctx, &types.MsgBurnHeldBalance{
					Authority: c.address.String(),
					BurnCoins: allBalance,
				})
				require.NoError(t, err)
			}
		})
	}
}
