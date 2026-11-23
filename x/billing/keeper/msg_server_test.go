/*
Package keeper_test contains unit tests for the billing module message server.

Test Coverage:
- MsgFundCredit: funding credit accounts, denom validation, balance tracking
- MsgUpdateParams: parameter updates, authority validation
- MsgCreateLease: lease creation with SKU validation, credit checks, max lease limits
- MsgCreateLeaseForTenant: authority-only lease creation on behalf of tenants
- MsgAcknowledgeLease: provider acknowledgement of pending leases
- MsgCloseLease: lease closure by tenant, provider, or authority
- MsgWithdraw: provider withdrawal, settled per (provider, tenant, denom) stream

NOTE: With the new lease lifecycle (PENDING -> ACTIVE), tests that need an ACTIVE
lease must call createAndAcknowledgeLease helper instead of just CreateLease.
*/
package keeper_test

import (
	"bytes"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/app/apptesting"
	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestMsgFundCredit(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	sender := f.TestAccs[0]
	tenant := f.TestAccs[1]
	denom := testDenom

	// Fund sender with tokens
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(10000000))
	f.fundAccount(t, sender, sdk.NewCoins(fundAmount))

	tests := []struct {
		name      string
		msg       *types.MsgFundCredit
		expectErr bool
		errMsg    string
	}{
		{
			name: "success: fund credit account",
			msg: &types.MsgFundCredit{
				Sender: sender.String(),
				Tenant: tenant.String(),
				Amount: sdk.NewCoin(denom, sdkmath.NewInt(5000000)),
			},
			expectErr: false,
		},
		{
			name: "success: fund own credit account",
			msg: &types.MsgFundCredit{
				Sender: sender.String(),
				Tenant: sender.String(),
				Amount: sdk.NewCoin(denom, sdkmath.NewInt(1000000)),
			},
			expectErr: false,
		},
		{
			name: "fail: insufficient balance - wrong denom",
			msg: &types.MsgFundCredit{
				Sender: sender.String(),
				Tenant: tenant.String(),
				Amount: sdk.NewCoin("wrongdenom", sdkmath.NewInt(1000000)),
			},
			expectErr: true,
			errMsg:    "insufficient funds", // No module-level denom restriction anymore; fails due to insufficient balance
		},
		{
			name: "fail: insufficient balance",
			msg: &types.MsgFundCredit{
				Sender: sender.String(),
				Tenant: tenant.String(),
				Amount: sdk.NewCoin(denom, sdkmath.NewInt(100000000000)), // Way more than funded
			},
			expectErr: true,
			errMsg:    "failed to transfer tokens",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := msgServer.FundCredit(f.Ctx, tc.msg)
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
				require.Nil(t, resp)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				require.NotEmpty(t, resp.CreditAddress)
				require.Equal(t, tc.msg.Amount.Denom, resp.NewBalance.Denom)
			}
		})
	}
}

func TestMsgFundCreditCreatesAccount(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	sender := f.TestAccs[0]
	tenant := f.TestAccs[1]
	denom := testDenom

	// Fund sender with tokens
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(10000000))
	f.fundAccount(t, sender, sdk.NewCoins(fundAmount))

	// Verify credit account doesn't exist
	_, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.ErrorIs(t, err, types.ErrCreditAccountNotFound)

	// Fund credit account
	msg := &types.MsgFundCredit{
		Sender: sender.String(),
		Tenant: tenant.String(),
		Amount: sdk.NewCoin(denom, sdkmath.NewInt(5000000)),
	}
	resp, err := msgServer.FundCredit(f.Ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify credit account was created
	ca, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, tenant.String(), ca.Tenant)
	require.Equal(t, resp.CreditAddress, ca.CreditAddress)

	// Verify the balance is tracked in bank module
	creditAddr, err := sdk.AccAddressFromBech32(resp.CreditAddress)
	require.NoError(t, err)
	balance := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, denom)
	require.Equal(t, msg.Amount, balance)

	// Verify the credit address account exists in the account keeper
	acc := f.App.AccountKeeper.GetAccount(f.Ctx, creditAddr)
	require.NotNil(t, acc)
}

func TestMsgFundCreditAdditionalFunding(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	sender := f.TestAccs[0]
	tenant := f.TestAccs[1]
	denom := testDenom

	// Fund sender with tokens
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(10000000))
	f.fundAccount(t, sender, sdk.NewCoins(fundAmount))

	// First funding
	firstAmount := sdk.NewCoin(denom, sdkmath.NewInt(3000000))
	resp1, err := msgServer.FundCredit(f.Ctx, &types.MsgFundCredit{
		Sender: sender.String(),
		Tenant: tenant.String(),
		Amount: firstAmount,
	})
	require.NoError(t, err)
	require.Equal(t, firstAmount, resp1.NewBalance)

	// Second funding
	secondAmount := sdk.NewCoin(denom, sdkmath.NewInt(2000000))
	resp2, err := msgServer.FundCredit(f.Ctx, &types.MsgFundCredit{
		Sender: sender.String(),
		Tenant: tenant.String(),
		Amount: secondAmount,
	})
	require.NoError(t, err)

	// New balance should be sum of both fundings
	expectedBalance := firstAmount.Add(secondAmount)
	require.Equal(t, expectedBalance, resp2.NewBalance)

	// Verify balance in bank module
	creditAddr, err := sdk.AccAddressFromBech32(resp2.CreditAddress)
	require.NoError(t, err)
	balance := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, denom)
	require.Equal(t, expectedBalance, balance)
}

func TestMsgFundCreditEvents(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	sender := f.TestAccs[0]
	tenant := f.TestAccs[1]
	denom := testDenom

	// Fund sender with tokens
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(10000000))
	f.fundAccount(t, sender, sdk.NewCoins(fundAmount))

	// Fund credit account
	_, err := msgServer.FundCredit(f.Ctx, &types.MsgFundCredit{
		Sender: sender.String(),
		Tenant: tenant.String(),
		Amount: sdk.NewCoin(denom, sdkmath.NewInt(5000000)),
	})
	require.NoError(t, err)

	// Check events
	events := f.Ctx.EventManager().Events()
	foundCreditFundedEvent := false
	for _, event := range events {
		if event.Type == types.EventTypeCreditFunded {
			foundCreditFundedEvent = true
			// Verify event attributes
			for _, attr := range event.Attributes {
				switch attr.Key {
				case types.AttributeKeyTenant:
					require.Equal(t, tenant.String(), attr.Value)
				case types.AttributeKeyAmount:
					require.Contains(t, attr.Value, denom)
				}
			}
		}
	}
	require.True(t, foundCreditFundedEvent, "credit_funded event should be emitted")
}

func TestMsgUpdateParams(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	authority := f.Authority
	nonAuthority := f.TestAccs[0]

	tests := []struct {
		name      string
		msg       *types.MsgUpdateParams
		expectErr bool
		errMsg    string
	}{
		{
			name: "success: update params",
			msg: &types.MsgUpdateParams{
				Authority: authority.String(),
				Params: types.NewParams(
					50,
					[]string{},
					20,
					3600,
					10,
					1800,
				),
			},
			expectErr: false,
		},
		{
			name: "fail: non-authority",
			msg: &types.MsgUpdateParams{
				Authority: nonAuthority.String(),
				Params:    types.DefaultParams(),
			},
			expectErr: true,
			errMsg:    "unauthorized",
		},
		{
			name: "fail: invalid params - zero max leases",
			msg: &types.MsgUpdateParams{
				Authority: authority.String(),
				Params: types.NewParams(
					0, // invalid: zero max leases
					[]string{},
					20,
					3600,
					10,
					1800,
				),
			},
			expectErr: true,
			errMsg:    "max_leases_per_tenant must be greater than zero",
		},
		{
			name: "fail: max_leases_per_tenant exceeds upper bound",
			msg: &types.MsgUpdateParams{
				Authority: authority.String(),
				Params: types.NewParams(
					types.MaxLeasesPerTenantUpperBound+1,
					[]string{},
					20,
					3600,
					10,
					1800,
				),
			},
			expectErr: true,
			errMsg:    "exceeds upper bound",
		},
		{
			name: "fail: min_lease_duration exceeds upper bound",
			msg: &types.MsgUpdateParams{
				Authority: authority.String(),
				Params: types.NewParams(
					100,
					[]string{},
					20,
					types.MaxMinLeaseDuration+1,
					10,
					1800,
				),
			},
			expectErr: true,
			errMsg:    "exceeds upper bound",
		},
		{
			name: "fail: max_pending_leases_per_tenant exceeds upper bound",
			msg: &types.MsgUpdateParams{
				Authority: authority.String(),
				Params: types.NewParams(
					100,
					[]string{},
					20,
					3600,
					types.MaxPendingLeasesPerTenantUpperBound+1,
					1800,
				),
			},
			expectErr: true,
			errMsg:    "exceeds upper bound",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := msgServer.UpdateParams(f.Ctx, tc.msg)
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
				require.Nil(t, resp)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)

				// Verify params were updated
				params, err := f.App.BillingKeeper.GetParams(f.Ctx)
				require.NoError(t, err)
				require.Equal(t, tc.msg.Params.MaxLeasesPerTenant, params.MaxLeasesPerTenant)
			}
		})
	}
}

func TestMsgUpdateParamsEvents(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	authority := f.Authority

	_, err := msgServer.UpdateParams(f.Ctx, &types.MsgUpdateParams{
		Authority: authority.String(),
		Params:    types.DefaultParams(),
	})
	require.NoError(t, err)

	// Check events
	events := f.Ctx.EventManager().Events()
	foundParamsUpdatedEvent := false
	for _, event := range events {
		if event.Type == types.EventTypeParamsUpdated {
			foundParamsUpdatedEvent = true
		}
	}
	require.True(t, foundParamsUpdatedEvent, "params_updated event should be emitted")
}

func TestMsgCreateLease(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 3600 per hour = 1 per second

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(10000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	tests := []struct {
		name      string
		msg       *types.MsgCreateLease
		expectErr bool
		errMsg    string
	}{
		{
			name: "success: create lease",
			msg: &types.MsgCreateLease{
				Tenant: tenant.String(),
				Items: []types.LeaseItemInput{
					{
						SkuUuid:  sku.Uuid,
						Quantity: 2,
					},
				},
			},
			expectErr: false,
		},
		{
			name: "fail: SKU not found",
			msg: &types.MsgCreateLease{
				Tenant: tenant.String(),
				Items: []types.LeaseItemInput{
					{
						SkuUuid:  "01912345-6789-7abc-8def-999999999999",
						Quantity: 1,
					},
				},
			},
			expectErr: true,
			errMsg:    "sku not found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := msgServer.CreateLease(f.Ctx, tc.msg)
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
				require.Nil(t, resp)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				require.NotEmpty(t, resp.LeaseUuid)

				// Verify lease was created in PENDING state (awaiting provider acknowledgement)
				lease, err := f.App.BillingKeeper.GetLease(f.Ctx, resp.LeaseUuid)
				require.NoError(t, err)
				require.Equal(t, tc.msg.Tenant, lease.Tenant)
				require.Equal(t, provider.Uuid, lease.ProviderUuid)
				require.Equal(t, types.LEASE_STATE_PENDING, lease.State)
				require.Len(t, lease.Items, len(tc.msg.Items))
			}
		})
	}
}

func TestMsgCreateLeaseInsufficientCredit(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 100)

	// Do NOT fund the tenant - they should have no credit account
	msg := &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{
				SkuUuid:  sku.Uuid,
				Quantity: 1,
			},
		},
	}

	resp, err := msgServer.CreateLease(f.Ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "credit account not found")
	require.Nil(t, resp)
}

func TestMsgCreateLeaseMaxLeasesReached(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Set max pending leases to 2 (leases start in PENDING state)
	params := types.DefaultParams()
	params.MaxPendingLeasesPerTenant = 2
	err := f.App.BillingKeeper.SetParams(f.Ctx, params)
	require.NoError(t, err)

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 3600 per hour = 1 per second

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(100000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create max number of pending leases
	for i := 0; i < 2; i++ {
		msg := &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items: []types.LeaseItemInput{
				{
					SkuUuid:  sku.Uuid,
					Quantity: 1,
				},
			},
		}
		_, err := msgServer.CreateLease(f.Ctx, msg)
		require.NoError(t, err)
	}

	// Try to create one more - should fail (max pending leases reached)
	msg := &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{
				SkuUuid:  sku.Uuid,
				Quantity: 1,
			},
		},
	}
	resp, err := msgServer.CreateLease(f.Ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "maximum pending leases per tenant reached")
	require.Nil(t, resp)
}

func TestMsgCreateLeaseWithMetaHash(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 3600 per hour = 1 per second

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(10000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create a SHA-256 hash (32 bytes) as meta_hash
	metaHash := []byte{
		0xa1, 0xb2, 0xc3, 0xd4, 0xe5, 0xf6, 0x01, 0x12,
		0x23, 0x34, 0x45, 0x56, 0x67, 0x78, 0x89, 0x9a,
		0xab, 0xbc, 0xcd, 0xde, 0xef, 0xf0, 0x01, 0x12,
		0x23, 0x34, 0x45, 0x56, 0x67, 0x78, 0x89, 0x9a,
	}

	t.Run("success: create lease with meta_hash", func(t *testing.T) {
		msg := &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items: []types.LeaseItemInput{
				{
					SkuUuid:  sku.Uuid,
					Quantity: 1,
				},
			},
			MetaHash: metaHash,
		}

		resp, err := msgServer.CreateLease(f.Ctx, msg)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotEmpty(t, resp.LeaseUuid)

		// Verify meta_hash was stored
		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, resp.LeaseUuid)
		require.NoError(t, err)
		require.Equal(t, metaHash, lease.MetaHash)
	})

	t.Run("success: create lease without meta_hash", func(t *testing.T) {
		msg := &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items: []types.LeaseItemInput{
				{
					SkuUuid:  sku.Uuid,
					Quantity: 1,
				},
			},
			// MetaHash not set - should be nil/empty
		}

		resp, err := msgServer.CreateLease(f.Ctx, msg)
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Verify meta_hash is empty
		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, resp.LeaseUuid)
		require.NoError(t, err)
		require.Empty(t, lease.MetaHash)
	})

	t.Run("success: create lease with max length meta_hash", func(t *testing.T) {
		maxMetaHash := make([]byte, types.MaxMetaHashLength)
		for i := range maxMetaHash {
			maxMetaHash[i] = byte(i)
		}

		msg := &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items: []types.LeaseItemInput{
				{
					SkuUuid:  sku.Uuid,
					Quantity: 1,
				},
			},
			MetaHash: maxMetaHash,
		}

		resp, err := msgServer.CreateLease(f.Ctx, msg)
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Verify max length meta_hash was stored
		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, resp.LeaseUuid)
		require.NoError(t, err)
		require.Equal(t, maxMetaHash, lease.MetaHash)
	})
}

func TestMsgCreateLeaseForTenantWithMetaHash(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	authority := f.App.BillingKeeper.GetAuthority()
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(10000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	metaHash := []byte{0xde, 0xad, 0xbe, 0xef, 0xca, 0xfe, 0xba, 0xbe}

	t.Run("success: authority creates lease with meta_hash", func(t *testing.T) {
		msg := &types.MsgCreateLeaseForTenant{
			Authority: authority,
			Tenant:    tenant.String(),
			Items: []types.LeaseItemInput{
				{
					SkuUuid:  sku.Uuid,
					Quantity: 1,
				},
			},
			MetaHash: metaHash,
		}

		resp, err := msgServer.CreateLeaseForTenant(f.Ctx, msg)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotEmpty(t, resp.LeaseUuid)

		// Verify meta_hash was stored
		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, resp.LeaseUuid)
		require.NoError(t, err)
		require.Equal(t, metaHash, lease.MetaHash)
	})
}

func TestMsgCreateLeaseForTenant(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	nonAuthority := f.TestAccs[3]
	authority := f.App.BillingKeeper.GetAuthority()
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 3600 per hour = 1 per second

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(10000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	tests := []struct {
		name      string
		msg       *types.MsgCreateLeaseForTenant
		expectErr bool
		errMsg    string
	}{
		{
			name: "success: authority creates lease for tenant",
			msg: &types.MsgCreateLeaseForTenant{
				Authority: authority,
				Tenant:    tenant.String(),
				Items: []types.LeaseItemInput{
					{
						SkuUuid:  sku.Uuid,
						Quantity: 2,
					},
				},
			},
			expectErr: false,
		},
		{
			name: "fail: non-authority creates lease",
			msg: &types.MsgCreateLeaseForTenant{
				Authority: nonAuthority.String(),
				Tenant:    tenant.String(),
				Items: []types.LeaseItemInput{
					{
						SkuUuid:  sku.Uuid,
						Quantity: 1,
					},
				},
			},
			expectErr: true,
			errMsg:    "unauthorized",
		},
		{
			name: "fail: SKU not found",
			msg: &types.MsgCreateLeaseForTenant{
				Authority: authority,
				Tenant:    tenant.String(),
				Items: []types.LeaseItemInput{
					{
						SkuUuid:  "01912345-6789-7abc-8def-999999999999",
						Quantity: 1,
					},
				},
			},
			expectErr: true,
			errMsg:    "sku not found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := msgServer.CreateLeaseForTenant(f.Ctx, tc.msg)
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
				require.Nil(t, resp)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				require.NotEmpty(t, resp.LeaseUuid)

				// Verify lease was created
				lease, err := f.App.BillingKeeper.GetLease(f.Ctx, resp.LeaseUuid)
				require.NoError(t, err)
				require.Equal(t, tc.msg.Tenant, lease.Tenant)
				require.Equal(t, provider.Uuid, lease.ProviderUuid)
				require.Equal(t, types.LEASE_STATE_PENDING, lease.State) // Leases start in PENDING state
				require.Len(t, lease.Items, len(tc.msg.Items))
			}
		})
	}
}

func TestMsgCreateLeaseForTenantWithAllowedList(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	allowedUser := f.TestAccs[3]
	notAllowed := f.TestAccs[4]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 3600 per hour = 1 per second

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(10000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Update params to add allowedUser to allowed list
	params := types.NewParams(
		types.DefaultMaxLeasesPerTenant,
		[]string{allowedUser.String()},
		types.DefaultMaxItemsPerLease,
		types.DefaultMinLeaseDuration,
		types.DefaultMaxPendingLeasesPerTenant,
		types.DefaultPendingTimeout,
	)
	err = f.App.BillingKeeper.SetParams(f.Ctx, params)
	require.NoError(t, err)

	// Test: allowed user can create lease for tenant
	resp, err := msgServer.CreateLeaseForTenant(f.Ctx, &types.MsgCreateLeaseForTenant{
		Authority: allowedUser.String(),
		Tenant:    tenant.String(),
		Items: []types.LeaseItemInput{
			{
				SkuUuid:  sku.Uuid,
				Quantity: 1,
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.LeaseUuid)

	// Test: non-allowed user cannot create lease for tenant
	_, err = msgServer.CreateLeaseForTenant(f.Ctx, &types.MsgCreateLeaseForTenant{
		Authority: notAllowed.String(),
		Tenant:    tenant.String(),
		Items: []types.LeaseItemInput{
			{
				SkuUuid:  sku.Uuid,
				Quantity: 1,
			},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not the authority or in the allowed list")
}

func TestMsgCloseLease(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(10000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create a lease and acknowledge it
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{
			SkuUuid:  sku.Uuid,
			Quantity: 1,
		},
	})

	tests := []struct {
		name      string
		msg       *types.MsgCloseLease
		expectErr bool
		errMsg    string
	}{
		{
			name: "success: tenant closes lease",
			msg: &types.MsgCloseLease{
				Sender:     tenant.String(),
				LeaseUuids: []string{leaseID},
			},
			expectErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := msgServer.CloseLease(f.Ctx, tc.msg)
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
				require.Nil(t, resp)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				require.Equal(t, uint64(1), resp.ClosedCount)

				// Verify lease is now inactive
				lease, err := f.App.BillingKeeper.GetLease(f.Ctx, tc.msg.LeaseUuids[0])
				require.NoError(t, err)
				require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
				require.NotNil(t, lease.ClosedAt)
			}
		})
	}
}

func TestMsgCloseLeaseUnauthorized(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	randomAddr := f.TestAccs[3]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(10000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create a lease and acknowledge it
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{
			SkuUuid:  sku.Uuid,
			Quantity: 1,
		},
	})

	// Try to close with random address
	resp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     randomAddr.String(),
		LeaseUuids: []string{leaseID},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
	require.Nil(t, resp)
}

func TestMsgWithdraw(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Create provider and SKU with 1 unit per second rate
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 3600 per hour = 1 per second

	// Fund tenant's credit account generously
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(100000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create a lease and acknowledge it
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{
			SkuUuid:  sku.Uuid,
			Quantity: 1,
		},
	})

	acknowledgedAt := f.Ctx.BlockTime()
	streamKey := keeper.NewStreamKey(provider.Uuid, tenant, denom)

	// Advance block time by 100 seconds
	newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
	f.Ctx = newCtx
	f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())

	// Provider withdraws
	resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:       providerAddr.String(),
		ProviderUuid: provider.Uuid,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, payoutAddr.String(), resp.PayoutAddress)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(100))), resp.TotalAmounts,
		"1/s for 100 seconds")
	require.Equal(t, uint64(1), resp.WithdrawalCount)
	require.False(t, resp.HasMore)
	require.Empty(t, resp.NextKey)

	// Verify the funds moved from the tenant's credit to the payout address
	payoutBalance := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom)
	require.Equal(t, sdkmath.NewInt(100), payoutBalance.Amount)
	creditBalance := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, denom)
	require.Equal(t, fundAmount.Amount.SubRaw(100), creditBalance.Amount)

	// The withdrawal settles the stream, not the lease
	stream, err := f.App.BillingKeeper.Streams.Get(f.Ctx, streamKey)
	require.NoError(t, err)
	require.Equal(t, acknowledgedAt.Add(100*time.Second), stream.LastSettledAt)
	require.False(t, stream.IsExhausted())

	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)
	require.Nil(t, lease.LastSettledAt, "the deprecated field is never set") //nolint:staticcheck // asserting the deprecated field stays unset

	// Events: one settlement and one summary carrying the same amount
	settled := findEvent(t, f.Ctx, types.EventTypeStreamSettled)
	require.Equal(t, provider.Uuid, attrValue(t, settled, types.AttributeKeyProviderUUID))
	require.Equal(t, tenant.String(), attrValue(t, settled, types.AttributeKeyTenant))
	require.Equal(t, "100"+denom, attrValue(t, settled, types.AttributeKeyAmount))
	require.Equal(t, payoutAddr.String(), attrValue(t, settled, types.AttributeKeyPayoutAddress))

	batch := findEvent(t, f.Ctx, types.EventTypeBatchWithdraw)
	require.Equal(t, provider.Uuid, attrValue(t, batch, types.AttributeKeyProviderUUID))
	require.Equal(t, "100"+denom, attrValue(t, batch, types.AttributeKeyAmount))
	require.Equal(t, "1", attrValue(t, batch, types.AttributeKeyStreamCount))
	require.Equal(t, payoutAddr.String(), attrValue(t, batch, types.AttributeKeyPayoutAddress))
	require.Equal(t, "0", attrValue(t, batch, types.AttributeKeyExhaustedCount))

	requireInvariants(t, f)
}

// TestMsgWithdrawBatch tests a withdrawal that covers several leases, and that
// it only reaches the streams of the provider named by the message.
func TestMsgWithdrawBatch(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 1 umfx/sec

	// Fund tenant's credit account generously
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(1000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	streamKey := keeper.NewStreamKey(provider.Uuid, tenant, denom)

	t.Run("success: one withdrawal pays for every lease of the stream", func(t *testing.T) {
		// Create and acknowledge 3 leases with different quantities: 1 + 2 + 3 = 6 umfx/sec
		for _, quantity := range []uint64{1, 2, 3} {
			f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
				{SkuUuid: sku.Uuid, Quantity: quantity},
			})
		}

		stream, err := f.App.BillingKeeper.Streams.Get(f.Ctx, streamKey)
		require.NoError(t, err)
		require.Equal(t, sdkmath.NewInt(6), stream.Rate)
		require.Equal(t, uint64(3), stream.LeaseCount)

		// Advance block time by 100 seconds
		newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
		f.Ctx = newCtx

		// Get initial balance
		initialBalance := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom)

		// The 3 leases share one stream, so they are paid by one settlement
		resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
			Sender:       providerAddr.String(),
			ProviderUuid: provider.Uuid,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, payoutAddr.String(), resp.PayoutAddress)
		require.Equal(t, uint64(1), resp.WithdrawalCount, "withdrawal_count counts streams, not leases")
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(600))), resp.TotalAmounts)

		// Verify the payout address received exactly the accrual of the 3 leases
		newBalance := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom)
		require.Equal(t, sdkmath.NewInt(600), newBalance.Amount.Sub(initialBalance.Amount))

		stream, err = f.App.BillingKeeper.Streams.Get(f.Ctx, streamKey)
		require.NoError(t, err)
		require.Equal(t, f.Ctx.BlockTime(), stream.LastSettledAt)
		requireInvariants(t, f)
	})

	t.Run("success: a withdrawal only settles the streams of its own provider", func(t *testing.T) {
		// Create another provider with its own payout address and a lease of the same tenant
		provider2Addr := f.TestAccs[3]
		payout2Addr := f.TestAccs[4]
		provider2 := f.createTestProvider(t, provider2Addr.String(), payout2Addr.String())
		sku2 := f.createTestSKU(t, provider2.Uuid, 3600)

		f.createAndAcknowledgeLease(t, msgServer, tenant, provider2Addr, []types.LeaseItemInput{
			{SkuUuid: sku2.Uuid, Quantity: 1},
		})
		stream2Key := keeper.NewStreamKey(provider2.Uuid, tenant, denom)
		stream2Before, err := f.App.BillingKeeper.Streams.Get(f.Ctx, stream2Key)
		require.NoError(t, err)

		// Advance time
		newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
		f.Ctx = newCtx

		initialBalance := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom)

		// Provider 1 withdraws: it is paid for its own stream (6 umfx/sec) only
		resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
			Sender:       providerAddr.String(),
			ProviderUuid: provider.Uuid,
		})
		require.NoError(t, err)
		require.Equal(t, uint64(1), resp.WithdrawalCount)
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(600))), resp.TotalAmounts)
		require.Equal(t, sdkmath.NewInt(600),
			f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom).Amount.Sub(initialBalance.Amount))

		// Provider 2's stream was left alone: nothing paid, nothing settled
		require.True(t, f.App.BankKeeper.GetBalance(f.Ctx, payout2Addr, denom).IsZero())
		stream2, err := f.App.BillingKeeper.Streams.Get(f.Ctx, stream2Key)
		require.NoError(t, err)
		require.Equal(t, stream2Before.LastSettledAt, stream2.LastSettledAt)

		// Provider 1 cannot withdraw on behalf of provider 2
		_, err = msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
			Sender:       providerAddr.String(),
			ProviderUuid: provider2.Uuid,
		})
		require.ErrorIs(t, err, types.ErrUnauthorized)
		require.True(t, f.App.BankKeeper.GetBalance(f.Ctx, payout2Addr, denom).IsZero())

		// Provider 2 is still owed the whole 100 seconds
		resp, err = msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
			Sender:       provider2Addr.String(),
			ProviderUuid: provider2.Uuid,
		})
		require.NoError(t, err)
		require.Equal(t, payout2Addr.String(), resp.PayoutAddress)
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(100))), resp.TotalAmounts)
		require.Equal(t, sdkmath.NewInt(100), f.App.BankKeeper.GetBalance(f.Ctx, payout2Addr, denom).Amount)
		requireInvariants(t, f)
	})
}

// TestMsgWithdraw_ErrorCases tests error scenarios for Withdraw.
func TestMsgWithdraw_ErrorCases(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	unauthorizedUser := f.TestAccs[3]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(100000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create a lease and acknowledge it
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{
			SkuUuid:  sku.Uuid,
			Quantity: 1,
		},
	})

	// Let the stream accrue, so a withdrawal that wrongly went through would pay out.
	streamKey := keeper.NewStreamKey(provider.Uuid, tenant, denom)
	streamBefore, err := f.App.BillingKeeper.Streams.Get(f.Ctx, streamKey)
	require.NoError(t, err)
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))

	tests := []struct {
		name        string
		msg         *types.MsgWithdraw
		wantErr     error
		errContains string
	}{
		{
			name: "fail: provider does not exist",
			msg: &types.MsgWithdraw{
				Sender:       providerAddr.String(),
				ProviderUuid: "01912345-6789-7abc-8def-999999999999",
			},
			wantErr:     types.ErrProviderNotFound,
			errContains: "not found",
		},
		{
			name: "fail: malformed provider uuid",
			msg: &types.MsgWithdraw{
				Sender:       providerAddr.String(),
				ProviderUuid: "not-a-uuid",
			},
			wantErr:     types.ErrProviderNotFound,
			errContains: "invalid provider_uuid format",
		},
		{
			name: "fail: provider_uuid is required",
			msg: &types.MsgWithdraw{
				Sender: providerAddr.String(),
			},
			wantErr:     types.ErrInvalidRequest,
			errContains: "provider_uuid is required",
		},
		{
			name: "fail: lease_uuids is rejected even alongside provider_uuid",
			msg: &types.MsgWithdraw{
				Sender:       providerAddr.String(),
				LeaseUuids:   []string{leaseID}, //nolint:staticcheck // exercising the deprecated field
				ProviderUuid: provider.Uuid,
			},
			wantErr:     types.ErrInvalidRequest,
			errContains: "lease_uuids is no longer supported",
		},
		{
			name: "fail: invalid sender address",
			msg: &types.MsgWithdraw{
				Sender:       "not-an-address",
				ProviderUuid: provider.Uuid,
			},
			wantErr:     types.ErrUnauthorized,
			errContains: "invalid sender address",
		},
		{
			name: "fail: unauthorized user cannot withdraw",
			msg: &types.MsgWithdraw{
				Sender:       unauthorizedUser.String(),
				ProviderUuid: provider.Uuid,
			},
			wantErr:     types.ErrUnauthorized,
			errContains: "not authorized",
		},
		{
			name: "fail: tenant cannot withdraw (not provider or authority)",
			msg: &types.MsgWithdraw{
				Sender:       tenant.String(),
				ProviderUuid: provider.Uuid,
			},
			wantErr:     types.ErrUnauthorized,
			errContains: "not authorized",
		},
		{
			name: "fail: payout address cannot withdraw (not provider or authority)",
			msg: &types.MsgWithdraw{
				Sender:       payoutAddr.String(),
				ProviderUuid: provider.Uuid,
			},
			wantErr:     types.ErrUnauthorized,
			errContains: "not authorized",
		},
		{
			name: "fail: limit above the maximum",
			msg: &types.MsgWithdraw{
				Sender:       providerAddr.String(),
				ProviderUuid: provider.Uuid,
				Limit:        types.MaxBatchLeaseSize + 1,
			},
			wantErr:     types.ErrInvalidLease,
			errContains: "exceeds maximum allowed",
		},
		{
			name: "fail: oversized pagination key",
			msg: &types.MsgWithdraw{
				Sender:       providerAddr.String(),
				ProviderUuid: provider.Uuid,
				Key:          make([]byte, types.MaxWithdrawCursorLen+1),
			},
			wantErr:     types.ErrInvalidRequest,
			errContains: "exceeds maximum",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := msgServer.Withdraw(f.Ctx, tc.msg)
			require.ErrorIs(t, err, tc.wantErr)
			require.ErrorContains(t, err, tc.errContains)
			require.Nil(t, resp)

			// A rejected withdrawal pays nothing and settles nothing.
			require.True(t, f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom).IsZero())
			require.Equal(t, fundAmount, f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, denom))
			stream, err := f.App.BillingKeeper.Streams.Get(f.Ctx, streamKey)
			require.NoError(t, err)
			require.Equal(t, streamBefore, stream)
		})
	}
}

// TestMsgWithdraw_PartialCreditExhaustion tests withdrawal when credit is partially exhausted.
func TestMsgWithdraw_PartialCreditExhaustion(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Set params with short min lease duration for testing
	params := types.DefaultParams()
	params.MaxLeasesPerTenant = 100
	params.MaxItemsPerLease = 20
	params.MinLeaseDuration = 10 // 10 seconds for testing
	err := f.App.BillingKeeper.SetParams(f.Ctx, params)
	require.NoError(t, err)

	// Create provider and SKU with 1 unit per second rate
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 3600 per hour = 1 per second

	// Fund tenant's credit account with enough for minimum duration + some extra
	// Min duration is 10 seconds at 1/second = 10 units minimum
	// We fund with 50 units (enough to create lease, but less than 100 accrued)
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	limitedFund := sdk.NewCoin(denom, sdkmath.NewInt(50))
	f.fundAccount(t, creditAddr, sdk.NewCoins(limitedFund))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create a lease and acknowledge it
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{
			SkuUuid:  sku.Uuid,
			Quantity: 1,
		},
	})

	acknowledgedAt := f.Ctx.BlockTime()
	streamKey := keeper.NewStreamKey(provider.Uuid, tenant, denom)

	// Advance block time by 100 seconds (should accrue 100 units, but only 50 available)
	newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
	f.Ctx = newCtx
	f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())

	// Provider withdraws - should only get the 50 of available credit
	resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:       providerAddr.String(),
		ProviderUuid: provider.Uuid,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, sdk.NewCoins(limitedFund), resp.TotalAmounts)
	require.Equal(t, uint64(1), resp.WithdrawalCount)

	// Verify provider's payout address received all the credit that was left, and no more
	payoutBalance := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom)
	require.Equal(t, sdkmath.NewInt(50), payoutBalance.Amount, "provider should receive exactly the credit balance")

	// Verify the credit is spent
	creditBalance := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, denom)
	require.True(t, creditBalance.Amount.IsZero(), "credit balance should be exhausted")

	// The withdrawal freezes the stream; it does not close the lease itself.
	stream, err := f.App.BillingKeeper.Streams.Get(f.Ctx, streamKey)
	require.NoError(t, err)
	require.True(t, stream.IsExhausted())
	require.Equal(t, f.Ctx.BlockTime(), *stream.ExhaustedAt)
	require.Equal(t, acknowledgedAt.Add(100*time.Second), stream.LastSettledAt)
	require.Equal(t, uint64(1), stream.LeaseCount)

	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State, "no lease is closed inside the withdraw tx")

	require.Equal(t, 1, countEvents(f.Ctx, types.EventTypeStreamExhausted))
	require.Zero(t, countEvents(f.Ctx, types.EventTypeLeaseAutoClose))
	exhausted := findEvent(t, f.Ctx, types.EventTypeStreamExhausted)
	require.Equal(t, provider.Uuid, attrValue(t, exhausted, types.AttributeKeyProviderUUID))
	require.Equal(t, tenant.String(), attrValue(t, exhausted, types.AttributeKeyTenant))
	require.Equal(t, denom, attrValue(t, exhausted, types.AttributeKeyDenom))
	batch := findEvent(t, f.Ctx, types.EventTypeBatchWithdraw)
	require.Equal(t, "50"+denom, attrValue(t, batch, types.AttributeKeyAmount))
	require.Equal(t, "1", attrValue(t, batch, types.AttributeKeyExhaustedCount))
	requireInvariants(t, f)

	// A second withdrawal finds a frozen stream: nothing accrues, nothing is paid.
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
	resp, err = msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:       providerAddr.String(),
		ProviderUuid: provider.Uuid,
	})
	require.NoError(t, err)
	require.True(t, resp.TotalAmounts.IsZero())
	require.Zero(t, resp.WithdrawalCount)

	// The EndBlocker closes the lease of the frozen stream.
	f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())
	require.NoError(t, f.App.BillingKeeper.EndBlocker(f.Ctx))
	require.Equal(t, 1, countEvents(f.Ctx, types.EventTypeLeaseAutoClose))

	lease, err = f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
	require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason)
	require.NotNil(t, lease.ClosedAt)
	require.Equal(t, f.Ctx.BlockTime(), *lease.ClosedAt)

	_, err = f.App.BillingKeeper.Streams.Get(f.Ctx, streamKey)
	require.ErrorIs(t, err, collections.ErrNotFound)

	creditAccount, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Zero(t, creditAccount.ActiveLeaseCount)
	require.True(t, creditAccount.ReservedAmounts.IsZero(), "the reservation is released with the lease")
	require.Equal(t, sdkmath.NewInt(50), f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom).Amount)
	requireInvariants(t, f)
}

// TestMsgWithdraw_ZeroDuration tests withdrawal with zero elapsed time.
func TestMsgWithdraw_ZeroDuration(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(100000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create a lease
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{
				SkuUuid:  sku.Uuid,
				Quantity: 1,
			},
		},
	})
	require.NoError(t, err)

	// requireNothingWithdrawn withdraws and checks that nothing was paid.
	requireNothingWithdrawn := func(t *testing.T) {
		t.Helper()
		f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())
		resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
			Sender:       providerAddr.String(),
			ProviderUuid: provider.Uuid,
		})
		require.NoError(t, err)
		require.True(t, resp.TotalAmounts.IsZero())
		require.Zero(t, resp.WithdrawalCount)
		require.False(t, resp.HasMore)
		require.Empty(t, resp.NextKey)
		require.Zero(t, countEvents(f.Ctx, types.EventTypeStreamSettled))
		require.Zero(t, countEvents(f.Ctx, "transfer"))
		require.True(t, f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom).IsZero())
		require.Equal(t, fundAmount, f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, denom))
	}

	t.Run("a pending lease has no stream to settle", func(t *testing.T) {
		// Billing starts at acknowledgement, however long the lease stays pending.
		f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
		requireNothingWithdrawn(t)
	})

	t.Run("an acknowledged lease has accrued nothing in the same block", func(t *testing.T) {
		_, err := msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{createResp.LeaseUuid},
		})
		require.NoError(t, err)

		// DO NOT advance block time - withdraw immediately
		// With zero duration, there's nothing to withdraw
		requireNothingWithdrawn(t)

		stream, err := f.App.BillingKeeper.Streams.Get(f.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, denom))
		require.NoError(t, err)
		require.Equal(t, f.Ctx.BlockTime(), stream.LastSettledAt)
		require.False(t, stream.IsExhausted(), "a funded stream with nothing accrued is not exhausted")
		requireInvariants(t, f)
	})
}

// TestMsgCloseLease_WithSettlement tests lease closure with settlement.
func TestMsgCloseLease_WithSettlement(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Create provider and SKU with 1 unit per second rate
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 3600 per hour = 1 per second

	// Fund tenant's credit account generously
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(100000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Get initial payout balance
	initialPayoutBalance := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom)

	// Create a lease and acknowledge it
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{
			SkuUuid:  sku.Uuid,
			Quantity: 1,
		},
	})

	// Advance block time by 100 seconds
	newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
	f.Ctx = newCtx
	f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())

	// Close lease (should settle outstanding amount)
	closeResp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)
	require.NotNil(t, closeResp)
	require.Equal(t, uint64(1), closeResp.ClosedCount)
	require.Equal(t, f.Ctx.BlockTime(), closeResp.ClosedAt)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(100))), closeResp.TotalSettledAmounts,
		"1/s for 100 seconds")

	// Verify lease is closed
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
	require.NotNil(t, lease.ClosedAt)
	require.Equal(t, f.Ctx.BlockTime(), *lease.ClosedAt)
	require.Empty(t, lease.ClosureReason, "a fully paid close without a reason records none")

	// Verify provider received exactly the accrued amount during closure
	finalPayoutBalance := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom)
	require.Equal(t, sdkmath.NewInt(100), finalPayoutBalance.Amount.Sub(initialPayoutBalance.Amount),
		"provider should have received settlement during close")
	require.Equal(t, fundAmount.Amount.SubRaw(100), f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, denom).Amount)

	// The lease was the stream's last one, so the stream is gone
	_, err = f.App.BillingKeeper.Streams.Get(f.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, denom))
	require.ErrorIs(t, err, collections.ErrNotFound)

	// The settlement is reported by stream_settled; lease_closed no longer carries amounts
	settled := findEvent(t, f.Ctx, types.EventTypeStreamSettled)
	require.Equal(t, "100"+denom, attrValue(t, settled, types.AttributeKeyAmount))
	require.Equal(t, payoutAddr.String(), attrValue(t, settled, types.AttributeKeyPayoutAddress))
	closed := findEvent(t, f.Ctx, types.EventTypeLeaseClosed)
	require.Equal(t, leaseID, attrValue(t, closed, types.AttributeKeyLeaseUUID))
	require.Equal(t, types.AttributeValueRoleTenant, attrValue(t, closed, types.AttributeKeyClosedBy))
	for _, attr := range closed.Attributes {
		require.NotEqual(t, types.AttributeKeySettledAmounts, attr.Key)
		require.NotEqual(t, "duration_seconds", attr.Key)
	}
	requireInvariants(t, f)
}

// TestMsgCloseLease_PartialSettlement tests lease closure with partial credit.
func TestMsgCloseLease_PartialSettlement(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Set params with short min lease duration for testing
	params := types.DefaultParams()
	params.MaxLeasesPerTenant = 100
	params.MaxItemsPerLease = 20
	params.MinLeaseDuration = 10 // 10 seconds for testing
	err := f.App.BillingKeeper.SetParams(f.Ctx, params)
	require.NoError(t, err)

	// Create provider and SKU with 1 unit per second rate
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 3600 per hour = 1 per second

	// Fund tenant's credit account with LIMITED funds (only 30 units)
	// Min duration is 10 seconds at 1/second = 10 units minimum
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	limitedFund := sdk.NewCoin(denom, sdkmath.NewInt(30))
	f.fundAccount(t, creditAddr, sdk.NewCoins(limitedFund))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create a lease and acknowledge it
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{
			SkuUuid:  sku.Uuid,
			Quantity: 1,
		},
	})

	// Advance block time by 100 seconds (should accrue 100 units, but only 30 available)
	newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
	f.Ctx = newCtx

	// Close lease (should settle only what's available)
	closeResp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)
	require.NotNil(t, closeResp)
	require.Equal(t, uint64(1), closeResp.ClosedCount)
	require.Equal(t, sdk.NewCoins(limitedFund), closeResp.TotalSettledAmounts)

	// Verify provider received 30 (all available credit)
	payoutBalance := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom)
	require.Equal(t, sdkmath.NewInt(30), payoutBalance.Amount)

	// Verify credit balance is now zero
	creditBalance := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, denom)
	require.True(t, creditBalance.Amount.IsZero())

	// The unpaid remainder makes this a credit exhaustion, whoever asked for the close
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
	require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason)

	// The close removed the stream it froze, so nothing is left for the EndBlocker
	_, err = f.App.BillingKeeper.Streams.Get(f.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, denom))
	require.ErrorIs(t, err, collections.ErrNotFound)
	requireInvariants(t, f)

	f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())
	require.NoError(t, f.App.BillingKeeper.EndBlocker(f.Ctx))
	require.Zero(t, countEvents(f.Ctx, types.EventTypeLeaseAutoClose))
}

// TestMsgCloseLease_CreditExhaustionLabelling verifies that a voluntary close is
// only recorded as credit_exhausted on a genuine shortfall. When the tenant's
// credit exactly covers the accrued charges — the accrued == balance case that
// the stream's GTE freeze boundary also treats as exhaustion — the caller's
// reason and role must be preserved. Regression for ENG-577.
func TestMsgCloseLease_CreditExhaustionLabelling(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Short min duration so a small credit balance can back a lease.
	params := types.DefaultParams()
	params.MinLeaseDuration = 10
	require.NoError(t, f.App.BillingKeeper.SetParams(f.Ctx, params))

	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 3600/hour = 1 umfx/second

	// fundTenant funds a tenant's credit account with exactly `amount` units.
	fundTenant := func(t *testing.T, tenant sdk.AccAddress, amount int64) {
		t.Helper()
		creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
		require.NoError(t, err)
		f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(amount))))
		require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
			Tenant:        tenant.String(),
			CreditAddress: creditAddr.String(),
		}))
	}

	// closedEventBy returns the closed_by attribute of the lease_closed event for leaseID.
	closedEventBy := func(leaseID string) string {
		for _, ev := range f.Ctx.EventManager().Events() {
			if ev.Type != types.EventTypeLeaseClosed {
				continue
			}
			var uuid, by string
			for _, a := range ev.Attributes {
				switch a.Key {
				case types.AttributeKeyLeaseUUID:
					uuid = a.Value
				case types.AttributeKeyClosedBy:
					by = a.Value
				}
			}
			if uuid == leaseID {
				return by
			}
		}
		return ""
	}

	t.Run("exact balance keeps caller reason (accrued == balance)", func(t *testing.T) {
		tenant := f.TestAccs[0]
		fundTenant(t, tenant, 100) // rate 1/s -> exactly 100s of runway

		leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		})

		// Advance exactly 100s: accrued (100) == balance (100).
		f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))

		const reason = "migrating-to-new-provider"
		_, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
			Sender:     tenant.String(),
			LeaseUuids: []string{leaseID},
			Reason:     reason,
		})
		require.NoError(t, err)

		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
		require.Equal(t, reason, lease.ClosureReason,
			"a fully-paid voluntary close must keep the caller's reason, not credit_exhausted")
		require.Equal(t, types.AttributeValueRoleTenant, closedEventBy(leaseID),
			"closed_by must be the caller role, not credit_exhaustion")

		// Full payment: provider received all 100 units.
		require.Equal(t, sdkmath.NewInt(100), f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom).Amount)
	})

	t.Run("genuine shortfall is recorded as credit_exhausted (accrued > balance)", func(t *testing.T) {
		tenant := f.TestAccs[3]
		fundTenant(t, tenant, 30) // only 30 units of runway

		leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		})

		// Advance 100s: accrued (100) > balance (30) -> genuine exhaustion.
		f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))

		_, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
			Sender:     tenant.String(),
			LeaseUuids: []string{leaseID},
			Reason:     "should-be-overridden",
		})
		require.NoError(t, err)

		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
		require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason,
			"an under-funded close is a genuine credit exhaustion")
		require.Equal(t, "credit_exhaustion", closedEventBy(leaseID),
			"closed_by must name credit exhaustion, not the caller role")
		requireInvariants(t, f)
	})

	t.Run("batch: earlier lease drawdown does not mislabel a fully-paid later lease", func(t *testing.T) {
		tenant := f.TestAccs[4]
		fundTenant(t, tenant, 200) // exactly enough for two 1/s leases over 100s

		leaseA := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		})
		leaseB := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		})

		f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))

		const reason = "planned-teardown"
		resp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
			Sender:     tenant.String(),
			LeaseUuids: []string{leaseA, leaseB},
			Reason:     reason,
		})
		require.NoError(t, err)
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(200))), resp.TotalSettledAmounts)

		// Both leases are fully paid (200 credit == 200 accrued total). They share
		// one stream, which is settled once for the message and drains the balance
		// to exactly zero; neither lease may be relabelled as credit exhaustion,
		// whatever its position in the batch.
		for _, id := range []string{leaseA, leaseB} {
			lease, err := f.App.BillingKeeper.GetLease(f.Ctx, id)
			require.NoError(t, err)
			require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
			require.Equal(t, reason, lease.ClosureReason,
				"lease %s: batch drawdown must not relabel a fully-paid close", id)
			require.Equal(t, types.AttributeValueRoleTenant, closedEventBy(id))
		}
		requireInvariants(t, f)
	})

	t.Run("batch: a shortfall labels every lease of the stream, whatever its position", func(t *testing.T) {
		// tenant4's earlier leases are closed; re-fund it one unit short of two
		// 1/s leases over 100s.
		tenant := f.TestAccs[4]
		fundTenant(t, tenant, 199)

		leaseA := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		})
		leaseB := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		})

		f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))

		resp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
			Sender:     tenant.String(),
			LeaseUuids: []string{leaseA, leaseB},
			Reason:     "should-be-overridden",
		})
		require.NoError(t, err)
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(199))), resp.TotalSettledAmounts)

		// Per-lease settlement would have paid lease A in full and labelled only B.
		// The stream is settled once, so both leases see the same shortfall.
		for _, id := range []string{leaseA, leaseB} {
			lease, err := f.App.BillingKeeper.GetLease(f.Ctx, id)
			require.NoError(t, err)
			require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
			require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason, "lease %s", id)
		}
		requireInvariants(t, f)
	})

	t.Run("overflow: an unsettled-for-a-century lease is still credit_exhausted", func(t *testing.T) {
		// tenant0's earlier lease is already closed; reuse and re-fund it.
		tenant := f.TestAccs[0]
		fundTenant(t, tenant, 5000)

		leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		})

		// Force the stream to look >100 years unsettled so accrual overflows.
		// The settlement then clamps the accrued amount to the remaining balance
		// and transfers it all (so the unpaid remainder reads zero) — the close
		// must still be recorded as credit exhaustion, not the caller reason.
		streamKey := keeper.NewStreamKey(provider.Uuid, tenant, denom)
		stream, err := f.App.BillingKeeper.Streams.Get(f.Ctx, streamKey)
		require.NoError(t, err)
		stream.LastSettledAt = f.Ctx.BlockTime().Add(-time.Duration(keeper.MaxDurationSeconds+10) * time.Second)
		require.NoError(t, f.App.BillingKeeper.Streams.Set(f.Ctx, streamKey, stream))

		resp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
			Sender:     tenant.String(),
			LeaseUuids: []string{leaseID},
			Reason:     "should-be-credit-exhausted",
		})
		require.NoError(t, err)
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(5000))), resp.TotalSettledAmounts,
			"an unrepresentable accrual transfers everything that remains")

		closed, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_CLOSED, closed.State)
		require.Equal(t, types.ClosureReasonCreditExhausted, closed.ClosureReason,
			"an overflow-driven close (accrued beyond representable range) is genuine exhaustion")
		requireInvariants(t, f)
	})

	t.Run("zero balance with zero elapsed time is still credit_exhausted", func(t *testing.T) {
		// tenant3's earlier lease is already closed; reuse and re-fund it.
		tenant := f.TestAccs[3]
		fundTenant(t, tenant, 100)
		creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
		require.NoError(t, err)

		leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		})

		// Drain the credit balance to zero WITHOUT advancing block time, so the
		// close sees duration == 0 and a zero balance. The stream freezes on its
		// GTE boundary (0 >= 0), but the settlement accrues and transfers nothing
		// (duration == 0), so there is no unpaid remainder — the close must still
		// be recorded as genuine credit exhaustion, not the reason.
		bal := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, denom)
		require.NoError(t, f.App.BankKeeper.SendCoins(f.Ctx, creditAddr, payoutAddr, sdk.NewCoins(bal)))

		resp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
			Sender:     tenant.String(),
			LeaseUuids: []string{leaseID},
			Reason:     "should-be-credit-exhausted",
		})
		require.NoError(t, err)
		require.True(t, resp.TotalSettledAmounts.IsZero())

		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
		require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason,
			"a zero-balance lease closed with no time elapsed is genuine exhaustion")
	})
}

// TestMsgCloseLease_ProviderClose tests that provider can close a lease.
func TestMsgCloseLease_ProviderClose(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(100000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create a lease and acknowledge it
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{
			SkuUuid:  sku.Uuid,
			Quantity: 1,
		},
	})

	// Advance block time
	newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(50 * time.Second))
	f.Ctx = newCtx

	// Provider closes the lease
	closeResp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)
	require.NotNil(t, closeResp)
	require.Equal(t, uint64(1), closeResp.ClosedCount)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(50))), closeResp.TotalSettledAmounts,
		"1/s for 50 seconds")
	require.Equal(t, sdkmath.NewInt(50), f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom).Amount)

	// Verify lease is closed
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
	requireInvariants(t, f)
}

// TestMsgCloseLease_AuthorityClose tests that authority can close any lease.
func TestMsgCloseLease_AuthorityClose(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(100000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create a lease and acknowledge it
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{
			SkuUuid:  sku.Uuid,
			Quantity: 1,
		},
	})

	// Authority closes the lease
	closeResp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     f.Authority.String(),
		LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)
	require.NotNil(t, closeResp)
	require.Equal(t, uint64(1), closeResp.ClosedCount)
	require.True(t, closeResp.TotalSettledAmounts.IsZero(), "closed in the block it was acknowledged in")

	// Verify lease is closed
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
	requireInvariants(t, f)
}

// TestMsgCloseLease_ErrorCases tests error scenarios for CloseLease.
func TestMsgCloseLease_ErrorCases(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	unauthorizedUser := f.TestAccs[3]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(100000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create a lease and acknowledge it (needed for close tests)
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{
			SkuUuid:  sku.Uuid,
			Quantity: 1,
		},
	})

	t.Run("fail: close non-existent lease", func(t *testing.T) {
		_, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
			Sender:     tenant.String(),
			LeaseUuids: []string{"01912345-6789-7abc-8def-999999999999"},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("fail: unauthorized user cannot close", func(t *testing.T) {
		_, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
			Sender:     unauthorizedUser.String(),
			LeaseUuids: []string{leaseID},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unauthorized")
	})

	t.Run("fail: close already closed lease", func(t *testing.T) {
		// First close the lease
		_, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
			Sender:     tenant.String(),
			LeaseUuids: []string{leaseID},
		})
		require.NoError(t, err)

		// Try to close again
		_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
			Sender:     tenant.String(),
			LeaseUuids: []string{leaseID},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not active")
	})
}

// TestMsgCloseLeaseBatch tests batch closure of multiple active leases.
func TestMsgCloseLeaseBatch(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	tenant2 := f.TestAccs[1]
	providerAddr := f.TestAccs[2]
	payoutAddr := f.TestAccs[3]
	provider2Addr := f.TestAccs[4]
	denom := testDenom

	// Create providers and SKUs (provider2 uses same addr for payout)
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 1 unit per second

	provider2 := f.createTestProvider(t, provider2Addr.String(), provider2Addr.String())
	sku2 := f.createTestSKU(t, provider2.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(100000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Fund tenant2's credit account
	creditAddr2, err := types.DeriveCreditAddressFromBech32(tenant2.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr2, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant2.String(),
		CreditAddress: creditAddr2.String(),
	})
	require.NoError(t, err)

	t.Run("success: tenant closes multiple leases at once", func(t *testing.T) {
		// Create and acknowledge 3 leases
		lease1 := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		})
		lease2 := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 2},
		})
		lease3 := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 3},
		})

		// Advance time to accrue some billing
		newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(10 * time.Second))
		f.Ctx = newCtx

		// Clear event manager to isolate close events
		f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())

		// Close all 3 at once
		resp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
			Sender:     tenant.String(),
			LeaseUuids: []string{lease1, lease2, lease3},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, uint64(3), resp.ClosedCount)
		require.Equal(t, f.Ctx.BlockTime(), resp.ClosedAt)
		// The 3 leases share one stream: (1 + 2 + 3) umfx/sec for 10 seconds, paid once.
		settledAmounts := sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(60)))
		require.Equal(t, settledAmounts, resp.TotalSettledAmounts)
		require.Equal(t, sdkmath.NewInt(60), f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom).Amount)
		require.Equal(t, 1, countEvents(f.Ctx, types.EventTypeStreamSettled), "one settlement for the whole batch")
		require.Equal(t, 1, countEvents(f.Ctx, "transfer"), "one bank transfer for the whole batch")

		// Verify all leases are now CLOSED
		for _, uuid := range []string{lease1, lease2, lease3} {
			lease, err := f.App.BillingKeeper.GetLease(f.Ctx, uuid)
			require.NoError(t, err)
			require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
			require.NotNil(t, lease.ClosedAt)
		}

		// Verify events: 3 per-lease events + 1 batch summary event
		events := f.Ctx.EventManager().Events()
		perLeaseEventCount := 0
		batchEventCount := 0
		for _, event := range events {
			switch event.Type {
			case types.EventTypeLeaseClosed:
				perLeaseEventCount++
			case types.EventTypeBatchClosed:
				batchEventCount++
				// Verify batch event attributes
				require.Equal(t, "3", attrValue(t, event, types.AttributeKeyLeaseCount))
				require.Equal(t, settledAmounts.String(), attrValue(t, event, types.AttributeKeySettledAmounts))
			}
		}
		require.Equal(t, 3, perLeaseEventCount, "should emit 3 per-lease events")
		require.Equal(t, 1, batchEventCount, "should emit 1 batch summary event")

		// The stream lost its last lease and is deleted
		_, err = f.App.BillingKeeper.Streams.Get(f.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, denom))
		require.ErrorIs(t, err, collections.ErrNotFound)
		requireInvariants(t, f)
	})

	t.Run("success: provider closes multiple leases", func(t *testing.T) {
		// Create and acknowledge leases for the same provider but different tenants
		lease1 := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		})
		lease2 := f.createAndAcknowledgeLease(t, msgServer, tenant2, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		})

		// Provider closes both leases
		resp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{lease1, lease2},
		})
		require.NoError(t, err)
		require.Equal(t, uint64(2), resp.ClosedCount)
	})

	t.Run("success: authority closes any leases", func(t *testing.T) {
		// Create leases for different providers
		lease1 := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		})
		lease2 := f.createAndAcknowledgeLease(t, msgServer, tenant, provider2Addr, []types.LeaseItemInput{
			{SkuUuid: sku2.Uuid, Quantity: 1},
		})

		// Authority can close leases from different providers
		resp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
			Sender:     f.Authority.String(),
			LeaseUuids: []string{lease1, lease2},
		})
		require.NoError(t, err)
		require.Equal(t, uint64(2), resp.ClosedCount)
	})

	t.Run("fail: tenant cannot close other tenant's lease in batch", func(t *testing.T) {
		// Create leases for different tenants
		lease1 := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		})
		lease2 := f.createAndAcknowledgeLease(t, msgServer, tenant2, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		})

		// Tenant1 tries to close both (includes tenant2's lease) - should fail
		_, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
			Sender:     tenant.String(),
			LeaseUuids: []string{lease1, lease2},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unauthorized")

		// Verify both leases are still ACTIVE (atomic rollback)
		l1, _ := f.App.BillingKeeper.GetLease(f.Ctx, lease1)
		require.Equal(t, types.LEASE_STATE_ACTIVE, l1.State)

		l2, _ := f.App.BillingKeeper.GetLease(f.Ctx, lease2)
		require.Equal(t, types.LEASE_STATE_ACTIVE, l2.State)
	})

	t.Run("fail: one lease not active (atomicity)", func(t *testing.T) {
		// Create one active lease
		activeLease := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		})

		// Create one pending lease
		pendingResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)

		// Try to close both (one is pending) - should fail
		_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
			Sender:     tenant.String(),
			LeaseUuids: []string{activeLease, pendingResp.LeaseUuid},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not active")

		// Verify the active lease is still ACTIVE (atomic - no partial success)
		lease, _ := f.App.BillingKeeper.GetLease(f.Ctx, activeLease)
		require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)
	})

	t.Run("fail: empty lease list", func(t *testing.T) {
		_, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
			Sender:     tenant.String(),
			LeaseUuids: []string{},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("fail: duplicate UUIDs", func(t *testing.T) {
		lease := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		})

		_, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
			Sender:     tenant.String(),
			LeaseUuids: []string{lease, lease},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "duplicate")
	})

	t.Run("success: aggregates settled amounts across denoms", func(t *testing.T) {
		// Create SKUs with different denoms
		sku3 := f.createTestSKUWithDenom(t, provider.Uuid, 3600, testDenom2)

		// Fund tenant with second denom
		f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom2, sdkmath.NewInt(100000000))))

		// Create two leases with different denoms
		lease1 := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		})
		lease2 := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku3.Uuid, Quantity: 1},
		})

		// The earlier subtests left 3 of this tenant's umfx leases with this
		// provider ACTIVE (1 umfx/sec each), so lease1 joined a stream of 4.
		umfxKey := keeper.NewStreamKey(provider.Uuid, tenant, denom)
		umfxStream, err := f.App.BillingKeeper.Streams.Get(f.Ctx, umfxKey)
		require.NoError(t, err)
		require.Equal(t, sdkmath.NewInt(4), umfxStream.Rate)
		require.Equal(t, uint64(4), umfxStream.LeaseCount)

		// Advance time
		newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(10 * time.Second))
		f.Ctx = newCtx

		// Close both leases
		resp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
			Sender:     tenant.String(),
			LeaseUuids: []string{lease1, lease2},
		})
		require.NoError(t, err)
		require.Equal(t, uint64(2), resp.ClosedCount)

		// TotalSettledAmounts is what the touched streams paid: closing one lease
		// of the umfx stream settles all 4 of its leases (4 × 10), and the upwr
		// stream held lease2 alone (1 × 10).
		require.Equal(t, sdk.NewCoins(
			sdk.NewCoin(denom, sdkmath.NewInt(40)),
			sdk.NewCoin(testDenom2, sdkmath.NewInt(10)),
		), resp.TotalSettledAmounts)

		// The umfx stream keeps its 3 other leases; the upwr stream is gone.
		umfxStream, err = f.App.BillingKeeper.Streams.Get(f.Ctx, umfxKey)
		require.NoError(t, err)
		require.Equal(t, sdkmath.NewInt(3), umfxStream.Rate)
		require.Equal(t, uint64(3), umfxStream.LeaseCount)
		require.Equal(t, f.Ctx.BlockTime(), umfxStream.LastSettledAt)
		_, err = f.App.BillingKeeper.Streams.Get(f.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom2))
		require.ErrorIs(t, err, collections.ErrNotFound)
		requireInvariants(t, f)
	})
}

// TestMsgWithdraw_FromClosedLease tests withdrawal from a closed lease.
func TestMsgWithdraw_FromClosedLease(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(100000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create a lease and acknowledge it, then close it
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{
			SkuUuid:  sku.Uuid,
			Quantity: 1,
		},
	})

	// Let the lease accrue, then close it: the close settles its stream
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
	closeResp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(100))), closeResp.TotalSettledAmounts)

	// Verify the lease is closed
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)

	// A closed lease has left its stream: however much time passes, a later
	// withdrawal finds nothing to pay and the lease is not billed past its close.
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
	resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:       providerAddr.String(),
		ProviderUuid: provider.Uuid,
	})
	require.NoError(t, err)
	require.True(t, resp.TotalAmounts.IsZero())
	require.Zero(t, resp.WithdrawalCount)
	require.False(t, resp.HasMore)

	require.Equal(t, sdkmath.NewInt(100), f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom).Amount)
	require.Equal(t, fundAmount.Amount.SubRaw(100), f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, denom).Amount)
	requireInvariants(t, f)
}

// =============================================================================
// Multi-Denom Tests
// =============================================================================

// TestMsgCreateLease_MultiDenom tests lease creation with SKUs using different denoms.
func TestMsgCreateLease_MultiDenom(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]

	// Create provider and two SKUs with different denoms
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku1 := f.createTestSKUWithDenom(t, provider.Uuid, 3600, testDenom)  // 1 per second in testDenom
	sku2 := f.createTestSKUWithDenom(t, provider.Uuid, 7200, testDenom2) // 2 per second in testDenom2

	// Fund tenant's credit account with BOTH denoms
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount1 := sdk.NewCoin(testDenom, sdkmath.NewInt(10000000))  // 10M testDenom
	fundAmount2 := sdk.NewCoin(testDenom2, sdkmath.NewInt(20000000)) // 20M testDenom2
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount1, fundAmount2))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	t.Run("success: create lease with multiple SKUs using different denoms", func(t *testing.T) {
		msg := &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items: []types.LeaseItemInput{
				{SkuUuid: sku1.Uuid, Quantity: 1}, // uses denom1
				{SkuUuid: sku2.Uuid, Quantity: 1}, // uses denom2
			},
		}
		resp, err := msgServer.CreateLease(f.Ctx, msg)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotEmpty(t, resp.LeaseUuid)

		// Verify lease was created with correct items
		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, resp.LeaseUuid)
		require.NoError(t, err)
		require.Len(t, lease.Items, 2)

		// Verify each item has the correct denom in its locked price
		require.Equal(t, testDenom, lease.Items[0].LockedPrice.Denom)
		require.Equal(t, testDenom2, lease.Items[1].LockedPrice.Denom)
	})
}

// TestMsgCreateLease_MultiDenom_InsufficientOneDenom tests that lease creation fails
// when the tenant has insufficient credit for one of the denoms.
func TestMsgCreateLease_MultiDenom_InsufficientOneDenom(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]

	// Create provider and two SKUs with different denoms
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku1 := f.createTestSKUWithDenom(t, provider.Uuid, 3600, testDenom)  // 1 per second in testDenom
	sku2 := f.createTestSKUWithDenom(t, provider.Uuid, 7200, testDenom2) // 2 per second in testDenom2

	// Fund tenant's credit account with ONLY testDenom (missing testDenom2)
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount1 := sdk.NewCoin(testDenom, sdkmath.NewInt(10000000)) // 10M testDenom
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount1))
	// NO testDenom2 funded!

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	t.Run("fail: insufficient credit for one denom", func(t *testing.T) {
		msg := &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items: []types.LeaseItemInput{
				{SkuUuid: sku1.Uuid, Quantity: 1}, // uses testDenom - has enough
				{SkuUuid: sku2.Uuid, Quantity: 1}, // uses testDenom2 - insufficient!
			},
		}
		resp, err := msgServer.CreateLease(f.Ctx, msg)
		require.Error(t, err)
		require.Contains(t, err.Error(), "insufficient credit")
		require.Contains(t, err.Error(), testDenom2) // Should mention the missing denom
		require.Nil(t, resp)
	})
}

// TestMsgWithdraw_MultiDenom tests withdrawal from a multi-denom lease.
func TestMsgWithdraw_MultiDenom(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]

	// Create provider and two SKUs with different denoms
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku1 := f.createTestSKUWithDenom(t, provider.Uuid, 3600, testDenom)  // 1 per second in testDenom
	sku2 := f.createTestSKUWithDenom(t, provider.Uuid, 7200, testDenom2) // 2 per second in testDenom2

	// Fund tenant's credit account with BOTH denoms
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount1 := sdk.NewCoin(testDenom, sdkmath.NewInt(100000000))  // 100M testDenom
	fundAmount2 := sdk.NewCoin(testDenom2, sdkmath.NewInt(200000000)) // 200M testDenom2
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount1, fundAmount2))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create lease with both SKUs and acknowledge it
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku1.Uuid, Quantity: 1},
		{SkuUuid: sku2.Uuid, Quantity: 1},
	})

	// Advance block time by 100 seconds
	// testDenom: 1/sec * 100 = 100
	// testDenom2: 2/sec * 100 = 200
	newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
	f.Ctx = newCtx

	t.Run("success: provider withdraws multiple denoms", func(t *testing.T) {
		// Get initial balances
		initialBalance1 := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom)
		initialBalance2 := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom2)

		f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())
		resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
			Sender:       providerAddr.String(),
			ProviderUuid: provider.Uuid,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, payoutAddr.String(), resp.PayoutAddress)

		// Verify provider received both denoms: the lease belongs to one stream per denom
		require.Equal(t, sdk.NewCoins(
			sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
			sdk.NewCoin(testDenom2, sdkmath.NewInt(200)),
		), resp.TotalAmounts)
		require.Equal(t, uint64(2), resp.WithdrawalCount, "one lease, two streams")
		require.Equal(t, 2, countEvents(f.Ctx, types.EventTypeStreamSettled))

		// Check actual balances increased by exactly the accrued amounts
		newBalance1 := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom)
		newBalance2 := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom2)

		require.Equal(t, sdkmath.NewInt(100), newBalance1.Amount.Sub(initialBalance1.Amount),
			"provider should receive testDenom funds")
		require.Equal(t, sdkmath.NewInt(200), newBalance2.Amount.Sub(initialBalance2.Amount),
			"provider should receive testDenom2 funds")

		// Both streams are settled up to now, and the lease stays active
		for _, streamDenom := range []string{testDenom, testDenom2} {
			stream, err := f.App.BillingKeeper.Streams.Get(f.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, streamDenom))
			require.NoError(t, err)
			require.Equal(t, f.Ctx.BlockTime(), stream.LastSettledAt)
			require.Equal(t, uint64(1), stream.LeaseCount)
		}
		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)
		requireInvariants(t, f)
	})
}

// TestMsgCloseLease_MultiDenom_Settlement tests that closing a multi-denom lease
// settles all denoms correctly.
func TestMsgCloseLease_MultiDenom_Settlement(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]

	// Create provider and two SKUs with different denoms
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku1 := f.createTestSKUWithDenom(t, provider.Uuid, 3600, testDenom)  // 1 per second in testDenom
	sku2 := f.createTestSKUWithDenom(t, provider.Uuid, 7200, testDenom2) // 2 per second in testDenom2

	// Fund tenant's credit account with BOTH denoms
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount1 := sdk.NewCoin(testDenom, sdkmath.NewInt(100000000))  // 100M testDenom
	fundAmount2 := sdk.NewCoin(testDenom2, sdkmath.NewInt(200000000)) // 200M testDenom2
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount1, fundAmount2))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create lease with both SKUs and acknowledge it
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku1.Uuid, Quantity: 1},
		{SkuUuid: sku2.Uuid, Quantity: 1},
	})

	// Advance block time by 100 seconds
	newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
	f.Ctx = newCtx

	// Get initial payout balances
	initialBalance1 := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom)
	initialBalance2 := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom2)

	t.Run("success: close lease settles all denoms", func(t *testing.T) {
		resp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
			Sender:     tenant.String(),
			LeaseUuids: []string{leaseID},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, uint64(1), resp.ClosedCount)
		require.Equal(t, sdk.NewCoins(
			sdk.NewCoin(testDenom, sdkmath.NewInt(100)),
			sdk.NewCoin(testDenom2, sdkmath.NewInt(200)),
		), resp.TotalSettledAmounts)

		// Verify lease is closed
		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)

		// Verify provider received both denoms via settlement
		newBalance1 := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom)
		newBalance2 := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom2)

		require.Equal(t, sdkmath.NewInt(100), newBalance1.Amount.Sub(initialBalance1.Amount),
			"provider should receive testDenom settlement")
		require.Equal(t, sdkmath.NewInt(200), newBalance2.Amount.Sub(initialBalance2.Amount),
			"provider should receive testDenom2 settlement")

		// The lease left both of its streams, which are deleted
		for _, streamDenom := range []string{testDenom, testDenom2} {
			_, err := f.App.BillingKeeper.Streams.Get(f.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, streamDenom))
			require.ErrorIs(t, err, collections.ErrNotFound)
		}
		requireInvariants(t, f)
	})
}

// TestMsgFundCredit_MultiDenom tests funding a credit account with multiple denoms.
func TestMsgFundCredit_MultiDenom(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	sender := f.TestAccs[0]
	tenant := f.TestAccs[1]

	// Fund sender with both denoms
	fundAmount1 := sdk.NewCoin(testDenom, sdkmath.NewInt(10000000))
	fundAmount2 := sdk.NewCoin(testDenom2, sdkmath.NewInt(20000000))
	f.fundAccount(t, sender, sdk.NewCoins(fundAmount1, fundAmount2))

	t.Run("success: fund credit with first denom", func(t *testing.T) {
		resp, err := msgServer.FundCredit(f.Ctx, &types.MsgFundCredit{
			Sender: sender.String(),
			Tenant: tenant.String(),
			Amount: sdk.NewCoin(testDenom, sdkmath.NewInt(5000000)),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, testDenom, resp.NewBalance.Denom)
	})

	t.Run("success: fund credit with second denom", func(t *testing.T) {
		resp, err := msgServer.FundCredit(f.Ctx, &types.MsgFundCredit{
			Sender: sender.String(),
			Tenant: tenant.String(),
			Amount: sdk.NewCoin(testDenom2, sdkmath.NewInt(10000000)),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, testDenom2, resp.NewBalance.Denom)
	})

	t.Run("success: verify credit account has both denoms", func(t *testing.T) {
		creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
		require.NoError(t, err)

		balance1 := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom)
		balance2 := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom2)

		require.Equal(t, sdkmath.NewInt(5000000), balance1.Amount)
		require.Equal(t, sdkmath.NewInt(10000000), balance2.Amount)
	})
}

// TestMsgCreateLease_MultiDenom_SameDenomMultipleSKUs tests lease creation
// with multiple SKUs that use the same denom.
func TestMsgCreateLease_MultiDenom_SameDenomMultipleSKUs(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]

	// Create provider and multiple SKUs with the SAME denom
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku1 := f.createTestSKUWithDenom(t, provider.Uuid, 3600, testDenom)  // 1 per second
	sku2 := f.createTestSKUWithDenom(t, provider.Uuid, 7200, testDenom)  // 2 per second
	sku3 := f.createTestSKUWithDenom(t, provider.Uuid, 10800, testDenom) // 3 per second

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	// Total rate = 1+2+3 = 6/sec, min duration = 3600, need at least 21,600
	fundAmount := sdk.NewCoin(testDenom, sdkmath.NewInt(100000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	t.Run("success: create lease with multiple SKUs same denom", func(t *testing.T) {
		msg := &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items: []types.LeaseItemInput{
				{SkuUuid: sku1.Uuid, Quantity: 1},
				{SkuUuid: sku2.Uuid, Quantity: 1},
				{SkuUuid: sku3.Uuid, Quantity: 1},
			},
		}
		resp, err := msgServer.CreateLease(f.Ctx, msg)
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Verify lease items all have the same denom
		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, resp.LeaseUuid)
		require.NoError(t, err)
		require.Len(t, lease.Items, 3)

		for _, item := range lease.Items {
			require.Equal(t, testDenom, item.LockedPrice.Denom)
		}
	})
}

// =============================================================================
// Lease Lifecycle Tests (Acknowledge/Reject/Cancel)
// =============================================================================

// TestMsgAcknowledgeLease tests the provider acknowledgement of pending leases.
func TestMsgAcknowledgeLease(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	unauthorizedUser := f.TestAccs[3]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(10000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create a lease (starts in PENDING state)
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		},
	})
	require.NoError(t, err)
	leaseID := createResp.LeaseUuid

	// Verify lease is PENDING
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_PENDING, lease.State)

	t.Run("success: provider acknowledges lease", func(t *testing.T) {
		// Clear event manager to isolate acknowledge events
		f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())

		resp, err := msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{leaseID},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.False(t, resp.AcknowledgedAt.IsZero())

		// Verify lease is now ACTIVE
		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)
		require.NotNil(t, lease.AcknowledgedAt)
		require.Nil(t, lease.LastSettledAt, "the deprecated field is never set") //nolint:staticcheck // asserting the deprecated field stays unset

		// Verify the lease joined its stream: billing starts at acknowledgement
		stream, err := f.App.BillingKeeper.Streams.Get(f.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
		require.NoError(t, err)
		require.Equal(t, sdkmath.NewInt(1), stream.Rate)
		require.Equal(t, uint64(1), stream.LeaseCount)
		require.Equal(t, f.Ctx.BlockTime(), stream.LastSettledAt)
		require.False(t, stream.IsExhausted())
		requireInvariants(t, f)

		// Verify single lease does NOT emit batch event
		events := f.Ctx.EventManager().Events()
		perLeaseEventCount := 0
		batchEventCount := 0
		for _, event := range events {
			switch event.Type {
			case types.EventTypeLeaseAcknowledged:
				perLeaseEventCount++
			case types.EventTypeBatchAcknowledged:
				batchEventCount++
			}
		}
		require.Equal(t, 1, perLeaseEventCount, "should emit 1 per-lease event")
		require.Equal(t, 0, batchEventCount, "should NOT emit batch event for single lease")
	})

	// Create another lease for error tests
	createResp2, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	leaseID2 := createResp2.LeaseUuid

	t.Run("fail: unauthorized user cannot acknowledge", func(t *testing.T) {
		_, err := msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
			Sender:     unauthorizedUser.String(),
			LeaseUuids: []string{leaseID2},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unauthorized")
	})

	t.Run("fail: cannot acknowledge non-existent lease", func(t *testing.T) {
		_, err := msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{"01912345-6789-7abc-8def-999999999999"},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("fail: cannot acknowledge already active lease", func(t *testing.T) {
		_, err := msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{leaseID}, // Already acknowledged above
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not in PENDING state")
	})

	t.Run("success: authority can acknowledge lease", func(t *testing.T) {
		resp, err := msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
			Sender:     f.Authority.String(),
			LeaseUuids: []string{leaseID2},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
	})
}

// TestMsgAcknowledgeLeaseBatch tests batch acknowledgement of multiple leases.
func TestMsgAcknowledgeLeaseBatch(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	provider2Addr := f.TestAccs[3]
	payout2Addr := f.TestAccs[4]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Create second provider for mixed-provider test
	provider2 := f.createTestProvider(t, provider2Addr.String(), payout2Addr.String())
	sku2 := f.createTestSKU(t, provider2.Uuid, 3600)

	// Fund tenant's credit account generously
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(100000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	t.Run("success: acknowledge multiple leases at once", func(t *testing.T) {
		// Create 3 pending leases for the same provider
		lease1, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)

		lease2, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 2}},
		})
		require.NoError(t, err)

		lease3, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 3}},
		})
		require.NoError(t, err)

		// Clear event manager to isolate acknowledge events
		f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())

		// Acknowledge all 3 at once
		resp, err := msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{lease1.LeaseUuid, lease2.LeaseUuid, lease3.LeaseUuid},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, uint64(3), resp.AcknowledgedCount)
		require.False(t, resp.AcknowledgedAt.IsZero())

		// Verify all leases are now ACTIVE
		for _, uuid := range []string{lease1.LeaseUuid, lease2.LeaseUuid, lease3.LeaseUuid} {
			lease, err := f.App.BillingKeeper.GetLease(f.Ctx, uuid)
			require.NoError(t, err)
			require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)
			require.NotNil(t, lease.AcknowledgedAt)
		}

		// Verify events: 3 per-lease events + 1 batch summary event
		events := f.Ctx.EventManager().Events()
		perLeaseEventCount := 0
		batchEventCount := 0
		for _, event := range events {
			switch event.Type {
			case types.EventTypeLeaseAcknowledged:
				perLeaseEventCount++
			case types.EventTypeBatchAcknowledged:
				batchEventCount++
				// Verify batch event attributes
				for _, attr := range event.Attributes {
					switch attr.Key {
					case types.AttributeKeyLeaseCount:
						require.Equal(t, "3", attr.Value)
					case types.AttributeKeyProviderUUID:
						require.Equal(t, provider.Uuid, attr.Value)
					case types.AttributeKeyAcknowledgedBy:
						require.Equal(t, providerAddr.String(), attr.Value)
					}
				}
			}
		}
		require.Equal(t, 3, perLeaseEventCount, "should emit 3 per-lease events")
		require.Equal(t, 1, batchEventCount, "should emit 1 batch summary event")

		// Verify the 3 leases joined one stream: (1 + 2 + 3) umfx/sec
		stream, err := f.App.BillingKeeper.Streams.Get(f.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, denom))
		require.NoError(t, err)
		require.Equal(t, sdkmath.NewInt(6), stream.Rate)
		require.Equal(t, uint64(3), stream.LeaseCount)
		require.Equal(t, f.Ctx.BlockTime(), stream.LastSettledAt)
		requireInvariants(t, f)
	})

	t.Run("fail: mixed providers", func(t *testing.T) {
		// Create leases for different providers
		leaseP1, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)

		leaseP2, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku2.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)

		// Try to acknowledge both (different providers) - should fail
		_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{leaseP1.LeaseUuid, leaseP2.LeaseUuid},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "belongs to provider")

		// Verify both leases are still PENDING (atomic rollback)
		lease1, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseP1.LeaseUuid)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_PENDING, lease1.State)

		lease2, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseP2.LeaseUuid)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_PENDING, lease2.State)
	})

	t.Run("fail: one lease not pending (atomicity)", func(t *testing.T) {
		// Create two leases
		pendingLease, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)

		activeLease, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)

		// Acknowledge one lease first
		_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{activeLease.LeaseUuid},
		})
		require.NoError(t, err)

		// Try to acknowledge both (one already active) - should fail
		_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{pendingLease.LeaseUuid, activeLease.LeaseUuid},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not in PENDING state")

		// Verify the pending lease is still PENDING (atomic - no partial success)
		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, pendingLease.LeaseUuid)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_PENDING, lease.State)

		// The failed batch left the stream as it was: the pending lease did not join it
		stream, err := f.App.BillingKeeper.Streams.Get(f.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, denom))
		require.NoError(t, err)
		require.Equal(t, sdkmath.NewInt(7), stream.Rate, "the 3 leases of the first subtest and the one acknowledged here")
		require.Equal(t, uint64(4), stream.LeaseCount)
		requireInvariants(t, f)
	})

	t.Run("fail: empty lease list", func(t *testing.T) {
		_, err := msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("fail: duplicate UUIDs", func(t *testing.T) {
		lease, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)

		_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{lease.LeaseUuid, lease.LeaseUuid},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "duplicate")
	})

	t.Run("fail: missing credit account (data integrity)", func(t *testing.T) {
		// Create a new tenant address without a credit account
		orphanTenant := sdk.AccAddress("orphan_tenant_addr__")

		// Directly create a lease in the store (bypassing normal flow to simulate data integrity issue)
		// Use valid UUIDv7 format (version 7 = 0x7xxx in the version nibble)
		orphanLeaseUUID := "01912345-6789-7abc-8def-ffffffffffff"
		orphanLease := types.Lease{
			Uuid:         orphanLeaseUUID,
			Tenant:       orphanTenant.String(),
			ProviderUuid: provider.Uuid,
			State:        types.LEASE_STATE_PENDING,
			Items: []types.LeaseItem{
				{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewInt64Coin(denom, 3600)},
			},
			CreatedAt: f.Ctx.BlockTime(),
		}
		err := f.App.BillingKeeper.SetLease(f.Ctx, orphanLease)
		require.NoError(t, err)

		// Try to acknowledge - should fail because credit account doesn't exist
		_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{orphanLeaseUUID},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "credit account not found")
		require.Contains(t, err.Error(), "data integrity issue")

		// Verify lease is still PENDING (no partial state change)
		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, orphanLeaseUUID)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_PENDING, lease.State)
	})
}

// TestMsgRejectLease tests the provider rejection of pending leases.
func TestMsgRejectLease(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	unauthorizedUser := f.TestAccs[3]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(10000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:           tenant.String(),
		CreditAddress:    creditAddr.String(),
		ActiveLeaseCount: 0,
	})
	require.NoError(t, err)

	// Create a lease (starts in PENDING state)
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	leaseID := createResp.LeaseUuid

	t.Run("success: provider rejects lease with reason", func(t *testing.T) {
		resp, err := msgServer.RejectLease(f.Ctx, &types.MsgRejectLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{leaseID},
			Reason:     "Resource unavailable",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.False(t, resp.RejectedAt.IsZero())
		require.Equal(t, uint64(1), resp.RejectedCount)

		// Verify lease is now REJECTED
		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_REJECTED, lease.State)
		require.NotNil(t, lease.RejectedAt)
		require.Equal(t, "Resource unavailable", lease.RejectionReason)
	})

	// Create another lease for more tests
	createResp2, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	leaseID2 := createResp2.LeaseUuid

	t.Run("fail: unauthorized user cannot reject", func(t *testing.T) {
		_, err := msgServer.RejectLease(f.Ctx, &types.MsgRejectLease{
			Sender:     unauthorizedUser.String(),
			LeaseUuids: []string{leaseID2},
			Reason:     "Test",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unauthorized")
	})

	t.Run("fail: cannot reject already rejected lease", func(t *testing.T) {
		_, err := msgServer.RejectLease(f.Ctx, &types.MsgRejectLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{leaseID}, // Already rejected above
			Reason:     "Test",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not in PENDING state")
	})

	t.Run("success: authority can reject lease", func(t *testing.T) {
		resp, err := msgServer.RejectLease(f.Ctx, &types.MsgRejectLease{
			Sender:     f.Authority.String(),
			LeaseUuids: []string{leaseID2},
			Reason:     "Admin rejection",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, uint64(1), resp.RejectedCount)
	})
}

// TestMsgRejectLeaseBatch tests batch rejection of multiple pending leases.
func TestMsgRejectLeaseBatch(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	provider2Addr := f.TestAccs[3]
	payout2Addr := f.TestAccs[4]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Create second provider for mixed-provider test
	provider2 := f.createTestProvider(t, provider2Addr.String(), payout2Addr.String())
	sku2 := f.createTestSKU(t, provider2.Uuid, 3600)

	// Fund tenant's credit account generously
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(100000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	t.Run("success: reject multiple leases at once", func(t *testing.T) {
		// Create 3 pending leases for the same provider
		lease1, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)

		lease2, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 2}},
		})
		require.NoError(t, err)

		lease3, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 3}},
		})
		require.NoError(t, err)

		// Clear event manager to isolate reject events
		f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())

		// Reject all 3 at once
		resp, err := msgServer.RejectLease(f.Ctx, &types.MsgRejectLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{lease1.LeaseUuid, lease2.LeaseUuid, lease3.LeaseUuid},
			Reason:     "Batch rejection test",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, uint64(3), resp.RejectedCount)
		require.False(t, resp.RejectedAt.IsZero())

		// Verify all leases are now REJECTED
		for _, uuid := range []string{lease1.LeaseUuid, lease2.LeaseUuid, lease3.LeaseUuid} {
			lease, err := f.App.BillingKeeper.GetLease(f.Ctx, uuid)
			require.NoError(t, err)
			require.Equal(t, types.LEASE_STATE_REJECTED, lease.State)
			require.NotNil(t, lease.RejectedAt)
			require.Equal(t, "Batch rejection test", lease.RejectionReason)
		}

		// Verify events: 3 per-lease events + 1 batch summary event
		events := f.Ctx.EventManager().Events()
		perLeaseEventCount := 0
		batchEventCount := 0
		for _, event := range events {
			switch event.Type {
			case types.EventTypeLeaseRejected:
				perLeaseEventCount++
			case types.EventTypeBatchRejected:
				batchEventCount++
				// Verify batch event attributes
				for _, attr := range event.Attributes {
					switch attr.Key {
					case types.AttributeKeyLeaseCount:
						require.Equal(t, "3", attr.Value)
					case types.AttributeKeyProviderUUID:
						require.Equal(t, provider.Uuid, attr.Value)
					case types.AttributeKeyRejectedBy:
						require.Equal(t, providerAddr.String(), attr.Value)
					}
				}
			}
		}
		require.Equal(t, 3, perLeaseEventCount, "should emit 3 per-lease events")
		require.Equal(t, 1, batchEventCount, "should emit 1 batch summary event")
	})

	t.Run("fail: mixed providers", func(t *testing.T) {
		// Create leases for different providers
		leaseP1, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)

		leaseP2, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku2.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)

		// Try to reject both (different providers) - should fail
		_, err = msgServer.RejectLease(f.Ctx, &types.MsgRejectLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{leaseP1.LeaseUuid, leaseP2.LeaseUuid},
			Reason:     "Test",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "belongs to provider")

		// Verify both leases are still PENDING (atomic rollback)
		lease1, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseP1.LeaseUuid)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_PENDING, lease1.State)

		lease2, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseP2.LeaseUuid)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_PENDING, lease2.State)
	})

	t.Run("fail: one lease not pending (atomicity)", func(t *testing.T) {
		// Create two leases
		pendingLease, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)

		ackLease, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)

		// Acknowledge one lease first (making it ACTIVE)
		_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{ackLease.LeaseUuid},
		})
		require.NoError(t, err)

		// Try to reject both (one is ACTIVE) - should fail
		_, err = msgServer.RejectLease(f.Ctx, &types.MsgRejectLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{pendingLease.LeaseUuid, ackLease.LeaseUuid},
			Reason:     "Test",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not in PENDING state")

		// Verify the pending lease is still PENDING (atomic - no partial success)
		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, pendingLease.LeaseUuid)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_PENDING, lease.State)
	})

	t.Run("fail: empty lease list", func(t *testing.T) {
		_, err := msgServer.RejectLease(f.Ctx, &types.MsgRejectLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{},
			Reason:     "Test",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("fail: duplicate UUIDs", func(t *testing.T) {
		lease, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)

		_, err = msgServer.RejectLease(f.Ctx, &types.MsgRejectLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{lease.LeaseUuid, lease.LeaseUuid},
			Reason:     "Test",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "duplicate")
	})
}

// TestMsgCancelLease tests the tenant cancellation of pending leases.
func TestMsgCancelLease(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	otherTenant := f.TestAccs[1]
	providerAddr := f.TestAccs[2]
	payoutAddr := f.TestAccs[3]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(10000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:           tenant.String(),
		CreditAddress:    creditAddr.String(),
		ActiveLeaseCount: 0,
	})
	require.NoError(t, err)

	// Create a lease (starts in PENDING state)
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	leaseID := createResp.LeaseUuid

	t.Run("success: tenant cancels own pending lease", func(t *testing.T) {
		resp, err := msgServer.CancelLease(f.Ctx, &types.MsgCancelLease{
			Tenant:     tenant.String(),
			LeaseUuids: []string{leaseID},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.False(t, resp.CancelledAt.IsZero())
		require.Equal(t, uint64(1), resp.CancelledCount)

		// Verify lease is now REJECTED (cancelled)
		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_REJECTED, lease.State)
	})

	// Create another lease for error tests
	createResp2, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	leaseID2 := createResp2.LeaseUuid

	t.Run("fail: other tenant cannot cancel someone else's lease", func(t *testing.T) {
		// otherTenant is not the tenant of leaseID2, so ownership check fails
		_, err := msgServer.CancelLease(f.Ctx, &types.MsgCancelLease{
			Tenant:     otherTenant.String(),
			LeaseUuids: []string{leaseID2},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "is not the tenant")
	})

	t.Run("fail: cannot cancel non-existent lease", func(t *testing.T) {
		_, err := msgServer.CancelLease(f.Ctx, &types.MsgCancelLease{
			Tenant:     tenant.String(),
			LeaseUuids: []string{"01912345-6789-7abc-8def-999999999999"},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	// First acknowledge the lease, then try to cancel
	_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{leaseID2},
	})
	require.NoError(t, err)

	t.Run("fail: cannot cancel active lease", func(t *testing.T) {
		_, err := msgServer.CancelLease(f.Ctx, &types.MsgCancelLease{
			Tenant:     tenant.String(),
			LeaseUuids: []string{leaseID2},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not in PENDING state")
	})
}

// TestMsgCancelLeaseBatch tests batch cancel operations.
func TestMsgCancelLeaseBatch(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(100000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:           tenant.String(),
		CreditAddress:    creditAddr.String(),
		ActiveLeaseCount: 0,
	})
	require.NoError(t, err)

	t.Run("success: batch cancel multiple pending leases", func(t *testing.T) {
		// Create 3 leases
		lease1, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)

		lease2, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)

		lease3, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)

		// Cancel all 3 leases in one transaction
		resp, err := msgServer.CancelLease(f.Ctx, &types.MsgCancelLease{
			Tenant:     tenant.String(),
			LeaseUuids: []string{lease1.LeaseUuid, lease2.LeaseUuid, lease3.LeaseUuid},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.False(t, resp.CancelledAt.IsZero())
		require.Equal(t, uint64(3), resp.CancelledCount)

		// Verify all leases are REJECTED
		for _, leaseUUID := range []string{lease1.LeaseUuid, lease2.LeaseUuid, lease3.LeaseUuid} {
			lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseUUID)
			require.NoError(t, err)
			require.Equal(t, types.LEASE_STATE_REJECTED, lease.State)
			require.Equal(t, "cancelled by tenant", lease.RejectionReason)
		}
	})

	t.Run("fail: atomicity - one non-pending lease fails entire batch", func(t *testing.T) {
		// Create 2 pending leases
		lease1, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)

		lease2, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)

		// Acknowledge lease1 to make it ACTIVE
		_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{lease1.LeaseUuid},
		})
		require.NoError(t, err)

		// Verify lease1 is now ACTIVE
		l1, err := f.App.BillingKeeper.GetLease(f.Ctx, lease1.LeaseUuid)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_ACTIVE, l1.State)

		// Try to cancel both - should fail because lease1 is ACTIVE
		_, err = msgServer.CancelLease(f.Ctx, &types.MsgCancelLease{
			Tenant:     tenant.String(),
			LeaseUuids: []string{lease1.LeaseUuid, lease2.LeaseUuid},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not in PENDING state")

		// Verify lease2 is still PENDING (atomicity: no partial changes)
		l2, err := f.App.BillingKeeper.GetLease(f.Ctx, lease2.LeaseUuid)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_PENDING, l2.State)
	})

	t.Run("fail: duplicate UUIDs in request", func(t *testing.T) {
		lease, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)

		_, err = msgServer.CancelLease(f.Ctx, &types.MsgCancelLease{
			Tenant:     tenant.String(),
			LeaseUuids: []string{lease.LeaseUuid, lease.LeaseUuid},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "duplicate")
	})

	t.Run("fail: empty lease_uuids", func(t *testing.T) {
		_, err := msgServer.CancelLease(f.Ctx, &types.MsgCancelLease{
			Tenant:     tenant.String(),
			LeaseUuids: []string{},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot be empty")
	})
}

// TestEndBlockerPendingLeaseExpiration tests that pending leases expire after the timeout.
func TestEndBlockerPendingLeaseExpiration(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Set a short pending timeout for testing (60 seconds)
	params := types.DefaultParams()
	params.PendingTimeout = 60 // 60 seconds
	err := f.App.BillingKeeper.SetParams(f.Ctx, params)
	require.NoError(t, err)

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(10000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create a lease (will be in PENDING state)
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		},
	})
	require.NoError(t, err)
	leaseUUID := createResp.LeaseUuid

	// Verify lease is in PENDING state
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseUUID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_PENDING, lease.State)

	// Verify pending lease count is 1
	creditAccount, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(1), creditAccount.PendingLeaseCount)

	t.Run("lease not expired before timeout", func(t *testing.T) {
		// Advance time by 30 seconds (less than timeout)
		newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(30 * time.Second))

		// Run EndBlocker
		err := f.App.BillingKeeper.EndBlocker(newCtx)
		require.NoError(t, err)

		// Lease should still be PENDING
		lease, err := f.App.BillingKeeper.GetLease(newCtx, leaseUUID)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_PENDING, lease.State)
	})

	t.Run("lease expires after timeout", func(t *testing.T) {
		// Advance time by 61 seconds (past the timeout)
		newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(61 * time.Second))

		// Run EndBlocker
		err := f.App.BillingKeeper.EndBlocker(newCtx)
		require.NoError(t, err)

		// Lease should now be EXPIRED
		lease, err := f.App.BillingKeeper.GetLease(newCtx, leaseUUID)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_EXPIRED, lease.State)
		require.NotNil(t, lease.ExpiredAt)

		// Pending lease count should be decremented
		creditAccount, err := f.App.BillingKeeper.GetCreditAccount(newCtx, tenant.String())
		require.NoError(t, err)
		require.Equal(t, uint64(0), creditAccount.PendingLeaseCount)
	})
}

// TestLeaseLifecycleEvents tests that proper events are emitted during lease lifecycle.
func TestLeaseLifecycleEvents(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(10000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create and acknowledge a lease
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)

	// Clear events before acknowledge
	f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())

	_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{createResp.LeaseUuid},
	})
	require.NoError(t, err)

	// Check for acknowledge event
	events := f.Ctx.EventManager().Events()
	foundAckEvent := false
	for _, event := range events {
		if event.Type == types.EventTypeLeaseAcknowledged {
			foundAckEvent = true
			break
		}
	}
	require.True(t, foundAckEvent, "lease_acknowledged event should be emitted")

	// Create another lease and reject it
	createResp2, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)

	// Clear events before reject
	f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())

	_, err = msgServer.RejectLease(f.Ctx, &types.MsgRejectLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{createResp2.LeaseUuid},
		Reason:     "Test rejection",
	})
	require.NoError(t, err)

	// Check for reject event
	events = f.Ctx.EventManager().Events()
	foundRejectEvent := false
	for _, event := range events {
		if event.Type == types.EventTypeLeaseRejected {
			foundRejectEvent = true
			break
		}
	}
	require.True(t, foundRejectEvent, "lease_rejected event should be emitted")
}

// TestMsgCreateLease_AllSKUsSameProvider tests that all SKUs in a lease must be from the same provider.
func TestMsgCreateLease_AllSKUsSameProvider(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	provider1Addr := f.TestAccs[1]
	provider2Addr := f.TestAccs[2]
	payout1Addr := f.TestAccs[3]
	payout2Addr := f.TestAccs[4]
	denom := testDenom

	// Create two different providers
	provider1 := f.createTestProvider(t, provider1Addr.String(), payout1Addr.String())
	provider2 := f.createTestProvider(t, provider2Addr.String(), payout2Addr.String())

	// Create SKUs for each provider
	sku1 := f.createTestSKU(t, provider1.Uuid, 3600)
	sku2 := f.createTestSKUWithDenom(t, provider2.Uuid, 3600, testDenom)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(100000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	t.Run("fail: SKUs from different providers", func(t *testing.T) {
		_, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items: []types.LeaseItemInput{
				{SkuUuid: sku1.Uuid, Quantity: 1}, // Provider 1
				{SkuUuid: sku2.Uuid, Quantity: 1}, // Provider 2
			},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "all SKUs in a lease must belong to the same provider")
	})

	t.Run("success: all SKUs from same provider", func(t *testing.T) {
		// Create another SKU for provider 1
		sku1b := f.createTestSKU(t, provider1.Uuid, 7200)

		resp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items: []types.LeaseItemInput{
				{SkuUuid: sku1.Uuid, Quantity: 1},
				{SkuUuid: sku1b.Uuid, Quantity: 1},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
	})
}

// TestMsgCreateLease_MaxLeasesLimit tests the max active leases per tenant limit.
func TestMsgCreateLease_MaxLeasesLimit(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Set params with low max leases (this uses MaxLeasesPerTenant, not MaxPendingLeasesPerTenant)
	params := types.DefaultParams()
	params.MaxLeasesPerTenant = 2
	err := f.App.BillingKeeper.SetParams(f.Ctx, params)
	require.NoError(t, err)

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(100000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	// Create credit account with ActiveLeaseCount already at max-1
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:           tenant.String(),
		CreditAddress:    creditAddr.String(),
		ActiveLeaseCount: 2, // Already at max
	})
	require.NoError(t, err)

	t.Run("fail: max leases reached", func(t *testing.T) {
		_, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "max")
	})
}

// TestEndBlockerMultiplePendingLeases tests that EndBlocker correctly handles multiple pending leases.
func TestEndBlockerMultiplePendingLeases(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Set a short pending timeout for testing (60 seconds)
	params := types.DefaultParams()
	params.PendingTimeout = 60 // 60 seconds
	params.MaxPendingLeasesPerTenant = 5
	err := f.App.BillingKeeper.SetParams(f.Ctx, params)
	require.NoError(t, err)

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(100000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create 3 leases at different times
	var leaseUUIDs []string

	// Lease 1 at t=0
	createResp1, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	leaseUUIDs = append(leaseUUIDs, createResp1.LeaseUuid)

	// Advance time by 20 seconds for lease 2
	ctx20 := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(20 * time.Second))

	// Lease 2 at t=20
	createResp2, err := msgServer.CreateLease(ctx20, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	leaseUUIDs = append(leaseUUIDs, createResp2.LeaseUuid)

	// Advance time by 40 seconds for lease 3
	ctx40 := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(40 * time.Second))

	// Lease 3 at t=40
	createResp3, err := msgServer.CreateLease(ctx40, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	leaseUUIDs = append(leaseUUIDs, createResp3.LeaseUuid)

	// Verify all 3 leases are pending
	creditAccount, err := f.App.BillingKeeper.GetCreditAccount(ctx40, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(3), creditAccount.PendingLeaseCount)

	t.Run("only first lease expires at t=61", func(t *testing.T) {
		// At t=61, only lease 1 should expire (created at t=0, timeout is 60s)
		ctx61 := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(61 * time.Second))

		err := f.App.BillingKeeper.EndBlocker(ctx61)
		require.NoError(t, err)

		// Lease 1 should be EXPIRED
		lease1, err := f.App.BillingKeeper.GetLease(ctx61, leaseUUIDs[0])
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_EXPIRED, lease1.State)

		// Lease 2 and 3 should still be PENDING
		lease2, err := f.App.BillingKeeper.GetLease(ctx61, leaseUUIDs[1])
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_PENDING, lease2.State)

		lease3, err := f.App.BillingKeeper.GetLease(ctx61, leaseUUIDs[2])
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_PENDING, lease3.State)

		// Pending count should be 2
		creditAccount, err := f.App.BillingKeeper.GetCreditAccount(ctx61, tenant.String())
		require.NoError(t, err)
		require.Equal(t, uint64(2), creditAccount.PendingLeaseCount)
	})

	t.Run("all leases expire at t=101", func(t *testing.T) {
		// At t=101, all leases should be expired
		// Lease 1: created at t=0, expired at t=60
		// Lease 2: created at t=20, expires at t=80
		// Lease 3: created at t=40, expires at t=100
		ctx101 := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(101 * time.Second))

		err := f.App.BillingKeeper.EndBlocker(ctx101)
		require.NoError(t, err)

		// All leases should be EXPIRED
		for _, leaseUUID := range leaseUUIDs {
			lease, err := f.App.BillingKeeper.GetLease(ctx101, leaseUUID)
			require.NoError(t, err)
			require.Equal(t, types.LEASE_STATE_EXPIRED, lease.State, "lease %s should be expired", leaseUUID)
		}

		// Pending count should be 0
		creditAccount, err := f.App.BillingKeeper.GetCreditAccount(ctx101, tenant.String())
		require.NoError(t, err)
		require.Equal(t, uint64(0), creditAccount.PendingLeaseCount)
	})
}

// TestEndBlockerCreditRefund tests that expired leases properly manage credit accounts.
// Note: PENDING leases do NOT lock credit - credit is only consumed when lease is ACTIVE.
func TestEndBlockerCreditRefund(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Set a short pending timeout for testing (60 seconds)
	params := types.DefaultParams()
	params.PendingTimeout = 60
	err := f.App.BillingKeeper.SetParams(f.Ctx, params)
	require.NoError(t, err)

	// Create provider and SKU with known price
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 3600 = 1 token per second

	// Fund tenant's credit account with exact amount needed
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	initialBalance := sdk.NewCoin(denom, sdkmath.NewInt(50000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(initialBalance))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Get initial balance
	balanceBefore := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, denom)
	t.Logf("Balance before lease: %s", balanceBefore)

	// Create a lease
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	leaseUUID := createResp.LeaseUuid

	// Get the lease to see the locked price
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseUUID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_PENDING, lease.State)
	t.Logf("Lease locked price per second: %v", lease.Items[0].LockedPrice)

	// Balance should NOT be reduced for PENDING leases - credit is only consumed when ACTIVE
	balanceAfterCreate := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, denom)
	t.Logf("Balance after lease creation (PENDING): %s", balanceAfterCreate)
	require.Equal(t, balanceBefore.Amount, balanceAfterCreate.Amount,
		"balance should NOT decrease for PENDING lease - credit not locked until acknowledged")

	// Verify pending lease count increased
	creditAccount, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(1), creditAccount.PendingLeaseCount)

	// Now expire the lease
	ctx61 := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(61 * time.Second))
	err = f.App.BillingKeeper.EndBlocker(ctx61)
	require.NoError(t, err)

	// Verify lease is expired
	expiredLease, err := f.App.BillingKeeper.GetLease(ctx61, leaseUUID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_EXPIRED, expiredLease.State)

	// Balance should be unchanged (no credit was consumed)
	balanceAfterExpire := f.App.BankKeeper.GetBalance(ctx61, creditAddr, denom)
	t.Logf("Balance after expiration: %s", balanceAfterExpire)
	require.Equal(t, balanceBefore.Amount, balanceAfterExpire.Amount,
		"balance should be unchanged - PENDING leases don't consume credit")

	// Pending lease count should be decremented
	creditAccount, err = f.App.BillingKeeper.GetCreditAccount(ctx61, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(0), creditAccount.PendingLeaseCount)
}

// TestEndBlockerDoesNotExpireAcknowledgedLeases tests that acknowledged leases are not expired.
func TestEndBlockerDoesNotExpireAcknowledgedLeases(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Set a short pending timeout for testing (60 seconds)
	params := types.DefaultParams()
	params.PendingTimeout = 60
	err := f.App.BillingKeeper.SetParams(f.Ctx, params)
	require.NoError(t, err)

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(10000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create and acknowledge a lease
	leaseUUID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr,
		[]types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}})

	// Verify lease is ACTIVE
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseUUID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)

	// Run EndBlocker well past the timeout
	ctx200 := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(200 * time.Second))
	err = f.App.BillingKeeper.EndBlocker(ctx200)
	require.NoError(t, err)

	// Lease should still be ACTIVE (not expired because it was acknowledged)
	lease, err = f.App.BillingKeeper.GetLease(ctx200, leaseUUID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State,
		"acknowledged lease should not be expired by EndBlocker")
}

// TestEndBlockerExpiredLeaseQueryFilter tests querying expired leases by state.
func TestEndBlockerExpiredLeaseQueryFilter(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Set a short pending timeout for testing
	params := types.DefaultParams()
	params.PendingTimeout = 60
	err := f.App.BillingKeeper.SetParams(f.Ctx, params)
	require.NoError(t, err)

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(10000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create a pending lease
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	leaseUUID := createResp.LeaseUuid

	// Expire the lease
	ctx61 := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(61 * time.Second))
	err = f.App.BillingKeeper.EndBlocker(ctx61)
	require.NoError(t, err)

	// Query lease directly - should be expired
	lease, err := f.App.BillingKeeper.GetLease(ctx61, leaseUUID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_EXPIRED, lease.State)
	require.NotNil(t, lease.ExpiredAt)
	require.False(t, lease.ExpiredAt.IsZero())
}

// TestEndBlockerRateLimiting tests that EndBlocker respects the rate limit
// and processes at most MaxPendingLeaseExpirationsPerBlock leases per block.
// This also verifies the iterator-based implementation works correctly without
// loading all pending leases into memory at once.
func TestEndBlockerRateLimiting(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Set a short pending timeout for testing (60 seconds)
	params := types.DefaultParams()
	params.PendingTimeout = 60
	params.MaxPendingLeasesPerTenant = 200 // Allow many leases for this test
	err := k.SetParams(f.Ctx, params)
	require.NoError(t, err)

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account with enough for many leases
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(1000000000000)) // 1 trillion
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create 150 pending leases directly (more than MaxPendingLeaseExpirationsPerBlock = 100)
	// Using direct SetLease to avoid message validation overhead for bulk creation
	numLeases := 150
	leaseUUIDs := make([]string, numLeases)
	baseTime := f.Ctx.BlockTime()

	for i := 0; i < numLeases; i++ {
		leaseUUID := fmt.Sprintf("rate-limit-test-lease-%03d", i)
		leaseUUIDs[i] = leaseUUID

		lease := types.Lease{
			Uuid:         leaseUUID,
			Tenant:       tenant.String(),
			ProviderUuid: provider.Uuid,
			Items: []types.LeaseItem{
				{
					SkuUuid:     sku.Uuid,
					Quantity:    1,
					LockedPrice: sdk.NewCoin(denom, sdkmath.NewInt(1)),
				},
			},
			State:          types.LEASE_STATE_PENDING,
			CreatedAt:      baseTime, // All created at same time
			AcknowledgedAt: nil,
			ClosedAt:       nil,
			ExpiredAt:      nil,
		}
		err := k.SetLease(f.Ctx, lease)
		require.NoError(t, err)
	}

	// Verify all 150 leases are pending
	pendingLeases, err := k.GetPendingLeases(f.Ctx)
	require.NoError(t, err)
	require.Len(t, pendingLeases, numLeases, "should have %d pending leases", numLeases)

	// Advance time past the pending timeout (60 seconds)
	expiredCtx := f.Ctx.WithBlockTime(baseTime.Add(61 * time.Second))

	t.Run("first EndBlocker call processes exactly rate limit", func(t *testing.T) {
		err := k.EndBlocker(expiredCtx)
		require.NoError(t, err)

		// Count expired vs still pending
		stillPending, err := k.GetPendingLeases(expiredCtx)
		require.NoError(t, err)

		expiredCount := numLeases - len(stillPending)
		require.Equal(t, types.MaxPendingLeaseExpirationsPerBlock, expiredCount,
			"should expire exactly %d leases (rate limit)", types.MaxPendingLeaseExpirationsPerBlock)
		require.Equal(t, 50, len(stillPending), "should have 50 leases still pending")
	})

	t.Run("second EndBlocker call processes remaining leases", func(t *testing.T) {
		err := k.EndBlocker(expiredCtx)
		require.NoError(t, err)

		// All should now be expired
		stillPending, err := k.GetPendingLeases(expiredCtx)
		require.NoError(t, err)
		require.Len(t, stillPending, 0, "all leases should be expired after second EndBlocker")

		// Verify all leases are in EXPIRED state
		for _, leaseUUID := range leaseUUIDs {
			lease, err := k.GetLease(expiredCtx, leaseUUID)
			require.NoError(t, err)
			require.Equal(t, types.LEASE_STATE_EXPIRED, lease.State,
				"lease %s should be expired", leaseUUID)
		}
	})
}

// TestEndBlockerIteratorDoesNotLoadAll verifies that the iterator-based
// EndBlocker processes leases one-by-one and stops at the rate limit,
// even when only some leases are expired.
func TestEndBlockerIteratorDoesNotLoadAll(t *testing.T) {
	f := initFixture(t)

	k := f.App.BillingKeeper
	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Set pending timeout for testing
	params := types.DefaultParams()
	params.PendingTimeout = 60
	params.MaxPendingLeasesPerTenant = 200
	err := k.SetParams(f.Ctx, params)
	require.NoError(t, err)

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant's credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(1000000000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = k.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	baseTime := f.Ctx.BlockTime()

	// Create 50 leases that will be expired (created at baseTime)
	expiredLeaseUUIDs := make([]string, 50)
	for i := 0; i < 50; i++ {
		leaseUUID := fmt.Sprintf("expired-lease-%03d", i)
		expiredLeaseUUIDs[i] = leaseUUID

		lease := types.Lease{
			Uuid:         leaseUUID,
			Tenant:       tenant.String(),
			ProviderUuid: provider.Uuid,
			Items:        []types.LeaseItem{{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewCoin(denom, sdkmath.NewInt(1))}},
			State:        types.LEASE_STATE_PENDING,
			CreatedAt:    baseTime, // Will be expired
		}
		err := k.SetLease(f.Ctx, lease)
		require.NoError(t, err)
	}

	// Create 50 leases that will NOT be expired (created in the future relative to check time)
	notExpiredLeaseUUIDs := make([]string, 50)
	futureTime := baseTime.Add(30 * time.Second)
	for i := 0; i < 50; i++ {
		leaseUUID := fmt.Sprintf("not-expired-lease-%03d", i)
		notExpiredLeaseUUIDs[i] = leaseUUID

		lease := types.Lease{
			Uuid:         leaseUUID,
			Tenant:       tenant.String(),
			ProviderUuid: provider.Uuid,
			Items:        []types.LeaseItem{{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewCoin(denom, sdkmath.NewInt(1))}},
			State:        types.LEASE_STATE_PENDING,
			CreatedAt:    futureTime, // Created 30s later, won't be expired yet
		}
		err := k.SetLease(f.Ctx, lease)
		require.NoError(t, err)
	}

	// Verify we have 100 pending leases total
	pendingLeases, err := k.GetPendingLeases(f.Ctx)
	require.NoError(t, err)
	require.Len(t, pendingLeases, 100)

	// Advance time to expire only the first batch (61 seconds from baseTime)
	// The second batch was created at baseTime+30s, so timeout is at baseTime+90s
	checkCtx := f.Ctx.WithBlockTime(baseTime.Add(61 * time.Second))

	t.Run("EndBlocker only expires eligible leases", func(t *testing.T) {
		err := k.EndBlocker(checkCtx)
		require.NoError(t, err)

		// Check expired leases
		for _, leaseUUID := range expiredLeaseUUIDs {
			lease, err := k.GetLease(checkCtx, leaseUUID)
			require.NoError(t, err)
			require.Equal(t, types.LEASE_STATE_EXPIRED, lease.State,
				"lease %s should be expired", leaseUUID)
		}

		// Check not-expired leases are still pending
		for _, leaseUUID := range notExpiredLeaseUUIDs {
			lease, err := k.GetLease(checkCtx, leaseUUID)
			require.NoError(t, err)
			require.Equal(t, types.LEASE_STATE_PENDING, lease.State,
				"lease %s should still be pending", leaseUUID)
		}

		// Verify counts
		stillPending, err := k.GetPendingLeases(checkCtx)
		require.NoError(t, err)
		require.Len(t, stillPending, 50, "50 leases should still be pending")
	})
}

// TestEndBlocker_ExpiresByCreatedAtNotUUIDOrder verifies that the range-bounded
// EndBlocker scan selects leases to expire by created_at, not by UUID string
// order. The StateCreatedAt index is keyed ((state, created_at), uuid); the fix
// ranges over created_at, so a lexicographically-small UUID with a large
// created_at must not be expired ahead of an older lease with a larger UUID.
func TestEndBlocker_ExpiresByCreatedAtNotUUIDOrder(t *testing.T) {
	f, providerUUID, skuUUID, denom, baseTime := newExpiryFixture(t, 60)
	k := f.App.BillingKeeper

	// UUID string order deliberately DISAGREES with created_at order:
	// "aaa-young" (created later) sorts before "zzz-old" (created earlier). Both
	// created_at values stay <= the EndBlocker block time (baseTime+61s) — as they
	// always are in production — while the young one stays above the expiry cutoff.
	f.setPendingLease(t, "zzz-old", providerUUID, skuUUID, denom, baseTime)
	f.setPendingLease(t, "aaa-young", providerUUID, skuUUID, denom, baseTime.Add(30*time.Second))

	// At baseTime+61s the old lease (created at baseTime) is past its 60s timeout;
	// the young one (created at +30s, cutoff is +1s) is not.
	ctx := f.Ctx.WithBlockTime(baseTime.Add(61 * time.Second))
	require.NoError(t, k.EndBlocker(ctx))

	oldLease, err := k.GetLease(ctx, "zzz-old")
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_EXPIRED, oldLease.State,
		"older lease must expire regardless of UUID string order")

	youngLease, err := k.GetLease(ctx, "aaa-young")
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_PENDING, youngLease.State,
		"younger lease must remain pending even though its UUID sorts first")
}

// TestEndBlocker_CutoffBoundaryIsStrict verifies that expiry is strict: a lease
// whose created_at equals exactly (blockTime - pendingTimeout) is NOT expired,
// while one nanosecond past the cutoff is. This locks the EndExclusive upper
// bound of the range and the blockTime.After(...) check.
func TestEndBlocker_CutoffBoundaryIsStrict(t *testing.T) {
	f, providerUUID, skuUUID, denom, baseTime := newExpiryFixture(t, 60)
	k := f.App.BillingKeeper

	f.setPendingLease(t, "boundary-lease", providerUUID, skuUUID, denom, baseTime)

	// Exactly at created_at + timeout: not expired (created_at == cutoff).
	atCutoff := f.Ctx.WithBlockTime(baseTime.Add(60 * time.Second))
	require.NoError(t, k.EndBlocker(atCutoff))
	lease, err := k.GetLease(atCutoff, "boundary-lease")
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_PENDING, lease.State,
		"lease exactly at the timeout must not expire")

	// One nanosecond past the cutoff: expired.
	pastCutoff := f.Ctx.WithBlockTime(baseTime.Add(60*time.Second + time.Nanosecond))
	require.NoError(t, k.EndBlocker(pastCutoff))
	lease, err = k.GetLease(pastCutoff, "boundary-lease")
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_EXPIRED, lease.State,
		"lease one nanosecond past the timeout must expire")
}

// TestEndBlocker_YoungBacklogDoesNotBlockExpiry is the motivating scenario: a
// large backlog of not-yet-expired (young) pending leases must neither be scanned
// into the expiration set nor prevent the few expirable (old) leases from being
// expired. UUIDs are arranged so the young leases sort before the old ones, to
// prove the range bounds — not iteration luck — drive the result.
func TestEndBlocker_YoungBacklogDoesNotBlockExpiry(t *testing.T) {
	f, providerUUID, skuUUID, denom, baseTime := newExpiryFixture(t, 60)
	k := f.App.BillingKeeper

	oldUUIDs := make([]string, 5)
	for i := range oldUUIDs {
		oldUUIDs[i] = fmt.Sprintf("zzz-old-%02d", i)
		f.setPendingLease(t, oldUUIDs[i], providerUUID, skuUUID, denom, baseTime)
	}
	// Young leases: created_at stays <= the EndBlocker block time (baseTime+61s, as
	// in production) but above the expiry cutoff (baseTime+1s), so they are not
	// expirable and must be excluded from the scan.
	youngUUIDs := make([]string, 300)
	for i := range youngUUIDs {
		youngUUIDs[i] = fmt.Sprintf("aaa-young-%03d", i)
		f.setPendingLease(t, youngUUIDs[i], providerUUID, skuUUID, denom, baseTime.Add(30*time.Second))
	}

	ctx := f.Ctx.WithBlockTime(baseTime.Add(61 * time.Second))
	require.NoError(t, k.EndBlocker(ctx))

	for _, u := range oldUUIDs {
		lease, err := k.GetLease(ctx, u)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_EXPIRED, lease.State, "old lease %s must expire", u)
	}
	for _, u := range youngUUIDs {
		lease, err := k.GetLease(ctx, u)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_PENDING, lease.State, "young lease %s must remain pending", u)
	}
	stillPending, err := k.GetPendingLeases(ctx)
	require.NoError(t, err)
	require.Len(t, stillPending, 300, "only the 5 old leases should expire; the 300 young leases remain")
}

// TestMsgWithdraw_ProviderWideMode tests the provider-wide withdrawal mode using --provider flag.
func TestMsgWithdraw_ProviderWideMode(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Create provider and SKU with 1 unit per second rate
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 3600 per hour = 1 per second

	// Fund tenant's credit account generously
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(100000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// A second tenant, so the provider has two streams: tenant at 3 umfx/sec
	// (three leases) and tenantB at 2 umfx/sec (one lease).
	tenantB := f.TestAccs[3]
	creditAddrB, err := types.DeriveCreditAddressFromBech32(tenantB.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddrB, sdk.NewCoins(fundAmount))
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenantB.String(),
		CreditAddress: creditAddrB.String(),
	}))

	streamKey := keeper.NewStreamKey(provider.Uuid, tenant, denom)
	streamKeyB := keeper.NewStreamKey(provider.Uuid, tenantB, denom)

	// requireSettledAt checks that a stream was last settled at the given time.
	requireSettledAt := func(t *testing.T, key keeper.StreamKey, settledAt time.Time) {
		t.Helper()
		stream, err := f.App.BillingKeeper.Streams.Get(f.Ctx, key)
		require.NoError(t, err)
		require.Equal(t, settledAt, stream.LastSettledAt)
	}

	t.Run("success: provider-wide withdrawal across tenants", func(t *testing.T) {
		// Create and acknowledge 3 leases for the first tenant
		for range 3 {
			f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
				{SkuUuid: sku.Uuid, Quantity: 1},
			})
		}
		// And one lease of quantity 2 for the second tenant
		f.createAndAcknowledgeLease(t, msgServer, tenantB, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 2},
		})

		// Advance block time by 100 seconds
		newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
		f.Ctx = newCtx

		// Get initial balance
		initialBalance := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom)

		// Withdraw for the whole provider
		resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
			Sender:       providerAddr.String(),
			ProviderUuid: provider.Uuid,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, payoutAddr.String(), resp.PayoutAddress)
		require.Equal(t, uint64(2), resp.WithdrawalCount, "two streams paid out for four leases")
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(500))), resp.TotalAmounts)
		require.False(t, resp.HasMore) // Only 2 streams, default limit is 50
		require.Empty(t, resp.NextKey)

		// Verify the payout came from each tenant's credit according to its rate
		newBalance := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom)
		require.Equal(t, sdkmath.NewInt(500), newBalance.Amount.Sub(initialBalance.Amount))
		require.Equal(t, fundAmount.Amount.SubRaw(300), f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, denom).Amount)
		require.Equal(t, fundAmount.Amount.SubRaw(200), f.App.BankKeeper.GetBalance(f.Ctx, creditAddrB, denom).Amount)

		// Verify each stream was settled (last_settled_at updated)
		requireSettledAt(t, streamKey, f.Ctx.BlockTime())
		requireSettledAt(t, streamKeyB, f.Ctx.BlockTime())
		requireInvariants(t, f)
	})

	t.Run("success: limit counts streams, not leases", func(t *testing.T) {
		// Create 5 more leases for the first tenant: its stream now aggregates 8 leases
		for range 5 {
			f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
				{SkuUuid: sku.Uuid, Quantity: 1},
			})
		}

		// Advance time so there's something to withdraw
		settledBefore := f.Ctx.BlockTime()
		newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
		f.Ctx = newCtx

		// 100 seconds at 8 umfx/sec and at 2 umfx/sec
		owed := map[string]sdkmath.Int{
			tenant.String():  sdkmath.NewInt(800),
			tenantB.String(): sdkmath.NewInt(200),
		}
		cursorCodec := collections.PairKeyCodec(sdk.AccAddressKey, collections.StringKey)

		// Withdraw with limit=1: one stream is paid, whatever its lease count
		first, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
			Sender:       providerAddr.String(),
			ProviderUuid: provider.Uuid,
			Limit:        1,
		})
		require.NoError(t, err)
		require.NotNil(t, first)
		require.Equal(t, uint64(1), first.WithdrawalCount)
		require.True(t, first.HasMore) // The other stream remains
		require.NotEmpty(t, first.NextKey)

		// next_key names the stream that was just paid
		_, cursor, err := cursorCodec.Decode(first.NextKey)
		require.NoError(t, err)
		require.Equal(t, denom, cursor.K2())
		firstTenant := cursor.K1()
		require.Contains(t, owed, firstTenant.String())
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(denom, owed[firstTenant.String()])), first.TotalAmounts)

		secondTenant := tenant
		if firstTenant.Equals(tenant) {
			secondTenant = tenantB
		}
		requireSettledAt(t, keeper.NewStreamKey(provider.Uuid, firstTenant, denom), f.Ctx.BlockTime())
		requireSettledAt(t, keeper.NewStreamKey(provider.Uuid, secondTenant, denom), settledBefore)

		// Resuming from next_key pays the other stream; has_more is false when all streams are processed
		second, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
			Sender:       providerAddr.String(),
			ProviderUuid: provider.Uuid,
			Limit:        1,
			Key:          first.NextKey,
		})
		require.NoError(t, err)
		require.Equal(t, uint64(1), second.WithdrawalCount)
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(denom, owed[secondTenant.String()])), second.TotalAmounts)
		require.False(t, second.HasMore)
		require.Empty(t, second.NextKey)
		requireSettledAt(t, keeper.NewStreamKey(provider.Uuid, secondTenant, denom), f.Ctx.BlockTime())

		// Withdraw with high limit (more than total streams): nothing is left in this block
		resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
			Sender:       providerAddr.String(),
			ProviderUuid: provider.Uuid,
			Limit:        100,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.False(t, resp.HasMore) // All streams processed
		require.True(t, resp.TotalAmounts.IsZero())
		require.Zero(t, resp.WithdrawalCount)
		requireInvariants(t, f)
	})

	t.Run("success: skips pending leases", func(t *testing.T) {
		// Create a new provider for isolation
		provider2Addr := f.TestAccs[3]
		provider2 := f.createTestProvider(t, provider2Addr.String(), provider2Addr.String())
		sku2 := f.createTestSKU(t, provider2.Uuid, 3600)

		// Create a lease but don't acknowledge it (stays PENDING)
		createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku2.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)

		// Verify it's pending
		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, createResp.LeaseUuid)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_PENDING, lease.State)

		// Advance time
		newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
		f.Ctx = newCtx

		// Provider-wide withdraw should return 0 (a pending lease has no stream)
		resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
			Sender:       provider2Addr.String(),
			ProviderUuid: provider2.Uuid,
		})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.WithdrawalCount)
		require.True(t, resp.TotalAmounts.IsZero())
		require.False(t, resp.HasMore)
		require.True(t, f.App.BankKeeper.GetBalance(f.Ctx, provider2Addr, denom).IsZero())
	})

	t.Run("success: authority can withdraw on behalf of provider", func(t *testing.T) {
		// Create a new provider for isolation, paid out to a fresh address
		provider4Addr := sdk.AccAddress("provider4_address___")
		payout4Addr := sdk.AccAddress("provider4_payout____")
		provider4 := f.createTestProvider(t, provider4Addr.String(), payout4Addr.String())
		sku4 := f.createTestSKU(t, provider4.Uuid, 3600)

		// Create and acknowledge a lease
		f.createAndAcknowledgeLease(t, msgServer, tenant, provider4Addr, []types.LeaseItemInput{
			{SkuUuid: sku4.Uuid, Quantity: 1},
		})

		// Advance time
		newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
		f.Ctx = newCtx

		// Authority withdraws on behalf of provider
		authority := f.App.BillingKeeper.GetAuthority()
		authorityAddr, err := sdk.AccAddressFromBech32(authority)
		require.NoError(t, err)
		resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
			Sender:       authority,
			ProviderUuid: provider4.Uuid,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, uint64(1), resp.WithdrawalCount)
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(100))), resp.TotalAmounts)
		require.Equal(t, payout4Addr.String(), resp.PayoutAddress)

		// The funds go to the provider's payout address, never to the sender
		require.Equal(t, sdkmath.NewInt(100), f.App.BankKeeper.GetBalance(f.Ctx, payout4Addr, denom).Amount)
		require.True(t, f.App.BankKeeper.GetBalance(f.Ctx, authorityAddr, denom).IsZero())
		require.True(t, f.App.BankKeeper.GetBalance(f.Ctx, provider4Addr, denom).IsZero())
	})

	t.Run("success: credit exhaustion freezes the stream and the EndBlocker closes its leases", func(t *testing.T) {
		// Create a new tenant with limited credit for this test
		limitedTenant := f.TestAccs[4]
		limitedCreditAddr, err := types.DeriveCreditAddressFromBech32(limitedTenant.String())
		require.NoError(t, err)

		// Fund with 5000 units - enough to create lease (min 3600 for 1hr min duration)
		// but will be exhausted after 5000 seconds
		limitedFundAmount := sdk.NewCoin(denom, sdkmath.NewInt(5000))
		f.fundAccount(t, limitedCreditAddr, sdk.NewCoins(limitedFundAmount))

		err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
			Tenant:        limitedTenant.String(),
			CreditAddress: limitedCreditAddr.String(),
		})
		require.NoError(t, err)

		// Create and acknowledge a lease (1 unit per second)
		leaseUUID := f.createAndAcknowledgeLease(t, msgServer, limitedTenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		})

		// Verify lease is active and credit account has correct counts
		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseUUID)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)

		creditAcct, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, limitedTenant.String())
		require.NoError(t, err)
		require.Equal(t, uint64(1), creditAcct.ActiveLeaseCount)

		// Settle the provider's other streams first, so this withdrawal's amounts are exact
		_, err = msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
			Sender:       providerAddr.String(),
			ProviderUuid: provider.Uuid,
		})
		require.NoError(t, err)

		// Advance time beyond credit capacity (10000 seconds > 5000 available)
		newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(10000 * time.Second))
		f.Ctx = newCtx
		f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())

		// Provider-wide withdrawal pays what is left of the limited tenant's credit
		// (5000, capped) and the full accrual of the two funded tenants
		// (8 umfx/sec and 2 umfx/sec for 10000 seconds).
		resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
			Sender:       providerAddr.String(),
			ProviderUuid: provider.Uuid,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(5000+80000+20000))), resp.TotalAmounts)
		require.Equal(t, uint64(3), resp.WithdrawalCount)
		require.True(t, f.App.BankKeeper.GetBalance(f.Ctx, limitedCreditAddr, denom).IsZero())

		// Only the limited tenant's stream is frozen
		limitedKey := keeper.NewStreamKey(provider.Uuid, limitedTenant, denom)
		limitedStream, err := f.App.BillingKeeper.Streams.Get(f.Ctx, limitedKey)
		require.NoError(t, err)
		require.True(t, limitedStream.IsExhausted())
		for _, key := range []keeper.StreamKey{streamKey, streamKeyB} {
			stream, err := f.App.BillingKeeper.Streams.Get(f.Ctx, key)
			require.NoError(t, err)
			require.False(t, stream.IsExhausted())
		}
		require.Equal(t, 1, countEvents(f.Ctx, types.EventTypeStreamExhausted))
		batch := findEvent(t, f.Ctx, types.EventTypeBatchWithdraw)
		require.Equal(t, "3", attrValue(t, batch, types.AttributeKeyStreamCount))
		require.Equal(t, "1", attrValue(t, batch, types.AttributeKeyExhaustedCount))

		// The withdrawal itself closes nothing
		lease, err = f.App.BillingKeeper.GetLease(f.Ctx, leaseUUID)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State, "no lease is closed inside the withdraw tx")
		require.Zero(t, countEvents(f.Ctx, types.EventTypeLeaseAutoClose))
		requireInvariants(t, f)

		// The EndBlocker closes the lease of the frozen stream, and only that one
		f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())
		require.NoError(t, f.App.BillingKeeper.EndBlocker(f.Ctx))
		require.Equal(t, 1, countEvents(f.Ctx, types.EventTypeLeaseAutoClose))
		autoClosed := findEvent(t, f.Ctx, types.EventTypeLeaseAutoClose)
		require.Equal(t, leaseUUID, attrValue(t, autoClosed, types.AttributeKeyLeaseUUID))
		require.Equal(t, limitedTenant.String(), attrValue(t, autoClosed, types.AttributeKeyTenant))
		require.Equal(t, provider.Uuid, attrValue(t, autoClosed, types.AttributeKeyProviderUUID))
		require.Equal(t, "credit_exhausted", attrValue(t, autoClosed, types.AttributeKeyReason))

		lease, err = f.App.BillingKeeper.GetLease(f.Ctx, leaseUUID)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_CLOSED, lease.State, "lease should be auto-closed due to credit exhaustion")
		require.NotNil(t, lease.ClosedAt, "closed_at should be set")
		require.Equal(t, f.Ctx.BlockTime(), *lease.ClosedAt)
		require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason, "closure reason should indicate credit exhaustion")

		_, err = f.App.BillingKeeper.Streams.Get(f.Ctx, limitedKey)
		require.ErrorIs(t, err, collections.ErrNotFound)

		// Verify credit account's active lease count was decremented and its reservation released
		creditAcct, err = f.App.BillingKeeper.GetCreditAccount(f.Ctx, limitedTenant.String())
		require.NoError(t, err)
		require.Equal(t, uint64(0), creditAcct.ActiveLeaseCount, "active lease count should be decremented")
		require.True(t, creditAcct.ReservedAmounts.IsZero())

		// The funded tenants' leases are untouched
		for _, key := range []keeper.StreamKey{streamKey, streamKeyB} {
			stream, err := f.App.BillingKeeper.Streams.Get(f.Ctx, key)
			require.NoError(t, err)
			require.False(t, stream.IsExhausted())
		}
		creditAcct, err = f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
		require.NoError(t, err)
		require.Equal(t, uint64(9), creditAcct.ActiveLeaseCount, "8 leases with this provider and 1 with provider4")
		requireInvariants(t, f)
	})
}

// TestMsgWithdraw_ProviderWideLargePagination tests provider-wide withdrawal with more streams
// than the default limit, verifying has_more flag and limit behavior.
// Pagination is per stream, so the test needs one tenant per stream.
// A call without a key iterates from the beginning and processes up to `limit` streams;
// a call with the previous page's next_key resumes after it.
func TestMsgWithdraw_ProviderWideLargePagination(t *testing.T) {
	s := newStreamFixture(t, testDenom)

	// Create 75 streams (more than default limit of 50): one tenant each, 1 umfx/sec
	const totalStreams = 75
	tenants := apptesting.CreateRandomAccounts(totalStreams)
	for _, tenant := range tenants {
		s.fundTenant(t, tenant, testDenom, 1_000_000)
		s.lease(t, tenant)
	}

	// requireSettled checks how many of the 75 streams are settled up to the current block time.
	requireSettled := func(t *testing.T, want int) {
		t.Helper()
		settled := 0
		for _, tenant := range tenants {
			state, exists := s.stream(t, tenant, testDenom)
			require.True(t, exists)
			if state.LastSettledAt.Equal(s.Ctx.BlockTime()) {
				settled++
			}
		}
		require.Equal(t, want, settled)
	}

	// Advance time so there's something to withdraw
	s.advance(100 * time.Second)

	t.Run("default limit processes 50 streams with has_more=true, next_key reaches the rest", func(t *testing.T) {
		// Limit: 0 means use default (50)
		first := s.withdraw(t, 0, nil)
		require.Equal(t, types.DefaultProviderWithdrawLimit, first.WithdrawalCount, "should process default limit of 50")
		require.True(t, first.HasMore, "should indicate more streams exist beyond limit")
		require.NotEmpty(t, first.NextKey)
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(5000))), first.TotalAmounts)

		// Verify funds were transferred
		require.Equal(t, sdkmath.NewInt(5000), s.payout(testDenom))
		requireSettled(t, 50)

		// The second page covers the remaining 25 streams
		second := s.withdraw(t, 0, first.NextKey)
		require.Equal(t, uint64(25), second.WithdrawalCount)
		require.False(t, second.HasMore)
		require.Empty(t, second.NextKey)
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(2500))), second.TotalAmounts)
		require.Equal(t, sdkmath.NewInt(7500), s.payout(testDenom))
		requireSettled(t, totalStreams)
	})

	t.Run("explicit limit=100 processes all 75 streams", func(t *testing.T) {
		// Advance time so there's more to withdraw
		s.advance(100 * time.Second)

		// Explicit max limit
		resp := s.withdraw(t, types.MaxBatchLeaseSize, nil)
		require.Equal(t, uint64(totalStreams), resp.WithdrawalCount, "should process all 75 streams")
		require.False(t, resp.HasMore, "no more streams when all processed")
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(7500))), resp.TotalAmounts)
		require.Equal(t, sdkmath.NewInt(15000), s.payout(testDenom))
		requireSettled(t, totalStreams)
	})

	t.Run("small limit processes only requested amount", func(t *testing.T) {
		// Advance time
		s.advance(100 * time.Second)

		// Small limit
		resp := s.withdraw(t, 10, nil)
		require.Equal(t, uint64(10), resp.WithdrawalCount, "should process exactly 10 streams")
		require.True(t, resp.HasMore, "should indicate more streams exist")
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1000))), resp.TotalAmounts)
		requireSettled(t, 10)

		// Without a key the same page is visited again: its streams are settled
		// already, so nothing is paid twice.
		again := s.withdraw(t, 10, nil)
		require.Zero(t, again.WithdrawalCount)
		require.True(t, again.TotalAmounts.IsZero())
		require.True(t, again.HasMore)
		require.Equal(t, resp.NextKey, again.NextKey)
		requireSettled(t, 10)
	})

	t.Run("funds accumulate correctly across withdrawals", func(t *testing.T) {
		// 75 streams × 200 seconds, plus 10 streams × 100 seconds
		require.Equal(t, sdkmath.NewInt(16000), s.payout(testDenom))
		for _, tenant := range tenants {
			paid := sdkmath.NewInt(1_000_000).Sub(s.credit(tenant, testDenom))
			require.True(t, paid.Equal(sdkmath.NewInt(200)) || paid.Equal(sdkmath.NewInt(300)),
				"tenant %s paid %s", tenant, paid)
		}
		s.requireInvariants(t)
	})
}

// TestMsgWithdraw_ProviderWideMultiDenom tests provider-wide withdrawal from
// multiple leases using different denoms, verifying correct aggregation.
func TestMsgWithdraw_ProviderWideMultiDenom(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]

	// Create provider with SKUs in different denoms
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku1 := f.createTestSKUWithDenom(t, provider.Uuid, 3600, testDenom)  // 1 per second in testDenom
	sku2 := f.createTestSKUWithDenom(t, provider.Uuid, 7200, testDenom2) // 2 per second in testDenom2

	// Fund tenant's credit account with BOTH denoms
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount1 := sdk.NewCoin(testDenom, sdkmath.NewInt(100000000))  // 100M testDenom
	fundAmount2 := sdk.NewCoin(testDenom2, sdkmath.NewInt(200000000)) // 200M testDenom2
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount1, fundAmount2))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	t.Run("success: provider-wide withdraw aggregates multiple denoms", func(t *testing.T) {
		// Create lease 1 with testDenom SKU
		lease1 := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku1.Uuid, Quantity: 1},
		})

		// Create lease 2 with testDenom2 SKU
		lease2 := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku2.Uuid, Quantity: 1},
		})

		// Create lease 3 with both denoms
		lease3 := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku1.Uuid, Quantity: 1},
			{SkuUuid: sku2.Uuid, Quantity: 1},
		})

		// Advance time by 100 seconds
		// testDenom: lease1 (1*100) + lease3 (1*100) = 200
		// testDenom2: lease2 (2*100) + lease3 (2*100) = 400
		f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))

		// Get initial balances
		initialBalance1 := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom)
		initialBalance2 := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom2)

		// Provider-wide withdraw
		resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
			Sender:       providerAddr.String(),
			ProviderUuid: provider.Uuid,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, uint64(2), resp.WithdrawalCount, "the 3 leases aggregate into one stream per denom")

		// Verify both denoms were received
		newBalance1 := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom)
		newBalance2 := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom2)

		withdrawnDenom1 := newBalance1.Amount.Sub(initialBalance1.Amount)
		withdrawnDenom2 := newBalance2.Amount.Sub(initialBalance2.Amount)

		require.Equal(t, sdkmath.NewInt(200), withdrawnDenom1,
			"should receive 200 testDenom (1/sec * 100sec * 2 leases)")
		require.Equal(t, sdkmath.NewInt(400), withdrawnDenom2,
			"should receive 400 testDenom2 (2/sec * 100sec * 2 leases)")

		// Verify response includes both denoms
		require.Equal(t, sdkmath.NewInt(200), resp.TotalAmounts.AmountOf(testDenom))
		require.Equal(t, sdkmath.NewInt(400), resp.TotalAmounts.AmountOf(testDenom2))

		// Verify both streams were settled: each aggregates the two leases priced in its denom
		for streamDenom, rate := range map[string]int64{testDenom: 2, testDenom2: 4} {
			stream, err := f.App.BillingKeeper.Streams.Get(f.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, streamDenom))
			require.NoError(t, err)
			require.Equal(t, sdkmath.NewInt(rate), stream.Rate, streamDenom)
			require.Equal(t, uint64(2), stream.LeaseCount, streamDenom)
			require.Equal(t, f.Ctx.BlockTime(), stream.LastSettledAt, streamDenom)
		}

		// The leases themselves are untouched by the withdrawal
		for _, leaseUUID := range []string{lease1, lease2, lease3} {
			lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseUUID)
			require.NoError(t, err)
			require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)
			require.Nil(t, lease.LastSettledAt) //nolint:staticcheck // asserting the deprecated field stays unset
		}
		requireInvariants(t, f)
	})

	t.Run("empty result when no withdrawable amounts", func(t *testing.T) {
		// Create a new provider for isolation
		provider2Addr := f.TestAccs[3]
		provider2 := f.createTestProvider(t, provider2Addr.String(), provider2Addr.String())
		sku3 := f.createTestSKU(t, provider2.Uuid, 3600)

		// Create and acknowledge a lease
		f.createAndAcknowledgeLease(t, msgServer, tenant, provider2Addr, []types.LeaseItemInput{
			{SkuUuid: sku3.Uuid, Quantity: 1},
		})

		// Withdraw immediately without advancing time - nothing to withdraw
		resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
			Sender:       provider2Addr.String(),
			ProviderUuid: provider2.Uuid,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, uint64(0), resp.WithdrawalCount, "should process 0 leases with withdrawable amounts")
		require.True(t, resp.TotalAmounts.IsZero(), "total amounts should be zero")
	})
}

// ============================================================================
// Credit Reservation / Overbooking Prevention Tests
// ============================================================================

// TestOverbookingPrevention tests that the credit reservation system prevents
// creating more leases than available credit can support.
func TestOverbookingPrevention(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	// SKU: 3600 per hour = 1 per second
	// With min_lease_duration of 3600 seconds (default), each lease reserves 3600 credits
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 3600 umfx per hour

	// Fund tenant with 10000 credits
	// With 3600 credits reserved per lease, tenant should only be able to create 2 leases
	// (2 * 3600 = 7200 < 10000, but 3 * 3600 = 10800 > 10000)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(10000))
	f.fundAccount(t, tenant, sdk.NewCoins(fundAmount))

	// Fund credit account
	_, err := msgServer.FundCredit(f.Ctx, &types.MsgFundCredit{
		Sender: tenant.String(),
		Tenant: tenant.String(),
		Amount: fundAmount,
	})
	require.NoError(t, err)

	// Lease 1: Should succeed (reserves 3600, available: 10000 - 3600 = 6400)
	createResp1, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	require.NotNil(t, createResp1)
	t.Logf("Lease 1 created: %s", createResp1.LeaseUuid)

	// Verify reservation was made
	creditAccount, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(3600), creditAccount.ReservedAmounts.AmountOf(denom))

	// Lease 2: Should succeed (reserves 3600, available: 10000 - 7200 = 2800)
	createResp2, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	require.NotNil(t, createResp2)
	t.Logf("Lease 2 created: %s", createResp2.LeaseUuid)

	// Verify reservation increased
	creditAccount, err = f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(7200), creditAccount.ReservedAmounts.AmountOf(denom))

	// Lease 3: Should FAIL (need 3600, only 2800 available)
	_, err = msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient available credit")
	t.Logf("Lease 3 correctly rejected: %v", err)

	// Verify reservation unchanged after failed attempt
	creditAccount, err = f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(7200), creditAccount.ReservedAmounts.AmountOf(denom))
}

// TestReservationReleaseOnClose tests that closing an active lease releases its reservation.
func TestReservationReleaseOnClose(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 3600 umfx per hour = 1 per second

	// Fund tenant with just enough for 2 leases
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(8000))
	f.fundAccount(t, tenant, sdk.NewCoins(fundAmount))

	_, err := msgServer.FundCredit(f.Ctx, &types.MsgFundCredit{
		Sender: tenant.String(),
		Tenant: tenant.String(),
		Amount: fundAmount,
	})
	require.NoError(t, err)

	// Create and acknowledge 2 leases
	leaseUUID1 := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr,
		[]types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}})
	leaseUUID2 := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr,
		[]types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}})

	// Verify both reservations exist
	creditAccount, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(7200), creditAccount.ReservedAmounts.AmountOf(denom))

	// Third lease should fail
	_, err = msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient available credit")

	// Close lease 1
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseUUID1},
	})
	require.NoError(t, err)

	// Verify reservation was released
	creditAccount, err = f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(3600), creditAccount.ReservedAmounts.AmountOf(denom))
	requireInvariants(t, f)

	// Now third lease should succeed (after closing first)
	createResp3, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	require.NotNil(t, createResp3)
	t.Logf("Lease 3 created after closing lease 1: %s", createResp3.LeaseUuid)

	// Cleanup: verify we can still close all leases
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseUUID2},
	})
	require.NoError(t, err)
	requireInvariants(t, f)
}

// TestReservationReleaseOnReject tests that rejecting a pending lease releases its reservation.
func TestReservationReleaseOnReject(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(8000))
	f.fundAccount(t, tenant, sdk.NewCoins(fundAmount))

	_, err := msgServer.FundCredit(f.Ctx, &types.MsgFundCredit{
		Sender: tenant.String(),
		Tenant: tenant.String(),
		Amount: fundAmount,
	})
	require.NoError(t, err)

	// Create 2 pending leases (don't acknowledge)
	createResp1, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)

	createResp2, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)

	// Verify reservations
	creditAccount, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(7200), creditAccount.ReservedAmounts.AmountOf(denom))

	// Third lease should fail
	_, err = msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.Error(t, err)

	// Provider rejects lease 1
	_, err = msgServer.RejectLease(f.Ctx, &types.MsgRejectLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{createResp1.LeaseUuid},
		Reason:     "Test rejection",
	})
	require.NoError(t, err)

	// Verify reservation was released
	creditAccount, err = f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(3600), creditAccount.ReservedAmounts.AmountOf(denom))

	// Now third lease should succeed
	createResp3, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	require.NotNil(t, createResp3)
	t.Logf("Lease 3 created after rejecting lease 1: %s", createResp3.LeaseUuid)

	// Cleanup
	_, err = msgServer.RejectLease(f.Ctx, &types.MsgRejectLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{createResp2.LeaseUuid, createResp3.LeaseUuid},
		Reason:     "Cleanup",
	})
	require.NoError(t, err)
}

// TestReservationReleaseOnCancel tests that cancelling a pending lease releases its reservation.
func TestReservationReleaseOnCancel(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(8000))
	f.fundAccount(t, tenant, sdk.NewCoins(fundAmount))

	_, err := msgServer.FundCredit(f.Ctx, &types.MsgFundCredit{
		Sender: tenant.String(),
		Tenant: tenant.String(),
		Amount: fundAmount,
	})
	require.NoError(t, err)

	// Create 2 pending leases
	createResp1, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)

	createResp2, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)

	// Verify reservations
	creditAccount, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(7200), creditAccount.ReservedAmounts.AmountOf(denom))

	// Tenant cancels lease 1
	_, err = msgServer.CancelLease(f.Ctx, &types.MsgCancelLease{
		Tenant:     tenant.String(),
		LeaseUuids: []string{createResp1.LeaseUuid},
	})
	require.NoError(t, err)

	// Verify reservation was released
	creditAccount, err = f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(3600), creditAccount.ReservedAmounts.AmountOf(denom))

	// Now third lease should succeed
	createResp3, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	require.NotNil(t, createResp3)
	t.Logf("Lease 3 created after cancelling lease 1: %s", createResp3.LeaseUuid)

	// Cleanup
	_, err = msgServer.CancelLease(f.Ctx, &types.MsgCancelLease{
		Tenant:     tenant.String(),
		LeaseUuids: []string{createResp2.LeaseUuid, createResp3.LeaseUuid},
	})
	require.NoError(t, err)
}

// TestReservationReleaseOnExpire tests that expiring a pending lease releases its reservation.
func TestReservationReleaseOnExpire(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	denom := testDenom

	// Set a short pending timeout for testing
	params := types.DefaultParams()
	params.PendingTimeout = 60 // 60 seconds
	err := f.App.BillingKeeper.SetParams(f.Ctx, params)
	require.NoError(t, err)

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(8000))
	f.fundAccount(t, tenant, sdk.NewCoins(fundAmount))

	_, err = msgServer.FundCredit(f.Ctx, &types.MsgFundCredit{
		Sender: tenant.String(),
		Tenant: tenant.String(),
		Amount: fundAmount,
	})
	require.NoError(t, err)

	// Create 2 pending leases
	createResp1, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)

	createResp2, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)

	// Verify reservations
	creditAccount, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(7200), creditAccount.ReservedAmounts.AmountOf(denom))

	// Third lease should fail
	_, err = msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.Error(t, err)

	// Advance time past pending timeout
	ctx61 := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(61 * time.Second))

	// Run EndBlocker to expire pending leases
	err = f.App.BillingKeeper.EndBlocker(ctx61)
	require.NoError(t, err)

	// Verify both leases are now EXPIRED
	lease1, err := f.App.BillingKeeper.GetLease(ctx61, createResp1.LeaseUuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_EXPIRED, lease1.State)

	lease2, err := f.App.BillingKeeper.GetLease(ctx61, createResp2.LeaseUuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_EXPIRED, lease2.State)

	// Verify reservations were released
	creditAccount, err = f.App.BillingKeeper.GetCreditAccount(ctx61, tenant.String())
	require.NoError(t, err)
	require.True(t, creditAccount.ReservedAmounts.IsZero() || creditAccount.ReservedAmounts.AmountOf(denom).IsZero(),
		"reservations should be released after expiry, got: %s", creditAccount.ReservedAmounts)

	// Now new leases should succeed
	createResp3, err := msgServer.CreateLease(ctx61, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	require.NotNil(t, createResp3)
	t.Logf("Lease 3 created after lease 1 and 2 expired: %s", createResp3.LeaseUuid)
}

// TestQueryAvailableBalances tests that the CreditAccount query returns correct available_balances.
func TestQueryAvailableBalances(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)
	querier := keeper.NewQuerier(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant with 10000 credits
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(10000))
	f.fundAccount(t, tenant, sdk.NewCoins(fundAmount))

	_, err := msgServer.FundCredit(f.Ctx, &types.MsgFundCredit{
		Sender: tenant.String(),
		Tenant: tenant.String(),
		Amount: fundAmount,
	})
	require.NoError(t, err)

	// Query initial state - all balances should be available
	resp, err := querier.CreditAccount(f.Ctx, &types.QueryCreditAccountRequest{
		Tenant: tenant.String(),
	})
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(10000), resp.Balances.AmountOf(denom))
	require.Equal(t, sdkmath.NewInt(10000), resp.AvailableBalances.AmountOf(denom))
	require.True(t, resp.CreditAccount.ReservedAmounts.IsZero())

	// Create a lease (reserves 3600)
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)

	// Query after lease creation
	resp, err = querier.CreditAccount(f.Ctx, &types.QueryCreditAccountRequest{
		Tenant: tenant.String(),
	})
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(10000), resp.Balances.AmountOf(denom), "total balance should be unchanged")
	require.Equal(t, sdkmath.NewInt(6400), resp.AvailableBalances.AmountOf(denom), "available should be balance - reserved")
	require.Equal(t, sdkmath.NewInt(3600), resp.CreditAccount.ReservedAmounts.AmountOf(denom), "reserved should show lease reservation")

	// Create another lease
	_, err = msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)

	// Query after second lease
	resp, err = querier.CreditAccount(f.Ctx, &types.QueryCreditAccountRequest{
		Tenant: tenant.String(),
	})
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(10000), resp.Balances.AmountOf(denom))
	require.Equal(t, sdkmath.NewInt(2800), resp.AvailableBalances.AmountOf(denom))
	require.Equal(t, sdkmath.NewInt(7200), resp.CreditAccount.ReservedAmounts.AmountOf(denom))

	// Cancel first lease
	_, err = msgServer.CancelLease(f.Ctx, &types.MsgCancelLease{
		Tenant:     tenant.String(),
		LeaseUuids: []string{createResp.LeaseUuid},
	})
	require.NoError(t, err)

	// Query after cancellation
	resp, err = querier.CreditAccount(f.Ctx, &types.QueryCreditAccountRequest{
		Tenant: tenant.String(),
	})
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(10000), resp.Balances.AmountOf(denom))
	require.Equal(t, sdkmath.NewInt(6400), resp.AvailableBalances.AmountOf(denom))
	require.Equal(t, sdkmath.NewInt(3600), resp.CreditAccount.ReservedAmounts.AmountOf(denom))
}

// TestMsgCreateLease_ServiceName tests lease creation with service_name fields.
func TestMsgCreateLease_ServiceName(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 3600 per hour = 1 per second

	// Fund tenant's credit account generously
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	fundAmount := sdk.NewCoin(denom, sdkmath.NewInt(100000000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(fundAmount))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	t.Run("success: same SKU with different service_names", func(t *testing.T) {
		resp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items: []types.LeaseItemInput{
				{SkuUuid: sku.Uuid, Quantity: 1, ServiceName: "web"},
				{SkuUuid: sku.Uuid, Quantity: 1, ServiceName: "db"},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotEmpty(t, resp.LeaseUuid)

		// Verify stored lease has correct service_names
		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, resp.LeaseUuid)
		require.NoError(t, err)
		require.Len(t, lease.Items, 2)
		require.Equal(t, "web", lease.Items[0].ServiceName)
		require.Equal(t, "db", lease.Items[1].ServiceName)
		require.Equal(t, sku.Uuid, lease.Items[0].SkuUuid)
		require.Equal(t, sku.Uuid, lease.Items[1].SkuUuid)
	})

	t.Run("fail: mixed service_names (some set, some not)", func(t *testing.T) {
		_, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items: []types.LeaseItemInput{
				{SkuUuid: sku.Uuid, Quantity: 1, ServiceName: "web"},
				{SkuUuid: sku.Uuid, Quantity: 1},
			},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "all items must have service_name or none")
	})

	t.Run("fail: duplicate service_name", func(t *testing.T) {
		_, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items: []types.LeaseItemInput{
				{SkuUuid: sku.Uuid, Quantity: 1, ServiceName: "web"},
				{SkuUuid: sku.Uuid, Quantity: 1, ServiceName: "web"},
			},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "duplicate service_name")
	})

	t.Run("backward compat: no service_names, duplicate SKU rejected", func(t *testing.T) {
		_, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items: []types.LeaseItemInput{
				{SkuUuid: sku.Uuid, Quantity: 1},
				{SkuUuid: sku.Uuid, Quantity: 2},
			},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "appears multiple times")
	})

	t.Run("backward compat: no service_names, single item passes", func(t *testing.T) {
		resp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items: []types.LeaseItemInput{
				{SkuUuid: sku.Uuid, Quantity: 3},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Verify service_name is empty
		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, resp.LeaseUuid)
		require.NoError(t, err)
		require.Len(t, lease.Items, 1)
		require.Empty(t, lease.Items[0].ServiceName)
	})
}

// TestBatchMultiTenantDeterminism is a regression test for non-deterministic map
// iteration in batch lease operations. When multiple tenants appear in a single
// AcknowledgeLease / RejectLease / CloseLease message, their credit accounts are
// collected in a map[string]CreditAccount. Iterating that map with `range` produces
// a random order per Go spec, which can cause divergent state across validators.
//
// The fix tracks a tenantOrder slice at insertion time. This test runs each
// batch operation 50 times on independent fixtures and asserts that the resulting
// credit-account state and event sequence are identical every time.
func TestBatchMultiTenantDeterminism(t *testing.T) {
	denom := testDenom

	// setupMultiTenant returns two tenants each with a pending lease for the same provider.
	setupMultiTenant := func(t *testing.T) (*testFixture, types.MsgServer, sdk.AccAddress, sdk.AccAddress, sdk.AccAddress, string, string) {
		t.Helper()
		f := initFixture(t)
		msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

		tenant1 := f.TestAccs[0]
		tenant2 := f.TestAccs[1]
		providerAddr := f.TestAccs[2]
		payoutAddr := f.TestAccs[3]

		provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
		sku := f.createTestSKU(t, provider.Uuid, 3600)

		for _, tenant := range []sdk.AccAddress{tenant1, tenant2} {
			creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
			require.NoError(t, err)
			f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(100_000_000))))
			err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
				Tenant:        tenant.String(),
				CreditAddress: creditAddr.String(),
			})
			require.NoError(t, err)
		}

		// Create one pending lease per tenant, both for the same provider.
		resp1, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant1.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)
		resp2, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant2.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 2}},
		})
		require.NoError(t, err)

		return f, msgServer, tenant1, tenant2, providerAddr, resp1.LeaseUuid, resp2.LeaseUuid
	}

	t.Run("AcknowledgeLease multi-tenant determinism", func(t *testing.T) {
		type snapshot struct {
			ca1Pending uint64
			ca1Active  uint64
			ca2Pending uint64
			ca2Active  uint64
			eventTypes []string
		}

		var reference *snapshot
		for i := range 50 {
			f, msgServer, tenant1, tenant2, providerAddr, lease1, lease2 := setupMultiTenant(t)
			f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())

			_, err := msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
				Sender:     providerAddr.String(),
				LeaseUuids: []string{lease1, lease2},
			})
			require.NoError(t, err)

			ca1, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant1.String())
			require.NoError(t, err)
			ca2, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant2.String())
			require.NoError(t, err)

			events := f.Ctx.EventManager().Events()
			eventTypes := make([]string, len(events))
			for j, ev := range events {
				eventTypes[j] = ev.Type
			}

			s := &snapshot{
				ca1Pending: ca1.PendingLeaseCount,
				ca1Active:  ca1.ActiveLeaseCount,
				ca2Pending: ca2.PendingLeaseCount,
				ca2Active:  ca2.ActiveLeaseCount,
				eventTypes: eventTypes,
			}

			if i == 0 {
				reference = s
			} else {
				require.Equal(t, reference, s, "iteration %d produced different results", i)
			}
		}
	})

	t.Run("RejectLease multi-tenant determinism", func(t *testing.T) {
		type snapshot struct {
			ca1Pending  uint64
			ca2Pending  uint64
			ca1Reserved string
			ca2Reserved string
			eventTypes  []string
		}

		var reference *snapshot
		for i := range 50 {
			f, msgServer, tenant1, tenant2, providerAddr, lease1, lease2 := setupMultiTenant(t)
			f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())

			_, err := msgServer.RejectLease(f.Ctx, &types.MsgRejectLease{
				Sender:     providerAddr.String(),
				LeaseUuids: []string{lease1, lease2},
				Reason:     "test rejection",
			})
			require.NoError(t, err)

			ca1, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant1.String())
			require.NoError(t, err)
			ca2, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant2.String())
			require.NoError(t, err)

			events := f.Ctx.EventManager().Events()
			eventTypes := make([]string, len(events))
			for j, ev := range events {
				eventTypes[j] = ev.Type
			}

			s := &snapshot{
				ca1Pending:  ca1.PendingLeaseCount,
				ca2Pending:  ca2.PendingLeaseCount,
				ca1Reserved: ca1.ReservedAmounts.String(),
				ca2Reserved: ca2.ReservedAmounts.String(),
				eventTypes:  eventTypes,
			}

			if i == 0 {
				reference = s
			} else {
				require.Equal(t, reference, s, "iteration %d produced different results", i)
			}
		}
	})

	t.Run("CloseLease multi-tenant determinism", func(t *testing.T) {
		type snapshot struct {
			ca1Active   uint64
			ca2Active   uint64
			ca1Reserved string
			ca2Reserved string
			eventTypes  []string
		}

		var reference *snapshot
		for i := range 50 {
			f, msgServer, tenant1, tenant2, providerAddr, lease1, lease2 := setupMultiTenant(t)

			// Acknowledge both leases first
			_, err := msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
				Sender:     providerAddr.String(),
				LeaseUuids: []string{lease1, lease2},
			})
			require.NoError(t, err)

			// Advance time to accrue billing
			f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(10 * time.Second))
			f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())

			_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
				Sender:     providerAddr.String(),
				LeaseUuids: []string{lease1, lease2},
			})
			require.NoError(t, err)

			ca1, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant1.String())
			require.NoError(t, err)
			ca2, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant2.String())
			require.NoError(t, err)

			events := f.Ctx.EventManager().Events()
			eventTypes := make([]string, len(events))
			for j, ev := range events {
				eventTypes[j] = ev.Type
			}

			s := &snapshot{
				ca1Active:   ca1.ActiveLeaseCount,
				ca2Active:   ca2.ActiveLeaseCount,
				ca1Reserved: ca1.ReservedAmounts.String(),
				ca2Reserved: ca2.ReservedAmounts.String(),
				eventTypes:  eventTypes,
			}

			if i == 0 {
				reference = s
			} else {
				require.Equal(t, reference, s, "iteration %d produced different results", i)
			}
		}
	})
}

// TestMsgWithdraw_ProviderWideCursorDrainsAllStreams verifies that looping
// provider-wide withdrawal with key <- next_key at a small limit settles every
// stream exactly once and terminates (the pre-cursor code re-scans the front
// forever). The cursor is per stream, so each stream belongs to its own tenant;
// every tenant holds two leases, so pages are counted in streams, not leases.
func TestMsgWithdraw_ProviderWideCursorDrainsAllStreams(t *testing.T) {
	s := newStreamFixture(t, testDenom)

	const totalStreams = 25
	tenants := apptesting.CreateRandomAccounts(totalStreams)
	for _, tenant := range tenants {
		s.fundTenant(t, tenant, testDenom, 1_000_000)
		s.lease(t, tenant)
		s.lease(t, tenant)
	}

	// Advance time so every stream has something to settle.
	s.advance(100 * time.Second)

	// Streams are visited in key order: (tenant address, denom) within the provider.
	sorted := slices.Clone(tenants)
	slices.SortFunc(sorted, func(a, b sdk.AccAddress) int { return bytes.Compare(a, b) })
	cursorCodec := collections.PairKeyCodec(sdk.AccAddressKey, collections.StringKey)

	var key []byte
	pages := 0
	totalSettled := uint64(0)
	totalPaid := sdk.NewCoins()
	for {
		resp := s.withdraw(t, 7, key)
		pages++
		totalSettled += resp.WithdrawalCount
		totalPaid = totalPaid.Add(resp.TotalAmounts...)
		require.LessOrEqual(t, resp.WithdrawalCount, uint64(7))
		require.Equal(t,
			sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewIntFromUint64(resp.WithdrawalCount*200))),
			resp.TotalAmounts,
			"each stream pays 2 umfx/sec for 100 seconds")
		require.Less(t, pages, 100, "pagination must terminate")
		if !resp.HasMore {
			require.Empty(t, resp.NextKey, "next_key must be empty on the final page")
			break
		}
		require.NotEmpty(t, resp.NextKey, "next_key must be set while has_more is true")

		// next_key names the last stream of the page.
		_, cursor, err := cursorCodec.Decode(resp.NextKey)
		require.NoError(t, err)
		require.Equal(t, sorted[pages*7-1], cursor.K1())
		require.Equal(t, testDenom, cursor.K2())
		key = resp.NextKey
	}

	// ceil(25 / 7) == 4 pages, every stream settled exactly once (no double-drain), and
	// every stream settled to the current block time.
	require.Equal(t, 4, pages, "should take 4 pages to drain 25 streams at limit 7")
	require.Equal(t, uint64(totalStreams), totalSettled, "each stream settled exactly once across all pages")
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(totalStreams*200))), totalPaid)
	require.Equal(t, sdkmath.NewInt(totalStreams*200), s.payout(testDenom))
	for _, tenant := range tenants {
		state, exists := s.stream(t, tenant, testDenom)
		require.True(t, exists)
		require.Equal(t, s.Ctx.BlockTime(), state.LastSettledAt,
			"stream of %s should be settled to current block time", tenant)
		require.Equal(t, sdkmath.NewInt(1_000_000-200), s.credit(tenant, testDenom),
			"tenant %s must be charged exactly once", tenant)
	}
	s.requireInvariants(t)
}

// TestMsgWithdraw_ProviderWideCursorMidCycleAckDeferredOneCycle verifies the
// documented liveness property: a stream created (first lease acknowledged,
// PENDING->ACTIVE) with a key *behind* an in-flight cursor is not settled in
// the current cycle (the cursor already passed its slot), but is picked up on
// the next full cycle (key=""). It is deferred by one cycle, never skipped
// forever or double-settled.
func TestMsgWithdraw_ProviderWideCursorMidCycleAckDeferredOneCycle(t *testing.T) {
	s := newStreamFixture(t, testDenom)

	// Streams are ordered by tenant address within the provider. The tenant with
	// the smallest address is the "early" one; its lease stays PENDING for now,
	// so it has no stream yet.
	tenants := apptesting.CreateRandomAccounts(5)
	slices.SortFunc(tenants, func(a, b sdk.AccAddress) int { return bytes.Compare(a, b) })
	early, active := tenants[0], tenants[1:]

	for _, tenant := range tenants {
		s.fundTenant(t, tenant, testDenom, 1_000_000)
	}
	earlyCreate, err := s.msgServer.CreateLease(s.Ctx, &types.MsgCreateLease{
		Tenant: early.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: s.sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)

	// Four ACTIVE streams (1 umfx/sec each), all with larger keys than the early tenant's.
	t0 := s.Ctx.BlockTime()
	for _, tenant := range active {
		s.lease(t, tenant)
	}

	requireSettledAt := func(t *testing.T, tenant sdk.AccAddress, settledAt time.Time, msg string) {
		t.Helper()
		state, exists := s.stream(t, tenant, testDenom)
		require.True(t, exists)
		require.Equal(t, settledAt, state.LastSettledAt, msg)
	}

	// T1: advance so the ACTIVE streams have something to settle.
	s.advance(100 * time.Second)
	t1 := s.Ctx.BlockTime()

	// Page 1 (limit 2, no cursor): settles the two smallest ACTIVE streams; the
	// cursor (next_key) lands on the 2nd-smallest ACTIVE stream.
	resp1 := s.withdraw(t, 2, nil)
	require.Equal(t, uint64(2), resp1.WithdrawalCount)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(200))), resp1.TotalAmounts)
	require.True(t, resp1.HasMore)
	require.NotEmpty(t, resp1.NextKey)
	_, cursor, err := collections.PairKeyCodec(sdk.AccAddressKey, collections.StringKey).Decode(resp1.NextKey)
	require.NoError(t, err)
	require.Equal(t, active[1], cursor.K1())

	// Acknowledge the early lease now: its stream is created with
	// LastSettledAt == t1 and a key smaller than the cursor, i.e. behind the
	// in-flight cursor.
	_, err = s.msgServer.AcknowledgeLease(s.Ctx, &types.MsgAcknowledgeLease{
		Sender: s.providerAddr.String(), LeaseUuids: []string{earlyCreate.LeaseUuid},
	})
	require.NoError(t, err)
	requireSettledAt(t, early, t1, "billing starts at acknowledgement")

	// Finish the current cycle from the cursor, 50 seconds later. StartExclusive
	// skips everything <= cursor, so neither the early stream nor the two streams
	// of page 1 (all behind the cursor) are settled by the rest of this cycle.
	s.advance(50 * time.Second)
	t1b := s.Ctx.BlockTime()
	key := resp1.NextKey
	restCount := uint64(0)
	restPaid := sdk.NewCoins()
	for pages := 0; ; pages++ {
		require.Less(t, pages, 100, "pagination must terminate")
		resp := s.withdraw(t, 2, key)
		restCount += resp.WithdrawalCount
		restPaid = restPaid.Add(resp.TotalAmounts...)
		if !resp.HasMore {
			break
		}
		key = resp.NextKey
	}
	require.Equal(t, uint64(2), restCount, "only the two streams ahead of the cursor are settled")
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(300))), restPaid,
		"two streams, 150 seconds since t0 each")
	requireSettledAt(t, early, t1,
		"early stream must NOT be settled in the cycle whose cursor already passed its slot")
	requireSettledAt(t, active[0], t1, "page 1 streams are behind the cursor")
	requireSettledAt(t, active[1], t1, "page 1 streams are behind the cursor")
	requireSettledAt(t, active[2], t1b, "streams ahead of the cursor are settled")
	requireSettledAt(t, active[3], t1b, "streams ahead of the cursor are settled")
	require.Equal(t, sdkmath.NewInt(1_000_000), s.credit(early, testDenom), "the early tenant has not been charged yet")

	// T2: advance again, then run a fresh cycle from the front (key="").
	s.advance(100 * time.Second)
	t2 := s.Ctx.BlockTime()
	resp2 := s.withdraw(t, 100, nil)
	require.False(t, resp2.HasMore)
	require.Equal(t, uint64(5), resp2.WithdrawalCount)
	// early: t1 -> t2 = 150s; active[0], active[1]: t1 -> t2 = 150s each;
	// active[2], active[3]: t1b -> t2 = 100s each.
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(650))), resp2.TotalAmounts)

	// The early stream is settled on the following cycle (deferred, not skipped),
	// and every stream ends settled to the same t2, having paid for exactly the
	// time its lease was active (no gap, no duplication).
	for _, tenant := range tenants {
		requireSettledAt(t, tenant, t2, "stream should be settled to t2 on the following cycle")
	}
	require.Equal(t, sdkmath.NewInt(int64(t2.Sub(t1)/time.Second)), sdkmath.NewInt(1_000_000).Sub(s.credit(early, testDenom)))
	for _, tenant := range active {
		require.Equal(t, sdkmath.NewInt(int64(t2.Sub(t0)/time.Second)), sdkmath.NewInt(1_000_000).Sub(s.credit(tenant, testDenom)))
	}
	require.Equal(t, sdkmath.NewInt(1150), s.payout(testDenom))
	s.requireInvariants(t)
}
