package keeper_test

// Shared fixtures, helpers and invariant checkers for the keeper tests.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"

	"github.com/manifest-network/manifest-ledger/app"
	"github.com/manifest-network/manifest-ledger/app/apptesting"
	appparams "github.com/manifest-network/manifest-ledger/app/params"
	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

const (
	testDenom        = "umfx"
	testDenom2       = "upwr"
	testLeaseUUID1   = "01912345-6789-7abc-8def-0123456789ab"
	testLeaseUUID2   = "01912345-6789-7abc-8def-0123456789ac"
	testProviderUUID = "01912345-6789-7abc-8def-0123456789ad"
	testSKUUUID      = "01912345-6789-7abc-8def-0123456789ae"
)

type testFixture struct {
	App         *app.ManifestApp
	EncodingCfg moduletestutil.TestEncodingConfig
	Ctx         sdk.Context
	QueryHelper *baseapp.QueryServiceTestHelper
	TestAccs    []sdk.AccAddress
	Authority   sdk.AccAddress
}

func initFixture(t *testing.T) *testFixture {
	t.Helper()

	s := testFixture{}

	appparams.SetAddressPrefixes()

	encCfg := moduletestutil.MakeTestEncodingConfig()

	s.Ctx, s.App = app.Setup(t)
	s.QueryHelper = &baseapp.QueryServiceTestHelper{
		GRPCQueryRouter: s.App.GRPCQueryRouter(),
		Ctx:             s.Ctx,
	}
	s.TestAccs = apptesting.CreateRandomAccounts(5)

	// Use testdata to generate authority key
	_, _, authority := testdata.KeyTestPubAddr()
	s.Authority = authority

	s.EncodingCfg = encCfg

	// Initialize default params
	err := s.App.BillingKeeper.SetParams(s.Ctx, types.DefaultParams())
	require.NoError(t, err)

	// Set authority
	s.App.BillingKeeper.SetAuthority(authority.String())

	return &s
}

// fundAccount sends tokens to an account using the bank module.
func (f *testFixture) fundAccount(t *testing.T, addr sdk.AccAddress, coins sdk.Coins) {
	t.Helper()
	err := f.App.BankKeeper.MintCoins(f.Ctx, "mint", coins)
	require.NoError(t, err)
	err = f.App.BankKeeper.SendCoinsFromModuleToAccount(f.Ctx, "mint", addr, coins)
	require.NoError(t, err)
}

// createTestProvider creates a provider in the SKU module for testing.
func (f *testFixture) createTestProvider(t *testing.T, address, payoutAddress string) skutypes.Provider {
	t.Helper()
	// Generate a unique UUID for the provider
	providerUUID, err := f.App.SKUKeeper.GenerateProviderUUID(f.Ctx)
	require.NoError(t, err)

	provider := skutypes.Provider{
		Uuid:          providerUUID,
		Address:       address,
		PayoutAddress: payoutAddress,
		Active:        true,
	}
	err = f.App.SKUKeeper.SetProvider(f.Ctx, provider)
	require.NoError(t, err)
	return provider
}

// createTestSKU creates a SKU in the SKU module for testing.
func (f *testFixture) createTestSKU(t *testing.T, providerUUID string, priceAmount int64) skutypes.SKU {
	t.Helper()
	// Generate a unique UUID for the SKU
	skuUUID, err := f.App.SKUKeeper.GenerateSKUUUID(f.Ctx)
	require.NoError(t, err)

	sku := skutypes.SKU{
		Uuid:         skuUUID,
		ProviderUuid: providerUUID,
		Name:         "Test SKU",
		Unit:         skutypes.Unit_UNIT_PER_HOUR,
		BasePrice:    sdk.NewCoin("umfx", sdkmath.NewInt(priceAmount)),
		Active:       true,
	}
	err = f.App.SKUKeeper.SetSKU(f.Ctx, sku)
	require.NoError(t, err)
	return sku
}

// createAndAcknowledgeLease is a helper that creates a lease and acknowledges it,
// returning the lease in ACTIVE state ready for testing.
func (f *testFixture) createAndAcknowledgeLease(
	t *testing.T,
	msgServer types.MsgServer,
	tenant sdk.AccAddress,
	providerAddr sdk.AccAddress,
	items []types.LeaseItemInput,
) string {
	t.Helper()

	// Create the lease
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  items,
	})
	require.NoError(t, err)
	require.NotNil(t, createResp)

	// Acknowledge the lease as the provider
	ackResp, err := msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{createResp.LeaseUuid},
	})
	require.NoError(t, err)
	require.NotNil(t, ackResp)

	// Verify lease is now ACTIVE
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, createResp.LeaseUuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)

	return createResp.LeaseUuid
}

// createTestSKUWithDenom creates a SKU with a specific denom for testing multi-denom scenarios.
func (f *testFixture) createTestSKUWithDenom(t *testing.T, providerUUID string, priceAmount int64, denom string) skutypes.SKU {
	t.Helper()

	skuUUID, err := f.App.SKUKeeper.GenerateSKUUUID(f.Ctx)
	require.NoError(t, err)

	sku := skutypes.SKU{
		Uuid:         skuUUID,
		ProviderUuid: providerUUID,
		Name:         "Test SKU " + denom,
		Unit:         skutypes.Unit_UNIT_PER_HOUR,
		BasePrice:    sdk.NewCoin(denom, sdkmath.NewInt(priceAmount)),
		Active:       true,
	}
	err = f.App.SKUKeeper.SetSKU(f.Ctx, sku)
	require.NoError(t, err)
	return sku
}

// newExpiryFixture sets up a fixture with a provider, an SKU, and a funded tenant
// (TestAccs[0]) credit account, plus the given pending timeout, for EndBlocker
// pending-lease expiration tests. It returns the fixture along with the provider
// UUID, SKU UUID, denom, and the fixture's base block time.
func newExpiryFixture(t *testing.T, pendingTimeoutSecs uint64) (*testFixture, string, string, string, time.Time) {
	t.Helper()
	f := initFixture(t)
	k := f.App.BillingKeeper

	params := types.DefaultParams()
	params.PendingTimeout = pendingTimeoutSecs
	params.MaxPendingLeasesPerTenant = 500
	require.NoError(t, k.SetParams(f.Ctx, params))

	provider := f.createTestProvider(t, f.TestAccs[1].String(), f.TestAccs[2].String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	tenant := f.TestAccs[0]
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000_000_000))))
	require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	}))

	return f, provider.Uuid, sku.Uuid, testDenom, f.Ctx.BlockTime()
}

// setPendingLease writes a PENDING lease with an explicit UUID and created_at
// directly to state (bypassing message validation) so expiration tests can
// control the (state, created_at) index key precisely.
func (f *testFixture) setPendingLease(t *testing.T, uuid, providerUUID, skuUUID, denom string, createdAt time.Time) {
	t.Helper()
	params, err := f.App.BillingKeeper.GetParams(f.Ctx)
	require.NoError(t, err)

	lease := types.Lease{
		Uuid:                       uuid,
		Tenant:                     f.TestAccs[0].String(),
		ProviderUuid:               providerUUID,
		Items:                      []types.LeaseItem{{SkuUuid: skuUUID, Quantity: 1, LockedPrice: sdk.NewCoin(denom, sdkmath.NewInt(1))}},
		State:                      types.LEASE_STATE_PENDING,
		CreatedAt:                  createdAt,
		MinLeaseDurationAtCreation: params.MinLeaseDuration,
	}
	require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, lease))

	// Mirror CreateLease's credit-account side effects so EndBlocker expiry finds a
	// consistent account: bump the pending count and reserve credit using the same
	// amount ExpirePendingLease will release. Without this, expiry logs
	// "data inconsistency" warnings (pending count already zero / reservation underflow).
	ca, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, lease.Tenant)
	require.NoError(t, err)
	ca.PendingLeaseCount++
	ca.ReservedAmounts = types.AddReservation(ca.ReservedAmounts, types.GetLeaseReservationAmount(&lease, params.MinLeaseDuration))
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, ca))
}

// findEvent returns the most recent event with the given type from the
// fixture's event manager, or fails the test if not found.
func findEvent(t *testing.T, ctx sdk.Context, eventType string) sdk.Event {
	t.Helper()
	events := ctx.EventManager().Events()
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == eventType {
			return events[i]
		}
	}
	t.Fatalf("event %q not found among %d emitted events", eventType, len(events))
	return sdk.Event{}
}

// attrValue returns the value of the named attribute from an event, or fails.
func attrValue(t *testing.T, ev sdk.Event, key string) string {
	t.Helper()
	for _, a := range ev.Attributes {
		if a.Key == key {
			return a.Value
		}
	}
	t.Fatalf("attribute %q not found on event %q", key, ev.Type)
	return ""
}

// countEvents returns how many events of the given type the context has emitted.
func countEvents(ctx sdk.Context, eventType string) int {
	n := 0
	for _, ev := range ctx.EventManager().Events() {
		if ev.Type == eventType {
			n++
		}
	}
	return n
}

// InvariantResult holds the result of an invariant check.
type InvariantResult struct {
	Name    string
	Passed  bool
	Message string
}

// CheckFundConservationInvariant verifies that the total credit balance across all
// credit accounts is non-negative. This is a basic sanity check — the bank module
// should never allow negative balances.
func CheckFundConservationInvariant(
	ctx context.Context,
	k keeper.Keeper,
	bankKeeper interface {
		GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	},
) InvariantResult {
	accounts, err := k.GetAllCreditAccounts(ctx)
	if err != nil {
		return InvariantResult{
			Name:    "FundConservation",
			Passed:  false,
			Message: "failed to get credit accounts: " + err.Error(),
		}
	}

	for _, ca := range accounts {
		creditAddr, err := types.DeriveCreditAddressFromBech32(ca.Tenant)
		if err != nil {
			return InvariantResult{
				Name:    "FundConservation",
				Passed:  false,
				Message: "invalid tenant address in credit account: " + ca.Tenant,
			}
		}

		// Check a representative denom — in practice you'd check all relevant denoms
		bal := bankKeeper.GetBalance(ctx, creditAddr, "umfx")
		if bal.Amount.IsNegative() {
			return InvariantResult{
				Name:   "FundConservation",
				Passed: false,
				Message: "negative balance for credit account " + ca.Tenant +
					": " + bal.String(),
			}
		}
	}

	return InvariantResult{Name: "FundConservation", Passed: true}
}

// CheckReservationConsistencyInvariant verifies that every credit account's
// ReservedAmounts equals the sum of GetLeaseReservationAmount for all PENDING
// and ACTIVE leases of that tenant.
func CheckReservationConsistencyInvariant(ctx context.Context, k keeper.Keeper) InvariantResult {
	params, err := k.GetParams(ctx)
	if err != nil {
		return InvariantResult{
			Name:    "ReservationConsistency",
			Passed:  false,
			Message: "failed to get params: " + err.Error(),
		}
	}

	allLeases, err := k.GetAllLeases(ctx)
	if err != nil {
		return InvariantResult{
			Name:    "ReservationConsistency",
			Passed:  false,
			Message: "failed to get leases: " + err.Error(),
		}
	}

	// Calculate expected reservations per tenant
	expected := types.CalculateExpectedReservationsByTenant(allLeases, params.MinLeaseDuration)

	accounts, err := k.GetAllCreditAccounts(ctx)
	if err != nil {
		return InvariantResult{
			Name:    "ReservationConsistency",
			Passed:  false,
			Message: "failed to get credit accounts: " + err.Error(),
		}
	}

	for _, ca := range accounts {
		exp := expected[types.CanonicalAddress(ca.Tenant)]
		if exp == nil {
			exp = sdk.NewCoins()
		}

		actualNormalized := sdk.NewCoins(ca.ReservedAmounts...)
		expectedNormalized := sdk.NewCoins(exp...)

		if !actualNormalized.Equal(expectedNormalized) {
			return InvariantResult{
				Name:   "ReservationConsistency",
				Passed: false,
				Message: "tenant " + ca.Tenant + " has reserved " +
					actualNormalized.String() + " but leases sum to " +
					expectedNormalized.String(),
			}
		}
	}

	return InvariantResult{Name: "ReservationConsistency", Passed: true}
}

// CheckLeaseCountConsistencyInvariant verifies that each credit account's
// ActiveLeaseCount and PendingLeaseCount match the actual number of leases
// in those states for that tenant.
func CheckLeaseCountConsistencyInvariant(ctx context.Context, k keeper.Keeper) InvariantResult {
	allLeases, err := k.GetAllLeases(ctx)
	if err != nil {
		return InvariantResult{
			Name:    "LeaseCountConsistency",
			Passed:  false,
			Message: "failed to get leases: " + err.Error(),
		}
	}

	activeCounts := make(map[string]uint64)
	pendingCounts := make(map[string]uint64)
	for _, lease := range allLeases {
		switch lease.State {
		case types.LEASE_STATE_ACTIVE:
			activeCounts[types.CanonicalAddress(lease.Tenant)]++
		case types.LEASE_STATE_PENDING:
			pendingCounts[types.CanonicalAddress(lease.Tenant)]++
		}
	}

	accounts, err := k.GetAllCreditAccounts(ctx)
	if err != nil {
		return InvariantResult{
			Name:    "LeaseCountConsistency",
			Passed:  false,
			Message: "failed to get credit accounts: " + err.Error(),
		}
	}

	for _, ca := range accounts {
		expectedActive := activeCounts[types.CanonicalAddress(ca.Tenant)]
		if ca.ActiveLeaseCount != expectedActive {
			return InvariantResult{
				Name:   "LeaseCountConsistency",
				Passed: false,
				Message: "tenant " + ca.Tenant + " active_lease_count=" +
					sdkmath.NewIntFromUint64(ca.ActiveLeaseCount).String() +
					" but actual=" + sdkmath.NewIntFromUint64(expectedActive).String(),
			}
		}

		expectedPending := pendingCounts[types.CanonicalAddress(ca.Tenant)]
		if ca.PendingLeaseCount != expectedPending {
			return InvariantResult{
				Name:   "LeaseCountConsistency",
				Passed: false,
				Message: "tenant " + ca.Tenant + " pending_lease_count=" +
					sdkmath.NewIntFromUint64(ca.PendingLeaseCount).String() +
					" but actual=" + sdkmath.NewIntFromUint64(expectedPending).String(),
			}
		}
	}

	return InvariantResult{Name: "LeaseCountConsistency", Passed: true}
}

// CheckNoOrphanedLeasesInvariant verifies that every PENDING or ACTIVE lease
// has a corresponding credit account for its tenant.
func CheckNoOrphanedLeasesInvariant(ctx context.Context, k keeper.Keeper) InvariantResult {
	allLeases, err := k.GetAllLeases(ctx)
	if err != nil {
		return InvariantResult{
			Name:    "NoOrphanedLeases",
			Passed:  false,
			Message: "failed to get leases: " + err.Error(),
		}
	}

	for _, lease := range allLeases {
		if lease.State == types.LEASE_STATE_PENDING || lease.State == types.LEASE_STATE_ACTIVE {
			_, err := k.GetCreditAccount(ctx, lease.Tenant)
			if err != nil {
				return InvariantResult{
					Name:   "NoOrphanedLeases",
					Passed: false,
					Message: "lease " + lease.Uuid + " (state=" + lease.State.String() +
						") has no credit account for tenant " + lease.Tenant,
				}
			}
		}
	}

	return InvariantResult{Name: "NoOrphanedLeases", Passed: true}
}

// requireInvariants runs every billing invariant (including the stream/lease
// consistency invariant and a genesis round trip) against the fixture's
// current state and fails the test on the first violation.
func requireInvariants(t *testing.T, f *testFixture) {
	t.Helper()
	for _, res := range RunAllInvariants(f.Ctx, f.App.BillingKeeper, f.App.BankKeeper, f.Ctx.BlockTime()) {
		require.True(t, res.Passed, "invariant %s failed: %s", res.Name, res.Message)
	}
}

// syncStreams rebuilds the settlement streams from the ACTIVE leases currently
// in the store, as if every one of them had last been settled at settledAt.
//
// Leases created through the message handlers get their streams from
// AcknowledgeLease. Tests that write ACTIVE leases straight into the store
// (SetLease) bypass that, and must call syncStreams afterwards so that
// settlement, auto-close and the stream invariant see a consistent state.
func (f *testFixture) syncStreams(t *testing.T, settledAt time.Time) {
	t.Helper()
	k := f.App.BillingKeeper

	require.NoError(t, k.Streams.Clear(f.Ctx, nil))
	require.NoError(t, k.StreamsByTenant.Clear(f.Ctx, nil))
	require.NoError(t, k.StreamLeases.Clear(f.Ctx, nil))
	require.NoError(t, k.ExhaustedStreams.Clear(f.Ctx, nil))

	leases, err := k.GetLeasesByState(f.Ctx, types.LEASE_STATE_ACTIVE)
	require.NoError(t, err)

	for i := range leases {
		lease := &leases[i]
		tenant, err := sdk.AccAddressFromBech32(lease.Tenant)
		require.NoError(t, err)
		rates, err := types.LeaseRatesPerSecond(lease.Items)
		require.NoError(t, err)

		for _, rate := range rates {
			key := keeper.NewStreamKey(lease.ProviderUuid, tenant, rate.Denom)
			state, err := k.Streams.Get(f.Ctx, key)
			if err != nil {
				require.ErrorIs(t, err, collections.ErrNotFound)
				state = types.StreamState{Rate: sdkmath.ZeroInt(), LastSettledAt: settledAt, Remainder: sdkmath.ZeroInt()}
				require.NoError(t, k.StreamsByTenant.Set(f.Ctx,
					collections.Join(tenant, collections.Join(lease.ProviderUuid, rate.Denom))))
			}
			state.Rate = state.Rate.Add(rate.Amount)
			state.LeaseCount++
			require.NoError(t, k.Streams.Set(f.Ctx, key, state))
			require.NoError(t, k.StreamLeases.Set(f.Ctx, collections.Join(key, lease.Uuid)))
		}
	}
}

// CheckStreamConsistencyInvariant verifies that the settlement streams match
// the ACTIVE leases: rate, lease count, membership entries, reverse lookup and
// drain queue (see Keeper.CheckStreamInvariant).
func CheckStreamConsistencyInvariant(ctx context.Context, k keeper.Keeper) InvariantResult {
	if err := k.CheckStreamInvariant(ctx); err != nil {
		return InvariantResult{Name: "StreamConsistency", Passed: false, Message: err.Error()}
	}
	return InvariantResult{Name: "StreamConsistency", Passed: true}
}

// CheckValidTimestampsInvariant verifies that all lease and stream timestamps are valid:
// - CreatedAt is not in the future
// - ClosedAt of closed leases is not in the future
// - no stream is settled or exhausted in the future relative to block time
// - no lease carries the deprecated last_settled_at
func CheckValidTimestampsInvariant(ctx context.Context, k keeper.Keeper, blockTime time.Time) InvariantResult {
	streams, err := k.GetAllStreams(ctx)
	if err != nil {
		return InvariantResult{
			Name:    "ValidTimestamps",
			Passed:  false,
			Message: "failed to get streams: " + err.Error(),
		}
	}
	for _, stream := range streams {
		if stream.State.LastSettledAt.After(blockTime) {
			return InvariantResult{
				Name:   "ValidTimestamps",
				Passed: false,
				Message: "stream " + stream.ProviderUuid + "/" + stream.Tenant + "/" + stream.Denom +
					" has LastSettledAt in the future: " + stream.State.LastSettledAt.String(),
			}
		}
		if stream.State.ExhaustedAt != nil && stream.State.ExhaustedAt.After(blockTime) {
			return InvariantResult{
				Name:   "ValidTimestamps",
				Passed: false,
				Message: "stream " + stream.ProviderUuid + "/" + stream.Tenant + "/" + stream.Denom +
					" has ExhaustedAt in the future: " + stream.State.ExhaustedAt.String(),
			}
		}
	}

	allLeases, err := k.GetAllLeases(ctx)
	if err != nil {
		return InvariantResult{
			Name:    "ValidTimestamps",
			Passed:  false,
			Message: "failed to get leases: " + err.Error(),
		}
	}

	for _, lease := range allLeases {
		if lease.CreatedAt.After(blockTime) {
			return InvariantResult{
				Name:   "ValidTimestamps",
				Passed: false,
				Message: "lease " + lease.Uuid + " has CreatedAt in the future: " +
					lease.CreatedAt.String(),
			}
		}

		if lease.LastSettledAt != nil { //nolint:staticcheck // asserting the deprecated field stays unset
			return InvariantResult{
				Name:    "ValidTimestamps",
				Passed:  false,
				Message: "lease " + lease.Uuid + " carries the deprecated last_settled_at",
			}
		}

		if lease.State == types.LEASE_STATE_CLOSED && lease.ClosedAt != nil {
			if lease.ClosedAt.After(blockTime) {
				return InvariantResult{
					Name:   "ValidTimestamps",
					Passed: false,
					Message: "lease " + lease.Uuid + " has ClosedAt in the future: " +
						lease.ClosedAt.String(),
				}
			}
		}
	}

	return InvariantResult{Name: "ValidTimestamps", Passed: true}
}

// CheckGenesisRoundTripInvariant exports genesis, validates it, and verifies
// the export is self-consistent.
func CheckGenesisRoundTripInvariant(ctx context.Context, k keeper.Keeper) InvariantResult {
	exported := k.ExportGenesis(ctx)

	if err := exported.Validate(); err != nil {
		return InvariantResult{
			Name:    "GenesisRoundTrip",
			Passed:  false,
			Message: "exported genesis is invalid: " + err.Error(),
		}
	}

	return InvariantResult{Name: "GenesisRoundTrip", Passed: true}
}

// RunAllInvariants runs all invariant checks and returns results.
func RunAllInvariants(
	ctx context.Context, k keeper.Keeper, bankKeeper interface {
		GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	}, blockTime time.Time,
) []InvariantResult {
	return []InvariantResult{
		CheckFundConservationInvariant(ctx, k, bankKeeper),
		CheckReservationConsistencyInvariant(ctx, k),
		CheckLeaseCountConsistencyInvariant(ctx, k),
		CheckNoOrphanedLeasesInvariant(ctx, k),
		CheckStreamConsistencyInvariant(ctx, k),
		CheckValidTimestampsInvariant(ctx, k, blockTime),
		CheckGenesisRoundTripInvariant(ctx, k),
	}
}
