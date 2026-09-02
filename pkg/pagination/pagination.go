// Package pagination provides generic pagination utilities for CosmosSDK collections.
//
// This package consolidates pagination helpers that are used across multiple modules
// to avoid code duplication and ensure consistent pagination behavior.
package pagination

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"

	"cosmossdk.io/collections"
	"cosmossdk.io/collections/indexes"

	"github.com/cosmos/cosmos-sdk/types/query"
)

const (
	// MaxPageLimit is the largest response page accepted by the shared module
	// query helpers. It bounds response allocation while remaining large enough
	// for bulk indexers.
	MaxPageLimit uint64 = 1000

	// MaxPageScanLimit is the largest number of index rows a filtered page may
	// inspect. A sparse filter can therefore return fewer than MaxPageLimit
	// values with a non-empty continuation cursor, but one unauthenticated query
	// can never force a full-index scan.
	MaxPageScanLimit uint64 = 1000

	// MaxOffsetCountTotalScanLimit is the largest number of physical collection
	// or index rows an offset or exact-total request may inspect. These legacy SDK
	// pagination modes necessarily scan from the beginning of a result set;
	// keeping their budget separate from MaxPageScanLimit preserves compatibility
	// for existing clients without making the work unbounded. A paginator may
	// read one additional key, without decoding its value, to produce next_key.
	MaxOffsetCountTotalScanLimit uint64 = 20_000
)

var (
	// ErrOffsetPaginationUnsupported is returned by cursor-only endpoints when a
	// request attempts an O(offset) front scan. Callers must use continuation
	// cursors on those endpoints.
	ErrOffsetPaginationUnsupported = errors.New("offset pagination is not supported; use pagination.key")

	// ErrCountTotalUnsupported is returned by cursor-only endpoints whose per-row
	// work makes even a separately bounded exact-total scan too expensive.
	ErrCountTotalUnsupported = errors.New("count_total is not supported; follow pagination.next_key")

	// ErrPageKeyAndOffset is returned when both mutually exclusive SDK
	// pagination positions are supplied.
	ErrPageKeyAndOffset = errors.New("invalid pagination: only one of key or offset may be set")

	// ErrPaginationScanLimitExceeded is returned when an offset or exact-total
	// request cannot be answered exactly within the compatibility scan ceiling.
	ErrPaginationScanLimitExceeded = errors.New("pagination scan limit exceeded")
)

// limitPageRequest returns a normalized defensive copy of pageReq with an
// explicit default response limit and a capped maximum. Supplying the default
// explicitly is important: the SDK pagination helper otherwise turns a zero
// limit into an implicit count_total request and scans the full collection.
// The caller-owned request and cursor bytes are never mutated.
func limitPageRequest(pageReq *query.PageRequest) *query.PageRequest {
	limited := query.PageRequest{Limit: query.DefaultLimit}
	if pageReq != nil {
		limited = *pageReq
		limited.Key = bytes.Clone(pageReq.Key)
	}
	if limited.Limit == 0 {
		limited.Limit = query.DefaultLimit
	}
	if limited.Limit > MaxPageLimit {
		limited.Limit = MaxPageLimit
	}
	return &limited
}

// BoundedPageRequest validates the SDK pagination contract and returns a
// normalized, capped defensive copy. Offset and count_total remain supported
// when no key is present; the pagination helpers enforce the compatibility scan
// ceiling against work actually performed. As in the SDK, count_total is
// ignored when key is set.
func BoundedPageRequest(pageReq *query.PageRequest) (*query.PageRequest, error) {
	limited := limitPageRequest(pageReq)
	if len(limited.Key) > 0 {
		if limited.Offset != 0 {
			return nil, ErrPageKeyAndOffset
		}
		limited.CountTotal = false
	}
	return limited, nil
}

// CursorPageRequest validates the bounded cursor-only pagination contract and
// returns a capped defensive copy. Public RPC handlers call this before opening
// an iterator so rejected requests perform no store scan.
func CursorPageRequest(pageReq *query.PageRequest) (*query.PageRequest, error) {
	if pageReq != nil && pageReq.Offset != 0 {
		return nil, ErrOffsetPaginationUnsupported
	}
	if pageReq != nil && pageReq.CountTotal {
		return nil, ErrCountTotalUnsupported
	}
	return BoundedPageRequest(pageReq)
}

// CollectionPaginate applies bounded SDK-compatible pagination to an
// unfiltered primary collection. It uses one iterator pass so offset and exact
// totals remain bounded by MaxOffsetCountTotalScanLimit. Unlike the SDK helper,
// a zero limit is normalized without implicitly enabling count_total.
func CollectionPaginate[K, V any, C query.Collection[K, V], T any](
	ctx context.Context,
	coll C,
	pageReq *query.PageRequest,
	transformFunc func(key K, value V) (T, error),
) (results []T, pageRes *query.PageResponse, err error) {
	bounded, err := BoundedPageRequest(pageReq)
	if err != nil {
		return nil, nil, err
	}

	order := collections.OrderAscending
	var start, end []byte
	if bounded.Reverse {
		order = collections.OrderDescending
		if len(bounded.Key) > 0 {
			// IterateRaw's end is exclusive. Appending a zero byte creates the
			// tight upper bound immediately after the cursor, so reverse resume is
			// inclusive without also admitting longer keys sharing its prefix.
			end = append(bytes.Clone(bounded.Key), 0)
		}
	} else if len(bounded.Key) > 0 {
		start = bounded.Key
	}

	iter, err := coll.IterateRaw(ctx, start, end, order)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if closeErr := iter.Close(); err == nil {
			err = closeErr
		}
	}()

	pageRes = new(query.PageResponse)
	hasKey := len(bounded.Key) > 0
	var scanned uint64

	for ; iter.Valid(); iter.Next() {
		if !hasKey && scanned == MaxOffsetCountTotalScanLimit {
			if bounded.CountTotal || uint64(len(results)) < bounded.Limit {
				return nil, nil, fmt.Errorf(
					"%w: request requires inspecting more than %d physical rows; use pagination.key",
					ErrPaginationScanLimitExceeded,
					MaxOffsetCountTotalScanLimit,
				)
			}
			key, keyErr := iter.Key()
			if keyErr != nil {
				return nil, nil, keyErr
			}
			pageRes.NextKey, err = collections.EncodeKeyWithPrefix(nil, coll.KeyCodec(), key)
			return results, pageRes, err
		}

		if uint64(len(results)) == bounded.Limit && len(pageRes.NextKey) == 0 {
			key, keyErr := iter.Key()
			if keyErr != nil {
				return nil, nil, keyErr
			}
			pageRes.NextKey, err = collections.EncodeKeyWithPrefix(nil, coll.KeyCodec(), key)
			if err != nil {
				return nil, nil, err
			}
			if hasKey || !bounded.CountTotal {
				return results, pageRes, nil
			}
		}

		scanned++
		if !hasKey && scanned <= bounded.Offset {
			continue
		}
		if uint64(len(results)) == bounded.Limit {
			// Only count_total reaches this branch: a page without an exact-total
			// request returned as soon as it captured next_key. Counting physical
			// rows does not require decoding potentially large stored values.
			continue
		}

		kv, kvErr := iter.KeyValue()
		if kvErr != nil {
			return nil, nil, kvErr
		}
		transformed, transformErr := transformFunc(kv.Key, kv.Value)
		if transformErr != nil {
			return nil, nil, transformErr
		}
		results = append(results, transformed)
	}

	if bounded.CountTotal {
		pageRes.Total = scanned
	}
	return results, pageRes, nil
}

// MatchExactWithOrder builds an index iterator for refKey. It applies descending
// order when pageReq.Reverse is set, and — when pageReq.Key is present — seeks the
// iterator to the resume cursor with a store-level bound. That seek is O(log n)
// and, unlike a front-scan, tolerant of the cursor row having been deleted between
// page calls (it resumes from the nearest surviving key). Cursor validation
// and response-limit normalization remain the responsibility of
// BoundedPageRequest or CursorPageRequest.
func MatchExactWithOrder[RK, V any](
	ctx context.Context,
	idx *indexes.Multi[RK, string, V],
	refKey RK,
	pageReq *query.PageRequest,
) (indexes.MultiIterator[RK, string], error) {
	rng := collections.NewPrefixedPairRange[RK, string](refKey)
	if pageReq != nil {
		// Resume by seeking, inclusively, to the cursor key. collections.PairRange's
		// Start* always binds the byte-order LOWER bound regardless of Descending(),
		// so reverse resume must bound the UPPER end (EndInclusive, which uses
		// RangeKeyNext) to land inclusively on the cursor and iterate strictly
		// downward from it. Inclusive bounds (never Exclusive) preserve the
		// first-unread + inclusive-resume next_key contract PaginateStringIndex
		// produces, byte-for-byte.
		if len(pageReq.Key) > 0 {
			cursor := string(pageReq.Key)
			if pageReq.Reverse {
				rng = rng.EndInclusive(cursor)
			} else {
				rng = rng.StartInclusive(cursor)
			}
		}
		if pageReq.Reverse {
			rng = rng.Descending()
		}
	}
	return idx.Iterate(ctx, rng)
}

// StringIndexIterator is an interface for index iterators that return string primary keys.
type StringIndexIterator interface {
	Valid() bool
	Next()
	Close() error
	PrimaryKey() (string, error)
}

type stringIndexPaginationOptions struct {
	has func(context.Context, string) (bool, error)
}

// StringIndexPaginationOption configures secondary-index pagination.
type StringIndexPaginationOption func(*stringIndexPaginationOptions)

// WithStringIndexHas supplies a cheap primary-key existence check. For
// unfiltered offset/count_total requests, the paginator uses it to preserve
// orphan-index handling without decoding large values outside the response
// page. Existing callers may omit the option and retain the decode-based path.
func WithStringIndexHas(has func(context.Context, string) (bool, error)) StringIndexPaginationOption {
	return func(options *stringIndexPaginationOptions) {
		options.has = has
	}
}

// PaginateStringIndex paginates over a string index iterator, fetching values from the primary map.
// This is a helper for paginating secondary indexes on IndexedMap collections with string primary keys.
//
// Parameters:
//   - ctx: context
//   - iter: the index iterator (from MatchExact or similar)
//   - getter: function to fetch values by primary key
//   - pageReq: pagination request
//   - filter: optional filter function, return true to include the value (nil means include all)
//
// Returns paginated values, page response, and any error.
func PaginateStringIndex[V any](
	ctx context.Context,
	iter StringIndexIterator,
	getter func(context.Context, string) (V, error),
	pageReq *query.PageRequest,
	filter func(V) bool,
	options ...StringIndexPaginationOption,
) (values []V, pageResponse *query.PageResponse, err error) {
	var orphaned uint64
	defer func() {
		if orphaned > 0 {
			slog.Warn("index references non-existent primary keys, skipping orphaned entries",
				"count", orphaned,
			)
		}
		if closeErr := iter.Close(); err == nil {
			err = closeErr
		}
	}()

	pageReq, err = BoundedPageRequest(pageReq)
	if err != nil {
		return nil, nil, err
	}

	limit := pageReq.Limit
	hasKey := len(pageReq.Key) > 0
	compatibilityMode := !hasKey && (pageReq.Offset != 0 || pageReq.CountTotal)
	scanLimit := MaxPageScanLimit
	if compatibilityMode {
		scanLimit = MaxOffsetCountTotalScanLimit
	}
	var paginationOptions stringIndexPaginationOptions
	for _, option := range options {
		if option != nil {
			option(&paginationOptions)
		}
	}
	useExistenceCheck := compatibilityMode && filter == nil && paginationOptions.has != nil

	var count uint64
	var skipped uint64
	var total uint64
	var nextKey []byte

	// The iterator is already positioned at the resume point by MatchExactWithOrder
	// (a store-level seek on pageReq.Key), so no front-scan is needed here.
	var scanned uint64

	for ; iter.Valid(); iter.Next() {
		if scanned >= scanLimit {
			if pageReq.CountTotal || (pageReq.Offset != 0 && count < limit) {
				return nil, nil, fmt.Errorf(
					"%w: request requires inspecting more than %d physical rows; use pagination.key",
					ErrPaginationScanLimitExceeded,
					scanLimit,
				)
			}
			pk, keyErr := iter.PrimaryKey()
			if keyErr != nil {
				return nil, nil, keyErr
			}
			nextKey = []byte(pk)
			break
		}

		pk, keyErr := iter.PrimaryKey()
		if keyErr != nil {
			return nil, nil, keyErr
		}
		scanned++

		var value V
		valueLoaded := false
		if useExistenceCheck {
			exists, hasErr := paginationOptions.has(ctx, pk)
			if hasErr != nil {
				return nil, nil, hasErr
			}
			if !exists {
				orphaned++
				continue
			}
		} else {
			value, err = getter(ctx, pk)
			if err != nil {
				if errors.Is(err, collections.ErrNotFound) {
					// Index references a key that no longer exists (index inconsistency).
					// Skip the entry rather than failing the entire query.
					orphaned++
					continue
				}
				return nil, nil, err
			}
			valueLoaded = true
		}

		// Apply filter if provided
		if filter != nil && !filter(value) {
			continue
		}
		if pageReq.CountTotal {
			total++
		}
		if !hasKey && skipped < pageReq.Offset {
			skipped++
			continue
		}

		// Check if we've reached the limit
		if count >= limit {
			if len(nextKey) == 0 {
				nextKey = []byte(pk)
			}
			if !pageReq.CountTotal {
				break
			}
			continue
		}

		if !valueLoaded {
			value, err = getter(ctx, pk)
			if err != nil {
				return nil, nil, err
			}
		}
		values = append(values, value)
		count++
	}

	pageRes := &query.PageResponse{NextKey: nextKey}
	if pageReq.CountTotal {
		pageRes.Total = total
	}
	return values, pageRes, nil
}
