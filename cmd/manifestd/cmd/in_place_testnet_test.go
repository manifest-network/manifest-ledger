package cmd

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/crypto"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/server"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	poakeeper "github.com/strangelove-ventures/poa/keeper"

	"github.com/manifest-network/manifest-ledger/app"
	appparams "github.com/manifest-network/manifest-ledger/app/params"
	manifesttypes "github.com/manifest-network/manifest-ledger/x/manifest/types"
)

func TestInitAppForTestnetReplacesSourceValidatorState(t *testing.T) {
	ctx, chainApp := setupInPlaceTestnet(t)
	oldVal := seedSourceTestnetValidator(t, ctx, chainApp, false)
	oldValAddr, err := sdk.ValAddressFromBech32(oldVal.OperatorAddress)
	require.NoError(t, err)
	oldVal.Status = stakingtypes.Bonded
	oldVal.DelegatorShares = sdkmath.LegacyNewDecFromInt(oldVal.Tokens)
	require.NoError(t, chainApp.StakingKeeper.SetValidator(ctx, oldVal))
	require.NoError(t, chainApp.StakingKeeper.SetLastValidatorPower(ctx, oldValAddr, 1))
	require.NoError(t, chainApp.StakingKeeper.SetLastTotalPower(ctx, sdkmath.OneInt()))
	require.NoError(t, chainApp.StakingKeeper.Hooks().AfterValidatorCreated(ctx, oldValAddr))
	oldDelegator := sdk.AccAddress(oldValAddr)
	require.NoError(t, chainApp.StakingKeeper.SetDelegation(ctx, stakingtypes.NewDelegation(oldDelegator.String(), oldValAddr.String(), oldVal.DelegatorShares)))
	require.NoError(t, chainApp.StakingKeeper.Hooks().AfterDelegationModified(ctx, oldDelegator, oldValAddr))
	sourceFunds := sdk.NewCoins(sdk.NewInt64Coin(appparams.BondDenom, 101))
	require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, manifesttypes.ModuleName, sourceFunds))
	require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, manifesttypes.ModuleName, oldDelegator, sourceFunds))
	oldVals := []stakingtypes.Validator{oldVal}
	oldConsAddr, err := oldVal.GetConsAddr()
	require.NoError(t, err)
	oldBalance := chainApp.BankKeeper.GetAllBalances(ctx, oldDelegator)

	// A power change can leave a second index pointing to the same validator.
	// Enumerating only current validators cannot find that obsolete entry.
	oldVal.Tokens = oldVal.Tokens.Add(chainApp.StakingKeeper.PowerReduction(ctx))
	require.NoError(t, chainApp.StakingKeeper.SetValidator(ctx, oldVal))
	require.NoError(t, chainApp.StakingKeeper.SetValidatorByPowerIndex(ctx, oldVal))
	jailed := seedSourceTestnetValidator(t, ctx, chainApp, true)
	unbonded := seedSourceTestnetValidator(t, ctx, chainApp, false)
	unbondedAddr, err := sdk.ValAddressFromBech32(unbonded.OperatorAddress)
	require.NoError(t, err)

	completion := ctx.BlockTime().Add(time.Hour)
	_, err = chainApp.StakingKeeper.SetUnbondingDelegationEntry(ctx, oldDelegator, oldValAddr, 1, completion, sdkmath.NewInt(7))
	require.NoError(t, err)
	require.NoError(t, chainApp.StakingKeeper.SetUBDQueueTimeSlice(ctx, completion, []stakingtypes.DVPair{{DelegatorAddress: oldDelegator.String(), ValidatorAddress: oldValAddr.String()}}))
	_, err = chainApp.StakingKeeper.SetRedelegationEntry(ctx, oldDelegator, oldValAddr, unbondedAddr, 1, completion, sdkmath.OneInt(), sdkmath.LegacyOneDec(), sdkmath.LegacyOneDec())
	require.NoError(t, err)
	require.NoError(t, chainApp.StakingKeeper.SetRedelegationQueueTimeSlice(ctx, completion, []stakingtypes.DVVTriplet{{DelegatorAddress: oldDelegator.String(), ValidatorSrcAddress: oldValAddr.String(), ValidatorDstAddress: unbondedAddr.String()}}))
	require.NoError(t, chainApp.StakingKeeper.SetUnbondingValidatorsQueue(ctx, completion, 1, []string{unbonded.OperatorAddress}))
	oldUpdate := oldVal.ABCIValidatorUpdate(chainApp.StakingKeeper.PowerReduction(ctx))
	require.NoError(t, chainApp.StakingKeeper.SetValidatorUpdates(ctx, []abci.ValidatorUpdate{oldUpdate}))

	oldPubKey, err := oldVal.ConsPubKey()
	require.NoError(t, err)
	require.NoError(t, chainApp.POAKeeper.PendingValidators.Set(ctx, poakeeper.DefaultPendingValidators()))
	require.NoError(t, chainApp.POAKeeper.AddPendingValidator(ctx, oldVal, oldPubKey))
	require.NoError(t, chainApp.POAKeeper.UpdatedValidatorsCache.Set(ctx, oldVal.OperatorAddress))
	require.NoError(t, chainApp.POAKeeper.SetCachedBlockPower(ctx, 13))
	require.NoError(t, chainApp.POAKeeper.SetAbsoluteChangedInBlockPower(ctx, 9))
	rewardCoins := sdk.NewCoins(sdk.NewInt64Coin(appparams.BondDenom, 29))
	require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, manifesttypes.ModuleName, rewardCoins))
	require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToModule(ctx, manifesttypes.ModuleName, distrtypes.ModuleName, rewardCoins))
	require.NoError(t, chainApp.DistrKeeper.SetValidatorOutstandingRewards(ctx, oldValAddr, distrtypes.ValidatorOutstandingRewards{Rewards: sdk.NewDecCoinsFromCoins(rewardCoins...)}))
	feePoolBefore, err := chainApp.DistrKeeper.FeePool.Get(ctx)
	require.NoError(t, err)

	paramsBefore, err := chainApp.StakingKeeper.GetParams(ctx)
	require.NoError(t, err)
	historical := stakingtypes.HistoricalInfo{Header: ctx.BlockHeader(), Valset: oldVals}
	require.NoError(t, chainApp.StakingKeeper.SetHistoricalInfo(ctx, 1, &historical))

	// Exercise surplus burning in both pools while preserving unrelated denoms.
	bondDenom, err := chainApp.StakingKeeper.BondDenom(ctx)
	require.NoError(t, err)
	wantTokens := sdkmath.NewInt(inPlaceTestnetPower).Mul(chainApp.StakingKeeper.PowerReduction(ctx))
	for _, pool := range []string{stakingtypes.BondedPoolName, stakingtypes.NotBondedPoolName} {
		funds := sdk.NewCoins(sdk.NewCoin(bondDenom, wantTokens.AddRaw(17)), sdk.NewInt64Coin("unrelated", 23))
		require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, manifesttypes.ModuleName, funds))
		require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToModule(ctx, manifesttypes.ModuleName, pool, funds))
	}
	bondedAddr := chainApp.AccountKeeper.GetModuleAddress(stakingtypes.BondedPoolName)
	notBondedAddr := chainApp.AccountKeeper.GetModuleAddress(stakingtypes.NotBondedPoolName)
	poolTokensBefore := chainApp.BankKeeper.GetBalance(ctx, bondedAddr, bondDenom).Amount.Add(chainApp.BankKeeper.GetBalance(ctx, notBondedAddr, bondDenom).Amount)
	supplyBefore := chainApp.BankKeeper.GetSupply(ctx, bondDenom).Amount
	feeSupplyBefore := chainApp.BankKeeper.GetSupply(ctx, appparams.BondDenom).Amount
	operator := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	newPubKey := ed25519.GenPrivKey().PubKey()

	require.NoError(t, initAppForTestnet(chainApp, newPubKey.Address(), newPubKey, operator.String(), ""))
	valAddr := sdk.ValAddress(operator)
	vals, err := chainApp.StakingKeeper.GetAllValidators(ctx)
	require.NoError(t, err)
	require.Len(t, vals, 1)
	require.Equal(t, valAddr.String(), vals[0].OperatorAddress)
	require.Equal(t, stakingtypes.Bonded, vals[0].Status)
	require.False(t, vals[0].Jailed)
	require.Equal(t, inPlaceTestnetPower, vals[0].ConsensusPower(chainApp.StakingKeeper.PowerReduction(ctx)))
	require.True(t, wantTokens.Equal(vals[0].Tokens))
	for _, obsolete := range []stakingtypes.Validator{oldVal, jailed, unbonded} {
		addr, err := obsolete.GetConsAddr()
		require.NoError(t, err)
		_, err = chainApp.StakingKeeper.GetValidatorByConsAddr(ctx, addr)
		require.ErrorIs(t, err, stakingtypes.ErrNoValidatorFound)
	}
	byConsAddr, err := chainApp.StakingKeeper.GetValidatorByConsAddr(ctx, sdk.ConsAddress(newPubKey.Address()))
	require.NoError(t, err)
	require.Equal(t, vals[0], byConsAddr)
	lastVals, err := chainApp.StakingKeeper.GetLastValidators(ctx)
	require.NoError(t, err)
	require.Equal(t, vals, lastVals)
	power, err := chainApp.StakingKeeper.GetLastValidatorPower(ctx, valAddr)
	require.NoError(t, err)
	require.Equal(t, inPlaceTestnetPower, power)
	totalPower, err := chainApp.StakingKeeper.GetLastTotalPower(ctx)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(inPlaceTestnetPower), totalPower)

	delegations, err := chainApp.StakingKeeper.GetAllDelegations(ctx)
	require.NoError(t, err)
	require.Equal(t, []stakingtypes.Delegation{stakingtypes.NewDelegation(operator.String(), valAddr.String(), vals[0].DelegatorShares)}, delegations)
	startingInfo, err := chainApp.DistrKeeper.GetDelegatorStartingInfo(ctx, valAddr, operator)
	require.NoError(t, err)
	require.True(t, startingInfo.Stake.Equal(sdkmath.LegacyNewDecFromInt(wantTokens)))
	_, err = chainApp.DistrKeeper.GetValidatorCurrentRewards(ctx, valAddr)
	require.NoError(t, err)
	feePoolAfter, err := chainApp.DistrKeeper.FeePool.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, feePoolBefore.CommunityPool.Add(sdk.NewDecCoinsFromCoins(rewardCoins...)...), feePoolAfter.CommunityPool)
	signingInfo, err := chainApp.SlashingKeeper.GetValidatorSigningInfo(ctx, sdk.ConsAddress(newPubKey.Address()))
	require.NoError(t, err)
	require.Equal(t, chainApp.LastBlockHeight()-1, signingInfo.StartHeight)
	require.False(t, signingInfo.Tombstoned)
	require.Zero(t, signingInfo.MissedBlocksCounter)
	registeredKey, err := chainApp.SlashingKeeper.GetPubkey(ctx, newPubKey.Address())
	require.NoError(t, err)
	require.Equal(t, newPubKey.Bytes(), registeredKey.Bytes())

	store := ctx.KVStore(chainApp.GetKey(stakingtypes.StoreKey))
	for _, prefix := range [][]byte{
		stakingtypes.UnbondingDelegationKey, stakingtypes.UnbondingDelegationByValIndexKey,
		stakingtypes.RedelegationKey, stakingtypes.RedelegationByValSrcIndexKey, stakingtypes.RedelegationByValDstIndexKey,
		stakingtypes.UnbondingIndexKey, stakingtypes.UnbondingTypeKey,
		stakingtypes.UnbondingQueueKey, stakingtypes.RedelegationQueueKey, stakingtypes.ValidatorQueueKey,
	} {
		iterator := storetypes.KVStorePrefixIterator(store, prefix)
		require.False(t, iterator.Valid(), "obsolete staking state under prefix %x", prefix)
		require.NoError(t, iterator.Close())
	}
	pending, err := chainApp.POAKeeper.GetPendingValidators(ctx)
	require.NoError(t, err)
	require.Empty(t, pending.Validators)
	cached, err := chainApp.POAKeeper.UpdatedValidatorsCache.Has(ctx, oldVal.OperatorAddress)
	require.NoError(t, err)
	require.False(t, cached)
	cachedPower, err := chainApp.POAKeeper.GetCachedBlockPower(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(inPlaceTestnetPower), cachedPower)
	changedPower, err := chainApp.POAKeeper.GetAbsoluteChangedInBlockPower(ctx)
	require.NoError(t, err)
	require.Zero(t, changedPower)

	require.NotNil(t, chainApp.AccountKeeper.GetAccount(ctx, operator))
	require.Equal(t, sdk.NewInt64Coin(appparams.BondDenom, testnetOperatorFunds), chainApp.BankKeeper.GetBalance(ctx, operator, appparams.BondDenom))
	isOperator, err := chainApp.POAKeeper.IsSenderValidator(ctx, operator.String(), valAddr.String())
	require.NoError(t, err)
	require.True(t, isOperator)
	require.Equal(t, oldBalance, chainApp.BankKeeper.GetAllBalances(ctx, oldDelegator))
	require.Equal(t, sdk.NewCoin(bondDenom, wantTokens), chainApp.BankKeeper.GetBalance(ctx, bondedAddr, bondDenom))
	require.True(t, chainApp.BankKeeper.GetBalance(ctx, notBondedAddr, bondDenom).IsZero())
	for _, address := range []sdk.AccAddress{bondedAddr, notBondedAddr} {
		require.Equal(t, sdk.NewInt64Coin("unrelated", 23), chainApp.BankKeeper.GetBalance(ctx, address, "unrelated"))
	}
	wantSupply := supplyBefore.Sub(poolTokensBefore).Add(wantTokens)
	if bondDenom == appparams.BondDenom {
		wantSupply = wantSupply.AddRaw(testnetOperatorFunds)
	} else {
		require.Equal(t, feeSupplyBefore.AddRaw(testnetOperatorFunds), chainApp.BankKeeper.GetSupply(ctx, appparams.BondDenom).Amount)
	}
	require.Equal(t, wantSupply, chainApp.BankKeeper.GetSupply(ctx, bondDenom).Amount)
	paramsAfter, err := chainApp.StakingKeeper.GetParams(ctx)
	require.NoError(t, err)
	require.Equal(t, paramsBefore, paramsAfter)
	historicalAfter, err := chainApp.StakingKeeper.GetHistoricalInfo(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, historical, historicalAfter)
	_, err = chainApp.UpgradeKeeper.GetUpgradePlan(ctx)
	require.ErrorIs(t, err, upgradetypes.ErrNoUpgradePlanFound)
	message, broken := stakingkeeper.AllInvariants(chainApp.StakingKeeper)(ctx)
	require.False(t, broken, message)
	message, broken = distrkeeper.AllInvariants(chainApp.DistrKeeper)(ctx)
	require.False(t, broken, message)

	// Running the real block hooks catches stale POA cache references and
	// power indices which would otherwise remove or duplicate the fork validator.
	blockCtx, _ := ctx.CacheContext()
	ctx = blockCtx.WithBlockHeight(chainApp.LastBlockHeight() + 1).WithBlockTime(completion.Add(time.Second))
	_, err = chainApp.BeginBlocker(ctx)
	require.NoError(t, err)
	endBlock, err := chainApp.EndBlocker(ctx)
	require.NoError(t, err)
	require.Empty(t, endBlock.ValidatorUpdates)
	message, broken = stakingkeeper.AllInvariants(chainApp.StakingKeeper)(ctx)
	require.False(t, broken, message)
	message, broken = distrkeeper.AllInvariants(chainApp.DistrKeeper)(ctx)
	require.False(t, broken, message)
	_, err = chainApp.StakingKeeper.GetValidatorByConsAddr(ctx, oldConsAddr)
	require.ErrorIs(t, err, stakingtypes.ErrNoValidatorFound)
	finalized, err := chainApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: ctx.BlockHeight(), Time: ctx.BlockTime(),
	})
	require.NoError(t, err)
	require.Empty(t, finalized.ValidatorUpdates)
	_, err = chainApp.Commit()
	require.NoError(t, err)
	committedCtx := chainApp.NewUncachedContext(true, ctx.BlockHeader())
	committedVals, err := chainApp.StakingKeeper.GetAllValidators(committedCtx)
	require.NoError(t, err)
	require.Equal(t, vals, committedVals)
	require.Equal(t, sdk.NewInt64Coin(appparams.BondDenom, testnetOperatorFunds), chainApp.BankKeeper.GetBalance(committedCtx, operator, appparams.BondDenom))
}

func seedSourceTestnetValidator(t *testing.T, ctx sdk.Context, chainApp *app.ManifestApp, jailed bool) stakingtypes.Validator {
	t.Helper()
	pubKey, err := cryptocodec.FromCmtPubKeyInterface(ed25519.GenPrivKey().PubKey())
	require.NoError(t, err)
	validator, err := stakingtypes.NewValidator(sdk.ValAddress(pubKey.Address()).String(), pubKey, stakingtypes.Description{Moniker: "source validator"})
	require.NoError(t, err)
	validator.Jailed = jailed
	validator.Tokens = chainApp.StakingKeeper.PowerReduction(ctx)
	require.NoError(t, chainApp.StakingKeeper.SetValidator(ctx, validator))
	require.NoError(t, chainApp.StakingKeeper.SetValidatorByConsAddr(ctx, validator))
	require.NoError(t, chainApp.StakingKeeper.SetValidatorByPowerIndex(ctx, validator))
	return validator
}

func TestInitAppForTestnetSchedulesUpgrade(t *testing.T) {
	ctx, chainApp := setupInPlaceTestnet(t)
	operator := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	pubKey := ed25519.GenPrivKey().PubKey()
	existing := sdk.NewCoins(sdk.NewInt64Coin(appparams.BondDenom, 57))
	require.NoError(t, chainApp.BankKeeper.MintCoins(ctx, manifesttypes.ModuleName, existing))
	require.NoError(t, chainApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, manifesttypes.ModuleName, operator, existing))
	require.NoError(t, initAppForTestnet(chainApp, pubKey.Address(), pubKey, operator.String(), "rehearsal-upgrade"))
	plan, err := chainApp.UpgradeKeeper.GetUpgradePlan(ctx)
	require.NoError(t, err)
	require.Equal(t, "rehearsal-upgrade", plan.Name)
	require.Equal(t, chainApp.LastBlockHeight()+testnetUpgradeDelay, plan.Height)
	require.Equal(t, sdk.NewInt64Coin(appparams.BondDenom, testnetOperatorFunds+57), chainApp.BankKeeper.GetBalance(ctx, operator, appparams.BondDenom))
	message, broken := stakingkeeper.AllInvariants(chainApp.StakingKeeper)(ctx)
	require.False(t, broken, message)
}

func TestInitAppForTestnetRejectsInvalidIdentity(t *testing.T) {
	ctx, chainApp := setupInPlaceTestnet(t)
	pubKey := ed25519.GenPrivKey().PubKey()
	operator := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address()).String()
	validatorsBefore, err := chainApp.StakingKeeper.GetAllValidators(ctx)
	require.NoError(t, err)
	supplyBefore := chainApp.BankKeeper.GetSupply(ctx, appparams.BondDenom)
	for _, tc := range []struct {
		name     string
		operator string
		address  cmtbytes.HexBytes
		pubKey   crypto.PubKey
	}{
		{name: "invalid account address", operator: "invalid", address: pubKey.Address(), pubKey: pubKey},
		{name: "validator address instead of account", operator: sdk.ValAddress(pubKey.Address()).String(), address: pubKey.Address(), pubKey: pubKey},
		{name: "nil public key", operator: operator, address: pubKey.Address()},
		{name: "mismatched consensus address", operator: operator, address: ed25519.GenPrivKey().PubKey().Address(), pubKey: pubKey},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, initAppForTestnet(chainApp, tc.address, tc.pubKey, tc.operator, ""))
			validatorsAfter, err := chainApp.StakingKeeper.GetAllValidators(ctx)
			require.NoError(t, err)
			require.Equal(t, validatorsBefore, validatorsAfter)
			require.Equal(t, supplyBefore, chainApp.BankKeeper.GetSupply(ctx, appparams.BondDenom))
		})
	}
}

func TestInitAppForTestnetRollsBackFailedUpgrade(t *testing.T) {
	ctx, chainApp := setupInPlaceTestnet(t)
	seedSourceTestnetValidator(t, ctx, chainApp, false)
	pubKey := ed25519.GenPrivKey().PubKey()
	operator := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
	validatorsBefore, err := chainApp.StakingKeeper.GetAllValidators(ctx)
	require.NoError(t, err)
	delegationsBefore, err := chainApp.StakingKeeper.GetAllDelegations(ctx)
	require.NoError(t, err)
	supplyBefore := chainApp.BankKeeper.GetSupply(ctx, appparams.BondDenom)
	// A damaged source upgrade plan fails scheduling after validator replacement
	// and funding. No portion of the attempted rewrite may reach the root store.
	upgradeStore := ctx.KVStore(chainApp.GetKey(upgradetypes.StoreKey))
	upgradeStore.Set(upgradetypes.PlanKey(), []byte{0xff})
	require.ErrorContains(t, initAppForTestnet(chainApp, pubKey.Address(), pubKey, operator.String(), "rehearsal-upgrade"), "schedule testnet upgrade")
	validatorsAfter, err := chainApp.StakingKeeper.GetAllValidators(ctx)
	require.NoError(t, err)
	require.Equal(t, validatorsBefore, validatorsAfter)
	delegationsAfter, err := chainApp.StakingKeeper.GetAllDelegations(ctx)
	require.NoError(t, err)
	require.Equal(t, delegationsBefore, delegationsAfter)
	require.Equal(t, supplyBefore, chainApp.BankKeeper.GetSupply(ctx, appparams.BondDenom))
	require.Nil(t, chainApp.AccountKeeper.GetAccount(ctx, operator))
	require.True(t, chainApp.BankKeeper.GetBalance(ctx, operator, appparams.BondDenom).IsZero())
	require.Equal(t, []byte{0xff}, upgradeStore.Get(upgradetypes.PlanKey()))
}

// Commit must follow FinalizeBlock, which flushes the genesis cache to the root
// multistore. Calling Commit directly after InitChain loses initialized stores.
func setupInPlaceTestnet(t *testing.T) (sdk.Context, *app.ManifestApp) {
	t.Helper()
	t.Setenv("POA_ADMIN_ADDRESS", sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address()).String())
	chainApp := app.NewApp(log.NewNopLogger(), dbm.NewMemDB(), nil, true, app.DefaultCommissionRateMinMax,
		simtestutil.NewAppOptionsWithFlagHome(t.TempDir()), baseapp.SetChainID(app.SimAppChainID))
	genesis := chainApp.DefaultGenesis()
	pubKey, err := cryptocodec.FromCmtPubKeyInterface(ed25519.GenPrivKey().PubKey())
	require.NoError(t, err)
	operator := sdk.AccAddress(pubKey.Address())
	validator, err := stakingtypes.NewValidator(sdk.ValAddress(operator).String(), pubKey, stakingtypes.Description{})
	require.NoError(t, err)
	validator.Status = stakingtypes.Bonded
	validator.Tokens = sdk.DefaultPowerReduction
	validator.DelegatorShares = sdkmath.LegacyNewDecFromInt(validator.Tokens)
	stakingParams := stakingtypes.DefaultParams()
	stakingParams.BondDenom = appparams.BondDenom
	genesis[stakingtypes.ModuleName] = chainApp.AppCodec().MustMarshalJSON(stakingtypes.NewGenesisState(stakingParams,
		[]stakingtypes.Validator{validator}, []stakingtypes.Delegation{stakingtypes.NewDelegation(operator.String(), validator.OperatorAddress, validator.DelegatorShares)}))
	genesis[authtypes.ModuleName] = chainApp.AppCodec().MustMarshalJSON(authtypes.NewGenesisState(authtypes.DefaultParams(),
		[]authtypes.GenesisAccount{authtypes.NewBaseAccount(operator, nil, 0, 0)}))
	bondedCoins := sdk.NewCoins(sdk.NewCoin(appparams.BondDenom, validator.Tokens))
	genesis[banktypes.ModuleName] = chainApp.AppCodec().MustMarshalJSON(banktypes.NewGenesisState(banktypes.DefaultParams(),
		[]banktypes.Balance{{Address: authtypes.NewModuleAddress(stakingtypes.BondedPoolName).String(), Coins: bondedCoins}}, bondedCoins, nil, nil))
	genesisJSON, err := json.Marshal(genesis)
	require.NoError(t, err)
	header := cmtproto.Header{Height: 1, ChainID: app.SimAppChainID, Time: time.Now().UTC()}
	_, err = chainApp.InitChain(&abci.RequestInitChain{
		InitialHeight: 1, ChainId: header.ChainID, Time: header.Time,
		AppStateBytes: genesisJSON, ConsensusParams: app.DefaultConsensusParams,
	})
	require.NoError(t, err)
	_, err = chainApp.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 1, Time: header.Time})
	require.NoError(t, err)
	_, err = chainApp.Commit()
	require.NoError(t, err)
	return chainApp.NewUncachedContext(true, header), chainApp
}

func TestNewTestnetAppRejectsIncorrectOptionTypes(t *testing.T) {
	pubKey := ed25519.GenPrivKey().PubKey()
	for _, tc := range []struct {
		name    string
		options simtestutil.AppOptionsMap
		message string
	}{
		{name: "address", options: simtestutil.AppOptionsMap{server.KeyNewValAddr: pubKey.Address().String()}, message: "in-place-testnet: expected consensus address as bytes.HexBytes"},
		{name: "public key", options: simtestutil.AppOptionsMap{server.KeyNewValAddr: pubKey.Address(), server.KeyUserPubKey: pubKey.Bytes()}, message: "in-place-testnet: expected consensus public key as crypto.PubKey"},
		{name: "operator", options: simtestutil.AppOptionsMap{server.KeyNewValAddr: pubKey.Address(), server.KeyUserPubKey: pubKey, server.KeyNewOpAddr: sdk.AccAddress(pubKey.Address())}, message: "in-place-testnet: expected operator account address as string"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.PanicsWithValue(t, tc.message, func() { newTestnetApp(log.NewNopLogger(), nil, nil, tc.options) })
		})
	}
}
