package interchaintest

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/strangelove-ventures/poa"
	"github.com/stretchr/testify/require"

	"github.com/liftedinit/manifest-ledger/interchaintest/helpers"
)

const (
	// cosmos1hj5fveer5cjtn4wd6wstzugjfdxzl0xpxvjjvr (test_node.sh) accMnemonic
	acc1Mnemonic = "wealth flavor believe regret funny network recall kiss grape useless pepper cram hint member few certain unveil rather brick bargain curious require crowd raise"
	userFunds    = 10_000_000_000
	numVals      = 2
	numNodes     = 0
)

func TestPOA(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// Same as ChainNode.HomeDir() but we need it before the chain is created
	// The node volume is always mounted at /var/cosmos-chain/[chain-name]
	// This is a hackish way to get the coverage files from the ephemeral containers
	name := "poa"
	internalGoCoverDir := path.Join("/var/cosmos-chain", name)

	cfgA := LocalChainConfig
	cfgA.Env = []string{
		fmt.Sprintf("GOCOVERDIR=%s", internalGoCoverDir),
	}

	// setup base chain
	chains := interchaintest.CreateChainWithConfig(t, numVals, numNodes, name, "", cfgA)
	chain := chains[0].(*cosmos.CosmosChain)

	enableBlockDB := false
	ctx, _, client, _ := interchaintest.BuildInitialChain(t, chains, enableBlockDB)

	// Make sure the chain's HomeDir and the GOCOVERDIR are the same
	require.Equal(t, internalGoCoverDir, chain.GetNode().HomeDir())

	// setup accounts
	acc0, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "acc0", accMnemonic, DefaultGenesisAmt, chain)
	if err != nil {
		t.Fatal(err)
	}
	acc1, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "acc1", acc1Mnemonic, DefaultGenesisAmt, chain)
	if err != nil {
		t.Fatal(err)
	}

	users := interchaintest.GetAndFundTestUsers(t, ctx, t.Name(), DefaultGenesisAmt, chain)
	incorrectUser := users[0]

	// get validator operator addresses

	vals, err := chain.StakingQueryValidators(ctx, stakingtypes.Bonded.String())
	require.NoError(t, err)
	require.Equal(t, len(vals), numVals)
	assertSignatures(t, ctx, chain, len(vals))

	validators := make([]string, len(vals))
	for i, v := range vals {
		validators[i] = v.OperatorAddress
	}

	// === Test Cases ===
	testStakingDisabled(t, ctx, chain, validators, acc0, acc1)
	testPowerErrors(t, ctx, chain, validators, incorrectUser, acc0)

	t.Cleanup(func() {
		// Copy coverage files from the container
		CopyCoverageFromContainer(ctx, t, client, chain.GetNode().ContainerID(), chain.HomeDir())
	})
}

// createAuthzJSON generates a JSON file for an authorization message.
// This is a copy of interchaintest/chain/cosmos/module_authz.go#createAuthzJSON with the addition of the chain environment variables to the Exec call
func createAuthzJSON(ctx context.Context, chain *cosmos.CosmosChain, filePath string, genMsgCmd []string) error {
	if !strings.Contains(strings.Join(genMsgCmd, " "), "--generate-only") {
		genMsgCmd = append(genMsgCmd, "--generate-only")
	}

	res, resErr, err := chain.GetNode().Exec(ctx, genMsgCmd, chain.Config().Env)
	if resErr != nil {
		return fmt.Errorf("failed to generate msg: %s", resErr)
	}
	if err != nil {
		return err
	}

	return chain.GetNode().WriteFile(ctx, res, filePath)
}

// ExecTx executes a transaction, waits for 2 blocks if successful, then returns the tx hash.
// This is a copy of interchaintest/chain/cosmos/chain_node.go#ExecTx with the addition of the chain environment variables to the Exec call
func ExecTx(ctx context.Context, chain *cosmos.CosmosChain, keyName string, command ...string) (string, error) {
	stdout, _, err := chain.GetNode().Exec(ctx, chain.GetNode().TxCommand(keyName, command...), chain.Config().Env)
	if err != nil {
		return "", err
	}
	output := cosmos.CosmosTx{}
	err = json.Unmarshal([]byte(stdout), &output)
	if err != nil {
		return "", err
	}
	if output.Code != 0 {
		return output.TxHash, fmt.Errorf("transaction failed with code %d: %s", output.Code, output.RawLog)
	}
	if err := testutil.WaitForBlocks(ctx, 2, chain.GetNode()); err != nil {
		return "", err
	}
	return output.TxHash, nil
}

func testStakingDisabled(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, validators []string, acc0, acc1 ibc.Wallet) {
	t.Log("\n===== TEST STAKING DISABLED =====")

	err := chain.GetNode().StakingDelegate(ctx, acc0.KeyName(), validators[0], "1stake")
	require.Error(t, err)
	require.Contains(t, err.Error(), poa.ErrStakingActionNotAllowed.Error())

	granter := acc1
	grantee := acc0

	// Grant grantee (acc0) the ability to delegate from granter (acc1)
	res, err := chain.GetNode().AuthzGrant(ctx, granter, grantee.FormattedAddress(), "generic", "--msg-type", "/cosmos.staking.v1beta1.MsgDelegate")
	require.NoError(t, err)
	require.EqualValues(t, res.Code, 0)

	// Generate nested message
	nested := []string{"tx", "staking", "delegate", validators[0], "1stake"}
	nestedCmd := helpers.TxCommandBuilder(ctx, chain, nested, granter.FormattedAddress())

	// Execute nested message via a wrapped Exec
	// Workaround AuthzExec which doesn't propagate the environment variables to the Exec call
	fileName := "authz.json"
	err = createAuthzJSON(ctx, chain, fileName, nestedCmd)
	require.NoError(t, err)

	_, err = ExecTx(ctx, chain, grantee.KeyName(), []string{"authz", "exec", path.Join(chain.GetNode().HomeDir(), fileName)}...)
	require.Error(t, err)
	require.ErrorContains(t, err, poa.ErrStakingActionNotAllowed.Error())
}

func testPowerErrors(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, validators []string, incorrectUser ibc.Wallet, admin ibc.Wallet) {
	t.Log("\n===== TEST POWER ERRORS =====")
	var res sdk.TxResponse
	var err error

	t.Run("fail: set-power message from a non authorized user", func(t *testing.T) {
		res, _ = helpers.POASetPower(t, ctx, chain, incorrectUser, validators[1], 1_000_000)
		res, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Contains(t, res.RawLog, poa.ErrNotAnAuthority.Error())
	})

	t.Run("fail: set-power message below minimum power requirement (self bond)", func(t *testing.T) {
		res, err = helpers.POASetPower(t, ctx, chain, admin, validators[0], 1)
		require.Error(t, err) // cli validate error
		require.ErrorContains(t, err, poa.ErrPowerBelowMinimum.Error())
	})
}

// assertSignatures asserts that the current block has the exact number of signatures as expected
func assertSignatures(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, expectedSigs int) {
	height, err := chain.GetNode().Height(ctx)
	require.NoError(t, err)
	block := helpers.GetBlockData(t, ctx, chain, height)
	require.Equal(t, len(block.LastCommit.Signatures), expectedSigs)
}
