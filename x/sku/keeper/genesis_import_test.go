package keeper_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

func TestInitGenesisPreparesAndPersistsRawAddresses(t *testing.T) {
	f := initFixture(t)
	manager := f.TestAccs[0]
	payout := f.TestAccs[1]
	allowed := f.TestAccs[2]
	genesis := &types.GenesisState{
		Params: types.Params{AllowedList: []string{
			strings.ToUpper(allowed.String()),
			allowed.String(),
		}},
		Providers: []types.Provider{{
			Uuid:          testProviderUUID,
			Address:       strings.ToUpper(manager.String()),
			PayoutAddress: strings.ToUpper(payout.String()),
			Active:        true,
		}},
		ProviderSequence: 1,
	}

	require.NoError(t, f.App.SKUKeeper.InitGenesis(f.Ctx, genesis))
	params, err := f.App.SKUKeeper.GetParams(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, []string{allowed.String()}, params.AllowedList)
	provider, err := f.App.SKUKeeper.GetProvider(f.Ctx, testProviderUUID)
	require.NoError(t, err)
	require.Equal(t, manager.String(), provider.Address)
	require.Equal(t, payout.String(), provider.PayoutAddress)
	requireProviderAddressIndex(t, f, manager, testProviderUUID)

	store := f.Ctx.KVStore(f.App.GetKey(types.StoreKey))
	rawParams := store.Get(types.ParamsKey.Bytes())
	rawProvider := store.Get(skuProviderStoreKey(t, testProviderUUID))
	require.True(t, bytes.HasPrefix(rawParams, []byte(testParamsStoragePrefix)))
	require.True(t, bytes.HasPrefix(rawProvider, []byte(testProviderStoragePrefix)))
	require.True(t, bytes.Contains(rawParams, allowed.Bytes()))
	require.True(t, bytes.Contains(rawProvider, manager.Bytes()))
	require.True(t, bytes.Contains(rawProvider, payout.Bytes()))
	require.False(t, bytes.Contains(rawParams, []byte(allowed.String())))
	require.False(t, bytes.Contains(rawProvider, []byte(manager.String())))
	require.False(t, bytes.Contains(rawProvider, []byte(payout.String())))

	exported := f.App.SKUKeeper.ExportGenesis(f.Ctx)
	require.NoError(t, exported.Validate())
	require.Equal(t, []string{allowed.String()}, exported.Params.AllowedList)
	require.Equal(t, manager.String(), exported.Providers[0].Address)
	require.Equal(t, payout.String(), exported.Providers[0].PayoutAddress)
}

func TestInitGenesisRejectsInvalidStateBeforeWriting(t *testing.T) {
	f := initFixture(t)
	originalParams, err := f.App.SKUKeeper.GetParams(f.Ctx)
	require.NoError(t, err)
	manager := f.TestAccs[0]
	payout := f.TestAccs[1]
	allowed := f.TestAccs[2]
	provider := types.Provider{
		Uuid:          testProviderUUID,
		Address:       manager.String(),
		PayoutAddress: payout.String(),
		Active:        true,
	}
	genesis := &types.GenesisState{
		Params:           types.Params{AllowedList: []string{allowed.String()}},
		Providers:        []types.Provider{provider, provider},
		ProviderSequence: 2,
	}

	err = f.App.SKUKeeper.InitGenesis(f.Ctx, genesis)
	require.ErrorContains(t, err, "duplicate provider uuid")
	paramsAfter, getErr := f.App.SKUKeeper.GetParams(f.Ctx)
	require.NoError(t, getErr)
	require.Equal(t, originalParams, paramsAfter)
	_, getErr = f.App.SKUKeeper.GetProvider(f.Ctx, provider.Uuid)
	require.ErrorIs(t, getErr, types.ErrProviderNotFound)

	store := f.Ctx.KVStore(f.App.GetKey(types.StoreKey))
	require.Nil(t, store.Get(skuProviderStoreKey(t, provider.Uuid)))
	require.False(t, bytes.Contains(store.Get(types.ParamsKey.Bytes()), allowed.Bytes()))
}
