// Package pagination provides generic pagination utilities for CosmosSDK collections.
//
// This package consolidates pagination helpers that are used across multiple modules
// to avoid code duplication and ensure consistent pagination behavior.
package pagination

import (
	"context"
	"errors"
	"log/slog"

	"cosmossdk.io/collections"
	"cosmossdk.io/collections/indexes"

	"github.com/cosmos/cosmos-sdk/types/query"
)

// MatchExactWithOrder builds an index iterator for refKey. It applies descending
// order when pageReq.Reverse is set, and — when pageReq.Key is present — seeks the
// iterator to the resume cursor with a store-level bound. That seek is O(log n)
// and, unlike a front-scan, tolerant of the cursor row having been deleted between
// page calls (it resumes from the nearest surviving key). limit, offset, and
// count_total remain the responsibility of PaginateStringIndex downstream.
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
) ([]V, *query.PageResponse, error) {
	defer iter.Close()

	// Initialize pagination defaults
	if pageReq == nil {
		pageReq = &query.PageRequest{}
	}
	limit := pageReq.Limit
	if limit == 0 {
		limit = query.DefaultLimit
	}

	countTotal := pageReq.CountTotal

	var values []V
	var count uint64
	var total uint64
	var nextKey []byte

	// The iterator is already positioned at the resume point by MatchExactWithOrder
	// (a store-level seek on pageReq.Key), so no front-scan is needed here. Offset
	// applies only when no cursor key is set (matching prior behavior).
	hasKey := len(pageReq.Key) > 0
	offset := pageReq.Offset
	var skipped uint64

	for ; iter.Valid(); iter.Next() {
		pk, err := iter.PrimaryKey()
		if err != nil {
			return nil, nil, err
		}

		value, err := getter(ctx, pk)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				// Index references a key that no longer exists (index inconsistency).
				// Skip the entry rather than failing the entire query.
				slog.Warn("index references non-existent key, skipping orphaned entry",
					"primary_key", pk,
				)
				continue
			}
			return nil, nil, err
		}

		// Apply filter if provided
		if filter != nil && !filter(value) {
			continue
		}

		// Count for total (if requested)
		if countTotal {
			total++
		}

		// Handle offset-based pagination (only applies if no key provided)
		if !hasKey && skipped < offset {
			skipped++
			continue
		}

		// Check if we've reached the limit
		if count >= limit {
			if len(nextKey) == 0 {
				nextKey = []byte(pk)
			}
			if !countTotal {
				break
			}
			continue
		}

		values = append(values, value)
		count++
	}

	pageRes := &query.PageResponse{NextKey: nextKey}
	if countTotal {
		pageRes.Total = total
	}

	return values, pageRes, nil
}
