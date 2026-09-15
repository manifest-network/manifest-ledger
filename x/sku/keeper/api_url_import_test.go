package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/manifest-network/manifest-ledger/x/sku/keeper"
	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

func TestHistoricalAPIURLImportAndInvariantCompatibility(t *testing.T) {
	for _, apiURL := range []string{"https://:443", "https://example.com:99999", "https://example.com:"} {
		t.Run(apiURL, func(t *testing.T) {
			f := initFixture(t)
			manager, payout := f.TestAccs[0], f.TestAccs[1]
			genesis := &types.GenesisState{
				Params: types.DefaultParams(),
				Providers: []types.Provider{{
					Uuid: testProviderUUID, Address: manager.String(), PayoutAddress: payout.String(),
					Active: true, ApiUrl: apiURL,
				}},
				ProviderSequence: 1,
			}
			require.ErrorIs(t, types.ValidateAPIURL(apiURL), types.ErrInvalidAPIURL)
			require.NoError(t, genesis.Validate(), "historically accepted metadata must remain importable")
			require.NoError(t, f.App.SKUKeeper.InitGenesis(f.Ctx, genesis))
			message, broken := keeper.StateInvariant(f.App.SKUKeeper)(f.Ctx)
			require.False(t, broken, message)
			exported := f.App.SKUKeeper.ExportGenesis(f.Ctx)
			require.Equal(t, apiURL, exported.Providers[0].ApiUrl)

			reimported := initFixture(t)
			require.NoError(t, reimported.App.SKUKeeper.InitGenesis(reimported.Ctx, exported))
			provider, err := reimported.App.SKUKeeper.GetProvider(reimported.Ctx, testProviderUUID)
			require.NoError(t, err)
			require.Equal(t, apiURL, provider.ApiUrl)

			// An unrelated provider update can preserve legacy metadata. A new
			// URL must pass the stricter message rules, and a valid replacement
			// remains available to repair the old endpoint.
			k := reimported.App.SKUKeeper
			k.SetAuthority(manager.String())
			server := keeper.NewMsgServerImpl(k)
			update := types.NewMsgUpdateProvider(manager.String(), provider.Uuid, manager.String(), payout.String(), nil, true, "")
			_, err = server.UpdateProvider(reimported.Ctx, update)
			require.NoError(t, err)
			provider, err = k.GetProvider(reimported.Ctx, provider.Uuid)
			require.NoError(t, err)
			require.Equal(t, apiURL, provider.ApiUrl)
			update.ApiUrl = apiURL
			_, err = server.UpdateProvider(reimported.Ctx, update)
			require.ErrorIs(t, err, types.ErrInvalidProvider)
			_, err = server.CreateProvider(reimported.Ctx, types.NewMsgCreateProvider(manager.String(), manager.String(), payout.String(), nil, apiURL))
			require.ErrorIs(t, err, types.ErrInvalidProvider)
			update.ApiUrl = "https://provider.example:8443"
			_, err = server.UpdateProvider(reimported.Ctx, update)
			require.NoError(t, err)
			provider, err = k.GetProvider(reimported.Ctx, provider.Uuid)
			require.NoError(t, err)
			require.Equal(t, update.ApiUrl, provider.ApiUrl)
		})
	}
}
