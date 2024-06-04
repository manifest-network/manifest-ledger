package keeper_test

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	"github.com/liftedinit/manifest-ledger/x/manifest/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"

	"github.com/liftedinit/manifest-ledger/app"
	"github.com/liftedinit/manifest-ledger/app/apptesting"
	appparams "github.com/liftedinit/manifest-ledger/app/params"
)

// Sets up the keeper test suite.

type testFixture struct {
	suite.Suite

	App         *app.ManifestApp
	EncodingCfg moduletestutil.TestEncodingConfig
	Ctx         sdk.Context
	QueryHelper *baseapp.QueryServiceTestHelper
	TestAccs    []sdk.AccAddress
}

func initFixture(t *testing.T) *testFixture {
	s := testFixture{}

	appparams.SetAddressPrefixes()

	encCfg := moduletestutil.MakeTestEncodingConfig()

	s.Ctx, s.App = app.Setup(t)
	s.QueryHelper = &baseapp.QueryServiceTestHelper{
		GRPCQueryRouter: s.App.GRPCQueryRouter(),
		Ctx:             s.Ctx,
	}
	s.TestAccs = apptesting.CreateRandomAccounts(3)

	s.EncodingCfg = encCfg

	return &s
}

func TestPayout(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, acc := testdata.KeyTestPubAddr()
	f := initFixture(t)

	k := f.App.ManifestKeeper
	k.SetAuthority(authority.String())

	type testcase struct {
		name    string
		payouts []types.PayoutPair
		errMsg  string
	}

	cases := []testcase{
		{
			name: "fail: invalid destination address",
			payouts: []types.PayoutPair{
				types.NewPayoutPair(acc, "umfx", 1),
				{Address: "badaddr", Coin: sdk.NewCoin("umfx", sdkmath.NewInt(2))},
			},
			errMsg: "decoding bech32 failed",
		},
		{
			name: "fail: invalid coin denom",
			payouts: []types.PayoutPair{
				types.NewPayoutPair(acc, "umfx", 1),
				{Address: acc.String(), Coin: sdk.Coin{Denom: ":::", Amount: sdkmath.NewInt(2)}},
			},
			errMsg: "invalid payout",
		},
		{
			name: "fail: invalid coin amount",
			payouts: []types.PayoutPair{
				types.NewPayoutPair(acc, "umfx", 1),
				{Address: acc.String(), Coin: sdk.Coin{Denom: "umfx", Amount: sdkmath.NewInt(-2)}},
			},
			errMsg: "invalid payout",
		},
	}

	for _, c := range cases {
		c := c

		t.Run(c.name, func(t *testing.T) {
			err := k.Payout(f.Ctx, c.payouts)

			if c.errMsg != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, c.errMsg)
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

func TestExportGenesis(t *testing.T) {
	f := initFixture(t)

	k := f.App.ManifestKeeper

	_, err := k.Params.Get(f.Ctx)
	require.NoError(t, err)

	genState := k.ExportGenesis(f.Ctx)

	require.NotNil(t, genState.Params)
}
