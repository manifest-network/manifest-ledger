package pagination

import (
	"context"
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
	keys   []string
	pos    int
	closed bool
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
	return nil
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
	values, pageResp, err := PaginateStringIndex(
		context.Background(),
		iter,
		mockGetter(data),
		&query.PageRequest{Offset: 2, Limit: 2},
		nil,
	)

	require.NoError(t, err)
	require.Len(t, values, 2)
	require.Equal(t, []string{"3", "4"}, values)
	require.Equal(t, []byte("e"), pageResp.NextKey)
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

	require.Error(t, err)
	require.ErrorIs(t, err, expectedErr)
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

func TestPaginateStringIndex_OffsetBeyondResults(t *testing.T) {
	keys := []string{"a", "b", "c"}
	data := map[string]string{"a": "1", "b": "2", "c": "3"}

	iter := newMockIterator(keys)
	values, pageResp, err := PaginateStringIndex(
		context.Background(),
		iter,
		mockGetter(data),
		&query.PageRequest{Offset: 10, Limit: 5},
		nil,
	)

	require.NoError(t, err)
	require.Empty(t, values)
	require.Empty(t, pageResp.NextKey)
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

func TestPaginateStringIndex_CountTotal(t *testing.T) {
	keys := []string{"a", "b", "c", "d", "e"}
	data := map[string]string{"a": "1", "b": "2", "c": "3", "d": "4", "e": "5"}

	iter := newMockIterator(keys)
	values, pageResp, err := PaginateStringIndex(
		context.Background(),
		iter,
		mockGetter(data),
		&query.PageRequest{Limit: 2, CountTotal: true},
		nil,
	)

	require.NoError(t, err)
	require.Len(t, values, 2)
	require.Equal(t, []string{"1", "2"}, values)
	require.NotEmpty(t, pageResp.NextKey)
	require.Equal(t, uint64(5), pageResp.Total)
}

func TestPaginateStringIndex_CountTotalWithFilter(t *testing.T) {
	keys := []string{"a", "b", "c", "d", "e", "f"}
	data := map[string]string{"a": "yes", "b": "no", "c": "yes", "d": "no", "e": "yes", "f": "yes"}

	iter := newMockIterator(keys)
	values, pageResp, err := PaginateStringIndex(
		context.Background(),
		iter,
		mockGetter(data),
		&query.PageRequest{Limit: 2, CountTotal: true},
		func(v string) bool { return v == filterYes },
	)

	require.NoError(t, err)
	require.Len(t, values, 2)
	require.Equal(t, []string{"yes", "yes"}, values)
	require.NotEmpty(t, pageResp.NextKey)
	require.Equal(t, uint64(4), pageResp.Total, "total should count only filtered matches")
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
