package decorators_test

import (
	"context"
	"testing"

	app "github.com/liftedinit/manifest-ledger/app"
	"github.com/liftedinit/manifest-ledger/app/decorators"
	appparams "github.com/liftedinit/manifest-ledger/app/params"
	manifestkeeper "github.com/liftedinit/manifest-ledger/x/manifest/keeper"
	tokenfactorytypes "github.com/reecepbcups/tokenfactory/x/tokenfactory/types"
	poa "github.com/strangelove-ventures/poa"
	poakeeper "github.com/strangelove-ventures/poa/keeper"
	"github.com/stretchr/testify/suite"

	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	mintkeeper "github.com/cosmos/cosmos-sdk/x/mint/keeper"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
)

// Define an empty ante handle
var (
	EmptyAnte = func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		return ctx, nil
	}
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

	inflation := sdkmath.LegacyNewDecWithPrec(1, 2) // 1%
	zero := sdkmath.LegacyZeroDec()

	// tx: inflation is 0 so manual minting is allowed from the poa admin
	s.Require().NoError(s.mintKeeper.Minter.Set(s.ctx, minttypes.InitialMinter(zero)))
	msg := tokenfactorytypes.NewMsgMint(poaAdmin.String(), sdk.NewCoin(appparams.BondDenom, sdkmath.NewInt(1)))
	_, err := ante.AnteHandle(s.ctx, decorators.NewMockTx(msg), false, EmptyAnte)
	s.Require().NoError(err)

	// minting is allowed from the stdUser too
	msg = tokenfactorytypes.NewMsgMint(stdUser.String(), sdk.NewCoin(appparams.BondDenom, sdkmath.NewInt(1)))
	_, err = ante.AnteHandle(s.ctx, decorators.NewMockTx(msg), false, EmptyAnte)
	s.Require().NoError(err)

	// tx: inflation is 1% so manual minting is not allowed from the poa admin
	s.Require().NoError(s.mintKeeper.Minter.Set(s.ctx, minttypes.InitialMinter(inflation)))
	msg = tokenfactorytypes.NewMsgMint(poaAdmin.String(), sdk.NewCoin(appparams.BondDenom, sdkmath.NewInt(1)))
	_, err = ante.AnteHandle(s.ctx, decorators.NewMockTx(msg), false, EmptyAnte)
	s.Require().Contains(err.Error(), manifestkeeper.ErrManualMintingDisabled.Error())

	// tx: inflation is still 1%, but normal users can still mint (non admins)
	msg = tokenfactorytypes.NewMsgMint(stdUser.String(), sdk.NewCoin(appparams.BondDenom, sdkmath.NewInt(1)))
	_, err = ante.AnteHandle(s.ctx, decorators.NewMockTx(msg), false, EmptyAnte)
	s.Require().NoError(err)
}
