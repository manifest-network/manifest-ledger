package keeper_test

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	"github.com/liftedinit/manifest-ledger/x/manifest/keeper"
	"github.com/liftedinit/manifest-ledger/x/manifest/types"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestCalculatePayoutLogic(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, acc := testdata.KeyTestPubAddr()
	_, _, acc2 := testdata.KeyTestPubAddr()
	_, _, acc3 := testdata.KeyTestPubAddr()
	_, _, acc4 := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.ManifestKeeper

	k.SetAuthority(f.Ctx, authority.String())
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
		Params:    types.NewParams(sh),
	})
	require.NoError(t, err)

	// validate the full payout of 100 tokens got split up between all fractional shares as expected
	res := k.CalculateShareHolderTokenPayout(f.Ctx, sdk.NewCoin("stake", sdkmath.NewInt(100_000_000)))
	for _, s := range sh {
		for w, coin := range res {
			if s.Address == w {
				require.EqualValues(t, s.Percentage, coin.Amount.Int64())
			}
		}
	}

}

func TestUpdateParams(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, acc := testdata.KeyTestPubAddr()
	_, _, acc2 := testdata.KeyTestPubAddr()

	f := initFixture(t)

	f.App.ManifestKeeper.SetAuthority(f.Ctx, authority.String())

	ms := keeper.NewMsgServerImpl(f.App.ManifestKeeper)

	for _, tc := range []struct {
		desc    string
		sender  string
		sh      []*types.StakeHolders
		success bool
	}{
		{
			desc:   "invalid authority",
			sender: acc.String(),
			sh: []*types.StakeHolders{
				{
					Address:    acc.String(),
					Percentage: 100_000_000,
				},
			},
			success: false,
		},
		{
			desc:   "invalid percent",
			sender: authority.String(),
			sh: []*types.StakeHolders{
				{
					Address:    acc.String(),
					Percentage: 7,
				},
			},
			success: false,
		},
		{
			desc:   "duplicate address",
			sender: authority.String(),
			sh: []*types.StakeHolders{
				{
					Address:    acc.String(),
					Percentage: 50_000_000,
				},
				{
					Address:    acc.String(),
					Percentage: 50_000_000,
				},
			},
			success: false,
		},
		{
			desc:    "success none",
			sender:  authority.String(),
			sh:      []*types.StakeHolders{},
			success: true,
		},
		{
			desc:   "success many stake holders",
			sender: authority.String(),
			sh: []*types.StakeHolders{
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
			},
			success: true,
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			// Set the params
			_, err := ms.UpdateParams(f.Ctx, &types.MsgUpdateParams{
				Authority: tc.sender,
				Params:    types.NewParams(tc.sh),
			})
			require.Equal(t, tc.success, err == nil, err)

			// Ensure they are set the same as the expected
			if tc.success && len(tc.sh) > 0 {
				params, err := f.App.ManifestKeeper.Params.Get(f.Ctx)
				require.NoError(t, err)

				require.Equal(t, tc.sh, params.StakeHolders)
			}
		})
	}
}
