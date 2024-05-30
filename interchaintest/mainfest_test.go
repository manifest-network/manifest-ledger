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
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"

	"github.com/liftedinit/manifest-ledger/interchaintest/helpers"
	manifesttypes "github.com/liftedinit/manifest-ledger/x/manifest/types"

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

	t.Run("success; query params", func(t *testing.T) {
		p, err = helpers.ManifestQueryParams(ctx, node)
		require.NoError(t, err)
	})

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
			{
				Address: "abcdefg",
				Coin:    sdk.NewCoin(Denom, sdkmath.NewInt(1)),
			},
		})
		require.Error(t, err)
	})

	t.Cleanup(func() {
		CopyCoverageFromContainer(ctx, t, client, appChain.GetNode().ContainerID(), appChain.HomeDir())
		_ = ic.Close()
	})
}
