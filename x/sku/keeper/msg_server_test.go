// Package keeper_test contains unit tests for the SKU module's message server.
//
// # Test Coverage
//
// ## Provider Tests
//
// TestCreateProvider:
//   - Success: authority creates provider with valid addresses
//   - Fail: unauthorized sender (not authority or in allowed list)
//   - Fail: invalid provider address format
//   - Fail: invalid payout address format
//
// TestUpdateProvider:
//   - Success: authority updates provider (address, payout address, active status)
//   - Fail: unauthorized sender
//   - Fail: provider not found
//   - Fail: zero provider ID
//
// TestDeactivateProvider:
//   - Success: authority deactivates active provider (soft delete)
//   - Fail: unauthorized sender
//   - Fail: provider not found
//   - Fail: provider already inactive
//   - Fail: zero provider ID
//
// ## SKU Tests
//
// TestCreateSKU:
//   - Success: authority creates SKU for active provider
//   - Fail: unauthorized sender
//   - Fail: provider not found
//   - Fail: provider is inactive (cannot create SKU for inactive provider)
//   - Fail: empty SKU name
//   - Fail: unspecified unit type
//   - Fail: zero base price
//   - Fail: zero provider_id
//
// TestUpdateSKU:
//   - Success: authority updates SKU (name, unit, price, active status)
//   - Fail: unauthorized sender
//   - Fail: SKU not found
//   - Fail: provider_id mismatch (SKU belongs to different provider)
//   - Fail: empty SKU name
//   - Fail: zero provider_id
//
// TestDeactivateSKU:
//   - Success: authority deactivates active SKU (soft delete)
//   - Fail: unauthorized sender
//   - Fail: SKU not found
//   - Fail: SKU already inactive
//
// ## Params Tests
//
// TestUpdateParams:
//   - Success: authority updates params with new allowed list
//   - Fail: unauthorized sender (non-authority, even if in allowed list)
//
// ## Allowed List Tests
//
// TestAllowedListAuthorization:
//   - Success: address in allowed list can create provider
//   - Success: address in allowed list can create SKU
//   - Success: address in allowed list can update SKU
//   - Success: address in allowed list can deactivate SKU
//   - Fail: address not in allowed list cannot create provider
//   - Fail: address not in allowed list cannot create SKU
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

func TestCreateProvider(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, acc := testdata.KeyTestPubAddr()
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	type testcase struct {
		name          string
		sender        string
		address       string
		payoutAddress string
		metaHash      []byte
		errMsg        string
	}

	cases := []testcase{
		{
			name:          "success; create provider",
			sender:        authority.String(),
			address:       providerAddr.String(),
			payoutAddress: payoutAddr.String(),
			metaHash:      []byte("testhash"),
		},
		{
			name:          "fail; unauthorized sender",
			sender:        acc.String(),
			address:       providerAddr.String(),
			payoutAddress: payoutAddr.String(),
			errMsg:        "unauthorized",
		},
		{
			name:          "fail; invalid provider address",
			sender:        authority.String(),
			address:       "invalid",
			payoutAddress: payoutAddr.String(),
			errMsg:        "invalid provider address",
		},
		{
			name:          "fail; invalid payout address",
			sender:        authority.String(),
			address:       providerAddr.String(),
			payoutAddress: "invalid",
			errMsg:        "invalid payout address",
		},
	}

	for _, c := range cases {
		c := c

		t.Run(c.name, func(t *testing.T) {
			msg := &types.MsgCreateProvider{
				Authority:     c.sender,
				Address:       c.address,
				PayoutAddress: c.payoutAddress,
				MetaHash:      c.metaHash,
			}

			resp, err := ms.CreateProvider(f.Ctx, msg)
			if c.errMsg != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, c.errMsg)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.NotEmpty(t, resp.Uuid)

			provider, err := k.GetProvider(f.Ctx, resp.Uuid)
			require.NoError(t, err)
			require.Equal(t, c.address, provider.Address)
			require.Equal(t, c.payoutAddress, provider.PayoutAddress)
			require.True(t, provider.Active)
		})
	}
}

func TestUpdateProvider(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, acc := testdata.KeyTestPubAddr()
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()
	_, _, newPayoutAddr := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	providerUUID := "01912345-6789-7abc-8def-0123456789ab"
	existingProvider := types.Provider{
		Uuid:          providerUUID,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        true,
	}
	err := k.SetProvider(f.Ctx, existingProvider)
	require.NoError(t, err)

	type testcase struct {
		name          string
		sender        string
		uuid          string
		address       string
		payoutAddress string
		active        bool
		errMsg        string
	}

	cases := []testcase{
		{
			name:          "success; update provider",
			sender:        authority.String(),
			uuid:          providerUUID,
			address:       providerAddr.String(),
			payoutAddress: newPayoutAddr.String(),
			active:        false,
		},
		{
			name:          "fail; unauthorized sender",
			sender:        acc.String(),
			uuid:          providerUUID,
			address:       providerAddr.String(),
			payoutAddress: newPayoutAddr.String(),
			active:        true,
			errMsg:        "unauthorized",
		},
		{
			name:          "fail; provider not found",
			sender:        authority.String(),
			uuid:          "01912345-6789-7abc-8def-999999999999",
			address:       providerAddr.String(),
			payoutAddress: newPayoutAddr.String(),
			active:        true,
			errMsg:        "not found",
		},
		{
			name:          "fail; empty uuid",
			sender:        authority.String(),
			uuid:          "",
			address:       providerAddr.String(),
			payoutAddress: newPayoutAddr.String(),
			active:        true,
			errMsg:        "uuid cannot be empty",
		},
	}

	for _, c := range cases {
		c := c

		t.Run(c.name, func(t *testing.T) {
			msg := &types.MsgUpdateProvider{
				Authority:     c.sender,
				Uuid:          c.uuid,
				Address:       c.address,
				PayoutAddress: c.payoutAddress,
				Active:        c.active,
			}

			_, err := ms.UpdateProvider(f.Ctx, msg)
			if c.errMsg != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, c.errMsg)
				return
			}
			require.NoError(t, err)

			provider, err := k.GetProvider(f.Ctx, c.uuid)
			require.NoError(t, err)
			require.Equal(t, c.payoutAddress, provider.PayoutAddress)
			require.Equal(t, c.active, provider.Active)
		})
	}
}

func TestDeactivateProvider(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, acc := testdata.KeyTestPubAddr()
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	// Create providers for testing with UUIDs
	providerUUIDs := []string{
		"01912345-6789-7abc-8def-0123456789a1",
		"01912345-6789-7abc-8def-0123456789a2",
		"01912345-6789-7abc-8def-0123456789a3",
	}
	for _, uuid := range providerUUIDs {
		provider := types.Provider{
			Uuid:          uuid,
			Address:       providerAddr.String(),
			PayoutAddress: payoutAddr.String(),
			Active:        true,
		}
		err := k.SetProvider(f.Ctx, provider)
		require.NoError(t, err)
	}

	// Create an already inactive provider
	inactiveUUID := "01912345-6789-7abc-8def-0123456789a4"
	inactiveProvider := types.Provider{
		Uuid:          inactiveUUID,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        false,
	}
	err := k.SetProvider(f.Ctx, inactiveProvider)
	require.NoError(t, err)

	type testcase struct {
		name   string
		sender string
		uuid   string
		errMsg string
	}

	cases := []testcase{
		{
			name:   "success; deactivate provider",
			sender: authority.String(),
			uuid:   providerUUIDs[0],
		},
		{
			name:   "fail; unauthorized sender",
			sender: acc.String(),
			uuid:   providerUUIDs[1],
			errMsg: "unauthorized",
		},
		{
			name:   "fail; provider not found",
			sender: authority.String(),
			uuid:   "01912345-6789-7abc-8def-999999999999",
			errMsg: "not found",
		},
		{
			name:   "fail; already inactive",
			sender: authority.String(),
			uuid:   inactiveUUID,
			errMsg: "already inactive",
		},
		{
			name:   "fail; empty uuid",
			sender: authority.String(),
			uuid:   "",
			errMsg: "uuid cannot be empty",
		},
	}

	for _, c := range cases {
		c := c

		t.Run(c.name, func(t *testing.T) {
			msg := &types.MsgDeactivateProvider{
				Authority: c.sender,
				Uuid:      c.uuid,
			}

			_, err := ms.DeactivateProvider(f.Ctx, msg)
			if c.errMsg != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, c.errMsg)
				return
			}
			require.NoError(t, err)

			// Verify provider still exists but is inactive
			provider, err := k.GetProvider(f.Ctx, c.uuid)
			require.NoError(t, err)
			require.False(t, provider.Active, "provider should be inactive after deactivation")
		})
	}
}

func TestCreateSKU(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, acc := testdata.KeyTestPubAddr()
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	// Create active provider
	activeProviderUUID := "01912345-6789-7abc-8def-0123456789a1"
	activeProvider := types.Provider{
		Uuid:          activeProviderUUID,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        true,
	}
	err := k.SetProvider(f.Ctx, activeProvider)
	require.NoError(t, err)

	// Create inactive provider
	inactiveProviderUUID := "01912345-6789-7abc-8def-0123456789a2"
	inactiveProvider := types.Provider{
		Uuid:          inactiveProviderUUID,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        false,
	}
	err = k.SetProvider(f.Ctx, inactiveProvider)
	require.NoError(t, err)

	// Use a price that produces a non-zero per-second rate (3600 / 3600 = 1 per second)
	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(3600))

	type testcase struct {
		name         string
		sender       string
		providerUUID string
		skuName      string
		unit         types.Unit
		basePrice    sdk.Coin
		metaHash     []byte
		errMsg       string
	}

	cases := []testcase{
		{
			name:         "success; create SKU",
			sender:       authority.String(),
			providerUUID: activeProviderUUID,
			skuName:      "Test SKU",
			unit:         types.Unit_UNIT_PER_HOUR,
			basePrice:    basePrice,
			metaHash:     []byte("testhash"),
		},
		{
			name:         "fail; unauthorized sender",
			sender:       acc.String(),
			providerUUID: activeProviderUUID,
			skuName:      "Test SKU",
			unit:         types.Unit_UNIT_PER_HOUR,
			basePrice:    basePrice,
			errMsg:       "unauthorized",
		},
		{
			name:         "fail; provider not found",
			sender:       authority.String(),
			providerUUID: "01912345-6789-7abc-8def-999999999999",
			skuName:      "Test SKU",
			unit:         types.Unit_UNIT_PER_HOUR,
			basePrice:    basePrice,
			errMsg:       "not found",
		},
		{
			name:         "fail; inactive provider",
			sender:       authority.String(),
			providerUUID: inactiveProviderUUID,
			skuName:      "Test SKU",
			unit:         types.Unit_UNIT_PER_HOUR,
			basePrice:    basePrice,
			errMsg:       "not active",
		},
		{
			name:         "fail; empty name",
			sender:       authority.String(),
			providerUUID: activeProviderUUID,
			skuName:      "",
			unit:         types.Unit_UNIT_PER_HOUR,
			basePrice:    basePrice,
			errMsg:       "name cannot be empty",
		},
		{
			name:         "fail; unspecified unit",
			sender:       authority.String(),
			providerUUID: activeProviderUUID,
			skuName:      "Test SKU",
			unit:         types.Unit_UNIT_UNSPECIFIED,
			basePrice:    basePrice,
			errMsg:       "unit cannot be unspecified",
		},
		{
			name:         "fail; zero base price",
			sender:       authority.String(),
			providerUUID: activeProviderUUID,
			skuName:      "Test SKU",
			unit:         types.Unit_UNIT_PER_HOUR,
			basePrice:    sdk.NewCoin("umfx", sdkmath.NewInt(0)),
			errMsg:       "base price must be valid and non-zero",
		},
		{
			name:         "fail; empty provider_uuid",
			sender:       authority.String(),
			providerUUID: "",
			skuName:      "Test SKU",
			unit:         types.Unit_UNIT_PER_HOUR,
			basePrice:    basePrice,
			errMsg:       "invalid provider_uuid",
		},
	}

	for _, c := range cases {
		c := c

		t.Run(c.name, func(t *testing.T) {
			msg := &types.MsgCreateSKU{
				Authority:    c.sender,
				ProviderUuid: c.providerUUID,
				Name:         c.skuName,
				Unit:         c.unit,
				BasePrice:    c.basePrice,
				MetaHash:     c.metaHash,
			}

			resp, err := ms.CreateSKU(f.Ctx, msg)
			if c.errMsg != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, c.errMsg)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.NotEmpty(t, resp.Uuid)

			sku, err := k.GetSKU(f.Ctx, resp.Uuid)
			require.NoError(t, err)
			require.Equal(t, c.providerUUID, sku.ProviderUuid)
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
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	// Use prices that produce non-zero per-second rates
	// For UNIT_PER_HOUR: 3600 / 3600 = 1 per second
	// For UNIT_PER_DAY: 86400 / 86400 = 1 per second
	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(3600))
	newPrice := sdk.NewCoin("umfx", sdkmath.NewInt(86400))

	// Create provider
	providerUUID := "01912345-6789-7abc-8def-0123456789a1"
	provider := types.Provider{
		Uuid:          providerUUID,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        true,
	}
	err := k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	skuUUID := "01912345-6789-7abc-8def-0123456789b1"
	existingSKU := types.SKU{
		Uuid:         skuUUID,
		ProviderUuid: providerUUID,
		Name:         "Original SKU",
		Unit:         types.Unit_UNIT_PER_HOUR,
		BasePrice:    basePrice,
		Active:       true,
	}
	err = k.SetSKU(f.Ctx, existingSKU)
	require.NoError(t, err)

	type testcase struct {
		name         string
		sender       string
		uuid         string
		providerUUID string
		skuName      string
		unit         types.Unit
		basePrice    sdk.Coin
		active       bool
		errMsg       string
	}

	cases := []testcase{
		{
			name:         "success; update SKU",
			sender:       authority.String(),
			uuid:         skuUUID,
			providerUUID: providerUUID,
			skuName:      "Updated SKU",
			unit:         types.Unit_UNIT_PER_DAY,
			basePrice:    newPrice,
			active:       false,
		},
		{
			name:         "fail; unauthorized sender",
			sender:       acc.String(),
			uuid:         skuUUID,
			providerUUID: providerUUID,
			skuName:      "Updated SKU",
			unit:         types.Unit_UNIT_PER_DAY,
			basePrice:    newPrice,
			active:       true,
			errMsg:       "unauthorized",
		},
		{
			name:         "fail; SKU not found",
			sender:       authority.String(),
			uuid:         "01912345-6789-7abc-8def-999999999999",
			providerUUID: providerUUID,
			skuName:      "Updated SKU",
			unit:         types.Unit_UNIT_PER_DAY,
			basePrice:    newPrice,
			active:       true,
			errMsg:       "sku not found",
		},
		{
			name:         "fail; provider mismatch",
			sender:       authority.String(),
			uuid:         skuUUID,
			providerUUID: "01912345-6789-7abc-8def-0123456789a2",
			skuName:      "Updated SKU",
			unit:         types.Unit_UNIT_PER_DAY,
			basePrice:    newPrice,
			active:       true,
			errMsg:       "provider_uuid mismatch",
		},
		{
			name:         "fail; empty name",
			sender:       authority.String(),
			uuid:         skuUUID,
			providerUUID: providerUUID,
			skuName:      "",
			unit:         types.Unit_UNIT_PER_DAY,
			basePrice:    newPrice,
			active:       true,
			errMsg:       "name cannot be empty",
		},
		{
			name:         "fail; empty provider_uuid",
			sender:       authority.String(),
			uuid:         skuUUID,
			providerUUID: "",
			skuName:      "Updated SKU",
			unit:         types.Unit_UNIT_PER_DAY,
			basePrice:    newPrice,
			active:       true,
			errMsg:       "invalid provider_uuid",
		},
	}

	for _, c := range cases {
		c := c

		t.Run(c.name, func(t *testing.T) {
			msg := &types.MsgUpdateSKU{
				Authority:    c.sender,
				Uuid:         c.uuid,
				ProviderUuid: c.providerUUID,
				Name:         c.skuName,
				Unit:         c.unit,
				BasePrice:    c.basePrice,
				Active:       c.active,
			}

			_, err := ms.UpdateSKU(f.Ctx, msg)
			if c.errMsg != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, c.errMsg)
				return
			}
			require.NoError(t, err)

			sku, err := k.GetSKU(f.Ctx, c.uuid)
			require.NoError(t, err)
			require.Equal(t, c.skuName, sku.Name)
			require.Equal(t, c.unit, sku.Unit)
			require.Equal(t, c.basePrice, sku.BasePrice)
			require.Equal(t, c.active, sku.Active)
		})
	}
}

func TestDeactivateSKUMsg(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, acc := testdata.KeyTestPubAddr()
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	// Create provider
	providerUUID := "01912345-6789-7abc-8def-0123456789a1"
	provider := types.Provider{
		Uuid:          providerUUID,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        true,
	}
	err := k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	// Create SKUs for testing with UUIDs
	skuUUIDs := []string{
		"01912345-6789-7abc-8def-0123456789b1",
		"01912345-6789-7abc-8def-0123456789b2",
		"01912345-6789-7abc-8def-0123456789b3",
		"01912345-6789-7abc-8def-0123456789b4",
	}
	for _, uuid := range skuUUIDs {
		sku := types.SKU{
			Uuid:         uuid,
			ProviderUuid: providerUUID,
			Name:         "Test SKU",
			Unit:         types.Unit_UNIT_PER_HOUR,
			BasePrice:    basePrice,
			Active:       true,
		}
		err := k.SetSKU(f.Ctx, sku)
		require.NoError(t, err)
	}

	// Create an already inactive SKU
	inactiveSKUUUID := "01912345-6789-7abc-8def-0123456789b5"
	inactiveSKU := types.SKU{
		Uuid:         inactiveSKUUUID,
		ProviderUuid: providerUUID,
		Name:         "Inactive SKU",
		Unit:         types.Unit_UNIT_PER_HOUR,
		BasePrice:    basePrice,
		Active:       false,
	}
	err = k.SetSKU(f.Ctx, inactiveSKU)
	require.NoError(t, err)

	type testcase struct {
		name   string
		sender string
		uuid   string
		errMsg string
	}

	cases := []testcase{
		{
			name:   "success; deactivate SKU",
			sender: authority.String(),
			uuid:   skuUUIDs[0],
		},
		{
			name:   "fail; unauthorized sender",
			sender: acc.String(),
			uuid:   skuUUIDs[1],
			errMsg: "unauthorized",
		},
		{
			name:   "fail; SKU not found",
			sender: authority.String(),
			uuid:   "01912345-6789-7abc-8def-999999999999",
			errMsg: "sku not found",
		},
		{
			name:   "fail; already inactive",
			sender: authority.String(),
			uuid:   inactiveSKUUUID,
			errMsg: "already inactive",
		},
		{
			name:   "fail; empty uuid",
			sender: authority.String(),
			uuid:   "",
			errMsg: "uuid cannot be empty",
		},
	}

	for _, c := range cases {
		c := c

		t.Run(c.name, func(t *testing.T) {
			msg := &types.MsgDeactivateSKU{
				Authority: c.sender,
				Uuid:      c.uuid,
			}

			_, err := ms.DeactivateSKU(f.Ctx, msg)
			if c.errMsg != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, c.errMsg)
				return
			}
			require.NoError(t, err)

			// Verify SKU still exists but is inactive
			sku, err := k.GetSKU(f.Ctx, c.uuid)
			require.NoError(t, err)
			require.False(t, sku.Active, "SKU should be inactive after deactivation")
		})
	}
}

func TestCreateMultipleSKUs(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	// Create provider
	providerUUID := "01912345-6789-7abc-8def-0123456789a1"
	provider := types.Provider{
		Uuid:          providerUUID,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        true,
	}
	err := k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	// Use a price that produces a non-zero per-second rate
	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(3600))

	createdUUIDs := make([]string, 5)
	for i := 0; i < 5; i++ {
		msg := &types.MsgCreateSKU{
			Authority:    authority.String(),
			ProviderUuid: providerUUID,
			Name:         "SKU",
			Unit:         types.Unit_UNIT_PER_HOUR,
			BasePrice:    basePrice,
		}

		resp, err := ms.CreateSKU(f.Ctx, msg)
		require.NoError(t, err)
		require.NotEmpty(t, resp.Uuid)
		createdUUIDs[i] = resp.Uuid
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
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	// Create provider
	providerUUID := "01912345-6789-7abc-8def-0123456789a1"
	provider := types.Provider{
		Uuid:          providerUUID,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        true,
	}
	err := k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	// Set params with allowedAddr in allowed list
	params := types.Params{
		AllowedList: []string{allowedAddr.String()},
	}
	err = k.SetParams(f.Ctx, params)
	require.NoError(t, err)

	// Use a price that produces a non-zero per-second rate
	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(3600))

	// Test that allowed address can create SKU
	msg := &types.MsgCreateSKU{
		Authority:    allowedAddr.String(),
		ProviderUuid: providerUUID,
		Name:         "Test SKU",
		Unit:         types.Unit_UNIT_PER_HOUR,
		BasePrice:    basePrice,
	}

	resp, err := ms.CreateSKU(f.Ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.Uuid)

	// Test that unauthorized address cannot create SKU
	msg = &types.MsgCreateSKU{
		Authority:    unauthorizedAddr.String(),
		ProviderUuid: providerUUID,
		Name:         "Test SKU 2",
		Unit:         types.Unit_UNIT_PER_HOUR,
		BasePrice:    basePrice,
	}

	_, err = ms.CreateSKU(f.Ctx, msg)
	require.Error(t, err)
	require.ErrorContains(t, err, "unauthorized")

	// Test that authority can still create SKU
	msg = &types.MsgCreateSKU{
		Authority:    authority.String(),
		ProviderUuid: providerUUID,
		Name:         "Test SKU 3",
		Unit:         types.Unit_UNIT_PER_HOUR,
		BasePrice:    basePrice,
	}

	resp, err = ms.CreateSKU(f.Ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestParamsAllowedListRemoval(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, allowedAddr := testdata.KeyTestPubAddr()
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	// Create provider
	providerUUID := "01912345-6789-7abc-8def-0123456789a1"
	provider := types.Provider{
		Uuid:          providerUUID,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        true,
	}
	err := k.SetProvider(f.Ctx, provider)
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
	// Use a price that produces a non-zero per-second rate
	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(3600))
	createMsg := &types.MsgCreateSKU{
		Authority:    allowedAddr.String(),
		ProviderUuid: providerUUID,
		Name:         "Test SKU",
		Unit:         types.Unit_UNIT_PER_HOUR,
		BasePrice:    basePrice,
	}

	_, err = ms.CreateSKU(f.Ctx, createMsg)
	require.Error(t, err)
	require.ErrorContains(t, err, "unauthorized")
}
