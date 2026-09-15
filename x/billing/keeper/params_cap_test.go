package keeper_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	sdkcodec "github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func billingCapAllowedList(size int) []string {
	addresses := make([]string, size)
	for i := range addresses {
		addresses[i] = sdk.AccAddress(bytes.Repeat([]byte{byte(i + 1)}, 20)).String()
	}
	return addresses
}

func billingCapReservedSuffixes(size int) []string {
	suffixes := make([]string, size)
	for i := range suffixes {
		suffixes[i] = fmt.Sprintf(".zone-%03d.example.com", i)
	}
	return suffixes
}

func TestMsgUpdateParamsRejectsOversizedListsWithoutPersisting(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*types.Params)
		want   string
	}{
		{
			name: "allowed list",
			mutate: func(params *types.Params) {
				params.AllowedList = billingCapAllowedList(types.MaxAllowedListEntries + 1)
			},
			want: "allowed list has 101 entries, maximum allowed is 100",
		},
		{
			name: "reserved domain suffixes",
			mutate: func(params *types.Params) {
				params.ReservedDomainSuffixes = billingCapReservedSuffixes(types.MaxReservedDomainSuffixEntries + 1)
			},
			want: "reserved domain suffix list has 101 entries, maximum allowed is 100",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := initFixture(t)
			k := f.App.BillingKeeper
			before := types.DefaultParams()
			before.MaxLeasesPerTenant--
			require.NoError(t, k.SetParams(f.Ctx, before))

			proposed := types.DefaultParams()
			test.mutate(&proposed)
			response, err := keeper.NewMsgServerImpl(k).UpdateParams(f.Ctx, &types.MsgUpdateParams{
				Authority: f.Authority.String(),
				Params:    proposed,
			})

			require.Nil(t, response)
			require.ErrorIs(t, err, types.ErrInvalidParams)
			require.ErrorContains(t, err, test.want)
			after, getErr := k.GetParams(f.Ctx)
			require.NoError(t, getErr)
			require.Equal(t, before, after, "a rejected parameter update must not change stored params")
		})
	}
}

func TestMigratorMigrate2to3RejectsOversizedReservedSuffixesBeforeWrite(t *testing.T) {
	f := initFixture(t)
	params := types.DefaultParams()
	params.ReservedDomainSuffixes = billingCapReservedSuffixes(types.MaxReservedDomainSuffixEntries + 1)
	legacyParams, err := sdkcodec.CollValue[types.Params](f.EncodingCfg.Codec).Encode(params)
	require.NoError(t, err)

	store := f.Ctx.KVStore(f.App.GetKey(types.StoreKey))
	store.Set(types.ParamsKey.Bytes(), legacyParams)
	before := bytes.Clone(store.Get(types.ParamsKey.Bytes()))
	require.False(t, bytes.HasPrefix(before, []byte(testParamsStoragePrefix)),
		"fixture must use the legacy encoding so an early current-codec write changes bytes")
	err = keeper.NewMigrator(f.App.BillingKeeper).Migrate2to3(f.Ctx)

	require.ErrorIs(t, err, types.ErrInvalidParams)
	require.ErrorContains(t, err, "reserved domain suffix list has 101 entries, maximum allowed is 100")
	require.Equal(t, before, store.Get(types.ParamsKey.Bytes()), "validation must fail before the first migration write")
}

func TestMigratorMigrate2to3AcceptsOversizedEquivalentAllowedList(t *testing.T) {
	f := initFixture(t)
	allowed := f.TestAccs[0]
	params := types.DefaultParams()
	params.AllowedList = make([]string, types.MaxAllowedListEntries+1)
	for i := range params.AllowedList {
		if i%2 == 0 {
			params.AllowedList[i] = allowed.String()
		} else {
			params.AllowedList[i] = strings.ToUpper(allowed.String())
		}
	}
	require.ErrorIs(t, params.Validate(), types.ErrInvalidParams)

	legacyParams, err := sdkcodec.CollValue[types.Params](f.EncodingCfg.Codec).Encode(params)
	require.NoError(t, err)
	store := f.Ctx.KVStore(f.App.GetKey(types.StoreKey))
	store.Set(types.ParamsKey.Bytes(), legacyParams)

	require.NoError(t, keeper.NewMigrator(f.App.BillingKeeper).Migrate2to3(f.Ctx))
	migrated, err := f.App.BillingKeeper.GetParams(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, []string{allowed.String()}, migrated.AllowedList)
}
