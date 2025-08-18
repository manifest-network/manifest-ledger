package interchaintest

import (
	"context"
	"fmt"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/dockerutil"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"

	"github.com/manifest-network/manifest-ledger/interchaintest/helpers"
	manifesttypes "github.com/manifest-network/manifest-ledger/x/manifest/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestManifestModule(t *testing.T) {
	ctx := context.Background()

	cfgA := LocalChainConfig
	cfgA.WithCodeCoverage()

	cf := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t, zaptest.Level(zapcore.DebugLevel)), []*interchaintest.ChainSpec{
		{
			Name:          "manifest",
			Version:       "local",
			ChainName:     cfgA.ChainID,
			NumValidators: &vals,
			NumFullNodes:  &fullNodes,
			ChainConfig:   cfgA,
		},
	})

	chains, err := cf.Chains(t.Name())
	require.NoError(t, err)
	manifestA := chains[0].(*cosmos.CosmosChain)

	// Relayer Factory
	client, network := interchaintest.DockerSetup(t)

	ic := interchaintest.NewInterchain().
		AddChain(manifestA)

	rep := testreporter.NewNopReporter()
	eRep := rep.RelayerExecReporter(t)

	// Build interchain
	require.NoError(t, ic.Build(ctx, eRep, interchaintest.InterchainBuildOptions{
		TestName:         t.Name(),
		Client:           client,
		NetworkID:        network,
		SkipPathCreation: false,
	}))

	// Chains
	appChain := chains[0].(*cosmos.CosmosChain)
	poaAdmin, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "acc0", accMnemonic, DefaultGenesisAmt, appChain)
	if err != nil {
		t.Fatal(err)
	}

	users := interchaintest.GetAndFundTestUsers(t, ctx, "default", DefaultGenesisAmt, appChain, appChain, appChain)
	user1, user2, user3 := users[0], users[1], users[2]
	uaddr, addr2, addr3 := user1.FormattedAddress(), user2.FormattedAddress(), user3.FormattedAddress()

	t.Run("success; Perform a manual distribution payout from the PoA admin", func(t *testing.T) {
		beforeBal1, _ := appChain.GetBalance(ctx, uaddr, Denom)
		beforeBal2, _ := appChain.GetBalance(ctx, addr2, Denom)
		beforeBal3, _ := appChain.GetBalance(ctx, addr3, Denom)

		payouts := []manifesttypes.PayoutPair{
			manifesttypes.NewPayoutPair(sdk.MustAccAddressFromBech32(uaddr), Denom, 1_000_000),
			manifesttypes.NewPayoutPair(sdk.MustAccAddressFromBech32(addr2), Denom, 2_000_000),
			manifesttypes.NewPayoutPair(sdk.MustAccAddressFromBech32(addr3), Denom, 3_000_000),
		}

		// print beforeBal1
		fmt.Println(beforeBal1)

		_, err := helpers.ManifestStakeholderPayout(t, ctx, appChain, poaAdmin, payouts)
		require.NoError(t, err)

		// validate new user1 balance is 1_000_000 higher
		user1bal, err := appChain.GetBalance(ctx, uaddr, Denom)
		require.NoError(t, err)
		fmt.Println(user1bal)
		fmt.Println(user1bal.Uint64())
		fmt.Println(user1bal.Int64())
		require.EqualValues(t, user1bal.Uint64(), beforeBal1.Uint64()+1_000_000, "user1 balance should be 1_000_000 higher")

		user2bal, err := appChain.GetBalance(ctx, addr2, Denom)
		require.NoError(t, err)
		require.EqualValues(t, user2bal.Uint64(), beforeBal2.Uint64()+2_000_000)

		user3bal, err := appChain.GetBalance(ctx, addr3, Denom)
		require.NoError(t, err)
		require.EqualValues(t, user3bal.Uint64(), beforeBal3.Uint64()+3_000_000)

	})

	t.Run("fail: invalid payout 0 coin", func(t *testing.T) {
		_, err := helpers.ManifestStakeholderPayout(t, ctx, appChain, poaAdmin, []manifesttypes.PayoutPair{
			manifesttypes.NewPayoutPair(sdk.MustAccAddressFromBech32(uaddr), Denom, 0),
		})
		require.Error(t, err)
	})

	t.Run("fail: invalid payout addr", func(t *testing.T) {
		_, err = helpers.ManifestStakeholderPayout(t, ctx, appChain, poaAdmin, []manifesttypes.PayoutPair{
			manifesttypes.NewPayoutPair(sdk.MustAccAddressFromBech32(uaddr), Denom, 1),
			manifesttypes.NewPayoutPair(sdk.MustAccAddressFromBech32(uaddr), Denom, 2),
		})
		require.Error(t, err)
	})

	t.Run("fail: duplicate address payout", func(t *testing.T) {
		_, err = helpers.ManifestStakeholderPayout(t, ctx, appChain, poaAdmin, []manifesttypes.PayoutPair{
			{
				Address: "abcdefg",
				Coin:    sdk.NewCoin(Denom, sdkmath.NewInt(1)),
			},
		})
		require.Error(t, err)
	})

	t.Run("fail: invalid burn authority", func(t *testing.T) {
		accBal, err := appChain.GetBalance(ctx, uaddr, Denom)
		require.NoError(t, err)
		o, err := helpers.ManifestBurnTokens(t, ctx, appChain, uaddr, "1"+Denom)
		require.NoError(t, err) // The tx is successful but the burn fails
		tx, err := appChain.GetTransaction(o.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, tx.Code, uint32(0x0)) // The burn failed
		require.Contains(t, tx.RawLog, "invalid authority")
		accBal2, err := appChain.GetBalance(ctx, uaddr, Denom)
		require.NoError(t, err)
		require.EqualValues(t, accBal, accBal2)
	})

	t.Run("success: burn tokens as poa admin", func(t *testing.T) {
		poaAdminAddr := poaAdmin.FormattedAddress()
		accBal, err := appChain.GetBalance(ctx, poaAdminAddr, Denom)
		require.NoError(t, err)
		o, err := helpers.ManifestBurnTokens(t, ctx, appChain, poaAdminAddr, "1"+Denom)
		require.NoError(t, err)
		tx, err := appChain.GetTransaction(o.TxHash)
		require.NoError(t, err)
		require.Equal(t, tx.Code, uint32(0x0))
		accBal2, err := appChain.GetBalance(ctx, poaAdminAddr, Denom)
		require.NoError(t, err)
		require.EqualValues(t, accBal2, accBal.Sub(sdkmath.OneInt()))
	})

	t.Run("fail: burn unknown denom as poa admin", func(t *testing.T) {
		poaAdminAddr := poaAdmin.FormattedAddress()
		accBal, err := appChain.GetBalance(ctx, poaAdminAddr, Denom)
		require.NoError(t, err)
		o, err := helpers.ManifestBurnTokens(t, ctx, appChain, poaAdminAddr, "1foobar")
		require.NoError(t, err) // The tx is successful but the burn fails
		tx, err := appChain.GetTransaction(o.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, tx.Code, uint32(0x0)) // The burn failed
		require.Contains(t, tx.RawLog, "insufficient funds ")
		accBal2, err := appChain.GetBalance(ctx, poaAdminAddr, Denom)
		require.NoError(t, err)
		require.EqualValues(t, accBal, accBal2)
	})

	t.Run("fail: burn invalid coin expression as poa admin", func(t *testing.T) {
		poaAdminAddr := poaAdmin.FormattedAddress()
		accBal, err := appChain.GetBalance(ctx, poaAdminAddr, Denom)
		require.NoError(t, err)
		_, err = helpers.ManifestBurnTokens(t, ctx, appChain, poaAdminAddr, "foobar")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid decimal coin expression")
		accBal2, err := appChain.GetBalance(ctx, poaAdminAddr, Denom)
		require.NoError(t, err)
		require.EqualValues(t, accBal, accBal2)
	})

	t.Cleanup(func() {
		dockerutil.CopyCoverageFromContainer(ctx, t, client, manifestA.GetNode().ContainerID(), manifestA.HomeDir(), ExternalGoCoverDir)
		_ = ic.Close()
	})
}
