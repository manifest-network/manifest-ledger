package interchaintest

import (
	"context"
	"strings"
	"testing"

	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"

	"github.com/liftedinit/manifest-ledger/interchaintest/helpers"
)

const GroupMetadataLimit = 2048

func TestGroupMetadataLimits(t *testing.T) {
	ctx := context.Background()

	cfgA := LocalChainConfig
	cfgA.Name = "manifest-2"
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

	appChain := chains[0].(*cosmos.CosmosChain)

	client, network := interchaintest.DockerSetup(t)

	ic := interchaintest.NewInterchain().
		AddChain(appChain)

	rep := testreporter.NewNopReporter()
	eRep := rep.RelayerExecReporter(t)

	require.NoError(t, ic.Build(ctx, eRep, interchaintest.InterchainBuildOptions{
		TestName:         t.Name(),
		Client:           client,
		NetworkID:        network,
		SkipPathCreation: false,
	}))

	users := interchaintest.GetAndFundTestUsers(t, ctx, "default", DefaultGenesisAmt, appChain)
	admin := users[0]
	adminAddr := admin.FormattedAddress()

	t.Run("success: create group with exact metadata limit", func(t *testing.T) {
		groupMetadata := strings.Repeat("A", GroupMetadataLimit)
		_, err := helpers.CreateGroupWithMetadata(ctx, t, appChain, adminAddr, groupMetadata)
		require.NoError(t, err)
	})

	t.Run("fail: create group with over metadata limit", func(t *testing.T) {
		groupMetadata := strings.Repeat("A", GroupMetadataLimit+1)
		_, err := helpers.CreateGroupWithMetadata(ctx, t, appChain, adminAddr, groupMetadata)
		require.Error(t, err)
		require.Contains(t, err.Error(), "group metadata: limit exceeded")
	})

	t.Cleanup(func() {
		_ = ic.Close()
	})
}
