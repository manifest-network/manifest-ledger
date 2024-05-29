package interchaintest

import (
	"context"
	"fmt"
	"path"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"

	"github.com/liftedinit/manifest-ledger/interchaintest/helpers"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestManifestModule(t *testing.T) {
	ctx := context.Background()

	// Same as ChainNode.HomeDir() but we need it before the chain is created
	// The node volume is always mounted at /var/cosmos-chain/[chain-name]
	// This is a hackish way to get the coverage files from the ephemeral containers
	cfgA := LocalChainConfig
	internalGoCoverDir := path.Join("/var/cosmos-chain", cfgA.ChainID)
	cfgA.Env = []string{
		fmt.Sprintf("POA_ADMIN_ADDRESS=%s", accAddr),
		fmt.Sprintf("GOCOVERDIR=%s", internalGoCoverDir),
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
	user1, user2, user3 := users[0], users[1], users[2]
	uaddr, addr2, addr3 := user1.FormattedAddress(), user2.FormattedAddress(), user3.FormattedAddress()

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
		txRes, _ := helpers.ManifestStakeholderPayout(t, ctx, appChain, poaAdmin, c.String())
		require.EqualValues(t, 0, txRes.Code)

		// ensure the new balance is not > c.Amount (a manual payout)
		latestBal, err := appChain.GetBalance(ctx, inflationAddr, Denom)
		require.NoError(t, err)
		require.LessOrEqual(t, latestBal.Uint64(), c.Amount.Uint64())
	})

	t.Run("success; disable auto inflation. Set new stakeholders", func(t *testing.T) {
		txRes, _ := helpers.ManifestUpdateParams(
			t, ctx, appChain, poaAdmin,
			fmt.Sprintf("%s:1_000_000,%s:98_000_000,%s:1_000_000", uaddr, addr2, addr3),
			"false",
			sdk.NewCoin(Denom, sdkmath.NewIntFromUint64(p.Inflation.YearlyAmount)).String(), // it's off, this just matches genesis
		)
		require.EqualValues(t, 0, txRes.Code)

		p, err = helpers.ManifestQueryParams(ctx, node)
		require.NoError(t, err)
		require.False(t, p.Inflation.AutomaticEnabled)
		require.Len(t, p.StakeHolders, 3)
	})

	t.Run("success; Perform a manual distribution payout from the PoA admin", func(t *testing.T) {

		beforeBal1, _ := appChain.GetBalance(ctx, uaddr, Denom)
		beforeBal2, _ := appChain.GetBalance(ctx, addr2, Denom)
		beforeBal3, _ := appChain.GetBalance(ctx, addr3, Denom)

		c := sdk.NewCoin(Denom, sdkmath.NewInt(100_000000))
		txRes, _ := helpers.ManifestStakeholderPayout(t, ctx, appChain, poaAdmin, c.String())
		require.EqualValues(t, 0, txRes.Code)

		user1bal, err := appChain.GetBalance(ctx, uaddr, Denom)
		require.NoError(t, err)
		require.EqualValues(t, user1bal.Uint64(), beforeBal1.Uint64()+1_000_000)

		user2bal, err := appChain.GetBalance(ctx, addr2, Denom)
		require.NoError(t, err)
		require.EqualValues(t, user2bal.Uint64(), beforeBal2.Uint64()+98_000_000)

		user3bal, err := appChain.GetBalance(ctx, addr3, Denom)
		require.NoError(t, err)
		require.EqualValues(t, user3bal.Uint64(), beforeBal3.Uint64()+1_000_000)

	})

	t.Run("fail: invalid payout coin", func(t *testing.T) {
		_, err := helpers.ManifestStakeholderPayout(t, ctx, appChain, poaAdmin, "foobar")
		require.Error(t, err)
		require.ErrorContains(t, err, "invalid decimal coin expression")
	})

	t.Run("fail: invalid stakeholder addr", func(t *testing.T) {
		_, err := helpers.ManifestUpdateParams(
			t, ctx, appChain, poaAdmin,
			fmt.Sprintf("%s:1_000_000,%s:99_000_000", uaddr, "foobar"),
			"false",
			sdk.NewCoin(Denom, sdkmath.NewIntFromUint64(p.Inflation.YearlyAmount)).String(), // it's off, this just matches genesis
		)
		require.Error(t, err)
		require.ErrorContains(t, err, "invalid address")
	})

	t.Run("fail: invalid stakeholder percentage (>100%)", func(t *testing.T) {
		_, err := helpers.ManifestUpdateParams(
			t, ctx, appChain, poaAdmin,
			fmt.Sprintf("%s:2_000_000,%s:99_000_000", uaddr, addr2),
			"false",
			sdk.NewCoin(Denom, sdkmath.NewIntFromUint64(p.Inflation.YearlyAmount)).String(), // it's off, this just matches genesis
		)
		require.Error(t, err)
		require.ErrorContains(t, err, "stakeholders should add up to")
	})

	t.Run("fail: invalid stakeholder percentage (<100%)", func(t *testing.T) {
		_, err := helpers.ManifestUpdateParams(
			t, ctx, appChain, poaAdmin,
			fmt.Sprintf("%s:1_000_000,%s:98_000_000", uaddr, addr2),
			"false",
			sdk.NewCoin(Denom, sdkmath.NewIntFromUint64(p.Inflation.YearlyAmount)).String(), // it's off, this just matches genesis
		)
		require.Error(t, err)
		require.ErrorContains(t, err, "stakeholders should add up to")
	})

	t.Run("fail: invalid stakeholder", func(t *testing.T) {
		_, err := helpers.ManifestUpdateParams(
			t, ctx, appChain, poaAdmin,
			"foobar",
			"false",
			sdk.NewCoin(Denom, sdkmath.NewIntFromUint64(p.Inflation.YearlyAmount)).String(), // it's off, this just matches genesis
		)
		require.Error(t, err)
		require.ErrorContains(t, err, "invalid stakeholder")
	})

	t.Run("fail: invalid percentage", func(t *testing.T) {
		_, err := helpers.ManifestUpdateParams(
			t, ctx, appChain, poaAdmin,
			fmt.Sprintf("%s:foobar", uaddr),
			"false",
			sdk.NewCoin(Denom, sdkmath.NewIntFromUint64(p.Inflation.YearlyAmount)).String(), // it's off, this just matches genesis
		)
		require.Error(t, err)
		require.ErrorContains(t, err, "invalid percentage")
	})

	t.Run("fail: invalid automatic inflation", func(t *testing.T) {
		_, err := helpers.ManifestUpdateParams(
			t, ctx, appChain, poaAdmin,
			fmt.Sprintf("%s:1_000_000,%s:99_000_000", uaddr, addr2),
			"foobar",
			sdk.NewCoin(Denom, sdkmath.NewIntFromUint64(p.Inflation.YearlyAmount)).String(), // it's off, this just matches genesis
		)
		require.Error(t, err)
		require.ErrorContains(t, err, "invalid syntax")
		require.ErrorContains(t, err, "strconv.ParseBool")
	})

	t.Run("fail: invalid inflation coin", func(t *testing.T) {
		_, err := helpers.ManifestUpdateParams(
			t, ctx, appChain, poaAdmin,
			fmt.Sprintf("%s:1_000_000,%s:99_000_000", uaddr, addr2),
			"false",
			"foobar",
		)
		require.Error(t, err)
		require.ErrorContains(t, err, "invalid decimal coin expression")
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
		CopyCoverageFromContainer(ctx, t, client, appChain.GetNode().ContainerID(), appChain.HomeDir())
		_ = ic.Close()
	})
}
