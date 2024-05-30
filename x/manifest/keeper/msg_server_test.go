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

	type testcases struct {
		name        string
		distrTokens int64
		sh          []*types.StakeHolders
		// expected overrides the default sh values for when the SDK deterministically rounds
		expected []*types.StakeHolders
	}

	cases := []testcases{
		{
			name:        "success; tokens split between 2 stakeholders",
			distrTokens: 1_000_000,
			sh: []*types.StakeHolders{
				{
					Address:    acc.String(),
					Percentage: 49_999_999, // 50%
				},
				{
					Address:    acc2.String(),
					Percentage: 50_000_001,
				},
			},
			// 1m tokens split between 2
			expected: []*types.StakeHolders{
				{
					Address:    acc.String(),
					Percentage: 500_000,
				},
				{
					Address:    acc2.String(),
					Percentage: 500_000,
				},
			},
		},
		{
			name:        "success; small amount of tokens split between 2 stakeholders",
			distrTokens: 1,
			sh: []*types.StakeHolders{
				{
					Address:    acc.String(),
					Percentage: 49_999_999, // 50%
				},
				{
					Address:    acc2.String(),
					Percentage: 50_000_001,
				},
			},
			// 1 tokens split between 2, the one with slightly more gets the actual token
			expected: []*types.StakeHolders{
				{
					Address:    acc.String(),
					Percentage: 0,
				},
				{
					Address:    acc2.String(),
					Percentage: 1,
				},
			},
		},
		{
			name:        "success; tokens split between 4 stakeholders",
			distrTokens: 100_000_000,
			sh: []*types.StakeHolders{
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
			},
		},
	}

	for _, c := range cases {
		c := c

		t.Run(c.name, func(t *testing.T) {
			sh := c.sh
			distrTokens := c.distrTokens
			expected := c.expected

			_, err := ms.UpdateParams(f.Ctx, &types.MsgUpdateParams{
				Authority: authority.String(),
				Params:    types.NewParams(sh, false, 0, "umfx"),
			})
			require.NoError(t, err)

			// validate the full payout of 100 tokens got split up between all fractional shares as expected
			res, err := k.CalculateShareHolderTokenPayout(f.Ctx, sdk.NewCoin("stake", sdkmath.NewInt(distrTokens)))
			require.NoError(t, err)
			for _, s := range sh {
				for w, shp := range res {

					// if expected is set, then check that value
					if expected != nil {
						for _, e := range expected {
							e := e
							if e.Address == shp.Address {
								require.EqualValues(t, e.Percentage, shp.Coin.Amount.Int64(), "stakeholder %d", w)
							}
						}
					} else {
						if s.Address == shp.Address {
							require.EqualValues(t, s.Percentage, shp.Coin.Amount.Int64(), "stakeholder %d", w)
						}
					}

				}
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
