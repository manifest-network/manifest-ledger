package keeper_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"

	sdkcodec "github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/sku/keeper"
	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

const (
	testParamsStoragePrefix   = "\x00sku/params/v1"
	testProviderStoragePrefix = "\x00sku/provider/v1"
)

func migrationAllowedList(size int) []string {
	addresses := make([]string, size)
	for i := range addresses {
		addresses[i] = sdk.AccAddress(bytes.Repeat([]byte{byte(i + 1)}, 20)).String()
	}
	return addresses
}

func TestMigratorMigrate1to2RejectsOversizedAllowedListBeforeWrite(t *testing.T) {
	f := initFixture(t)
	params := types.Params{AllowedList: migrationAllowedList(types.MaxAllowedListEntries + 1)}
	require.NoError(t, f.App.SKUKeeper.SetParams(f.Ctx, params))

	store := f.Ctx.KVStore(f.App.GetKey(types.StoreKey))
	before := bytes.Clone(store.Get(types.ParamsKey.Bytes()))
	err := keeper.NewMigrator(f.App.SKUKeeper).Migrate1to2(f.Ctx)
	require.ErrorIs(t, err, types.ErrInvalidConfig)
	require.Contains(t, err.Error(), "allowed list has 101 entries, maximum allowed is 100")
	require.Equal(t, before, store.Get(types.ParamsKey.Bytes()))
}

func TestMigratorMigrate1to2RewritesLegacyAddressStrings(t *testing.T) {
	f := initFixture(t)
	manager := f.TestAccs[0]
	payout := f.TestAccs[1]
	allowed := f.TestAccs[2]
	upperManager := strings.ToUpper(manager.String())
	upperPayout := strings.ToUpper(payout.String())
	upperAllowed := strings.ToUpper(allowed.String())

	params := types.Params{AllowedList: []string{allowed.String(), upperAllowed}}
	provider := types.Provider{
		Uuid:          testProviderUUID,
		Address:       upperManager,
		PayoutAddress: upperPayout,
		MetaHash:      []byte{0xaa, 0xbb},
		Active:        true,
		ApiUrl:        "https://provider.example",
	}

	// Seed the current byte-keyed index, then replace only primary values with
	// their exact v1 CollValue encodings.
	require.NoError(t, f.App.SKUKeeper.SetParams(f.Ctx, params))
	require.NoError(t, f.App.SKUKeeper.SetProvider(f.Ctx, provider))
	legacyParams, err := sdkcodec.CollValue[types.Params](f.EncodingCfg.Codec).Encode(params)
	require.NoError(t, err)
	legacyProvider, err := sdkcodec.CollValue[types.Provider](f.EncodingCfg.Codec).Encode(provider)
	require.NoError(t, err)
	providerKey := skuProviderStoreKey(t, provider.Uuid)
	store := f.Ctx.KVStore(f.App.GetKey(types.StoreKey))
	store.Set(types.ParamsKey.Bytes(), legacyParams)
	store.Set(providerKey, legacyProvider)
	require.True(t, bytes.Contains(store.Get(types.ParamsKey.Bytes()), []byte(upperAllowed)))
	require.True(t, bytes.Contains(store.Get(providerKey), []byte(upperManager)))
	require.True(t, bytes.Contains(store.Get(providerKey), []byte(upperPayout)))
	requireProviderAddressIndex(t, f, manager, provider.Uuid)

	require.NoError(t, keeper.NewMigrator(f.App.SKUKeeper).Migrate1to2(f.Ctx))

	rawParams := store.Get(types.ParamsKey.Bytes())
	rawProvider := store.Get(providerKey)
	require.True(t, bytes.HasPrefix(rawParams, []byte(testParamsStoragePrefix)))
	require.True(t, bytes.HasPrefix(rawProvider, []byte(testProviderStoragePrefix)))
	require.True(t, bytes.Contains(rawParams, allowed.Bytes()))
	require.True(t, bytes.Contains(rawProvider, manager.Bytes()))
	require.True(t, bytes.Contains(rawProvider, payout.Bytes()))
	require.False(t, bytes.Contains(rawParams, []byte(allowed.String())))
	require.False(t, bytes.Contains(rawParams, []byte(upperAllowed)))
	require.False(t, bytes.Contains(rawProvider, []byte(manager.String())))
	require.False(t, bytes.Contains(rawProvider, []byte(upperManager)))
	require.False(t, bytes.Contains(rawProvider, []byte(payout.String())))
	require.False(t, bytes.Contains(rawProvider, []byte(upperPayout)))

	gotParams, err := f.App.SKUKeeper.GetParams(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, []string{allowed.String()}, gotParams.AllowedList)
	gotProvider, err := f.App.SKUKeeper.GetProvider(f.Ctx, provider.Uuid)
	require.NoError(t, err)
	require.Equal(t, manager.String(), gotProvider.Address)
	require.Equal(t, payout.String(), gotProvider.PayoutAddress)
	require.Equal(t, provider.Uuid, gotProvider.Uuid)
	require.Equal(t, provider.MetaHash, gotProvider.MetaHash)
	require.Equal(t, provider.Active, gotProvider.Active)
	require.Equal(t, provider.ApiUrl, gotProvider.ApiUrl)
	requireProviderAddressIndex(t, f, manager, provider.Uuid)

	paramsSnapshot := bytes.Clone(rawParams)
	providerSnapshot := bytes.Clone(rawProvider)
	require.NoError(t, keeper.NewMigrator(f.App.SKUKeeper).Migrate1to2(f.Ctx))
	require.Equal(t, paramsSnapshot, store.Get(types.ParamsKey.Bytes()))
	require.Equal(t, providerSnapshot, store.Get(providerKey))
}

func TestMigratorMigrate1to2ProcessesMultipleOrderedPages(t *testing.T) {
	f := initFixture(t)
	manager := f.TestAccs[0]
	payout := f.TestAccs[1]
	store := f.Ctx.KVStore(f.App.GetKey(types.StoreKey))
	legacyCodec := sdkcodec.CollValue[types.Provider](f.EncodingCfg.Codec)

	const providerCount = 1001
	for i := range providerCount {
		uuid := fmt.Sprintf("provider-%04d", i)
		provider := types.Provider{
			Uuid:          uuid,
			Address:       strings.ToUpper(manager.String()),
			PayoutAddress: strings.ToUpper(payout.String()),
			Active:        i%2 == 0,
		}
		require.NoError(t, f.App.SKUKeeper.SetProvider(f.Ctx, provider))
		legacy, err := legacyCodec.Encode(provider)
		require.NoError(t, err)
		store.Set(skuProviderStoreKey(t, uuid), legacy)
	}

	require.NoError(t, keeper.NewMigrator(f.App.SKUKeeper).Migrate1to2(f.Ctx))
	for _, uuid := range []string{"provider-0000", "provider-0999", "provider-1000"} {
		raw := store.Get(skuProviderStoreKey(t, uuid))
		require.True(t, bytes.HasPrefix(raw, []byte(testProviderStoragePrefix)))
		require.False(t, bytes.Contains(raw, []byte(strings.ToUpper(manager.String()))))
	}
}

func TestModuleManagerRunsSKUMigrate1to2(t *testing.T) {
	f := initFixture(t)
	manager := f.TestAccs[0]
	payout := f.TestAccs[1]
	provider := types.Provider{
		Uuid:          testProviderUUID,
		Address:       strings.ToUpper(manager.String()),
		PayoutAddress: strings.ToUpper(payout.String()),
		Active:        true,
	}
	require.NoError(t, f.App.SKUKeeper.SetProvider(f.Ctx, provider))
	legacy, err := sdkcodec.CollValue[types.Provider](f.EncodingCfg.Codec).Encode(provider)
	require.NoError(t, err)
	providerKey := skuProviderStoreKey(t, provider.Uuid)
	store := f.Ctx.KVStore(f.App.GetKey(types.StoreKey))
	store.Set(providerKey, legacy)

	versionMap := f.App.ModuleManager.GetVersionMap()
	versionMap[types.ModuleName] = 1
	updatedVersionMap, err := f.App.ModuleManager.RunMigrations(
		f.Ctx,
		f.App.Configurator(),
		versionMap,
	)
	require.NoError(t, err)
	require.Equal(t, uint64(2), updatedVersionMap[types.ModuleName])
	require.True(t, bytes.HasPrefix(store.Get(providerKey), []byte(testProviderStoragePrefix)))
}

func skuProviderStoreKey(t *testing.T, uuid string) []byte {
	t.Helper()
	key, err := collections.EncodeKeyWithPrefix(
		types.ProviderKey.Bytes(),
		collections.StringKey,
		uuid,
	)
	require.NoError(t, err)
	return key
}

func requireProviderAddressIndex(t *testing.T, f *testFixture, address sdk.AccAddress, providerUUID string) {
	t.Helper()
	iterator, err := f.App.SKUKeeper.Providers.Indexes.Address.MatchExact(f.Ctx, address)
	require.NoError(t, err)
	require.True(t, iterator.Valid())
	gotUUID, err := iterator.PrimaryKey()
	require.NoError(t, err)
	require.Equal(t, providerUUID, gotUUID)
	iterator.Next()
	require.False(t, iterator.Valid())
	require.NoError(t, iterator.Close())
}
