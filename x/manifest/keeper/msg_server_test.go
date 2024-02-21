package keeper_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	"github.com/liftedinit/manifest-ledger/x/manifest/keeper"
	"github.com/liftedinit/manifest-ledger/x/manifest/types"
	"github.com/stretchr/testify/require"
)

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
			_, err := ms.UpdateParams(f.Ctx, &types.MsgUpdateParams{
				Authority: tc.sender,
				Params:    types.NewParams(tc.sh),
			})
			require.Equal(t, tc.success, err == nil, err)
		})
	}

}
