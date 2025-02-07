package interchaintest

import (
    "context"
    "testing"
    "encoding/json"

    "github.com/strangelove-ventures/interchaintest/v8"
    "github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
    "github.com/stretchr/testify/require"
)

func TestCosmWasm(t *testing.T) {
    ctx := context.Background()

    cfg := LocalChainConfig
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

    // Get test users
    users := interchaintest.GetAndFundTestUsers(t, ctx, "default", DefaultGenesisAmt, chain)
    user := users[0]

    // Test contract upload & instantiation
    t.Run("upload and instantiate contract", func(t *testing.T) {
        // Add contract upload & instantiation tests
    })
}