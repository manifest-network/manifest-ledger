// Package pagination provides generic pagination utilities for CosmosSDK collections.
//
// This package consolidates pagination helpers that are used across multiple modules
// to avoid code duplication and ensure consistent pagination behavior.
package pagination

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	"cosmossdk.io/collections/indexes"

	"github.com/cosmos/cosmos-sdk/types/query"
)

// MatchExactWithOrder is like indexes.Multi.MatchExact but applies descending
// iteration order when pageReq.Reverse is true.
// It only controls iteration direction; limit, offset, and key-based cursor
// pagination are handled by PaginateStringIndex (or similar) downstream.
func MatchExactWithOrder[RK, V any](
	ctx context.Context,
	idx *indexes.Multi[RK, string, V],
	refKey RK,
	pageReq *query.PageRequest,
) (indexes.MultiIterator[RK, string], error) {
	rng := collections.NewPrefixedPairRange[RK, string](refKey)
	if pageReq != nil && pageReq.Reverse {
		rng = rng.Descending()
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

	// Handle key-based pagination: skip items until we reach the start key
	startKey := pageReq.Key
	foundStart := len(startKey) == 0 // If no start key, we've already "found" it

	// Handle offset-based pagination
	offset := pageReq.Offset
	var skipped uint64

	for ; iter.Valid(); iter.Next() {
		pk, err := iter.PrimaryKey()
		if err != nil {
			return nil, nil, err
		}

		// For key-based pagination, skip until we reach the start key
		if !foundStart {
			if string(startKey) == pk {
				foundStart = true
			} else {
				continue
			}
		}

		value, err := getter(ctx, pk)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				// Index references a key that no longer exists (index inconsistency).
				// Skip the entry rather than failing the entire query.
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
		if len(startKey) == 0 && skipped < offset {
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
