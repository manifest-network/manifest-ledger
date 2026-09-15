package collectionsutil

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	"cosmossdk.io/collections/indexes"
)

// MultiIndexContains checks membership using an exact (reference, primary)
// range. Collections v0.4 does not expose Multi's backing KeySet.Has; using
// MatchExact would scan every primary sharing the reference. This checks only
// membership: use ValidateMultiIndex to validate the stored index markers.
func MultiIndexContains[ReferenceKey, PrimaryKey, Value any](
	ctx context.Context,
	index *indexes.Multi[ReferenceKey, PrimaryKey, Value],
	reference ReferenceKey,
	primary PrimaryKey,
) (found bool, err error) {
	key := collections.Join(reference, primary)
	rangeKey := new(collections.Range[collections.Pair[ReferenceKey, PrimaryKey]]).
		StartInclusive(key).EndInclusive(key)
	iterator, err := index.Iterate(ctx, rangeKey)
	if err != nil {
		return false, err
	}
	defer func() {
		err = errors.Join(err, iterator.Close())
	}()
	return iterator.Valid(), nil
}
