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

// nanosPerSecond converts an amount in whole units of a denom into the unit of
// StreamState.Remainder (denom units × nanoseconds).
const nanosPerSecond = 1_000_000_000

// maxSettlementGapSeconds matches the keeper's MaxDurationSeconds (~100 years):
// a stream last settled longer ago than this is settled as an accrual overflow.
const maxSettlementGapSeconds = 100 * 365 * 24 * 60 * 60

// Keeper is the part of the billing keeper the migration works through.
type Keeper interface {
	Logger() log.Logger

	// GetAllLeases returns every lease, still carrying the deprecated
	// last_settled_at written by consensus version 2.
	GetAllLeases(ctx context.Context) ([]types.Lease, error)
	// RewriteLease overwrites the stored value of an existing lease without
	// touching its indexes.
	RewriteLease(ctx context.Context, lease types.Lease) error

	// CreditBalance returns the tenant's credit balance in denom.
	CreditBalance(ctx context.Context, tenant sdk.AccAddress, denom string) sdkmath.Int
	// PayProvider transfers amount from the tenant's credit account to the
	// provider's payout address.
	PayProvider(ctx context.Context, providerUUID string, tenant sdk.AccAddress, amount sdk.Coin) error

	// SetMigratedStream writes a stream with its reverse lookup, its membership
	// entries for leaseUUIDs and, when exhausted, its drain queue entry.
	SetMigratedStream(ctx context.Context, stream types.Stream, leaseUUIDs []string) error
	// CheckStreamInvariant verifies the streams match the ACTIVE leases.
	CheckStreamInvariant(ctx context.Context) error
}

// pending is what one (provider, tenant, denom) owes and aggregates at the
// upgrade height.
type pending struct {
	tenant sdk.AccAddress
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
// upgrade block time T0:
//
//  1. Every lease's unsettled accrual up to T0 is summed per
//     (provider, tenant, denom) and paid with one transfer, capped at the
//     tenant's credit balance.
//  2. A stream is created for each (provider, tenant, denom) with ACTIVE
//     leases, settled at T0. Where the credit could not cover what was owed
//     the stream starts frozen, so the EndBlocker closes its leases in the
//     blocks after the upgrade exactly as it does for any exhausted stream.
//  3. The deprecated last_settled_at is cleared on every lease.
//
// A data problem affecting one stream (for example a payout that fails) does
// not abort the upgrade: the payment is discarded, the problem is logged, and
// the stream is created owing exactly what could not be paid, so its first
// settlement still collects it. The migration fails only if the result is internally
// inconsistent, since continuing would misprice every later settlement.
func MigrateStore(ctx sdk.Context, k Keeper) error {
	now := ctx.BlockTime()

	leases, err := k.GetAllLeases(ctx)
	if err != nil {
		return err
	}

	byStream := make(map[types.StreamID]*pending)
	get := func(id types.StreamID, tenant sdk.AccAddress) *pending {
		p, ok := byStream[id]
		if !ok {
			p = &pending{tenant: tenant, rate: sdkmath.ZeroInt(), owed: sdkmath.ZeroInt()}
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
				p := get(types.NewStreamID(lease.ProviderUuid, tenant, rate.Denom), tenant)
				p.addOwed(rate.Amount, secs)

				// Only ACTIVE leases form a stream; a CLOSED lease only settles
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

	// Process in a fixed order: the result must not depend on map iteration,
	// and when a tenant's credit is short the order decides who is paid.
	ids := slices.SortedFunc(maps.Keys(byStream), types.CompareStreamIDs)

	for _, id := range ids {
		p := byStream[id]

		// Re-read the balance each time: an earlier payment of this loop may
		// have drawn on the same credit account.
		balance := k.CreditBalance(ctx, p.tenant, id.Denom)
		owed := p.owed
		if p.overflowed {
			owed = balance
		}
		pay := sdkmath.MinInt(owed, balance)

		paid := true
		if pay.IsPositive() {
			// Pay in a CacheContext so a failed transfer leaves nothing behind.
			cacheCtx, write := ctx.CacheContext()
			if payErr := k.PayProvider(cacheCtx, id.ProviderUUID, p.tenant, sdk.NewCoin(id.Denom, pay)); payErr != nil {
				k.Logger().Error("stream migration could not pay a provider, carrying the debt into the stream",
					"provider_uuid", id.ProviderUUID, "tenant", id.Tenant, "denom", id.Denom,
					"amount", pay.String(), "error", payErr)
				paid = false
			} else {
				write()
			}
		}

		// Nothing more to do when only CLOSED leases owed something.
		if len(p.leaseUUIDs) == 0 {
			continue
		}

		state := types.StreamState{
			Rate:          p.rate,
			LastSettledAt: now,
			LeaseCount:    uint64(len(p.leaseUUIDs)),
			Remainder:     sdkmath.ZeroInt(),
		}
		switch {
		case !paid:
			// Keep the whole debt: date the stream and set its remainder so
			// that it owes exactly this amount now. Its first settlement then
			// pays it along with what accrues until then.
			state.LastSettledAt, state.Remainder = carryDebt(now, owed, p.rate, p.overflowed)
		case p.overflowed || owed.GTE(balance):
			// Same rule as a normal settlement: the accrual consumed the whole
			// balance, so the stream is frozen and its leases will be closed.
			exhaustedAt := now
			state.ExhaustedAt = &exhaustedAt
		}

		stream := types.Stream{ProviderUuid: id.ProviderUUID, Tenant: id.Tenant, Denom: id.Denom, State: state}
		if err := k.SetMigratedStream(ctx, stream, p.leaseUUIDs); err != nil {
			return err
		}
	}

	if err := k.CheckStreamInvariant(ctx); err != nil {
		return types.ErrInvalidLease.Wrapf("stream migration produced inconsistent state: %s", err)
	}

	k.Logger().Info("migrated billing to settlement streams", "leases", len(leases), "streams_or_debts", len(ids))
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

// carryDebt returns the settlement time and remainder that make a stream of
// the given rate owe exactly owed at now.
//
// A stream accrues remainder + rate × nanoseconds since last_settled_at, in
// (denom units × nanoseconds). The debt in that unit is owed × 1e9; dividing
// it by the rate gives how far back to date the stream (the quotient, in
// nanoseconds) and what is left to put in the remainder (the rest, smaller
// than the rate). Nothing is rounded away, and neither value needs more bits
// than the rate does, however large the debt: scaling the debt to nanosecond
// units and storing that in the remainder alone would not fit for large debts.
//
// When the debt is not representable, or would date the stream further back
// than a settlement can accrue over, the stream is dated just beyond that
// limit. Its first settlement then follows the accrual-overflow rule (pay the
// whole balance and freeze), which is how a lease left unsettled for that long
// was treated before streams.
func carryDebt(now time.Time, owed, rate sdkmath.Int, unrepresentable bool) (lastSettledAt time.Time, remainder sdkmath.Int) {
	beyondLimit := now.Add(-(maxSettlementGapSeconds + 1) * time.Second)
	if unrepresentable || !rate.IsPositive() {
		return beyondLimit, sdkmath.ZeroInt()
	}

	scaled := new(big.Int).Mul(owed.BigInt(), big.NewInt(nanosPerSecond))
	gap, rest := new(big.Int).QuoRem(scaled, rate.BigInt(), new(big.Int))
	if !gap.IsInt64() || gap.Int64()/nanosPerSecond > maxSettlementGapSeconds {
		return beyondLimit, sdkmath.ZeroInt()
	}
	return now.Add(-time.Duration(gap.Int64())), sdkmath.NewIntFromBigInt(rest)
}
