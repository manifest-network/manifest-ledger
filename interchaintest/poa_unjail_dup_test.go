package interchaintest

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/dockerutil"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/manifest-network/manifest-ledger/interchaintest/helpers"
)

// ENG-288 regression test.
//
// Reproduces the "unjail-after-full-unbond duplicate-validator-update" halt: a PoA-managed
// validator that is re-powered across block boundaries can leave a stale power-index key in
// x/staking. When the validator is later jailed (via downtime) and then self-unjails,
// EndBlock's power iterator visits both the stale and the current power-index keys, emitting
// two ABCIValidatorUpdate entries for the SAME consensus pubkey. CometBFT rejects the
// duplicate and the chain halts.
//
// The test is image-parameterized:
//   - default image (manifest:local, current/fixed branch): the chain MUST keep producing
//     blocks past the unjail. "FIX VERIFIED".
//   - UNJAIL_DUP_IMAGE set (e.g. ghcr.io/manifest-network/manifest-ledger:2.1.1, the buggy
//     release): the chain MUST halt at the unjail. "BUG REPRODUCED".
//
// Run:
//   make ictest-poa-unjail-dup        # fix path, manifest:local, expect PASS via progression
//   make ictest-poa-unjail-dup-bug    # bug path, ghcr 2.1.1, expect PASS via halt detection

const (
	// progressBlocks is how many blocks past the unjail we require the chain to advance
	// (fixed image) or fail to advance (buggy image).
	progressBlocks = 5

	// progressBudget is the wall-clock window in which the chain must produce progressBlocks
	// blocks past the unjail. With ~2s blocks, 5 blocks take ~10s; 60s leaves generous slack for
	// CI jitter while still failing fast on a halt.
	progressBudget = 60 * time.Second

	// victimHighPower / victimLowPower are two DISTINCT powers assigned to the victim across
	// block boundaries. They must differ so the PoA BeginBlocker's deferred power-index delete
	// (recomputed from CURRENT tokens) cannot match and remove the earlier key, leaving an
	// orphan power-index entry. victimHighPower is >30% of total so it requires --unsafe.
	victimHighPower = int64(5_000_000)
	victimLowPower  = int64(2_000_000)

	unjailValKey = "validator" // interchaintest valKey: the self-signed unjail key name on each val node
)

// resolveTestImage selects the docker image under test. With no env override it uses the
// locally built manifest:local image (current branch, fix). With UNJAIL_DUP_IMAGE set it uses
// the referenced image (e.g. the buggy ghcr release).
func resolveTestImage() ibc.DockerImage {
	if ref := os.Getenv("UNJAIL_DUP_IMAGE"); ref != "" {
		repo, version, found := strings.Cut(ref, ":")
		if !found {
			version = "latest"
		}
		return ibc.DockerImage{Repository: repo, Version: version, UIDGID: "1025:1025"}
	}
	return ibc.DockerImage{Repository: "manifest", Version: "local", UIDGID: "1025:1025"}
}

// isBuggyImage reports whether we are running against an externally pinned (buggy) image rather
// than the locally built fix. Coverage instrumentation only exists in manifest:local, so the
// buggy path skips coverage copy and flips the halt/progress expectation.
func isBuggyImage() bool { return os.Getenv("UNJAIL_DUP_IMAGE") != "" }

func TestPOAUnjailDup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	ctx := context.Background()

	img := resolveTestImage()
	t.Logf("running against image %s:%s (buggy=%v)", img.Repository, img.Version, isBuggyImage())

	// Clone the default genesis and tighten the slashing params so downtime jailing happens
	// within a handful of blocks and JailedUntil elapses quickly. This keeps the test fast and
	// deterministic instead of waiting out the default 100-block window + 10m jail duration.
	genesis := append([]cosmos.GenesisKV{}, DefaultGenesis...)
	genesis = append(genesis,
		cosmos.NewGenesisKV("app_state.slashing.params.signed_blocks_window", "10"),
		cosmos.NewGenesisKV("app_state.slashing.params.min_signed_per_window", "0.500000000000000000"),
		cosmos.NewGenesisKV("app_state.slashing.params.downtime_jail_duration", "10s"),
		// keep the downtime slash fraction tiny so the victim retains enough tokens to be
		// re-powered after the jail without tripping the PoA minimum-power guard.
		cosmos.NewGenesisKV("app_state.slashing.params.slash_fraction_downtime", "0.000000000000000000"),
	)

	cfg := LocalChainConfig
	cfg.Name = "poa-unjail-dup"
	cfg.Images = []ibc.DockerImage{img}
	cfg.ModifyGenesis = cosmos.ModifyGenesis(genesis)
	// NOTE: do NOT call cfg.WithCodeCoverage(): coverage instrumentation only exists in
	// manifest:local; the buggy ghcr image is not instrumented.

	chains, err := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t), []*interchaintest.ChainSpec{
		{
			Name:          cfg.Name,
			Version:       img.Version,
			ChainName:     cfg.ChainID,
			NumValidators: &vals,
			NumFullNodes:  &fullNodes,
			ChainConfig:   cfg,
		},
	}).Chains(t.Name())
	require.NoError(t, err)

	chain := chains[0].(*cosmos.CosmosChain)

	client, network := interchaintest.DockerSetup(t)

	ic := interchaintest.NewInterchain().AddChain(chain)

	rep := testreporter.NewNopReporter()
	eRep := rep.RelayerExecReporter(t)

	require.NoError(t, ic.Build(ctx, eRep, interchaintest.InterchainBuildOptions{
		TestName:         t.Name(),
		Client:           client,
		NetworkID:        network,
		SkipPathCreation: true,
	}))

	t.Cleanup(func() {
		// Coverage only exists in the locally built (fix) image.
		if !isBuggyImage() {
			dockerutil.CopyCoverageFromContainer(ctx, t, client, chain.GetNode().ContainerID(), chain.HomeDir(), ExternalGoCoverDir)
		}
		_ = ic.Close()
	})

	// Fund the PoA admin (acc0 from accMnemonic; accAddr is wired as POA_ADMIN_ADDRESS).
	admin, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "acc0", accMnemonic, DefaultGenesisAmt, chain)
	require.NoError(t, err)

	// === Resolve validators ===
	queried, err := chain.StakingQueryValidators(ctx, stakingtypes.Bonded.String())
	require.NoError(t, err)
	require.Equal(t, numVals, len(queried))

	validators := make([]string, len(queried))
	for i, v := range queried {
		validators[i] = v.OperatorAddress
	}

	// The victim must NOT be chain.Validators[0]: ALL chain-level queries and RPC
	// (chain.Height, StakingQueryValidator, SlashingQuerySigningInfo, WaitForBlocks) route
	// through GetFullNode() -> GetNode() -> Validators[0]. With fullNodes=0 there is no separate
	// query node, so if we pause Validators[0] every poll/height read would hit a frozen node and
	// the test would stall (not because of the bug). Pick the victim as the operator backed by a
	// node OTHER than Validators[0]; pausing it leaves the query/RPC node (Validators[0]) healthy.
	victim, victimNode := selectVictim(t, ctx, chain)
	t.Logf("victim valoper=%s resolved to node %s (query node=%s)", victim, victimNode.Name(), chain.Validators[0].Name())

	// === Step 1: bump victim to a high distinct power (registers power-index key K1). ===
	// 5_000_000 of ~6_000_000 total is >30%, so --unsafe is required by the PoA guard.
	t.Log("step 1: set victim to high power")
	_, err = helpers.POASetPower(ctx, chain, admin, victim, victimHighPower, "--unsafe")
	require.NoError(t, err)

	// === Step 2: change victim to a DIFFERENT power -> new key K2; K1 becomes an orphan. ===
	t.Log("step 2: set victim to a different power (orphans the prior power-index key)")
	_, err = helpers.POASetPower(ctx, chain, admin, victim, victimLowPower)
	require.NoError(t, err)

	// With the victim now at victimLowPower, Validators[0] must still hold >2/3 of voting power so
	// that pausing the victim (step 3) does NOT stall consensus — otherwise the chain freezes and
	// the test hangs at waitForJailed instead of reproducing the bug. Fail fast if that invariant
	// is ever broken by a future tweak to the power constants / self-bond / vals.
	assertVictimNotQuorumCritical(t, ctx, chain, victim)

	// === Step 3: jail the victim via downtime. ===
	t.Log("step 3: pause victim node to trigger downtime jailing")
	require.NoError(t, victimNode.PauseContainer(ctx))

	consAddr := waitForJailed(t, ctx, chain, victim)
	t.Logf("victim jailed; consAddr=%s", consAddr)

	// Unpause the node so it can sign again and self-unjail. (PauseContainer/UnpauseContainer
	// are a Docker pause/unpause pair; StartContainer is for a STOPPED container and refuses a
	// paused one with "cannot start a paused container".)
	t.Log("step 3: unpause victim node")
	require.NoError(t, victimNode.UnpauseContainer(ctx))

	// === Step 4: wait for JailedUntil to elapse so unjail is permitted. ===
	t.Log("step 4: wait for JailedUntil to elapse")
	waitForJailExpiry(t, ctx, chain, consAddr)

	// === Step 5: unjail -> on the buggy image this emits a duplicate validator update. ===
	t.Log("step 5: self-unjail victim")
	startHeight := heightWithin(t, ctx, chain, 15*time.Second)
	require.Greater(t, startHeight, int64(0), "could not read start height before unjail")

	// Broadcast the self-unjail WITHOUT waiting for block inclusion. The interchaintest
	// ChainNode.SlashingUnJail -> ExecTx helper waits 2 blocks after broadcast to confirm the tx,
	// but on the buggy image the unjail is the tx that HALTS the chain (its EndBlock emits the
	// duplicate validator update and CometBFT panics), so those confirmation blocks never arrive
	// and ExecTx would hang forever. We use --broadcast-mode sync (returns after CheckTx, before
	// inclusion) and run the command directly so the call returns regardless of the subsequent
	// halt; the halt-vs-progress decision is made by polling height below.
	unjailCmd := victimNode.TxCommand(unjailValKey, "slashing", "unjail", "--broadcast-mode", "sync")
	stdout, stderr, err := victimNode.Exec(ctx, unjailCmd, chain.Config().Env)
	require.NoError(t, err, "broadcasting unjail failed (stderr=%s)", string(stderr))
	t.Logf("unjail broadcast (sync) result: %s", strings.TrimSpace(string(stdout)))

	// === Assertion: halt (buggy) vs progress (fixed). ===
	//
	// We do NOT use testutil.WaitForBlocks here: on the buggy image the chain panics in EndBlock
	// applying the duplicate validator update, and the halted node's /status RPC then blocks the
	// HTTP request without honoring the request context — so WaitForBlocks hangs past its own
	// deadline. Instead we poll the height ourselves with a per-read timeout and a hard
	// wall-clock budget, treating "no advancement within the budget" as a halt. Each read is
	// independently bounded, so a wedged RPC can never hang the test.
	advanced := waitForProgressOrHalt(t, ctx, chain, startHeight, progressBlocks, progressBudget)

	if isBuggyImage() {
		require.Less(t, advanced, int64(progressBlocks),
			"expected halt; chain advanced %d blocks from height %d within %s", advanced, startHeight, progressBudget)

		// Confirm the halt is THIS bug (a duplicate consensus key in the EndBlock validator-update
		// set), not an unrelated stall. CometBFT's processChanges panics with "duplicate entry ...";
		// every validator that applies the offending block panics, so scan all node logs.
		dupConfirmed := false
		for _, n := range chain.Validators {
			if nodeLogContains(t, n, "duplicate entry") {
				dupConfirmed = true
				t.Logf("confirmed CometBFT duplicate-entry panic in node %s logs", n.Name())
				break
			}
		}
		require.True(t, dupConfirmed,
			"chain halted but no node logged the CometBFT \"duplicate entry\" panic; the halt may be unrelated to ENG-288")

		t.Logf("BUG REPRODUCED: chain halted at the unjail (duplicate validator-update), advanced only %d blocks within %s", advanced, progressBudget)
	} else {
		require.GreaterOrEqual(t, advanced, int64(progressBlocks),
			"expected progression; advanced only %d blocks from height %d within %s", advanced, startHeight, progressBudget)
		t.Logf("FIX VERIFIED: chain progressed %d blocks past the unjail (no duplicate-update halt)", advanced)
	}
}

// assertVictimNotQuorumCritical fails fast if pausing the victim would break >2/3 voting power.
// With vals=2 and fullNodes=0, the chain keeps producing blocks while the victim is paused ONLY
// because the victim was just lowered to victimLowPower while Validators[0] keeps its much larger
// default self-bond — so Validators[0] alone holds >2/3. If a future change to victimLowPower, the
// gentx self-bond, or `vals` violates that, the chain would stall on the pause and this test would
// hang at waitForJailed instead of reproducing the bug; this guard turns that silent hang into a
// clear failure. Voting power is proportional to bonded Tokens, so we compare Tokens.
func assertVictimNotQuorumCritical(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, victim string) {
	t.Helper()
	bonded, err := chain.StakingQueryValidators(ctx, stakingtypes.Bonded.String())
	require.NoError(t, err)

	total := sdkmath.ZeroInt()
	victimTokens := sdkmath.ZeroInt()
	for _, v := range bonded {
		total = total.Add(v.Tokens)
		if v.OperatorAddress == victim {
			victimTokens = v.Tokens
		}
	}
	require.False(t, victimTokens.IsZero(), "victim %s not found among bonded validators", victim)
	// Remaining power after pausing the victim is total-victim; it must exceed 2/3 of total, i.e.
	// victim must be < 1/3 of total, i.e. victim*3 < total.
	require.True(t, victimTokens.MulRaw(3).LT(total),
		"victim power %s is >= 1/3 of total bonded power %s; pausing it would break 2/3 quorum and "+
			"stall the chain (the test would hang, not reproduce the bug). Lower victimLowPower or raise "+
			"the non-victim validators' power.", victimTokens.String(), total.String())
}

// nodeLogContains reports whether the node container's logs contain needle. Best-effort via the
// docker CLI (always present where these ictests run, and it honors DOCKER_HOST); a fetch error is
// logged and treated as "not found". Works on exited/panicked containers too.
func nodeLogContains(t *testing.T, node *cosmos.ChainNode, needle string) bool {
	t.Helper()
	out, err := exec.Command("docker", "logs", node.ContainerID()).CombinedOutput()
	if err != nil {
		t.Logf("could not read docker logs for %s: %v", node.Name(), err)
		return false
	}
	return strings.Contains(string(out), needle)
}

// waitForProgressOrHalt polls the chain height until it advances by at least want blocks from
// start, or until budget elapses, and returns the number of blocks advanced (>= 0). Each height
// read is independently time-bounded so a halted node whose RPC has wedged cannot hang the test;
// a failed/timed-out read is treated as "no new height" and the loop keeps sampling until the
// budget is spent. On a healthy chain this returns as soon as want blocks are produced; on a
// halted chain it returns ~0 after the budget.
func waitForProgressOrHalt(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, start, want int64, budget time.Duration) int64 {
	t.Helper()
	deadline := time.Now().Add(budget)
	var advanced int64
	for time.Now().Before(deadline) {
		h := heightWithin(t, ctx, chain, 8*time.Second)
		if h > start {
			advanced = h - start
			if advanced >= want {
				return advanced
			}
		}
		select {
		case <-ctx.Done():
			return advanced
		case <-time.After(2 * time.Second):
		}
	}
	return advanced
}

// heightWithin reads the chain height but never blocks longer than d, even if the underlying
// CometBFT RPC ignores its context deadline. On the buggy image the chain panics in EndBlock
// applying the duplicate validator update, and the halted node's /status HTTP call can wedge
// without honoring the request context — so we run the read in a goroutine and race it against a
// hard timer. A timed-out or failed read returns 0; the caller compares start vs end heights, so
// a halt naturally shows as little/no advancement, which the buggy-branch assertion requires.
func heightWithin(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, d time.Duration) int64 {
	t.Helper()
	hctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()

	type result struct {
		h   int64
		err error
	}
	ch := make(chan result, 1)
	go func() {
		h, err := chain.Height(hctx)
		ch <- result{h: h, err: err}
	}()

	select {
	case <-time.After(d):
		t.Logf("height read did not return within %s (chain RPC may be wedged); treating as no height", d)
		return 0
	case r := <-ch:
		if r.err != nil {
			t.Logf("height read failed within %s (chain may be halted): %v", d, r.err)
			return 0
		}
		return r.h
	}
}

// selectVictim picks a victim operator backed by a node OTHER than chain.Validators[0] and
// returns both the operator (valoper) address and its node. Validators[0] is the chain's
// query/RPC node (GetFullNode -> GetNode -> Validators[0]); the victim must not be it, or
// pausing the victim would freeze all chain queries. Each node's valoper is resolved from its
// own validator key (`keys show validator --bech val`) so we never rely on the Validators slice
// order matching the StakingQueryValidators order.
func selectVictim(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain) (string, *cosmos.ChainNode) {
	t.Helper()
	queryNode := chain.Validators[0]
	for _, node := range chain.Validators {
		if node == queryNode {
			continue
		}
		valoper, err := node.KeyBech32(ctx, unjailValKey, "val")
		require.NoError(t, err)
		require.NotEmpty(t, valoper, "node %s returned empty valoper", node.Name())
		return valoper, node
	}
	t.Fatalf("need at least 2 validators so the victim is not the query node (Validators[0])")
	return "", nil
}

// waitForJailed polls until the victim validator reports Jailed=true, then returns its
// consensus address (for slashing signing-info queries). The poll is bounded.
func waitForJailed(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, victim string) string {
	t.Helper()

	const maxBlocks = 40
	for i := 0; i < maxBlocks; i++ {
		val, err := chain.StakingQueryValidator(ctx, victim)
		if err == nil && val.Jailed {
			return victimConsAddr(t, ctx, chain, victim)
		}
		if err := testutil.WaitForBlocks(ctx, 1, chain); err != nil {
			t.Fatalf("error waiting for blocks while polling for jail: %v", err)
		}
	}
	t.Fatalf("victim %s was not jailed within %d blocks", victim, maxBlocks)
	return ""
}

// victimConsAddr returns the consensus address of the victim by scanning all slashing signing
// infos and matching against the one belonging to the jailed validator. With a 2-val chain
// where validators[0] stays healthy, the jailed signing info uniquely identifies the victim.
func victimConsAddr(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, victim string) string {
	t.Helper()

	infos, err := chain.SlashingQuerySigningInfos(ctx)
	require.NoError(t, err)

	// The jailed victim has a JailedUntil set in the (recent) past or future; a never-jailed
	// validator keeps the zero time (year 1). With validators[0] healthy, exactly one signing
	// info carries a non-zero JailedUntil: the victim's.
	var candidate string
	for _, info := range infos {
		if info.JailedUntil.After(time.Unix(0, 0)) {
			candidate = info.Address
		}
	}
	require.NotEmpty(t, candidate, "could not resolve victim consensus address from signing infos")
	return candidate
}

// waitForJailExpiry polls SlashingQuerySigningInfo until JailedUntil is in the past relative to
// the latest block time, so the self-unjail tx will be accepted.
func waitForJailExpiry(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, consAddr string) {
	t.Helper()

	const maxBlocks = 40
	for i := 0; i < maxBlocks; i++ {
		info, err := chain.SlashingQuerySigningInfo(ctx, consAddr)
		require.NoError(t, err)
		if time.Now().After(info.JailedUntil) {
			return
		}
		if err := testutil.WaitForBlocks(ctx, 1, chain); err != nil {
			t.Fatalf("error waiting for blocks while polling for jail expiry: %v", err)
		}
	}
	t.Fatalf("jail did not expire within %d blocks for cons %s", maxBlocks, consAddr)
}
