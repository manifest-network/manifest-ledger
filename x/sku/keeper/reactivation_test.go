package keeper_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/sku/keeper"
	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

func TestProviderReactivationRequiresCompletedCascade(t *testing.T) {
	f := initFixture(t)
	k := f.App.SKUKeeper
	authority, manager, payout := f.TestAccs[0], f.TestAccs[1], f.TestAccs[2]
	k.SetAuthority(authority.String())
	server := keeper.NewMsgServerImpl(k)
	created, err := server.CreateProvider(f.Ctx, types.NewMsgCreateProvider(authority.String(), manager.String(), payout.String(), nil, ""))
	require.NoError(t, err)
	price := sdk.NewInt64Coin("umfx", 3600)
	for _, name := range []string{"first", "second"} {
		_, err := server.CreateSKU(f.Ctx, types.NewMsgCreateSKU(authority.String(), created.Uuid, name, types.Unit_UNIT_PER_HOUR, price, nil))
		require.NoError(t, err)
	}
	update := types.NewMsgUpdateProvider(authority.String(), created.Uuid, manager.String(), payout.String(), nil, true, "https://provider.example")
	_, err = server.UpdateProvider(f.Ctx, update)
	require.NoError(t, err, "metadata updates on an active provider must not require inactive SKUs")

	deactivate := types.NewMsgDeactivateProvider(authority.String(), created.Uuid, 1)
	response, err := server.DeactivateProvider(f.Ctx, deactivate)
	require.NoError(t, err)
	require.Equal(t, uint64(1), response.DeactivatedSkuCount)
	require.True(t, response.HasMore)

	// Updating metadata while remaining inactive must also remain possible.
	update.Active = false
	_, err = server.UpdateProvider(f.Ctx, update)
	require.NoError(t, err)
	update.Active = true
	update.MetaHash = []byte("must not be written")
	update.ApiUrl = "https://replacement.example"
	before := snapshotSKUStore(t, f)
	eventsBefore := slices.Clone(f.Ctx.EventManager().Events())
	updated, err := server.UpdateProvider(f.Ctx, update)
	require.Nil(t, updated)
	require.ErrorIs(t, err, types.ErrInvalidProvider)
	require.ErrorContains(t, err, "finish deactivating its SKUs first")
	require.Equal(t, before, snapshotSKUStore(t, f))
	require.Equal(t, eventsBefore, f.Ctx.EventManager().Events())

	response, err = server.DeactivateProvider(f.Ctx, deactivate)
	require.NoError(t, err)
	require.Equal(t, uint64(1), response.DeactivatedSkuCount)
	require.False(t, response.HasMore)
	_, err = server.UpdateProvider(f.Ctx, update)
	require.NoError(t, err)
	provider, err := k.GetProvider(f.Ctx, created.Uuid)
	require.NoError(t, err)
	require.True(t, provider.Active)
	skus, err := k.GetSKUsByProviderUUID(f.Ctx, created.Uuid)
	require.NoError(t, err)
	require.Len(t, skus, 2)
	for _, sku := range skus {
		require.False(t, sku.Active, "completing the provider lifecycle must not reactivate SKUs")
	}
	sku := skus[0]
	_, err = server.UpdateSKU(f.Ctx, types.NewMsgUpdateSKU(authority.String(), sku.Uuid, sku.ProviderUuid, sku.Name, sku.Unit, sku.BasePrice, sku.MetaHash, true))
	require.NoError(t, err)
	updatedSKU, err := k.GetSKU(f.Ctx, sku.Uuid)
	require.NoError(t, err)
	require.True(t, updatedSKU.Active)
}
