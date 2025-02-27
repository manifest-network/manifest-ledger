package interchaintest

import (
	"context"
	"testing"

	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/dockerutil"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

type GetCountResponse struct {
	Data struct {
		Count int `json:"count"`
	} `json:"data"`
}

const wasmFile = "../scripts/cw_template.wasm"

func TestCosmWasm(t *testing.T) {
	ctx := context.Background()

	cfg := LocalChainConfig
	cfg.Name = "manifest-2"
	cfg.WithCodeCoverage()

	// Setup chain
	chains, err := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t), []*interchaintest.ChainSpec{
		{
			Name:          "manifest",
			Version:       "local",
			ChainName:     cfg.ChainID,
			NumValidators: &vals,
			NumFullNodes:  &fullNodes,
			ChainConfig:   cfg,
		},
	}).Chains(t.Name())
	require.NoError(t, err)

	chain := chains[0].(*cosmos.CosmosChain)

	// Setup client and network
	client, network := interchaintest.DockerSetup(t)

	ic := interchaintest.NewInterchain().
		AddChain(chain)

	rep := testreporter.NewNopReporter()
	eRep := rep.RelayerExecReporter(t)

	// Build interchain
	require.NoError(t, ic.Build(ctx, eRep, interchaintest.InterchainBuildOptions{
		TestName:         t.Name(),
		Client:           client,
		NetworkID:        network,
		SkipPathCreation: true,
	}))

	// Get test users
	user1Wallet, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "user1", accMnemonic, DefaultGenesisAmt, chain)
	require.NoError(t, err)

	var contractAddr string
	var codeId string

	// Test contract upload & instantiation
	t.Run("upload contract", func(t *testing.T) {
		// Store contract directly using local file path
		wasmFile := "../scripts/cw_template.wasm"
		t.Logf("Storing contract from local path: %s", wasmFile)
		codeIdStr, err := chain.GetNode().StoreContract(ctx, user1Wallet.KeyName(), wasmFile)
		codeId = codeIdStr
		require.NoError(t, err)
		t.Logf("Received code ID: %s", codeId)
	})

	t.Run("instantiate contract", func(t *testing.T) {
		contractAddr, err = chain.GetNode().InstantiateContract(
			ctx,
			user1Wallet.KeyName(),
			codeId,
			`{"count":1}`,
			true,
		)
		require.NoError(t, err)
		require.NotEmpty(t, contractAddr)
	})

	t.Run("query contract info", func(t *testing.T) {
		var resp GetCountResponse
		err = chain.QueryContract(ctx, contractAddr, `{"get_count":{}}`, &resp)
		require.NoError(t, err)
		require.Equal(t, 1, resp.Data.Count)
	})

	t.Run("increment and query count", func(t *testing.T) {
		_, err := chain.ExecuteContract(ctx, user1Wallet.KeyName(), contractAddr, `{"increment":{}}`)
		require.NoError(t, err)

		// Query again to verify execution
		var resp GetCountResponse
		err = chain.QueryContract(ctx, contractAddr, `{"get_count":{}}`, &resp)
		require.NoError(t, err)
		require.Equal(t, 2, resp.Data.Count)
	})

	t.Cleanup(func() {
		dockerutil.CopyCoverageFromContainer(ctx, t, client, chain.GetNode().ContainerID(), chain.HomeDir(), ExternalGoCoverDir)
		_ = ic.Close()
	})
}
