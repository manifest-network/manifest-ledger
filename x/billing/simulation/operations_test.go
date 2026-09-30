package simulation_test

import (
	"encoding/json"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"

	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/manifest-network/manifest-ledger/app"
	appparams "github.com/manifest-network/manifest-ledger/app/params"
	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/simulation"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

type leaseFixture struct {
	app    *app.ManifestApp
	ctx    sdk.Context
	tenant simtypes.Account
	sku    skutypes.SKU
}

func newLeaseFixture(t *testing.T, active, pending uint64) leaseFixture {
	t.Helper()
	appparams.SetAddressPrefixes()
	key := secp256k1.GenPrivKey()
	tenant := simtypes.Account{PrivKey: key, PubKey: key.PubKey(), Address: sdk.AccAddress(key.PubKey().Address())}
	t.Setenv("POA_ADMIN_ADDRESS", tenant.Address.String())
	opts := simtestutil.AppOptionsMap{flags.FlagHome: t.TempDir()}
	chain := app.NewApp(log.NewNopLogger(), dbm.NewMemDB(), nil, true, app.DefaultCommissionRateMinMax, opts, baseapp.SetChainID("billing-simulation"))
	t.Cleanup(func() { require.NoError(t, chain.Close()) })
	genesis := chain.DefaultGenesis()
	validator, err := stakingtypes.NewValidator(sdk.ValAddress(tenant.Address).String(), ed25519.GenPrivKey().PubKey(), stakingtypes.Description{})
	require.NoError(t, err)
	validator.Status = stakingtypes.Bonded
	validator.Tokens = sdk.DefaultPowerReduction
	validator.DelegatorShares = sdkmath.LegacyNewDecFromInt(validator.Tokens)
	genesis[stakingtypes.ModuleName] = chain.AppCodec().MustMarshalJSON(stakingtypes.NewGenesisState(stakingtypes.DefaultParams(),
		[]stakingtypes.Validator{validator}, []stakingtypes.Delegation{stakingtypes.NewDelegation(tenant.Address.String(), validator.OperatorAddress, validator.DelegatorShares)}))
	genesis[authtypes.ModuleName] = chain.AppCodec().MustMarshalJSON(authtypes.NewGenesisState(authtypes.DefaultParams(),
		[]authtypes.GenesisAccount{authtypes.NewBaseAccount(tenant.Address, tenant.PubKey, 0, 0)}))
	bondedCoins := sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, validator.Tokens))
	genesis[banktypes.ModuleName] = chain.AppCodec().MustMarshalJSON(banktypes.NewGenesisState(banktypes.DefaultParams(),
		[]banktypes.Balance{{Address: authtypes.NewModuleAddress(stakingtypes.BondedPoolName).String(), Coins: bondedCoins}}, bondedCoins, nil, nil))
	genesisJSON, err := json.Marshal(genesis)
	require.NoError(t, err)
	_, err = chain.InitChain(&abci.RequestInitChain{
		ChainId: "billing-simulation", InitialHeight: 1, Time: time.Unix(1700000000, 0),
		AppStateBytes: genesisJSON, ConsensusParams: simtestutil.DefaultConsensusParams,
	})
	require.NoError(t, err)
	ctx := chain.GetContextForFinalizeBlock(nil)
	chain.BillingKeeper.SetAuthority(tenant.Address.String())
	params := types.DefaultParams()
	params.MaxLeasesPerTenant = 7
	params.MaxPendingLeasesPerTenant = 6
	require.NoError(t, chain.BillingKeeper.SetParams(ctx, params))
	creditAddr := types.DeriveCreditAddress(tenant.Address)
	// Seed the canonical counters independently of lease indexes: creation must
	// use the same source of truth as the keeper's message handlers.
	require.NoError(t, chain.BillingKeeper.SetCreditAccount(ctx, types.CreditAccount{
		Tenant: tenant.Address.String(), CreditAddress: creditAddr.String(),
		ActiveLeaseCount: active, PendingLeaseCount: pending,
	}))
	for _, addr := range []sdk.AccAddress{tenant.Address, creditAddr} {
		coins := sdk.NewCoins(sdk.NewInt64Coin(appparams.BondDenom, 1_000_000_000))
		require.NoError(t, chain.BankKeeper.MintCoins(ctx, "mint", coins))
		require.NoError(t, chain.BankKeeper.SendCoinsFromModuleToAccount(ctx, "mint", addr, coins))
	}
	providerID, err := chain.SKUKeeper.GenerateProviderUUID(ctx)
	require.NoError(t, err)
	require.NoError(t, chain.SKUKeeper.SetProvider(ctx, skutypes.Provider{
		Uuid: providerID, Address: tenant.Address.String(), PayoutAddress: tenant.Address.String(), Active: true,
	}))
	skuID, err := chain.SKUKeeper.GenerateSKUUUID(ctx)
	require.NoError(t, err)
	sku := skutypes.SKU{
		Uuid: skuID, ProviderUuid: providerID, Name: "simulation", Active: true,
		Unit: skutypes.Unit_UNIT_PER_HOUR, BasePrice: sdk.NewInt64Coin(appparams.BondDenom, 3600),
	}
	require.NoError(t, chain.SKUKeeper.SetSKU(ctx, sku))
	return leaseFixture{app: chain, ctx: ctx, tenant: tenant, sku: sku}
}

func (f leaseFixture) operation(forTenant bool) simtypes.Operation {
	if forTenant {
		return simulation.SimulateMsgCreateLeaseForTenant(f.app.TxConfig(), f.app.BillingKeeper, &f.app.SKUKeeper)
	}
	return simulation.SimulateMsgCreateLease(f.app.TxConfig(), f.app.BillingKeeper, &f.app.SKUKeeper)
}

func TestSimulateLeaseCreationLimits(t *testing.T) {
	for _, operation := range []struct {
		name      string
		forTenant bool
	}{{"tenant", false}, {"authority", true}} {
		t.Run(operation.name, func(t *testing.T) {
			for _, tc := range []struct {
				name            string
				active, pending uint64
				comment         string
				wantErr         error
			}{
				{"active_at_limit", 7, 0, "tenant at max lease limit", types.ErrMaxLeasesReached},
				{"active_above_limit", 8, 0, "tenant at max lease limit", types.ErrMaxLeasesReached},
				{"pending_at_limit", 0, 6, "tenant at max pending lease limit", types.ErrMaxPendingLeasesReached},
				{"pending_above_limit", 0, 7, "tenant at max pending lease limit", types.ErrMaxPendingLeasesReached},
			} {
				t.Run(tc.name, func(t *testing.T) {
					f := newLeaseFixture(t, tc.active, tc.pending)
					before := f.app.BillingKeeper.ExportGenesis(f.ctx)
					balance := f.app.BankKeeper.GetAllBalances(f.ctx, f.tenant.Address)
					// Confirm the actual handler rejects the same canonical counters.
					cache, _ := f.ctx.CacheContext()
					server := keeper.NewMsgServerImpl(f.app.BillingKeeper)
					items := []types.LeaseItemInput{{SkuUuid: f.sku.Uuid, Quantity: 1}}
					var err error
					if operation.forTenant {
						_, err = server.CreateLeaseForTenant(cache, &types.MsgCreateLeaseForTenant{
							Authority: f.tenant.Address.String(), Tenant: f.tenant.Address.String(), Items: items,
						})
					} else {
						_, err = server.CreateLease(cache, &types.MsgCreateLease{Tenant: f.tenant.Address.String(), Items: items})
					}
					require.ErrorIs(t, err, tc.wantErr)
					msg, future, err := f.operation(operation.forTenant)(rand.New(rand.NewSource(1)), f.app.BaseApp, f.ctx, []simtypes.Account{f.tenant}, f.ctx.ChainID())
					require.NoError(t, err)
					require.False(t, msg.OK)
					require.Equal(t, tc.comment, msg.Comment)
					require.Empty(t, future)
					after := f.app.BillingKeeper.ExportGenesis(f.ctx)
					require.Equal(t, before, after)
					require.Equal(t, balance, f.app.BankKeeper.GetAllBalances(f.ctx, f.tenant.Address))
				})
			}
			t.Run("last_available_slot", func(t *testing.T) {
				f := newLeaseFixture(t, 6, 5)
				rng := rand.New(rand.NewSource(1))
				op := f.operation(operation.forTenant)
				msg, _, err := op(rng, f.app.BaseApp, f.ctx, []simtypes.Account{f.tenant}, f.ctx.ChainID())
				require.NoError(t, err)
				require.True(t, msg.OK, msg.Comment)
				account, err := f.app.BillingKeeper.GetCreditAccount(f.ctx, f.tenant.Address.String())
				require.NoError(t, err)
				require.Equal(t, uint64(6), account.ActiveLeaseCount)
				require.Equal(t, uint64(6), account.PendingLeaseCount)
				leases, err := f.app.BillingKeeper.GetAllLeases(f.ctx)
				require.NoError(t, err)
				require.Len(t, leases, 1)
				msg, _, err = op(rng, f.app.BaseApp, f.ctx, []simtypes.Account{f.tenant}, f.ctx.ChainID())
				require.NoError(t, err)
				require.False(t, msg.OK)
				require.Equal(t, "tenant at max pending lease limit", msg.Comment)
			})
		})
	}
}
