package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// exhaustedStreamEntry is one entry of the drain queue.
type exhaustedStreamEntry struct {
	key    StreamKey
	cursor string
}

// DrainExhaustedStreams closes the leases of streams frozen for exhausted
// credit. It is called by the EndBlocker.
//
// EndBlocker work is not gas-metered, so the drain is bounded: it attempts at
// most MaxAutoClosesPerBlock lease closures per block and resumes in the next
// block. Each closure runs in its own CacheContext and never returns an error
// to the EndBlocker: a lease that cannot be closed is logged, skipped via the
// stream's cursor and retried later, so it can neither halt the chain nor
// starve the leases queued behind it.
//
// The drain is also fair to streams. A frozen stream can keep receiving newly
// acknowledged leases, so neither one stream nor the first streams of the
// queue may be allowed to absorb the whole budget block after block:
//   - within a block, every stream collected gets an equal share of the budget
//     before any of them gets more;
//   - across blocks, when more streams are queued than one block can serve,
//     the walk of the queue resumes after the last stream served, so it
//     rotates through the whole queue.
func (k *Keeper) DrainExhaustedStreams(ctx context.Context, minLeaseDuration uint64) {
	budget := types.MaxAutoClosesPerBlock

	// Every stream needs at least one attempt, so at most `budget` streams can
	// be served in a block.
	entries, start, truncated, err := k.collectExhaustedStreams(ctx, budget)
	if err != nil {
		k.logger.Error("failed to collect exhausted streams", "error", err)
		return
	}

	closed := 0
	drain := func(perStream int, wrap bool) {
		for i := range entries {
			if budget <= 0 {
				return
			}
			attempts, closedHere, cursor := k.drainStream(ctx, entries[i], min(perStream, budget), wrap, minLeaseDuration)
			budget -= attempts
			closed += closedHere
			entries[i].cursor = cursor
		}
	}
	if len(entries) > 0 {
		// First an equal share each, then whatever is left in queue order. The
		// second pass does not wrap around a stream's cursor, which keeps it
		// from spending the leftover budget on leases that just failed.
		drain(max(1, budget/len(entries)), true)
		drain(budget, false)
	}

	// Rotate only while the queue is longer than one block can serve; otherwise
	// every queued stream is served every block and no state needs writing.
	switch {
	case truncated:
		if setErr := k.ExhaustedStreamsStart.Set(ctx, entries[len(entries)-1].key); setErr != nil {
			k.logger.Error("failed to save exhausted streams start key", "error", setErr)
		}
	case start != nil:
		if rmErr := k.ExhaustedStreamsStart.Remove(ctx); rmErr != nil {
			k.logger.Error("failed to clear exhausted streams start key", "error", rmErr)
		}
	}

	if closed > 0 {
		k.logger.Info("auto-closed leases of exhausted streams in EndBlocker", "closed_count", closed)
	}
	if budget <= 0 {
		k.logger.Warn("reached max auto-closes per block", "limit", types.MaxAutoClosesPerBlock)
	}
}

// collectExhaustedStreams returns up to limit entries of the drain queue. It
// starts after the saved start key (if any) and wraps around to the beginning,
// so successive blocks rotate through a queue too long to serve in one block.
// start is the saved start key it resumed from, and truncated reports whether
// more streams are queued than were returned.
//
// The entries are collected before anything is drained because draining
// deletes entries from the collection being iterated.
func (k *Keeper) collectExhaustedStreams(ctx context.Context, limit int) (entries []exhaustedStreamEntry, start *StreamKey, truncated bool, err error) {
	saved, getErr := k.ExhaustedStreamsStart.Get(ctx)
	switch {
	case getErr == nil:
		start = &saved
	case !errors.Is(getErr, collections.ErrNotFound):
		return nil, nil, false, getErr
	}

	entries = make([]exhaustedStreamEntry, 0)
	collect := func(rng collections.Ranger[StreamKey]) error {
		iter, iterErr := k.ExhaustedStreams.Iterate(ctx, rng)
		if iterErr != nil {
			return iterErr
		}
		defer iter.Close()

		for ; iter.Valid(); iter.Next() {
			if len(entries) >= limit {
				truncated = true
				return nil
			}
			kv, kvErr := iter.KeyValue()
			if kvErr != nil {
				return kvErr
			}
			entries = append(entries, exhaustedStreamEntry{key: kv.Key, cursor: kv.Value})
		}
		return nil
	}

	if start == nil {
		return entries, nil, truncated, collect(nil)
	}

	// Streams after the start key first, then wrap around up to and including it.
	if err := collect(new(collections.Range[StreamKey]).StartExclusive(*start)); err != nil {
		return nil, start, false, err
	}
	if !truncated {
		if err := collect(new(collections.Range[StreamKey]).EndInclusive(*start)); err != nil {
			return nil, start, false, err
		}
	}
	return entries, start, truncated, nil
}

// drainStream closes up to budget leases of one exhausted stream, starting
// after the stream's cursor. With wrap it continues from the beginning once it
// reaches the end, so leases skipped earlier are retried. It returns the number
// of closures attempted, the number that succeeded, and the stream's cursor.
func (k *Keeper) drainStream(ctx context.Context, entry exhaustedStreamEntry, budget int, wrap bool, minLeaseDuration uint64) (attempts, closed int, cursor string) {
	key := entry.key
	cursor = entry.cursor
	providerUUID, tenant, denom := streamKeyParts(key)

	// Drop dangling queue entries: the stream is gone, or is no longer frozen.
	state, err := k.Streams.Get(ctx, key)
	if err != nil || !state.IsExhausted() {
		if err != nil && !errors.Is(err, collections.ErrNotFound) {
			k.logger.Error("failed to load exhausted stream", "error", err)
			return 0, 0, cursor
		}
		if rmErr := k.ExhaustedStreams.Remove(ctx, key); rmErr != nil {
			k.logger.Error("failed to remove dangling exhausted stream entry", "error", rmErr)
		}
		return 0, 0, cursor
	}

	leaseUUIDs, err := k.collectStreamLeases(ctx, key, cursor, budget, wrap)
	if err != nil {
		k.logger.Error("failed to collect leases of exhausted stream", "error", err)
		return 0, 0, cursor
	}

	// Nothing behind the cursor in a forward-only pass: the stream is served
	// again, from the beginning, in the next block.
	fullScan := wrap || cursor == ""
	if len(leaseUUIDs) == 0 && !fullScan {
		return 0, 0, cursor
	}

	// A frozen stream without any member lease has drifted from its leases:
	// nothing can ever drain it, so remove it rather than rescan it forever.
	if len(leaseUUIDs) == 0 {
		k.logger.Error("data inconsistency: exhausted stream has no leases to close",
			"provider_uuid", providerUUID,
			"tenant", tenant.String(),
			"denom", denom,
			"lease_count", state.LeaseCount,
		)
		if delErr := k.deleteStream(ctx, key); delErr != nil {
			k.logger.Error("failed to delete drifted stream", "error", delErr)
		}
		return 0, 0, cursor
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	lastFailed := ""
	for _, leaseUUID := range leaseUUIDs {
		attempts++

		// Each closure is atomic: the lease, its streams, the credit account
		// and the event commit together or not at all.
		cacheCtx, write := sdkCtx.CacheContext()
		if closeErr := k.closeExhaustedLease(cacheCtx, key, leaseUUID, minLeaseDuration); closeErr != nil {
			k.logger.Error("failed to auto-close lease of exhausted stream",
				"lease_uuid", leaseUUID,
				"provider_uuid", providerUUID,
				"tenant", tenant.String(),
				"error", closeErr,
			)
			lastFailed = leaseUUID
			continue
		}
		write()
		closed++
	}

	// Move the cursor past the last lease that failed, so the next block starts
	// with the leases behind it. The stream (and its queue entry) is deleted by
	// the closure that removes its last lease; do not resurrect the entry.
	if lastFailed != "" {
		cursor = lastFailed
		if has, hasErr := k.ExhaustedStreams.Has(ctx, key); hasErr == nil && has {
			if setErr := k.ExhaustedStreams.Set(ctx, key, lastFailed); setErr != nil {
				k.logger.Error("failed to advance exhausted stream cursor", "error", setErr)
			}
		}
	}

	return attempts, closed, cursor
}

// collectStreamLeases returns up to limit member leases of a stream, starting
// after cursor. With wrap it then continues from the beginning up to the
// cursor, so leases skipped by an earlier pass are retried once the ones behind
// them have been served.
func (k *Keeper) collectStreamLeases(ctx context.Context, key StreamKey, cursor string, limit int, wrap bool) ([]string, error) {
	collect := func(rng *collections.PairRange[StreamKey, string], out []string) ([]string, error) {
		iter, err := k.StreamLeases.Iterate(ctx, rng)
		if err != nil {
			return out, err
		}
		defer iter.Close()

		for ; iter.Valid() && len(out) < limit; iter.Next() {
			member, err := iter.Key()
			if err != nil {
				return out, err
			}
			out = append(out, member.K2())
		}
		return out, nil
	}

	forward := collections.NewPrefixedPairRange[StreamKey, string](key)
	if cursor != "" {
		forward = forward.StartExclusive(cursor)
	}
	leaseUUIDs, err := collect(forward, make([]string, 0, limit))
	if err != nil {
		return nil, err
	}

	if wrap && cursor != "" && len(leaseUUIDs) < limit {
		wrapped := collections.NewPrefixedPairRange[StreamKey, string](key).EndInclusive(cursor)
		leaseUUIDs, err = collect(wrapped, leaseUUIDs)
		if err != nil {
			return nil, err
		}
	}

	return leaseUUIDs, nil
}

// closeExhaustedLease closes one lease of an exhausted stream: it removes the
// lease from all its streams, marks it CLOSED for exhausted credit, and
// releases its reservation. Any panic is converted to an error so a single bad
// lease cannot halt the chain from the EndBlocker.
func (k *Keeper) closeExhaustedLease(ctx sdk.Context, key StreamKey, leaseUUID string, minLeaseDuration uint64) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic while auto-closing lease %s: %v", leaseUUID, r)
		}
	}()

	blockTime := ctx.BlockTime()

	lease, err := k.GetLease(ctx, leaseUUID)
	if err != nil && !errors.Is(err, types.ErrLeaseNotFound) {
		return err
	}
	if err != nil || lease.State != types.LEASE_STATE_ACTIVE {
		// The membership entry is stale: its lease is gone or no longer ACTIVE.
		// Remove the entry so the drain can make progress.
		k.logger.Error("data inconsistency: stream lists a lease that is not active",
			"lease_uuid", leaseUUID,
		)
		return k.StreamLeases.Remove(ctx, collections.Join(key, leaseUUID))
	}

	// Leave every stream the lease belongs to. The exhausted stream no longer
	// accrues; a sibling stream in another denom that is still healthy is
	// settled up to now before its rate drops.
	streams := k.newStreamCache(blockTime)
	if _, err := streams.leave(ctx, &lease); err != nil {
		return err
	}

	lease.State = types.LEASE_STATE_CLOSED
	lease.ClosedAt = &blockTime
	lease.ClosureReason = types.ClosureReasonCreditExhausted

	// SetLease reconciles the custom_domain reverse index from the lease's new
	// (terminal) state.
	if err := k.SetLease(ctx, lease); err != nil {
		return err
	}

	creditAccount, err := k.GetCreditAccount(ctx, lease.Tenant)
	if err != nil {
		return err
	}
	k.DecrementActiveLeaseCount(&creditAccount, lease.Uuid)
	k.ReleaseLeaseReservation(&creditAccount, &lease, minLeaseDuration)
	if err := k.SetCreditAccount(ctx, creditAccount); err != nil {
		return err
	}

	if err := streams.flush(ctx); err != nil {
		return err
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeLeaseAutoClose,
			sdk.NewAttribute(types.AttributeKeyLeaseUUID, lease.Uuid),
			sdk.NewAttribute(types.AttributeKeyTenant, lease.Tenant),
			sdk.NewAttribute(types.AttributeKeyProviderUUID, lease.ProviderUuid),
			sdk.NewAttribute(types.AttributeKeyReason, "credit_exhausted"),
		),
	)

	return nil
}

// GetAllStreams returns every stream in key order, in the genesis shape.
func (k *Keeper) GetAllStreams(ctx context.Context) ([]types.Stream, error) {
	streams := make([]types.Stream, 0)
	err := k.Streams.Walk(ctx, nil, func(key StreamKey, state types.StreamState) (bool, error) {
		streams = append(streams, streamFromState(key, state))
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	return streams, nil
}

// initGenesisStreams restores the streams from genesis and rebuilds the state
// derived from them and from the leases: the tenant reverse lookup, the drain
// queue (from exhausted_at) and the membership entries (from ACTIVE leases).
func (k *Keeper) initGenesisStreams(ctx context.Context, gs *types.GenesisState) error {
	for _, stream := range gs.Streams {
		tenant, err := sdk.AccAddressFromBech32(stream.Tenant)
		if err != nil {
			return types.ErrInvalidLease.Wrapf("stream %v has invalid tenant address: %s", stream.ID(), err)
		}
		key := NewStreamKey(stream.ProviderUuid, tenant, stream.Denom)

		if err := k.Streams.Set(ctx, key, stream.State); err != nil {
			return err
		}
		if err := k.StreamsByTenant.Set(ctx, streamTenantKey(key)); err != nil {
			return err
		}
		if stream.State.IsExhausted() {
			if err := k.ExhaustedStreams.Set(ctx, key, ""); err != nil {
				return err
			}
		}
	}

	for i := range gs.Leases {
		lease := &gs.Leases[i]
		if lease.State != types.LEASE_STATE_ACTIVE {
			continue
		}
		tenant, err := sdk.AccAddressFromBech32(lease.Tenant)
		if err != nil {
			return err
		}
		rates, err := types.LeaseRatesPerSecond(lease.Items)
		if err != nil {
			return types.ErrInvalidLease.Wrapf("lease %s: %s", lease.Uuid, err)
		}
		for _, rate := range rates {
			key := NewStreamKey(lease.ProviderUuid, tenant, rate.Denom)
			if err := k.StreamLeases.Set(ctx, collections.Join(key, lease.Uuid)); err != nil {
				return err
			}
		}
	}

	return nil
}
