// Package v3 migrates the billing store from consensus version 2 to 3:
// per-lease settlement (Lease.last_settled_at) is replaced by settlement
// streams, one per (provider_uuid, tenant, denom).
package v3

import (
	"cmp"
	"context"
	"slices"
	"time"

	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// maxBackdateSeconds bounds how far a stream may be back-dated to carry an
// unpaid debt. It matches the keeper's MaxDurationSeconds (~100 years), beyond
// which a settlement treats the accrual as an overflow.
const maxBackdateSeconds = 100 * 365 * 24 * 60 * 60

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
// the stream is back-dated so the debt is still collected by its first
// settlement. The migration fails only if the result is internally
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

			for _, rate := range rates {
				p := get(types.StreamID{ProviderUUID: lease.ProviderUuid, Tenant: lease.Tenant, Denom: rate.Denom}, tenant)
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
	ids := make([]types.StreamID, 0, len(byStream))
	for id := range byStream {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b types.StreamID) int {
		return cmp.Or(
			cmp.Compare(a.Tenant, b.Tenant),
			cmp.Compare(a.ProviderUUID, b.ProviderUUID),
			cmp.Compare(a.Denom, b.Denom),
		)
	})

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
			// Keep the debt: start the stream far enough in the past that its
			// first settlement accrues what could not be paid now.
			state.LastSettledAt = backdate(now, owed, p.rate)
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

// backdate returns the settlement time that makes a stream of the given rate
// accrue owed by now.
func backdate(now time.Time, owed, rate sdkmath.Int) time.Time {
	if !rate.IsPositive() {
		return now
	}
	secs := owed.Quo(rate)
	if !secs.IsInt64() || secs.Int64() > maxBackdateSeconds {
		return now.Add(-maxBackdateSeconds * time.Second)
	}
	return now.Add(-time.Duration(secs.Int64()) * time.Second)
}
