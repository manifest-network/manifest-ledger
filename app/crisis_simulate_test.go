package app

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"

	circuittypes "cosmossdk.io/x/circuit/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/testutil/sims"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	crisistypes "github.com/cosmos/cosmos-sdk/x/crisis/types"
)

func TestCrisisSimulationHonorsNodeGasAndCircuitControls(t *testing.T) {
	lowLimit, highLimit := uint64(30_000), uint64(250_000)
	for _, test := range []struct {
		name      string
		blockGas  int64
		nodeLimit *uint64
		wantLimit uint64
		trip      bool
		wantError string
	}{
		{name: "positive block fallback", blockGas: 1_000_000, wantLimit: 1_000_000},
		{name: "explicit positive node limit", blockGas: 1_000_000, nodeLimit: &highLimit, wantLimit: highLimit},
		{name: "explicit node limit exhaustion", blockGas: 1_000_000, nodeLimit: &lowLimit, wantError: "out of gas"},
		{name: "block fallback exhaustion", blockGas: 30_000, wantError: "out of gas"},
		// The SDK recommends not exceeding block gas, but does not clamp an
		// explicit simulation limit to it. Pin this precedence for operators.
		{name: "explicit node limit overrides smaller block limit", blockGas: 30_000, nodeLimit: &highLimit, wantLimit: highLimit},
		{name: "circuit rejects before invariant work", blockGas: 1_000_000, nodeLimit: &highLimit, trip: true, wantError: "tx type not allowed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, manifest := setupCrisisAppWithGas(t, test.blockGas, test.nodeLimit)
			privateKey, publicKey, sender := testdata.KeyTestPubAddr()
			account := manifest.AccountKeeper.NewAccountWithAddress(ctx, sender)
			require.NoError(t, account.SetPubKey(publicKey))
			require.NoError(t, account.SetSequence(7))
			manifest.AccountKeeper.SetAccount(ctx, account)
			funds := sdk.NewCoins(sdk.NewInt64Coin("umfx", 100_000))
			require.NoError(t, manifest.BankKeeper.MintCoins(ctx, "mint", funds))
			require.NoError(t, manifest.BankKeeper.SendCoinsFromModuleToAccount(ctx, "mint", sender, funds))
			require.NoError(t, manifest.CrisisKeeper.ConstantFee.Set(ctx, sdk.NewInt64Coin("umfx", 1_000)))
			addCrisisBillingAccounts(t, ctx, manifest, 4)
			verify := &crisistypes.MsgVerifyInvariant{
				Sender: sender.String(), InvariantModuleName: "billing", InvariantRoute: "reservation-accounting",
			}
			if test.trip {
				trip := &circuittypes.MsgTripCircuitBreaker{
					Authority:   sdk.AccAddress(manifest.CircuitKeeper.GetAuthority()).String(),
					MsgTypeUrls: []string{sdk.MsgTypeURL(verify)},
				}
				_, err := manifest.MsgServiceRouter().Handler(trip)(ctx, trip)
				require.NoError(t, err)
			}
			called := false
			var observedLimit uint64
			routes := manifest.CrisisKeeper.Routes()
			found := false
			for i := range routes {
				if routes[i].ModuleName != verify.InvariantModuleName || routes[i].Route != verify.InvariantRoute {
					continue
				}
				found = true
				invariant := routes[i].Invar
				routes[i].Invar = func(ctx sdk.Context) (string, bool) {
					called, observedLimit = true, ctx.GasMeter().Limit()
					return invariant(ctx)
				}
			}
			require.True(t, found, "observe the registered billing invariant without replacing its work")
			// The request is signed with this fixture's own key and uses its
			// funded account. A nonzero fee exercises discarded cache writes.
			tx, err := sims.GenSignedMockTx(
				rand.New(rand.NewSource(1)), //nolint:gosec // deterministic transaction memo in an integration test
				manifest.TxConfig(), []sdk.Msg{verify}, sdk.NewCoins(sdk.NewInt64Coin("umfx", 17)),
				30_000, SimAppChainID, []uint64{account.GetAccountNumber()}, []uint64{account.GetSequence()}, privateKey,
			)
			require.NoError(t, err)
			txBytes, err := manifest.TxConfig().TxEncoder()(tx)
			require.NoError(t, err)
			manifest.RegisterTxService(client.Context{})
			const path = "/cosmos.tx.v1beta1.Service/Simulate"
			handler := manifest.GRPCQueryRouter().Route(path)
			require.NotNil(t, handler)
			requestBytes, err := (&txtypes.SimulateRequest{TxBytes: txBytes}).Marshal()
			require.NoError(t, err)
			// Simulate branches from the CheckTx cache. Observe that cache,
			// rather than the CMS used to seed this fixture, so leaked writes
			// from a simulation branch would be visible to these assertions.
			stateCtx := manifest.GetContextForCheckTx(nil)
			billingBefore := manifest.BillingKeeper.ExportGenesis(stateCtx)
			collector := manifest.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName)
			senderBefore := manifest.BankKeeper.GetBalance(stateCtx, sender, "umfx")
			collectorBefore := manifest.BankKeeper.GetBalance(stateCtx, collector, "umfx")
			sequenceBefore := manifest.AccountKeeper.GetAccount(stateCtx, sender).GetSequence()
			response, err := handler(ctx, &abci.RequestQuery{Path: path, Data: requestBytes})
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				require.Nil(t, response)
			} else {
				require.NoError(t, err)
				var result txtypes.SimulateResponse
				require.NoError(t, result.Unmarshal(response.Value))
				require.True(t, called)
				require.Equal(t, test.wantLimit, observedLimit)
				require.Equal(t, test.wantLimit, result.GasInfo.GasWanted)
				require.LessOrEqual(t, result.GasInfo.GasUsed, test.wantLimit)
			}
			if test.trip {
				require.False(t, called, "the circuit must reject before billing state is scanned")
			}
			stateCtx = manifest.GetContextForCheckTx(nil)
			require.Equal(t, senderBefore, manifest.BankKeeper.GetBalance(stateCtx, sender, "umfx"))
			require.Equal(t, collectorBefore, manifest.BankKeeper.GetBalance(stateCtx, collector, "umfx"))
			require.Equal(t, sequenceBefore, manifest.AccountKeeper.GetAccount(stateCtx, sender).GetSequence())
			require.Equal(t, billingBefore, manifest.BillingKeeper.ExportGenesis(stateCtx))
		})
	}
}
