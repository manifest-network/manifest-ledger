package types_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestLeaseRatesPerSecond(t *testing.T) {
	// 2^255, so that doubling it no longer fits the 256-bit Int.
	huge := math.NewIntFromUint64(1 << 63).Mul(math.NewIntFromUint64(1 << 63)).
		Mul(math.NewIntFromUint64(1 << 63)).Mul(math.NewIntFromUint64(1 << 63)).
		Mul(math.NewIntFromUint64(1 << 3))

	tests := []struct {
		name      string
		items     []types.LeaseItem
		want      sdk.Coins
		expectErr string
	}{
		{
			name:  "no items",
			items: nil,
			want:  sdk.NewCoins(),
		},
		{
			name:  "price times quantity",
			items: []types.LeaseItem{{Quantity: 3, LockedPrice: sdk.NewCoin("umfx", math.NewInt(7))}},
			want:  sdk.NewCoins(sdk.NewCoin("umfx", math.NewInt(21))),
		},
		{
			name: "items in the same denom are summed",
			items: []types.LeaseItem{
				{Quantity: 1, LockedPrice: sdk.NewCoin("umfx", math.NewInt(100))},
				{Quantity: 2, LockedPrice: sdk.NewCoin("umfx", math.NewInt(50))},
			},
			want: sdk.NewCoins(sdk.NewCoin("umfx", math.NewInt(200))),
		},
		{
			name: "one entry per denom, sorted whatever the item order",
			items: []types.LeaseItem{
				{Quantity: 1, LockedPrice: sdk.NewCoin("upwr", math.NewInt(5))},
				{Quantity: 1, LockedPrice: sdk.NewCoin("umfx", math.NewInt(3))},
				{Quantity: 4, LockedPrice: sdk.NewCoin("upwr", math.NewInt(1))},
			},
			want: sdk.NewCoins(sdk.NewCoin("umfx", math.NewInt(3)), sdk.NewCoin("upwr", math.NewInt(9))),
		},
		{
			name:      "price times quantity overflow is an error, not a panic",
			items:     []types.LeaseItem{{SkuUuid: "sku-1", Quantity: 2, LockedPrice: sdk.Coin{Denom: "umfx", Amount: huge}}},
			expectErr: "rate overflow for sku sku-1",
		},
		{
			name: "sum overflow is an error, not a panic",
			items: []types.LeaseItem{
				{Quantity: 1, LockedPrice: sdk.Coin{Denom: "umfx", Amount: huge}},
				{Quantity: 1, LockedPrice: sdk.Coin{Denom: "umfx", Amount: huge}},
			},
			expectErr: "rate overflow for denom umfx",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := types.LeaseRatesPerSecond(tc.items)
			if tc.expectErr != "" {
				require.ErrorContains(t, err, tc.expectErr)
				return
			}
			require.NoError(t, err)
			require.True(t, tc.want.Equal(got), "want %s, got %s", tc.want, got)
			require.True(t, got.IsValid() || got.Empty(), "result must be a valid, sorted Coins")
		})
	}
}

func TestExpectedStreamAggregates(t *testing.T) {
	_, _, tenantAddr := testdata.KeyTestPubAddr()
	_, _, tenant2Addr := testdata.KeyTestPubAddr()
	tenant, tenant2 := tenantAddr.String(), tenant2Addr.String()
	const providerA, providerB = "provider-a", "provider-b"

	lease := func(uuid, owner, provider string, state types.LeaseState, items ...types.LeaseItem) types.Lease {
		return types.Lease{Uuid: uuid, Tenant: owner, ProviderUuid: provider, State: state, Items: items}
	}
	item := func(denom string, price int64, quantity uint64) types.LeaseItem {
		return types.LeaseItem{Quantity: quantity, LockedPrice: sdk.NewCoin(denom, math.NewInt(price))}
	}

	leases := []types.Lease{
		// Two ACTIVE leases of one tenant with one provider share a stream.
		lease("l1", tenant, providerA, types.LEASE_STATE_ACTIVE, item("umfx", 10, 1)),
		lease("l2", tenant, providerA, types.LEASE_STATE_ACTIVE, item("umfx", 5, 2)),
		// A multi-denom lease belongs to one stream per denom.
		lease("l3", tenant, providerA, types.LEASE_STATE_ACTIVE, item("umfx", 1, 1), item("upwr", 7, 1)),
		// Another provider and another tenant each get their own stream.
		lease("l4", tenant, providerB, types.LEASE_STATE_ACTIVE, item("umfx", 3, 1)),
		lease("l5", tenant2, providerA, types.LEASE_STATE_ACTIVE, item("umfx", 4, 1)),
		// Only ACTIVE leases contribute.
		lease("l6", tenant, providerA, types.LEASE_STATE_PENDING, item("umfx", 1000, 1)),
		lease("l7", tenant, providerA, types.LEASE_STATE_CLOSED, item("umfx", 1000, 1)),
		lease("l8", tenant, providerA, types.LEASE_STATE_REJECTED, item("umfx", 1000, 1)),
		lease("l9", tenant, providerA, types.LEASE_STATE_EXPIRED, item("umfx", 1000, 1)),
	}

	got, err := types.ExpectedStreamAggregates(leases)
	require.NoError(t, err)

	want := map[types.StreamID]types.StreamAggregate{
		{ProviderUUID: providerA, Tenant: tenant, Denom: "umfx"}:  {Rate: math.NewInt(21), LeaseCount: 3},
		{ProviderUUID: providerA, Tenant: tenant, Denom: "upwr"}:  {Rate: math.NewInt(7), LeaseCount: 1},
		{ProviderUUID: providerB, Tenant: tenant, Denom: "umfx"}:  {Rate: math.NewInt(3), LeaseCount: 1},
		{ProviderUUID: providerA, Tenant: tenant2, Denom: "umfx"}: {Rate: math.NewInt(4), LeaseCount: 1},
	}
	require.Len(t, got, len(want))
	for id, w := range want {
		g, ok := got[id]
		require.True(t, ok, "missing stream %v", id)
		require.True(t, w.Rate.Equal(g.Rate), "stream %v: want rate %s, got %s", id, w.Rate, g.Rate)
		require.Equal(t, w.LeaseCount, g.LeaseCount, "stream %v lease count", id)
	}

	t.Run("no active leases means no streams", func(t *testing.T) {
		got, err := types.ExpectedStreamAggregates(leases[5:])
		require.NoError(t, err)
		require.Empty(t, got)
	})
}

func TestStreamState_IsExhausted(t *testing.T) {
	require.False(t, types.StreamState{}.IsExhausted())

	now := time.Now()
	require.True(t, types.StreamState{ExhaustedAt: &now}.IsExhausted())
}

// TestStreamIdentityIgnoresAddressSpelling checks that a stream is identified
// by the tenant's address, not by how the address is spelled. Bech32 is also
// valid in upper case and leases store the tenant as it was sent.
func TestStreamIdentityIgnoresAddressSpelling(t *testing.T) {
	_, _, addr := testdata.KeyTestPubAddr()
	lower := addr.String()
	upper := strings.ToUpper(lower)
	require.NotEqual(t, lower, upper)

	t.Run("CanonicalAddress", func(t *testing.T) {
		require.Equal(t, lower, types.CanonicalAddress(lower))
		require.Equal(t, lower, types.CanonicalAddress(upper))
		require.Equal(t, "not-an-address", types.CanonicalAddress("not-an-address"), "an invalid address is returned unchanged")
		require.Empty(t, types.CanonicalAddress(""))
	})

	t.Run("stream identifiers", func(t *testing.T) {
		want := types.StreamID{ProviderUUID: "p", Tenant: lower, Denom: "umfx"}
		require.Equal(t, want, types.NewStreamID("p", addr, "umfx"))
		require.Equal(t, want, types.Stream{ProviderUuid: "p", Tenant: lower, Denom: "umfx"}.ID())
		require.Equal(t, want, types.Stream{ProviderUuid: "p", Tenant: upper, Denom: "umfx"}.ID())
	})

	t.Run("leases spelled differently share one stream", func(t *testing.T) {
		item := types.LeaseItem{Quantity: 1, LockedPrice: sdk.NewCoin("umfx", math.NewInt(10))}
		leases := []types.Lease{
			{Uuid: "l1", Tenant: lower, ProviderUuid: "p", State: types.LEASE_STATE_ACTIVE, Items: []types.LeaseItem{item}},
			{Uuid: "l2", Tenant: upper, ProviderUuid: "p", State: types.LEASE_STATE_ACTIVE, Items: []types.LeaseItem{item}},
		}
		got, err := types.ExpectedStreamAggregates(leases)
		require.NoError(t, err)
		require.Len(t, got, 1)
		agg := got[types.NewStreamID("p", addr, "umfx")]
		require.True(t, agg.Rate.Equal(math.NewInt(20)))
		require.Equal(t, uint64(2), agg.LeaseCount)
	})

	t.Run("an active lease with an invalid tenant is an error", func(t *testing.T) {
		_, err := types.ExpectedStreamAggregates([]types.Lease{
			{Uuid: "l1", Tenant: "not-an-address", ProviderUuid: "p", State: types.LEASE_STATE_ACTIVE},
		})
		require.ErrorContains(t, err, "invalid tenant address")
	})

	t.Run("genesis validation compares tenants by address", func(t *testing.T) {
		now := time.Now().UTC()
		lease := func(uuid, tenant string) types.Lease {
			return types.Lease{
				Uuid:         uuid,
				Tenant:       tenant,
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
				Items: []types.LeaseItem{
					{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin("umfx", math.NewInt(100))},
				},
				State:     types.LEASE_STATE_ACTIVE,
				CreatedAt: now,
			}
		}
		// The two leases, the credit account and the stream name the same
		// tenant in different spellings.
		gs := &types.GenesisState{
			Params: types.DefaultParams(),
			Leases: []types.Lease{
				lease("01912345-6789-7abc-8def-0123456789ab", upper),
				lease("01912345-6789-7abc-8def-0123456789ae", lower),
			},
			CreditAccounts: []types.CreditAccount{{
				Tenant:           upper,
				CreditAddress:    types.DeriveCreditAddress(addr).String(),
				ActiveLeaseCount: 2,
				ReservedAmounts:  sdk.NewCoins(sdk.NewCoin("umfx", math.NewInt(720_000))),
			}},
			LeaseSequence: 2,
			Streams: []types.Stream{{
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
				Tenant:       lower,
				Denom:        "umfx",
				State:        types.StreamState{Rate: math.NewInt(200), LastSettledAt: now, LeaseCount: 2},
			}},
		}
		require.NoError(t, gs.Validate())

		// The same stream listed under both spellings is a duplicate.
		gs.Streams = append(gs.Streams, gs.Streams[0])
		gs.Streams[1].Tenant = upper
		require.ErrorContains(t, gs.Validate(), "duplicate stream")
	})
}

func TestCompareStreamIDs(t *testing.T) {
	ids := []types.StreamID{
		{ProviderUUID: "p2", Tenant: "b", Denom: "x"},
		{ProviderUUID: "p1", Tenant: "b", Denom: "y"},
		{ProviderUUID: "p1", Tenant: "b", Denom: "x"},
		{ProviderUUID: "p9", Tenant: "a", Denom: "z"},
	}
	slices.SortFunc(ids, types.CompareStreamIDs)
	require.Equal(t, []types.StreamID{
		{ProviderUUID: "p9", Tenant: "a", Denom: "z"},
		{ProviderUUID: "p1", Tenant: "b", Denom: "x"},
		{ProviderUUID: "p1", Tenant: "b", Denom: "y"},
		{ProviderUUID: "p2", Tenant: "b", Denom: "x"},
	}, ids, "ordered by tenant, then provider, then denom")
}
