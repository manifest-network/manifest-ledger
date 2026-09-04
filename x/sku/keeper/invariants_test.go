package keeper_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/sku/keeper"
	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

func TestStateInvariant(t *testing.T) {
	f := initFixture(t)
	_, sku := seedInvariantState(t, f)

	message, broken := keeper.StateInvariant(f.App.SKUKeeper)(f.Ctx)
	require.False(t, broken, message)

	sku.ProviderUuid = testProvider2UUID
	require.NoError(t, f.App.SKUKeeper.SetSKU(f.Ctx, sku))
	message, broken = keeper.StateInvariant(f.App.SKUKeeper)(f.Ctx)
	require.True(t, broken)
	require.Contains(t, message, "references non-existent provider")
}

func TestStateInvariantReportsCorruptParamsWithoutPanicking(t *testing.T) {
	f := initFixture(t)
	f.Ctx.KVStore(f.App.GetKey(types.StoreKey)).Set(types.ParamsKey.Bytes(), []byte{0xff})

	message, broken := keeper.StateInvariant(f.App.SKUKeeper)(f.Ctx)
	require.True(t, broken)
	require.Contains(t, message, "failed to export SKU state")
	require.Contains(t, message, "read SKU params")
}

func TestStateInvariantValidatesRawStoredParams(t *testing.T) {
	tests := []struct {
		name   string
		params types.Params
		want   string
	}{
		{
			name: "duplicate decoded identity",
			params: func() types.Params {
				address := sdk.AccAddress([]byte("duplicate-identity__")).String()
				return types.Params{AllowedList: []string{address, strings.ToUpper(address)}}
			}(),
			want: "duplicate address in allowed list",
		},
		{
			name: "over cap equivalent aliases",
			params: func() types.Params {
				address := sdk.AccAddress([]byte("over-cap-identity___")).String()
				allowed := make([]string, types.MaxAllowedListEntries+1)
				for i := range allowed {
					allowed[i] = address
				}
				return types.Params{AllowedList: allowed}
			}(),
			want: "allowed list has 101 entries, maximum allowed is 100",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := initFixture(t)
			require.NoError(t, f.App.SKUKeeper.SetParams(f.Ctx, test.params))

			message, broken := keeper.StateInvariant(f.App.SKUKeeper)(f.Ctx)
			require.True(t, broken)
			require.Contains(t, message, "invalid stored SKU params")
			require.Contains(t, message, test.want)
		})
	}
}

func TestStateInvariant_DetectsSecondaryIndexCorruption(t *testing.T) {
	tests := []struct {
		name     string
		corrupt  func(*testing.T, *testFixture, types.Provider, types.SKU)
		contains string
	}{
		{
			name: "missing provider address row",
			corrupt: func(t *testing.T, f *testFixture, provider types.Provider, _ types.SKU) {
				require.NoError(t, f.App.SKUKeeper.Providers.Indexes.Address.Unreference(
					f.Ctx,
					provider.Uuid,
					func() (types.Provider, error) { return provider, nil },
				))
			},
			contains: "provider address index contains 0 entries, expected 1",
		},
		{
			name: "missing provider active row",
			corrupt: func(t *testing.T, f *testFixture, provider types.Provider, _ types.SKU) {
				require.NoError(t, f.App.SKUKeeper.Providers.Indexes.Active.Unreference(
					f.Ctx,
					provider.Uuid,
					func() (types.Provider, error) { return provider, nil },
				))
			},
			contains: "provider active index contains 0 entries, expected 1",
		},
		{
			name: "missing SKU provider row",
			corrupt: func(t *testing.T, f *testFixture, _ types.Provider, sku types.SKU) {
				require.NoError(t, f.App.SKUKeeper.SKUs.Indexes.Provider.Unreference(
					f.Ctx,
					sku.Uuid,
					func() (types.SKU, error) { return sku, nil },
				))
			},
			contains: "SKU provider index contains 0 entries, expected 1",
		},
		{
			name: "missing SKU active row",
			corrupt: func(t *testing.T, f *testFixture, _ types.Provider, sku types.SKU) {
				require.NoError(t, f.App.SKUKeeper.SKUs.Indexes.Active.Unreference(
					f.Ctx,
					sku.Uuid,
					func() (types.SKU, error) { return sku, nil },
				))
			},
			contains: "SKU active index contains 0 entries, expected 1",
		},
		{
			name: "missing SKU provider-active row",
			corrupt: func(t *testing.T, f *testFixture, _ types.Provider, sku types.SKU) {
				require.NoError(t, f.App.SKUKeeper.SKUs.Indexes.ProviderActive.Unreference(
					f.Ctx,
					sku.Uuid,
					func() (types.SKU, error) { return sku, nil },
				))
			},
			contains: "SKU provider-active index contains 0 entries, expected 1",
		},
		{
			name: "mismatched provider address row",
			corrupt: func(t *testing.T, f *testFixture, provider types.Provider, _ types.SKU) {
				mismatched := provider
				mismatched.Address = f.TestAccs[3].String()
				require.NoError(t, f.App.SKUKeeper.Providers.Indexes.Address.Reference(
					f.Ctx,
					provider.Uuid,
					mismatched,
					func() (types.Provider, error) { return types.Provider{}, collections.ErrNotFound },
				))
			},
			contains: "provider address index key",
		},
		{
			name: "orphaned SKU provider-active row",
			corrupt: func(t *testing.T, f *testFixture, _ types.Provider, sku types.SKU) {
				require.NoError(t, f.App.SKUKeeper.SKUs.Indexes.ProviderActive.Reference(
					f.Ctx,
					testSKU2UUID,
					sku,
					func() (types.SKU, error) { return types.SKU{}, collections.ErrNotFound },
				))
			},
			contains: "SKU provider-active index references missing primary key",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := initFixture(t)
			provider, sku := seedInvariantState(t, f)
			message, broken := keeper.StateInvariant(f.App.SKUKeeper)(f.Ctx)
			require.False(t, broken, message)

			test.corrupt(t, f, provider, sku)
			message, broken = keeper.StateInvariant(f.App.SKUKeeper)(f.Ctx)
			require.True(t, broken)
			require.Contains(t, message, test.contains)
		})
	}
}

func seedInvariantState(t *testing.T, f *testFixture) (types.Provider, types.SKU) {
	t.Helper()
	provider := types.Provider{
		Uuid:          testProviderUUID,
		Address:       f.TestAccs[0].String(),
		PayoutAddress: f.TestAccs[1].String(),
		Active:        true,
	}
	sku := types.SKU{
		Uuid:         testSKU1UUID,
		ProviderUuid: provider.Uuid,
		Name:         "compute",
		Unit:         types.Unit_UNIT_PER_HOUR,
		BasePrice:    sdk.NewCoin("umfx", sdkmath.NewInt(3600)),
		Active:       true,
	}
	require.NoError(t, f.App.SKUKeeper.SetProvider(f.Ctx, provider))
	require.NoError(t, f.App.SKUKeeper.SetSKU(f.Ctx, sku))
	require.NoError(t, f.App.SKUKeeper.ProviderSequence.Set(f.Ctx, 1))
	require.NoError(t, f.App.SKUKeeper.SKUSequence.Set(f.Ctx, 1))
	return provider, sku
}
