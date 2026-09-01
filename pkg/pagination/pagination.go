// Package pagination provides generic pagination utilities for CosmosSDK collections.
//
// This package consolidates pagination helpers that are used across multiple modules
// to avoid code duplication and ensure consistent pagination behavior.
package pagination

import (
	"bytes"
	"context"
	"errors"
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
)

var (
	// ErrOffsetPaginationUnsupported is returned for public list requests that
	// attempt an O(offset) front scan. Callers must use continuation cursors.
	ErrOffsetPaginationUnsupported = errors.New("offset pagination is not supported; use pagination.key")

	// ErrCountTotalUnsupported is returned because an exact total requires an
	// unbounded scan on the collection and filtered-index query paths.
	ErrCountTotalUnsupported = errors.New("count_total is not supported; follow pagination.next_key")
)

// limitPageRequest returns a defensive copy of pageReq with its response limit
// capped. The caller-owned request and cursor bytes are never mutated.
func limitPageRequest(pageReq *query.PageRequest) *query.PageRequest {
	if pageReq == nil {
		return nil
	}
	limited := *pageReq
	limited.Key = bytes.Clone(pageReq.Key)
	if limited.Limit > MaxPageLimit {
		limited.Limit = MaxPageLimit
	}
	return &limited
}

// CursorPageRequest validates the bounded cursor-only pagination contract and
// returns a capped defensive copy. Public RPC handlers call this before opening
// an iterator so rejected requests perform no store scan.
func CursorPageRequest(pageReq *query.PageRequest) (*query.PageRequest, error) {
	if pageReq == nil {
		return nil, nil
	}
	if pageReq.Offset != 0 {
		return nil, ErrOffsetPaginationUnsupported
	}
	if pageReq.CountTotal {
		return nil, ErrCountTotalUnsupported
	}
	return limitPageRequest(pageReq), nil
}

// MatchExactWithOrder builds an index iterator for refKey. It applies descending
// order when pageReq.Reverse is set, and — when pageReq.Key is present — seeks the
// iterator to the resume cursor with a store-level bound. That seek is O(log n)
// and, unlike a front-scan, tolerant of the cursor row having been deleted between
// page calls (it resumes from the nearest surviving key). Cursor validation
// and response-limit normalization remain the responsibility of
// CursorPageRequest.
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
) (values []V, pageResponse *query.PageResponse, err error) {
	defer func() {
		if closeErr := iter.Close(); err == nil {
			err = closeErr
		}
	}()

	pageReq, err = CursorPageRequest(pageReq)
	if err != nil {
		return nil, nil, err
	}

	// Initialize pagination defaults
	if pageReq == nil {
		pageReq = &query.PageRequest{}
	}
	limit := pageReq.Limit
	if limit == 0 {
		limit = query.DefaultLimit
	}

	var count uint64
	var nextKey []byte

	// The iterator is already positioned at the resume point by MatchExactWithOrder
	// (a store-level seek on pageReq.Key), so no front-scan is needed here.
	var scanned uint64

	for ; iter.Valid(); iter.Next() {
		pk, err := iter.PrimaryKey()
		if err != nil {
			return nil, nil, err
		}
		if scanned >= MaxPageScanLimit {
			nextKey = []byte(pk)
			break
		}
		scanned++

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

		// Check if we've reached the limit
		if count >= limit {
			nextKey = []byte(pk)
			break
		}

		values = append(values, value)
		count++
	}

	return values, &query.PageResponse{NextKey: nextKey}, nil
}
