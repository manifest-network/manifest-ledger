package interchaintest

import (
	"context"
	"fmt"
	"testing"

	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
)

func TestManifest(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfgA := LocalChainConfig
	cfgA.Env = []string{
		fmt.Sprintf("OVERRIDE_POA_ADMIN_ADDRESS=%s", accAddr),
	}

	fmt.Printf("cfgA: %+v\n", cfgA)

	cf := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t, zaptest.Level(zapcore.DebugLevel)), []*interchaintest.ChainSpec{
		{
			Name:          "manifest",
			Version:       "local",
			ChainName:     cfgA.ChainID,
			NumValidators: &vals,
			NumFullNodes:  &fullNodes,
			ChainConfig:   cfgA,
		},
		//
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

	// load in the PoA admin to sudo mint via the TokenFactory
	poaAdmin, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "acc0", accMnemonic, DefaultGenesisAmt, appChain)
	if err != nil {
		t.Fatal(err)
	}
	poaAdminAddr := poaAdmin.FormattedAddress()
	t.Logf("poaAdminAddr: %s\n", poaAdminAddr)

	// users := interchaintest.GetAndFundTestUsers(t, ctx, "default", DefaultGenesisAmt, appChain, appChain)
	// user := users[0]
	// uaddr := user.FormattedAddress()

	// user2 := users[1]
	// uaddr2 := user2.FormattedAddress()

	// node := appChain.GetNode()

	t.Cleanup(func() {
		_ = ic.Close()
	})
}
