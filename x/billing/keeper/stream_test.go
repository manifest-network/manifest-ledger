/*
Package keeper_test contains unit tests for settlement streams.

Test Coverage:
- Stream lifecycle: acknowledge joins, close leaves, last lease deletes
- Settlement: one transfer per stream, exact accrual with a carried remainder
- Gas: withdraw and close cost do not grow with the leases in a stream
- Credit exhaustion: freeze in the settling tx, bounded EndBlocker drain
- Closure reason (ENG-577): paid in full keeps the caller's reason
- Multi-denom leases with streams in different states
- Withdraw pagination cursor
- Accrual overflow
- Deprecated per-lease API surface
*/
package keeper_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

// mainnetLikeDenom has the length of the tokenfactory denom leases are priced
// in on mainnet; key length drives store gas, so gas tests use it.
const mainnetLikeDenom = "factory/manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj/upwr"

// streamFixture is a provider with one SKU priced at 1 per second in denom,
// and helpers to fund tenants and move time.
type streamFixture struct {
	*testFixture
	msgServer    types.MsgServer
	provider     skutypes.Provider
	providerAddr sdk.AccAddress
	payoutAddr   sdk.AccAddress
	sku          skutypes.SKU
	denom        string
}

func newStreamFixture(t *testing.T, denom string) *streamFixture {
	t.Helper()

	f := initFixture(t)
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())

	params := types.DefaultParams()
	params.MaxLeasesPerTenant = 500
	params.MaxPendingLeasesPerTenant = 500
	require.NoError(t, f.App.BillingKeeper.SetParams(f.Ctx, params))

	return &streamFixture{
		testFixture:  f,
		msgServer:    keeper.NewMsgServerImpl(f.App.BillingKeeper),
		provider:     provider,
		providerAddr: providerAddr,
		payoutAddr:   payoutAddr,
		sku:          f.createTestSKUWithDenom(t, provider.Uuid, 3600, denom), // 1 per second
		denom:        denom,
	}
}

// fundTenant gives the tenant's credit account the given amount of denom.
func (s *streamFixture) fundTenant(t *testing.T, tenant sdk.AccAddress, denom string, amount int64) {
	t.Helper()
	creditAddr := types.DeriveCreditAddress(tenant)
	s.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(amount))))
	if _, err := s.App.BillingKeeper.GetCreditAccount(s.Ctx, tenant.String()); err != nil {
		require.NoError(t, s.App.BillingKeeper.SetCreditAccount(s.Ctx, types.CreditAccount{
			Tenant:        tenant.String(),
			CreditAddress: creditAddr.String(),
		}))
	}
}

// lease creates and acknowledges one lease of the fixture SKU for the tenant.
func (s *streamFixture) lease(t *testing.T, tenant sdk.AccAddress) string {
	t.Helper()
	return s.createAndAcknowledgeLease(t, s.msgServer, tenant, s.providerAddr, []types.LeaseItemInput{
		{SkuUuid: s.sku.Uuid, Quantity: 1},
	})
}

func (s *streamFixture) advance(d time.Duration) {
	s.Ctx = s.Ctx.WithBlockTime(s.Ctx.BlockTime().Add(d))
}

func (s *streamFixture) withdraw(t *testing.T, limit uint64, key []byte) *types.MsgWithdrawResponse {
	t.Helper()
	resp, err := s.msgServer.Withdraw(s.Ctx, &types.MsgWithdraw{
		Sender:       s.providerAddr.String(),
		ProviderUuid: s.provider.Uuid,
		Limit:        limit,
		Key:          key,
	})
	require.NoError(t, err)
	return resp
}

func (s *streamFixture) stream(t *testing.T, tenant sdk.AccAddress, denom string) (types.StreamState, bool) {
	t.Helper()
	state, err := s.App.BillingKeeper.Streams.Get(s.Ctx, keeper.NewStreamKey(s.provider.Uuid, tenant, denom))
	if err != nil {
		require.ErrorIs(t, err, collections.ErrNotFound)
		return types.StreamState{}, false
	}
	return state, true
}

func (s *streamFixture) payout(denom string) sdkmath.Int {
	return s.App.BankKeeper.GetBalance(s.Ctx, s.payoutAddr, denom).Amount
}

func (s *streamFixture) credit(tenant sdk.AccAddress, denom string) sdkmath.Int {
	return s.App.BankKeeper.GetBalance(s.Ctx, types.DeriveCreditAddress(tenant), denom).Amount
}

// requireInvariants runs every billing invariant, including the stream/lease
// consistency invariant.
func (s *streamFixture) requireInvariants(t *testing.T) {
	t.Helper()
	requireInvariants(t, s.testFixture)
}

func gasOf(ctx sdk.Context, fn func(ctx sdk.Context)) uint64 {
	meter := storetypes.NewInfiniteGasMeter()
	fn(ctx.WithGasMeter(meter))
	return meter.GasConsumed()
}

func TestStream_AcknowledgeJoinsAndCloseLeaves(t *testing.T) {
	s := newStreamFixture(t, testDenom)
	tenant := s.TestAccs[0]
	s.fundTenant(t, tenant, testDenom, 1_000_000)

	_, exists := s.stream(t, tenant, testDenom)
	require.False(t, exists, "no stream before the first lease is acknowledged")

	leases := []string{s.lease(t, tenant), s.lease(t, tenant), s.lease(t, tenant)}

	state, exists := s.stream(t, tenant, testDenom)
	require.True(t, exists)
	require.Equal(t, sdkmath.NewInt(3), state.Rate, "three leases at 1/s")
	require.Equal(t, uint64(3), state.LeaseCount)
	require.False(t, state.IsExhausted())
	require.Equal(t, s.Ctx.BlockTime(), state.LastSettledAt, "billing starts at acknowledgement")
	s.requireInvariants(t)

	// Closing a lease settles the stream for all three, then drops its rate.
	s.advance(100 * time.Second)
	resp, err := s.msgServer.CloseLease(s.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leases[0]},
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(300))), resp.TotalSettledAmounts)
	require.Equal(t, sdkmath.NewInt(300), s.payout(testDenom))

	state, exists = s.stream(t, tenant, testDenom)
	require.True(t, exists)
	require.Equal(t, sdkmath.NewInt(2), state.Rate)
	require.Equal(t, uint64(2), state.LeaseCount)
	s.requireInvariants(t)

	// Closing the rest in one message settles the stream once and deletes it.
	s.advance(50 * time.Second)
	resp, err = s.msgServer.CloseLease(s.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: leases[1:],
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100))), resp.TotalSettledAmounts)
	require.Equal(t, sdkmath.NewInt(400), s.payout(testDenom))

	_, exists = s.stream(t, tenant, testDenom)
	require.False(t, exists, "a stream without leases is deleted")
	s.requireInvariants(t)

	for _, uuid := range leases {
		lease, err := s.App.BillingKeeper.GetLease(s.Ctx, uuid)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
		require.Nil(t, lease.LastSettledAt, "the deprecated field is never set") //nolint:staticcheck // asserting the deprecated field stays unset
	}
}

func TestStream_WithdrawSettlesOncePerStream(t *testing.T) {
	s := newStreamFixture(t, testDenom)
	tenant := s.TestAccs[0]
	s.fundTenant(t, tenant, testDenom, 1_000_000)
	for range 5 {
		s.lease(t, tenant)
	}

	s.advance(100 * time.Second)
	s.Ctx = s.Ctx.WithEventManager(sdk.NewEventManager())
	resp := s.withdraw(t, 0, nil)

	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(500))), resp.TotalAmounts)
	require.Equal(t, uint64(1), resp.WithdrawalCount, "one stream paid out, whatever the lease count")
	require.False(t, resp.HasMore)
	require.Empty(t, resp.NextKey)
	require.Equal(t, s.payoutAddr.String(), resp.PayoutAddress)
	require.Equal(t, sdkmath.NewInt(500), s.payout(testDenom))

	// Five leases, one bank transfer.
	require.Equal(t, 1, countEvents(s.Ctx, "transfer"))
	require.Equal(t, 1, countEvents(s.Ctx, types.EventTypeStreamSettled))
	require.Equal(t, 1, countEvents(s.Ctx, types.EventTypeBatchWithdraw))
	require.Zero(t, countEvents(s.Ctx, types.EventTypeProviderWithdraw), "per-lease withdraw events are gone")

	state, _ := s.stream(t, tenant, testDenom)
	require.Equal(t, s.Ctx.BlockTime(), state.LastSettledAt)

	// Nothing accrues within the same block.
	resp = s.withdraw(t, 0, nil)
	require.True(t, resp.TotalAmounts.IsZero())
	require.Zero(t, resp.WithdrawalCount)
	require.Equal(t, sdkmath.NewInt(500), s.payout(testDenom))
	s.requireInvariants(t)
}

// TestStream_AccrualIsExactAcrossSettlements checks that accrual is computed to
// the nanosecond: what a settlement cannot pay as a whole unit is carried in
// the stream's remainder, so no fraction of a second is ever dropped, however
// the same span of time is cut into settlements.
func TestStream_AccrualIsExactAcrossSettlements(t *testing.T) {
	s := newStreamFixture(t, testDenom)
	tenant := s.TestAccs[0]
	s.fundTenant(t, tenant, testDenom, 1_000_000)
	s.lease(t, tenant)
	start := s.Ctx.BlockTime()

	requireStream := func(wantSettledAt time.Time, wantRemainder int64) {
		t.Helper()
		state, exists := s.stream(t, tenant, testDenom)
		require.True(t, exists)
		require.Equal(t, wantSettledAt, state.LastSettledAt, "the stream's clock always moves to the settlement time")
		require.Equal(t, sdkmath.NewInt(wantRemainder), state.Remainder)
	}

	// 1.5s at 1/s: one whole unit is paid and half a unit is carried.
	s.advance(1500 * time.Millisecond)
	s.withdraw(t, 0, nil)
	require.Equal(t, sdkmath.NewInt(1), s.payout(testDenom))
	requireStream(start.Add(1500*time.Millisecond), 500_000_000)

	// Another 0.5s completes the second unit.
	s.advance(500 * time.Millisecond)
	s.withdraw(t, 0, nil)
	require.Equal(t, sdkmath.NewInt(2), s.payout(testDenom))
	requireStream(start.Add(2*time.Second), 0)

	// Ten settlements 0.3s apart pay exactly 3 units over 3 seconds: settling
	// often loses nothing, unlike rounding each settlement down to whole seconds.
	for range 10 {
		s.advance(300 * time.Millisecond)
		s.withdraw(t, 0, nil)
	}
	require.Equal(t, sdkmath.NewInt(5), s.payout(testDenom))
	requireStream(start.Add(5*time.Second), 0)

	// A single nanosecond accrues one billionth of a unit.
	s.advance(time.Nanosecond)
	s.withdraw(t, 0, nil)
	require.Equal(t, sdkmath.NewInt(5), s.payout(testDenom))
	requireStream(start.Add(5*time.Second+time.Nanosecond), 1)
	s.requireInvariants(t)
}

// TestStream_JoiningLeaseIsNotBilledBeforeItJoined checks that a lease
// acknowledged between two whole seconds of an existing stream pays only for
// the time after its acknowledgement.
func TestStream_JoiningLeaseIsNotBilledBeforeItJoined(t *testing.T) {
	s := newStreamFixture(t, testDenom)
	tenant := s.TestAccs[0]
	s.fundTenant(t, tenant, testDenom, 1_000_000)

	// Lease A opens the stream at 0.0s.
	s.lease(t, tenant)

	// 1.5s: lease B joins. The stream settles A's 1.5s first: 1 unit paid,
	// half a unit carried.
	s.advance(1500 * time.Millisecond)
	b := s.lease(t, tenant)
	require.Equal(t, sdkmath.NewInt(1), s.payout(testDenom))
	state, _ := s.stream(t, tenant, testDenom)
	require.Equal(t, sdkmath.NewInt(2), state.Rate)
	require.Equal(t, sdkmath.NewInt(500_000_000), state.Remainder)

	// 2.0s: B leaves after exactly half a second. In that half second the
	// stream accrued 2 × 0.5 = 1 unit; with A's carried half that is 1.5: one
	// unit is paid and half is carried again. Over the whole 2s, A accrued 2
	// and B 0.5, of which 2 are paid and 0.5 is carried: B was charged for its
	// own half second only.
	s.advance(500 * time.Millisecond)
	resp, err := s.msgServer.CloseLease(s.Ctx, &types.MsgCloseLease{Sender: tenant.String(), LeaseUuids: []string{b}})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1))), resp.TotalSettledAmounts)
	require.Equal(t, sdkmath.NewInt(2), s.payout(testDenom))
	state, _ = s.stream(t, tenant, testDenom)
	require.Equal(t, sdkmath.NewInt(1), state.Rate)
	require.Equal(t, sdkmath.NewInt(500_000_000), state.Remainder)

	// 2.5s: A alone accrues another half, completing the carried unit.
	s.advance(500 * time.Millisecond)
	s.withdraw(t, 0, nil)
	require.Equal(t, sdkmath.NewInt(3), s.payout(testDenom), "A: 2.5 units, B: 0.5 units, 3 paid in total")
	state, _ = s.stream(t, tenant, testDenom)
	require.True(t, state.Remainder.IsZero())
	s.requireInvariants(t)
}

// TestStream_WithdrawGasIndependentOfLeaseCount is the regression test for
// ENG-473: withdrawing must cost the same whether a stream aggregates one
// lease or hundreds.
func TestStream_WithdrawGasIndependentOfLeaseCount(t *testing.T) {
	gasFor := func(t *testing.T, leases int) uint64 {
		t.Helper()
		s := newStreamFixture(t, mainnetLikeDenom)
		tenant := s.TestAccs[0]
		s.fundTenant(t, tenant, mainnetLikeDenom, 1_000_000_000_000)
		for range leases {
			s.lease(t, tenant)
		}
		s.advance(time.Hour)

		var resp *types.MsgWithdrawResponse
		gas := gasOf(s.Ctx, func(ctx sdk.Context) {
			var err error
			resp, err = s.msgServer.Withdraw(ctx, &types.MsgWithdraw{
				Sender:       s.providerAddr.String(),
				ProviderUuid: s.provider.Uuid,
			})
			require.NoError(t, err)
		})
		require.Equal(t,
			sdk.NewCoins(sdk.NewCoin(mainnetLikeDenom, sdkmath.NewInt(int64(leases)*3600))),
			resp.TotalAmounts,
			"every lease of the stream is paid for",
		)
		return gas
	}

	one := gasFor(t, 1)
	fifty := gasFor(t, 50)
	mainnet := gasFor(t, 382) // the largest mainnet provider at the time of ENG-473
	t.Logf("withdraw gas: 1 lease = %d, 50 leases = %d, 382 leases = %d", one, fifty, mainnet)

	// The only lease-count-dependent bytes are the varint lease count and the
	// decimal rate in the stream record: a few bytes, a few hundred gas.
	const tolerance = 1_000
	require.InDelta(t, one, fifty, tolerance)
	require.InDelta(t, one, mainnet, tolerance)

	// Per-lease settlement cost ~93k gas per lease (9.3M for 100 leases).
	require.Less(t, mainnet, uint64(200_000))
}

// TestStream_CloseGasIndependentOfStreamSize checks that closing one lease
// does not get dearer as the stream it belongs to grows.
func TestStream_CloseGasIndependentOfStreamSize(t *testing.T) {
	gasFor := func(t *testing.T, leases int) uint64 {
		t.Helper()
		s := newStreamFixture(t, mainnetLikeDenom)
		tenant := s.TestAccs[0]
		s.fundTenant(t, tenant, mainnetLikeDenom, 1_000_000_000_000)
		var last string
		for range leases {
			last = s.lease(t, tenant)
		}
		s.advance(time.Hour)

		return gasOf(s.Ctx, func(ctx sdk.Context) {
			_, err := s.msgServer.CloseLease(ctx, &types.MsgCloseLease{
				Sender:     tenant.String(),
				LeaseUuids: []string{last},
			})
			require.NoError(t, err)
		})
	}

	// Compare two streams that both survive the close (a stream emptied by the
	// close is deleted, which is a different, cheaper path).
	two := gasFor(t, 2)
	many := gasFor(t, 382)
	t.Logf("close-one-lease gas: stream of 2 = %d, stream of 382 = %d", two, many)
	require.InDelta(t, two, many, 2_000)
}

func TestStream_ExhaustionFreezesThenEndBlockerDrains(t *testing.T) {
	s := newStreamFixture(t, testDenom)
	tenant := s.TestAccs[0]

	// 150 leases need 150 × 3600 reserved credit at creation; fund exactly that.
	const leases = 150
	s.fundTenant(t, tenant, testDenom, leases*3600)
	uuids := make([]string, 0, leases)
	for range leases {
		uuids = append(uuids, s.lease(t, tenant))
	}

	// After 2h the stream has accrued twice the tenant's credit.
	s.advance(2 * time.Hour)
	s.Ctx = s.Ctx.WithEventManager(sdk.NewEventManager())
	resp := s.withdraw(t, 0, nil)

	// The settling tx pays what is left and only freezes the stream.
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(leases*3600))), resp.TotalAmounts)
	require.True(t, s.credit(tenant, testDenom).IsZero())
	require.Equal(t, 1, countEvents(s.Ctx, types.EventTypeStreamExhausted))
	require.Zero(t, countEvents(s.Ctx, types.EventTypeLeaseAutoClose), "no lease is closed inside the tx")

	state, exists := s.stream(t, tenant, testDenom)
	require.True(t, exists)
	require.True(t, state.IsExhausted())
	require.Equal(t, uint64(leases), state.LeaseCount, "a frozen stream keeps its leases until drained")
	s.requireInvariants(t)

	// A frozen stream accrues nothing more, however much time passes.
	s.advance(time.Hour)
	require.True(t, s.withdraw(t, 0, nil).TotalAmounts.IsZero())

	// First EndBlocker closes at most MaxAutoClosesPerBlock leases.
	s.Ctx = s.Ctx.WithEventManager(sdk.NewEventManager())
	require.NoError(t, s.App.BillingKeeper.EndBlocker(s.Ctx))
	require.Equal(t, types.MaxAutoClosesPerBlock, countEvents(s.Ctx, types.EventTypeLeaseAutoClose))
	state, exists = s.stream(t, tenant, testDenom)
	require.True(t, exists)
	require.Equal(t, uint64(leases-types.MaxAutoClosesPerBlock), state.LeaseCount)
	s.requireInvariants(t)

	// Second EndBlocker finishes the drain and removes the stream.
	s.Ctx = s.Ctx.WithEventManager(sdk.NewEventManager())
	require.NoError(t, s.App.BillingKeeper.EndBlocker(s.Ctx))
	require.Equal(t, leases-types.MaxAutoClosesPerBlock, countEvents(s.Ctx, types.EventTypeLeaseAutoClose))
	_, exists = s.stream(t, tenant, testDenom)
	require.False(t, exists)

	queued, err := s.App.BillingKeeper.ExhaustedStreams.Has(s.Ctx, keeper.NewStreamKey(s.provider.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.False(t, queued, "the drain queue entry goes with the stream")

	closeTime := s.Ctx.BlockTime()
	for _, uuid := range uuids {
		lease, err := s.App.BillingKeeper.GetLease(s.Ctx, uuid)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
		require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason)
		require.NotNil(t, lease.ClosedAt)
		require.Equal(t, closeTime, *lease.ClosedAt)
	}

	account, err := s.App.BillingKeeper.GetCreditAccount(s.Ctx, tenant.String())
	require.NoError(t, err)
	require.Zero(t, account.ActiveLeaseCount)
	require.True(t, account.ReservedAmounts.IsZero(), "reservations are released by the drain")
	s.requireInvariants(t)

	// A third EndBlocker has nothing left to do.
	s.Ctx = s.Ctx.WithEventManager(sdk.NewEventManager())
	require.NoError(t, s.App.BillingKeeper.EndBlocker(s.Ctx))
	require.Zero(t, countEvents(s.Ctx, types.EventTypeLeaseAutoClose))
}

func TestStream_LeaseAcknowledgedIntoFrozenStreamIsClosed(t *testing.T) {
	s := newStreamFixture(t, testDenom)
	tenant := s.TestAccs[0]
	s.fundTenant(t, tenant, testDenom, 3600)
	first := s.lease(t, tenant)

	s.advance(2 * time.Hour)
	s.withdraw(t, 0, nil)
	state, _ := s.stream(t, tenant, testDenom)
	require.True(t, state.IsExhausted())

	// A top-up after the freeze lets the tenant create a new lease, but does
	// not unfreeze the stream: the new lease joins it and is closed with it.
	s.fundTenant(t, tenant, testDenom, 100_000)
	second := s.lease(t, tenant)
	state, _ = s.stream(t, tenant, testDenom)
	require.True(t, state.IsExhausted())
	require.Equal(t, uint64(2), state.LeaseCount)
	s.requireInvariants(t)

	require.NoError(t, s.App.BillingKeeper.EndBlocker(s.Ctx))
	for _, uuid := range []string{first, second} {
		lease, err := s.App.BillingKeeper.GetLease(s.Ctx, uuid)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
		require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason)
	}
	_, exists := s.stream(t, tenant, testDenom)
	require.False(t, exists)
	require.Equal(t, sdkmath.NewInt(100_000), s.credit(tenant, testDenom), "the top-up was not spent on the frozen stream")
	s.requireInvariants(t)

	// With the stream drained, a new lease starts a fresh, healthy stream.
	third := s.lease(t, tenant)
	state, exists = s.stream(t, tenant, testDenom)
	require.True(t, exists)
	require.False(t, state.IsExhausted())
	s.advance(10 * time.Second)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10))), s.withdraw(t, 0, nil).TotalAmounts)
	lease, err := s.App.BillingKeeper.GetLease(s.Ctx, third)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)
	s.requireInvariants(t)
}

func TestStream_UserCloseWhileQueuedForDrain(t *testing.T) {
	s := newStreamFixture(t, testDenom)
	tenant := s.TestAccs[0]
	s.fundTenant(t, tenant, testDenom, 2*3600)
	first := s.lease(t, tenant)
	second := s.lease(t, tenant)

	s.advance(3 * time.Hour)
	s.withdraw(t, 0, nil)

	// The tenant closes one lease itself before the EndBlocker runs. The stream
	// is frozen, so the reason is credit exhaustion whatever the tenant says.
	_, err := s.msgServer.CloseLease(s.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{first},
		Reason:     "done",
	})
	require.NoError(t, err)
	lease, err := s.App.BillingKeeper.GetLease(s.Ctx, first)
	require.NoError(t, err)
	require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason)
	s.requireInvariants(t)

	// Closing the last lease by hand deletes the stream and its queue entry;
	// the EndBlocker then finds nothing to do.
	_, err = s.msgServer.CloseLease(s.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{second},
	})
	require.NoError(t, err)
	_, exists := s.stream(t, tenant, testDenom)
	require.False(t, exists)

	s.Ctx = s.Ctx.WithEventManager(sdk.NewEventManager())
	require.NoError(t, s.App.BillingKeeper.EndBlocker(s.Ctx))
	require.Zero(t, countEvents(s.Ctx, types.EventTypeLeaseAutoClose))
	s.requireInvariants(t)
}

// TestStream_CloseReasonPaidInFull covers ENG-577: a stream freezes when the
// accrual consumes the whole balance (accrued >= balance), which includes the
// case where the credit exactly covers the charge. That is a full payment, so
// the caller's reason is kept; an unpaid remainder is genuine exhaustion.
func TestStream_CloseReasonPaidInFull(t *testing.T) {
	tests := []struct {
		name       string
		elapsed    time.Duration
		wantReason string
		wantPaid   int64
	}{
		{name: "credit left over keeps the caller's reason", elapsed: 1800 * time.Second, wantReason: "done", wantPaid: 1800},
		{name: "credit exactly covers the charge keeps the caller's reason", elapsed: 3600 * time.Second, wantReason: "done", wantPaid: 3600},
		{name: "credit falls short is credit exhaustion", elapsed: 3601 * time.Second, wantReason: types.ClosureReasonCreditExhausted, wantPaid: 3600},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStreamFixture(t, testDenom)
			tenant := s.TestAccs[0]
			s.fundTenant(t, tenant, testDenom, 3600)
			uuid := s.lease(t, tenant)

			s.advance(tc.elapsed)
			resp, err := s.msgServer.CloseLease(s.Ctx, &types.MsgCloseLease{
				Sender:     tenant.String(),
				LeaseUuids: []string{uuid},
				Reason:     "done",
			})
			require.NoError(t, err)
			require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(tc.wantPaid))), resp.TotalSettledAmounts)

			lease, err := s.App.BillingKeeper.GetLease(s.Ctx, uuid)
			require.NoError(t, err)
			require.Equal(t, tc.wantReason, lease.ClosureReason)
			s.requireInvariants(t)
		})
	}
}

func TestStream_MultiDenomLeaseWithMixedStreamStates(t *testing.T) {
	s := newStreamFixture(t, testDenom)
	tenant := s.TestAccs[0]
	skuOther := s.createTestSKUWithDenom(t, s.provider.Uuid, 3600, testDenom2)

	// testDenom is funded for exactly the reservation of one lease; testDenom2
	// is plentiful.
	s.fundTenant(t, tenant, testDenom, 3600)
	s.fundTenant(t, tenant, testDenom2, 1_000_000)

	both := s.createAndAcknowledgeLease(t, s.msgServer, tenant, s.providerAddr, []types.LeaseItemInput{
		{SkuUuid: s.sku.Uuid, Quantity: 1},
		{SkuUuid: skuOther.Uuid, Quantity: 1},
	})
	otherOnly := s.createAndAcknowledgeLease(t, s.msgServer, tenant, s.providerAddr, []types.LeaseItemInput{
		{SkuUuid: skuOther.Uuid, Quantity: 1},
	})

	state, _ := s.stream(t, tenant, testDenom2)
	require.Equal(t, sdkmath.NewInt(2), state.Rate)
	require.Equal(t, uint64(2), state.LeaseCount)
	s.requireInvariants(t)

	// 4000s: testDenom accrues 4000 against 3600 (exhausted); testDenom2
	// accrues 2 × 4000 and stays healthy.
	s.advance(4000 * time.Second)
	resp := s.withdraw(t, 0, nil)
	require.Equal(t, sdk.NewCoins(
		sdk.NewCoin(testDenom, sdkmath.NewInt(3600)),
		sdk.NewCoin(testDenom2, sdkmath.NewInt(8000)),
	), resp.TotalAmounts)
	require.Equal(t, uint64(2), resp.WithdrawalCount)

	exhausted, _ := s.stream(t, tenant, testDenom)
	require.True(t, exhausted.IsExhausted())
	healthy, _ := s.stream(t, tenant, testDenom2)
	require.False(t, healthy.IsExhausted())
	s.requireInvariants(t)

	// The drain closes only the lease priced in the exhausted denom, and takes
	// it out of the healthy stream too.
	require.NoError(t, s.App.BillingKeeper.EndBlocker(s.Ctx))

	lease, err := s.App.BillingKeeper.GetLease(s.Ctx, both)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
	require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason)

	lease, err = s.App.BillingKeeper.GetLease(s.Ctx, otherOnly)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State, "a lease not priced in the exhausted denom survives")

	_, exists := s.stream(t, tenant, testDenom)
	require.False(t, exists)
	healthy, exists = s.stream(t, tenant, testDenom2)
	require.True(t, exists)
	require.Equal(t, sdkmath.NewInt(1), healthy.Rate)
	require.Equal(t, uint64(1), healthy.LeaseCount)
	s.requireInvariants(t)
}

func TestStream_WithdrawPagination(t *testing.T) {
	s := newStreamFixture(t, testDenom)
	tenants := []sdk.AccAddress{s.TestAccs[0], s.TestAccs[3], s.TestAccs[4]}
	for _, tenant := range tenants {
		s.fundTenant(t, tenant, testDenom, 1_000_000)
		s.lease(t, tenant)
	}
	s.advance(100 * time.Second)

	// One stream per page: three pages, the last without a next key.
	var key []byte
	pages := 0
	for {
		resp := s.withdraw(t, 1, key)
		pages++
		require.Equal(t, uint64(1), resp.WithdrawalCount)
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100))), resp.TotalAmounts)
		require.Equal(t, resp.HasMore, len(resp.NextKey) > 0, "next_key is set if and only if has_more")
		if !resp.HasMore {
			break
		}
		key = resp.NextKey
	}
	require.Equal(t, 3, pages)
	require.Equal(t, sdkmath.NewInt(300), s.payout(testDenom))
	s.requireInvariants(t)
}

func TestStream_WithdrawCursor(t *testing.T) {
	cursorCodec := collections.PairKeyCodec(sdk.AccAddressKey, collections.StringKey)
	encode := func(t *testing.T, tenant sdk.AccAddress, denom string) []byte {
		t.Helper()
		cursor := collections.Join(tenant, denom)
		buf := make([]byte, cursorCodec.Size(cursor))
		_, err := cursorCodec.Encode(buf, cursor)
		require.NoError(t, err)
		return buf
	}

	t.Run("a cursor on a deleted stream resumes after it", func(t *testing.T) {
		s := newStreamFixture(t, testDenom)
		tenants := []sdk.AccAddress{s.TestAccs[0], s.TestAccs[3], s.TestAccs[4]}
		leases := make(map[string]string)
		for _, tenant := range tenants {
			s.fundTenant(t, tenant, testDenom, 1_000_000)
			leases[tenant.String()] = s.lease(t, tenant)
		}
		s.advance(100 * time.Second)

		first := s.withdraw(t, 1, nil)
		require.True(t, first.HasMore)

		// The stream the cursor points at disappears between the two pages.
		_, cursor, err := cursorCodec.Decode(first.NextKey)
		require.NoError(t, err)
		gone := cursor.K1()
		_, err = s.msgServer.CloseLease(s.Ctx, &types.MsgCloseLease{
			Sender:     gone.String(),
			LeaseUuids: []string{leases[gone.String()]},
		})
		require.NoError(t, err)

		rest := s.withdraw(t, 10, first.NextKey)
		require.False(t, rest.HasMore)
		require.Equal(t, uint64(2), rest.WithdrawalCount, "the two streams behind the cursor are still served")
		require.Equal(t, sdkmath.NewInt(300), s.payout(testDenom))
	})

	t.Run("a cursor cannot leave the provider's streams", func(t *testing.T) {
		s := newStreamFixture(t, testDenom)
		tenant := s.TestAccs[0]
		s.fundTenant(t, tenant, testDenom, 1_000_000)
		s.lease(t, tenant)

		// A second provider with its own stream and payout address.
		otherProviderAddr := s.TestAccs[3]
		otherPayout := s.TestAccs[4]
		otherProvider := s.createTestProvider(t, otherProviderAddr.String(), otherPayout.String())
		otherSKU := s.createTestSKUWithDenom(t, otherProvider.Uuid, 3600, testDenom)
		s.createAndAcknowledgeLease(t, s.msgServer, tenant, otherProviderAddr, []types.LeaseItemInput{
			{SkuUuid: otherSKU.Uuid, Quantity: 1},
		})
		s.advance(100 * time.Second)

		// Whatever (tenant, denom) the cursor names, iteration stays inside the
		// provider named by the message: an all-zero address sorts before every
		// stream, so the provider's single stream is served and nothing else.
		lowest := encode(t, sdk.AccAddress(make([]byte, 20)), "")
		resp := s.withdraw(t, 10, lowest)
		require.Equal(t, uint64(1), resp.WithdrawalCount)
		require.Equal(t, sdkmath.NewInt(100), s.payout(testDenom))
		require.True(t, s.App.BankKeeper.GetBalance(s.Ctx, otherPayout, testDenom).IsZero(),
			"the other provider's stream was not settled")
	})

	t.Run("a malformed cursor is rejected", func(t *testing.T) {
		s := newStreamFixture(t, testDenom)
		_, err := s.msgServer.Withdraw(s.Ctx, &types.MsgWithdraw{
			Sender:       s.providerAddr.String(),
			ProviderUuid: s.provider.Uuid,
			Key:          []byte{0xff},
		})
		require.ErrorIs(t, err, types.ErrInvalidRequest)
	})

	t.Run("a cursor with a long denom fits the length bound", func(t *testing.T) {
		s := newStreamFixture(t, mainnetLikeDenom)
		tenants := []sdk.AccAddress{s.TestAccs[0], s.TestAccs[3]}
		for _, tenant := range tenants {
			s.fundTenant(t, tenant, mainnetLikeDenom, 1_000_000)
			s.lease(t, tenant)
		}
		s.advance(100 * time.Second)

		first := s.withdraw(t, 1, nil)
		require.True(t, first.HasMore)
		require.LessOrEqual(t, len(first.NextKey), types.MaxWithdrawCursorLen)
		second := s.withdraw(t, 1, first.NextKey)
		require.False(t, second.HasMore)
		require.Equal(t, sdkmath.NewInt(200), s.payout(mainnetLikeDenom))
	})
}

func TestStream_AccrualOverflowPaysRemainingCreditAndFreezes(t *testing.T) {
	s := newStreamFixture(t, testDenom)
	tenant := s.TestAccs[0]
	s.fundTenant(t, tenant, testDenom, 50_000)
	uuid := s.lease(t, tenant)

	// Rewind the stream beyond MaxDurationSeconds (~100 years).
	key := keeper.NewStreamKey(s.provider.Uuid, tenant, testDenom)
	state, err := s.App.BillingKeeper.Streams.Get(s.Ctx, key)
	require.NoError(t, err)
	state.LastSettledAt = s.Ctx.BlockTime().Add(-150 * 365 * 24 * time.Hour)
	require.NoError(t, s.App.BillingKeeper.Streams.Set(s.Ctx, key, state))

	resp := s.withdraw(t, 0, nil)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(50_000))), resp.TotalAmounts,
		"an unrepresentable accrual transfers everything that remains")
	state, _ = s.stream(t, tenant, testDenom)
	require.True(t, state.IsExhausted())

	require.NoError(t, s.App.BillingKeeper.EndBlocker(s.Ctx))
	lease, err := s.App.BillingKeeper.GetLease(s.Ctx, uuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
	require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason)
	s.requireInvariants(t)
}

func TestStream_DrainDropsDanglingAndDriftedEntries(t *testing.T) {
	s := newStreamFixture(t, testDenom)
	tenant := s.TestAccs[0]
	k := s.App.BillingKeeper

	t.Run("queue entry without a stream", func(t *testing.T) {
		key := keeper.NewStreamKey(s.provider.Uuid, tenant, "ghost")
		require.NoError(t, k.ExhaustedStreams.Set(s.Ctx, key, ""))
		require.NoError(t, k.EndBlocker(s.Ctx))
		has, err := k.ExhaustedStreams.Has(s.Ctx, key)
		require.NoError(t, err)
		require.False(t, has)
	})

	t.Run("frozen stream without any member lease", func(t *testing.T) {
		key := keeper.NewStreamKey(s.provider.Uuid, tenant, "drift")
		frozenAt := s.Ctx.BlockTime()
		require.NoError(t, k.Streams.Set(s.Ctx, key, types.StreamState{
			Rate:          sdkmath.NewInt(5),
			LastSettledAt: frozenAt,
			LeaseCount:    3,
			ExhaustedAt:   &frozenAt,
		}))
		require.NoError(t, k.ExhaustedStreams.Set(s.Ctx, key, ""))

		// Nothing can ever drain it, so it is removed rather than rescanned forever.
		require.NoError(t, k.EndBlocker(s.Ctx))
		_, err := k.Streams.Get(s.Ctx, key)
		require.ErrorIs(t, err, collections.ErrNotFound)
		has, err := k.ExhaustedStreams.Has(s.Ctx, key)
		require.NoError(t, err)
		require.False(t, has)
	})

	t.Run("membership entry for a lease that is not active", func(t *testing.T) {
		s.fundTenant(t, tenant, testDenom, 3600)
		uuid := s.lease(t, tenant)
		key := keeper.NewStreamKey(s.provider.Uuid, tenant, testDenom)

		// A stale entry naming a lease that does not exist sorts first and must
		// not block the real lease behind it.
		stale := "00000000-0000-7000-8000-000000000000"
		require.NoError(t, k.StreamLeases.Set(s.Ctx, collections.Join(key, stale)))

		s.advance(2 * time.Hour)
		s.withdraw(t, 0, nil)
		require.NoError(t, k.EndBlocker(s.Ctx))

		has, err := k.StreamLeases.Has(s.Ctx, collections.Join(key, stale))
		require.NoError(t, err)
		require.False(t, has, "the stale entry is removed")
		lease, err := k.GetLease(s.Ctx, uuid)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
		s.requireInvariants(t)
	})
}

func TestStream_DeprecatedPerLeaseSurface(t *testing.T) {
	s := newStreamFixture(t, testDenom)
	tenant := s.TestAccs[0]
	s.fundTenant(t, tenant, testDenom, 1_000_000)
	uuid := s.lease(t, tenant)

	t.Run("MsgWithdraw with lease_uuids is rejected with a pointer to provider_uuid", func(t *testing.T) {
		_, err := s.msgServer.Withdraw(s.Ctx, &types.MsgWithdraw{
			Sender:     s.providerAddr.String(),
			LeaseUuids: []string{uuid}, //nolint:staticcheck // exercising the deprecated field
		})
		require.ErrorIs(t, err, types.ErrInvalidRequest)
		require.ErrorContains(t, err, "provider_uuid")
	})

	t.Run("WithdrawableAmount is unimplemented", func(t *testing.T) {
		querier := keeper.NewQuerier(s.App.BillingKeeper)
		_, err := querier.WithdrawableAmount(s.Ctx, &types.QueryWithdrawableAmountRequest{LeaseUuid: uuid}) //nolint:staticcheck // exercising the deprecated method
		require.Equal(t, codes.Unimplemented, status.Code(err))
		require.ErrorContains(t, err, "StreamsByProvider")
	})

	t.Run("genesis with last_settled_at set is rejected", func(t *testing.T) {
		gs := s.App.BillingKeeper.ExportGenesis(s.Ctx)
		require.NoError(t, gs.Validate())

		settledAt := s.Ctx.BlockTime()
		gs.Leases[0].LastSettledAt = &settledAt //nolint:staticcheck // exercising the deprecated field
		err := gs.Validate()
		require.ErrorIs(t, err, types.ErrInvalidLease)
		require.ErrorContains(t, err, "last_settled_at")
	})
}

func TestStream_Queries(t *testing.T) {
	s := newStreamFixture(t, testDenom)
	querier := keeper.NewQuerier(s.App.BillingKeeper)
	tenantA, tenantB := s.TestAccs[0], s.TestAccs[3]
	s.fundTenant(t, tenantA, testDenom, 1_000_000)
	s.fundTenant(t, tenantB, testDenom, 3600)
	s.lease(t, tenantA)
	s.lease(t, tenantA)
	s.lease(t, tenantB)
	s.advance(100 * time.Second)

	t.Run("ProviderWithdrawable sums the streams", func(t *testing.T) {
		resp, err := querier.ProviderWithdrawable(s.Ctx, &types.QueryProviderWithdrawableRequest{ProviderUuid: s.provider.Uuid})
		require.NoError(t, err)
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(300))), resp.Amounts)
		require.Equal(t, uint64(3), resp.LeaseCount, "leases covered by streams with a nonzero amount")

		// It matches what a withdrawal then pays.
		paid := s.withdraw(t, 0, nil)
		require.Equal(t, resp.Amounts, paid.TotalAmounts)
	})

	t.Run("StreamsByProvider and StreamsByTenant", func(t *testing.T) {
		byProvider, err := querier.StreamsByProvider(s.Ctx, &types.QueryStreamsByProviderRequest{ProviderUuid: s.provider.Uuid})
		require.NoError(t, err)
		require.Len(t, byProvider.Streams, 2)

		byTenant, err := querier.StreamsByTenant(s.Ctx, &types.QueryStreamsByTenantRequest{Tenant: tenantA.String()})
		require.NoError(t, err)
		require.Len(t, byTenant.Streams, 1)
		require.Equal(t, s.provider.Uuid, byTenant.Streams[0].ProviderUuid)
		require.Equal(t, tenantA.String(), byTenant.Streams[0].Tenant)
		require.Equal(t, testDenom, byTenant.Streams[0].Denom)
		require.Equal(t, sdkmath.NewInt(2), byTenant.Streams[0].State.Rate)
		require.Equal(t, uint64(2), byTenant.Streams[0].State.LeaseCount)
	})

	t.Run("CreditEstimate deducts unsettled accrual", func(t *testing.T) {
		// tenantB: balance 3600 - 100 already paid = 3500, rate 1/s, and 50s
		// accrued but not yet settled.
		s.advance(50 * time.Second)
		resp, err := querier.CreditEstimate(s.Ctx, &types.QueryCreditEstimateRequest{Tenant: tenantB.String()})
		require.NoError(t, err)
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1))), resp.TotalRatePerSecond)
		require.Equal(t, uint64(1), resp.ActiveLeaseCount)
		require.Equal(t, uint64(3450), resp.EstimatedDurationSeconds)
	})
}

// TestStream_TenantAddressSpelling checks that streams identify a tenant by
// its address, not by how the address was spelled. Bech32 is also valid in
// upper case and the handlers store the tenant string as it was sent, so the
// same tenant can appear under two spellings in leases while stream keys and
// exported streams always use the canonical lower-case form.
func TestStream_TenantAddressSpelling(t *testing.T) {
	tests := []struct {
		name string
		// spelling of the tenant in MsgFundCredit and in MsgCreateLease
		fund, lease func(addr sdk.AccAddress) string
	}{
		{
			name:  "upper case everywhere",
			fund:  func(a sdk.AccAddress) string { return strings.ToUpper(a.String()) },
			lease: func(a sdk.AccAddress) string { return strings.ToUpper(a.String()) },
		},
		{
			name:  "funded in lower case, leased in upper case",
			fund:  func(a sdk.AccAddress) string { return a.String() },
			lease: func(a sdk.AccAddress) string { return strings.ToUpper(a.String()) },
		},
		{
			name:  "funded in upper case, leased in lower case",
			fund:  func(a sdk.AccAddress) string { return strings.ToUpper(a.String()) },
			lease: func(a sdk.AccAddress) string { return a.String() },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStreamFixture(t, testDenom)
			tenant := s.TestAccs[0]
			funder := s.TestAccs[3]
			s.fundAccount(t, funder, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))

			_, err := s.msgServer.FundCredit(s.Ctx, &types.MsgFundCredit{
				Sender: funder.String(),
				Tenant: tc.fund(tenant),
				Amount: sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000)),
			})
			require.NoError(t, err)

			// Two leases, one per spelling, must share one stream. Each is
			// acknowledged in its own message: the batch handlers key their
			// credit-account bookkeeping by the stored tenant string, which is
			// a separate matter from how streams identify a tenant.
			for _, spelling := range []string{tc.lease(tenant), tenant.String()} {
				created, err := s.msgServer.CreateLease(s.Ctx, &types.MsgCreateLease{
					Tenant: spelling,
					Items:  []types.LeaseItemInput{{SkuUuid: s.sku.Uuid, Quantity: 1}},
				})
				require.NoError(t, err)
				_, err = s.msgServer.AcknowledgeLease(s.Ctx, &types.MsgAcknowledgeLease{
					Sender:     s.providerAddr.String(),
					LeaseUuids: []string{created.LeaseUuid},
				})
				require.NoError(t, err)
			}

			state, exists := s.stream(t, tenant, testDenom)
			require.True(t, exists)
			require.Equal(t, sdkmath.NewInt(2), state.Rate)
			require.Equal(t, uint64(2), state.LeaseCount)

			// The stream invariant holds and the exported genesis validates and
			// re-imports (requireInvariants runs both).
			s.requireInvariants(t)
			gs := s.App.BillingKeeper.ExportGenesis(s.Ctx)
			require.NoError(t, gs.Validate())
			require.Len(t, gs.Streams, 1)
			require.Equal(t, tenant.String(), gs.Streams[0].Tenant, "streams are exported under the canonical spelling")

			s.advance(100 * time.Second)
			require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(200))), s.withdraw(t, 0, nil).TotalAmounts)
			s.requireInvariants(t)
		})
	}
}

// TestStream_LargeRateDoesNotOverflowAccrual checks that a rate whose product
// with the elapsed nanoseconds does not fit 256 bits is still settled exactly.
// Only the intermediate is wide; the charge itself fits, so the stream must
// pay that charge and must not be treated as exhausted.
func TestStream_LargeRateDoesNotOverflowAccrual(t *testing.T) {
	s := newStreamFixture(t, testDenom)
	tenant := s.TestAccs[0]

	// 10^70 per second: the rate has 233 bits, and rate × 1e9 ns has 263.
	perSecond, ok := sdkmath.NewIntFromString("1" + strings.Repeat("0", 70))
	require.True(t, ok)
	perHour := perSecond.MulRaw(3600)

	skuUUID, err := s.App.SKUKeeper.GenerateSKUUUID(s.Ctx)
	require.NoError(t, err)
	require.NoError(t, s.App.SKUKeeper.SetSKU(s.Ctx, skutypes.SKU{
		Uuid:         skuUUID,
		ProviderUuid: s.provider.Uuid,
		Name:         "huge",
		Unit:         skutypes.Unit_UNIT_PER_HOUR,
		BasePrice:    sdk.NewCoin(testDenom, perHour),
		Active:       true,
	}))

	// Credit for exactly one hour (also the reservation of the lease).
	creditAddr := types.DeriveCreditAddress(tenant)
	s.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, perHour)))
	require.NoError(t, s.App.BillingKeeper.SetCreditAccount(s.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	}))
	s.createAndAcknowledgeLease(t, s.msgServer, tenant, s.providerAddr, []types.LeaseItemInput{
		{SkuUuid: skuUUID, Quantity: 1},
	})

	// One second: one second's worth is paid, not the whole balance.
	s.advance(time.Second)
	resp := s.withdraw(t, 0, nil)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, perSecond)), resp.TotalAmounts)
	state, _ := s.stream(t, tenant, testDenom)
	require.False(t, state.IsExhausted(), "an intermediate overflow is not credit exhaustion")
	require.True(t, state.Remainder.IsZero())

	// Half a second: the sub-second part is exact as well.
	s.advance(500 * time.Millisecond)
	resp = s.withdraw(t, 0, nil)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, perSecond.QuoRaw(2))), resp.TotalAmounts)
	s.requireInvariants(t)
}

// TestStream_CreditEstimateWithHugeUnsettledAccrual checks that CreditEstimate
// reports an exhausted tenant, instead of panicking, when the accruals of its
// streams each fit sdkmath.Int but their sum does not.
func TestStream_CreditEstimateWithHugeUnsettledAccrual(t *testing.T) {
	s := newStreamFixture(t, testDenom)
	tenant := s.TestAccs[0]
	querier := keeper.NewQuerier(s.App.BillingKeeper)

	// 10^70 per second.
	perSecond, ok := sdkmath.NewIntFromString("1" + strings.Repeat("0", 70))
	require.True(t, ok)
	perHour := perSecond.MulRaw(3600)

	hugeSKU := func(providerUUID string) string {
		uuid, err := s.App.SKUKeeper.GenerateSKUUUID(s.Ctx)
		require.NoError(t, err)
		require.NoError(t, s.App.SKUKeeper.SetSKU(s.Ctx, skutypes.SKU{
			Uuid:         uuid,
			ProviderUuid: providerUUID,
			Name:         "huge",
			Unit:         skutypes.Unit_UNIT_PER_HOUR,
			BasePrice:    sdk.NewCoin(testDenom, perHour),
			Active:       true,
		}))
		return uuid
	}

	// Two providers, so the tenant has two streams in the same denom.
	otherProviderAddr := s.TestAccs[3]
	otherProvider := s.createTestProvider(t, otherProviderAddr.String(), s.TestAccs[4].String())

	// Credit for the two reservations (one hour each).
	creditAddr := types.DeriveCreditAddress(tenant)
	s.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, perHour.MulRaw(2))))
	require.NoError(t, s.App.BillingKeeper.SetCreditAccount(s.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	}))
	s.createAndAcknowledgeLease(t, s.msgServer, tenant, s.providerAddr, []types.LeaseItemInput{
		{SkuUuid: hugeSKU(s.provider.Uuid), Quantity: 1},
	})
	s.createAndAcknowledgeLease(t, s.msgServer, tenant, otherProviderAddr, []types.LeaseItemInput{
		{SkuUuid: hugeSKU(otherProvider.Uuid), Quantity: 1},
	})

	// After 10^7 seconds each stream has accrued 10^77, which fits 256 bits;
	// the two together (2 × 10^77) do not.
	s.advance(10_000_000 * time.Second)

	resp, err := querier.CreditEstimate(s.Ctx, &types.QueryCreditEstimateRequest{Tenant: tenant.String()})
	require.NoError(t, err)
	require.Zero(t, resp.EstimatedDurationSeconds, "what is owed exceeds the balance many times over")
	require.Equal(t, uint64(2), resp.ActiveLeaseCount)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, perSecond.MulRaw(2))), resp.TotalRatePerSecond)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, perHour.MulRaw(2))), resp.CurrentBalance)
}
