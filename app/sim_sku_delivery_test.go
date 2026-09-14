package app

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simulationtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	skusimulation "github.com/manifest-network/manifest-ledger/x/sku/simulation"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

func TestSKUSimulationCreatesProviderAndSKUThroughSignedTransactions(t *testing.T) {
	// Reuse the complete InitChain/FinalizeBlock fixture, including a validator
	// and POA state, so delivery exercises the real application ante handler.
	// The SDK simulation generator declares 10M gas per transaction. Keep a
	// finite block limit that admits both signed fixture operations.
	ctx, manifest := setupCrisisAppWithGas(t, 20_000_000, nil)
	privateKey := secp256k1.GenPrivKeyFromSecret([]byte("sku-simulation-success-fixture"))
	manager := sdk.AccAddress(privateKey.PubKey().Address())
	account := manifest.AccountKeeper.NewAccountWithAddress(ctx, manager)
	require.NoError(t, account.SetPubKey(privateKey.PubKey()))
	manifest.AccountKeeper.SetAccount(ctx, account)
	funds := sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 1_000_000_000))
	require.NoError(t, manifest.BankKeeper.MintCoins(ctx, "mint", funds))
	require.NoError(t, manifest.BankKeeper.SendCoinsFromModuleToAccount(ctx, "mint", manager, funds))
	manifest.BankKeeper.SetSendEnabled(ctx, sdk.DefaultBondDenom, true)
	require.NoError(t, manifest.SKUKeeper.SetParams(ctx, skutypes.Params{AllowedList: []string{manager.String()}}))

	// Use the same post-FinalizeBlock delivery context as the full simulator.
	// Each real operation generates and signs its transaction with our sole
	// funded manager, removing random authorization and prerequisite no-ops.
	_, err := manifest.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: manifest.LastBlockHeight() + 1, Time: ctx.BlockTime().Add(time.Second),
	})
	require.NoError(t, err)
	ctx = manifest.GetContextForFinalizeBlock(nil)
	accounts := []simulationtypes.Account{{Address: manager, PubKey: privateKey.PubKey(), PrivKey: privateKey}}
	random := rand.New(rand.NewSource(1729)) //nolint:gosec // deterministic simulation fixture
	stats := simulation.NewEventStats()
	for _, operation := range []simulationtypes.Operation{
		skusimulation.SimulateMsgCreateProvider(manifest.TxConfig(), manifest.SKUKeeper),
		skusimulation.SimulateMsgCreateSKU(manifest.TxConfig(), manifest.SKUKeeper),
	} {
		message, future, err := operation(random, manifest.BaseApp, ctx, accounts, ctx.ChainID())
		require.NoError(t, err)
		require.True(t, message.OK, message.Comment)
		require.Empty(t, future)
		message.LogEvent(stats.Tally)
	}
	require.Equal(t, 1, stats[skutypes.ModuleName][sdk.MsgTypeURL(&skutypes.MsgCreateProvider{})]["ok"])
	require.Equal(t, 1, stats[skutypes.ModuleName][sdk.MsgTypeURL(&skutypes.MsgCreateSKU{})]["ok"])
	providers, err := manifest.SKUKeeper.GetAllProviders(ctx)
	require.NoError(t, err)
	require.Len(t, providers, 1, "success statistics must correspond to a stored provider")
	require.Equal(t, manager.String(), providers[0].Address)
	require.Equal(t, manager.String(), providers[0].PayoutAddress)
	require.True(t, providers[0].Active)
	skus, err := manifest.SKUKeeper.GetAllSKUs(ctx)
	require.NoError(t, err)
	require.Len(t, skus, 1, "success statistics must correspond to a stored SKU")
	require.Equal(t, providers[0].Uuid, skus[0].ProviderUuid)
	require.True(t, skus[0].Active)
	require.NoError(t, skutypes.ValidatePriceAndUnit(skus[0].BasePrice, skus[0].Unit))
	require.Equal(t, uint64(2), manifest.AccountKeeper.GetAccount(ctx, manager).GetSequence(), "both signed transactions must pass ante handling")
}
