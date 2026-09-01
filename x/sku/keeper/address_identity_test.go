package keeper_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/manifest-network/manifest-ledger/x/sku/keeper"
	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

func TestMsgServerUsesDecodedAddressIdentity(t *testing.T) {
	t.Run("authority alias and provider fields", func(t *testing.T) {
		f := initFixture(t)
		authority := f.TestAccs[0]
		manager := f.TestAccs[1]
		payout := f.TestAccs[2]
		f.App.SKUKeeper.SetAuthority(authority.String())
		server := keeper.NewMsgServerImpl(f.App.SKUKeeper)

		response, err := server.CreateProvider(f.Ctx, &types.MsgCreateProvider{
			Authority:     strings.ToUpper(authority.String()),
			Address:       strings.ToUpper(manager.String()),
			PayoutAddress: strings.ToUpper(payout.String()),
		})
		require.NoError(t, err)
		provider, err := f.App.SKUKeeper.GetProvider(f.Ctx, response.Uuid)
		require.NoError(t, err)
		require.Equal(t, manager.String(), provider.Address)
		require.Equal(t, payout.String(), provider.PayoutAddress)
	})

	t.Run("allowed-list alias", func(t *testing.T) {
		f := initFixture(t)
		authority := f.TestAccs[0]
		allowed := f.TestAccs[1]
		manager := f.TestAccs[2]
		payout := f.TestAccs[3]
		f.App.SKUKeeper.SetAuthority(authority.String())
		require.NoError(t, f.App.SKUKeeper.SetParams(f.Ctx, types.Params{
			AllowedList: []string{allowed.String()},
		}))
		server := keeper.NewMsgServerImpl(f.App.SKUKeeper)

		_, err := server.CreateProvider(f.Ctx, &types.MsgCreateProvider{
			Authority:     strings.ToUpper(allowed.String()),
			Address:       manager.String(),
			PayoutAddress: payout.String(),
		})
		require.NoError(t, err)
	})

	t.Run("update params authority alias", func(t *testing.T) {
		f := initFixture(t)
		authority := f.TestAccs[0]
		allowed := f.TestAccs[1]
		f.App.SKUKeeper.SetAuthority(authority.String())
		server := keeper.NewMsgServerImpl(f.App.SKUKeeper)

		_, err := server.UpdateParams(f.Ctx, &types.MsgUpdateParams{
			Authority: strings.ToUpper(authority.String()),
			Params:    types.Params{AllowedList: []string{allowed.String()}},
		})
		require.NoError(t, err)
	})
}
