package interchaintest

import (
	"context"
	"testing"

	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
)

func TestTokenFactory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfgA := LocalChainConfig

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

	// load in the PoA admin to sudo mint via the TokenFactory
	poaAdmin, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "acc0", accMnemonic, DefaultGenesisAmt, appChain)
	if err != nil {
		t.Fatal(err)
	}
	poaAdminAddr := poaAdmin.FormattedAddress()

	users := interchaintest.GetAndFundTestUsers(t, ctx, "default", DefaultGenesisAmt, appChain, appChain)
	user := users[0]
	uaddr := user.FormattedAddress()

	user2 := users[1]
	uaddr2 := user2.FormattedAddress()

	node := appChain.GetNode()

	tfDenom, _, err := node.TokenFactoryCreateDenom(ctx, user, "ictestdenom", 2_500_00)
	t.Log("TF Denom: ", tfDenom)
	require.NoError(t, err)

	t.Log("Mint TF Denom to user")
	node.TokenFactoryMintDenom(ctx, user.FormattedAddress(), tfDenom, 100)
	if balance, err := appChain.GetBalance(ctx, uaddr, tfDenom); err != nil {
		t.Fatal(err)
	} else if balance.Int64() != 100 {
		t.Fatal("balance not 100")
	}

	t.Log("SudoMint from PoA Admin - Manifest specific")
	sudoToken, mintAmt := "unique", uint64(7) // we mint unique token instead of umfx incase that is used in the cfg

	prevBal, err := appChain.GetBalance(ctx, poaAdminAddr, sudoToken)
	require.NoError(t, err)

	txhash, err := node.TokenFactoryMintDenom(ctx, poaAdmin.FormattedAddress(), sudoToken, mintAmt)
	require.NoError(t, err)
	t.Log("SudoMint txhash", txhash)

	expectedBalance := prevBal.AddRaw(int64(mintAmt))
	newBal, err := appChain.GetBalance(ctx, poaAdminAddr, sudoToken)
	require.NoError(t, err)
	if !expectedBalance.Equal(newBal) {
		t.Fatalf("expected balance %s, got %s", expectedBalance, newBal)
	}

	t.Log("SudoMint from non PoA Admin (fail) - Manifest specific")
	node.TokenFactoryMintDenom(ctx, user.FormattedAddress(), sudoToken, 7)
	if balance, err := appChain.GetBalance(ctx, uaddr, sudoToken); err != nil {
		t.Fatal(err)
	} else if balance.Int64() != 0 {
		res, err := node.TxHashToResponse(ctx, txhash)
		t.Fatal("balance not 0", err, res)
	}

	t.Log("Mint TF Denom to another user")
	node.TokenFactoryMintDenomTo(ctx, user.FormattedAddress(), tfDenom, 70, user2.FormattedAddress())
	if balance, err := appChain.GetBalance(ctx, uaddr2, tfDenom); err != nil {
		t.Fatal(err)
	} else if balance.Int64() != 70 {
		t.Fatal("balance not 70")
	}

	// change admin to uaddr2
	_, err = node.TokenFactoryChangeAdmin(ctx, user.KeyName(), tfDenom, uaddr2)
	require.NoError(t, err)

	// ensure the admin is the contract
	admin, err := appChain.TokenFactoryQueryAdmin(ctx, tfDenom)
	t.Log("admin", admin)
	require.NoError(t, err)
	if admin.AuthorityMetadata.Admin != uaddr2 {
		t.Fatal("admin not uaddr2. Did not properly transfer.")
	}

	t.Cleanup(func() {
		_ = ic.Close()
	})
}
