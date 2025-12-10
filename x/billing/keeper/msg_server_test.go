/*
Package keeper_test contains unit tests for the billing module message server.

Test Coverage:
- MsgFundCredit: funding credit accounts, denom validation, balance tracking
- MsgUpdateParams: parameter updates, authority validation
- MsgCreateLease: lease creation with SKU validation, credit checks, max lease limits
- MsgCreateLeaseForTenant: authority-only lease creation on behalf of tenants
- MsgCloseLease: lease closure by tenant, provider, or authority
- MsgWithdraw: provider withdrawal from individual leases
- MsgWithdrawAll: provider batch withdrawal from all leases
*/
package keeper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestMsgFundCredit(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	sender := f.TestAccs[0]
	tenant := f.TestAccs[1]
	denom := types.DefaultDenom

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
			name: "fail: wrong denom",
			msg: &types.MsgFundCredit{
				Sender: sender.String(),
				Tenant: tenant.String(),
				Amount: sdk.NewCoin("wrongdenom", sdkmath.NewInt(1000000)),
			},
			expectErr: true,
			errMsg:    "invalid denomination",
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
	denom := types.DefaultDenom

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
	denom := types.DefaultDenom

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
	denom := types.DefaultDenom

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
					"factory/newdenom/upwr",
					sdkmath.NewInt(10000000),
					50,
					[]string{},
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
			name: "fail: invalid params",
			msg: &types.MsgUpdateParams{
				Authority: authority.String(),
				Params: types.NewParams(
					"", // invalid: empty denom
					sdkmath.NewInt(5000000),
					100,
					[]string{},
				),
			},
			expectErr: true,
			errMsg:    "denom cannot be empty",
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
				require.Equal(t, tc.msg.Params.Denom, params.Denom)
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
	denom := types.DefaultDenom

	// Initialize sequences
	err := f.App.BillingKeeper.NextLeaseID.Set(f.Ctx, 1)
	require.NoError(t, err)
	err = f.App.SKUKeeper.NextProviderID.Set(f.Ctx, 1)
	require.NoError(t, err)
	err = f.App.SKUKeeper.NextSKUID.Set(f.Ctx, 1)
	require.NoError(t, err)

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Id, 3600) // 3600 per hour = 1 per second

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
						SkuId:    sku.Id,
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
						SkuId:    999,
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
				require.Greater(t, resp.LeaseId, uint64(0))

				// Verify lease was created
				lease, err := f.App.BillingKeeper.GetLease(f.Ctx, resp.LeaseId)
				require.NoError(t, err)
				require.Equal(t, tc.msg.Tenant, lease.Tenant)
				require.Equal(t, provider.Id, lease.ProviderId)
				require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)
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

	// Initialize sequences
	err := f.App.BillingKeeper.NextLeaseID.Set(f.Ctx, 1)
	require.NoError(t, err)
	err = f.App.SKUKeeper.NextProviderID.Set(f.Ctx, 1)
	require.NoError(t, err)
	err = f.App.SKUKeeper.NextSKUID.Set(f.Ctx, 1)
	require.NoError(t, err)

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Id, 100)

	// Do NOT fund the tenant - they should have 0 credit
	msg := &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{
				SkuId:    sku.Id,
				Quantity: 1,
			},
		},
	}

	resp, err := msgServer.CreateLease(f.Ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient credit balance")
	require.Nil(t, resp)
}

func TestMsgCreateLeaseMaxLeasesReached(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := types.DefaultDenom

	// Set max leases to 2
	params := types.DefaultParams()
	params.MaxLeasesPerTenant = 2
	err := f.App.BillingKeeper.SetParams(f.Ctx, params)
	require.NoError(t, err)

	// Initialize sequences
	err = f.App.BillingKeeper.NextLeaseID.Set(f.Ctx, 1)
	require.NoError(t, err)
	err = f.App.SKUKeeper.NextProviderID.Set(f.Ctx, 1)
	require.NoError(t, err)
	err = f.App.SKUKeeper.NextSKUID.Set(f.Ctx, 1)
	require.NoError(t, err)

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Id, 100)

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

	// Create max number of leases
	for i := 0; i < 2; i++ {
		msg := &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items: []types.LeaseItemInput{
				{
					SkuId:    sku.Id,
					Quantity: 1,
				},
			},
		}
		_, err := msgServer.CreateLease(f.Ctx, msg)
		require.NoError(t, err)
	}

	// Try to create one more - should fail
	msg := &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{
				SkuId:    sku.Id,
				Quantity: 1,
			},
		},
	}
	resp, err := msgServer.CreateLease(f.Ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "maximum leases per tenant reached")
	require.Nil(t, resp)
}

func TestMsgCreateLeaseForTenant(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	nonAuthority := f.TestAccs[3]
	authority := f.App.BillingKeeper.GetAuthority()
	denom := types.DefaultDenom

	// Initialize sequences
	err := f.App.BillingKeeper.NextLeaseID.Set(f.Ctx, 1)
	require.NoError(t, err)
	err = f.App.SKUKeeper.NextProviderID.Set(f.Ctx, 1)
	require.NoError(t, err)
	err = f.App.SKUKeeper.NextSKUID.Set(f.Ctx, 1)
	require.NoError(t, err)

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Id, 3600) // 3600 per hour = 1 per second

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
						SkuId:    sku.Id,
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
						SkuId:    sku.Id,
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
						SkuId:    999,
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
				require.Greater(t, resp.LeaseId, uint64(0))

				// Verify lease was created
				lease, err := f.App.BillingKeeper.GetLease(f.Ctx, resp.LeaseId)
				require.NoError(t, err)
				require.Equal(t, tc.msg.Tenant, lease.Tenant)
				require.Equal(t, provider.Id, lease.ProviderId)
				require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)
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
	denom := types.DefaultDenom

	// Initialize sequences
	err := f.App.BillingKeeper.NextLeaseID.Set(f.Ctx, 1)
	require.NoError(t, err)
	err = f.App.SKUKeeper.NextProviderID.Set(f.Ctx, 1)
	require.NoError(t, err)
	err = f.App.SKUKeeper.NextSKUID.Set(f.Ctx, 1)
	require.NoError(t, err)

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Id, 3600) // 3600 per hour = 1 per second

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
		denom,
		types.DefaultMinCreditBalance,
		types.DefaultMaxLeasesPerTenant,
		[]string{allowedUser.String()},
	)
	err = f.App.BillingKeeper.SetParams(f.Ctx, params)
	require.NoError(t, err)

	// Test: allowed user can create lease for tenant
	resp, err := msgServer.CreateLeaseForTenant(f.Ctx, &types.MsgCreateLeaseForTenant{
		Authority: allowedUser.String(),
		Tenant:    tenant.String(),
		Items: []types.LeaseItemInput{
			{
				SkuId:    sku.Id,
				Quantity: 1,
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Greater(t, resp.LeaseId, uint64(0))

	// Test: non-allowed user cannot create lease for tenant
	_, err = msgServer.CreateLeaseForTenant(f.Ctx, &types.MsgCreateLeaseForTenant{
		Authority: notAllowed.String(),
		Tenant:    tenant.String(),
		Items: []types.LeaseItemInput{
			{
				SkuId:    sku.Id,
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
	denom := types.DefaultDenom

	// Initialize sequences
	err := f.App.BillingKeeper.NextLeaseID.Set(f.Ctx, 1)
	require.NoError(t, err)
	err = f.App.SKUKeeper.NextProviderID.Set(f.Ctx, 1)
	require.NoError(t, err)
	err = f.App.SKUKeeper.NextSKUID.Set(f.Ctx, 1)
	require.NoError(t, err)

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Id, 3600)

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

	// Create a lease
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{
				SkuId:    sku.Id,
				Quantity: 1,
			},
		},
	})
	require.NoError(t, err)

	tests := []struct {
		name      string
		msg       *types.MsgCloseLease
		expectErr bool
		errMsg    string
	}{
		{
			name: "success: tenant closes lease",
			msg: &types.MsgCloseLease{
				Sender:  tenant.String(),
				LeaseId: createResp.LeaseId,
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

				// Verify lease is now inactive
				lease, err := f.App.BillingKeeper.GetLease(f.Ctx, tc.msg.LeaseId)
				require.NoError(t, err)
				require.Equal(t, types.LEASE_STATE_INACTIVE, lease.State)
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
	denom := types.DefaultDenom

	// Initialize sequences
	err := f.App.BillingKeeper.NextLeaseID.Set(f.Ctx, 1)
	require.NoError(t, err)
	err = f.App.SKUKeeper.NextProviderID.Set(f.Ctx, 1)
	require.NoError(t, err)
	err = f.App.SKUKeeper.NextSKUID.Set(f.Ctx, 1)
	require.NoError(t, err)

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Id, 3600)

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

	// Create a lease
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{
				SkuId:    sku.Id,
				Quantity: 1,
			},
		},
	})
	require.NoError(t, err)

	// Try to close with random address
	resp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:  randomAddr.String(),
		LeaseId: createResp.LeaseId,
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
	denom := types.DefaultDenom

	// Initialize sequences
	err := f.App.BillingKeeper.NextLeaseID.Set(f.Ctx, 1)
	require.NoError(t, err)
	err = f.App.SKUKeeper.NextProviderID.Set(f.Ctx, 1)
	require.NoError(t, err)
	err = f.App.SKUKeeper.NextSKUID.Set(f.Ctx, 1)
	require.NoError(t, err)

	// Create provider and SKU with 1 unit per second rate
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Id, 3600) // 3600 per hour = 1 per second

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

	// Create a lease
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{
				SkuId:    sku.Id,
				Quantity: 1,
			},
		},
	})
	require.NoError(t, err)

	// Advance block time by 100 seconds
	newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
	f.Ctx = newCtx

	// Provider withdraws
	resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:  providerAddr.String(),
		LeaseId: createResp.LeaseId,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, payoutAddr.String(), resp.PayoutAddress)
	require.True(t, resp.Amount.Amount.IsPositive())

	// Verify payout address received funds
	payoutBalance := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom)
	require.True(t, payoutBalance.Amount.IsPositive())
}

func TestMsgWithdrawAll(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := types.DefaultDenom

	// Initialize sequences
	err := f.App.BillingKeeper.NextLeaseID.Set(f.Ctx, 1)
	require.NoError(t, err)
	err = f.App.SKUKeeper.NextProviderID.Set(f.Ctx, 1)
	require.NoError(t, err)
	err = f.App.SKUKeeper.NextSKUID.Set(f.Ctx, 1)
	require.NoError(t, err)

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Id, 3600)

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

	// Create multiple leases
	for i := 0; i < 3; i++ {
		_, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items: []types.LeaseItemInput{
				{
					SkuId:    sku.Id,
					Quantity: 1,
				},
			},
		})
		require.NoError(t, err)
	}

	// Advance block time
	newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
	f.Ctx = newCtx

	// Provider withdraws all
	resp, err := msgServer.WithdrawAll(f.Ctx, &types.MsgWithdrawAll{
		Sender:     providerAddr.String(),
		ProviderId: provider.Id,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, payoutAddr.String(), resp.PayoutAddress)
	require.Equal(t, uint64(3), resp.LeaseCount)
	require.True(t, resp.TotalAmount.Amount.IsPositive())
}
