package app

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"
	tmed25519 "github.com/cometbft/cometbft/crypto/ed25519"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	tmtypes "github.com/cometbft/cometbft/types"

	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	circuittypes "cosmossdk.io/x/circuit/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/testutil/sims"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	crisistypes "github.com/cosmos/cosmos-sdk/x/crisis/types"

	appparams "github.com/manifest-network/manifest-ledger/app/params"
	billingkeeper "github.com/manifest-network/manifest-ledger/x/billing/keeper"
	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestCrisisFeeRollsBackAfterFailingMessageButAnteFeeRemains(t *testing.T) {
	for _, test := range []struct {
		name        string
		failingTail bool
		anteFee     int64
	}{
		{"successful verification pays crisis and ante fees", false, 17},
		{"failed tail refunds crisis fee only", true, 17},
		{"failed tail with zero ante fee costs no tokens", true, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, manifest := setupCrisisApp(t)
			privateKey, _, sender := testdata.KeyTestPubAddr()
			_, _, recipient := testdata.KeyTestPubAddr()
			account := manifest.AccountKeeper.NewAccountWithAddress(ctx, sender)
			manifest.AccountKeeper.SetAccount(ctx, account)
			initial := sdk.NewInt64Coin("umfx", 1_000_000)
			crisisFee := sdk.NewInt64Coin("umfx", 1_000)
			require.NoError(t, manifest.BankKeeper.MintCoins(ctx, "mint", sdk.NewCoins(initial)))
			require.NoError(t, manifest.BankKeeper.SendCoinsFromModuleToAccount(ctx, "mint", sender, sdk.NewCoins(initial)))
			require.NoError(t, manifest.CrisisKeeper.ConstantFee.Set(ctx, crisisFee))
			messages := []sdk.Msg{&crisistypes.MsgVerifyInvariant{
				Sender: sender.String(), InvariantModuleName: "billing", InvariantRoute: "reservation-accounting",
			}}
			if test.failingTail {
				// This is valid at the signer/ante boundary, but cannot execute
				// after the first message has deducted its crisis constant fee.
				messages = append(messages, &banktypes.MsgSend{
					FromAddress: sender.String(), ToAddress: recipient.String(), Amount: sdk.NewCoins(initial),
				})
			}
			fees := sdk.NewCoins()
			if test.anteFee > 0 {
				fees = sdk.NewCoins(sdk.NewInt64Coin("umfx", test.anteFee))
			}
			tx, err := sims.GenSignedMockTx(
				rand.New(rand.NewSource(1)), //nolint:gosec // deterministic transaction memo in an integration test
				manifest.TxConfig(), messages, fees, 1_000_000, SimAppChainID,
				[]uint64{account.GetAccountNumber()}, []uint64{account.GetSequence()}, privateKey,
			)
			require.NoError(t, err)
			txBytes, err := manifest.TxConfig().TxEncoder()(tx)
			require.NoError(t, err)
			if test.anteFee == 0 {
				check, err := manifest.CheckTx(&abci.RequestCheckTx{Tx: txBytes, Type: abci.CheckTxType_New})
				require.NoError(t, err)
				require.NotZero(t, check.Code)
				require.Contains(t, check.Log, "insufficient fees", "local minimum gas prices reject mempool admission")
			}
			// A proposer can still include these bytes. FinalizeBlock does not
			// enforce the validator's local CheckTx minimum gas prices.
			response, err := manifest.FinalizeBlock(&abci.RequestFinalizeBlock{
				Height: manifest.LastBlockHeight() + 1, Time: ctx.BlockTime().Add(time.Second), Txs: [][]byte{txBytes},
			})
			require.NoError(t, err)
			require.Len(t, response.TxResults, 1)
			result := response.TxResults[0]
			if test.failingTail {
				require.NotZero(t, result.Code)
				require.Contains(t, result.Log, "message index: 1", "the real crisis message must have completed before the failing send")
			} else {
				require.Zero(t, result.Code, result.Log)
			}
			require.Positive(t, result.GasUsed, "the transaction still consumes gas when message state is rolled back")
			_, err = manifest.Commit()
			require.NoError(t, err)
			committedCtx := manifest.GetContextForCheckTx(nil)
			paid := test.anteFee
			if !test.failingTail {
				paid += crisisFee.Amount.Int64()
			}
			require.Equal(t, initial.Amount.SubRaw(paid), manifest.BankKeeper.GetBalance(committedCtx, sender, "umfx").Amount)
			require.Equal(t, uint64(1), manifest.AccountKeeper.GetAccount(committedCtx, sender).GetSequence(), "ante effects persist on message failure")
			collector := manifest.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName)
			require.Equal(t, paid, manifest.BankKeeper.GetBalance(committedCtx, collector, "umfx").Amount.Int64())
			require.Zero(t, manifest.BankKeeper.GetBalance(committedCtx, recipient, "umfx").Amount.Int64())
			t.Logf("tx gas=%d paid=%dumfx failing_tail=%t", result.GasUsed, paid, test.failingTail)
		})
	}
}

func TestCrisisCircuitBreakerBlocksDirectAndNestedRoutes(t *testing.T) {
	ctx, manifest := setupCrisisApp(t)
	_, _, sender := testdata.KeyTestPubAddr()
	_, _, recipient := testdata.KeyTestPubAddr()
	funds := sdk.NewCoins(sdk.NewInt64Coin("umfx", 10_000))
	require.NoError(t, manifest.BankKeeper.MintCoins(ctx, "mint", funds))
	require.NoError(t, manifest.BankKeeper.SendCoinsFromModuleToAccount(ctx, "mint", sender, funds))
	require.NoError(t, manifest.CrisisKeeper.ConstantFee.Set(ctx, sdk.NewInt64Coin("umfx", 1_000)))
	verify := &crisistypes.MsgVerifyInvariant{
		Sender: sender.String(), InvariantModuleName: "billing", InvariantRoute: "reservation-accounting",
	}
	url := sdk.MsgTypeURL(verify)
	require.Equal(t, "/cosmos.crisis.v1beta1.MsgVerifyInvariant", url)
	exec := authz.NewMsgExec(sender, []sdk.Msg{verify})
	route := func(msg sdk.Msg) error {
		handler := manifest.MsgServiceRouter().Handler(msg)
		require.NotNil(t, handler)
		_, err := handler(ctx, msg)
		return err
	}
	authority := sdk.AccAddress(manifest.CircuitKeeper.GetAuthority()).String()
	require.NoError(t, route(&circuittypes.MsgTripCircuitBreaker{
		Authority: authority, MsgTypeUrls: []string{url},
	}))
	// The top-level authz message remains allowed. The router must also check
	// its nested message, after authz has accepted execution by the signer.
	allowed, err := manifest.CircuitKeeper.IsAllowed(ctx, sdk.MsgTypeURL(&exec))
	require.NoError(t, err)
	require.True(t, allowed)
	for _, msg := range []sdk.Msg{verify, &exec} {
		err := route(msg)
		require.ErrorContains(t, err, "circuit breaker disables execution of this message: "+url)
	}
	require.Equal(t, funds.AmountOf("umfx"), manifest.BankKeeper.GetBalance(ctx, sender, "umfx").Amount,
		"both blocked routes must fail before charging the crisis fee")
	require.NoError(t, route(&banktypes.MsgSend{
		FromAddress: sender.String(), ToAddress: recipient.String(), Amount: sdk.NewCoins(sdk.NewInt64Coin("umfx", 1)),
	}))
	require.Equal(t, int64(1), manifest.BankKeeper.GetBalance(ctx, recipient, "umfx").Amount.Int64())
	require.NoError(t, route(&circuittypes.MsgResetCircuitBreaker{
		Authority: authority, MsgTypeUrls: []string{url},
	}))
	require.NoError(t, route(verify))
	require.NoError(t, route(&exec))
	require.Equal(t, int64(7_999), manifest.BankKeeper.GetBalance(ctx, sender, "umfx").Amount.Int64(),
		"reset restores both routes and their crisis fees")
}

func setupCrisisApp(t *testing.T) (sdk.Context, *ManifestApp) {
	t.Helper()
	return setupCrisisAppWithGas(t, DefaultConsensusParams.Block.MaxGas, nil)
}

func setupCrisisAppWithGas(t *testing.T, maxBlockGas int64, simulationGasLimit *uint64) (sdk.Context, *ManifestApp) {
	t.Helper()
	appparams.SetAddressPrefixes()
	db := dbm.NewMemDB()
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	options := sims.AppOptionsMap{flags.FlagHome: t.TempDir()}
	if simulationGasLimit != nil {
		options["wasm.simulation_gas_limit"] = *simulationGasLimit
	}
	manifest := NewApp(log.NewNopLogger(), db, nil, true, DefaultCommissionRateMinMax,
		options, baseapp.SetChainID(SimAppChainID), baseapp.SetMinGasPrices("0.001umfx"))
	validatorKey := tmed25519.GenPrivKey()
	validator := tmtypes.NewValidator(validatorKey.PubKey(), 1)
	valSet := tmtypes.NewValidatorSet([]*tmtypes.Validator{validator})
	owner := authtypes.NewBaseAccountWithAddress(sdk.AccAddress(validatorKey.PubKey().Address()))
	genesisState := genesisStateWithValSet(t, manifest, manifest.DefaultGenesis(), valSet, []authtypes.GenesisAccount{owner})
	genesis, err := json.Marshal(genesisState)
	require.NoError(t, err)
	blockTime := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	consensusParams := *DefaultConsensusParams
	blockParams := *consensusParams.Block
	blockParams.MaxGas = maxBlockGas
	consensusParams.Block = &blockParams
	_, err = manifest.InitChain(&abci.RequestInitChain{
		ChainId: SimAppChainID, InitialHeight: 1, Time: blockTime,
		ConsensusParams: &consensusParams, AppStateBytes: genesis,
	})
	require.NoError(t, err)
	// FinalizeBlock flushes genesis writes before Commit, as it does on chain.
	_, err = manifest.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 1, Time: blockTime})
	require.NoError(t, err)
	_, err = manifest.Commit()
	require.NoError(t, err)
	ctx := manifest.NewUncachedContext(false, tmproto.Header{
		ChainID: SimAppChainID, Height: 1, Time: blockTime,
	})
	return ctx, manifest
}

func TestBillingInvariantChargesCallerGasForStateReads(t *testing.T) {
	ctx, manifest := setupCrisisApp(t)
	invariant := billingkeeper.ReservationAccountingInvariant(manifest.BillingKeeper)
	measure := func() uint64 {
		meter := storetypes.NewGasMeter(1_000_000)
		message, broken := invariant(ctx.WithGasMeter(meter))
		require.False(t, broken, message)
		return meter.GasConsumed()
	}
	emptyGas := measure()
	require.Positive(t, emptyGas, "invariant reads must charge the supplied meter, independently of ante overhead")
	const accounts = 16
	addCrisisBillingAccounts(t, ctx, manifest, accounts)
	populatedGas := measure()
	require.Greater(t, populatedGas, emptyGas)
	require.GreaterOrEqual(t, populatedGas-emptyGas, uint64(accounts)*storetypes.KVGasConfig().IterNextCostFlat,
		"reading each added account must contribute to caller gas")
	require.Panics(t, func() {
		invariant(ctx.WithGasMeter(storetypes.NewGasMeter(emptyGas)))
	}, "a budget sufficient for empty state must stop the larger bounded fixture")
}

func addCrisisBillingAccounts(t *testing.T, ctx sdk.Context, manifest *ManifestApp, count int) {
	t.Helper()
	for i := range count {
		tenant := sdk.AccAddress(fmt.Appendf(nil, "crisis-account-%05d", i))
		require.NoError(t, manifest.BillingKeeper.SetCreditAccount(ctx, billingtypes.CreditAccount{
			Tenant: tenant.String(), CreditAddress: billingtypes.DeriveCreditAddress(tenant).String(),
		}))
	}
}
