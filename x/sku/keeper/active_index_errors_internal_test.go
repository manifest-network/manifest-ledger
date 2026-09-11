package keeper

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections/colltest"
	store "cosmossdk.io/core/store"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/log"

	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"

	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

type activeIndexReadFaultStore struct {
	store.KVStore
	fault activeIndexErrorStore
}

func (s activeIndexReadFaultStore) Iterator(start, end []byte) (store.Iterator, error) {
	return s.fault.Iterator(start, end)
}

func TestProviderReactivationPreservesIndexErrors(t *testing.T) {
	for _, operation := range []string{"open", "close"} {
		t.Run(operation, func(t *testing.T) {
			service, ctx := colltest.MockStore()
			fault := activeIndexErrorStore{}
			if operation == "open" {
				fault.openErr = errors.New("open failed")
			} else {
				fault.closeErr = errors.New("close failed")
			}
			encoding := moduletestutil.MakeTestEncodingConfig()
			address := sdk.AccAddress([]byte("12345678901234567890")).String()
			k := NewKeeper(encoding.Codec, activeIndexErrorStoreService{
				store: activeIndexReadFaultStore{KVStore: service.OpenKVStore(ctx), fault: fault},
			}, log.NewNopLogger(), address, nil, nil)
			provider := types.Provider{
				Uuid: "01912345-6789-7abc-8def-0123456789ab", Address: address, PayoutAddress: address,
			}
			require.NoError(t, k.SetProvider(ctx, provider))
			response, err := NewMsgServerImpl(k).UpdateProvider(ctx, types.NewMsgUpdateProvider(address, provider.Uuid, address, address, nil, true, ""))
			require.Nil(t, response)
			require.ErrorIs(t, err, types.ErrInternalCorruption)
			require.ErrorContains(t, err, operation+" failed")
			stored, err := k.GetProvider(ctx, provider.Uuid)
			require.NoError(t, err)
			require.Equal(t, provider, stored)
		})
	}
}

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
