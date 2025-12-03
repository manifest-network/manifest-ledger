package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/sku/keeper"
	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

func TestCreateSKU(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, acc := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	err := k.NextID.Set(f.Ctx, 1)
	require.NoError(t, err)

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	type testcase struct {
		name      string
		sender    string
		provider  string
		skuName   string
		unit      types.Unit
		basePrice sdk.Coin
		metaHash  []byte
		errMsg    string
	}

	cases := []testcase{
		{
			name:      "success; create SKU",
			sender:    authority.String(),
			provider:  "provider1",
			skuName:   "Test SKU",
			unit:      types.Unit_UNIT_PER_HOUR,
			basePrice: basePrice,
			metaHash:  []byte("testhash"),
		},
		{
			name:      "fail; unauthorized sender",
			sender:    acc.String(),
			provider:  "provider1",
			skuName:   "Test SKU",
			unit:      types.Unit_UNIT_PER_HOUR,
			basePrice: basePrice,
			errMsg:    "unauthorized",
		},
		{
			name:      "fail; empty provider",
			sender:    authority.String(),
			provider:  "",
			skuName:   "Test SKU",
			unit:      types.Unit_UNIT_PER_HOUR,
			basePrice: basePrice,
			errMsg:    "provider cannot be empty",
		},
		{
			name:      "fail; empty name",
			sender:    authority.String(),
			provider:  "provider1",
			skuName:   "",
			unit:      types.Unit_UNIT_PER_HOUR,
			basePrice: basePrice,
			errMsg:    "name cannot be empty",
		},
		{
			name:      "fail; unspecified unit",
			sender:    authority.String(),
			provider:  "provider1",
			skuName:   "Test SKU",
			unit:      types.Unit_UNIT_UNSPECIFIED,
			basePrice: basePrice,
			errMsg:    "unit cannot be unspecified",
		},
		{
			name:      "fail; zero base price",
			sender:    authority.String(),
			provider:  "provider1",
			skuName:   "Test SKU",
			unit:      types.Unit_UNIT_PER_HOUR,
			basePrice: sdk.NewCoin("umfx", sdkmath.NewInt(0)),
			errMsg:    "base price must be valid and non-zero",
		},
	}

	for _, c := range cases {
		c := c

		t.Run(c.name, func(t *testing.T) {
			msg := &types.MsgCreateSKU{
				Authority: c.sender,
				Provider:  c.provider,
				Name:      c.skuName,
				Unit:      c.unit,
				BasePrice: c.basePrice,
				MetaHash:  c.metaHash,
			}

			resp, err := ms.CreateSKU(f.Ctx, msg)
			if c.errMsg != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, c.errMsg)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Greater(t, resp.Id, uint64(0))

			sku, err := k.GetSKU(f.Ctx, resp.Id)
			require.NoError(t, err)
			require.Equal(t, c.provider, sku.Provider)
			require.Equal(t, c.skuName, sku.Name)
			require.Equal(t, c.unit, sku.Unit)
			require.Equal(t, c.basePrice, sku.BasePrice)
			require.True(t, sku.Active)
		})
	}
}

func TestUpdateSKU(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, acc := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))
	newPrice := sdk.NewCoin("umfx", sdkmath.NewInt(200))

	existingSKU := types.SKU{
		Id:        1,
		Provider:  "provider1",
		Name:      "Original SKU",
		Unit:      types.Unit_UNIT_PER_HOUR,
		BasePrice: basePrice,
		Active:    true,
	}
	err := k.SetSKU(f.Ctx, existingSKU)
	require.NoError(t, err)

	type testcase struct {
		name      string
		sender    string
		provider  string
		id        uint64
		skuName   string
		unit      types.Unit
		basePrice sdk.Coin
		active    bool
		errMsg    string
	}

	cases := []testcase{
		{
			name:      "success; update SKU",
			sender:    authority.String(),
			provider:  "provider1",
			id:        1,
			skuName:   "Updated SKU",
			unit:      types.Unit_UNIT_PER_DAY,
			basePrice: newPrice,
			active:    false,
		},
		{
			name:      "fail; unauthorized sender",
			sender:    acc.String(),
			provider:  "provider1",
			id:        1,
			skuName:   "Updated SKU",
			unit:      types.Unit_UNIT_PER_DAY,
			basePrice: newPrice,
			active:    true,
			errMsg:    "unauthorized",
		},
		{
			name:      "fail; SKU not found",
			sender:    authority.String(),
			provider:  "provider1",
			id:        999,
			skuName:   "Updated SKU",
			unit:      types.Unit_UNIT_PER_DAY,
			basePrice: newPrice,
			active:    true,
			errMsg:    "sku not found",
		},
		{
			name:      "fail; provider mismatch",
			sender:    authority.String(),
			provider:  "provider2",
			id:        1,
			skuName:   "Updated SKU",
			unit:      types.Unit_UNIT_PER_DAY,
			basePrice: newPrice,
			active:    true,
			errMsg:    "provider mismatch",
		},
		{
			name:      "fail; empty name",
			sender:    authority.String(),
			provider:  "provider1",
			id:        1,
			skuName:   "",
			unit:      types.Unit_UNIT_PER_DAY,
			basePrice: newPrice,
			active:    true,
			errMsg:    "name cannot be empty",
		},
	}

	for _, c := range cases {
		c := c

		t.Run(c.name, func(t *testing.T) {
			msg := &types.MsgUpdateSKU{
				Authority: c.sender,
				Provider:  c.provider,
				Id:        c.id,
				Name:      c.skuName,
				Unit:      c.unit,
				BasePrice: c.basePrice,
				Active:    c.active,
			}

			_, err := ms.UpdateSKU(f.Ctx, msg)
			if c.errMsg != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, c.errMsg)
				return
			}
			require.NoError(t, err)

			sku, err := k.GetSKU(f.Ctx, c.id)
			require.NoError(t, err)
			require.Equal(t, c.skuName, sku.Name)
			require.Equal(t, c.unit, sku.Unit)
			require.Equal(t, c.basePrice, sku.BasePrice)
			require.Equal(t, c.active, sku.Active)
		})
	}
}

func TestDeleteSKUMsg(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, acc := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	type testcase struct {
		name     string
		sender   string
		provider string
		id       uint64
		errMsg   string
	}

	cases := []testcase{
		{
			name:     "success; delete SKU",
			sender:   authority.String(),
			provider: "provider1",
			id:       1,
		},
		{
			name:     "fail; unauthorized sender",
			sender:   acc.String(),
			provider: "provider1",
			id:       2,
			errMsg:   "unauthorized",
		},
		{
			name:     "fail; SKU not found",
			sender:   authority.String(),
			provider: "provider1",
			id:       999,
			errMsg:   "sku not found",
		},
		{
			name:     "fail; provider mismatch",
			sender:   authority.String(),
			provider: "provider2",
			id:       3,
			errMsg:   "provider mismatch",
		},
	}

	for i, c := range cases {
		c := c
		idx := i

		sku := types.SKU{
			Id:        uint64(idx + 1), //nolint:gosec // test code, i is always small
			Provider:  "provider1",
			Name:      "Test SKU",
			Unit:      types.Unit_UNIT_PER_HOUR,
			BasePrice: basePrice,
			Active:    true,
		}
		err := k.SetSKU(f.Ctx, sku)
		require.NoError(t, err)

		t.Run(c.name, func(t *testing.T) {
			msg := &types.MsgDeleteSKU{
				Authority: c.sender,
				Provider:  c.provider,
				Id:        c.id,
			}

			_, err := ms.DeleteSKU(f.Ctx, msg)
			if c.errMsg != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, c.errMsg)
				return
			}
			require.NoError(t, err)

			_, err = k.GetSKU(f.Ctx, c.id)
			require.Error(t, err)
			require.ErrorIs(t, err, types.ErrSKUNotFound)
		})
	}
}

func TestCreateMultipleSKUs(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	err := k.NextID.Set(f.Ctx, 1)
	require.NoError(t, err)

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	for i := 0; i < 5; i++ {
		msg := &types.MsgCreateSKU{
			Authority: authority.String(),
			Provider:  "provider1",
			Name:      "SKU",
			Unit:      types.Unit_UNIT_PER_HOUR,
			BasePrice: basePrice,
		}

		resp, err := ms.CreateSKU(f.Ctx, msg)
		require.NoError(t, err)
		require.Equal(t, uint64(i+1), resp.Id) //nolint:gosec // test code, i is always small
	}

	allSKUs, err := k.GetAllSKUs(f.Ctx)
	require.NoError(t, err)
	require.Len(t, allSKUs, 5)
}

func TestUpdateParams(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, allowedAddr := testdata.KeyTestPubAddr()
	_, _, otherAddr := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	type testcase struct {
		name   string
		sender string
		params types.Params
		errMsg string
	}

	cases := []testcase{
		{
			name:   "success; update params with allowed list",
			sender: authority.String(),
			params: types.Params{
				AllowedList: []string{allowedAddr.String()},
			},
		},
		{
			name:   "fail; unauthorized sender",
			sender: otherAddr.String(),
			params: types.Params{
				AllowedList: []string{allowedAddr.String()},
			},
			errMsg: "unauthorized",
		},
		{
			name:   "success; empty allowed list",
			sender: authority.String(),
			params: types.Params{
				AllowedList: []string{},
			},
		},
	}

	for _, c := range cases {
		c := c

		t.Run(c.name, func(t *testing.T) {
			msg := &types.MsgUpdateParams{
				Authority: c.sender,
				Params:    c.params,
			}

			_, err := ms.UpdateParams(f.Ctx, msg)
			if c.errMsg != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, c.errMsg)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestAllowedListCreateSKU(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, allowedAddr := testdata.KeyTestPubAddr()
	_, _, unauthorizedAddr := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	err := k.NextID.Set(f.Ctx, 1)
	require.NoError(t, err)

	// Set params with allowedAddr in allowed list
	params := types.Params{
		AllowedList: []string{allowedAddr.String()},
	}
	err = k.SetParams(f.Ctx, params)
	require.NoError(t, err)

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	// Test that allowed address can create SKU
	msg := &types.MsgCreateSKU{
		Authority: allowedAddr.String(),
		Provider:  "provider1",
		Name:      "Test SKU",
		Unit:      types.Unit_UNIT_PER_HOUR,
		BasePrice: basePrice,
	}

	resp, err := ms.CreateSKU(f.Ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, uint64(1), resp.Id)

	// Test that unauthorized address cannot create SKU
	msg = &types.MsgCreateSKU{
		Authority: unauthorizedAddr.String(),
		Provider:  "provider2",
		Name:      "Test SKU 2",
		Unit:      types.Unit_UNIT_PER_HOUR,
		BasePrice: basePrice,
	}

	_, err = ms.CreateSKU(f.Ctx, msg)
	require.Error(t, err)
	require.ErrorContains(t, err, "unauthorized")

	// Test that authority can still create SKU
	msg = &types.MsgCreateSKU{
		Authority: authority.String(),
		Provider:  "provider3",
		Name:      "Test SKU 3",
		Unit:      types.Unit_UNIT_PER_HOUR,
		BasePrice: basePrice,
	}

	resp, err = ms.CreateSKU(f.Ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestParamsAllowedListRemoval(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, allowedAddr := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	err := k.NextID.Set(f.Ctx, 1)
	require.NoError(t, err)

	// Set params with allowedAddr
	params := types.Params{
		AllowedList: []string{allowedAddr.String()},
	}
	err = k.SetParams(f.Ctx, params)
	require.NoError(t, err)

	// Verify address is in allowed list
	gotParams, err := k.GetParams(f.Ctx)
	require.NoError(t, err)
	require.True(t, gotParams.IsAllowed(allowedAddr.String()))

	// Remove via UpdateParams message
	updateMsg := &types.MsgUpdateParams{
		Authority: authority.String(),
		Params: types.Params{
			AllowedList: []string{}, // Empty list removes the address
		},
	}

	_, err = ms.UpdateParams(f.Ctx, updateMsg)
	require.NoError(t, err)

	// Verify address is no longer in allowed list
	gotParams, err = k.GetParams(f.Ctx)
	require.NoError(t, err)
	require.False(t, gotParams.IsAllowed(allowedAddr.String()))

	// Verify removed address cannot create SKU
	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))
	createMsg := &types.MsgCreateSKU{
		Authority: allowedAddr.String(),
		Provider:  "provider1",
		Name:      "Test SKU",
		Unit:      types.Unit_UNIT_PER_HOUR,
		BasePrice: basePrice,
	}

	_, err = ms.CreateSKU(f.Ctx, createMsg)
	require.Error(t, err)
	require.ErrorContains(t, err, "unauthorized")
}
