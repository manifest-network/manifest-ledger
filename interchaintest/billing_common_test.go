// Package interchaintest contains end-to-end tests for the billing module.
// This file contains shared setup and test context for billing e2e tests.
package interchaintest

import (
	"context"
	"fmt"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/dockerutil"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/stretchr/testify/require"

	"github.com/manifest-network/manifest-ledger/interchaintest/helpers"
	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
)

// Package-level variables for backward compatibility with test functions
// that reference these globals. Each test file sets these from billingTestContext.
var (
	testPWRDenom     string
	testProviderUUID string
	testSKUUUID      string
	testSKUUUID2     string
)

// billingTestContext holds shared test state for billing e2e tests.
type billingTestContext struct {
	chain            *cosmos.CosmosChain
	authority        ibc.Wallet
	providerWallet   ibc.Wallet
	tenant1          ibc.Wallet
	tenant2          ibc.Wallet
	unauthorizedUser ibc.Wallet
	pwrDenom         string
	providerUUID     string
	skuUUID          string // per-hour pricing SKU
	skuUUID2         string // per-day pricing SKU
}

// setupBillingTest creates chain and infrastructure for billing tests.
// Returns context, test context, and cleanup function.
func setupBillingTest(t *testing.T, testName string) (context.Context, *billingTestContext, func()) {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// Setup chain
	cfgA := LocalChainConfig
	cfgA.Name = testName
	cfgA.WithCodeCoverage()

	chains := interchaintest.CreateChainWithConfig(t, vals, fullNodes, testName, "", cfgA)
	chain := chains[0].(*cosmos.CosmosChain)

	enableBlockDB := false
	ctx, ic, client, _ := interchaintest.BuildInitialChain(t, chains, enableBlockDB)

	// Setup accounts
	authority, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "authority", accMnemonic, DefaultGenesisAmt, chain)
	require.NoError(t, err)

	// Regular users (tenants)
	users := interchaintest.GetAndFundTestUsers(t, ctx, t.Name(), DefaultGenesisAmt, chain, chain, chain, chain)
	tenant1 := users[0]
	tenant2 := users[1]
	providerWallet := users[2]
	unauthorizedUser := users[3]

	tc := &billingTestContext{
		chain:            chain,
		authority:        authority,
		providerWallet:   providerWallet,
		tenant1:          tenant1,
		tenant2:          tenant2,
		unauthorizedUser: unauthorizedUser,
	}

	// Setup test infrastructure (PWR denom, provider, SKUs)
	setupBillingInfrastructure(t, ctx, tc)

	// Return cleanup function
	cleanup := func() {
		dockerutil.CopyCoverageFromContainer(ctx, t, client, chain.GetNode().ContainerID(), chain.HomeDir(), ExternalGoCoverDir)
		_ = ic.Close()
	}

	return ctx, tc, cleanup
}

// setupBillingInfrastructure creates the PWR denom, provider, and SKUs needed for tests.
func setupBillingInfrastructure(t *testing.T, ctx context.Context, tc *billingTestContext) {
	t.Helper()
	t.Log("=== Setting up Billing Test Infrastructure ===")

	node := tc.chain.GetNode()
	var err error

	// Create PWR denom via tokenfactory
	t.Log("Creating PWR denom...")
	tc.pwrDenom, _, err = node.TokenFactoryCreateDenom(ctx, tc.authority, "upwr", 2_500_00)
	require.NoError(t, err, "failed to create PWR denom")
	t.Logf("Created PWR denom: %s", tc.pwrDenom)

	// Mint PWR tokens to authority for distribution
	t.Log("Minting PWR tokens to authority...")
	_, err = node.TokenFactoryMintDenom(ctx, tc.authority.FormattedAddress(), tc.pwrDenom, 1_000_000_000_000)
	require.NoError(t, err, "failed to mint PWR tokens")

	balance, err := tc.chain.GetBalance(ctx, tc.authority.FormattedAddress(), tc.pwrDenom)
	require.NoError(t, err)
	require.True(t, balance.GT(sdkmath.ZeroInt()), "authority should have PWR balance")
	t.Logf("Authority PWR balance: %s", balance)

	// Create provider
	t.Log("Creating provider...")
	res, err := helpers.SKUCreateProvider(ctx, tc.chain, tc.authority, tc.providerWallet.FormattedAddress(), tc.providerWallet.FormattedAddress(), "")
	require.NoError(t, err, "failed to create provider")

	txRes, err := tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "provider creation should succeed: %s", txRes.RawLog)

	tc.providerUUID, err = helpers.GetProviderUUIDFromTxHash(ctx, tc.chain, res.TxHash)
	require.NoError(t, err)
	t.Logf("Created provider UUID: %s", tc.providerUUID)

	// Create SKU with per-hour pricing (Unit = 1)
	// Price: 3600000 upwr per hour = 1000 per second
	t.Log("Creating SKU (per-hour)...")
	res, err = helpers.SKUCreateSKU(ctx, tc.chain, tc.authority, tc.providerUUID, "Compute Small", 1, fmt.Sprintf("3600000%s", tc.pwrDenom), "")
	require.NoError(t, err, "failed to create per-hour SKU")

	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "SKU creation should succeed: %s", txRes.RawLog)

	tc.skuUUID, err = helpers.GetSKUUUIDFromTxHash(ctx, tc.chain, res.TxHash)
	require.NoError(t, err)
	t.Logf("Created SKU UUID (per-hour): %s", tc.skuUUID)

	// Create SKU with per-day pricing (Unit = 2)
	// Price: 86400000 upwr per day = 1000 per second
	t.Log("Creating SKU (per-day)...")
	res, err = helpers.SKUCreateSKU(ctx, tc.chain, tc.authority, tc.providerUUID, "Storage Large", 2, fmt.Sprintf("86400000%s", tc.pwrDenom), "")
	require.NoError(t, err, "failed to create per-day SKU")

	txRes, err = tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "SKU creation should succeed: %s", txRes.RawLog)

	tc.skuUUID2, err = helpers.GetSKUUUIDFromTxHash(ctx, tc.chain, res.TxHash)
	require.NoError(t, err)
	t.Logf("Created SKU UUID (per-day): %s", tc.skuUUID2)

	// Wait for blocks to settle
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))
}

// streamKey identifies a settlement stream: one per (provider, tenant, denom)
// with at least one ACTIVE lease.
type streamKey struct {
	providerUUID string
	tenant       string
	denom        string
}

// snapshotProviderStreams returns every stream of a provider, by identity.
func snapshotProviderStreams(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, providerUUID string) map[streamKey]helpers.StreamStateJSON {
	t.Helper()

	res, err := helpers.BillingQueryStreamsByProvider(ctx, chain, providerUUID)
	require.NoError(t, err)
	require.Empty(t, res.GetNextKeyString(), "provider %s has more streams than one query page", providerUUID)

	streams := make(map[streamKey]helpers.StreamStateJSON, len(res.Streams))
	for _, stream := range res.Streams {
		require.Equal(t, providerUUID, stream.ProviderUuid, "streams-by-provider returned another provider's stream")
		key := streamKey{providerUUID: stream.ProviderUuid, tenant: stream.Tenant, denom: stream.Denom}
		require.NotContains(t, streams, key, "duplicate stream")

		// The remainder is a fraction of one unit of the denom, in units × nanoseconds.
		require.False(t, stream.State.Remainder.IsNil(), "stream %+v has no remainder", key)
		require.False(t, stream.State.Remainder.IsNegative(), "stream %+v remainder %s", key, stream.State.Remainder)
		require.True(t, stream.State.Remainder.LT(sdkmath.NewInt(helpers.NanosPerSecond)),
			"stream %+v remainder %s is a whole unit or more", key, stream.State.Remainder)
		streams[key] = stream.State
	}
	return streams
}

// requireSameStreamState requires two readings of a stream to be identical.
func requireSameStreamState(t *testing.T, key streamKey, want, got helpers.StreamStateJSON) {
	t.Helper()
	require.Equal(t, want.Rate.String(), got.Rate.String(), "stream %+v rate", key)
	require.Equal(t, want.Remainder.String(), got.Remainder.String(), "stream %+v remainder", key)
	require.Equal(t, want.LeaseCount, got.LeaseCount, "stream %+v lease count", key)
	require.True(t, want.LastSettledAt.Equal(got.LastSettledAt),
		"stream %+v last_settled_at %s, expected %s", key, got.LastSettledAt, want.LastSettledAt)
	require.Equal(t, want.IsExhausted(), got.IsExhausted(), "stream %+v exhausted", key)
	if want.IsExhausted() {
		require.True(t, want.ExhaustedAt.Equal(*got.ExhaustedAt), "stream %+v exhausted_at", key)
	}
}

// requireSameStreams requires two snapshots to list the same streams in the
// same state: nothing was settled, created or deleted in between.
func requireSameStreams(t *testing.T, want, got map[streamKey]helpers.StreamStateJSON) {
	t.Helper()
	require.Len(t, got, len(want), "number of streams")
	for key, state := range want {
		other, ok := got[key]
		require.True(t, ok, "stream %+v is missing", key)
		requireSameStreamState(t, key, state, other)
	}
}

// streamSettlement is what one message settling some of a provider's streams
// did, derived from the streams before and after it.
type streamSettlement struct {
	// At is the block time the streams were settled at; zero if none was.
	At time.Time
	// Settled lists the streams the message settled: their last_settled_at
	// moved to At.
	Settled []streamKey
	// Amounts is what each settled stream paid. It can be zero: a stream that
	// accrued less than one unit only grows its remainder.
	Amounts map[streamKey]sdkmath.Int
	// Paid is the sum of Amounts, per denom.
	Paid sdk.Coins
}

// paying counts the settled streams that paid something.
func (s streamSettlement) paying() int {
	count := 0
	for _, amount := range s.Amounts {
		if amount.IsPositive() {
			count++
		}
	}
	return count
}

// settledBetween derives the settlement a single message performed from two
// snapshots of a provider's streams taken around it, and verifies the exact
// accrual of every stream it settled.
//
// A settlement at block time T computes
//
//	total = remainder + rate × (T − last_settled_at, in nanoseconds)
//
// pays total / 1e9, keeps total % 1e9 as the new remainder and moves
// last_settled_at to T. Nothing is rounded to whole seconds. So each stream
// must either be untouched or sit at the one block time T with exactly that
// remainder, and its payment follows from its state before.
//
// That only holds while the streams are otherwise unchanged and the tenants'
// credit covers the accrual, so the snapshots must list the same streams with
// the same rate and lease count, none of them frozen.
func settledBetween(t *testing.T, before, after map[streamKey]helpers.StreamStateJSON) streamSettlement {
	t.Helper()
	require.Len(t, after, len(before), "a stream was created or deleted between the snapshots")

	settlement := streamSettlement{Amounts: make(map[streamKey]sdkmath.Int), Paid: sdk.NewCoins()}
	for key, was := range before {
		is, ok := after[key]
		require.True(t, ok, "stream %+v was deleted between the snapshots", key)
		require.Equal(t, was.Rate.String(), is.Rate.String(), "stream %+v changed rate between the snapshots", key)
		require.Equal(t, was.LeaseCount, is.LeaseCount, "stream %+v changed lease count between the snapshots", key)
		require.False(t, was.IsExhausted(), "stream %+v was already frozen", key)
		require.False(t, is.IsExhausted(), "stream %+v froze: its payment was capped by the tenant's credit", key)

		if is.LastSettledAt.Equal(was.LastSettledAt) {
			require.Equal(t, was.Remainder.String(), is.Remainder.String(),
				"stream %+v changed remainder without being settled", key)
			continue
		}
		require.True(t, is.LastSettledAt.After(was.LastSettledAt), "stream %+v last_settled_at moved backwards", key)
		if settlement.At.IsZero() {
			settlement.At = is.LastSettledAt
		}
		require.True(t, settlement.At.Equal(is.LastSettledAt),
			"stream %+v settled at %s, another stream of the same message at %s", key, is.LastSettledAt, settlement.At)

		amount, remainder := was.AccrualAt(is.LastSettledAt)
		require.Equal(t, remainder.String(), is.Remainder.String(),
			"stream %+v remainder after accruing rate %s from %s (remainder %s) to %s",
			key, was.Rate, was.LastSettledAt, was.Remainder, is.LastSettledAt)

		settlement.Settled = append(settlement.Settled, key)
		settlement.Amounts[key] = amount
		if amount.IsPositive() {
			settlement.Paid = settlement.Paid.Add(sdk.NewCoin(key.denom, amount))
		}
	}
	return settlement
}

// providerWithdrawal is the verified outcome of a provider withdrawal.
type providerWithdrawal struct {
	// Tx is the committed withdraw transaction.
	Tx *sdk.TxResponse
	// Response is the transaction's MsgWithdrawResponse.
	Response *billingtypes.MsgWithdrawResponse
	// Paid is what the provider's payout address received, per denom.
	Paid sdk.Coins
	// Settled lists the streams the withdrawal settled.
	Settled []streamKey
	// Amounts is what each settled stream paid.
	Amounts map[streamKey]sdkmath.Int
	// Before and After are the provider's streams around the withdrawal.
	Before, After map[streamKey]helpers.StreamStateJSON
}

// requireProviderWithdraw runs `tx billing withdraw --provider` signed by
// signer and verifies it end to end: the transaction succeeds, every stream
// either stays untouched or is settled at the time of the transaction's block
// with the exact remainder, and the payout address, the message response and
// the events all report exactly what those streams accrued (see
// settledBetween).
//
// limit counts streams (0 = the default); key resumes after a previous page.
// It must not be used when a stream is expected to freeze: a frozen stream
// pays the tenant's remaining credit, not its accrual.
func requireProviderWithdraw(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, signer ibc.Wallet, providerUUID string, limit uint64, key string, flags ...string) providerWithdrawal {
	t.Helper()

	provider, err := helpers.SKUQueryProvider(ctx, chain, providerUUID)
	require.NoError(t, err)
	payoutAddr := provider.Provider.PayoutAddress
	require.NotEmpty(t, payoutAddr)

	before := snapshotProviderStreams(t, ctx, chain, providerUUID)
	balancesBefore := make(map[string]sdkmath.Int)
	for stream := range before {
		if _, ok := balancesBefore[stream.denom]; ok {
			continue
		}
		balance, err := chain.GetBalance(ctx, payoutAddr, stream.denom)
		require.NoError(t, err)
		balancesBefore[stream.denom] = balance
	}

	res, err := helpers.BillingWithdrawByProviderPaginated(ctx, chain, signer, providerUUID, limit, key, flags...)
	require.NoError(t, err)
	txRes, err := chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "provider withdraw should succeed: %s", txRes.RawLog)

	after := snapshotProviderStreams(t, ctx, chain, providerUUID)
	settlement := settledBetween(t, before, after)
	paid, settled, paying := settlement.Paid, settlement.Settled, settlement.paying()

	// A settlement moves the stream to the exact time of its block.
	if len(settled) > 0 {
		block, err := helpers.GetBlockInfo(ctx, chain, txRes.Height)
		require.NoError(t, err)
		require.True(t, block.Time.Equal(settlement.At),
			"streams settled at %s, the withdrawal's block %d has time %s", settlement.At, txRes.Height, block.Time)
	}

	// The payout address received exactly what the streams accrued.
	for denom, balanceBefore := range balancesBefore {
		balanceAfter, err := chain.GetBalance(ctx, payoutAddr, denom)
		require.NoError(t, err)
		require.Equal(t, paid.AmountOf(denom).String(), balanceAfter.Sub(balanceBefore).String(),
			"payout address %s should receive the exact accrual of the settled streams in %s", payoutAddr, denom)
	}

	response, err := helpers.GetWithdrawResponse(txRes)
	require.NoError(t, err)
	require.True(t, paid.Equal(response.TotalAmounts), "response total_amounts %s, streams paid %s", response.TotalAmounts, paid)
	require.Equal(t, payoutAddr, response.PayoutAddress)
	require.Equal(t, uint64(paying), response.WithdrawalCount, "withdrawal_count counts the streams that paid out")
	require.Equal(t, response.HasMore, len(response.NextKey) > 0, "next_key is set if and only if has_more")

	// One batch_withdraw event summarizes the message.
	batches := helpers.FilterEvents(txRes.Events, billingtypes.EventTypeBatchWithdraw)
	require.Len(t, batches, 1)
	batch := batches[0]
	require.Equal(t, providerUUID, batch[billingtypes.AttributeKeyProviderUUID])
	require.Equal(t, payoutAddr, batch[billingtypes.AttributeKeyPayoutAddress])
	require.Equal(t, paid.String(), batch[billingtypes.AttributeKeyAmount])
	require.Equal(t, fmt.Sprintf("%d", paying), batch[billingtypes.AttributeKeyStreamCount])
	require.Equal(t, "0", batch[billingtypes.AttributeKeyExhaustedCount])

	// One stream_settled event per stream that paid, for exactly its accrual.
	settledEvents := helpers.FilterEvents(txRes.Events, billingtypes.EventTypeStreamSettled)
	require.Len(t, settledEvents, paying)
	for _, stream := range settled {
		if !settlement.Amounts[stream].IsPositive() {
			continue
		}
		amount := sdk.NewCoin(stream.denom, settlement.Amounts[stream]).String()
		found := false
		for _, event := range settledEvents {
			if event[billingtypes.AttributeKeyTenant] == stream.tenant && event[billingtypes.AttributeKeyAmount] == amount {
				require.Equal(t, providerUUID, event[billingtypes.AttributeKeyProviderUUID])
				require.Equal(t, payoutAddr, event[billingtypes.AttributeKeyPayoutAddress])
				found = true
				break
			}
		}
		require.True(t, found, "no stream_settled event of %s for tenant %s in %v", amount, stream.tenant, settledEvents)
	}

	// Per-lease settlement is gone, and nothing is closed inside the transaction.
	require.Empty(t, helpers.FilterEvents(txRes.Events, billingtypes.EventTypeProviderWithdraw), "provider_withdraw is no longer emitted")
	require.Empty(t, helpers.FilterEvents(txRes.Events, billingtypes.EventTypeStreamExhausted))
	require.Empty(t, helpers.FilterEvents(txRes.Events, billingtypes.EventTypeLeaseAutoClose))

	return providerWithdrawal{
		Tx: txRes, Response: response, Paid: paid, Settled: settled, Amounts: settlement.Amounts, Before: before, After: after,
	}
}

// waitForBlockTime waits until a block whose header time is at or after target
// has been committed. A transaction sent afterwards runs at a later time still.
// Accrual is a function of block time, so waiting on it (not on a number of
// blocks) is what makes an exhaustion test independent of the block interval.
func waitForBlockTime(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, target time.Time) {
	t.Helper()

	const maxBlocks = 120
	for range maxBlocks {
		now, err := helpers.GetLatestBlockTime(ctx, chain)
		require.NoError(t, err)
		if !now.Before(target) {
			return
		}
		require.NoError(t, testutil.WaitForBlocks(ctx, 1, chain))
	}
	require.FailNow(t, "block time did not reach target", "target %s after %d blocks", target, maxBlocks)
}

// waitForLeaseState polls a lease, one block at a time, until it is in the
// wanted state. Leases of a stream frozen for exhausted credit are closed by
// the EndBlocker (at most MaxAutoClosesPerBlock per block), not by the
// transaction that froze the stream, so their closure has to be awaited.
func waitForLeaseState(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, leaseUUID string, want billingtypes.LeaseState, maxBlocks int) *helpers.LeaseResponseJSON {
	t.Helper()

	for waited := 0; ; waited++ {
		lease, err := helpers.BillingQueryLease(ctx, chain, leaseUUID)
		require.NoError(t, err)
		if lease.Lease.GetState() == want {
			return lease
		}
		require.Less(t, waited, maxBlocks, "lease %s is %s after %d blocks, expected %s", leaseUUID, lease.Lease.State, waited, want)
		require.NoError(t, testutil.WaitForBlocks(ctx, 1, chain))
	}
}

// fundTenantCredit is a helper to fund a tenant's credit account.
func fundTenantCredit(t *testing.T, ctx context.Context, tc *billingTestContext, tenant ibc.Wallet, amount int64) {
	t.Helper()

	// First send PWR to tenant
	err := tc.chain.GetNode().SendFunds(ctx, tc.authority.KeyName(), ibc.WalletAmount{
		Address: tenant.FormattedAddress(),
		Denom:   tc.pwrDenom,
		Amount:  sdkmath.NewInt(amount),
	})
	require.NoError(t, err)
	require.NoError(t, testutil.WaitForBlocks(ctx, 2, tc.chain))

	// Fund credit account
	fundAmount := fmt.Sprintf("%d%s", amount, tc.pwrDenom)
	res, err := helpers.BillingFundCredit(ctx, tc.chain, tenant, tenant.FormattedAddress(), fundAmount)
	require.NoError(t, err)

	txRes, err := tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "fund credit should succeed: %s", txRes.RawLog)
}
