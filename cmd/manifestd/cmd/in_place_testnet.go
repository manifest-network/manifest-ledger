package cmd

import (
	"bytes"
	"fmt"
	"io"

	"github.com/spf13/cast"

	"github.com/cometbft/cometbft/crypto"
	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	poakeeper "github.com/strangelove-ventures/poa/keeper"

	"github.com/manifest-network/manifest-ledger/app"
	"github.com/manifest-network/manifest-ledger/app/helpers"
	"github.com/manifest-network/manifest-ledger/app/params"
	manifesttypes "github.com/manifest-network/manifest-ledger/x/manifest/types"
)

const (
	// Keep this equal to the CometBFT power installed by SDK server.testnetify.
	// Tokens must include PowerReduction; copying the SDK documentation's token
	// amount directly leaves staking and CometBFT with different voting powers.
	inPlaceTestnetPower  int64 = 900000000000000
	testnetOperatorFunds int64 = 1000000000000
	testnetUpgradeDelay  int64 = 1
)

// newTestnetApp is used only by in-place-testnet. Normal start and export must
// never rewrite validator state or mint rehearsal funds.
func newTestnetApp(logger log.Logger, db dbm.DB, traceStore io.Writer, appOpts servertypes.AppOptions) servertypes.Application {
	newValAddr, ok := appOpts.Get(server.KeyNewValAddr).(cmtbytes.HexBytes)
	if !ok {
		panic("in-place-testnet: expected consensus address as bytes.HexBytes")
	}
	newValPubKey, ok := appOpts.Get(server.KeyUserPubKey).(crypto.PubKey)
	if !ok {
		panic("in-place-testnet: expected consensus public key as crypto.PubKey")
	}
	newOperatorAddress, ok := appOpts.Get(server.KeyNewOpAddr).(string)
	if !ok {
		panic("in-place-testnet: expected operator account address as string")
	}
	operator, err := sdk.AccAddressFromBech32(newOperatorAddress)
	if err != nil {
		panic(fmt.Errorf("decode operator account address: %w", err))
	}
	if err := validateTestnetAuthority(operator, helpers.GetPoAAdmin()); err != nil {
		panic(fmt.Errorf("initialize in-place testnet: %w", err))
	}

	chainApp := newApp(logger, db, traceStore, appOpts).(*app.ManifestApp)
	if err := initAppForTestnet(chainApp, newValAddr, newValPubKey, newOperatorAddress, cast.ToString(appOpts.Get(server.KeyTriggerTestnetUpgrade))); err != nil {
		panic(fmt.Errorf("initialize in-place testnet: %w", err))
	}
	return chainApp
}

func validateTestnetAuthority(operator sdk.AccAddress, authority string) error {
	if operator.String() != authority {
		return fmt.Errorf("operator account %s does not match configured POA admin %s; set POA_ADMIN_ADDRESS to the operator before starting", operator, authority)
	}
	return nil
}

// initAppForTestnet replaces the source chain's validator state on a disposable
// copy of its database. It writes to the root multistore without advancing the
// height: the first fork block commits these changes. Stop and restart with
// normal start only after that block has committed.
func initAppForTestnet(chainApp *app.ManifestApp, newValAddr cmtbytes.HexBytes, newValPubKey crypto.PubKey, newOperatorAddress, upgradeToTrigger string) error {
	operator, err := sdk.AccAddressFromBech32(newOperatorAddress)
	if err != nil {
		return fmt.Errorf("decode operator account address: %w", err)
	}
	if newValPubKey == nil || !bytes.Equal(newValAddr, newValPubKey.Address()) {
		return fmt.Errorf("consensus address does not match public key")
	}
	pubKey, err := cryptocodec.FromCmtPubKeyInterface(newValPubKey)
	if err != nil {
		return fmt.Errorf("convert consensus public key: %w", err)
	}
	if chainApp.LastBlockHeight() < 1 {
		return fmt.Errorf("in-place-testnet requires committed source chain state")
	}

	// Keep a failed app rewrite atomic. NewContext writes only to the check
	// cache; NewUncachedContext is required for the first FinalizeBlock to see it.
	ctx := chainApp.NewUncachedContext(true, cmtproto.Header{Height: chainApp.LastBlockHeight()})
	if err := validateTestnetAuthority(operator, chainApp.POAKeeper.GetAdmin(ctx)); err != nil {
		return err
	}
	ctx, write := ctx.CacheContext()
	valAddr := sdk.ValAddress(operator)
	validator, err := stakingtypes.NewValidator(valAddr.String(), pubKey, stakingtypes.Description{Moniker: "Testnet Validator"})
	if err != nil {
		return fmt.Errorf("create testnet validator: %w", err)
	}
	validator.Status = stakingtypes.Bonded
	validator.Tokens = math.NewInt(inPlaceTestnetPower).Mul(chainApp.StakingKeeper.PowerReduction(ctx))
	validator.DelegatorShares = math.LegacyNewDecFromInt(validator.Tokens)
	validator.MinSelfDelegation = math.OneInt()

	if err := clearTestnetValidatorState(ctx, chainApp); err != nil {
		return fmt.Errorf("clear source validators: %w", err)
	}
	if err := chainApp.StakingKeeper.SetValidator(ctx, validator); err != nil {
		return err
	}
	if err := chainApp.StakingKeeper.SetValidatorByConsAddr(ctx, validator); err != nil {
		return err
	}
	if err := chainApp.StakingKeeper.SetValidatorByPowerIndex(ctx, validator); err != nil {
		return err
	}
	if err := chainApp.StakingKeeper.SetLastValidatorPower(ctx, valAddr, inPlaceTestnetPower); err != nil {
		return err
	}
	if err := chainApp.StakingKeeper.SetLastTotalPower(ctx, math.NewInt(inPlaceTestnetPower)); err != nil {
		return err
	}
	// Distribution's creation hook initializes historical/current rewards,
	// commission and outstanding rewards; slashing's hook registers the pubkey.
	if err := chainApp.StakingKeeper.Hooks().AfterValidatorCreated(ctx, valAddr); err != nil {
		return err
	}
	delegation := stakingtypes.NewDelegation(operator.String(), valAddr.String(), validator.DelegatorShares)
	if err := chainApp.StakingKeeper.SetDelegation(ctx, delegation); err != nil {
		return err
	}
	if err := chainApp.StakingKeeper.Hooks().AfterDelegationModified(ctx, operator, valAddr); err != nil {
		return err
	}
	consAddr := sdk.ConsAddress(newValAddr)
	if err := chainApp.SlashingKeeper.DeleteMissedBlockBitmap(ctx, consAddr); err != nil {
		return err
	}
	if err := chainApp.SlashingKeeper.SetValidatorSigningInfo(ctx, consAddr, slashingtypes.ValidatorSigningInfo{
		Address: consAddr.String(), StartHeight: chainApp.LastBlockHeight() - 1,
	}); err != nil {
		return err
	}

	bondDenom, err := chainApp.StakingKeeper.BondDenom(ctx)
	if err != nil {
		return err
	}
	if err := setTestnetPoolBalance(ctx, chainApp, stakingtypes.BondedPoolName, bondDenom, validator.Tokens); err != nil {
		return err
	}
	if err := setTestnetPoolBalance(ctx, chainApp, stakingtypes.NotBondedPoolName, bondDenom, math.ZeroInt()); err != nil {
		return err
	}
	if err := chainApp.POAKeeper.SetCachedBlockPower(ctx, uint64(inPlaceTestnetPower)); err != nil {
		return err
	}
	if err := chainApp.POAKeeper.SetAbsoluteChangedInBlockPower(ctx, 0); err != nil {
		return err
	}

	funds := sdk.NewCoins(sdk.NewInt64Coin(params.BondDenom, testnetOperatorFunds))
	if err := chainApp.BankKeeper.MintCoins(ctx, manifesttypes.ModuleName, funds); err != nil {
		return err
	}
	// This also creates the x/auth account required to sign rehearsal txs.
	if err := chainApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, manifesttypes.ModuleName, operator, funds); err != nil {
		return err
	}
	if upgradeToTrigger != "" {
		if err := chainApp.UpgradeKeeper.ScheduleUpgrade(ctx, upgradetypes.Plan{
			Name: upgradeToTrigger, Height: chainApp.LastBlockHeight() + testnetUpgradeDelay,
		}); err != nil {
			return fmt.Errorf("schedule testnet upgrade: %w", err)
		}
	}
	write()
	return nil
}

func clearTestnetValidatorState(ctx sdk.Context, chainApp *app.ManifestApp) error {
	// The removed validators' outstanding rewards remain backed by the
	// distribution account. Move the claims to the community pool before
	// deleting their reward histories and delegation reference counts.
	feePool, err := chainApp.DistrKeeper.FeePool.Get(ctx)
	if err != nil {
		return err
	}
	chainApp.DistrKeeper.IterateValidatorOutstandingRewards(ctx, func(_ sdk.ValAddress, rewards distrtypes.ValidatorOutstandingRewards) bool {
		feePool.CommunityPool = feePool.CommunityPool.Add(rewards.Rewards...)
		return false
	})
	if err := chainApp.DistrKeeper.FeePool.Set(ctx, feePool); err != nil {
		return err
	}
	distrStore := ctx.KVStore(chainApp.GetKey(distrtypes.StoreKey))
	for _, key := range [][]byte{
		distrtypes.ValidatorOutstandingRewardsPrefix, distrtypes.DelegatorStartingInfoPrefix,
		distrtypes.ValidatorHistoricalRewardsPrefix, distrtypes.ValidatorCurrentRewardsPrefix,
		distrtypes.ValidatorAccumulatedCommissionPrefix, distrtypes.ValidatorSlashEventPrefix,
	} {
		if err := deleteTestnetPrefix(distrStore, key); err != nil {
			return err
		}
	}
	store := ctx.KVStore(chainApp.GetKey(stakingtypes.StoreKey))
	// Delete entire prefixes, including orphaned and duplicate indices, rather
	// than walking only bonded validators. Delegations and their queues must
	// follow their removed validators, including POA's synthetic self stakes.
	for _, key := range [][]byte{
		stakingtypes.ValidatorsKey, stakingtypes.ValidatorsByConsAddrKey,
		stakingtypes.ValidatorsByPowerIndexKey, stakingtypes.LastValidatorPowerKey,
		stakingtypes.ValidatorQueueKey, stakingtypes.ValidatorUpdatesKey,
		stakingtypes.DelegationKey, stakingtypes.DelegationByValIndexKey,
		stakingtypes.UnbondingDelegationKey, stakingtypes.UnbondingDelegationByValIndexKey,
		stakingtypes.RedelegationKey, stakingtypes.RedelegationByValSrcIndexKey,
		stakingtypes.RedelegationByValDstIndexKey, stakingtypes.UnbondingIndexKey,
		stakingtypes.UnbondingTypeKey, stakingtypes.UnbondingQueueKey, stakingtypes.RedelegationQueueKey,
	} {
		if err := deleteTestnetPrefix(store, key); err != nil {
			return err
		}
	}
	if err := chainApp.POAKeeper.UpdatedValidatorsCache.Clear(ctx, nil); err != nil {
		return err
	}
	return chainApp.POAKeeper.PendingValidators.Set(ctx, poakeeper.DefaultPendingValidators())
}

func deleteTestnetPrefix(store storetypes.KVStore, prefix []byte) error {
	iterator := storetypes.KVStorePrefixIterator(store, prefix)
	var keys [][]byte
	for ; iterator.Valid(); iterator.Next() {
		keys = append(keys, bytes.Clone(iterator.Key()))
	}
	if err := iterator.Close(); err != nil {
		return err
	}
	for _, key := range keys {
		store.Delete(key)
	}
	return nil
}

// Adjust only the staking denomination, using bank APIs to keep supply and
// module balances consistent. All unrelated application balances remain intact.
func setTestnetPoolBalance(ctx sdk.Context, chainApp *app.ManifestApp, pool, denom string, target math.Int) error {
	address := chainApp.AccountKeeper.GetModuleAddress(pool)
	current := chainApp.BankKeeper.GetBalance(ctx, address, denom).Amount
	switch {
	case current.LT(target):
		coins := sdk.NewCoins(sdk.NewCoin(denom, target.Sub(current)))
		if err := chainApp.BankKeeper.MintCoins(ctx, manifesttypes.ModuleName, coins); err != nil {
			return err
		}
		return chainApp.BankKeeper.SendCoinsFromModuleToModule(ctx, manifesttypes.ModuleName, pool, coins)
	case current.GT(target):
		return chainApp.BankKeeper.BurnCoins(ctx, pool, sdk.NewCoins(sdk.NewCoin(denom, current.Sub(target))))
	default:
		return nil
	}
}
