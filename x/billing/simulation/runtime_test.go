package simulation_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	cmttypes "github.com/cometbft/cometbft/types"

	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktestutil "github.com/cosmos/cosmos-sdk/x/bank/testutil"

	"github.com/manifest-network/manifest-ledger/app"
	appparams "github.com/manifest-network/manifest-ledger/app/params"
	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	billingsimulation "github.com/manifest-network/manifest-ledger/x/billing/simulation"
	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
	skusimulation "github.com/manifest-network/manifest-ledger/x/sku/simulation"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

func simulationRuntimeFixture(t *testing.T) (sdk.Context, *app.ManifestApp, []simtypes.Account, skutypes.Provider, skutypes.SKU) {
	t.Helper()
	return simulationRuntimeFixtureWithAuthority(t, false)
}

func simulationRuntimeFixtureWithAuthority(t *testing.T, signableAuthority bool) (sdk.Context, *app.ManifestApp, []simtypes.Account, skutypes.Provider, skutypes.SKU) {
	t.Helper()
	appparams.SetAddressPrefixes()
	authority := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	if signableAuthority {
		key := secp256k1.GenPrivKeyFromSecret([]byte("billing-simulation-2"))
		authority = sdk.AccAddress(key.PubKey().Address())
	}
	t.Setenv("POA_ADMIN_ADDRESS", authority.String())
	manifestApp := app.NewApp(log.NewNopLogger(), dbm.NewMemDB(), nil, true,
		app.DefaultCommissionRateMinMax, simtestutil.AppOptionsMap{flags.FlagHome: t.TempDir()}, baseapp.SetChainID(app.SimAppChainID))
	t.Cleanup(func() { require.NoError(t, manifestApp.Close()) })
	validatorKey := cmted25519.GenPrivKeyFromSecret([]byte("billing-simulation-validator"))
	validatorSet := cmttypes.NewValidatorSet([]*cmttypes.Validator{cmttypes.NewValidator(validatorKey.PubKey(), 1)})
	genesisState, err := simtestutil.GenesisStateWithValSet(manifestApp.AppCodec(), manifestApp.DefaultGenesis(), validatorSet,
		[]authtypes.GenesisAccount{authtypes.NewBaseAccountWithAddress(sdk.AccAddress(bytes.Repeat([]byte{1}, 20)))})
	require.NoError(t, err)
	genesis, err := json.Marshal(genesisState)
	require.NoError(t, err)
	now := time.Unix(10_000, 0).UTC()
	_, err = manifestApp.InitChain(&abci.RequestInitChain{
		ChainId: app.SimAppChainID, InitialHeight: 1, Time: now,
		AppStateBytes: genesis, ConsensusParams: simtestutil.DefaultConsensusParams,
	})
	require.NoError(t, err)
	_, err = manifestApp.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 1, Time: now})
	require.NoError(t, err)
	ctx := manifestApp.GetContextForFinalizeBlock(nil)
	accountCount := 2
	if signableAuthority {
		accountCount++
	}
	accounts := make([]simtypes.Account, accountCount)
	for i := range accounts {
		key := secp256k1.GenPrivKeyFromSecret([]byte(fmt.Sprintf("billing-simulation-%d", i)))
		accounts[i] = simtypes.Account{PrivKey: key, PubKey: key.PubKey(), Address: sdk.AccAddress(key.PubKey().Address())}
		account := manifestApp.AccountKeeper.NewAccountWithAddress(ctx, accounts[i].Address)
		manifestApp.AccountKeeper.SetAccount(ctx, account)
	}
	params := billingtypes.DefaultParams()
	params.MinLeaseDuration = 1
	params.AllowedList = []string{accounts[0].Address.String()}
	require.NoError(t, manifestApp.BillingKeeper.SetParams(ctx, params))
	provider := skutypes.Provider{
		Uuid: "01912345-6789-7abc-8def-0123456789ab", Address: accounts[0].Address.String(),
		PayoutAddress: sdk.AccAddress(bytes.Repeat([]byte{9}, 20)).String(), Active: true,
	}
	require.NoError(t, manifestApp.SKUKeeper.SetProvider(ctx, provider))
	sku := skutypes.SKU{
		Uuid: "01912345-6789-7abc-8def-0123456789ac", ProviderUuid: provider.Uuid,
		Name: "Simulation SKU", Unit: skutypes.Unit_UNIT_PER_HOUR,
		BasePrice: sdk.NewInt64Coin("ucontinuation", 3600), Active: true,
	}
	require.NoError(t, manifestApp.SKUKeeper.SetSKU(ctx, sku))
	creditAddress := billingtypes.DeriveCreditAddress(accounts[1].Address)
	require.NoError(t, banktestutil.FundAccount(ctx, manifestApp.BankKeeper, creditAddress, sdk.NewCoins(sdk.NewInt64Coin("ucontinuation", 1_000_000))))
	require.NoError(t, manifestApp.BillingKeeper.SetCreditAccount(ctx, billingtypes.CreditAccount{Tenant: accounts[1].Address.String(), CreditAddress: creditAddress.String()}))
	require.NoError(t, banktestutil.FundAccount(ctx, manifestApp.BankKeeper, accounts[0].Address, sdk.NewCoins(sdk.NewInt64Coin(appparams.BondDenom, 100_000_000))))
	if signableAuthority {
		require.NoError(t, banktestutil.FundAccount(ctx, manifestApp.BankKeeper, authority, sdk.NewCoins(sdk.NewInt64Coin(appparams.BondDenom, 100_000_000))))
	}
	return ctx, manifestApp, accounts, provider, sku
}

func TestSimulationCreateLeaseForTenantDeliversAllowedSigner(t *testing.T) {
	ctx, manifestApp, accounts, _, _ := simulationRuntimeFixture(t)
	operation := billingsimulation.SimulateMsgCreateLeaseForTenant(manifestApp.TxConfig(), manifestApp.BillingKeeper, &manifestApp.SKUKeeper)
	op, future, err := operation(rand.New(rand.NewSource(1)), manifestApp.BaseApp, ctx, accounts, ctx.ChainID()) //nolint:gosec // deterministic simulation PRNG
	require.NoError(t, err)
	require.True(t, op.OK, op.Comment)
	require.Empty(t, future)
	var message billingtypes.MsgCreateLeaseForTenant
	require.NoError(t, message.Unmarshal(op.Msg))
	require.Equal(t, accounts[0].Address.String(), message.Authority)
	require.Equal(t, accounts[1].Address.String(), message.Tenant)
	leases, err := manifestApp.BillingKeeper.GetAllLeases(ctx)
	require.NoError(t, err)
	require.Len(t, leases, 1)
	require.Equal(t, billingtypes.LEASE_STATE_PENDING, leases[0].State)
	require.Equal(t, message.Tenant, leases[0].Tenant)
	require.NotEmpty(t, leases[0].Reservation.RemainingAmounts)
}

func TestSimulationProviderWithdrawalResumesAfterClosedCursor(t *testing.T) {
	ctx, manifestApp, accounts, provider, sku := simulationRuntimeFixture(t)
	k := manifestApp.BillingKeeper
	startedAt := ctx.BlockTime().Add(-10 * time.Second)
	ids := make([]string, 3)
	for i := range ids {
		ids[i] = fmt.Sprintf("01912345-6789-7abc-8def-%012d", i+1)
		require.NoError(t, k.SetLease(ctx, billingtypes.Lease{
			Uuid: ids[i], Tenant: accounts[1].Address.String(), ProviderUuid: provider.Uuid,
			Items: []billingtypes.LeaseItem{{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewInt64Coin("ucontinuation", 1)}},
			State: billingtypes.LEASE_STATE_ACTIVE, CreatedAt: startedAt, LastSettledAt: startedAt,
			MinLeaseDurationAtCreation: 1,
			Reservation:                &billingtypes.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
		}))
	}
	account, err := k.GetCreditAccount(ctx, accounts[1].Address.String())
	require.NoError(t, err)
	account.ActiveLeaseCount = 3
	require.NoError(t, k.SetCreditAccount(ctx, account))
	operation := billingsimulation.SimulateMsgWithdraw(manifestApp.TxConfig(), k, &manifestApp.SKUKeeper)
	// Seed 3 selects provider-wide mode and a one-lease page.
	op, future, err := operation(rand.New(rand.NewSource(3)), manifestApp.BaseApp, ctx, accounts, ctx.ChainID()) //nolint:gosec // deterministic simulation PRNG
	require.NoError(t, err)
	require.True(t, op.OK, op.Comment)
	var first billingtypes.MsgWithdraw
	require.NoError(t, first.Unmarshal(op.Msg))
	require.Equal(t, provider.Uuid, first.ProviderUuid)
	require.Equal(t, uint64(1), first.Limit)
	require.Empty(t, first.Key)
	require.Len(t, future, 1)
	firstLease, err := k.GetLease(ctx, ids[0])
	require.NoError(t, err)
	require.Equal(t, ctx.BlockTime(), firstLease.LastSettledAt)
	_, err = keeper.NewMsgServerImpl(k).CloseLease(ctx, &billingtypes.MsgCloseLease{Sender: accounts[1].Address.String(), LeaseUuids: []string{ids[0]}})
	require.NoError(t, err)
	// Rotate ownership while the first continuation is queued.
	provider.Address = accounts[1].Address.String()
	require.NoError(t, manifestApp.SKUKeeper.SetProvider(ctx, provider))
	require.NoError(t, banktestutil.FundAccount(ctx, manifestApp.BankKeeper, accounts[1].Address, sdk.NewCoins(sdk.NewInt64Coin(appparams.BondDenom, 100_000_000))))
	for index := 1; index < len(ids); index++ {
		require.Len(t, future, 1)
		next := future[0]
		require.Equal(t, int(ctx.BlockHeight()+1), next.BlockHeight) //nolint:gosec // bounded test height
		require.Zero(t, next.BlockTime)
		nextTime := ctx.BlockTime().Add(time.Second)
		// SimDeliver runs after FinalizeBlock's working-hash flush. Flush its
		// additional cached writes before committing this synthetic block.
		ctx.MultiStore().(storetypes.CacheMultiStore).Write()
		_, err = manifestApp.Commit()
		require.NoError(t, err)
		_, err = manifestApp.FinalizeBlock(&abci.RequestFinalizeBlock{Height: manifestApp.LastBlockHeight() + 1, Time: nextTime})
		require.NoError(t, err)
		ctx = manifestApp.GetContextForFinalizeBlock(nil)
		op, future, err = next.Op(rand.New(rand.NewSource(int64(index))), manifestApp.BaseApp, ctx, accounts, ctx.ChainID()) //nolint:gosec // deterministic simulation PRNG
		require.NoError(t, err)
		require.True(t, op.OK, op.Comment)
		var resumed billingtypes.MsgWithdraw
		require.NoError(t, resumed.Unmarshal(op.Msg))
		require.Equal(t, []byte(ids[index-1]), resumed.Key)
		require.Equal(t, accounts[1].Address.String(), resumed.Sender)
		current, err := k.GetLease(ctx, ids[index])
		require.NoError(t, err)
		require.Equal(t, ctx.BlockTime(), current.LastSettledAt)
	}
	require.Empty(t, future)
	payout := sdk.MustAccAddressFromBech32(provider.PayoutAddress)
	require.Equal(t, sdk.NewInt64Coin("ucontinuation", 33), manifestApp.BankKeeper.GetBalance(ctx, payout, "ucontinuation"))
}

func TestSimulationCloseLeaseDeliversAllAuthorizedSigners(t *testing.T) {
	for _, signerIndex := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("signer %d", signerIndex), func(t *testing.T) {
			ctx, manifestApp, accounts, _, sku := simulationRuntimeFixtureWithAuthority(t, true)
			ms := keeper.NewMsgServerImpl(manifestApp.BillingKeeper)
			created, err := ms.CreateLease(ctx, &billingtypes.MsgCreateLease{
				Tenant: accounts[1].Address.String(), Items: []billingtypes.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
			})
			require.NoError(t, err)
			_, err = ms.AcknowledgeLease(ctx, &billingtypes.MsgAcknowledgeLease{Sender: accounts[0].Address.String(), LeaseUuids: []string{created.LeaseUuid}})
			require.NoError(t, err)
			require.NoError(t, banktestutil.FundAccount(ctx, manifestApp.BankKeeper, accounts[1].Address, sdk.NewCoins(sdk.NewInt64Coin(appparams.BondDenom, 100_000_000))))
			operation := billingsimulation.SimulateMsgCloseLease(manifestApp.TxConfig(), manifestApp.BillingKeeper, &manifestApp.SKUKeeper)
			op, future, err := operation(rand.New(rand.NewSource(1)), manifestApp.BaseApp, ctx, []simtypes.Account{accounts[signerIndex]}, ctx.ChainID()) //nolint:gosec // deterministic simulation PRNG
			require.NoError(t, err)
			require.True(t, op.OK, op.Comment)
			require.Empty(t, future)
			var message billingtypes.MsgCloseLease
			require.NoError(t, message.Unmarshal(op.Msg))
			require.Equal(t, accounts[signerIndex].Address.String(), message.Sender)
			lease, err := manifestApp.BillingKeeper.GetLease(ctx, created.LeaseUuid)
			require.NoError(t, err)
			require.Equal(t, billingtypes.LEASE_STATE_CLOSED, lease.State)
		})
	}
}

func TestSimulationUpdateParamsDeliversOnlyConfiguredAuthority(t *testing.T) {
	for _, signableAuthority := range []bool{false, true} {
		for _, module := range []string{billingtypes.ModuleName, skutypes.ModuleName} {
			t.Run(fmt.Sprintf("%s signable authority %t", module, signableAuthority), func(t *testing.T) {
				ctx, manifestApp, accounts, _, _ := simulationRuntimeFixtureWithAuthority(t, signableAuthority)
				current, err := manifestApp.BillingKeeper.GetParams(ctx)
				require.NoError(t, err)
				current.ReservedDomainSuffixes = []string{".preserved.example"}
				require.NoError(t, manifestApp.BillingKeeper.SetParams(ctx, current))
				// The account in the delegated lists must not impersonate authority
				// when the configured authority has no key in this imported state.
				skuCurrent := skutypes.Params{AllowedList: []string{accounts[0].Address.String()}}
				require.NoError(t, manifestApp.SKUKeeper.SetParams(ctx, skuCurrent))
				operation := billingsimulation.SimulateMsgUpdateParams(manifestApp.TxConfig(), manifestApp.BillingKeeper)
				if module == skutypes.ModuleName {
					operation = skusimulation.SimulateMsgUpdateParams(manifestApp.TxConfig(), manifestApp.SKUKeeper)
				}
				op, future, err := operation(rand.New(rand.NewSource(1)), manifestApp.BaseApp, ctx, accounts, ctx.ChainID()) //nolint:gosec // deterministic simulation PRNG
				require.NoError(t, err)
				require.Empty(t, future)
				billingAfter, err := manifestApp.BillingKeeper.GetParams(ctx)
				require.NoError(t, err)
				skuAfter, err := manifestApp.SKUKeeper.GetParams(ctx)
				require.NoError(t, err)
				if !signableAuthority {
					require.False(t, op.OK)
					require.Equal(t, "authority account not found in simulation", op.Comment)
					require.Equal(t, current, billingAfter)
					require.Equal(t, skuCurrent, skuAfter)
					return
				}
				require.True(t, op.OK, op.Comment)
				if module == billingtypes.ModuleName {
					var message billingtypes.MsgUpdateParams
					require.NoError(t, message.Unmarshal(op.Msg))
					require.Equal(t, manifestApp.BillingKeeper.GetAuthority(), message.Authority)
					require.Equal(t, message.Params, billingAfter)
					require.Equal(t, current.AllowedList, billingAfter.AllowedList)
					require.Equal(t, current.ReservedDomainSuffixes, billingAfter.ReservedDomainSuffixes)
					require.NotEqual(t, current.MinLeaseDuration, billingAfter.MinLeaseDuration)
				} else {
					var message skutypes.MsgUpdateParams
					require.NoError(t, message.Unmarshal(op.Msg))
					require.Equal(t, manifestApp.SKUKeeper.GetAuthority(), message.Authority)
					require.Equal(t, message.Params, skuAfter)
				}
				require.NoError(t, billingAfter.Validate())
				require.NoError(t, skuAfter.Validate())
			})
		}
	}
}
