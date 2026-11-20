// Package v3 migrates the billing store from consensus version 2 to 3:
// per-lease settlement (Lease.last_settled_at) is replaced by settlement
// streams, one per (provider_uuid, tenant, denom).
package v3

import (
	"context"
	"maps"
	"math/big"
	"slices"
	"time"

	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// Keeper is the part of the billing keeper the migration works through.
type Keeper interface {
	Logger() log.Logger

	// GetAllLeases returns every lease, still carrying the deprecated
	// last_settled_at written by consensus version 2.
	GetAllLeases(ctx context.Context) ([]types.Lease, error)
	// RewriteLease overwrites the stored value of an existing lease without
	// touching its indexes.
	RewriteLease(ctx context.Context, lease types.Lease) error

	// SetMigratedStream writes a stream with its reverse lookup and its
	// membership entries for leaseUUIDs.
	SetMigratedStream(ctx context.Context, stream types.Stream, leaseUUIDs []string) error
	// CheckStreamInvariant verifies the streams match the ACTIVE leases.
	CheckStreamInvariant(ctx context.Context) error
}

// pending is what one (provider, tenant, denom) owes and aggregates at the
// upgrade height.
type pending struct {
	// rate and leaseUUIDs describe the stream to create; they stay empty when
	// only CLOSED leases owe something.
	rate       sdkmath.Int
	leaseUUIDs []string
	// owed is what the leases accrued under per-lease settlement and have not
	// been paid yet.
	owed sdkmath.Int
	// overflowed is true when owed is not representable.
	overflowed bool
}

// MigrateStore converts per-lease settlement to settlement streams at the
// upgrade block time T0. It only rewrites the billing store: no coins move.
//
//  1. Every lease's unsettled accrual up to T0, as per-lease settlement
//     computed it, is summed per (provider, tenant, denom).
//  2. A stream is created for each (provider, tenant, denom) with ACTIVE
//     leases. It starts accruing at T0 and carries that sum as its arrears.
//  3. The deprecated last_settled_at is cleared on every lease.
//
// The provider's first settlement after the upgrade (a withdrawal, or a lease
// joining or leaving the stream) pays the arrears together with what accrued
// since, through the same code path, events and credit-exhaustion rule as any
// other settlement. Under per-lease settlement that amount was likewise only
// paid when the provider withdrew, so nothing is paid later than it would
// have been.
//
// Paying at the upgrade instead would make the outcome of the migration
// depend on the bank module (a transfer can fail), which needs a second path
// for what could not be paid; keeping the debt as state needs only one.
//
// The migration fails only if the result is internally inconsistent, since
// continuing would misprice every later settlement.
func MigrateStore(ctx sdk.Context, k Keeper) error {
	now := ctx.BlockTime()

	leases, err := k.GetAllLeases(ctx)
	if err != nil {
		return err
	}

	byStream := make(map[types.StreamID]*pending)
	get := func(id types.StreamID) *pending {
		p, ok := byStream[id]
		if !ok {
			p = &pending{rate: sdkmath.ZeroInt(), owed: sdkmath.ZeroInt()}
			byStream[id] = p
		}
		return p
	}

	for i := range leases {
		lease := &leases[i]
		settledAt := lease.LastSettledAt //nolint:staticcheck // the migration reads the deprecated field

		if secs, accrues := unsettledSeconds(lease, settledAt, now); accrues {
			tenant, addrErr := sdk.AccAddressFromBech32(lease.Tenant)
			if addrErr != nil {
				k.Logger().Error("skipping lease with invalid tenant address in stream migration",
					"lease_uuid", lease.Uuid, "error", addrErr)
				continue
			}
			rates, rateErr := types.LeaseRatesPerSecond(lease.Items)
			if rateErr != nil {
				k.Logger().Error("skipping lease with unrepresentable rate in stream migration",
					"lease_uuid", lease.Uuid, "error", rateErr)
				continue
			}

			// Group by the tenant's address, not by lease.Tenant: v2 stored the
			// tenant as it was sent, and bech32 is also valid in upper case.
			for _, rate := range rates {
				p := get(types.NewStreamID(lease.ProviderUuid, tenant, rate.Denom))
				p.addOwed(rate.Amount, secs)

				// Only ACTIVE leases form a stream; a CLOSED lease only adds
				// what it still owed up to its closure.
				if lease.State == types.LEASE_STATE_ACTIVE {
					total, addErr := p.rate.SafeAdd(rate.Amount)
					if addErr != nil {
						return types.ErrInvalidLease.Wrapf("stream rate overflow for lease %s: %s", lease.Uuid, addErr)
					}
					p.rate = total
					p.leaseUUIDs = append(p.leaseUUIDs, lease.Uuid)
				}
			}
		}

		// Clear the deprecated field so stored leases are canonical.
		if settledAt != nil {
			lease.LastSettledAt = nil //nolint:staticcheck // the migration clears the deprecated field
			if err := k.RewriteLease(ctx, *lease); err != nil {
				return err
			}
		}
	}

	// Write in a fixed order, so the store writes (and the gas they consume)
	// do not depend on map iteration.
	ids := slices.SortedFunc(maps.Keys(byStream), types.CompareStreamIDs)

	streams := 0
	for _, id := range ids {
		p := byStream[id]

		// What was owed is not representable, so it exceeds any balance. The
		// largest representable amount has the same effect: the first
		// settlement pays the whole balance and freezes the stream.
		arrears := p.owed
		if p.overflowed {
			arrears = maxInt()
		}

		if len(p.leaseUUIDs) == 0 {
			// Only CLOSED leases owed something, and with no ACTIVE lease there
			// is no stream to carry it. Handler-closed leases are always
			// settled up to their closure, so this needs a hand-written
			// genesis; it is reported rather than silently dropped.
			if arrears.IsPositive() {
				k.Logger().Error("stream migration found closed leases with unsettled accrual and no stream to carry it",
					"provider_uuid", id.ProviderUUID, "tenant", id.Tenant, "denom", id.Denom,
					"amount", arrears.String())
			}
			continue
		}

		stream := types.Stream{
			ProviderUuid: id.ProviderUUID,
			Tenant:       id.Tenant,
			Denom:        id.Denom,
			State: types.StreamState{
				Rate:          p.rate,
				LastSettledAt: now,
				LeaseCount:    uint64(len(p.leaseUUIDs)),
				Remainder:     sdkmath.ZeroInt(),
				Arrears:       arrears,
			},
		}
		if err := k.SetMigratedStream(ctx, stream, p.leaseUUIDs); err != nil {
			return err
		}
		streams++
	}

	if err := k.CheckStreamInvariant(ctx); err != nil {
		return types.ErrInvalidLease.Wrapf("stream migration produced inconsistent state: %s", err)
	}

	k.Logger().Info("migrated billing to settlement streams", "leases", len(leases), "streams", streams)
	return nil
}

// unsettledSeconds returns how many whole seconds of accrual a lease still
// owes at the upgrade, and whether it accrues at all.
//
// An ACTIVE lease accrues from its last settlement up to now. A CLOSED lease
// normally has last_settled_at == closed_at; one that does not (only reachable
// through a hand-written genesis) still owes up to its closure, and that debt
// would be unrecoverable once the field is gone.
func unsettledSeconds(lease *types.Lease, settledAt *time.Time, now time.Time) (int64, bool) {
	var from, to time.Time

	switch lease.State {
	case types.LEASE_STATE_ACTIVE:
		// Billing starts at acknowledgement; fall back to it when the field is
		// missing or predates it.
		start := lease.CreatedAt
		if lease.AcknowledgedAt != nil {
			start = *lease.AcknowledgedAt
		}
		from = start
		if settledAt != nil && settledAt.After(start) {
			from = *settledAt
		}
		to = now
	case types.LEASE_STATE_CLOSED:
		if settledAt == nil || lease.ClosedAt == nil {
			return 0, false
		}
		from, to = *settledAt, *lease.ClosedAt
		if to.After(now) {
			to = now
		}
	default:
		return 0, false
	}

	secs := int64(to.Sub(from) / time.Second)
	if secs < 0 {
		// A settlement time in the future: nothing is owed yet.
		secs = 0
	}
	// An ACTIVE lease joins a stream even when it owes nothing.
	return secs, lease.State == types.LEASE_STATE_ACTIVE || secs > 0
}

// addOwed adds rate × secs to what is owed, flagging an overflow instead of
// panicking.
func (p *pending) addOwed(rate sdkmath.Int, secs int64) {
	if p.overflowed {
		return
	}
	accrued, err := rate.SafeMul(sdkmath.NewInt(secs))
	if err != nil {
		p.overflowed = true
		return
	}
	total, err := p.owed.SafeAdd(accrued)
	if err != nil {
		p.overflowed = true
		return
	}
	p.owed = total
}

// maxInt returns the largest amount sdkmath.Int can hold.
func maxInt() sdkmath.Int {
	limit := new(big.Int).Lsh(big.NewInt(1), sdkmath.MaxBitLen)
	return sdkmath.NewIntFromBigInt(limit.Sub(limit, big.NewInt(1)))
}
