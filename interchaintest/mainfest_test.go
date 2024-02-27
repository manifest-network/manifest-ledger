package interchaintest

import (
	"context"
	"fmt"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/liftedinit/manifest-ledger/interchaintest/helpers"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestManifestModule(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfgA := LocalChainConfig
	cfgA.Env = []string{
		fmt.Sprintf("POA_ADMIN_ADDRESS=%s", accAddr),
	}

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
	user1, user2 := users[0], users[1]
	uaddr, addr2 := user1.FormattedAddress(), user2.FormattedAddress()

	node := appChain.GetNode()

	// Base Query Check of genesis defaults
	p, err := helpers.ManifestQueryParams(ctx, node)
	require.NoError(t, err)
	fmt.Println(p)
	require.True(t, p.Inflation.AutomaticEnabled)
	require.EqualValues(t, p.Inflation.MintDenom, Denom)
	inflationAddr := p.StakeHolders[0].Address

	t.Run("Ensure the account's balance gets auto paid out with auto inflation on", func(t *testing.T) {
		oldBal, err := appChain.GetBalance(ctx, inflationAddr, Denom)
		require.NoError(t, err)

		require.NoError(t, testutil.WaitForBlocks(ctx, 2, appChain))

		newBal, err := appChain.GetBalance(ctx, inflationAddr, Denom)
		require.NoError(t, err)

		require.Greater(t, newBal.Uint64(), oldBal.Uint64())
	})

	t.Run("fail; Perform a manual distribution payout from the PoA admin (fails due to auto inflation being on)", func(t *testing.T) {
		c := sdk.NewCoin(Denom, sdkmath.NewInt(9999999999))
		txRes, _ := helpers.ManifestStakeholderPayout(t, ctx, appChain, poaAdmin, c)
		require.EqualValues(t, 0, txRes.Code)

		// ensure the new balance is not > c.Amount (a manual payout)
		latestBal, err := appChain.GetBalance(ctx, inflationAddr, Denom)
		require.NoError(t, err)
		require.LessOrEqual(t, latestBal.Uint64(), c.Amount.Uint64())
	})

	t.Run("success; disable auto inflation. Set new stakeholders", func(t *testing.T) {
		txRes, _ := helpers.ManifestUpdateParams(
			t, ctx, appChain, poaAdmin,
			fmt.Sprintf("%s:1_000_000,%s:99_000_000", uaddr, addr2),
			false,
			sdk.NewCoin(Denom, sdkmath.NewIntFromUint64(p.Inflation.YearlyAmount)), // it's off, this just matches genesis
		)
		require.EqualValues(t, 0, txRes.Code)

		p, err = helpers.ManifestQueryParams(ctx, node)
		require.NoError(t, err)
		require.False(t, p.Inflation.AutomaticEnabled)
		require.Len(t, p.StakeHolders, 2)
	})

	t.Run("success; Perform a manual distribution payout from the PoA admin", func(t *testing.T) {

		beforeBal1, _ := appChain.GetBalance(ctx, uaddr, Denom)
		beforeBal2, _ := appChain.GetBalance(ctx, addr2, Denom)

		c := sdk.NewCoin(Denom, sdkmath.NewInt(100_000000))
		txRes, _ := helpers.ManifestStakeholderPayout(t, ctx, appChain, poaAdmin, c)
		require.EqualValues(t, 0, txRes.Code)

		user1bal, err := appChain.GetBalance(ctx, uaddr, Denom)
		require.NoError(t, err)
		require.EqualValues(t, user1bal.Uint64(), beforeBal1.Uint64()+1_000_000)

		user2bal, err := appChain.GetBalance(ctx, addr2, Denom)
		require.NoError(t, err)
		require.EqualValues(t, user2bal.Uint64(), beforeBal2.Uint64()+99_000_000)

	})

	t.Cleanup(func() {
		_ = ic.Close()
	})
}
