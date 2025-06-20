package interchaintest

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/dockerutil"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	interchaintestrelayer "github.com/strangelove-ventures/interchaintest/v8/relayer"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
)

func TestIBC(t *testing.T) {
	ctx := context.Background()

	cfgA := LocalChainConfig
	cfgA.ChainID = "manifest-9"
	cfgA.WithCodeCoverage()

	cfgB := LocalChainConfig
	cfgB.ChainID = "manifest-10"
	cfgB.WithCodeCoverage()

	cf := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t, zaptest.Level(zapcore.DebugLevel)), []*interchaintest.ChainSpec{
		{
			Name:          "manifest",
			Version:       "local",
			ChainName:     cfgA.ChainID,
			NumValidators: &vals,
			NumFullNodes:  &fullNodes,
			ChainConfig:   cfgA,
		},
		{
			Name:          "manifest",
			Version:       "local",
			ChainName:     cfgB.ChainID,
			NumValidators: &vals,
			NumFullNodes:  &fullNodes,

			ChainConfig: cfgB,
		},
	})

	chains, err := cf.Chains(t.Name())
	require.NoError(t, err)
	manifestA, manifestB := chains[0].(*cosmos.CosmosChain), chains[1].(*cosmos.CosmosChain)

	// Relayer Factory
	client, network := interchaintest.DockerSetup(t)

	rf := interchaintest.NewBuiltinRelayerFactory(
		ibc.CosmosRly,
		zaptest.NewLogger(t, zaptest.Level(zapcore.DebugLevel)),
		interchaintestrelayer.CustomDockerImage("ghcr.io/cosmos/relayer", "main", "100:1000"),
		interchaintestrelayer.StartupFlags("--processor", "events", "--block-history", "100"),
	)

	r := rf.Build(t, client, network)

	const ibcPath = "ibc-path"
	ic := interchaintest.NewInterchain().
		AddChain(manifestA).
		AddChain(manifestB).
		AddRelayer(r, "relayer").
		AddLink(interchaintest.InterchainLink{
			Chain1:  manifestA,
			Chain2:  manifestB,
			Relayer: r,
			Path:    ibcPath,
		})

	rep := testreporter.NewNopReporter()
	eRep := rep.RelayerExecReporter(t)

	// Build interchain
	require.NoError(t, ic.Build(ctx, eRep, interchaintest.InterchainBuildOptions{
		TestName:         t.Name(),
		Client:           client,
		NetworkID:        network,
		SkipPathCreation: false,
	}))

	// Create and Fund User Wallets
	fundAmount := math.NewInt(10_000_000)
	users := interchaintest.GetAndFundTestUsers(t, ctx, "default", fundAmount, manifestA, manifestB)
	manifestAUser := users[0]
	manifestBUser := users[1]

	manifestAUserBalInitial, err := manifestA.GetBalance(ctx, manifestAUser.FormattedAddress(), manifestA.Config().Denom)
	require.NoError(t, err)
	require.True(t, manifestAUserBalInitial.Equal(fundAmount))

	// Get Channel ID
	manifestAChannelInfo, err := r.GetChannels(ctx, eRep, manifestA.Config().ChainID)
	require.NoError(t, err)
	manifestAChannelID := manifestAChannelInfo[0].ChannelID

	osmoChannelInfo, err := r.GetChannels(ctx, eRep, manifestB.Config().ChainID)
	require.NoError(t, err)
	osmoChannelID := osmoChannelInfo[0].ChannelID

	// Send Transaction
	amountToSend := math.NewInt(1_000_000)
	dstAddress := manifestBUser.FormattedAddress()
	transfer := ibc.WalletAmount{
		Address: dstAddress,
		Denom:   manifestA.Config().Denom,
		Amount:  amountToSend,
	}

	_, err = manifestA.SendIBCTransfer(ctx, manifestAChannelID, manifestAUser.KeyName(), transfer, ibc.TransferOptions{})
	require.NoError(t, err)

	// relay MsgRecvPacket to manifestB, then MsgAcknowledgement back to manifestA
	require.NoError(t, r.Flush(ctx, eRep, ibcPath, manifestAChannelID))

	// test source wallet has decreased funds
	expectedBal := manifestAUserBalInitial.Sub(amountToSend)
	manifestAUserBalNew, err := manifestA.GetBalance(ctx, manifestAUser.FormattedAddress(), manifestA.Config().Denom)
	require.NoError(t, err)
	require.True(t, manifestAUserBalNew.Equal(expectedBal))

	// Trace IBC Denom
	srcDenomTrace := transfertypes.ParseDenomTrace(transfertypes.GetPrefixedDenom("transfer", osmoChannelID, manifestA.Config().Denom))
	dstIbcDenom := srcDenomTrace.IBCDenom()

	// Test destination wallet has increased funds
	osmosUserBalNew, err := manifestB.GetBalance(ctx, manifestBUser.FormattedAddress(), dstIbcDenom)
	require.NoError(t, err)
	require.True(t, osmosUserBalNew.Equal(amountToSend))

	t.Cleanup(func() {
		dockerutil.CopyCoverageFromContainer(ctx, t, client, manifestA.GetNode().ContainerID(), manifestA.HomeDir(), ExternalGoCoverDir)
		_ = ic.Close()
	})
}
