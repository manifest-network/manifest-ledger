/*
Package keeper_test contains unit tests for the billing store migrations.

Test Coverage (Migrate2to3, per-lease settlement to settlement streams):
  - Unsettled accrual of ACTIVE leases is paid once per stream at the upgrade
  - Streams are built from the ACTIVE leases and settled at the upgrade time
  - A tenant whose credit cannot cover what is owed gets a frozen stream that
    the EndBlocker then drains
  - CLOSED leases that still owed something are paid; fully settled ones are not
  - Missing and future last_settled_at values
  - A failing payout does not abort the upgrade and the whole debt is carried

- Tenants are grouped by address, whatever the spelling stored in the lease
  - The deprecated field is cleared, stored bytes are canonical, indexes intact
  - Payment order is deterministic when credit is short
*/
package keeper_test

import (
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
		Items:                      []types.LeaseItem{{SkuUuid: skuUUID, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1))}},
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

func TestMigrate2to3_PaysAccrualAndBuildsStreams(t *testing.T) {
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

	require.Equal(t, sdkmath.NewInt(160), s.balance(payout), "each lease is paid up to the upgrade")
	require.Equal(t, 1, countEvents(s.Ctx, "transfer"), "one transfer per stream, not per lease")

	state, err := s.App.BillingKeeper.Streams.Get(s.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(3), state.Rate)
	require.Equal(t, uint64(3), state.LeaseCount)
	require.Equal(t, now, state.LastSettledAt, "the stream starts settled at the upgrade time")
	require.False(t, state.IsExhausted())

	for _, uuid := range uuids {
		lease, err := s.App.BillingKeeper.GetLease(s.Ctx, uuid)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)
		require.Nil(t, lease.LastSettledAt) //nolint:staticcheck // asserting the deprecated field is cleared
	}
	s.requireInvariants()

	// Settlement continues from the upgrade time: 10s later, 3 leases × 10s.
	s.Ctx = s.Ctx.WithBlockTime(now.Add(10 * time.Second))
	resp, err := keeper.NewMsgServerImpl(s.App.BillingKeeper).Withdraw(s.Ctx, &types.MsgWithdraw{
		Sender:       s.TestAccs[1].String(),
		ProviderUuid: provider.Uuid,
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(30))), resp.TotalAmounts)
}

func TestMigrate2to3_ExhaustedTenantGetsFrozenStream(t *testing.T) {
	s := newV2State(t)
	now := s.Ctx.BlockTime()
	tenant := s.TestAccs[0]
	provider, sku, payout := s.provider()
	s.fund(tenant, 7_200)

	// Two leases owe 2 × 10000 against a balance of 7200.
	first := s.lease(tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_ACTIVE, ago(now, 10_000*time.Second), nil)
	second := s.lease(tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_ACTIVE, ago(now, 10_000*time.Second), nil)

	s.migrate()

	require.Equal(t, sdkmath.NewInt(7_200), s.balance(payout), "the provider receives everything that remains")
	require.True(t, s.balance(types.DeriveCreditAddress(tenant)).IsZero())

	state, err := s.App.BillingKeeper.Streams.Get(s.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.True(t, state.IsExhausted(), "the migration does not close leases itself; it queues the stream")
	require.Equal(t, uint64(2), state.LeaseCount)
	s.requireInvariants()

	// The normal EndBlocker drain closes the leases after the upgrade.
	require.NoError(t, s.App.BillingKeeper.EndBlocker(s.Ctx))
	for _, uuid := range []string{first, second} {
		lease, err := s.App.BillingKeeper.GetLease(s.Ctx, uuid)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
		require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason)
	}
	s.requireInvariants()
}

func TestMigrate2to3_ClosedLeases(t *testing.T) {
	s := newV2State(t)
	now := s.Ctx.BlockTime()
	tenant := s.TestAccs[0]
	provider, sku, payout := s.provider()
	s.fund(tenant, 1_000_000)

	closedAt := ago(now, time.Hour)
	// Settled up to its closure, as every handler-closed lease is.
	settled := s.lease(tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_CLOSED, closedAt, closedAt)
	// Still owing 40s up to its closure (only reachable through genesis).
	residual := s.lease(tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_CLOSED, ago(*closedAt, 40*time.Second), closedAt)

	s.migrate()

	require.Equal(t, sdkmath.NewInt(40), s.balance(payout), "only the residual of the closed lease is paid")

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

func TestMigrate2to3_MissingAndFutureLastSettledAt(t *testing.T) {
	s := newV2State(t)
	now := s.Ctx.BlockTime()
	tenant := s.TestAccs[0]
	provider, sku, payout := s.provider()
	s.fund(tenant, 1_000_000_000)

	// No last_settled_at: billing started at acknowledgement, 24h ago.
	s.lease(tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_ACTIVE, nil, nil)
	// last_settled_at in the future: nothing is owed yet.
	future := now.Add(time.Hour)
	s.lease(tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_ACTIVE, &future, nil)

	s.migrate()

	require.Equal(t, sdkmath.NewInt(24*3600), s.balance(payout))
	state, err := s.App.BillingKeeper.Streams.Get(s.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(2), state.Rate)
	require.Equal(t, now, state.LastSettledAt, "a future last_settled_at does not leak into the stream")
	s.requireInvariants()
}

func TestMigrate2to3_FailingPayoutCarriesTheDebt(t *testing.T) {
	s := newV2State(t)
	now := s.Ctx.BlockTime()
	tenant := s.TestAccs[0]
	_, sku, _ := s.provider()
	s.fund(tenant, 1_000_000)

	// The lease names a provider that does not exist, so the payout fails.
	missingProvider := "01912345-0000-7000-8000-00000000dead"
	uuid := s.lease(tenant, missingProvider, sku.Uuid, types.LEASE_STATE_ACTIVE, ago(now, 500*time.Second), nil)

	// The upgrade must not fail over one bad stream.
	s.migrate()

	require.Equal(t, sdkmath.NewInt(1_000_000), s.balance(types.DeriveCreditAddress(tenant)), "the failed payment left nothing behind")

	state, err := s.App.BillingKeeper.Streams.Get(s.Ctx, keeper.NewStreamKey(missingProvider, tenant, testDenom))
	require.NoError(t, err)
	require.False(t, state.IsExhausted())
	require.Equal(t, now, state.LastSettledAt)
	require.Equal(t, sdkmath.NewInt(500).MulRaw(1_000_000_000), state.Remainder,
		"the 500 units that could not be paid are carried in the remainder")

	lease, err := s.App.BillingKeeper.GetLease(s.Ctx, uuid)
	require.NoError(t, err)
	require.Nil(t, lease.LastSettledAt) //nolint:staticcheck // asserting the deprecated field is cleared
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

func TestMigrate2to3_PaymentOrderIsDeterministicWhenCreditIsShort(t *testing.T) {
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

	// Streams are processed in (tenant, provider_uuid, denom) order, so the
	// provider with the smaller UUID is paid in full and the other gets the rest.
	firstPayout, secondPayout := payoutA, payoutB
	firstProvider, secondProvider := providerA.Uuid, providerB.Uuid
	if providerB.Uuid < providerA.Uuid {
		firstPayout, secondPayout = payoutB, payoutA
		firstProvider, secondProvider = providerB.Uuid, providerA.Uuid
	}
	require.Equal(t, sdkmath.NewInt(5_000), s.balance(firstPayout))
	require.Equal(t, sdkmath.NewInt(2_200), s.balance(secondPayout))

	first, err := s.App.BillingKeeper.Streams.Get(s.Ctx, keeper.NewStreamKey(firstProvider, tenant, testDenom))
	require.NoError(t, err)
	require.False(t, first.IsExhausted(), "paid in full with credit to spare")
	second, err := s.App.BillingKeeper.Streams.Get(s.Ctx, keeper.NewStreamKey(secondProvider, tenant, testDenom))
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
	provider, sku, payout := s.provider()
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

	require.Equal(t, sdkmath.NewInt(150), s.balance(payout))
	streams, err := s.App.BillingKeeper.GetAllStreams(s.Ctx)
	require.NoError(t, err)
	require.Len(t, streams, 1, "both spellings are one tenant, so one stream")
	require.Equal(t, tenant.String(), streams[0].Tenant)
	require.Equal(t, sdkmath.NewInt(2), streams[0].State.Rate)
	require.Equal(t, uint64(2), streams[0].State.LeaseCount)

	for _, uuid := range []string{lower, upper} {
		member, err := s.App.BillingKeeper.StreamLeases.Has(s.Ctx,
			collections.Join(keeper.NewStreamKey(provider.Uuid, tenant, testDenom), uuid))
		require.NoError(t, err)
		require.True(t, member)
	}
	s.requireInvariants()
}

// TestMigrate2to3_FailedPayoutKeepsTheWholeDebt checks that when the migration
// cannot pay a provider, the stream carries exactly what was owed, including
// the part smaller than one second of the stream's rate.
func TestMigrate2to3_FailedPayoutKeepsTheWholeDebt(t *testing.T) {
	s := newV2State(t)
	now := s.Ctx.BlockTime()
	tenant := s.TestAccs[0]
	provider, sku, payout := s.provider()
	s.fund(tenant, 1_000_000)

	// Two leases at 1/s, one settled at the upgrade and one a second earlier:
	// 1 unit is owed in total, against a stream rate of 2/s. Expressing that
	// debt as whole seconds of the stream's rate would round it down to zero.
	s.lease(tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_ACTIVE, ago(now, 0), nil)
	s.lease(tenant, provider.Uuid, sku.Uuid, types.LEASE_STATE_ACTIVE, ago(now, time.Second), nil)

	// Make the payout fail during the migration.
	broken := provider
	broken.PayoutAddress = "not-an-address"
	require.NoError(t, s.App.SKUKeeper.SetProvider(s.Ctx, broken))

	s.migrate()
	require.True(t, s.balance(payout).IsZero(), "nothing could be paid during the migration")

	state, err := s.App.BillingKeeper.Streams.Get(s.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.False(t, state.IsExhausted())
	s.requireInvariants()

	// Once the payout works again, one second later the provider receives the
	// carried unit plus one second of both leases: 1 + 2.
	require.NoError(t, s.App.SKUKeeper.SetProvider(s.Ctx, provider))
	s.Ctx = s.Ctx.WithBlockTime(now.Add(time.Second))
	resp, err := keeper.NewMsgServerImpl(s.App.BillingKeeper).Withdraw(s.Ctx, &types.MsgWithdraw{
		Sender:       s.TestAccs[1].String(),
		ProviderUuid: provider.Uuid,
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(3))), resp.TotalAmounts)
	require.Equal(t, sdkmath.NewInt(3), s.balance(payout))
	s.requireInvariants()
}
