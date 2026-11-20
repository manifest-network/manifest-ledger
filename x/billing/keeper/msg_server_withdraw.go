package keeper

import (
	"context"
	"strconv"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// withdrawCursorCodec encodes the pagination cursor of a provider withdrawal:
// the (tenant, denom) part of the last stream key returned. The provider part
// is never taken from the cursor; it always comes from the message, so a
// cursor cannot move the iteration into another provider's streams.
var withdrawCursorCodec = collections.PairKeyCodec(sdk.AccAddressKey, collections.StringKey)

func encodeWithdrawCursor(key StreamKey) ([]byte, error) {
	cursor := key.K2()
	buf := make([]byte, withdrawCursorCodec.Size(cursor))
	if _, err := withdrawCursorCodec.Encode(buf, cursor); err != nil {
		return nil, err
	}
	return buf, nil
}

func decodeWithdrawCursor(bz []byte) (collections.Pair[sdk.AccAddress, string], error) {
	read, cursor, err := withdrawCursorCodec.Decode(bz)
	if err != nil {
		return cursor, types.ErrInvalidRequest.Wrapf("invalid key (pagination cursor): %s", err)
	}
	if read != len(bz) {
		return cursor, types.ErrInvalidRequest.Wrap("invalid key (pagination cursor): trailing bytes")
	}
	return cursor, nil
}

// Withdraw settles a provider's streams, paying what they accrued to the
// provider's payout address.
//
// Accrual is settled per (provider, tenant, denom) stream, so the cost is
// proportional to the number of streams and independent of how many leases
// they aggregate. Streams are processed in pages of msg.Limit; each settles in
// its own CacheContext, so one failing stream is skipped without affecting the
// others.
func (ms msgServer) Withdraw(ctx context.Context, msg *types.MsgWithdraw) (*types.MsgWithdrawResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime()

	// Get provider and verify authorization
	providerUUID := msg.ProviderUuid
	provider, err := ms.validateProviderAuthorization(ctx, msg.Sender, providerUUID, "withdraw from")
	if err != nil {
		return nil, err
	}

	// Apply default limit if not specified (limit=0 means use default, not unlimited)
	limit := msg.Limit
	if limit == 0 {
		limit = types.DefaultProviderWithdrawLimit
	}

	// Range over this provider's streams only. The cursor is the (tenant, denom)
	// of the last stream of the previous page; StartExclusive seeks past it at
	// the store level and tolerates that stream having been deleted since.
	rng := collections.NewPrefixedPairRange[string, collections.Pair[sdk.AccAddress, string]](providerUUID)
	if len(msg.Key) > 0 {
		cursor, err := decodeWithdrawCursor(msg.Key)
		if err != nil {
			return nil, err
		}
		rng = rng.StartExclusive(cursor)
	}

	// Two passes: collect the stream keys, close the iterator, then settle.
	// Settling writes to the collection being iterated.
	iter, err := ms.k.Streams.Iterate(ctx, rng)
	if err != nil {
		return nil, err
	}

	var keys []StreamKey
	hasMore := false
	for ; iter.Valid(); iter.Next() {
		if uint64(len(keys)) >= limit {
			hasMore = true
			break
		}
		key, keyErr := iter.Key()
		if keyErr != nil {
			if closeErr := iter.Close(); closeErr != nil {
				ms.k.Logger().Error("failed to close provider withdraw iterator", "error", closeErr)
			}
			return nil, keyErr
		}
		keys = append(keys, key)
	}
	if closeErr := iter.Close(); closeErr != nil {
		ms.k.Logger().Error("failed to close provider withdraw iterator", "error", closeErr)
	}

	// next_key is the last collected stream; the next call resumes after it.
	var nextKey []byte
	if hasMore {
		nextKey, err = encodeWithdrawCursor(keys[len(keys)-1])
		if err != nil {
			return nil, err
		}
	}

	totalAmounts := sdk.NewCoins()
	var withdrawalCount, exhaustedCount uint64

	for _, key := range keys {
		// Settle each stream atomically: the bank transfer, the stream update
		// and (on exhaustion) the drain queue entry commit together or not at all.
		cacheCtx, write := sdkCtx.CacheContext()

		settlement, settleErr := ms.k.SettleStream(cacheCtx, key, blockTime)
		if settleErr != nil {
			_, tenant, denom := streamKeyParts(key)
			ms.k.Logger().Error("failed to settle stream",
				"provider_uuid", providerUUID,
				"tenant", tenant.String(),
				"denom", denom,
				"error", settleErr,
			)
			continue
		}
		write()

		if settlement.Transferred.IsPositive() {
			totalAmounts = totalAmounts.Add(settlement.Transferred)
			withdrawalCount++
		}
		if settlement.Froze {
			exhaustedCount++
		}
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeBatchWithdraw,
			sdk.NewAttribute(types.AttributeKeyProviderUUID, providerUUID),
			sdk.NewAttribute(types.AttributeKeyAmount, totalAmounts.String()),
			sdk.NewAttribute(types.AttributeKeyStreamCount, strconv.FormatUint(withdrawalCount, 10)),
			sdk.NewAttribute(types.AttributeKeyPayoutAddress, provider.GetPayoutAddress()),
			sdk.NewAttribute(types.AttributeKeyExhaustedCount, strconv.FormatUint(exhaustedCount, 10)),
		),
	)

	return &types.MsgWithdrawResponse{
		TotalAmounts:    totalAmounts,
		PayoutAddress:   provider.GetPayoutAddress(),
		WithdrawalCount: withdrawalCount,
		HasMore:         hasMore,
		NextKey:         nextKey,
	}, nil
}
