/*
Package keeper_test contains tests that verify billing module state consistency
with the invariant checkers of helpers_test.go. These invariants can be run
against any state to detect corruption.
*/
package keeper_test

import (
	"errors"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// =============================================================================
// Tests Using Invariant Checkers
// =============================================================================

// TestInvariants_AfterFullLifecycle runs all invariants after each step of a
// complete lease lifecycle.
func TestInvariants_AfterFullLifecycle(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100_000_000))))
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create -> Acknowledge -> Withdraw -> Close
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 2},
	})
	requireInvariants(t, f)

	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(500 * time.Second))

	// Quantity 2 at 1/s for 500 seconds.
	withdrawResp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:       providerAddr.String(),
		ProviderUuid: provider.Uuid,
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1000))), withdrawResp.TotalAmounts)
	requireInvariants(t, f)

	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(500 * time.Second))

	closeResp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1000))), closeResp.TotalSettledAmounts)

	// The last lease took its stream with it.
	streams, err := f.App.BillingKeeper.GetAllStreams(f.Ctx)
	require.NoError(t, err)
	require.Empty(t, streams)

	// Run all invariants
	requireInvariants(t, f)
}

// TestInvariants_AfterMixedOperations runs all invariants after a complex set of
// mixed operations: creates, acknowledges, rejects, cancels, closes, and withdrawals.
func TestInvariants_AfterMixedOperations(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant1 := f.TestAccs[0]
	tenant2 := f.TestAccs[1]
	providerAddr := f.TestAccs[2]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Setup tenants
	for _, tenant := range []sdk.AccAddress{tenant1, tenant2} {
		creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
		require.NoError(t, err)
		f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100_000_000))))
		err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
			Tenant:        tenant.String(),
			CreditAddress: creditAddr.String(),
		})
		require.NoError(t, err)
	}

	// Tenant1: create and acknowledge lease
	f.createAndAcknowledgeLease(t, msgServer, tenant1, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	// Tenant1: create and reject lease
	rejectedResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant1.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	_, err = msgServer.RejectLease(f.Ctx, &types.MsgRejectLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{rejectedResp.LeaseUuid},
	})
	require.NoError(t, err)

	// Tenant2: create and cancel lease
	cancelResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant2.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	_, err = msgServer.CancelLease(f.Ctx, &types.MsgCancelLease{
		Tenant:     tenant2.String(),
		LeaseUuids: []string{cancelResp.LeaseUuid},
	})
	require.NoError(t, err)
	requireInvariants(t, f)

	// Advance time
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(300 * time.Second))

	// Withdraw tenant1's stream: only the acknowledged lease ever accrued.
	withdrawResp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:       providerAddr.String(),
		ProviderUuid: provider.Uuid,
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(300))), withdrawResp.TotalAmounts)
	require.Equal(t, uint64(1), withdrawResp.WithdrawalCount)

	// Tenant2: create pending lease (leave it pending for EndBlocker to potentially expire)
	_, err = msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant2.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)

	// Run EndBlocker (should not expire anything yet — not past timeout)
	err = f.App.BillingKeeper.EndBlocker(f.Ctx)
	require.NoError(t, err)

	// Run all invariants
	requireInvariants(t, f)
}

// TestInvariants_AfterEndBlockerExpiration runs invariants after EndBlocker expires
// pending leases.
func TestInvariants_AfterEndBlockerExpiration(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100_000_000))))
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create several pending leases
	for i := 0; i < 5; i++ {
		_, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)
	}

	// Advance past pending timeout
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(1801 * time.Second))

	// Run EndBlocker
	err = f.App.BillingKeeper.EndBlocker(f.Ctx)
	require.NoError(t, err)

	// A lease that was never acknowledged never had a stream.
	streams, err := f.App.BillingKeeper.GetAllStreams(f.Ctx)
	require.NoError(t, err)
	require.Empty(t, streams)

	// Run all invariants
	requireInvariants(t, f)
}

// TestInvariants_AfterAutoClose runs invariants at each stage of a credit
// exhaustion: after the settlement that freezes the stream, and after the
// EndBlocker closes its lease.
func TestInvariants_AfterAutoClose(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	// Fund just enough for reservation + a tiny bit more
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(3700))))
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	// Advance time far past credit exhaustion
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(10000 * time.Second))

	// The withdrawal pays what is left and freezes the stream; the lease stays
	// ACTIVE until the EndBlocker.
	resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:       providerAddr.String(),
		ProviderUuid: provider.Uuid,
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(3700))), resp.TotalAmounts)

	state, err := f.App.BillingKeeper.Streams.Get(f.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.True(t, state.IsExhausted())
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)
	requireInvariants(t, f)

	// The EndBlocker closes the lease.
	require.NoError(t, f.App.BillingKeeper.EndBlocker(f.Ctx))
	lease, err = f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
	require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason)

	// Run all invariants
	requireInvariants(t, f)
}

// =============================================================================
// Randomized Operation Sequence
// =============================================================================

// TestInvariants_RandomizedOperationSequence drives a fixed-seed random
// sequence of create / acknowledge / reject / cancel / close / withdraw /
// top-up / time advance / EndBlocker operations across several tenants, two
// providers and two denoms, and checks after every single step that:
//
//   - every billing invariant holds (RunAllInvariants);
//   - funds are conserved per denom: credit balances + payouts == funded;
//   - no payout balance ever decreases.
//
// Block time advances by amounts that are never a whole number of seconds, so
// every settlement goes through the stream's carried remainder.
//
// At regular checkpoints every provider withdraws all its streams, and what
// each stream was paid over its lifetime (from its first lease to the close of
// its last one) is compared with the exact accrual recomputed from the leases
// alone (acknowledged_at, closed_at and locked prices), which does not depend
// on the stream bookkeeping:
//
//		exact = SUM over the lifetime's leases of rate × active nanoseconds
//
//	  - a lifetime that never ran out of credit was paid exactly floor(exact / 1e9),
//	    and while it is alive its remainder is exactly exact mod 1e9: nothing is
//	    lost or double-counted when leases join and leave a stream;
//	  - a lifetime that froze was paid at most that;
//	  - nothing was ever paid outside a lifetime, and the payments add up to the
//	    providers' payout balances.
func TestInvariants_RandomizedOperationSequence(t *testing.T) {
	const (
		seed            = 20261120
		steps           = 1500
		checkpointEvery = 50
	)
	rng := rand.New(rand.NewPCG(seed, seed))

	f := initFixture(t)
	k := f.App.BillingKeeper
	msgServer := keeper.NewMsgServerImpl(k)
	denoms := []string{testDenom, testDenom2}

	// Fixed addresses, so the order of streams in the store (and with it the
	// order of pagination and of the drain) is the same on every run.
	fixedAddr := func(name string) sdk.AccAddress {
		bz := make([]byte, 20)
		copy(bz, name)
		return sdk.AccAddress(bz)
	}

	type simProvider struct {
		uuid   string
		addr   sdk.AccAddress
		payout sdk.AccAddress
		skus   []string
	}
	providers := make([]simProvider, 0, 2)
	providerByUUID := make(map[string]simProvider)
	for _, name := range []string{"a", "b"} {
		p := simProvider{addr: fixedAddr("sim-provider-" + name), payout: fixedAddr("sim-payout-" + name)}
		p.uuid = f.createTestProvider(t, p.addr.String(), p.payout.String()).Uuid
		// Per denom, one SKU at 1/s and one at 2/s.
		for _, denom := range denoms {
			for _, price := range []int64{3600, 7200} {
				p.skus = append(p.skus, f.createTestSKUWithDenom(t, p.uuid, price, denom).Uuid)
			}
		}
		providers = append(providers, p)
		providerByUUID[p.uuid] = p
	}

	funded := map[string]sdkmath.Int{testDenom: sdkmath.ZeroInt(), testDenom2: sdkmath.ZeroInt()}
	fund := func(tenant sdk.AccAddress, denom string, amount int64) {
		f.fundAccount(t, types.DeriveCreditAddress(tenant), sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(amount))))
		funded[denom] = funded[denom].AddRaw(amount)
	}

	// Two tenants that never run out, two that run out quickly.
	tenants := []sdk.AccAddress{
		fixedAddr("sim-tenant-rich-1"), fixedAddr("sim-tenant-rich-2"),
		fixedAddr("sim-tenant-poor-1"), fixedAddr("sim-tenant-poor-2"),
	}
	for i, tenant := range tenants {
		require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
			Tenant:        tenant.String(),
			CreditAddress: types.DeriveCreditAddress(tenant).String(),
		}))
		for _, denom := range denoms {
			if i < 2 {
				fund(tenant, denom, 1_000_000_000_000)
			} else {
				fund(tenant, denom, 25_000)
			}
		}
	}

	var created []string // lease UUIDs in creation order
	leasesIn := func(state types.LeaseState) []types.Lease {
		out := make([]types.Lease, 0, len(created))
		for _, uuid := range created {
			lease, err := k.GetLease(f.Ctx, uuid)
			require.NoError(t, err)
			if lease.State == state {
				out = append(out, lease)
			}
		}
		return out
	}
	// batchOf returns first plus up to two more leases of candidates accepted by same.
	batchOf := func(first types.Lease, candidates []types.Lease, same func(types.Lease) bool) []string {
		batch := []string{first.Uuid}
		extra := rng.IntN(3)
		for _, lease := range candidates {
			if len(batch) > extra {
				break
			}
			if lease.Uuid != first.Uuid && same(lease) {
				batch = append(batch, lease.Uuid)
			}
		}
		return batch
	}
	isOneOf := func(err error, targets ...error) bool {
		for _, target := range targets {
			if errors.Is(err, target) {
				return true
			}
		}
		return false
	}
	payoutOf := func(p simProvider, denom string) sdkmath.Int {
		return f.App.BankKeeper.GetBalance(f.Ctx, p.payout, denom).Amount
	}
	withdrawAll := func(p simProvider) {
		var key []byte
		for {
			resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
				Sender:       p.addr.String(),
				ProviderUuid: p.uuid,
				Limit:        3,
				Key:          key,
			})
			require.NoError(t, err)
			if !resp.HasMore {
				return
			}
			key = resp.NextKey
		}
	}

	// A stream key (provider, tenant, denom) can have several lifetimes: a
	// stream is deleted with its last lease and a later lease starts a new one,
	// with a fresh clock and no remainder. streamLife names one of them.
	type streamLife struct {
		id  types.StreamID
		gen int
	}
	var allStreamIDs []types.StreamID
	for _, p := range providers {
		for _, tenant := range tenants {
			for _, denom := range denoms {
				allStreamIDs = append(allStreamIDs, types.StreamID{ProviderUUID: p.uuid, Tenant: tenant.String(), Denom: denom})
			}
		}
	}
	streamState := func(id types.StreamID) (types.StreamState, bool) {
		state, err := k.Streams.Get(f.Ctx, keeper.NewStreamKey(id.ProviderUUID, sdk.MustAccAddressFromBech32(id.Tenant), id.Denom))
		if err != nil {
			require.ErrorIs(t, err, collections.ErrNotFound)
			return types.StreamState{}, false
		}
		return state, true
	}

	alive := make(map[types.StreamID]bool)
	ended := make(map[types.StreamID]int)    // lifetimes of a key that are over
	paid := make(map[streamLife]sdkmath.Int) // from stream_settled events
	frozen := make(map[streamLife]bool)      // from stream_exhausted events
	leaseLife := make(map[string]streamLife) // lease uuid + "/" + denom -> the lifetime it joined
	lastPayout := make(map[string]sdkmath.Int)
	stats := make(map[string]int)

	// recordJoin notes which lifetime of each of its streams a lease joins. It
	// runs right after the acknowledgement, before checkStep notices any stream
	// that ended in this step.
	recordJoin := func(uuid string) {
		lease, err := k.GetLease(f.Ctx, uuid)
		require.NoError(t, err)
		rates, err := types.LeaseRatesPerSecond(lease.Items)
		require.NoError(t, err)
		for _, rate := range rates {
			id := types.StreamID{ProviderUUID: lease.ProviderUuid, Tenant: lease.Tenant, Denom: rate.Denom}
			leaseLife[uuid+"/"+rate.Denom] = streamLife{id: id, gen: ended[id]}
		}
	}

	checkStep := func(step int, op string) {
		// Events first: a settlement in the step that deletes a stream still
		// belongs to the lifetime that is ending.
		for _, ev := range f.Ctx.EventManager().Events() {
			switch ev.Type {
			case types.EventTypeStreamExhausted:
				id := types.StreamID{
					ProviderUUID: attrValue(t, ev, types.AttributeKeyProviderUUID),
					Tenant:       attrValue(t, ev, types.AttributeKeyTenant),
					Denom:        attrValue(t, ev, types.AttributeKeyDenom),
				}
				frozen[streamLife{id: id, gen: ended[id]}] = true
				stats["stream_exhausted"]++
			case types.EventTypeLeaseAutoClose:
				stats["lease_auto_closed"]++
			case types.EventTypeStreamSettled:
				amount, err := sdk.ParseCoinNormalized(attrValue(t, ev, types.AttributeKeyAmount))
				require.NoError(t, err)
				id := types.StreamID{
					ProviderUUID: attrValue(t, ev, types.AttributeKeyProviderUUID),
					Tenant:       attrValue(t, ev, types.AttributeKeyTenant),
					Denom:        amount.Denom,
				}
				life := streamLife{id: id, gen: ended[id]}
				if _, ok := paid[life]; !ok {
					paid[life] = sdkmath.ZeroInt()
				}
				paid[life] = paid[life].Add(amount.Amount)
				stats["stream_settled"]++
			}
		}
		for _, id := range allStreamIDs {
			_, exists := streamState(id)
			if alive[id] && !exists {
				ended[id]++
				stats["stream_lifetimes_ended"]++
			}
			alive[id] = exists
		}

		for _, r := range RunAllInvariants(f.Ctx, k, f.App.BankKeeper, f.Ctx.BlockTime()) {
			require.True(t, r.Passed, "step %d (%s): invariant %s failed: %s", step, op, r.Name, r.Message)
		}

		for _, denom := range denoms {
			total := sdkmath.ZeroInt()
			for _, tenant := range tenants {
				total = total.Add(f.App.BankKeeper.GetBalance(f.Ctx, types.DeriveCreditAddress(tenant), denom).Amount)
			}
			for _, p := range providers {
				paid := payoutOf(p, denom)
				total = total.Add(paid)

				id := p.uuid + "/" + denom
				if prev, ok := lastPayout[id]; ok {
					require.True(t, paid.GTE(prev), "step %d (%s): payout of %s went from %s down to %s", step, op, id, prev, paid)
				}
				lastPayout[id] = paid
			}
			require.Equal(t, funded[denom], total, "step %d (%s): %s is not conserved", step, op, denom)
		}
	}

	// checkpoint settles everything and compares what every stream lifetime was
	// paid with the exact accrual recomputed from the leases.
	checkpoint := func(step int) {
		f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())
		for _, p := range providers {
			withdrawAll(p)
		}
		checkStep(step, "checkpoint")

		// exact[life] = SUM(rate × active nanoseconds) over the lifetime's leases,
		// in billionths of a unit.
		now := f.Ctx.BlockTime()
		exact := make(map[streamLife]sdkmath.Int)
		leases, err := k.GetAllLeases(f.Ctx)
		require.NoError(t, err)
		for i := range leases {
			lease := &leases[i]
			if lease.AcknowledgedAt == nil {
				continue // never active: never billed
			}
			end := now
			if lease.State != types.LEASE_STATE_ACTIVE {
				require.NotNil(t, lease.ClosedAt, "lease %s", lease.Uuid)
				end = *lease.ClosedAt
			}
			nanos := int64(end.Sub(*lease.AcknowledgedAt))
			require.GreaterOrEqual(t, nanos, int64(0))

			rates, err := types.LeaseRatesPerSecond(lease.Items)
			require.NoError(t, err)
			for _, rate := range rates {
				life, ok := leaseLife[lease.Uuid+"/"+rate.Denom]
				require.True(t, ok, "lease %s was acknowledged without the test recording its stream", lease.Uuid)
				if _, ok := exact[life]; !ok {
					exact[life] = sdkmath.ZeroInt()
				}
				exact[life] = exact[life].Add(rate.Amount.MulRaw(nanos))
			}
		}

		for life, accrual := range exact {
			owed := accrual.QuoRaw(int64(time.Second))
			got, ok := paid[life]
			if !ok {
				got = sdkmath.ZeroInt()
			}

			if frozen[life] {
				// Credit ran out: the provider got less than (or exactly) what accrued.
				require.True(t, got.LTE(owed), "step %d: frozen stream %v (lifetime %d) was paid %s, more than the %s it accrued",
					step, life.id, life.gen, got, owed)
				stats["frozen_lifetimes_checked"]++
				continue
			}
			require.Equal(t, owed.String(), got.String(), "step %d: stream %v (lifetime %d) accrued %s billionths", step, life.id, life.gen, accrual)
			stats["exact_lifetimes_checked"]++

			// A lifetime still running was settled just now: its clock is at the
			// block time and its remainder is the fraction not yet payable.
			if life.gen == ended[life.id] && alive[life.id] {
				state, exists := streamState(life.id)
				require.True(t, exists)
				require.Equal(t, now, state.LastSettledAt, "step %d: stream %v", step, life.id)
				require.Equal(t, accrual.ModRaw(int64(time.Second)).String(), state.Remainder.String(), "step %d: stream %v", step, life.id)
				if !state.Remainder.IsZero() {
					stats["nonzero_remainders_checked"]++
				}
			}
		}

		// Nothing was paid outside a lifetime with leases, and the payments
		// recorded from the events are the providers' payout balances.
		paidTo := make(map[string]sdkmath.Int)
		for life, amount := range paid {
			_, ok := exact[life]
			require.True(t, ok, "step %d: stream %v (lifetime %d) was paid %s without any lease", step, life.id, life.gen, amount)
			id := life.id.ProviderUUID + "/" + life.id.Denom
			if _, ok := paidTo[id]; !ok {
				paidTo[id] = sdkmath.ZeroInt()
			}
			paidTo[id] = paidTo[id].Add(amount)
		}
		for _, p := range providers {
			for _, denom := range denoms {
				want, ok := paidTo[p.uuid+"/"+denom]
				if !ok {
					want = sdkmath.ZeroInt()
				}
				require.Equal(t, want.String(), payoutOf(p, denom).String(), "step %d: payout of provider %s in %s", step, p.uuid, denom)
			}
		}
	}

	for step := 1; step <= steps; step++ {
		f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())
		var op string

		switch roll := rng.IntN(100); {
		case roll < 25:
			op = "create"
			tenant := tenants[rng.IntN(len(tenants))]
			p := providers[rng.IntN(len(providers))]
			order := rng.Perm(len(p.skus))
			items := make([]types.LeaseItemInput, 0, 2)
			for _, idx := range order[:1+rng.IntN(2)] {
				items = append(items, types.LeaseItemInput{SkuUuid: p.skus[idx], Quantity: uint64(1 + rng.IntN(3))}) //nolint:gosec // test code, value is in [1,3]
			}
			resp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{Tenant: tenant.String(), Items: items})
			if err != nil {
				require.True(t, isOneOf(err, types.ErrInsufficientCredit, types.ErrMaxLeasesReached, types.ErrMaxPendingLeasesReached),
					"step %d: unexpected create error: %s", step, err)
				stats["create_rejected"]++
				break
			}
			created = append(created, resp.LeaseUuid)

		case roll < 45:
			op = "acknowledge"
			pending := leasesIn(types.LEASE_STATE_PENDING)
			if len(pending) == 0 {
				break
			}
			first := pending[rng.IntN(len(pending))]
			sender := providerByUUID[first.ProviderUuid].addr
			if rng.IntN(4) == 0 {
				sender = f.Authority
			}
			batch := batchOf(first, pending, func(l types.Lease) bool {
				return l.ProviderUuid == first.ProviderUuid
			})
			_, err := msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
				Sender:     sender.String(),
				LeaseUuids: batch,
			})
			require.NoError(t, err, "step %d", step)
			for _, uuid := range batch {
				recordJoin(uuid)
			}
			stats["acknowledge"]++

		case roll < 53:
			op = "close"
			active := leasesIn(types.LEASE_STATE_ACTIVE)
			if len(active) == 0 {
				break
			}
			first := active[rng.IntN(len(active))]
			msg := &types.MsgCloseLease{Reason: "done"}
			switch rng.IntN(3) {
			case 0:
				msg.Sender = first.Tenant
				msg.LeaseUuids = batchOf(first, active, func(l types.Lease) bool { return l.Tenant == first.Tenant })
			case 1:
				msg.Sender = providerByUUID[first.ProviderUuid].addr.String()
				msg.LeaseUuids = batchOf(first, active, func(l types.Lease) bool { return l.ProviderUuid == first.ProviderUuid })
			default:
				msg.Sender = f.Authority.String()
				msg.LeaseUuids = batchOf(first, active, func(types.Lease) bool { return true })
			}
			_, err := msgServer.CloseLease(f.Ctx, msg)
			require.NoError(t, err, "step %d", step)
			stats["close"]++

		case roll < 68:
			op = "withdraw"
			p := providers[rng.IntN(len(providers))]
			sender := p.addr
			if rng.IntN(4) == 0 {
				sender = f.Authority
			}
			msg := &types.MsgWithdraw{
				Sender:       sender.String(),
				ProviderUuid: p.uuid,
				Limit:        []uint64{0, 1, 2, 5}[rng.IntN(4)],
			}
			resp, err := msgServer.Withdraw(f.Ctx, msg)
			require.NoError(t, err, "step %d", step)
			if resp.HasMore && rng.IntN(2) == 0 {
				msg.Key = resp.NextKey
				_, err = msgServer.Withdraw(f.Ctx, msg)
				require.NoError(t, err, "step %d", step)
			}
			stats["withdraw"]++

		case roll < 82:
			op = "advance"
			// Never a whole number of seconds: a fraction with nanosecond
			// precision, alone one time in four, otherwise on top of up to
			// half an hour.
			d := time.Duration(1 + rng.Int64N(int64(time.Second)-1))
			if rng.IntN(4) != 0 {
				d += time.Duration(1+rng.IntN(1800)) * time.Second
			}
			f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(d))

		case roll < 90:
			op = "end_blocker"
			require.NoError(t, k.EndBlocker(f.Ctx), "step %d", step)

		case roll < 96:
			op = "fund"
			fund(tenants[rng.IntN(len(tenants))], denoms[rng.IntN(len(denoms))], int64(2_000+rng.IntN(20_000)))

		case roll < 98:
			op = "cancel_or_reject"
			pending := leasesIn(types.LEASE_STATE_PENDING)
			if len(pending) == 0 {
				break
			}
			lease := pending[rng.IntN(len(pending))]
			var err error
			if rng.IntN(2) == 0 {
				_, err = msgServer.CancelLease(f.Ctx, &types.MsgCancelLease{Tenant: lease.Tenant, LeaseUuids: []string{lease.Uuid}})
			} else {
				_, err = msgServer.RejectLease(f.Ctx, &types.MsgRejectLease{
					Sender:     providerByUUID[lease.ProviderUuid].addr.String(),
					LeaseUuids: []string{lease.Uuid},
				})
			}
			require.NoError(t, err, "step %d", step)

		default:
			op = "unauthorized_withdraw"
			// A tenant, or the other provider, tries to withdraw a provider's streams.
			p := providers[rng.IntN(len(providers))]
			intruders := []sdk.AccAddress{tenants[rng.IntN(len(tenants))], providers[0].addr, providers[1].addr}
			intruder := intruders[rng.IntN(len(intruders))]
			if intruder.Equals(p.addr) {
				intruder = tenants[0]
			}
			_, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{Sender: intruder.String(), ProviderUuid: p.uuid})
			require.ErrorIs(t, err, types.ErrUnauthorized, "step %d", step)
		}

		checkStep(step, op)
		if step%checkpointEvery == 0 {
			checkpoint(step)
		}
	}

	// Drain whatever is still frozen, then take a final checkpoint.
	f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())
	require.NoError(t, k.EndBlocker(f.Ctx))
	checkStep(steps, "final_end_blocker")
	checkpoint(steps)

	// The sequence must actually have exercised the paths it is meant to cover.
	t.Logf("leases created: %d, stats: %v", len(created), stats)
	require.Greater(t, len(created), 50)
	require.Positive(t, stats["acknowledge"])
	require.Positive(t, stats["close"])
	require.Positive(t, stats["withdraw"])
	require.Positive(t, stats["stream_settled"])
	require.Positive(t, stats["stream_exhausted"], "no stream ever ran out of credit")
	require.Positive(t, stats["lease_auto_closed"], "the EndBlocker never closed a lease")
	require.Positive(t, stats["create_rejected"], "no tenant ever hit a credit or lease limit")
	require.Positive(t, stats["stream_lifetimes_ended"], "no stream was ever deleted and started again")
	require.Positive(t, stats["exact_lifetimes_checked"])
	require.Positive(t, stats["frozen_lifetimes_checked"])
	require.Positive(t, stats["nonzero_remainders_checked"], "the carried remainder was never exercised")
}
