package app

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"
	tmed25519 "github.com/cometbft/cometbft/crypto/ed25519"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	tmtypes "github.com/cometbft/cometbft/types"

	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log"
	"cosmossdk.io/store/rootmulti"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/testutil/sims"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	appparams "github.com/manifest-network/manifest-ledger/app/params"
	billingkeeper "github.com/manifest-network/manifest-ledger/x/billing/keeper"
	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

func TestZeroHeightExportRetainsBillingBlockTime(t *testing.T) {
	for _, mode := range []string{"in process", "reopened latest", "reopened historical height"} {
		t.Run(mode, func(t *testing.T) {
			manifest, _, sourceTime := setupBillingExportApp(t, mode)
			source := manifest.BillingKeeper.ExportGenesis(manifest.GetContextForCheckTx(nil))
			require.Len(t, source.Leases, 3, "exercise pending, active, and closed billing state")
			exported, err := manifest.ExportAppStateAndValidators(true, nil, nil)
			require.NoError(t, err)
			require.Zero(t, exported.Height)
			var state GenesisState
			require.NoError(t, json.Unmarshal(exported.AppState, &state))
			var billing billingtypes.GenesisState
			require.NoError(t, manifest.AppCodec().UnmarshalJSON(state[billingtypes.ModuleName], &billing))
			require.Equal(t, source.String(), billing.String(), "zero-height preparation must preserve billing timestamps and accounting")
			require.NoError(t, billing.ValidateWithBlockTime(sourceTime))

			// The SDK export command retains the original genesis_time in its
			// outer JSON document. A restart must explicitly use the source block
			// time or later, because zero-height export does not rewind leases.
			imported := newBillingExportApp(t, dbm.NewMemDB(), true)
			_, err = imported.InitChain(&abci.RequestInitChain{
				ChainId: SimAppChainID, InitialHeight: 1, Time: sourceTime,
				ConsensusParams: &exported.ConsensusParams, AppStateBytes: exported.AppState,
			})
			require.NoError(t, err)
			restored := imported.BillingKeeper.ExportGenesis(imported.GetContextForFinalizeBlock(nil))
			require.Equal(t, billing.String(), restored.String())
		})
	}
}

func TestZeroHeightExportStillRejectsFutureBillingTimestamp(t *testing.T) {
	manifest, _, sourceTime := setupBillingExportApp(t, "reopened historical height")
	ctx := manifest.GetContextForCheckTx(nil)
	genesis := manifest.BillingKeeper.ExportGenesis(ctx)
	lease := genesis.Leases[0]
	// The next committed height has this timestamp. Reading the latest commit
	// instead of the selected export height would incorrectly accept it.
	lease.LastSettledAt = sourceTime.Add(10 * time.Second)
	if lease.State == billingtypes.LEASE_STATE_CLOSED {
		lease.ClosedAt = &lease.LastSettledAt
	}
	require.NoError(t, manifest.BillingKeeper.SetLease(ctx, lease))
	require.NoError(t, manifest.BillingKeeper.ExportGenesis(ctx).ValidateCurrentState())
	requireBillingExportPanic(t, "invalid billing timestamps", func() {
		_, _ = manifest.ExportAppStateAndValidators(true, nil, nil)
	})
}

func TestZeroHeightBillingImportRequiresUpdatedGenesisTime(t *testing.T) {
	manifest, originalTime, _ := setupBillingExportApp(t, "reopened latest")
	exported, err := manifest.ExportAppStateAndValidators(true, nil, nil)
	require.NoError(t, err)
	var state GenesisState
	require.NoError(t, json.Unmarshal(exported.AppState, &state))
	var billing billingtypes.GenesisState
	require.NoError(t, manifest.AppCodec().UnmarshalJSON(state[billingtypes.ModuleName], &billing))
	require.ErrorContains(t, billing.ValidateWithBlockTime(originalTime), "in the future relative to block time")
	imported := newBillingExportApp(t, dbm.NewMemDB(), true)
	// Rewinding time also relocks this fixture's credit. InitGenesis detects
	// that backing failure before it reaches the independent timestamp check.
	requireBillingExportPanic(t, "below reservation", func() {
		_, _ = imported.InitChain(&abci.RequestInitChain{
			ChainId: SimAppChainID, InitialHeight: 1, Time: originalTime,
			ConsensusParams: &exported.ConsensusParams, AppStateBytes: exported.AppState,
		})
	})
}

func TestZeroHeightExportRequiresSourceTimeAfterRollback(t *testing.T) {
	manifest, _, _ := setupBillingExportApp(t, "reopened rollback")
	store, ok := manifest.CommitMultiStore().(*rootmulti.Store)
	require.True(t, ok)
	commitInfo, err := store.GetCommitInfo(manifest.LastBlockHeight())
	require.NoError(t, err)
	require.True(t, commitInfo.Timestamp.IsZero(), "pinned SDK rollback reconstructs metadata without its timestamp")
	_, err = manifest.ExportAppStateAndValidators(true, nil, nil)
	require.ErrorContains(t, err, "source block time is unavailable")
	require.ErrorContains(t, err, "snapshot restore or rollback")
	_, err = manifest.ExportAppStateAndValidators(false, nil, nil)
	require.NoError(t, err, "ordinary export remains available without guessing a valuation time")
}

func TestZeroHeightExportRejectsStaleCheckTxTimeAfterRollback(t *testing.T) {
	manifest, _, laterTime := setupBillingExportApp(t, "in process")
	checkCtx := manifest.GetContextForCheckTx(nil)
	require.Equal(t, int64(3), checkCtx.BlockHeight())
	require.Equal(t, laterTime, checkCtx.BlockTime())
	store, ok := manifest.CommitMultiStore().(*rootmulti.Store)
	require.True(t, ok)
	sourceInfo, err := store.GetCommitInfo(2)
	require.NoError(t, err)
	require.True(t, sourceInfo.Timestamp.Before(laterTime))

	require.NoError(t, store.RollbackToVersion(2))
	require.Equal(t, int64(2), manifest.LastBlockHeight())
	require.Equal(t, int64(3), manifest.GetContextForCheckTx(nil).BlockHeight(), "rollback does not refresh BaseApp's CheckTx header")
	rolledBackInfo, err := store.GetCommitInfo(2)
	require.NoError(t, err)
	require.True(t, rolledBackInfo.Timestamp.IsZero(), "rollback also removes the matching commit timestamp")

	// A timestamp accepted by the stale header must not make the selected
	// height's export look valid. Keep structural/accounting checks satisfied
	// so only the missing source clock separates this state from acceptance.
	genesis := manifest.BillingKeeper.ExportGenesis(checkCtx)
	lease := genesis.Leases[0]
	lease.LastSettledAt = laterTime
	if lease.State == billingtypes.LEASE_STATE_CLOSED {
		lease.ClosedAt = &lease.LastSettledAt
	}
	require.NoError(t, manifest.BillingKeeper.SetLease(checkCtx, lease))
	genesis = manifest.BillingKeeper.ExportGenesis(checkCtx)
	require.NoError(t, genesis.ValidateCurrentState())
	require.NoError(t, genesis.ValidateWithBlockTime(laterTime))
	require.ErrorContains(t, genesis.ValidateWithBlockTime(sourceInfo.Timestamp), "in the future relative to block time")
	before := genesis.String()
	_, err = manifest.ExportAppStateAndValidators(true, nil, nil)
	require.ErrorContains(t, err, "height 2: source block time is unavailable")
	require.Equal(t, before, manifest.BillingKeeper.ExportGenesis(checkCtx).String())
}

func requireBillingExportPanic(t *testing.T, expected string, operation func()) {
	t.Helper()
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		operation()
	}()
	require.Contains(t, fmt.Sprint(recovered), expected)
}

func newBillingExportApp(t *testing.T, db dbm.DB, loadLatest bool) *ManifestApp {
	t.Helper()
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return NewApp(log.NewNopLogger(), db, nil, loadLatest, DefaultCommissionRateMinMax,
		sims.NewAppOptionsWithFlagHome(t.TempDir()), baseapp.SetChainID(SimAppChainID))
}

func setupBillingExportApp(t *testing.T, mode string) (*ManifestApp, time.Time, time.Time) {
	t.Helper()
	appparams.SetAddressPrefixes()
	directory := t.TempDir()
	db, err := dbm.NewDB("billing-export", dbm.GoLevelDBBackend, directory)
	require.NoError(t, err)
	// This database is explicitly closed before reopening, unlike the fresh
	// import databases managed by newBillingExportApp.
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	manifest := NewApp(log.NewNopLogger(), db, nil, true, DefaultCommissionRateMinMax,
		sims.NewAppOptionsWithFlagHome(t.TempDir()), baseapp.SetChainID(SimAppChainID))
	validatorKey := tmed25519.GenPrivKey()
	validator := tmtypes.NewValidator(validatorKey.PubKey(), 1)
	owner := authtypes.NewBaseAccountWithAddress(sdk.AccAddress(validatorKey.PubKey().Address()))
	_, _, tenant := testdata.KeyTestPubAddr()
	_, _, providerAddress := testdata.KeyTestPubAddr()
	creditAddress := billingtypes.DeriveCreditAddress(tenant)
	originalTime := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	leaseTime := originalTime.Add(time.Hour)
	creditBalance := sdk.NewCoins(sdk.NewInt64Coin("umfx", 1_000_000))
	// All credit is locked at genesis and becomes spendable at the source
	// lease block. Export must use that block's time for reservation backing;
	// merely skipping the timestamp invariant at zero time would still fail.
	vestingCredit, err := vestingtypes.NewDelayedVestingAccount(
		authtypes.NewBaseAccountWithAddress(creditAddress), creditBalance, leaseTime.Unix(),
	)
	require.NoError(t, err)
	state := genesisStateWithValSet(t, manifest, manifest.DefaultGenesis(),
		tmtypes.NewValidatorSet([]*tmtypes.Validator{validator}), []authtypes.GenesisAccount{owner, vestingCredit},
		banktypes.Balance{Address: creditAddress.String(), Coins: creditBalance})
	const providerUUID = "01912345-6789-7abc-8def-0123456789ad"
	const skuUUID = "01912345-6789-7abc-8def-0123456789ae"
	state[skutypes.ModuleName] = manifest.AppCodec().MustMarshalJSON(skutypes.NewGenesisState(
		skutypes.DefaultParams(),
		[]skutypes.Provider{{Uuid: providerUUID, Address: providerAddress.String(), PayoutAddress: providerAddress.String(), Active: true}},
		[]skutypes.SKU{{Uuid: skuUUID, ProviderUuid: providerUUID, Name: "export", Unit: skutypes.Unit_UNIT_PER_HOUR, BasePrice: sdk.NewInt64Coin("umfx", 3600), Active: true}},
		1, 1,
	))
	state[billingtypes.ModuleName] = manifest.AppCodec().MustMarshalJSON(billingtypes.NewGenesisState(
		billingtypes.DefaultParams(), nil,
		[]billingtypes.CreditAccount{{Tenant: tenant.String(), CreditAddress: creditAddress.String()}}, 0,
	))
	genesis, err := json.Marshal(state)
	require.NoError(t, err)
	_, err = manifest.InitChain(&abci.RequestInitChain{
		ChainId: SimAppChainID, InitialHeight: 1, Time: originalTime,
		ConsensusParams: DefaultConsensusParams, AppStateBytes: genesis,
	})
	require.NoError(t, err)
	_, err = manifest.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 1, Time: originalTime})
	require.NoError(t, err)
	_, err = manifest.Commit()
	require.NoError(t, err)

	require.True(t, manifest.BankKeeper.SpendableCoins(manifest.GetContextForCheckTx(nil), creditAddress).IsZero())
	ctx := manifest.NewUncachedContext(false, tmproto.Header{ChainID: SimAppChainID, Height: 2, Time: leaseTime})
	require.Equal(t, creditBalance, manifest.BankKeeper.SpendableCoins(ctx, creditAddress))
	server := billingkeeper.NewMsgServerImpl(manifest.BillingKeeper)
	for _, state := range []billingtypes.LeaseState{billingtypes.LEASE_STATE_PENDING, billingtypes.LEASE_STATE_ACTIVE, billingtypes.LEASE_STATE_CLOSED} {
		created, err := server.CreateLease(ctx, &billingtypes.MsgCreateLease{
			Tenant: tenant.String(), Items: []billingtypes.LeaseItemInput{{SkuUuid: skuUUID, Quantity: 1}},
		})
		require.NoError(t, err)
		if state != billingtypes.LEASE_STATE_PENDING {
			_, err = server.AcknowledgeLease(ctx, &billingtypes.MsgAcknowledgeLease{Sender: providerAddress.String(), LeaseUuids: []string{created.LeaseUuid}})
			require.NoError(t, err)
		}
		if state == billingtypes.LEASE_STATE_CLOSED {
			_, err = server.CloseLease(ctx, &billingtypes.MsgCloseLease{Sender: tenant.String(), LeaseUuids: []string{created.LeaseUuid}})
			require.NoError(t, err)
		}
	}
	for height := int64(2); height <= 3; height++ {
		_, err = manifest.FinalizeBlock(&abci.RequestFinalizeBlock{Height: height, Time: leaseTime.Add(time.Duration(height-2) * 10 * time.Second)})
		require.NoError(t, err)
		_, err = manifest.Commit()
		require.NoError(t, err)
	}
	sourceTime := leaseTime.Add(10 * time.Second)
	if mode != "in process" {
		if mode == "reopened rollback" {
			store, ok := manifest.CommitMultiStore().(*rootmulti.Store)
			require.True(t, ok)
			require.NoError(t, store.RollbackToVersion(2))
			sourceTime = leaseTime
		}
		require.NoError(t, db.Close())
		db, err = dbm.NewDB("billing-export", dbm.GoLevelDBBackend, directory)
		require.NoError(t, err)
		manifest = NewApp(log.NewNopLogger(), db, nil, mode != "reopened historical height", DefaultCommissionRateMinMax,
			sims.NewAppOptionsWithFlagHome(t.TempDir()), baseapp.SetChainID(SimAppChainID))
		if mode == "reopened historical height" {
			require.NoError(t, manifest.LoadHeight(2))
			sourceTime = leaseTime
		}
		require.True(t, manifest.GetContextForCheckTx(nil).BlockTime().IsZero(), "reopening must exercise absent in-memory header time")
	}
	return manifest, originalTime, sourceTime
}
