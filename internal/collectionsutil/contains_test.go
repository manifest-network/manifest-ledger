package collectionsutil

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	"cosmossdk.io/collections/colltest"
	"cosmossdk.io/collections/indexes"
	"cosmossdk.io/core/store"
)

func TestMultiIndexContainsUsesExactPairRange(t *testing.T) {
	service, ctx := colltest.MockStore()
	recorder := &rangeRecordingStore{KVStore: service.OpenKVStore(ctx)}
	schema := collections.NewSchemaBuilder(rangeRecordingService{recorder})
	index := indexes.NewMulti(schema, collections.NewPrefix(1), "references", collections.StringKey, collections.StringKey,
		func(_ string, reference string) (string, error) { return reference, nil })
	_, err := schema.Build()
	require.NoError(t, err)
	for i := range 1000 {
		require.NoError(t, index.Reference(ctx, fmt.Sprintf("%04d", i), "shared", func() (string, error) {
			return "", collections.ErrNotFound
		}))
	}
	for _, primary := range []string{"", "a", "aa"} {
		require.NoError(t, index.Reference(ctx, primary, "shared", func() (string, error) {
			return "", collections.ErrNotFound
		}))
	}
	for _, test := range []struct {
		reference string
		primary   string
		want      bool
	}{
		{"shared", "", true},
		{"shared", "a", true},
		{"shared", "aa", true},
		{"shared", "aaa", false},
		{"shared", "0999", true},
		{"shared", "1000", false},
		{"share", "0999", false},
	} {
		t.Run(test.reference+"/"+test.primary, func(t *testing.T) {
			found, err := MultiIndexContains(ctx, index, test.reference, test.primary)
			require.NoError(t, err)
			require.Equal(t, test.want, found)
			key, err := collections.EncodeKeyWithPrefix([]byte{1}, index.KeyCodec(), collections.Join(test.reference, test.primary))
			require.NoError(t, err)
			require.Equal(t, key, recorder.start)
			require.Equal(t, append(bytes.Clone(key), 0), recorder.end)
		})
	}

	fault := errors.New("iterator unavailable")
	recorder.openErr = fault
	_, err = MultiIndexContains(ctx, index, "shared", "0999")
	require.ErrorIs(t, err, fault)
	recorder.openErr = nil
	recorder.closeErr = fault
	_, err = MultiIndexContains(ctx, index, "shared", "0999")
	require.ErrorIs(t, err, fault)
}

type rangeRecordingService struct{ store.KVStore }

func (s rangeRecordingService) OpenKVStore(context.Context) store.KVStore { return s.KVStore }

type rangeRecordingStore struct {
	store.KVStore
	start, end        []byte
	openErr, closeErr error
}

func (s *rangeRecordingStore) Iterator(start, end []byte) (store.Iterator, error) {
	s.start, s.end = bytes.Clone(start), bytes.Clone(end)
	if s.openErr != nil {
		return nil, s.openErr
	}
	iterator, err := s.KVStore.Iterator(start, end)
	if err != nil {
		return nil, err
	}
	return rangeRecordingIterator{iterator, s.closeErr}, nil
}

type rangeRecordingIterator struct {
	store.Iterator
	closeErr error
}

func (i rangeRecordingIterator) Close() error {
	return errors.Join(i.Iterator.Close(), i.closeErr)
}
