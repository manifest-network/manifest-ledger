package keeper_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"

	"github.com/manifest-network/manifest-ledger/x/sku/keeper"
	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

// snapshotSKUStore includes sequences and every primary and secondary index row.
func snapshotSKUStore(t *testing.T, f *testFixture) map[string][]byte {
	t.Helper()
	store := f.Ctx.KVStore(f.App.GetKey(types.StoreKey))
	iter := store.Iterator(nil, nil)
	state := make(map[string][]byte)
	for ; iter.Valid(); iter.Next() {
		state[string(iter.Key())] = bytes.Clone(iter.Value())
	}
	require.NoError(t, iter.Close())
	return state
}

func TestProviderPayoutPolicy(t *testing.T) {
	for _, role := range []string{"authority", "allowed"} {
		t.Run(role, func(t *testing.T) {
			f := initFixture(t)
			k := f.App.SKUKeeper
			authority, manager, payout, allowed := f.TestAccs[0], f.TestAccs[1], f.TestAccs[2], f.TestAccs[3]
			k.SetAuthority(authority.String())
			sender := authority
			if role == "allowed" {
				require.NoError(t, k.SetParams(f.Ctx, types.Params{AllowedList: []string{allowed.String()}}))
				sender = allowed
			}
			server := keeper.NewMsgServerImpl(k)
			blocked := authtypes.NewModuleAddress(distrtypes.ModuleName)
			require.True(t, f.App.BankKeeper.BlockedAddr(blocked))
			require.False(t, f.App.BankKeeper.BlockedAddr(payout))

			before := snapshotSKUStore(t, f)
			eventsBefore := slices.Clone(f.Ctx.EventManager().Events())
			created, err := server.CreateProvider(f.Ctx, &types.MsgCreateProvider{
				Authority: sender.String(), Address: manager.String(), PayoutAddress: strings.ToUpper(blocked.String()),
			})
			require.Nil(t, created)
			require.ErrorIs(t, err, types.ErrInvalidProvider)
			require.ErrorContains(t, err, "payout address is blocked by bank policy")
			require.Equal(t, before, snapshotSKUStore(t, f), "rejected creation must not allocate a UUID or write state")
			require.Equal(t, eventsBefore, f.Ctx.EventManager().Events())

			created, err = server.CreateProvider(f.Ctx, &types.MsgCreateProvider{
				Authority: sender.String(), Address: manager.String(), PayoutAddress: payout.String(),
			})
			require.NoError(t, err)
			provider, err := k.GetProvider(f.Ctx, created.Uuid)
			require.NoError(t, err)
			require.Equal(t, payout.String(), provider.PayoutAddress)

			before = snapshotSKUStore(t, f)
			eventsBefore = slices.Clone(f.Ctx.EventManager().Events())
			updated, err := server.UpdateProvider(f.Ctx, &types.MsgUpdateProvider{
				Authority: sender.String(), Uuid: created.Uuid, Address: payout.String(),
				PayoutAddress: blocked.String(), MetaHash: []byte("changed"), Active: true,
			})
			require.Nil(t, updated)
			require.ErrorIs(t, err, types.ErrInvalidProvider)
			require.ErrorContains(t, err, "payout address is blocked by bank policy")
			require.Equal(t, before, snapshotSKUStore(t, f), "rejected update must preserve provider fields and indexes")
			require.Equal(t, eventsBefore, f.Ctx.EventManager().Events())

			// Historical state can contain a now-blocked payout. Validation must
			// inspect the requested replacement so administrators can repair it.
			provider.PayoutAddress = blocked.String()
			require.NoError(t, k.SetProvider(f.Ctx, provider))
			_, err = server.UpdateProvider(f.Ctx, &types.MsgUpdateProvider{
				Authority: sender.String(), Uuid: created.Uuid, Address: manager.String(),
				PayoutAddress: payout.String(), Active: true,
			})
			require.NoError(t, err)
			repaired, err := k.GetProvider(f.Ctx, created.Uuid)
			require.NoError(t, err)
			require.Equal(t, payout.String(), repaired.PayoutAddress)
		})
	}
}
