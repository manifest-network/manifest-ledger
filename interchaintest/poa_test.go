package interchaintest

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/dockerutil"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/poa"
	"github.com/stretchr/testify/require"

	"github.com/manifest-network/manifest-ledger/interchaintest/helpers"
)

const (
	// cosmos1hj5fveer5cjtn4wd6wstzugjfdxzl0xpxvjjvr (test_node.sh) accMnemonic
	acc1Mnemonic = "wealth flavor believe regret funny network recall kiss grape useless pepper cram hint member few certain unveil rather brick bargain curious require crowd raise"
	numVals      = 2
	numNodes     = 0
)

func TestPOA(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	name := "poa"
	cfgA := LocalChainConfig
	cfgA.Name = name
	cfgA.WithCodeCoverage()

	// setup base chain
	chains := interchaintest.CreateChainWithConfig(t, numVals, numNodes, name, "", cfgA)
	chain := chains[0].(*cosmos.CosmosChain)

	enableBlockDB := false
	ctx, ic, client, _ := interchaintest.BuildInitialChain(t, chains, enableBlockDB)

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
		dockerutil.CopyCoverageFromContainer(ctx, t, client, chain.GetNode().ContainerID(), chain.HomeDir(), ExternalGoCoverDir)
		_ = ic.Close()
	})
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

	_, err = chain.GetNode().AuthzExec(ctx, grantee, nestedCmd)
	require.Error(t, err)
	require.ErrorContains(t, err, poa.ErrStakingActionNotAllowed.Error())
}

func testPowerErrors(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, validators []string, incorrectUser ibc.Wallet, admin ibc.Wallet) {
	t.Log("\n===== TEST POWER ERRORS =====")
	var res sdk.TxResponse
	var err error

	t.Run("fail: set-power message from a non authorized user", func(t *testing.T) {
		res, _ = helpers.POASetPower(ctx, chain, incorrectUser, validators[1], 1_000_000)
		res, err := chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Contains(t, res.RawLog, poa.ErrNotAnAuthority.Error())
	})

	t.Run("fail: set-power message below minimum power requirement (self bond)", func(t *testing.T) {
		res, err = helpers.POASetPower(ctx, chain, admin, validators[0], 1)
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
