package keeper

import (
	"bytes"
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
)

// Uint64IndexIterator is an interface for index iterators that return uint64 primary keys.
type Uint64IndexIterator interface {
	Valid() bool
	Next()
	Close() error
	PrimaryKey() (uint64, error)
}

// PaginateUint64Index paginates over a uint64 index iterator, fetching values from the primary map.
// This is a helper for paginating secondary indexes on IndexedMap collections with uint64 primary keys.
//
// Parameters:
//   - ctx: context
//   - iter: the index iterator (from MatchExact or similar)
//   - getter: function to fetch values by primary key
//   - pageReq: pagination request
//   - filter: optional filter function, return true to include the value (nil means include all)
//
// Returns paginated values, page response, and any error.
func PaginateUint64Index[V any](
	ctx context.Context,
	iter Uint64IndexIterator,
	getter func(context.Context, uint64) (V, error),
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

	var values []V
	var count uint64
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
			pkBytes := sdk.Uint64ToBigEndian(pk)
			if bytes.Equal(pkBytes, startKey) {
				foundStart = true
			} else {
				continue
			}
		}

		value, err := getter(ctx, pk)
		if err != nil {
			// Skip if value not found (index inconsistency - shouldn't happen)
			continue
		}

		// Apply filter if provided
		if filter != nil && !filter(value) {
			continue
		}

		// Handle offset-based pagination (only applies if no key provided)
		if len(startKey) == 0 && skipped < offset {
			skipped++
			continue
		}

		// Check if we've reached the limit
		if count >= limit {
			// Encode the primary key for next_key
			nextKey = sdk.Uint64ToBigEndian(pk)
			break
		}

		values = append(values, value)
		count++
	}

	return values, &query.PageResponse{NextKey: nextKey}, nil
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

	var values []V
	var count uint64
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
			// Skip if value not found (index inconsistency - shouldn't happen)
			continue
		}

		// Apply filter if provided
		if filter != nil && !filter(value) {
			continue
		}

		// Handle offset-based pagination (only applies if no key provided)
		if len(startKey) == 0 && skipped < offset {
			skipped++
			continue
		}

		// Check if we've reached the limit
		if count >= limit {
			// Encode the primary key for next_key
			nextKey = []byte(pk)
			break
		}

		values = append(values, value)
		count++
	}

	return values, &query.PageResponse{NextKey: nextKey}, nil
}
