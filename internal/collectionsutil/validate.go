// Package collectionsutil provides deterministic diagnostic traversal helpers
// for Cosmos SDK collections.
package collectionsutil

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/collections/indexes"
)

// ValidateMap scans a collection in its iterator order and validates every
// decoded row. Keys are decoded before values so a corrupt value can be
// attributed to its exact key. formatKey is required so callers render typed
// keys semantically instead of relying on implementation-specific formatting.
func ValidateMap[Key, Value any](
	ctx context.Context,
	name string,
	iterate func(
		context.Context,
		collections.Ranger[Key],
	) (collections.Iterator[Key, Value], error),
	formatKey func(Key) string,
	validate func(Key, Value) error,
) (uint64, error) {
	iterator, err := iterate(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("open %s iterator: %w", name, err)
	}

	var count uint64
	var validationErr error
	for ; iterator.Valid(); iterator.Next() {
		key, err := iterator.Key()
		if err != nil {
			validationErr = fmt.Errorf(
				"%s row %d: decode key: %w",
				name,
				count+1,
				err,
			)
			break
		}
		value, err := iterator.Value()
		if err != nil {
			validationErr = fmt.Errorf(
				"%s row %s: decode value: %w",
				name,
				formatKey(key),
				err,
			)
			break
		}
		if err := validate(key, value); err != nil {
			validationErr = err
			break
		}
		count++
	}

	closeErr := iterator.Close()
	if validationErr != nil {
		return count, validationErr
	}
	if closeErr != nil {
		return count, fmt.Errorf("close %s iterator: %w", name, closeErr)
	}
	return count, nil
}

// ValidateMultiIndex validates every decoded row in a Collections Multi index.
// Errors raised by validate already contain entity context and are preserved;
// traversal errors raised outside the callback are attributed to the index.
// Collections v0.4's public Multi iterator does not expose its KeySet values,
// so Walk is required here to keep malformed non-empty index markers visible;
// Walk itself does not report iterator-close errors in that SDK version.
func ValidateMultiIndex[ReferenceKey, PrimaryKey, Value any](
	ctx context.Context,
	name string,
	index *indexes.Multi[ReferenceKey, PrimaryKey, Value],
	validate func(ReferenceKey, PrimaryKey) error,
) error {
	var validationErr error
	err := index.Walk(ctx, nil, func(reference ReferenceKey, primary PrimaryKey) (bool, error) {
		validationErr = validate(reference, primary)
		return validationErr != nil, validationErr
	})
	if validationErr != nil {
		return validationErr
	}
	if err != nil {
		return fmt.Errorf("walk %s index: %w", name, err)
	}
	return nil
}
