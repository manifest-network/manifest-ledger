package keeper

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	store "cosmossdk.io/core/store"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/log"

	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"

	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

type activeIndexErrorStoreService struct {
	store store.KVStore
}

func (s activeIndexErrorStoreService) OpenKVStore(context.Context) store.KVStore {
	return s.store
}

type activeIndexErrorStore struct {
	openErr  error
	closeErr error
}

func (s activeIndexErrorStore) Get([]byte) ([]byte, error) { return nil, nil }
func (s activeIndexErrorStore) Has([]byte) (bool, error)   { return false, nil }
func (s activeIndexErrorStore) Set([]byte, []byte) error   { return nil }
func (s activeIndexErrorStore) Delete([]byte) error        { return nil }
func (s activeIndexErrorStore) Iterator(_, _ []byte) (store.Iterator, error) {
	if s.openErr != nil {
		return nil, s.openErr
	}
	return activeIndexEmptyIterator{closeErr: s.closeErr}, nil
}

func (s activeIndexErrorStore) ReverseIterator(start, end []byte) (store.Iterator, error) {
	return s.Iterator(start, end)
}

type activeIndexEmptyIterator struct {
	closeErr error
}

func (activeIndexEmptyIterator) Domain() ([]byte, []byte) { return nil, nil }
func (activeIndexEmptyIterator) Valid() bool              { return false }
func (activeIndexEmptyIterator) Next()                    { panic("Next called on invalid iterator") }
func (activeIndexEmptyIterator) Key() []byte              { panic("Key called on invalid iterator") }
func (activeIndexEmptyIterator) Value() []byte            { panic("Value called on invalid iterator") }
func (activeIndexEmptyIterator) Error() error             { return nil }
func (i activeIndexEmptyIterator) Close() error           { return i.closeErr }

func TestHasActiveSKUsByProviderClassifiesIndexFailures(t *testing.T) {
	testCases := []struct {
		name  string
		store activeIndexErrorStore
	}{
		{name: "open", store: activeIndexErrorStore{openErr: errors.New("open failed")}},
		{name: "close", store: activeIndexErrorStore{closeErr: errors.New("close failed")}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			encodingConfig := moduletestutil.MakeTestEncodingConfig()
			keeper := NewKeeper(
				encodingConfig.Codec,
				activeIndexErrorStoreService{store: testCase.store},
				log.NewNopLogger(),
				"",
				nil,
				nil,
			)

			hasActive, err := keeper.HasActiveSKUsByProvider(context.Background(), "provider")
			require.False(t, hasActive)
			require.ErrorIs(t, err, types.ErrInternalCorruption)
			codespace, code, _ := errorsmod.ABCIInfo(err, false)
			require.Equal(t, types.ModuleName, codespace)
			require.Equal(t, uint32(9), code)
		})
	}
}
