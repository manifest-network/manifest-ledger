package types

import (
	"cmp"
	"fmt"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Settlement streams
//
// A stream aggregates every ACTIVE lease a tenant holds with one provider in
// one denom. Accrual is settled per stream, so a provider withdrawal costs
// O(streams) instead of O(leases).
//
// # Invariant
//
// For every (provider_uuid, tenant, denom):
//
//	Stream.Rate       == SUM(locked_price × quantity) over the denom's items
//	                     of all ACTIVE leases of that tenant with that provider
//	Stream.LeaseCount == number of those leases
//
// and a stream exists if and only if LeaseCount > 0. An exhausted stream keeps
// its rate and lease count; it only stops accruing.

// StreamID identifies a stream by its bech32/UUID string parts. It is
// comparable, so it can key a map.
//
// Tenant is always the canonical (lower-case) bech32 spelling of the tenant's
// address. Bech32 is also valid in upper case and leases store the tenant as
// it was sent, so a StreamID must be built with NewStreamID (or Stream.ID),
// never from a raw tenant string: otherwise one tenant spelled two ways would
// look like two.
type StreamID struct {
	ProviderUUID string
	Tenant       string
	Denom        string
}

// NewStreamID builds the identifier of the stream of a provider, tenant and denom.
func NewStreamID(providerUUID string, tenant sdk.AccAddress, denom string) StreamID {
	return StreamID{ProviderUUID: providerUUID, Tenant: tenant.String(), Denom: denom}
}

// CompareStreamIDs orders stream identifiers by tenant, then provider, then denom.
func CompareStreamIDs(a, b StreamID) int {
	return cmp.Or(
		cmp.Compare(a.Tenant, b.Tenant),
		cmp.Compare(a.ProviderUUID, b.ProviderUUID),
		cmp.Compare(a.Denom, b.Denom),
	)
}

// StreamAggregate is the part of a stream that is derived from leases.
type StreamAggregate struct {
	Rate       sdkmath.Int
	LeaseCount uint64
}

// LeaseRatesPerSecond returns the per-second rate of a lease per denom:
// sum(locked_price × quantity) over its items. The result is a sorted Coins,
// so iterating it is deterministic. It returns an error instead of panicking
// when the arithmetic overflows.
func LeaseRatesPerSecond(items []LeaseItem) (sdk.Coins, error) {
	totals := make(map[string]sdkmath.Int, len(items))
	denoms := make([]string, 0, len(items))
	for _, item := range items {
		itemRate, err := item.LockedPrice.Amount.SafeMul(sdkmath.NewIntFromUint64(item.Quantity))
		if err != nil {
			return nil, fmt.Errorf("rate overflow for sku %s: %w", item.SkuUuid, err)
		}
		if !itemRate.IsPositive() {
			continue
		}
		denom := item.LockedPrice.Denom
		total, seen := totals[denom]
		if !seen {
			total = sdkmath.ZeroInt()
			denoms = append(denoms, denom)
		}
		total, err = total.SafeAdd(itemRate)
		if err != nil {
			return nil, fmt.Errorf("rate overflow for denom %s: %w", denom, err)
		}
		totals[denom] = total
	}

	// denoms is in first-seen order; NewCoins sorts, so the result does not
	// depend on map iteration.
	coins := make([]sdk.Coin, 0, len(denoms))
	for _, denom := range denoms {
		coins = append(coins, sdk.NewCoin(denom, totals[denom]))
	}
	return sdk.NewCoins(coins...), nil
}

// ExpectedStreamAggregates recomputes, from a list of leases, the rate and
// lease count every stream must have. Only ACTIVE leases contribute.
func ExpectedStreamAggregates(leases []Lease) (map[StreamID]StreamAggregate, error) {
	expected := make(map[StreamID]StreamAggregate)
	for i := range leases {
		lease := &leases[i]
		if lease.State != LEASE_STATE_ACTIVE {
			continue
		}
		tenant, err := sdk.AccAddressFromBech32(lease.Tenant)
		if err != nil {
			return nil, fmt.Errorf("lease %s: invalid tenant address: %w", lease.Uuid, err)
		}
		rates, err := LeaseRatesPerSecond(lease.Items)
		if err != nil {
			return nil, fmt.Errorf("lease %s: %w", lease.Uuid, err)
		}
		for _, rate := range rates {
			id := NewStreamID(lease.ProviderUuid, tenant, rate.Denom)
			agg, ok := expected[id]
			if !ok {
				agg = StreamAggregate{Rate: sdkmath.ZeroInt()}
			}
			total, err := agg.Rate.SafeAdd(rate.Amount)
			if err != nil {
				return nil, fmt.Errorf("stream %v: rate overflow: %w", id, err)
			}
			agg.Rate = total
			agg.LeaseCount++
			expected[id] = agg
		}
	}
	return expected, nil
}

// ID returns the stream's identifier, with the tenant in its canonical
// spelling. A tenant that is not a valid address is kept as is; validation
// rejects such a stream separately.
func (s Stream) ID() StreamID {
	return StreamID{ProviderUUID: s.ProviderUuid, Tenant: CanonicalAddress(s.Tenant), Denom: s.Denom}
}

// IsExhausted reports whether the stream is frozen awaiting lease closure.
func (s StreamState) IsExhausted() bool {
	return s.ExhaustedAt != nil
}
