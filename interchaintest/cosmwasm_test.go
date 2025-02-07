package interchaintest

import (
    "context"
    "testing"
    "encoding/json"
    "strconv"
    "github.com/strangelove-ventures/interchaintest/v8"
    "github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
    "github.com/stretchr/testify/require"
    "github.com/strangelove-ventures/interchaintest/v8/testreporter"
    "github.com/strangelove-ventures/interchaintest/v8/dockerutil"
    "go.uber.org/zap/zaptest"
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
    users := interchaintest.GetAndFundTestUsers(t, ctx, "default", DefaultGenesisAmt, chain)
    user := users[0]

    var contractAddr string
    var codeId uint64

    // Test contract upload & instantiation
    t.Run("upload contract", func(t *testing.T) {
        // Store contract directly using local file path
        wasmFile := "../scripts/cw_template.wasm"
        t.Logf("Storing contract from local path: %s", wasmFile)
        codeIdStr, err := chain.GetNode().StoreContract(ctx, user.KeyName(), wasmFile)
        require.NoError(t, err)
        t.Logf("Received code ID: %s", codeIdStr)
        codeId, err = strconv.ParseUint(codeIdStr, 10, 64)
        require.NoError(t, err)
        require.Greater(t, codeId, uint64(0))
    })

    t.Run("instantiate contract", func(t *testing.T) {
        // Prepare init message
        initMsg := map[string]interface{}{
            "count": 0,
        }
        initMsgBz, err := json.Marshal(initMsg)
        require.NoError(t, err)
        
        // Instantiate contract with JSON string and required flags
        contractAddr, err = chain.GetNode().InstantiateContract(
            ctx, 
            user.KeyName(), 
            strconv.FormatUint(codeId, 10),
            string(initMsgBz), 
            true,
        )
        require.NoError(t, err)
        require.NotEmpty(t, contractAddr)
    })

    t.Run("query contract info", func(t *testing.T) {
        queryMsg := map[string]interface{}{
            "get_count": struct{}{},
        }
        queryMsgBz, err := json.Marshal(queryMsg)
        require.NoError(t, err)
        
        var resp struct {
            Count int `json:"count"`
        }
        err = chain.QueryContract(ctx, contractAddr, string(queryMsgBz), &resp)
        require.NoError(t, err)
        require.Equal(t, 0, resp.Count)
    })

    t.Run("increment and query count", func(t *testing.T) {
        // Test contract execute
        executeMsg := map[string]interface{}{
            "increment": struct{}{},
        }
        executeMsgBz, err := json.Marshal(executeMsg)
        require.NoError(t, err)
        
        _, err = chain.ExecuteContract(ctx, user.KeyName(), contractAddr, string(executeMsgBz))
        require.NoError(t, err)

        // Query again to verify execution
        queryMsg := map[string]interface{}{
            "get_count": struct{}{},
        }
        queryMsgBz, err := json.Marshal(queryMsg)
        require.NoError(t, err)

        var resp struct {
            Count int `json:"count"`
        }
        err = chain.QueryContract(ctx, contractAddr, string(queryMsgBz), &resp)
        require.NoError(t, err)
        require.Equal(t, 0, resp.Count)
    })

    t.Cleanup(func() {
        dockerutil.CopyCoverageFromContainer(ctx, t, client, chain.GetNode().ContainerID(), chain.HomeDir(), ExternalGoCoverDir)
        _ = ic.Close()
    })
}