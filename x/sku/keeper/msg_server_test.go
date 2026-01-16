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

	existingProvider := types.Provider{
		Uuid:          testProviderUUID,
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
			uuid:          testProviderUUID,
			address:       providerAddr.String(),
			payoutAddress: newPayoutAddr.String(),
			active:        false,
		},
		{
			name:          "fail; unauthorized sender",
			sender:        acc.String(),
			uuid:          testProviderUUID,
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

func TestProviderReactivation(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, allowedUser := testdata.KeyTestPubAddr()
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()
	_, _, newPayoutAddr := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	providerUUID2 := "01912345-6789-7abc-8def-0123456789ac"

	// Add allowedUser to the allowed list
	err := k.SetParams(f.Ctx, types.Params{
		AllowedList: []string{allowedUser.String()},
	})
	require.NoError(t, err)

	// Create an INACTIVE provider for allowed user test
	inactiveProvider := types.Provider{
		Uuid:          testProviderUUID,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        false,
	}
	err = k.SetProvider(f.Ctx, inactiveProvider)
	require.NoError(t, err)

	// Create another INACTIVE provider for authority test
	inactiveProvider2 := types.Provider{
		Uuid:          providerUUID2,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        false,
	}
	err = k.SetProvider(f.Ctx, inactiveProvider2)
	require.NoError(t, err)

	t.Run("allowed user can reactivate provider", func(t *testing.T) {
		msg := &types.MsgUpdateProvider{
			Authority:     allowedUser.String(),
			Uuid:          testProviderUUID,
			Address:       providerAddr.String(),
			PayoutAddress: newPayoutAddr.String(),
			Active:        true, // Reactivating
		}
		_, err := ms.UpdateProvider(f.Ctx, msg)
		require.NoError(t, err)

		provider, err := k.GetProvider(f.Ctx, testProviderUUID)
		require.NoError(t, err)
		require.True(t, provider.Active)
	})

	t.Run("authority can reactivate provider", func(t *testing.T) {
		msg := &types.MsgUpdateProvider{
			Authority:     authority.String(),
			Uuid:          providerUUID2,
			Address:       providerAddr.String(),
			PayoutAddress: newPayoutAddr.String(),
			Active:        true, // Reactivating
		}
		_, err := ms.UpdateProvider(f.Ctx, msg)
		require.NoError(t, err)

		provider, err := k.GetProvider(f.Ctx, providerUUID2)
		require.NoError(t, err)
		require.True(t, provider.Active)
	})
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

func TestDeactivateProviderCascadesSKUs(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	// Price must be high enough that per-second rate is non-zero
	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(3600))

	providerUUID := "01912345-6789-7abc-8def-0123456789d1"

	// Create an active provider
	provider := types.Provider{
		Uuid:          providerUUID,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        true,
	}
	err := k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	// Create multiple SKUs under this provider (some active, some already inactive)
	skuUUIDs := []string{
		"01912345-6789-7abc-8def-0123456789d2",
		"01912345-6789-7abc-8def-0123456789d3",
		"01912345-6789-7abc-8def-0123456789d4",
	}

	// Two active SKUs
	for i := 0; i < 2; i++ {
		sku := types.SKU{
			Uuid:         skuUUIDs[i],
			ProviderUuid: providerUUID,
			Name:         "Active SKU",
			Unit:         types.Unit_UNIT_PER_HOUR,
			BasePrice:    basePrice,
			Active:       true,
		}
		err := k.SetSKU(f.Ctx, sku)
		require.NoError(t, err)
	}

	// One already inactive SKU
	inactiveSKU := types.SKU{
		Uuid:         skuUUIDs[2],
		ProviderUuid: providerUUID,
		Name:         "Inactive SKU",
		Unit:         types.Unit_UNIT_PER_HOUR,
		BasePrice:    basePrice,
		Active:       false,
	}
	err = k.SetSKU(f.Ctx, inactiveSKU)
	require.NoError(t, err)

	// Verify initial state: 2 active SKUs, 1 inactive
	skus, err := k.GetSKUsByProviderUUID(f.Ctx, providerUUID)
	require.NoError(t, err)
	require.Len(t, skus, 3)

	activeCount := 0
	for _, sku := range skus {
		if sku.Active {
			activeCount++
		}
	}
	require.Equal(t, 2, activeCount, "should have 2 active SKUs before deactivation")

	// Deactivate the provider
	msg := &types.MsgDeactivateProvider{
		Authority: authority.String(),
		Uuid:      providerUUID,
	}
	_, err = ms.DeactivateProvider(f.Ctx, msg)
	require.NoError(t, err)

	// Verify provider is inactive
	provider, err = k.GetProvider(f.Ctx, providerUUID)
	require.NoError(t, err)
	require.False(t, provider.Active, "provider should be inactive")

	// Verify ALL SKUs are now inactive (cascade deactivation)
	skus, err = k.GetSKUsByProviderUUID(f.Ctx, providerUUID)
	require.NoError(t, err)
	require.Len(t, skus, 3, "all SKUs should still exist")

	for _, sku := range skus {
		require.False(t, sku.Active, "SKU %s should be inactive after provider deactivation", sku.Uuid)
	}

	// Verify events were emitted for each deactivated SKU (only 2 were active)
	sdkCtx := sdk.UnwrapSDKContext(f.Ctx)
	events := sdkCtx.EventManager().Events()

	skuDeactivatedCount := 0
	providerDeactivatedCount := 0
	for _, event := range events {
		if event.Type == types.EventTypeSKUDeactivated {
			skuDeactivatedCount++
		}
		if event.Type == types.EventTypeProviderDeactivated {
			providerDeactivatedCount++
		}
	}
	require.Equal(t, 2, skuDeactivatedCount, "should emit sku_deactivated event for each active SKU")
	require.Equal(t, 1, providerDeactivatedCount, "should emit exactly one provider_deactivated event")
}

func TestDeactivateProviderCascadeDoesNotAffectOtherProviders(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(3600))

	// Create two providers
	provider1UUID := "01912345-6789-7abc-8def-0123456789e1"
	provider2UUID := "01912345-6789-7abc-8def-0123456789e2"

	for _, uuid := range []string{provider1UUID, provider2UUID} {
		provider := types.Provider{
			Uuid:          uuid,
			Address:       providerAddr.String(),
			PayoutAddress: payoutAddr.String(),
			Active:        true,
		}
		err := k.SetProvider(f.Ctx, provider)
		require.NoError(t, err)
	}

	// Create SKUs for provider 1
	sku1UUID := "01912345-6789-7abc-8def-0123456789e3"
	sku1 := types.SKU{
		Uuid:         sku1UUID,
		ProviderUuid: provider1UUID,
		Name:         "Provider 1 SKU",
		Unit:         types.Unit_UNIT_PER_HOUR,
		BasePrice:    basePrice,
		Active:       true,
	}
	err := k.SetSKU(f.Ctx, sku1)
	require.NoError(t, err)

	// Create SKUs for provider 2
	sku2UUID := "01912345-6789-7abc-8def-0123456789e4"
	sku2 := types.SKU{
		Uuid:         sku2UUID,
		ProviderUuid: provider2UUID,
		Name:         "Provider 2 SKU",
		Unit:         types.Unit_UNIT_PER_HOUR,
		BasePrice:    basePrice,
		Active:       true,
	}
	err = k.SetSKU(f.Ctx, sku2)
	require.NoError(t, err)

	// Deactivate provider 1
	msg := &types.MsgDeactivateProvider{
		Authority: authority.String(),
		Uuid:      provider1UUID,
	}
	_, err = ms.DeactivateProvider(f.Ctx, msg)
	require.NoError(t, err)

	// Verify provider 1's SKU is inactive
	sku1After, err := k.GetSKU(f.Ctx, sku1UUID)
	require.NoError(t, err)
	require.False(t, sku1After.Active, "provider 1's SKU should be inactive")

	// Verify provider 2 is still active
	provider2, err := k.GetProvider(f.Ctx, provider2UUID)
	require.NoError(t, err)
	require.True(t, provider2.Active, "provider 2 should still be active")

	// Verify provider 2's SKU is still active
	sku2After, err := k.GetSKU(f.Ctx, sku2UUID)
	require.NoError(t, err)
	require.True(t, sku2After.Active, "provider 2's SKU should still be active")
}

func TestDeactivateProviderCascadeByAllowedListUser(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, allowedUser := testdata.KeyTestPubAddr()
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	// Add allowedUser to the allowed list
	err := k.SetParams(f.Ctx, types.Params{
		AllowedList: []string{allowedUser.String()},
	})
	require.NoError(t, err)

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(3600))
	providerUUID := "01912345-6789-7abc-8def-0123456789f1"

	// Create provider
	provider := types.Provider{
		Uuid:          providerUUID,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        true,
	}
	err = k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	// Create SKU
	skuUUID := "01912345-6789-7abc-8def-0123456789f2"
	sku := types.SKU{
		Uuid:         skuUUID,
		ProviderUuid: providerUUID,
		Name:         "Test SKU",
		Unit:         types.Unit_UNIT_PER_HOUR,
		BasePrice:    basePrice,
		Active:       true,
	}
	err = k.SetSKU(f.Ctx, sku)
	require.NoError(t, err)

	// Allowed user deactivates the provider
	msg := &types.MsgDeactivateProvider{
		Authority: allowedUser.String(),
		Uuid:      providerUUID,
	}
	_, err = ms.DeactivateProvider(f.Ctx, msg)
	require.NoError(t, err)

	// Verify provider is inactive
	provider, err = k.GetProvider(f.Ctx, providerUUID)
	require.NoError(t, err)
	require.False(t, provider.Active, "provider should be inactive")

	// Verify SKU is also inactive (cascade)
	sku, err = k.GetSKU(f.Ctx, skuUUID)
	require.NoError(t, err)
	require.False(t, sku.Active, "SKU should be inactive after cascade deactivation by allowed user")
}

func TestDeactivateProviderWithNoSKUs(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	providerUUID := "01912345-6789-7abc-8def-0123456789e1"

	// Create an active provider with no SKUs
	provider := types.Provider{
		Uuid:          providerUUID,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        true,
	}
	err := k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	// Deactivate the provider (should succeed even with no SKUs)
	msg := &types.MsgDeactivateProvider{
		Authority: authority.String(),
		Uuid:      providerUUID,
	}
	_, err = ms.DeactivateProvider(f.Ctx, msg)
	require.NoError(t, err)

	// Verify provider is inactive
	provider, err = k.GetProvider(f.Ctx, providerUUID)
	require.NoError(t, err)
	require.False(t, provider.Active, "provider should be inactive")
}

// TestProviderReactivationDoesNotReactivateSKUs verifies that when a provider is
// reactivated via UpdateProvider, its SKUs that were cascade-deactivated remain inactive.
// SKUs must be reactivated individually via UpdateSKU.
func TestProviderReactivationDoesNotReactivateSKUs(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(3600))
	providerUUID := "01912345-6789-7abc-8def-0123456789e1"
	skuUUID := "01912345-6789-7abc-8def-0123456789e2"

	// Create an active provider with an active SKU
	provider := types.Provider{
		Uuid:          providerUUID,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        true,
	}
	err := k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	sku := types.SKU{
		Uuid:         skuUUID,
		ProviderUuid: providerUUID,
		Name:         "Test SKU",
		Unit:         types.Unit_UNIT_PER_HOUR,
		BasePrice:    basePrice,
		Active:       true,
	}
	err = k.SetSKU(f.Ctx, sku)
	require.NoError(t, err)

	// Verify initial state
	sku, err = k.GetSKU(f.Ctx, skuUUID)
	require.NoError(t, err)
	require.True(t, sku.Active, "SKU should be active initially")

	// Deactivate the provider (cascades to SKU)
	deactivateMsg := &types.MsgDeactivateProvider{
		Authority: authority.String(),
		Uuid:      providerUUID,
	}
	_, err = ms.DeactivateProvider(f.Ctx, deactivateMsg)
	require.NoError(t, err)

	// Verify both are inactive
	provider, err = k.GetProvider(f.Ctx, providerUUID)
	require.NoError(t, err)
	require.False(t, provider.Active, "provider should be inactive")

	sku, err = k.GetSKU(f.Ctx, skuUUID)
	require.NoError(t, err)
	require.False(t, sku.Active, "SKU should be inactive after cascade")

	// Reactivate the provider via UpdateProvider
	updateMsg := &types.MsgUpdateProvider{
		Authority:     authority.String(),
		Uuid:          providerUUID,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        true,
	}
	_, err = ms.UpdateProvider(f.Ctx, updateMsg)
	require.NoError(t, err)

	// Verify provider is active but SKU remains inactive
	provider, err = k.GetProvider(f.Ctx, providerUUID)
	require.NoError(t, err)
	require.True(t, provider.Active, "provider should be active after reactivation")

	sku, err = k.GetSKU(f.Ctx, skuUUID)
	require.NoError(t, err)
	require.False(t, sku.Active, "SKU should remain inactive - must be reactivated individually")

	// SKU can be reactivated individually via UpdateSKU
	updateSKUMsg := &types.MsgUpdateSKU{
		Authority:    authority.String(),
		Uuid:         skuUUID,
		ProviderUuid: providerUUID,
		Name:         "Test SKU",
		Unit:         types.Unit_UNIT_PER_HOUR,
		BasePrice:    basePrice,
		Active:       true,
	}
	_, err = ms.UpdateSKU(f.Ctx, updateSKUMsg)
	require.NoError(t, err)

	sku, err = k.GetSKU(f.Ctx, skuUUID)
	require.NoError(t, err)
	require.True(t, sku.Active, "SKU should be active after individual reactivation")
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
	activeProvider := types.Provider{
		Uuid:          testProvider1UUID,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        true,
	}
	err := k.SetProvider(f.Ctx, activeProvider)
	require.NoError(t, err)

	// Create inactive provider
	inactiveProvider := types.Provider{
		Uuid:          testProvider2UUID,
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
			providerUUID: testProvider1UUID,
			skuName:      "Test SKU",
			unit:         types.Unit_UNIT_PER_HOUR,
			basePrice:    basePrice,
			metaHash:     []byte("testhash"),
		},
		{
			name:         "fail; unauthorized sender",
			sender:       acc.String(),
			providerUUID: testProvider1UUID,
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
			providerUUID: testProvider2UUID,
			skuName:      "Test SKU",
			unit:         types.Unit_UNIT_PER_HOUR,
			basePrice:    basePrice,
			errMsg:       "not active",
		},
		{
			name:         "fail; empty name",
			sender:       authority.String(),
			providerUUID: testProvider1UUID,
			skuName:      "",
			unit:         types.Unit_UNIT_PER_HOUR,
			basePrice:    basePrice,
			errMsg:       "name cannot be empty",
		},
		{
			name:         "fail; unspecified unit",
			sender:       authority.String(),
			providerUUID: testProvider1UUID,
			skuName:      "Test SKU",
			unit:         types.Unit_UNIT_UNSPECIFIED,
			basePrice:    basePrice,
			errMsg:       "unit cannot be unspecified",
		},
		{
			name:         "fail; zero base price",
			sender:       authority.String(),
			providerUUID: testProvider1UUID,
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
	provider := types.Provider{
		Uuid:          testProvider1UUID,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        true,
	}
	err := k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	skuUUID := "01912345-6789-7abc-8def-0123456789b1"
	existingSKU := types.SKU{
		Uuid:         skuUUID,
		ProviderUuid: testProvider1UUID,
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
			providerUUID: testProvider1UUID,
			skuName:      "Updated SKU",
			unit:         types.Unit_UNIT_PER_DAY,
			basePrice:    newPrice,
			active:       false,
		},
		{
			name:         "fail; unauthorized sender",
			sender:       acc.String(),
			uuid:         skuUUID,
			providerUUID: testProvider1UUID,
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
			providerUUID: testProvider1UUID,
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
			providerUUID: testProvider2UUID,
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
			providerUUID: testProvider1UUID,
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
	provider := types.Provider{
		Uuid:          testProvider1UUID,
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
			ProviderUuid: testProvider1UUID,
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
		ProviderUuid: testProvider1UUID,
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

func TestSKUReactivation(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, allowedUser := testdata.KeyTestPubAddr()
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	// Price must be high enough that per-second rate is non-zero
	// 3600 umfx per hour = 1 umfx per second
	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(3600))

	// Add allowedUser to the allowed list
	err := k.SetParams(f.Ctx, types.Params{
		AllowedList: []string{allowedUser.String()},
	})
	require.NoError(t, err)

	// Create an active provider (required for SKU operations)
	provider := types.Provider{
		Uuid:          testProvider1UUID,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        true,
	}
	err = k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	skuUUID1 := "01912345-6789-7abc-8def-0123456789c1"
	skuUUID2 := "01912345-6789-7abc-8def-0123456789c2"

	// Create INACTIVE SKUs for reactivation tests
	inactiveSKU1 := types.SKU{
		Uuid:         skuUUID1,
		ProviderUuid: testProvider1UUID,
		Name:         "Inactive SKU 1",
		Unit:         types.Unit_UNIT_PER_HOUR,
		BasePrice:    basePrice,
		Active:       false,
	}
	err = k.SetSKU(f.Ctx, inactiveSKU1)
	require.NoError(t, err)

	inactiveSKU2 := types.SKU{
		Uuid:         skuUUID2,
		ProviderUuid: testProvider1UUID,
		Name:         "Inactive SKU 2",
		Unit:         types.Unit_UNIT_PER_HOUR,
		BasePrice:    basePrice,
		Active:       false,
	}
	err = k.SetSKU(f.Ctx, inactiveSKU2)
	require.NoError(t, err)

	t.Run("allowed user can reactivate SKU", func(t *testing.T) {
		msg := &types.MsgUpdateSKU{
			Authority:    allowedUser.String(),
			Uuid:         skuUUID1,
			ProviderUuid: testProvider1UUID,
			Name:         "Reactivated SKU 1",
			Unit:         types.Unit_UNIT_PER_HOUR,
			BasePrice:    basePrice,
			Active:       true, // Reactivating
		}
		_, err := ms.UpdateSKU(f.Ctx, msg)
		require.NoError(t, err)

		sku, err := k.GetSKU(f.Ctx, skuUUID1)
		require.NoError(t, err)
		require.True(t, sku.Active, "SKU should be active after reactivation by allowed user")
		require.Equal(t, "Reactivated SKU 1", sku.Name)
	})

	t.Run("authority can reactivate SKU", func(t *testing.T) {
		msg := &types.MsgUpdateSKU{
			Authority:    authority.String(),
			Uuid:         skuUUID2,
			ProviderUuid: testProvider1UUID,
			Name:         "Reactivated SKU 2",
			Unit:         types.Unit_UNIT_PER_HOUR,
			BasePrice:    basePrice,
			Active:       true, // Reactivating
		}
		_, err := ms.UpdateSKU(f.Ctx, msg)
		require.NoError(t, err)

		sku, err := k.GetSKU(f.Ctx, skuUUID2)
		require.NoError(t, err)
		require.True(t, sku.Active, "SKU should be active after reactivation by authority")
		require.Equal(t, "Reactivated SKU 2", sku.Name)
	})
}

func TestSKUReactivationRequiresActiveProvider(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()

	f := initFixture(t)

	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	ms := keeper.NewMsgServerImpl(k)

	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(3600))

	// Create an INACTIVE provider
	providerUUID := "01912345-6789-7abc-8def-012345678901"
	provider := types.Provider{
		Uuid:          providerUUID,
		Address:       providerAddr.String(),
		PayoutAddress: payoutAddr.String(),
		Active:        false, // Provider is inactive
	}
	err := k.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	// Create an inactive SKU under this inactive provider
	skuUUID := "01912345-6789-7abc-8def-012345678902"
	sku := types.SKU{
		Uuid:         skuUUID,
		ProviderUuid: providerUUID,
		Name:         "Inactive SKU",
		Unit:         types.Unit_UNIT_PER_HOUR,
		BasePrice:    basePrice,
		Active:       false,
	}
	err = k.SetSKU(f.Ctx, sku)
	require.NoError(t, err)

	t.Run("fail: cannot reactivate SKU when provider is inactive", func(t *testing.T) {
		msg := &types.MsgUpdateSKU{
			Authority:    authority.String(),
			Uuid:         skuUUID,
			ProviderUuid: providerUUID,
			Name:         "Trying to Reactivate",
			Unit:         types.Unit_UNIT_PER_HOUR,
			BasePrice:    basePrice,
			Active:       true, // Attempting to reactivate
		}
		_, err := ms.UpdateSKU(f.Ctx, msg)
		require.Error(t, err)
		require.ErrorContains(t, err, "cannot reactivate SKU: provider is inactive")

		// Verify SKU is still inactive
		sku, err := k.GetSKU(f.Ctx, skuUUID)
		require.NoError(t, err)
		require.False(t, sku.Active, "SKU should still be inactive")
	})

	t.Run("success: can update inactive SKU without reactivating when provider is inactive", func(t *testing.T) {
		// Should be able to update metadata without reactivating
		msg := &types.MsgUpdateSKU{
			Authority:    authority.String(),
			Uuid:         skuUUID,
			ProviderUuid: providerUUID,
			Name:         "Updated Name",
			Unit:         types.Unit_UNIT_PER_HOUR,
			BasePrice:    basePrice,
			Active:       false, // Keeping it inactive
		}
		_, err := ms.UpdateSKU(f.Ctx, msg)
		require.NoError(t, err)

		sku, err := k.GetSKU(f.Ctx, skuUUID)
		require.NoError(t, err)
		require.False(t, sku.Active, "SKU should still be inactive")
		require.Equal(t, "Updated Name", sku.Name)
	})

	t.Run("success: can reactivate SKU after provider is reactivated", func(t *testing.T) {
		// First, reactivate the provider
		provider.Active = true
		err := k.SetProvider(f.Ctx, provider)
		require.NoError(t, err)

		// Now reactivating SKU should work
		msg := &types.MsgUpdateSKU{
			Authority:    authority.String(),
			Uuid:         skuUUID,
			ProviderUuid: providerUUID,
			Name:         "Reactivated SKU",
			Unit:         types.Unit_UNIT_PER_HOUR,
			BasePrice:    basePrice,
			Active:       true, // Reactivating
		}
		_, err = ms.UpdateSKU(f.Ctx, msg)
		require.NoError(t, err)

		sku, err := k.GetSKU(f.Ctx, skuUUID)
		require.NoError(t, err)
		require.True(t, sku.Active, "SKU should be active after reactivation")
	})
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
	provider := types.Provider{
		Uuid:          testProvider1UUID,
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
			ProviderUuid: testProvider1UUID,
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
	provider := types.Provider{
		Uuid:          testProvider1UUID,
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
		ProviderUuid: testProvider1UUID,
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
		ProviderUuid: testProvider1UUID,
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
		ProviderUuid: testProvider1UUID,
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
	provider := types.Provider{
		Uuid:          testProvider1UUID,
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
		ProviderUuid: testProvider1UUID,
		Name:         "Test SKU",
		Unit:         types.Unit_UNIT_PER_HOUR,
		BasePrice:    basePrice,
	}

	_, err = ms.CreateSKU(f.Ctx, createMsg)
	require.Error(t, err)
	require.ErrorContains(t, err, "unauthorized")
}
