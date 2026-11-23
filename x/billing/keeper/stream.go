package keeper

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// StreamKey identifies a stream in the store: (provider_uuid, (tenant, denom)).
// Provider comes first so a provider withdrawal ranges over exactly its own
// streams.
type StreamKey = collections.Pair[string, collections.Pair[sdk.AccAddress, string]]

// StreamTenantKey is the reverse lookup key: (tenant, (provider_uuid, denom)).
type StreamTenantKey = collections.Pair[sdk.AccAddress, collections.Pair[string, string]]

var (
	streamKeyCodec = collections.PairKeyCodec(
		collections.StringKey,
		collections.PairKeyCodec(sdk.AccAddressKey, collections.StringKey),
	)
	streamTenantKeyCodec = collections.PairKeyCodec(
		sdk.AccAddressKey,
		collections.PairKeyCodec(collections.StringKey, collections.StringKey),
	)
)

// NewStreamKey builds the store key of a stream.
func NewStreamKey(providerUUID string, tenant sdk.AccAddress, denom string) StreamKey {
	return collections.Join(providerUUID, collections.Join(tenant, denom))
}

// streamKeyParts splits a stream key into provider UUID, tenant and denom.
func streamKeyParts(key StreamKey) (providerUUID string, tenant sdk.AccAddress, denom string) {
	return key.K1(), key.K2().K1(), key.K2().K2()
}

// streamTenantKey returns the reverse lookup key of a stream.
func streamTenantKey(key StreamKey) StreamTenantKey {
	providerUUID, tenant, denom := streamKeyParts(key)
	return collections.Join(tenant, collections.Join(providerUUID, denom))
}

// streamFromState pairs a stored stream with its key, in the genesis/query shape.
func streamFromState(key StreamKey, state types.StreamState) types.Stream {
	providerUUID, tenant, denom := streamKeyParts(key)
	return types.Stream{
		ProviderUuid: providerUUID,
		Tenant:       tenant.String(),
		Denom:        denom,
		State:        state,
	}
}

// StreamSettlement is the outcome of settling one stream.
type StreamSettlement struct {
	// Transferred is the amount paid to the provider by this settlement.
	Transferred sdk.Coin
	// Exhausted is true when the stream is frozen after this settlement,
	// whether this settlement froze it or it already was.
	Exhausted bool
	// Froze is true when this settlement is the one that froze the stream.
	Froze bool
	// PaidInFull is true when this settlement accrued a positive amount and the
	// tenant's credit covered all of it. A stream can be both Exhausted and
	// PaidInFull: the credit exactly covered the charge (ENG-577).
	PaidInFull bool
}

// creditExhausted reports whether the settlement is a genuine credit
// exhaustion: the stream is frozen and the charge was not simply paid in full.
// That covers an unpaid remainder, an accrual overflow, a zero balance with
// nothing accrued, and a stream that was already frozen.
func (s StreamSettlement) creditExhausted() bool {
	return s.Exhausted && !s.PaidInFull
}

// nanosPerSecond scales the sub-second remainder a stream carries.
var nanosPerSecond = big.NewInt(int64(time.Second))

// streamRemainder returns the sub-unit accrual a stream carries, treating an
// unset value as zero.
func streamRemainder(state types.StreamState) sdkmath.Int {
	if state.Remainder.IsNil() {
		return sdkmath.ZeroInt()
	}
	return state.Remainder
}

// streamArrears returns the unpaid amount a stream carries on top of its
// accrual, treating an unset value as zero.
func streamArrears(state types.StreamState) sdkmath.Int {
	if state.Arrears.IsNil() {
		return sdkmath.ZeroInt()
	}
	return state.Arrears
}

// streamAccrual computes what a stream has accrued at now.
//
// Rates are per second and block time has nanosecond precision, so accrual is
// computed exactly: remainder + rate × elapsed nanoseconds, divided by 1e9.
// The quotient is what is payable in whole units of the denom (accrued) and
// what is left over (remainder, below 1e9) is carried to the next
// settlement. Nothing is rounded away between settlements, and because the
// stream's clock always advances to now, a lease that joins the stream is
// never charged for time before it joined.
//
// A frozen stream, or one last settled at or after now, accrues nothing. ok is
// false when the accrual is not representable: the gap exceeds
// MaxDurationSeconds, or the amount payable does not fit sdkmath.Int.
//
// The intermediate product is computed with arbitrary precision. rate × elapsed
// nanoseconds is 1e9 times larger than the amount it yields, so it can exceed
// the 256 bits of sdkmath.Int while the charge itself fits comfortably; only
// the charge decides whether the accrual is representable.
func streamAccrual(state types.StreamState, now time.Time) (accrued, remainder sdkmath.Int, ok bool) {
	remainder = streamRemainder(state)
	if state.IsExhausted() {
		return sdkmath.ZeroInt(), remainder, true
	}

	elapsed := now.Sub(state.LastSettledAt)
	if elapsed <= 0 {
		return sdkmath.ZeroInt(), remainder, true
	}
	if int64(elapsed/time.Second) > MaxDurationSeconds {
		return sdkmath.ZeroInt(), sdkmath.ZeroInt(), false
	}

	total := new(big.Int)
	if !state.Rate.IsNil() {
		total.Mul(state.Rate.BigInt(), big.NewInt(int64(elapsed)))
	}
	total.Add(total, remainder.BigInt())

	units, rest := new(big.Int).QuoRem(total, nanosPerSecond, new(big.Int))
	if units.BitLen() > sdkmath.MaxBitLen {
		return sdkmath.ZeroInt(), sdkmath.ZeroInt(), false
	}
	return sdkmath.NewIntFromBigInt(units), sdkmath.NewIntFromBigInt(rest), true
}

// streamDue computes what a stream owes the provider at now: what it has
// accrued (see streamAccrual) plus its arrears, an amount that was due earlier
// and could not be transferred. remainder is the sub-unit accrual to carry.
// A frozen stream owes nothing. ok is false when the amount is not
// representable.
func streamDue(state types.StreamState, now time.Time) (due, remainder sdkmath.Int, ok bool) {
	accrued, remainder, ok := streamAccrual(state, now)
	if !ok {
		return sdkmath.ZeroInt(), sdkmath.ZeroInt(), false
	}
	if state.IsExhausted() {
		return sdkmath.ZeroInt(), remainder, true
	}
	due, err := accrued.SafeAdd(streamArrears(state))
	if err != nil {
		return sdkmath.ZeroInt(), sdkmath.ZeroInt(), false
	}
	return due, remainder, true
}

// streamWithdrawable returns what settling a stream at now would pay the
// provider: what it owes, capped at the tenant's credit balance. It is read-only.
func (k *Keeper) streamWithdrawable(ctx context.Context, key StreamKey, state types.StreamState, now time.Time) sdk.Coin {
	_, tenant, denom := streamKeyParts(key)
	if state.IsExhausted() {
		return sdk.NewCoin(denom, sdkmath.ZeroInt())
	}

	balance := k.bankKeeper.GetBalance(ctx, types.DeriveCreditAddress(tenant), denom).Amount
	due, _, ok := streamDue(state, now)
	if !ok {
		// An unrepresentable amount certainly exceeds the balance.
		return sdk.NewCoin(denom, balance)
	}
	return sdk.NewCoin(denom, sdkmath.MinInt(due, balance))
}

// settleStreamState settles a stream up to now: it pays the provider what the
// stream owes (its accrual since last_settled_at plus any arrears, capped at
// the tenant's credit balance), moves last_settled_at to now, and freezes the
// stream when the credit is exhausted.
//
// It mutates state but does not persist it; the caller writes the stream. The
// bank transfer, the drain queue entry and the events are written to ctx, so
// the caller must run it in a context it is prepared to discard on error.
func (k *Keeper) settleStreamState(ctx context.Context, key StreamKey, state *types.StreamState, now time.Time) (StreamSettlement, error) {
	providerUUID, tenant, denom := streamKeyParts(key)
	result := StreamSettlement{Transferred: sdk.NewCoin(denom, sdkmath.ZeroInt())}

	// A frozen stream no longer accrues: its leases are closed by the EndBlocker.
	if state.IsExhausted() {
		result.Exhausted = true
		return result, nil
	}

	creditAddr := types.DeriveCreditAddress(tenant)
	balance := k.bankKeeper.GetBalance(ctx, creditAddr, denom).Amount

	// Accrue exactly up to now. What does not amount to a whole unit of the
	// denom is carried in the remainder, so the clock can always move to now.
	// Arrears are due in full: they are either paid by this settlement or, if
	// the credit does not cover them, forgiven with the rest of the shortfall
	// when the stream freezes.
	accrued, remainder, representable := streamDue(*state, now)
	overflowed := !representable
	if overflowed {
		// What is owed is not representable, so it certainly exceeds the
		// balance: the provider receives everything that remains.
		k.logger.Warn("stream accrual overflow, transferring remaining credit",
			"provider_uuid", providerUUID,
			"tenant", tenant.String(),
			"denom", denom,
			"last_settled_at", state.LastSettledAt,
		)
		accrued = balance
		remainder = sdkmath.ZeroInt()
	} else {
		result.PaidInFull = accrued.IsPositive() && accrued.LTE(balance)
	}
	state.Remainder = remainder
	state.Arrears = sdkmath.ZeroInt()
	// A stream dated in the future (only reachable through a hand-written
	// genesis, which validation rejects) accrues nothing and keeps its date.
	if now.After(state.LastSettledAt) {
		state.LastSettledAt = now
	}

	transfer := sdkmath.MinInt(accrued, balance)

	if transfer.IsPositive() {
		provider, err := k.skuKeeper.GetProvider(ctx, providerUUID)
		if err != nil {
			return result, types.ErrProviderNotFound.Wrapf("provider_uuid %s not found", providerUUID)
		}
		payoutAddr, err := sdk.AccAddressFromBech32(provider.PayoutAddress)
		if err != nil {
			return result, types.ErrProviderNotFound.Wrapf("invalid payout address: %s", err)
		}

		amount := sdk.NewCoin(denom, transfer)
		if err := k.bankKeeper.SendCoins(ctx, creditAddr, payoutAddr, sdk.NewCoins(amount)); err != nil {
			return result, types.ErrInvalidCreditOperation.Wrapf("failed to transfer: %s", err)
		}
		result.Transferred = amount

		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeStreamSettled,
				sdk.NewAttribute(types.AttributeKeyProviderUUID, providerUUID),
				sdk.NewAttribute(types.AttributeKeyTenant, tenant.String()),
				sdk.NewAttribute(types.AttributeKeyAmount, amount.String()),
				sdk.NewAttribute(types.AttributeKeyPayoutAddress, provider.PayoutAddress),
			),
		)
	}

	// Credit is exhausted when the accrual consumes the whole balance. This is
	// the rule the per-lease auto-close used (accrued >= balance); it also
	// covers a zero balance with nothing accrued.
	if overflowed || accrued.GTE(balance) {
		exhaustedAt := now
		state.ExhaustedAt = &exhaustedAt
		result.Exhausted = true
		result.Froze = true

		if err := k.ExhaustedStreams.Set(ctx, key, ""); err != nil {
			return result, err
		}

		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeStreamExhausted,
				sdk.NewAttribute(types.AttributeKeyProviderUUID, providerUUID),
				sdk.NewAttribute(types.AttributeKeyTenant, tenant.String()),
				sdk.NewAttribute(types.AttributeKeyDenom, denom),
			),
		)
	}

	return result, nil
}

// SettleStream loads a stream, settles it up to now and writes it back. It
// returns collections.ErrNotFound when the stream does not exist.
func (k *Keeper) SettleStream(ctx context.Context, key StreamKey, now time.Time) (StreamSettlement, error) {
	state, err := k.Streams.Get(ctx, key)
	if err != nil {
		return StreamSettlement{}, err
	}

	result, err := k.settleStreamState(ctx, key, &state, now)
	if err != nil {
		return result, err
	}

	if err := k.Streams.Set(ctx, key, state); err != nil {
		return result, err
	}
	return result, nil
}

// deleteStream removes a stream together with its reverse lookup entry and any
// drain queue entry. It is the only place a stream is removed.
func (k *Keeper) deleteStream(ctx context.Context, key StreamKey) error {
	if err := k.Streams.Remove(ctx, key); err != nil {
		return err
	}
	if err := k.StreamsByTenant.Remove(ctx, streamTenantKey(key)); err != nil {
		return err
	}
	return k.ExhaustedStreams.Remove(ctx, key)
}

// streamEntry is one stream held in a streamCache.
type streamEntry struct {
	key   StreamKey
	state types.StreamState
	// stored is true when the stream exists in the store (so it has a reverse
	// lookup entry).
	stored bool
	// settlement is the outcome of the one settlement this cache ran.
	settlement StreamSettlement
}

// streamCache batches stream updates for one message (or one EndBlocker item).
// Each stream is read once, settled once and written once however many leases
// of the batch belong to it, which is what keeps a 382-lease close from doing
// 382 stream writes and 382 transfers.
//
// A cache is tied to the context it was used with: if that context is
// discarded, the cache must be discarded too.
type streamCache struct {
	k       *Keeper
	now     time.Time
	entries map[string]*streamEntry
	// order lists entries in first-touch order so flush is deterministic.
	order []string
}

func (k *Keeper) newStreamCache(now time.Time) *streamCache {
	return &streamCache{k: k, now: now, entries: make(map[string]*streamEntry)}
}

// LeaseStreamResult summarizes what joining or leaving streams did for a lease.
type LeaseStreamResult struct {
	// Transferred is what the settlements triggered by this call paid out. A
	// stream is settled once per cache, so later leases of the same stream
	// report nothing here.
	Transferred sdk.Coins
	// CreditExhausted is true when any of the lease's streams is frozen for a
	// reason other than a charge paid in full (see StreamSettlement).
	CreditExhausted bool
}

// entry returns the cached entry for a stream, loading and settling it on
// first touch. A stream that does not exist yet starts empty at now when
// create is true; otherwise a missing stream yields nil.
func (c *streamCache) entry(ctx context.Context, key StreamKey, create bool) (*streamEntry, sdk.Coin, error) {
	buf := make([]byte, streamKeyCodec.Size(key))
	if _, err := streamKeyCodec.Encode(buf, key); err != nil {
		return nil, sdk.Coin{}, err
	}
	id := string(buf)

	_, _, denom := streamKeyParts(key)
	none := sdk.NewCoin(denom, sdkmath.ZeroInt())

	if e, ok := c.entries[id]; ok {
		return e, none, nil
	}

	e := &streamEntry{key: key}
	state, err := c.k.Streams.Get(ctx, key)
	switch {
	case err == nil:
		e.state = state
		e.stored = true
	case errors.Is(err, collections.ErrNotFound):
		if !create {
			return nil, none, nil
		}
		e.state = types.StreamState{
			Rate:          sdkmath.ZeroInt(),
			LastSettledAt: c.now,
			Remainder:     sdkmath.ZeroInt(),
			Arrears:       sdkmath.ZeroInt(),
		}
	default:
		return nil, none, err
	}

	settlement, err := c.k.settleStreamState(ctx, key, &e.state, c.now)
	if err != nil {
		return nil, none, err
	}
	e.settlement = settlement

	c.entries[id] = e
	c.order = append(c.order, id)
	return e, settlement.Transferred, nil
}

// join adds an ACTIVE lease to its streams: one stream per denom the lease is
// priced in. Each stream is settled before its rate changes.
func (c *streamCache) join(ctx context.Context, lease *types.Lease) (LeaseStreamResult, error) {
	result := LeaseStreamResult{Transferred: sdk.NewCoins()}

	tenant, err := sdk.AccAddressFromBech32(lease.Tenant)
	if err != nil {
		return result, err
	}
	rates, err := types.LeaseRatesPerSecond(lease.Items)
	if err != nil {
		return result, types.ErrInvalidLease.Wrap(err.Error())
	}

	for _, rate := range rates {
		key := NewStreamKey(lease.ProviderUuid, tenant, rate.Denom)
		e, transferred, err := c.entry(ctx, key, true)
		if err != nil {
			return result, err
		}

		newRate, err := e.state.Rate.SafeAdd(rate.Amount)
		if err != nil {
			return result, types.ErrInvalidLease.Wrapf("stream rate overflow: %s", err)
		}
		e.state.Rate = newRate
		e.state.LeaseCount++

		if err := c.k.StreamLeases.Set(ctx, collections.Join(key, lease.Uuid)); err != nil {
			return result, err
		}

		result.add(transferred, e.settlement)
	}

	return result, nil
}

// leave removes a lease from its streams. Each stream is settled before its
// rate changes, so the lease pays up to now.
func (c *streamCache) leave(ctx context.Context, lease *types.Lease) (LeaseStreamResult, error) {
	result := LeaseStreamResult{Transferred: sdk.NewCoins()}

	tenant, err := sdk.AccAddressFromBech32(lease.Tenant)
	if err != nil {
		return result, err
	}
	rates, err := types.LeaseRatesPerSecond(lease.Items)
	if err != nil {
		return result, types.ErrInvalidLease.Wrap(err.Error())
	}

	for _, rate := range rates {
		key := NewStreamKey(lease.ProviderUuid, tenant, rate.Denom)
		e, transferred, err := c.entry(ctx, key, false)
		if err != nil {
			return result, err
		}
		if e == nil {
			// The lease is ACTIVE but has no stream: state has drifted. Do not
			// fail the close over it.
			c.k.logger.Error("data inconsistency: active lease has no stream",
				"lease_uuid", lease.Uuid,
				"provider_uuid", lease.ProviderUuid,
				"tenant", lease.Tenant,
				"denom", rate.Denom,
			)
			continue
		}

		if e.state.Rate.LT(rate.Amount) {
			c.k.logger.Error("data inconsistency: stream rate would go negative",
				"lease_uuid", lease.Uuid,
				"stream_rate", e.state.Rate.String(),
				"lease_rate", rate.Amount.String(),
			)
			e.state.Rate = sdkmath.ZeroInt()
		} else {
			e.state.Rate = e.state.Rate.Sub(rate.Amount)
		}

		if e.state.LeaseCount > 0 {
			e.state.LeaseCount--
		} else {
			c.k.logger.Error("data inconsistency: stream lease count already zero",
				"lease_uuid", lease.Uuid,
			)
		}

		if err := c.k.StreamLeases.Remove(ctx, collections.Join(key, lease.Uuid)); err != nil {
			return result, err
		}

		result.add(transferred, e.settlement)
	}

	return result, nil
}

func (r *LeaseStreamResult) add(transferred sdk.Coin, settlement StreamSettlement) {
	if transferred.IsPositive() {
		r.Transferred = r.Transferred.Add(transferred)
	}
	r.CreditExhausted = r.CreditExhausted || settlement.creditExhausted()
}

// flush writes every touched stream once, in first-touch order. A stream left
// without leases is deleted.
func (c *streamCache) flush(ctx context.Context) error {
	for _, id := range c.order {
		e := c.entries[id]

		if e.state.LeaseCount == 0 {
			if e.stored {
				if err := c.k.deleteStream(ctx, e.key); err != nil {
					return err
				}
			} else if err := c.k.ExhaustedStreams.Remove(ctx, e.key); err != nil {
				// A stream created and emptied within one cache may still have
				// queued itself when it was settled.
				return err
			}
			continue
		}

		if err := c.k.Streams.Set(ctx, e.key, e.state); err != nil {
			return err
		}
		if !e.stored {
			if err := c.k.StreamsByTenant.Set(ctx, streamTenantKey(e.key)); err != nil {
				return err
			}
			e.stored = true
		}
	}
	return nil
}

// CheckStreamInvariant verifies that the stored streams match the ACTIVE
// leases: every expected stream exists with the expected rate and lease count,
// no other stream exists, and each stream's membership and reverse lookup
// entries are present. It returns the first violation found.
func (k *Keeper) CheckStreamInvariant(ctx context.Context) error {
	leases, err := k.GetLeasesByState(ctx, types.LEASE_STATE_ACTIVE)
	if err != nil {
		return err
	}
	expected, err := types.ExpectedStreamAggregates(leases)
	if err != nil {
		return err
	}

	seen := 0
	err = k.Streams.Walk(ctx, nil, func(key StreamKey, state types.StreamState) (bool, error) {
		id := streamFromState(key, state).ID()
		want, ok := expected[id]
		if !ok {
			return true, fmt.Errorf("stream %v exists without active leases", id)
		}
		if !state.Rate.Equal(want.Rate) {
			return true, fmt.Errorf("stream %v rate %s, expected %s", id, state.Rate, want.Rate)
		}
		if state.LeaseCount != want.LeaseCount {
			return true, fmt.Errorf("stream %v lease count %d, expected %d", id, state.LeaseCount, want.LeaseCount)
		}

		has, err := k.StreamsByTenant.Has(ctx, streamTenantKey(key))
		if err != nil {
			return true, err
		}
		if !has {
			return true, fmt.Errorf("stream %v has no tenant lookup entry", id)
		}

		members, err := k.countStreamLeases(ctx, key)
		if err != nil {
			return true, err
		}
		if members != want.LeaseCount {
			return true, fmt.Errorf("stream %v has %d membership entries, expected %d", id, members, want.LeaseCount)
		}

		queued, err := k.ExhaustedStreams.Has(ctx, key)
		if err != nil {
			return true, err
		}
		if queued != state.IsExhausted() {
			return true, fmt.Errorf("stream %v exhausted=%t but queued=%t", id, state.IsExhausted(), queued)
		}

		seen++
		return false, nil
	})
	if err != nil {
		return err
	}
	if seen != len(expected) {
		return fmt.Errorf("found %d streams, expected %d from active leases", seen, len(expected))
	}
	return nil
}

// countStreamLeases counts the membership entries of a stream.
func (k *Keeper) countStreamLeases(ctx context.Context, key StreamKey) (uint64, error) {
	rng := collections.NewPrefixedPairRange[StreamKey, string](key)
	iter, err := k.StreamLeases.Iterate(ctx, rng)
	if err != nil {
		return 0, err
	}
	defer iter.Close()

	var count uint64
	for ; iter.Valid(); iter.Next() {
		count++
	}
	return count, nil
}
