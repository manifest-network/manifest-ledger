/*
Package keeper_test contains unit tests for the billing store migrations.

Test Coverage (Migrate2to3, per-lease settlement to settlement streams):
  - Streams are built from the ACTIVE leases and accrue from the upgrade time
  - What the leases had accrued is carried as the stream's arrears; the
    migration moves no coins and the first settlement pays them
  - A tenant whose credit cannot cover what is owed is frozen by that first
    settlement, and the EndBlocker then drains the stream
  - CLOSED leases: debt joins the stream's arrears, and forms no stream alone
  - Missing and future last_settled_at values
  - The migration does not depend on the provider or on the bank module
  - Tenants are grouped by address, whatever the spelling stored in the lease
  - The deprecated field is cleared, stored bytes are canonical, indexes intact
  - Large and unrepresentable debts
  - A model of per-lease settlement, on random v2 states
*/
package keeper_test

import (
	"fmt"
	"math/big"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	pkguuid "github.com/manifest-network/manifest-ledger/pkg/uuid"
	billing "github.com/manifest-network/manifest-ledger/x/billing"
	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

// v2State seeds billing state as consensus version 2 left it: leases carrying
// last_settled_at and no streams.
type v2State struct {
	*testFixture
	t   *testing.T
	seq int
}

func newV2State(t *testing.T) *v2State {
	t.Helper()
	return &v2State{testFixture: initFixture(t), t: t}
}

// provider creates a provider with its own payout address and a SKU priced at
// 1 per second in testDenom.
func (s *v2State) provider() (skutypes.Provider, skutypes.SKU, sdk.AccAddress) {
	s.t.Helper()
	s.seq++
	payout := sdk.AccAddress([]byte{byte(s.seq), 'p', 'a', 'y', 'o', 'u', 't', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1})
	provider := s.createTestProvider(s.t, s.TestAccs[1].String(), payout.String())
	sku := s.createTestSKUWithDenom(s.t, provider.Uuid, 3600, testDenom)
	return provider, sku, payout
}

// fund gives the tenant a credit account holding amount of testDenom.
func (s *v2State) fund(tenant sdk.AccAddress, amount int64) {
	s.t.Helper()
	creditAddr := types.DeriveCreditAddress(tenant)
	if amount > 0 {
		s.fundAccount(s.t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(amount))))
	}
	if _, err := s.App.BillingKeeper.GetCreditAccount(s.Ctx, tenant.String()); err != nil {
		require.NoError(s.t, s.App.BillingKeeper.SetCreditAccount(s.Ctx, types.CreditAccount{
			Tenant:        tenant.String(),
			CreditAddress: creditAddr.String(),
		}))
	}
}

// lease stores a v2-shaped lease directly, bypassing the handlers (which no
// longer write last_settled_at), and keeps the credit account's counters and
// reservation consistent with it.
func (s *v2State) lease(tenant sdk.AccAddress, providerUUID, skuUUID string, state types.LeaseState, settledAt, closedAt *time.Time) string {
	s.t.Helper()
	return s.leasePriced(sdkmath.NewInt(1), tenant, providerUUID, skuUUID, state, settledAt, closedAt)
}

// leasePriced is lease with an explicit per-second price.
func (s *v2State) leasePriced(perSecond sdkmath.Int, tenant sdk.AccAddress, providerUUID, skuUUID string, state types.LeaseState, settledAt, closedAt *time.Time) string {
	s.t.Helper()
	k := s.App.BillingKeeper

	leaseSeq, err := k.GetNextLeaseSequence(s.Ctx)
	require.NoError(s.t, err)
	uuid := pkguuid.GenerateUUIDv7(s.Ctx, types.ModuleName, leaseSeq)
	params, err := k.GetParams(s.Ctx)
	require.NoError(s.t, err)

	ackAt := s.Ctx.BlockTime().Add(-24 * time.Hour)
	lease := types.Lease{
		Uuid:                       uuid,
		Tenant:                     tenant.String(),
		ProviderUuid:               providerUUID,
		Items:                      []types.LeaseItem{{SkuUuid: skuUUID, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, perSecond)}},
		State:                      state,
		CreatedAt:                  ackAt,
		AcknowledgedAt:             &ackAt,
		ClosedAt:                   closedAt,
		LastSettledAt:              settledAt, //nolint:staticcheck // seeding the deprecated field as v2 wrote it
		MinLeaseDurationAtCreation: params.MinLeaseDuration,
	}
	require.NoError(s.t, k.SetLease(s.Ctx, lease))

	if state == types.LEASE_STATE_ACTIVE {
		account, err := k.GetCreditAccount(s.Ctx, tenant.String())
		require.NoError(s.t, err)
		account.ActiveLeaseCount++
		account.ReservedAmounts = types.AddReservation(account.ReservedAmounts, types.GetLeaseReservationAmount(&lease, params.MinLeaseDuration))
		require.NoError(s.t, k.SetCreditAccount(s.Ctx, account))
	}
	return uuid
}

func (s *v2State) migrate() {
	s.t.Helper()
	require.NoError(s.t, keeper.NewMigrator(s.App.BillingKeeper).Migrate2to3(s.Ctx))
}

// withdraw has the provider (owned by TestAccs[1], see provider) withdraw.
func (s *v2State) withdraw(providerUUID string) *types.MsgWithdrawResponse {
	s.t.Helper()
	resp, err := keeper.NewMsgServerImpl(s.App.BillingKeeper).Withdraw(s.Ctx, &types.MsgWithdraw{
		Sender:       s.TestAccs[1].String(),
		ProviderUuid: providerUUID,
	})
	require.NoError(s.t, err)
	return resp
}

func (s *v2State) balance(addr sdk.AccAddress) sdkmath.Int {
	return s.App.BankKeeper.GetBalance(s.Ctx, addr, testDenom).Amount
}

func (s *v2State) requireInvariants() {
	s.t.Helper()
	requireInvariants(s.t, s.testFixture)
}

func ago(now time.Time, d time.Duration) *time.Time {
	t := now.Add(-d)
	return &t
}

func TestMigrate2to3_ConsensusVersion(t *testing.T) {
	require.EqualValues(t, 3, billing.ConsensusVersion)
}

func TestMigrate2to3_EmptyState(t *testing.T) {
	s := newV2State(t)
	s.migrate()

	streams, err := s.App.BillingKeeper.GetAllStreams(s.Ctx)
	require.NoError(t, err)
	require.Empty(t, streams)
	s.requireInvariants()
}

func TestMigrate2to3_BuildsStreamsCarryingAccrualAsArrears(t *testing.T) {
	s := newV2State(t)
	now := s.Ctx.BlockTime()
	tenant := s.TestAccs[0]
	provider, sku, payout := s.provider()
	s.fund(tenant, 1_000_000)

	// Three ACTIVE leases last settled at different times: 100s + 50s + 10s owed.
	uuids := []string{
		s.lease(tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_ACTIVE, ago(now, 100*time.Second), nil),
		s.lease(tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_ACTIVE, ago(now, 50*time.Second), nil),
		s.lease(tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_ACTIVE, ago(now, 10*time.Second), nil),
	}

	s.Ctx = s.Ctx.WithEventManager(sdk.NewEventManager())
	s.migrate()

	require.True(t, s.balance(payout).IsZero(), "the migration moves no coins")
	require.Equal(t, sdkmath.NewInt(1_000_000), s.balance(types.DeriveCreditAddress(tenant)))
	require.Zero(t, countEvents(s.Ctx, "transfer"))

	state, err := s.App.BillingKeeper.Streams.Get(s.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(3), state.Rate)
	require.Equal(t, uint64(3), state.LeaseCount)
	require.Equal(t, now, state.LastSettledAt, "the stream accrues from the upgrade time")
	require.Equal(t, sdkmath.NewInt(160), state.Arrears, "what each lease accrued up to the upgrade is carried as arrears")
	require.True(t, state.Remainder.IsZero())
	require.False(t, state.IsExhausted())

	for _, uuid := range uuids {
		lease, err := s.App.BillingKeeper.GetLease(s.Ctx, uuid)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)
		require.Nil(t, lease.LastSettledAt) //nolint:staticcheck // asserting the deprecated field is cleared
	}
	s.requireInvariants()

	// The first withdrawal, 10s later, pays the arrears and 3 leases × 10s with
	// one transfer, and clears the arrears.
	s.Ctx = s.Ctx.WithBlockTime(now.Add(10 * time.Second)).WithEventManager(sdk.NewEventManager())
	resp := s.withdraw(provider.Uuid)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(190))), resp.TotalAmounts)
	require.Equal(t, sdkmath.NewInt(190), s.balance(payout))
	require.Equal(t, 1, countEvents(s.Ctx, "transfer"), "one transfer per stream, not per lease")

	state, err = s.App.BillingKeeper.Streams.Get(s.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.True(t, state.Arrears.IsZero(), "arrears are paid once")

	// The next one only pays what accrued since.
	s.Ctx = s.Ctx.WithBlockTime(now.Add(20 * time.Second))
	resp = s.withdraw(provider.Uuid)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(30))), resp.TotalAmounts)
	s.requireInvariants()
}

func TestMigrate2to3_ExhaustedTenantFreezesAtFirstSettlement(t *testing.T) {
	s := newV2State(t)
	now := s.Ctx.BlockTime()
	tenant := s.TestAccs[0]
	provider, sku, payout := s.provider()
	s.fund(tenant, 7_200)

	// Two leases owe 2 × 10000 against a balance of 7200.
	first := s.lease(tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_ACTIVE, ago(now, 10_000*time.Second), nil)
	second := s.lease(tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_ACTIVE, ago(now, 10_000*time.Second), nil)

	s.migrate()

	// Exhaustion is discovered by a settlement, as it was under per-lease
	// settlement; the migration itself settles nothing.
	state, err := s.App.BillingKeeper.Streams.Get(s.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.False(t, state.IsExhausted())
	require.Equal(t, sdkmath.NewInt(20_000), state.Arrears)
	require.True(t, s.balance(payout).IsZero())
	s.requireInvariants()

	resp := s.withdraw(provider.Uuid)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(7_200))), resp.TotalAmounts, "the provider receives everything that remains")
	require.True(t, s.balance(types.DeriveCreditAddress(tenant)).IsZero())

	state, err = s.App.BillingKeeper.Streams.Get(s.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.True(t, state.IsExhausted())
	require.True(t, state.Arrears.IsZero(), "what the credit could not cover is not carried by a frozen stream")
	require.Equal(t, uint64(2), state.LeaseCount)
	s.requireInvariants()

	// The normal EndBlocker drain closes the leases.
	require.NoError(t, s.App.BillingKeeper.EndBlocker(s.Ctx))
	for _, uuid := range []string{first, second} {
		lease, err := s.App.BillingKeeper.GetLease(s.Ctx, uuid)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
		require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason)
	}
	s.requireInvariants()
}

// TestMigrate2to3_TopUpBeforeFirstSettlementCounts checks that a tenant short
// of credit at the upgrade is not frozen by it: credit added before the
// stream's first settlement pays the arrears and keeps the leases running.
func TestMigrate2to3_TopUpBeforeFirstSettlementCounts(t *testing.T) {
	s := newV2State(t)
	now := s.Ctx.BlockTime()
	tenant := s.TestAccs[0]
	provider, sku, payout := s.provider()
	s.fund(tenant, 7_200)

	// Two leases owe 2 × 10000 against a balance of 7200.
	s.lease(tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_ACTIVE, ago(now, 10_000*time.Second), nil)
	s.lease(tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_ACTIVE, ago(now, 10_000*time.Second), nil)

	s.migrate()
	s.fund(tenant, 1_000_000)

	resp := s.withdraw(provider.Uuid)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(20_000))), resp.TotalAmounts)
	require.Equal(t, sdkmath.NewInt(20_000), s.balance(payout))

	state, err := s.App.BillingKeeper.Streams.Get(s.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.False(t, state.IsExhausted())
	require.True(t, state.Arrears.IsZero())
	s.requireInvariants()
}

func TestMigrate2to3_ClosedLeasesFormNoStream(t *testing.T) {
	s := newV2State(t)
	now := s.Ctx.BlockTime()
	tenant := s.TestAccs[0]
	provider, sku, payout := s.provider()
	s.fund(tenant, 1_000_000)

	closedAt := ago(now, time.Hour)
	// Settled up to its closure, as every handler-closed lease is.
	settled := s.lease(tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_CLOSED, closedAt, closedAt)
	// Still owing 40s up to its closure (only reachable through genesis). With
	// no ACTIVE lease there is no stream to carry it: it is logged, not paid.
	residual := s.lease(tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_CLOSED, ago(*closedAt, 40*time.Second), closedAt)

	s.migrate()

	require.True(t, s.balance(payout).IsZero(), "the migration moves no coins")
	streams, err := s.App.BillingKeeper.GetAllStreams(s.Ctx)
	require.NoError(t, err)
	require.Empty(t, streams, "closed leases form no stream")

	for _, uuid := range []string{settled, residual} {
		lease, err := s.App.BillingKeeper.GetLease(s.Ctx, uuid)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
		require.Nil(t, lease.LastSettledAt) //nolint:staticcheck // asserting the deprecated field is cleared
	}
	s.requireInvariants()
}

// TestMigrate2to3_ClosedLeaseDebtJoinsTheStreamArrears checks that what a
// CLOSED lease still owed is carried by the stream of the same provider,
// tenant and denom, as an amount. That debt has nothing to do with the
// stream's rate (which only counts ACTIVE leases): 4e9 units owed against a
// rate of 1/s is "127 years of the rate", yet it is an ordinary, payable debt.
func TestMigrate2to3_ClosedLeaseDebtJoinsTheStreamArrears(t *testing.T) {
	s := newV2State(t)
	now := s.Ctx.BlockTime()
	tenant := s.TestAccs[0]
	provider, sku, payout := s.provider()
	s.fund(tenant, 5_000_000_000)

	// A CLOSED lease at 4e9/s owes the second from T-2s to T-1s.
	closedAt := ago(now, time.Second)
	s.leasePriced(sdkmath.NewInt(4_000_000_000), tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_CLOSED, ago(now, 2*time.Second), closedAt)
	// An ACTIVE lease at 1/s is settled up to the upgrade.
	s.lease(tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_ACTIVE, ago(now, 0), nil)

	s.migrate()

	state, err := s.App.BillingKeeper.Streams.Get(s.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(1), state.Rate, "only the ACTIVE lease sets the rate")
	require.Equal(t, sdkmath.NewInt(4_000_000_000), state.Arrears)
	require.Equal(t, now, state.LastSettledAt)
	s.requireInvariants()

	// One second later: the debt plus one second of the ACTIVE lease, and the
	// stream keeps running on the credit that is left.
	s.Ctx = s.Ctx.WithBlockTime(now.Add(time.Second))
	resp := s.withdraw(provider.Uuid)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(4_000_000_001))), resp.TotalAmounts)
	require.Equal(t, sdkmath.NewInt(4_000_000_001), s.balance(payout))
	require.Equal(t, sdkmath.NewInt(999_999_999), s.balance(types.DeriveCreditAddress(tenant)))

	state, err = s.App.BillingKeeper.Streams.Get(s.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.False(t, state.IsExhausted())
	require.True(t, state.Arrears.IsZero())
	s.requireInvariants()
}

func TestMigrate2to3_MissingAndFutureLastSettledAt(t *testing.T) {
	s := newV2State(t)
	now := s.Ctx.BlockTime()
	tenant := s.TestAccs[0]
	provider, sku, _ := s.provider()
	s.fund(tenant, 1_000_000_000)

	// No last_settled_at: billing started at acknowledgement, 24h ago.
	s.lease(tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_ACTIVE, nil, nil)
	// last_settled_at in the future: nothing is owed yet.
	future := now.Add(time.Hour)
	s.lease(tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_ACTIVE, &future, nil)

	s.migrate()

	state, err := s.App.BillingKeeper.Streams.Get(s.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(24*3600), state.Arrears)
	require.Equal(t, sdkmath.NewInt(2), state.Rate)
	require.Equal(t, now, state.LastSettledAt, "a future last_settled_at does not leak into the stream")
	s.requireInvariants()
}

// TestMigrate2to3_DoesNotDependOnProviderOrBank checks that the migration
// succeeds whatever the state of the provider or of its payout address, since
// it pays nothing, and that the arrears stay on the stream until a settlement
// can actually pay them.
func TestMigrate2to3_DoesNotDependOnProviderOrBank(t *testing.T) {
	s := newV2State(t)
	now := s.Ctx.BlockTime()
	tenant := s.TestAccs[0]
	provider, sku, payout := s.provider()
	s.fund(tenant, 1_000_000)

	// A lease naming a provider that does not exist, and one whose provider has
	// an unusable payout address.
	missingProvider := "01912345-0000-7000-8000-00000000dead"
	s.lease(tenant, missingProvider, sku.Uuid, types.LEASE_STATE_ACTIVE, ago(now, 500*time.Second), nil)
	// Two leases at 1/s, one settled at the upgrade and one a second earlier:
	// 1 unit is owed against a stream rate of 2/s, less than a second of it.
	s.lease(tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_ACTIVE, ago(now, 0), nil)
	s.lease(tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_ACTIVE, ago(now, time.Second), nil)
	broken := provider
	broken.PayoutAddress = "not-an-address"
	require.NoError(t, s.App.SKUKeeper.SetProvider(s.Ctx, broken))

	s.migrate()
	require.Equal(t, sdkmath.NewInt(1_000_000), s.balance(types.DeriveCreditAddress(tenant)))

	orphan, err := s.App.BillingKeeper.Streams.Get(s.Ctx, keeper.NewStreamKey(missingProvider, tenant, testDenom))
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(500), orphan.Arrears)
	require.Equal(t, now, orphan.LastSettledAt)

	key := keeper.NewStreamKey(provider.Uuid, tenant, testDenom)
	state, err := s.App.BillingKeeper.Streams.Get(s.Ctx, key)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(1), state.Arrears)
	s.requireInvariants()

	// A settlement that cannot pay leaves the stream, arrears included, as it was.
	s.Ctx = s.Ctx.WithBlockTime(now.Add(time.Second))
	resp := s.withdraw(provider.Uuid)
	require.True(t, resp.TotalAmounts.IsZero())
	unchanged, err := s.App.BillingKeeper.Streams.Get(s.Ctx, key)
	require.NoError(t, err)
	require.Equal(t, state, unchanged)

	// Once the payout works again, the provider receives the carried unit plus
	// one second of both leases: 1 + 2.
	require.NoError(t, s.App.SKUKeeper.SetProvider(s.Ctx, provider))
	resp = s.withdraw(provider.Uuid)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(3))), resp.TotalAmounts)
	require.Equal(t, sdkmath.NewInt(3), s.balance(payout))
	s.requireInvariants()
}

func TestMigrate2to3_RewritesLeasesCanonicallyWithoutTouchingIndexes(t *testing.T) {
	s := newV2State(t)
	now := s.Ctx.BlockTime()
	tenant := s.TestAccs[0]
	provider, sku, _ := s.provider()
	s.fund(tenant, 1_000_000)
	uuid := s.lease(tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_ACTIVE, ago(now, 100*time.Second), nil)

	store := s.Ctx.KVStore(s.App.GetKey(types.StoreKey))
	leaseKey, err := collections.EncodeKeyWithPrefix(types.LeaseKey.Bytes(), collections.StringKey, uuid)
	require.NoError(t, err)

	before := store.Get(leaseKey)
	require.NotEmpty(t, before)

	s.migrate()

	lease, err := s.App.BillingKeeper.GetLease(s.Ctx, uuid)
	require.NoError(t, err)
	canonical, err := s.App.AppCodec().Marshal(&lease)
	require.NoError(t, err)

	after := store.Get(leaseKey)
	require.Equal(t, canonical, after, "the stored bytes are exactly the re-encoded lease")
	require.Less(t, len(after), len(before), "the deprecated field is gone from the stored value")

	// Every index still resolves the lease.
	byProvider, err := s.App.BillingKeeper.GetLeasesByProviderAndState(s.Ctx, provider.Uuid, types.LEASE_STATE_ACTIVE)
	require.NoError(t, err)
	require.Len(t, byProvider, 1)
	byTenant, err := s.App.BillingKeeper.GetLeasesByTenantAndState(s.Ctx, tenant.String(), types.LEASE_STATE_ACTIVE)
	require.NoError(t, err)
	require.Len(t, byTenant, 1)
	bySKU, err := s.App.BillingKeeper.GetLeasesBySKU(s.Ctx, sku.Uuid)
	require.NoError(t, err)
	require.Len(t, bySKU, 1)
}

// TestMigrate2to3_ShortCreditIsPaidInSettlementOrder checks a tenant that owes
// two providers more than it holds: as under per-lease settlement, whoever
// settles first is paid first.
func TestMigrate2to3_ShortCreditIsPaidInSettlementOrder(t *testing.T) {
	s := newV2State(t)
	now := s.Ctx.BlockTime()
	tenant := s.TestAccs[0]
	s.fund(tenant, 7_200)

	// One tenant owes two providers 5000 each but holds 7200.
	providerA, skuA, payoutA := s.provider()
	providerB, skuB, payoutB := s.provider()
	s.lease(tenant, providerA.Uuid, skuA.Uuid, types.LEASE_STATE_ACTIVE, ago(now, 5_000*time.Second), nil)
	s.lease(tenant, providerB.Uuid, skuB.Uuid, types.LEASE_STATE_ACTIVE, ago(now, 5_000*time.Second), nil)

	s.migrate()
	s.requireInvariants()

	s.withdraw(providerB.Uuid)
	s.withdraw(providerA.Uuid)
	require.Equal(t, sdkmath.NewInt(5_000), s.balance(payoutB))
	require.Equal(t, sdkmath.NewInt(2_200), s.balance(payoutA))

	first, err := s.App.BillingKeeper.Streams.Get(s.Ctx, keeper.NewStreamKey(providerB.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.False(t, first.IsExhausted(), "paid in full with credit to spare")
	second, err := s.App.BillingKeeper.Streams.Get(s.Ctx, keeper.NewStreamKey(providerA.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.True(t, second.IsExhausted(), "the credit ran out on the second stream")
	s.requireInvariants()
}

// TestMigrate2to3_TenantAddressSpelling checks that the migration groups
// leases by tenant address, not by how the address was spelled. Bech32 is also
// valid in upper case and v2 stored the tenant string as it was sent; an
// upgrade must not fail, or split one tenant into two streams, because of it.
func TestMigrate2to3_TenantAddressSpelling(t *testing.T) {
	s := newV2State(t)
	now := s.Ctx.BlockTime()
	tenant := s.TestAccs[0]
	provider, sku, _ := s.provider()
	s.fund(tenant, 1_000_000)

	lower := s.lease(tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_ACTIVE, ago(now, 100*time.Second), nil)
	upper := s.lease(tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_ACTIVE, ago(now, 50*time.Second), nil)

	// Respell the second lease's tenant in upper case, as a v2 handler would
	// have stored it had the message been sent that way.
	lease, err := s.App.BillingKeeper.GetLease(s.Ctx, upper)
	require.NoError(t, err)
	lease.Tenant = strings.ToUpper(tenant.String())
	require.NoError(t, s.App.BillingKeeper.SetLease(s.Ctx, lease))

	s.migrate()

	streams, err := s.App.BillingKeeper.GetAllStreams(s.Ctx)
	require.NoError(t, err)
	require.Len(t, streams, 1, "both spellings are one tenant, so one stream")
	require.Equal(t, tenant.String(), streams[0].Tenant)
	require.Equal(t, sdkmath.NewInt(2), streams[0].State.Rate)
	require.Equal(t, uint64(2), streams[0].State.LeaseCount)
	require.Equal(t, sdkmath.NewInt(150), streams[0].State.Arrears)

	for _, uuid := range []string{lower, upper} {
		member, err := s.App.BillingKeeper.StreamLeases.Has(s.Ctx,
			collections.Join(keeper.NewStreamKey(provider.Uuid, tenant, testDenom), uuid))
		require.NoError(t, err)
		require.True(t, member)
	}
	s.requireInvariants()
}

// hugeCredit funds the tenant's credit account with 2^253 of testDenom.
func (s *v2State) hugeCredit(tenant sdk.AccAddress) sdkmath.Int {
	s.t.Helper()
	credit := sdkmath.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 253))
	creditAddr := types.DeriveCreditAddress(tenant)
	s.fundAccount(s.t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, credit)))
	require.NoError(s.t, s.App.BillingKeeper.SetCreditAccount(s.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	}))
	return credit
}

// TestMigrate2to3_LargeArrearsAreExact checks that a debt is carried exactly
// whatever its size: here the debt, the rate and the balance all fit
// sdkmath.Int, but the debt scaled to nanosecond units would not.
func TestMigrate2to3_LargeArrearsAreExact(t *testing.T) {
	s := newV2State(t)
	now := s.Ctx.BlockTime()
	tenant := s.TestAccs[0]
	provider, sku, payout := s.provider()
	s.hugeCredit(tenant)

	// 2^240 per second, one second unsettled at the upgrade: 2^240 is owed.
	rate := sdkmath.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 240))
	s.leasePriced(rate, tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_ACTIVE, ago(now, time.Second), nil)

	s.migrate()
	state, err := s.App.BillingKeeper.Streams.Get(s.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.Equal(t, rate, state.Arrears)
	s.requireInvariants()

	// One second later: the carried second plus the new one.
	s.Ctx = s.Ctx.WithBlockTime(now.Add(time.Second))
	resp := s.withdraw(provider.Uuid)
	want := rate.MulRaw(2)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, want)), resp.TotalAmounts)
	require.True(t, s.balance(payout).Equal(want))

	state, err = s.App.BillingKeeper.Streams.Get(s.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.False(t, state.IsExhausted())
	require.True(t, state.Arrears.IsZero())
	require.True(t, state.Remainder.IsZero())
	s.requireInvariants()
}

// TestMigrate2to3_UnrepresentableDebtTakesTheWholeBalance checks a debt that
// does not fit sdkmath.Int at all. It exceeds any balance, so the first
// settlement pays everything that remains and freezes the stream, which is how
// per-lease settlement treated an accrual overflow.
func TestMigrate2to3_UnrepresentableDebtTakesTheWholeBalance(t *testing.T) {
	s := newV2State(t)
	tenant := s.TestAccs[0]
	provider, sku, payout := s.provider()
	credit := s.hugeCredit(tenant)

	// 2^240 per second, unsettled since its acknowledgement 24h ago:
	// 86400 × 2^240 exceeds 2^256.
	rate := sdkmath.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 240))
	s.leasePriced(rate, tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_ACTIVE, nil, nil)

	s.migrate()
	state, err := s.App.BillingKeeper.Streams.Get(s.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.False(t, state.IsExhausted())
	require.Equal(t, sdkmath.MaxBitLen, state.Arrears.BigInt().BitLen(), "the largest representable amount stands for the debt")
	s.requireInvariants()

	s.Ctx = s.Ctx.WithBlockTime(s.Ctx.BlockTime().Add(time.Second))
	resp := s.withdraw(provider.Uuid)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, credit)), resp.TotalAmounts)
	require.True(t, s.balance(payout).Equal(credit))

	state, err = s.App.BillingKeeper.Streams.Get(s.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.True(t, state.IsExhausted())
	s.requireInvariants()
}

// TestMigrate2to3_MatchesPerLeaseSettlement compares the migration against a
// model of per-lease settlement on random v2 states.
//
// The model is what consensus version 2 would have paid a provider that
// withdrew at the upgrade block (each lease's rate × its whole unsettled
// seconds, capped per tenant at the credit balance), plus what the stream
// accrues exactly from the upgrade to the withdrawal. The migrated chain must
// pay the provider the same amount on its first withdrawal, charge each tenant
// the same, and freeze exactly the streams whose tenant could not cover it.
// The model is computed from how the leases were built, not by the migration's
// own code.
func TestMigrate2to3_MatchesPerLeaseSettlement(t *testing.T) {
	const day = 24 * time.Hour // leases are acknowledged a day before the upgrade

	type tenantModel struct {
		addr   sdk.AccAddress
		active bool
		owed   *big.Int // unsettled under per-lease settlement at the upgrade
		rate   *big.Int // summed rate of the ACTIVE leases
	}

	for seed := uint64(1); seed <= 40; seed++ {
		t.Run(fmt.Sprintf("seed %d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewPCG(seed, seed))
			s := newV2State(t)
			now := s.Ctx.BlockTime()
			provider, sku, payout := s.provider()

			tenants := []*tenantModel{{addr: s.TestAccs[0]}, {addr: s.TestAccs[2]}, {addr: s.TestAccs[3]}}
			for _, tm := range tenants {
				tm.owed, tm.rate = new(big.Int), new(big.Int)
				s.fund(tm.addr, 0)

				for range rng.IntN(5) {
					price := rng.Int64N(1_000) + 1
					// A duration with a sub-second part: per-lease settlement
					// only counted whole seconds.
					unsettled := time.Duration(rng.Int64N(int64(6 * time.Hour)))
					secs := int64(unsettled / time.Second)

					switch rng.IntN(5) {
					case 0: // ACTIVE, never settled: accrues from its acknowledgement
						s.leasePriced(sdkmath.NewInt(price), tm.addr, provider.Uuid, sku.Uuid, types.LEASE_STATE_ACTIVE, nil, nil)
						secs = int64(day / time.Second)
						tm.active = true
						tm.rate.Add(tm.rate, big.NewInt(price))
					case 1, 2: // ACTIVE, last settled a while ago
						s.leasePriced(sdkmath.NewInt(price), tm.addr, provider.Uuid, sku.Uuid, types.LEASE_STATE_ACTIVE, ago(now, unsettled), nil)
						tm.active = true
						tm.rate.Add(tm.rate, big.NewInt(price))
					case 3: // CLOSED and settled up to its closure, as handlers leave it
						closedAt := ago(now, unsettled)
						s.leasePriced(sdkmath.NewInt(price), tm.addr, provider.Uuid, sku.Uuid, types.LEASE_STATE_CLOSED, closedAt, closedAt)
						secs = 0
					case 4: // CLOSED while still owing up to its closure
						closedAt := ago(now, time.Duration(rng.Int64N(int64(time.Hour))))
						s.leasePriced(sdkmath.NewInt(price), tm.addr, provider.Uuid, sku.Uuid, types.LEASE_STATE_CLOSED, ago(*closedAt, unsettled), closedAt)
					}
					tm.owed.Add(tm.owed, new(big.Int).Mul(big.NewInt(price), big.NewInt(secs)))
				}

				// Credit: exactly the reservation of the ACTIVE leases (often
				// less than what is owed), or plenty.
				account, err := s.App.BillingKeeper.GetCreditAccount(s.Ctx, tm.addr.String())
				require.NoError(t, err)
				credit := account.ReservedAmounts.AmountOf(testDenom).Int64()
				if rng.IntN(2) == 0 {
					credit += 1_000_000_000_000
				}
				s.fund(tm.addr, credit)
			}

			s.migrate()
			s.requireInvariants()
			require.True(t, s.balance(payout).IsZero(), "the migration moves no coins")

			// The provider withdraws some time after the upgrade, not on a
			// whole second.
			elapsed := time.Duration(rng.Int64N(int64(2 * time.Minute)))
			s.Ctx = s.Ctx.WithBlockTime(now.Add(elapsed))

			credits := make(map[string]sdkmath.Int)
			for _, tm := range tenants {
				credits[tm.addr.String()] = s.balance(types.DeriveCreditAddress(tm.addr))
			}
			s.withdraw(provider.Uuid)

			wantPayout := new(big.Int)
			for _, tm := range tenants {
				key := keeper.NewStreamKey(provider.Uuid, tm.addr, testDenom)
				before := credits[tm.addr.String()]
				after := s.balance(types.DeriveCreditAddress(tm.addr))

				if !tm.active {
					has, err := s.App.BillingKeeper.Streams.Has(s.Ctx, key)
					require.NoError(t, err)
					require.False(t, has, "no ACTIVE lease, no stream")
					require.Equal(t, before.String(), after.String())
					continue
				}

				// due = owed + floor(rate × elapsed ns / 1e9), capped at the credit.
				due := new(big.Int).Mul(tm.rate, big.NewInt(int64(elapsed)))
				due.Quo(due, big.NewInt(int64(time.Second)))
				due.Add(due, tm.owed)
				pay := new(big.Int).Set(due)
				if pay.Cmp(before.BigInt()) > 0 {
					pay.Set(before.BigInt())
				}
				wantPayout.Add(wantPayout, pay)

				require.Equal(t, pay.String(), before.Sub(after).String(), "tenant charge (owed %s, rate %s, elapsed %s, credit %s)", tm.owed, tm.rate, elapsed, before)
				state, err := s.App.BillingKeeper.Streams.Get(s.Ctx, key)
				require.NoError(t, err)
				require.Equal(t, due.Cmp(before.BigInt()) >= 0, state.IsExhausted(), "frozen exactly when the credit does not exceed what is owed")
				require.True(t, state.Arrears.IsZero())
			}
			require.Equal(t, wantPayout.String(), s.balance(payout).String())
			s.requireInvariants()
		})
	}
}
