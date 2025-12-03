package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/manifest-network/manifest-ledger/x/sku/keeper"
	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

func TestQuerierSKUsPagination(t *testing.T) {
	f := initFixture(t)
	k := f.App.SKUKeeper
	q := keeper.NewQuerier(k)

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	// Create 5 SKUs
	for i := uint64(1); i <= 5; i++ {
		sku := types.SKU{
			Id:        i,
			Provider:  "test-provider",
			Name:      "SKU",
			Unit:      types.Unit_UNIT_PER_HOUR,
			BasePrice: basePrice,
			Active:    true,
		}
		err := k.SKUs.Set(f.Ctx, i, sku)
		require.NoError(t, err)
	}

	// Query first page
	res1, err := q.SKUs(f.Ctx, &types.QuerySKUsRequest{
		Pagination: &query.PageRequest{Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, res1.Skus, 2)
	require.NotNil(t, res1.Pagination)
	require.NotEmpty(t, res1.Pagination.NextKey)

	t.Logf("First page SKU IDs: %d, %d", res1.Skus[0].Id, res1.Skus[1].Id)
	t.Logf("NextKey: %x", res1.Pagination.NextKey)

	// Query second page using next key
	res2, err := q.SKUs(f.Ctx, &types.QuerySKUsRequest{
		Pagination: &query.PageRequest{Key: res1.Pagination.NextKey, Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, res2.Skus, 2, "second page should have 2 SKUs")

	t.Logf("Second page SKU IDs: %d, %d", res2.Skus[0].Id, res2.Skus[1].Id)

	// Query third page
	res3, err := q.SKUs(f.Ctx, &types.QuerySKUsRequest{
		Pagination: &query.PageRequest{Key: res2.Pagination.NextKey, Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, res3.Skus, 1, "third page should have 1 SKU")

	t.Logf("Third page SKU IDs: %d", res3.Skus[0].Id)
}
