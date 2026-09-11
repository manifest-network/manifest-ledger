package keeper_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/sku/keeper"
	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

func TestCreationAndDeactivationEventsCanonicalizeSigner(t *testing.T) {
	f := initFixture(t)
	signer, providerAddress := f.TestAccs[0], f.TestAccs[1]
	k := f.App.SKUKeeper
	k.SetAuthority(signer.String())
	ms := keeper.NewMsgServerImpl(k)
	authority := strings.ToUpper(signer.String())
	assertSigner := func(eventType, key string) {
		t.Helper()
		matches := 0
		for _, event := range f.Ctx.EventManager().Events() {
			if event.Type != eventType {
				continue
			}
			for _, attribute := range event.Attributes {
				if attribute.Key == key {
					require.Equal(t, signer.String(), attribute.Value)
					matches++
				}
			}
		}
		require.Equal(t, 1, matches)
	}
	f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())
	provider, err := ms.CreateProvider(f.Ctx, &types.MsgCreateProvider{Authority: authority, Address: providerAddress.String(), PayoutAddress: providerAddress.String()})
	require.NoError(t, err)
	assertSigner(types.EventTypeProviderCreated, types.AttributeKeyCreatedBy)
	var skus []string
	for range 2 {
		f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())
		sku, err := ms.CreateSKU(f.Ctx, &types.MsgCreateSKU{Authority: authority, ProviderUuid: provider.Uuid, Name: "test", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: sdk.NewInt64Coin("umfx", 3_600)})
		require.NoError(t, err)
		skus = append(skus, sku.Uuid)
		assertSigner(types.EventTypeSKUCreated, types.AttributeKeyCreatedBy)
	}
	f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())
	_, err = ms.DeactivateSKU(f.Ctx, &types.MsgDeactivateSKU{Authority: authority, Uuid: skus[0]})
	require.NoError(t, err)
	assertSigner(types.EventTypeSKUDeactivated, types.AttributeKeyDeactivatedBy)
	f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())
	_, err = ms.DeactivateProvider(f.Ctx, &types.MsgDeactivateProvider{Authority: authority, Uuid: provider.Uuid})
	require.NoError(t, err)
	assertSigner(types.EventTypeProviderDeactivated, types.AttributeKeyDeactivatedBy)
	assertSigner(types.EventTypeSKUDeactivated, types.AttributeKeyDeactivatedBy)
}
