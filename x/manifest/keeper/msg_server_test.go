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
