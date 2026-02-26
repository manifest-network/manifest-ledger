package pagination

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"

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
	keys := []string{"a", "b", "c", "d", "e"}
	data := map[string]string{"a": "1", "b": "2", "c": "3", "d": "4", "e": "5"}

	// Start from key "c" (simulating second page with NextKey from previous call)
	iter := newMockIterator(keys)
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
	keys := []string{"a", "b", "c", "d", "e"}
	data := map[string]string{"a": "1", "b": "2", "c": "3", "d": "4", "e": "5"}

	// Start from key "d", limit 5 (more than remaining)
	iter := newMockIterator(keys)
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

func TestPaginateStringIndex_KeyNotFound(t *testing.T) {
	// When the start key doesn't exist in the iterator, no results are returned
	keys := []string{"a", "b", "c"}
	data := map[string]string{"a": "1", "b": "2", "c": "3"}

	iter := newMockIterator(keys)
	values, pageResp, err := PaginateStringIndex(
		context.Background(),
		iter,
		mockGetter(data),
		&query.PageRequest{Key: []byte("nonexistent"), Limit: 10},
		nil,
	)

	require.NoError(t, err)
	require.Empty(t, values, "no results when key is not found in iterator")
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
