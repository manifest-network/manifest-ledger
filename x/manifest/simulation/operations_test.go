package simulation_test

import (
	"math/rand"

	"cosmossdk.io/depinject"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil/configurator"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktestutil "github.com/cosmos/cosmos-sdk/x/bank/testutil"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/gogoproto/proto"
	"github.com/liftedinit/manifest-ledger/x/manifest/keeper"
	"github.com/liftedinit/manifest-ledger/x/manifest/simulation"
	"github.com/liftedinit/manifest-ledger/x/manifest/types"
	"github.com/stretchr/testify/suite"
)

var AppConfig = configurator.NewAppConfig(
	configurator.AuthModule(),
	configurator.BankModule(),
	configurator.StakingModule(),
	configurator.TxModule(),
	configurator.ConsensusModule(),
	configurator.GenutilModule(),
	configurator.DistributionModule(),
	configurator.MintModule(),
)

type SimTestSuite struct {
	suite.Suite

	ctx         sdk.Context
	app         *runtime.App
	genesisVals []stakingtypes.Validator

	txConfig       client.TxConfig
	cdc            codec.Codec
	stakingKeeper  *stakingkeeper.Keeper
	accountKeeper  authkeeper.AccountKeeper
	bankKeeper     bankkeeper.Keeper
	manifestKeeper keeper.Keeper
}

func (suite *SimTestSuite) SetupTest() {
	var (
		appBuilder *runtime.AppBuilder
		err        error
	)
	suite.app, err = simtestutil.Setup(
		depinject.Configs(
			AppConfig,
			depinject.Supply(log.NewNopLogger()),
		),
		&suite.accountKeeper,
		&suite.bankKeeper,
		&suite.cdc,
		&appBuilder,
		&suite.stakingKeeper,
		&suite.manifestKeeper,
		&suite.txConfig,
	)

	suite.NoError(err)

	suite.ctx = suite.app.BaseApp.NewContext(false)

	genesisVals, err := suite.stakingKeeper.GetAllValidators(suite.ctx)
	suite.Require().NoError(err)
	suite.Require().Len(genesisVals, 1)
	suite.genesisVals = genesisVals
}

func (suite *SimTestSuite) TestSimulateMsgShareholdersPayout() {
	// setup 3 accounts
	s := rand.NewSource(1)
	r := rand.New(s)
	accounts := suite.getTestingAccounts(r, 3)

	op := simulation.SimulateMsgPayout(suite.manifestKeeper)
	operationMsg, futureOperations, err := op(r, suite.app.BaseApp, suite.ctx, accounts, "")
	suite.Require().NoError(err)

	var msg types.MsgPayout
	err = proto.Unmarshal(operationMsg.Msg, &msg)
	suite.Require().NoError(err)
	suite.Require().True(operationMsg.OK)
	suite.Require().Len(futureOperations, 0)

	// execute operation
	//op := simulation.SimulateMsgSetWithdrawAddress(suite.txConfig, suite.accountKeeper, suite.bankKeeper, suite.distrKeeper)
	//operationMsg, futureOperations, err := op(r, suite.app.BaseApp, suite.ctx, accounts, "")
	//suite.Require().NoError(err)

	//var msg types.MsgSetWithdrawAddress
	//err = proto.Unmarshal(operationMsg.Msg, &msg)
	//suite.Require().NoError(err)
	//suite.Require().True(operationMsg.OK)
	//suite.Require().Equal("cosmos1ghekyjucln7y67ntx7cf27m9dpuxxemn4c8g4r", msg.DelegatorAddress)
	//suite.Require().Equal("cosmos1p8wcgrjr4pjju90xg6u9cgq55dxwq8j7u4x9a0", msg.WithdrawAddress)
	//suite.Require().Equal(sdk.MsgTypeURL(&types.MsgSetWithdrawAddress{}), sdk.MsgTypeURL(&msg))
	//suite.Require().Len(futureOperations, 0)
}

func (suite *SimTestSuite) getTestingAccounts(r *rand.Rand, n int) []simtypes.Account {
	accounts := simtypes.RandomAccounts(r, n)

	initAmt := suite.stakingKeeper.TokensFromConsensusPower(suite.ctx, 200)
	initCoins := sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, initAmt))

	// add coins to the accounts
	for _, account := range accounts {
		acc := suite.accountKeeper.NewAccountWithAddress(suite.ctx, account.Address)
		suite.accountKeeper.SetAccount(suite.ctx, acc)
		suite.Require().NoError(banktestutil.FundAccount(suite.ctx, suite.bankKeeper, account.Address, initCoins))
	}

	return accounts
}
