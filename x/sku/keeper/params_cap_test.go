package keeper_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	sdkcodec "github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/sku/keeper"
	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

func skuCapAllowedList(size int) []string {
	addresses := make([]string, size)
	for i := range addresses {
		addresses[i] = sdk.AccAddress(bytes.Repeat([]byte{byte(i + 1)}, 20)).String()
	}
	return addresses
}

func TestMsgUpdateParamsRejectsOversizedAllowedListWithoutPersisting(t *testing.T) {
	f := initFixture(t)
	k := f.App.SKUKeeper
	authority := f.TestAccs[0]
	k.SetAuthority(authority.String())
	before := types.Params{AllowedList: []string{f.TestAccs[1].String()}}
	require.NoError(t, k.SetParams(f.Ctx, before))

	response, err := keeper.NewMsgServerImpl(k).UpdateParams(f.Ctx, &types.MsgUpdateParams{
		Authority: authority.String(),
		Params: types.Params{
			AllowedList: skuCapAllowedList(types.MaxAllowedListEntries + 1),
		},
	})

	require.Nil(t, response)
	require.ErrorIs(t, err, types.ErrInvalidConfig)
	require.ErrorContains(t, err, "allowed list has 101 entries, maximum allowed is 100")
	after, getErr := k.GetParams(f.Ctx)
	require.NoError(t, getErr)
	require.Equal(t, before, after, "a rejected parameter update must not change stored params")
}

func TestMigratorMigrate1to2AcceptsOversizedEquivalentAllowedList(t *testing.T) {
	f := initFixture(t)
	k := f.App.SKUKeeper
	allowed := f.TestAccs[0]
	params := types.Params{AllowedList: make([]string, types.MaxAllowedListEntries+1)}
	for i := range params.AllowedList {
		if i%2 == 0 {
			params.AllowedList[i] = allowed.String()
		} else {
			params.AllowedList[i] = strings.ToUpper(allowed.String())
		}
	}
	require.ErrorIs(t, params.Validate(), types.ErrInvalidConfig)

	legacyParams, err := sdkcodec.CollValue[types.Params](f.EncodingCfg.Codec).Encode(params)
	require.NoError(t, err)
	f.Ctx.KVStore(f.App.GetKey(types.StoreKey)).Set(types.ParamsKey.Bytes(), legacyParams)

	require.NoError(t, keeper.NewMigrator(k).Migrate1to2(f.Ctx))
	migrated, err := k.GetParams(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, []string{allowed.String()}, migrated.AllowedList)
}
