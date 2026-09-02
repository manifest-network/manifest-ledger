package pagination

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	"cosmossdk.io/collections/colltest"
	"cosmossdk.io/collections/indexes"

	"github.com/cosmos/cosmos-sdk/types/query"
)

const filterYes = "yes"

// mockIterator implements StringIndexIterator for testing.
type mockIterator struct {
	keys     []string
	pos      int
	closed   bool
	closeErr error
}

func newMockIterator(keys []string) *mockIterator {
	return &mockIterator{keys: keys}
}

func (m *mockIterator) Valid() bool {
	return m.pos < len(m.keys) && !m.closed
}

func (m *mockIterator) Next() {
	m.pos++
}

func (m *mockIterator) Close() error {
	m.closed = true
	return m.closeErr
}

func (m *mockIterator) PrimaryKey() (string, error) {
	if m.pos >= len(m.keys) {
		return "", fmt.Errorf("iterator exhausted")
	}
	return m.keys[m.pos], nil
}

// mockGetter returns a getter function backed by a map.
func mockGetter(data map[string]string) func(context.Context, string) (string, error) {
	return func(_ context.Context, key string) (string, error) {
		v, ok := data[key]
		if !ok {
			return "", collections.ErrNotFound
		}
		return v, nil
	}
}

func TestPaginateStringIndex_DefaultLimit(t *testing.T) {
	// When pageReq is nil, should use default limit
	keys := make([]string, 200)
	data := make(map[string]string, 200)
	for i := range 200 {
		k := fmt.Sprintf("key-%03d", i)
		keys[i] = k
		data[k] = fmt.Sprintf("val-%03d", i)
	}

	iter := newMockIterator(keys)
	values, pageResp, err := PaginateStringIndex(
		context.Background(),
		iter,
		mockGetter(data),
		nil, // nil pageReq
		nil, // no filter
	)

	require.NoError(t, err)
	require.Len(t, values, int(query.DefaultLimit))
	require.NotNil(t, pageResp)
	require.NotEmpty(t, pageResp.NextKey)
	require.True(t, iter.closed)
}

func TestPaginateStringIndex_ExplicitLimit(t *testing.T) {
	keys := []string{"a", "b", "c", "d", "e"}
	data := map[string]string{"a": "1", "b": "2", "c": "3", "d": "4", "e": "5"}

	iter := newMockIterator(keys)
	values, pageResp, err := PaginateStringIndex(
		context.Background(),
		iter,
		mockGetter(data),
		&query.PageRequest{Limit: 3},
		nil,
	)

	require.NoError(t, err)
	require.Len(t, values, 3)
	require.Equal(t, []string{"1", "2", "3"}, values)
	require.Equal(t, []byte("d"), pageResp.NextKey)
}

func TestLimitPageRequest_CapsWithoutMutatingCaller(t *testing.T) {
	original := &query.PageRequest{
		Key:        []byte("cursor"),
		Offset:     7,
		Limit:      MaxPageLimit + 1,
		CountTotal: true,
		Reverse:    true,
	}

	limited := limitPageRequest(original)
	require.NotSame(t, original, limited)
	require.Equal(t, MaxPageLimit, limited.Limit)
	require.Equal(t, original.Offset, limited.Offset)
	require.Equal(t, original.CountTotal, limited.CountTotal)
	require.Equal(t, original.Reverse, limited.Reverse)
	require.Equal(t, original.Key, limited.Key)

	limited.Key[0] = 'X'
	require.Equal(t, []byte("cursor"), original.Key)
	require.Equal(t, MaxPageLimit+1, original.Limit)
	require.Equal(t, &query.PageRequest{Limit: query.DefaultLimit}, limitPageRequest(nil))
}

func TestBoundedPageRequest_SDKCompatibilityAndBounds(t *testing.T) {
	t.Run("nil and zero limits are explicit without implicit totals", func(t *testing.T) {
		for _, pageReq := range []*query.PageRequest{nil, {}} {
			bounded, err := BoundedPageRequest(pageReq)
			require.NoError(t, err)
			require.Equal(t, uint64(query.DefaultLimit), bounded.Limit)
			require.False(t, bounded.CountTotal)
		}
	})

	t.Run("key and offset are mutually exclusive", func(t *testing.T) {
		_, err := BoundedPageRequest(&query.PageRequest{Key: []byte("cursor"), Offset: 1})
		require.ErrorIs(t, err, ErrPageKeyAndOffset)
	})

	t.Run("count total is ignored with a key", func(t *testing.T) {
		original := &query.PageRequest{Key: []byte("cursor"), Limit: 2, CountTotal: true}
		bounded, err := BoundedPageRequest(original)
		require.NoError(t, err)
		require.False(t, bounded.CountTotal)
		require.True(t, original.CountTotal, "the caller-owned request must not be mutated")
	})

	t.Run("large offset window is checked against actual work by paginator", func(t *testing.T) {
		bounded, err := BoundedPageRequest(&query.PageRequest{
			Offset: MaxOffsetCountTotalScanLimit,
			Limit:  1,
		})
		require.NoError(t, err)
		require.Equal(t, MaxOffsetCountTotalScanLimit, bounded.Offset)
	})

	t.Run("exact total bounds the scan when offset plus limit crosses the ceiling", func(t *testing.T) {
		bounded, err := BoundedPageRequest(&query.PageRequest{
			Offset:     MaxOffsetCountTotalScanLimit - 500,
			Limit:      1000,
			CountTotal: true,
		})
		require.NoError(t, err)
		require.Equal(t, MaxOffsetCountTotalScanLimit-500, bounded.Offset)
		require.True(t, bounded.CountTotal)
	})
}

func TestCursorPageRequest_RejectsUnboundedModes(t *testing.T) {
	_, err := CursorPageRequest(&query.PageRequest{Offset: 1})
	require.ErrorIs(t, err, ErrOffsetPaginationUnsupported)

	_, err = CursorPageRequest(&query.PageRequest{CountTotal: true})
	require.ErrorIs(t, err, ErrCountTotalUnsupported)

	original := &query.PageRequest{Key: []byte("cursor"), Limit: MaxPageLimit + 1, Reverse: true}
	bounded, err := CursorPageRequest(original)
	require.NoError(t, err)
	require.Equal(t, MaxPageLimit, bounded.Limit)
	require.Equal(t, original.Key, bounded.Key)
	require.Equal(t, original.Reverse, bounded.Reverse)
	require.NotSame(t, original, bounded)
}

func TestPaginateStringIndex_CapsOversizedLimit(t *testing.T) {
	keyCount := int(MaxPageLimit + 2)
	keys := make([]string, keyCount)
	data := make(map[string]string, keyCount)
	for i := range keyCount {
		key := fmt.Sprintf("key-%04d", i)
		keys[i] = key
		data[key] = key
	}

	values, pageResp, err := PaginateStringIndex(
		context.Background(),
		newMockIterator(keys),
		mockGetter(data),
		&query.PageRequest{Limit: MaxPageLimit + 1},
		nil,
	)
	require.NoError(t, err)
	require.Len(t, values, int(MaxPageLimit))
	require.Equal(t, []byte("key-1000"), pageResp.NextKey)
}

func TestPaginateStringIndex_KeyBasedCursor(t *testing.T) {
	// MatchExactWithOrder now seeks the iterator to the cursor, so PaginateStringIndex
	// receives an ALREADY-POSITIONED iterator (here, one that starts at "c"). The Key
	// field only signals "a cursor is in play" (which disables offset); it no longer
	// drives a front-scan inside this function.
	iter := newMockIterator([]string{"c", "d", "e"})
	data := map[string]string{"c": "3", "d": "4", "e": "5"}
	values, pageResp, err := PaginateStringIndex(
		context.Background(),
		iter,
		mockGetter(data),
		&query.PageRequest{Key: []byte("c"), Limit: 2},
		nil,
	)

	require.NoError(t, err)
	require.Len(t, values, 2)
	require.Equal(t, []string{"3", "4"}, values)
	require.Equal(t, []byte("e"), pageResp.NextKey)
}

func TestPaginateStringIndex_KeyBasedCursorLastPage(t *testing.T) {
	// Pre-positioned iterator starting at "d" (as MatchExactWithOrder would seek it),
	// limit 5 (more than remaining) -> last page, empty NextKey.
	iter := newMockIterator([]string{"d", "e"})
	data := map[string]string{"d": "4", "e": "5"}
	values, pageResp, err := PaginateStringIndex(
		context.Background(),
		iter,
		mockGetter(data),
		&query.PageRequest{Key: []byte("d"), Limit: 5},
		nil,
	)

	require.NoError(t, err)
	require.Len(t, values, 2)
	require.Equal(t, []string{"4", "5"}, values)
	require.Empty(t, pageResp.NextKey, "last page should have empty NextKey")
}

func TestPaginateStringIndex_OffsetPagination(t *testing.T) {
	keys := []string{"a", "b", "c", "d", "e"}
	data := map[string]string{"a": "1", "b": "2", "c": "3", "d": "4", "e": "5"}

	iter := newMockIterator(keys)
	values, pageRes, err := PaginateStringIndex(
		context.Background(),
		iter,
		mockGetter(data),
		&query.PageRequest{Offset: 2, Limit: 2},
		nil,
	)

	require.NoError(t, err)
	require.Equal(t, []string{"3", "4"}, values)
	require.Equal(t, []byte("e"), pageRes.NextKey)
	require.True(t, iter.closed)
}

func TestPaginateStringIndex_FilterFunction(t *testing.T) {
	keys := []string{"a", "b", "c", "d", "e"}
	data := map[string]string{"a": "odd", "b": "even", "c": "odd", "d": "even", "e": "odd"}

	// Filter to include only "odd" values
	iter := newMockIterator(keys)
	values, pageResp, err := PaginateStringIndex(
		context.Background(),
		iter,
		mockGetter(data),
		&query.PageRequest{Limit: 10},
		func(v string) bool { return v == "odd" },
	)

	require.NoError(t, err)
	require.Len(t, values, 3)
	require.Equal(t, []string{"odd", "odd", "odd"}, values)
	require.Empty(t, pageResp.NextKey)
}

func TestPaginateStringIndex_FilterWithLimit(t *testing.T) {
	keys := []string{"a", "b", "c", "d", "e", "f"}
	data := map[string]string{"a": "yes", "b": "no", "c": "yes", "d": "no", "e": "yes", "f": "yes"}

	// Filter "yes" with limit 2
	iter := newMockIterator(keys)
	values, pageResp, err := PaginateStringIndex(
		context.Background(),
		iter,
		mockGetter(data),
		&query.PageRequest{Limit: 2},
		func(v string) bool { return v == filterYes },
	)

	require.NoError(t, err)
	require.Len(t, values, 2)
	require.Equal(t, []string{"yes", "yes"}, values)
	// NextKey should point to "e" (next matching key after limit reached)
	require.NotEmpty(t, pageResp.NextKey)
}

func TestPaginateStringIndex_IndexInconsistencySkip(t *testing.T) {
	// Simulate index referencing a key that no longer exists
	keys := []string{"a", "b", "deleted", "c"}
	data := map[string]string{"a": "1", "b": "2", "c": "3"} // "deleted" not in data

	iter := newMockIterator(keys)
	values, pageResp, err := PaginateStringIndex(
		context.Background(),
		iter,
		mockGetter(data),
		&query.PageRequest{Limit: 10},
		nil,
	)

	require.NoError(t, err)
	require.Len(t, values, 3, "should skip the deleted entry")
	require.Equal(t, []string{"1", "2", "3"}, values)
	require.Empty(t, pageResp.NextKey)
}

func TestPaginateStringIndex_NonNotFoundErrorPropagated(t *testing.T) {
	// A non-ErrNotFound error from the getter should be propagated
	keys := []string{"a", "b"}
	expectedErr := fmt.Errorf("storage I/O error")

	getter := func(_ context.Context, key string) (string, error) {
		if key == "b" {
			return "", expectedErr
		}
		return "val-" + key, nil
	}

	iter := newMockIterator(keys)
	_, _, err := PaginateStringIndex(
		context.Background(),
		iter,
		getter,
		&query.PageRequest{Limit: 10},
		nil,
	)

	require.Same(t, expectedErr, err)
	require.True(t, iter.closed)
}

func TestPaginateStringIndex_CloseErrorPropagated(t *testing.T) {
	expectedErr := errors.New("close iterator")
	iter := newMockIterator(nil)
	iter.closeErr = expectedErr

	values, pageResp, err := PaginateStringIndex(
		context.Background(),
		iter,
		mockGetter(nil),
		&query.PageRequest{Limit: 10},
		nil,
	)

	require.Same(t, expectedErr, err)
	require.Empty(t, values)
	require.NotNil(t, pageResp)
	require.True(t, iter.closed)
}

func TestPaginateStringIndex_PrimaryErrorWinsOverCloseError(t *testing.T) {
	primaryErr := errors.New("read iterator")
	iter := newMockIterator([]string{"a"})
	iter.closeErr = errors.New("close iterator")

	_, _, err := PaginateStringIndex(
		context.Background(),
		iter,
		func(context.Context, string) (string, error) { return "", primaryErr },
		&query.PageRequest{Limit: 10},
		nil,
	)

	require.Same(t, primaryErr, err)
	require.True(t, iter.closed)
}

func TestPaginateStringIndex_EmptyIterator(t *testing.T) {
	iter := newMockIterator(nil)
	values, pageResp, err := PaginateStringIndex(
		context.Background(),
		iter,
		mockGetter(nil),
		&query.PageRequest{Limit: 10},
		nil,
	)

	require.NoError(t, err)
	require.Empty(t, values)
	require.NotNil(t, pageResp)
	require.Empty(t, pageResp.NextKey)
}

func TestPaginateStringIndex_AllFilteredOut(t *testing.T) {
	keys := []string{"a", "b", "c"}
	data := map[string]string{"a": "no", "b": "no", "c": "no"}

	iter := newMockIterator(keys)
	values, pageResp, err := PaginateStringIndex(
		context.Background(),
		iter,
		mockGetter(data),
		&query.PageRequest{Limit: 10},
		func(v string) bool { return v == filterYes },
	)

	require.NoError(t, err)
	require.Empty(t, values)
	require.Empty(t, pageResp.NextKey)
}

func TestPaginateStringIndex_ExactLimit(t *testing.T) {
	// When results exactly match the limit, NextKey should be empty
	keys := []string{"a", "b", "c"}
	data := map[string]string{"a": "1", "b": "2", "c": "3"}

	iter := newMockIterator(keys)
	values, pageResp, err := PaginateStringIndex(
		context.Background(),
		iter,
		mockGetter(data),
		&query.PageRequest{Limit: 3},
		nil,
	)

	require.NoError(t, err)
	require.Len(t, values, 3)
	require.Empty(t, pageResp.NextKey, "should be empty when results exactly match limit")
}

func TestPaginateStringIndex_OffsetBeyondEnd(t *testing.T) {
	keys := []string{"a", "b", "c"}
	data := map[string]string{"a": "1", "b": "2", "c": "3"}

	iter := newMockIterator(keys)
	values, pageRes, err := PaginateStringIndex(
		context.Background(),
		iter,
		mockGetter(data),
		&query.PageRequest{Offset: 10, Limit: 5},
		nil,
	)

	require.NoError(t, err)
	require.Empty(t, values)
	require.Empty(t, pageRes.NextKey)
	require.Equal(t, len(keys), iter.pos)
	require.True(t, iter.closed)
}

func TestPaginateStringIndex_KeyNotUsedForPositioning(t *testing.T) {
	// PaginateStringIndex no longer front-scans for the cursor key — MatchExactWithOrder
	// seeks the iterator instead (a store-level, deletion-tolerant seek). Here the Key
	// does not match any yielded item; it is simply not used for positioning, so the
	// pre-positioned iterator's items are all returned. (Deletion-tolerance itself is
	// covered by the real-store keeper tests, since a mock iterator cannot seek.)
	iter := newMockIterator([]string{"a", "b", "c"})
	data := map[string]string{"a": "1", "b": "2", "c": "3"}
	values, pageResp, err := PaginateStringIndex(
		context.Background(),
		iter,
		mockGetter(data),
		&query.PageRequest{Key: []byte("nonexistent"), Limit: 10},
		nil,
	)

	require.NoError(t, err)
	require.Equal(t, []string{"1", "2", "3"}, values, "Key is not a positioning input to PaginateStringIndex anymore")
	require.Empty(t, pageResp.NextKey)
}

func TestPaginateStringIndex_ExactCountTotal(t *testing.T) {
	keys := []string{"a", "b", "c", "d", "e"}
	data := map[string]string{"a": "1", "b": "2", "c": "3", "d": "4", "e": "5"}

	iter := newMockIterator(keys)
	values, pageRes, err := PaginateStringIndex(
		context.Background(),
		iter,
		mockGetter(data),
		&query.PageRequest{Limit: 2, CountTotal: true},
		nil,
	)

	require.NoError(t, err)
	require.Equal(t, []string{"1", "2"}, values)
	require.Equal(t, []byte("c"), pageRes.NextKey)
	require.Equal(t, uint64(5), pageRes.Total)
	require.Equal(t, len(keys), iter.pos)
	require.True(t, iter.closed)
}

func TestPaginateStringIndex_FilteredExactTotalCountsMatches(t *testing.T) {
	keys := []string{"a", "b", "c", "d", "e"}
	data := map[string]string{"a": "yes", "b": "no", "c": "yes", "d": "no", "e": "yes"}
	values, pageRes, err := PaginateStringIndex(
		context.Background(),
		newMockIterator(keys),
		mockGetter(data),
		&query.PageRequest{Offset: 1, Limit: 1, CountTotal: true},
		func(value string) bool { return value == filterYes },
	)

	require.NoError(t, err)
	require.Equal(t, []string{"yes"}, values)
	require.Equal(t, []byte("e"), pageRes.NextKey)
	require.Equal(t, uint64(3), pageRes.Total,
		"total counts matching values, not physical index rows")
}

func TestPaginateStringIndex_UnfilteredExactTotalAvoidsOffPageValueDecodes(t *testing.T) {
	keys := []string{"a", "b", "c", "d", "e"}
	data := map[string]string{"a": "1", "b": "2", "c": "3", "d": "4", "e": "5"}
	getterCalls := 0
	values, pageRes, err := PaginateStringIndex(
		context.Background(),
		newMockIterator(keys),
		func(_ context.Context, key string) (string, error) {
			getterCalls++
			return data[key], nil
		},
		&query.PageRequest{Offset: 1, Limit: 1, CountTotal: true},
		nil,
		WithStringIndexHas(func(_ context.Context, key string) (bool, error) {
			_, ok := data[key]
			return ok, nil
		}),
	)

	require.NoError(t, err)
	require.Equal(t, []string{"2"}, values)
	require.Equal(t, uint64(5), pageRes.Total)
	require.Equal(t, []byte("c"), pageRes.NextKey)
	require.Equal(t, 1, getterCalls, "only returned page values should be decoded")
}

func TestPaginateStringIndex_ExistenceCheck(t *testing.T) {
	t.Run("orphan is excluded from page and total", func(t *testing.T) {
		keys := []string{"a", "orphan", "c"}
		data := map[string]string{"a": "1", "c": "3"}
		values, pageRes, err := PaginateStringIndex(
			context.Background(),
			newMockIterator(keys),
			mockGetter(data),
			&query.PageRequest{Limit: 1, CountTotal: true},
			nil,
			WithStringIndexHas(func(_ context.Context, key string) (bool, error) {
				_, ok := data[key]
				return ok, nil
			}),
		)

		require.NoError(t, err)
		require.Equal(t, []string{"1"}, values)
		require.Equal(t, uint64(2), pageRes.Total)
		require.Equal(t, []byte("c"), pageRes.NextKey)
	})

	t.Run("existence error propagates", func(t *testing.T) {
		expectedErr := errors.New("primary store unavailable")
		_, _, err := PaginateStringIndex(
			context.Background(),
			newMockIterator([]string{"a"}),
			mockGetter(map[string]string{"a": "1"}),
			&query.PageRequest{Limit: 1, CountTotal: true},
			nil,
			WithStringIndexHas(func(context.Context, string) (bool, error) {
				return false, expectedErr
			}),
		)

		require.ErrorIs(t, err, expectedErr)
	})
}

func TestPaginateStringIndex_KeyIgnoresCountTotal(t *testing.T) {
	iter := newMockIterator([]string{"c", "d", "e"})
	data := map[string]string{"c": "3", "d": "4", "e": "5"}
	values, pageRes, err := PaginateStringIndex(
		context.Background(),
		iter,
		mockGetter(data),
		&query.PageRequest{Key: []byte("c"), Limit: 2, CountTotal: true},
		nil,
	)

	require.NoError(t, err)
	require.Equal(t, []string{"3", "4"}, values)
	require.Equal(t, []byte("e"), pageRes.NextKey)
	require.Zero(t, pageRes.Total)
}

func TestPaginateStringIndex_ExactTotalExceedingCeilingFails(t *testing.T) {
	keys := make([]string, MaxOffsetCountTotalScanLimit+1)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%05d", i)
	}
	getterCalls := 0
	iter := newMockIterator(keys)
	_, _, err := PaginateStringIndex(
		context.Background(),
		iter,
		func(_ context.Context, key string) (string, error) {
			getterCalls++
			return key, nil
		},
		&query.PageRequest{Limit: 1, CountTotal: true},
		nil,
	)

	require.ErrorIs(t, err, ErrPaginationScanLimitExceeded)
	require.Equal(t, int(MaxOffsetCountTotalScanLimit), getterCalls)
	require.True(t, iter.closed)
}

func TestPaginateStringIndex_SparseCompatibilityScansFailExactly(t *testing.T) {
	keys := make([]string, MaxOffsetCountTotalScanLimit+1)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%05d", i)
	}

	for _, pageReq := range []*query.PageRequest{
		{Offset: 1, Limit: 1},
		{Limit: 1, CountTotal: true},
	} {
		iter := newMockIterator(keys)
		_, _, err := PaginateStringIndex(
			context.Background(),
			iter,
			func(_ context.Context, _ string) (string, error) { return "no", nil },
			pageReq,
			func(value string) bool { return value == filterYes },
		)
		require.ErrorIs(t, err, ErrPaginationScanLimitExceeded)
		require.True(t, iter.closed)
	}
}

func TestPaginateStringIndex_BoundsSparseFilterScan(t *testing.T) {
	keyCount := int(MaxPageScanLimit + 1)
	keys := make([]string, keyCount)
	data := make(map[string]string, keyCount)
	getterCalls := 0
	for i := range keyCount {
		key := fmt.Sprintf("key-%04d", i)
		keys[i] = key
		data[key] = "no"
	}

	values, pageResp, err := PaginateStringIndex(
		context.Background(),
		newMockIterator(keys),
		func(ctx context.Context, key string) (string, error) {
			getterCalls++
			return mockGetter(data)(ctx, key)
		},
		&query.PageRequest{Limit: MaxPageLimit},
		func(v string) bool { return v == filterYes },
	)

	require.NoError(t, err)
	require.Empty(t, values)
	require.Equal(t, int(MaxPageScanLimit), getterCalls)
	require.Equal(t, []byte("key-1000"), pageResp.NextKey)
}

func TestPaginateStringIndex_CountTotalDisabled(t *testing.T) {
	keys := []string{"a", "b", "c", "d", "e"}
	data := map[string]string{"a": "1", "b": "2", "c": "3", "d": "4", "e": "5"}

	iter := newMockIterator(keys)
	values, pageResp, err := PaginateStringIndex(
		context.Background(),
		iter,
		mockGetter(data),
		&query.PageRequest{Limit: 2, CountTotal: false},
		nil,
	)

	require.NoError(t, err)
	require.Len(t, values, 2)
	require.Equal(t, uint64(0), pageResp.Total, "total should be 0 when CountTotal is false")
}

func TestPaginateStringIndex_MultipleInconsistentEntries(t *testing.T) {
	// Multiple deleted entries should all be skipped
	keys := []string{"del1", "a", "del2", "b", "del3"}
	data := map[string]string{"a": "1", "b": "2"}

	iter := newMockIterator(keys)
	values, pageResp, err := PaginateStringIndex(
		context.Background(),
		iter,
		mockGetter(data),
		&query.PageRequest{Limit: 10},
		nil,
	)

	require.NoError(t, err)
	require.Len(t, values, 2)
	require.Equal(t, []string{"1", "2"}, values)
	require.Empty(t, pageResp.NextKey)
}

// --- Real in-memory collections index, to exercise MatchExactWithOrder's
// --- store-level seek (which a mock iterator cannot express). ---

type probeIndexes struct {
	byGroup *indexes.Multi[int32, string, uint64]
}

func (i probeIndexes) IndexesList() []collections.Index[string, uint64] {
	return []collections.Index[string, uint64]{i.byGroup}
}

func newProbeMap(t *testing.T) (*collections.IndexedMap[string, uint64, probeIndexes], context.Context) {
	t.Helper()
	sk, ctx := colltest.MockStore()
	sb := collections.NewSchemaBuilder(sk)
	im := collections.NewIndexedMap(
		sb,
		collections.NewPrefix(0), "probe",
		collections.StringKey, collections.Uint64Value,
		probeIndexes{
			byGroup: indexes.NewMulti(
				sb, collections.NewPrefix(1), "probe_by_group",
				collections.Int32Key, collections.StringKey,
				func(_ string, group uint64) (int32, error) { return int32(group), nil }, //nolint:gosec // test value fits int32
			),
		},
	)
	_, err := sb.Build()
	require.NoError(t, err)
	return im, ctx
}

func newProbeCollection(t *testing.T) (collections.Map[string, uint64], context.Context) {
	t.Helper()
	sk, ctx := colltest.MockStore()
	sb := collections.NewSchemaBuilder(sk)
	m := collections.NewMap(
		sb,
		collections.NewPrefix(0), "probe_collection",
		collections.StringKey, collections.Uint64Value,
	)
	_, err := sb.Build()
	require.NoError(t, err)
	return m, ctx
}

func TestCollectionPaginate_DefaultDoesNotImplicitlyCountTotal(t *testing.T) {
	m, ctx := newProbeCollection(t)
	for i := range query.DefaultLimit + 1 {
		key := fmt.Sprintf("key-%03d", i)
		require.NoError(t, m.Set(ctx, key, uint64(i)))
	}

	values, pageRes, err := CollectionPaginate(
		ctx,
		m,
		nil,
		func(key string, _ uint64) (string, error) { return key, nil },
	)
	require.NoError(t, err)
	require.Len(t, values, query.DefaultLimit)
	require.NotEmpty(t, pageRes.NextKey)
	require.Zero(t, pageRes.Total,
		"a nil request must not trigger the SDK helper's implicit full count")
}

func TestCollectionPaginate_OffsetAndExactTotalBeyondPageScanLimit(t *testing.T) {
	m, ctx := newProbeCollection(t)
	const rowCount = MaxPageScanLimit + 1
	for i := range rowCount {
		key := fmt.Sprintf("key-%04d", i)
		require.NoError(t, m.Set(ctx, key, i))
	}

	transformCalls := 0
	values, pageRes, err := CollectionPaginate(
		ctx,
		m,
		&query.PageRequest{Offset: 5, Limit: 2, CountTotal: true},
		func(key string, _ uint64) (string, error) {
			transformCalls++
			return key, nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, []string{"key-0005", "key-0006"}, values)
	require.Equal(t, rowCount, pageRes.Total)
	require.NotEmpty(t, pageRes.NextKey)
	require.Equal(t, 2, transformCalls, "only returned page values should be decoded and transformed")
}

func TestCollectionPaginate_ExactTotalOverCeilingFails(t *testing.T) {
	m, ctx := newProbeCollection(t)
	for i := uint64(0); i < MaxOffsetCountTotalScanLimit; i++ {
		key := fmt.Sprintf("key-%05d", i)
		require.NoError(t, m.Set(ctx, key, i))
	}

	_, pageRes, err := CollectionPaginate(
		ctx,
		m,
		&query.PageRequest{Limit: 1, CountTotal: true},
		func(key string, _ uint64) (string, error) { return key, nil },
	)
	require.NoError(t, err)
	require.Equal(t, MaxOffsetCountTotalScanLimit, pageRes.Total)

	key := fmt.Sprintf("key-%05d", MaxOffsetCountTotalScanLimit)
	require.NoError(t, m.Set(ctx, key, MaxOffsetCountTotalScanLimit))
	_, _, err = CollectionPaginate(
		ctx,
		m,
		&query.PageRequest{Limit: 1, CountTotal: true},
		func(key string, _ uint64) (string, error) { return key, nil },
	)
	require.ErrorIs(t, err, ErrPaginationScanLimitExceeded)
}

func TestCollectionPaginate_LargeOffsetOnShortCollectionIsEmpty(t *testing.T) {
	m, ctx := newProbeCollection(t)
	require.NoError(t, m.Set(ctx, "only", 1))

	values, pageRes, err := CollectionPaginate(
		ctx,
		m,
		&query.PageRequest{Offset: MaxOffsetCountTotalScanLimit + 1, Limit: 1},
		func(key string, _ uint64) (string, error) { return key, nil },
	)
	require.NoError(t, err)
	require.Empty(t, values)
	require.Empty(t, pageRes.NextKey)
}

func TestCollectionPaginate_OffsetOverCeilingFailsOnlyWhenMoreRowsExist(t *testing.T) {
	m, ctx := newProbeCollection(t)
	for i := uint64(0); i <= MaxOffsetCountTotalScanLimit; i++ {
		key := fmt.Sprintf("key-%05d", i)
		require.NoError(t, m.Set(ctx, key, i))
	}

	_, _, err := CollectionPaginate(
		ctx,
		m,
		&query.PageRequest{Offset: MaxOffsetCountTotalScanLimit + 1, Limit: 1},
		func(key string, _ uint64) (string, error) { return key, nil },
	)
	require.ErrorIs(t, err, ErrPaginationScanLimitExceeded)
}

func TestCollectionPaginate_ReverseAndCursorSemantics(t *testing.T) {
	m, ctx := newProbeCollection(t)
	for i, key := range []string{"aa", "ab", "abcd", "b"} {
		require.NoError(t, m.Set(ctx, key, uint64(i)))
	}

	values, pageRes, err := CollectionPaginate(
		ctx,
		m,
		&query.PageRequest{Offset: 1, Limit: 1, CountTotal: true, Reverse: true},
		func(key string, _ uint64) (string, error) { return key, nil },
	)
	require.NoError(t, err)
	require.Equal(t, []string{"abcd"}, values)
	require.Equal(t, uint64(4), pageRes.Total)
	require.Equal(t, []byte("ab"), pageRes.NextKey)

	values, pageRes, err = CollectionPaginate(
		ctx,
		m,
		&query.PageRequest{Key: []byte("ab"), Limit: 10, CountTotal: true, Reverse: true},
		func(key string, _ uint64) (string, error) { return key, nil },
	)
	require.NoError(t, err)
	require.Equal(t, []string{"ab", "aa"}, values,
		"reverse resume must not include the longer prefix-colliding key")
	require.Zero(t, pageRes.Total, "count_total is ignored when key is present")

	require.NoError(t, m.Remove(ctx, "ab"))
	values, _, err = CollectionPaginate(
		ctx,
		m,
		&query.PageRequest{Key: []byte("ab"), Limit: 10, Reverse: true},
		func(key string, _ uint64) (string, error) { return key, nil },
	)
	require.NoError(t, err)
	require.Equal(t, []string{"aa"}, values,
		"a deleted cursor resumes at the nearest surviving key")
}

// collectGroup pages the (group) index at pageReq via MatchExactWithOrder +
// PaginateStringIndex, returning the primary keys collected on this page.
func collectGroup(ctx context.Context, t *testing.T, im *collections.IndexedMap[string, uint64, probeIndexes], group int32, pageReq *query.PageRequest) ([]string, *query.PageResponse) {
	t.Helper()
	iter, err := MatchExactWithOrder(ctx, im.Indexes.byGroup, group, pageReq)
	require.NoError(t, err)
	keys, pageRes, err := PaginateStringIndex(ctx, iter,
		func(_ context.Context, pk string) (string, error) { return pk, nil }, // value == primary key
		pageReq, nil)
	require.NoError(t, err)
	return keys, pageRes
}

func TestMatchExactWithOrder_StoreSeek(t *testing.T) {
	im, ctx := newProbeMap(t)
	const g = int32(7)
	for _, k := range []string{"k01", "k02", "k03", "k04", "k05"} {
		require.NoError(t, im.Set(ctx, k, uint64(g)))
	}
	// A different group to prove the seek stays within the refKey prefix.
	require.NoError(t, im.Set(ctx, "z99", uint64(9)))

	t.Run("ascending resume is inclusive of the cursor", func(t *testing.T) {
		keys, _ := collectGroup(ctx, t, im, g, &query.PageRequest{Key: []byte("k02"), Limit: 10})
		require.Equal(t, []string{"k02", "k03", "k04", "k05"}, keys)
	})

	t.Run("reverse resume is inclusive of the cursor and descends", func(t *testing.T) {
		keys, _ := collectGroup(ctx, t, im, g, &query.PageRequest{Key: []byte("k04"), Reverse: true, Limit: 10})
		require.Equal(t, []string{"k04", "k03", "k02", "k01"}, keys)
	})

	t.Run("ascending is deletion-tolerant when the cursor row is gone", func(t *testing.T) {
		require.NoError(t, im.Remove(ctx, "k02"))
		keys, _ := collectGroup(ctx, t, im, g, &query.PageRequest{Key: []byte("k02"), Limit: 10})
		require.Equal(t, []string{"k03", "k04", "k05"}, keys, "must resume from nearest surviving key, not return empty")
		require.NoError(t, im.Set(ctx, "k02", uint64(g)))
	})

	t.Run("reverse is deletion-tolerant when the cursor row is gone", func(t *testing.T) {
		require.NoError(t, im.Remove(ctx, "k04"))
		keys, _ := collectGroup(ctx, t, im, g, &query.PageRequest{Key: []byte("k04"), Reverse: true, Limit: 10})
		require.Equal(t, []string{"k03", "k02", "k01"}, keys)
		require.NoError(t, im.Set(ctx, "k04", uint64(g)))
	})

	t.Run("seek stays within the refKey prefix", func(t *testing.T) {
		keys, pageRes := collectGroup(ctx, t, im, g, &query.PageRequest{Key: []byte("k05"), Limit: 10})
		require.Equal(t, []string{"k05"}, keys) // does not cross into group 9's z99
		require.Empty(t, pageRes.NextKey)
	})
}

func TestMatchExactWithOrder_PrefixCollisionReverse(t *testing.T) {
	// Reverse resume from a cursor that is a byte-prefix of another key in the same
	// group must NOT include the longer key. This pins EndInclusive's RangeKeyNext
	// (tight, append-0x00) upper bound; a looser PrefixEndBytes (increment-last-byte)
	// bound would wrongly pull "abcd" into a reverse page resumed from "ab".
	im, ctx := newProbeMap(t)
	const g = int32(3)
	require.NoError(t, im.Set(ctx, "ab", uint64(g)))
	require.NoError(t, im.Set(ctx, "abcd", uint64(g)))

	keys, _ := collectGroup(ctx, t, im, g, &query.PageRequest{Key: []byte("ab"), Reverse: true, Limit: 10})
	require.Equal(t, []string{"ab"}, keys, "reverse from 'ab' must yield only 'ab', not 'abcd'")
}
