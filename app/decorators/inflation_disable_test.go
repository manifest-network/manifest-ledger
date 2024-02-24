package decorators_test

import (
	"context"
	"testing"

	app "github.com/liftedinit/manifest-ledger/app"
	"github.com/liftedinit/manifest-ledger/app/decorators"
	appparams "github.com/liftedinit/manifest-ledger/app/params"
	manifestkeeper "github.com/liftedinit/manifest-ledger/x/manifest/keeper"
	manifesttypes "github.com/liftedinit/manifest-ledger/x/manifest/types"
	tokenfactorytypes "github.com/reecepbcups/tokenfactory/x/tokenfactory/types"
	poa "github.com/strangelove-ventures/poa"
	poakeeper "github.com/strangelove-ventures/poa/keeper"
	"github.com/stretchr/testify/suite"

	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	mintkeeper "github.com/cosmos/cosmos-sdk/x/mint/keeper"
)

// Define an empty ante handle
var (
	EmptyAnte = func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		return ctx, nil
	}

	coin   = sdk.NewCoin(appparams.BondDenom, sdkmath.NewInt(1))
	tfCoin = sdk.NewCoin("factory", sdkmath.NewInt(1))
)

type AnteTestSuite struct {
	suite.Suite

	ctx sdk.Context
	app *app.ManifestApp

	manifestKeeper manifestkeeper.Keeper
	mintKeeper     mintkeeper.Keeper

	poakeeper       poakeeper.Keeper
	isSudoAdminFunc func(ctx context.Context, fromAddr string) bool
}

func (s *AnteTestSuite) SetupTest() {
	s.ctx, s.app = app.Setup(s.T())

	s.manifestKeeper = s.app.ManifestKeeper
	s.mintKeeper = s.app.MintKeeper

	s.poakeeper = s.app.POAKeeper
	s.isSudoAdminFunc = s.app.POAKeeper.IsAdmin
}

func TestAnteTestSuite(t *testing.T) {
	suite.Run(t, new(AnteTestSuite))
}

func (s *AnteTestSuite) TestAnteInflationAndMinting() {
	poaAdmin := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	stdUser := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())

	s.Require().NoError(s.poakeeper.Params.Set(s.ctx, poa.Params{
		Admins: []string{poaAdmin.String()},
	}))

	// validate s.poakeeper.IsAdmin works for poaAdmin
	s.Require().True(s.poakeeper.IsAdmin(s.ctx, poaAdmin.String()))
	s.Require().False(s.poakeeper.IsAdmin(s.ctx, stdUser.String()))

	ante := decorators.NewMsgManualMintFilterDecorator(&s.manifestKeeper, s.poakeeper.IsAdmin)

	type tc struct {
		name      string
		inflation bool
		msg       sdk.Msg
		err       string
	}

	tcs := []tc{
		{
			name:      "success; 0 inflation tokenfactory mint",
			inflation: false,
			msg:       tokenfactorytypes.NewMsgMint(poaAdmin.String(), coin),
		},
		{
			name:      "success; 0 inflation payout stakeholders",
			inflation: false,
			msg:       manifesttypes.NewMsgPayoutStakeholders(poaAdmin, coin),
		},
		{
			name:      "success; TF mint from standard user",
			inflation: false,
			msg:       tokenfactorytypes.NewMsgMint(stdUser.String(), tfCoin),
		},
		{
			name:      "success; TF mint from standard user with inflation still allowed",
			inflation: true,
			msg:       tokenfactorytypes.NewMsgMint(stdUser.String(), tfCoin),
		},
		{
			name:      "fail; inflation enabled, no manual mint from admin",
			inflation: true,
			msg:       tokenfactorytypes.NewMsgMint(poaAdmin.String(), coin),
			err:       manifestkeeper.ErrManualMintingDisabled.Error(),
		},
		{
			name:      "fail; inflation enabled, no manual payout from admin",
			inflation: true,
			msg:       manifesttypes.NewMsgPayoutStakeholders(poaAdmin, coin),
			err:       manifestkeeper.ErrManualMintingDisabled.Error(),
		},
	}

	for _, tc := range tcs {
		tc := tc

		// set auto inflation (or not)
		currParams, err := s.manifestKeeper.Params.Get(s.ctx)
		s.Require().NoError(err)
		currParams.Inflation = manifesttypes.NewInflation(tc.inflation, 50_000_000000, "umfx")
		s.manifestKeeper.Params.Set(s.ctx, currParams)

		_, err = ante.AnteHandle(s.ctx, decorators.NewMockTx(tc.msg), false, EmptyAnte)

		if tc.err == "" {
			s.Require().NoError(err)
		} else {
			s.Require().Error(err)
			s.Require().Contains(err.Error(), tc.err)
		}
	}
}
