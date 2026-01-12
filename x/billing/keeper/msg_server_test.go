/*
Package keeper_test contains unit tests for the billing module message server.

Test Coverage:
- MsgFundCredit: funding credit accounts, denom validation, balance tracking
- MsgUpdateParams: parameter updates, authority validation
- MsgCreateLease: lease creation with SKU validation, credit checks, max lease limits
- MsgCreateLeaseForTenant: authority-only lease creation on behalf of tenants
- MsgAcknowledgeLease: provider acknowledgement of pending leases
- MsgCloseLease: lease closure by tenant, provider, or authority
- MsgWithdraw: provider withdrawal from individual leases
- MsgWithdrawAll: provider batch withdrawal from all leases

NOTE: With the new lease lifecycle (PENDING -> ACTIVE), tests that need an ACTIVE
lease must call createAndAcknowledgeLease helper instead of just CreateLease.
*/
package keeper_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

// createAndAcknowledgeLease is a helper that creates a lease and acknowledges it,
// returning the lease in ACTIVE state ready for testing.
func (f *testFixture) createAndAcknowledgeLease(
	t *testing.T,
	msgServer types.MsgServer,
	tenant sdk.AccAddress,
	providerAddr sdk.AccAddress,
	items []types.LeaseItemInput,
) string {
	t.Helper()

	// Create the lease
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  items,
	})
	require.NoError(t, err)
	require.NotNil(t, createResp)

	// Acknowledge the lease as the provider
	ackResp, err := msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{createResp.LeaseUuid},
	})
	require.NoError(t, err)
	require.NotNil(t, ackResp)

	// Verify lease is now ACTIVE
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, createResp.LeaseUuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)

	return createResp.LeaseUuid
}

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
	sku := f.createTestSKU(t, provider.Uuid, 100)

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
				Sender:    tenant.String(),
				LeaseUuid: leaseID,
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
				lease, err := f.App.BillingKeeper.GetLease(f.Ctx, tc.msg.LeaseUuid)
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
		Sender:    randomAddr.String(),
		LeaseUuid: leaseID,
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

	// Advance block time by 100 seconds
	newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
	f.Ctx = newCtx

	// Provider withdraws
	resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:    providerAddr.String(),
		LeaseUuid: leaseID,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, payoutAddr.String(), resp.PayoutAddress)
	require.False(t, resp.Amounts.IsZero())

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

	// Create multiple leases and acknowledge them
	for i := 0; i < 3; i++ {
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

		// Acknowledge each lease
		_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
			Sender:     providerAddr.String(),
			LeaseUuids: []string{createResp.LeaseUuid},
		})
		require.NoError(t, err)
	}

	// Advance block time
	newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
	f.Ctx = newCtx

	// Provider withdraws all
	resp, err := msgServer.WithdrawAll(f.Ctx, &types.MsgWithdrawAll{
		Sender:       providerAddr.String(),
		ProviderUuid: provider.Uuid,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, payoutAddr.String(), resp.PayoutAddress)
	require.Equal(t, uint64(3), resp.LeaseCount)
	require.False(t, resp.TotalAmounts.IsZero())
}

// TestMsgWithdrawAll_AtomicSettlement verifies that WithdrawAll uses CacheContext
// to make settlement + timestamp update atomic per lease. This is tested by:
// 1. Calling WithdrawAll to settle leases (should transfer funds)
// 2. Calling WithdrawAll again immediately (should transfer 0 - proves LastSettledAt was updated)
// If settlement and timestamp update weren't atomic, a failure in SetLease could
// leave LastSettledAt unchanged while funds were transferred, allowing double-withdrawal.
func TestMsgWithdrawAll_AtomicSettlement(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	denom := testDenom

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 1 token per second

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

	// Create and acknowledge a lease
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

	_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{createResp.LeaseUuid},
	})
	require.NoError(t, err)

	// Record payout address balance before any withdrawals
	payoutBalanceBefore := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom)

	// Advance block time by 100 seconds
	newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
	f.Ctx = newCtx

	// First WithdrawAll - should transfer ~100 tokens (100 seconds * 1 token/second)
	resp1, err := msgServer.WithdrawAll(f.Ctx, &types.MsgWithdrawAll{
		Sender:       providerAddr.String(),
		ProviderUuid: provider.Uuid,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(1), resp1.LeaseCount)
	require.False(t, resp1.TotalAmounts.IsZero(), "first withdrawal should transfer funds")

	firstWithdrawalAmount := resp1.TotalAmounts.AmountOf(denom)
	require.True(t, firstWithdrawalAmount.GT(sdkmath.ZeroInt()), "first withdrawal should be positive")

	// Second WithdrawAll immediately (same block time) - should transfer 0
	// This proves that LastSettledAt was atomically updated with the settlement
	resp2, err := msgServer.WithdrawAll(f.Ctx, &types.MsgWithdrawAll{
		Sender:       providerAddr.String(),
		ProviderUuid: provider.Uuid,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(0), resp2.LeaseCount, "second withdrawal should process 0 leases")
	require.True(t, resp2.TotalAmounts.IsZero(), "second withdrawal should transfer 0 (LastSettledAt was updated)")

	// Verify payout address received exactly the first withdrawal amount
	payoutBalanceAfter := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom)
	actualTransferred := payoutBalanceAfter.Amount.Sub(payoutBalanceBefore.Amount)
	require.Equal(t, firstWithdrawalAmount, actualTransferred,
		"payout address should receive exactly the first withdrawal amount, no double-payment")

	// Advance time again and verify we can still withdraw new accruals
	newCtx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(50 * time.Second))
	f.Ctx = newCtx

	resp3, err := msgServer.WithdrawAll(f.Ctx, &types.MsgWithdrawAll{
		Sender:       providerAddr.String(),
		ProviderUuid: provider.Uuid,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(1), resp3.LeaseCount)
	require.False(t, resp3.TotalAmounts.IsZero(), "third withdrawal should transfer new accruals")
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

	t.Run("fail: withdraw from non-existent lease", func(t *testing.T) {
		_, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
			Sender:    providerAddr.String(),
			LeaseUuid: "01912345-6789-7abc-8def-999999999999",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("fail: unauthorized user cannot withdraw", func(t *testing.T) {
		_, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
			Sender:    unauthorizedUser.String(),
			LeaseUuid: leaseID,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unauthorized")
	})

	t.Run("fail: tenant cannot withdraw (not provider or authority)", func(t *testing.T) {
		_, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
			Sender:    tenant.String(),
			LeaseUuid: leaseID,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unauthorized")
	})
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

	// Advance block time by 100 seconds (should accrue 100 units, but only 50 available)
	newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
	f.Ctx = newCtx

	// Provider withdraws - should only get up to 50 (the available credit)
	resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:    providerAddr.String(),
		LeaseUuid: leaseID,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify provider's payout address received funds
	payoutBalance := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom)
	require.True(t, payoutBalance.Amount.IsPositive(), "provider should have received some funds")
	require.True(t, payoutBalance.Amount.LTE(sdkmath.NewInt(50)), "provider should not receive more than credit balance")

	// Verify credit balance decreased
	creditBalance := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, denom)
	require.True(t, creditBalance.Amount.LT(sdkmath.NewInt(50)), "credit balance should have decreased")
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

	// DO NOT advance block time - withdraw immediately
	// With zero duration, there's nothing to withdraw, so it should error
	_, err = msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:    providerAddr.String(),
		LeaseUuid: createResp.LeaseUuid,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no withdrawable amount")
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

	// Close lease (should settle outstanding amount)
	closeResp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:    tenant.String(),
		LeaseUuid: leaseID,
	})
	require.NoError(t, err)
	require.NotNil(t, closeResp)

	// Verify lease is closed
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
	require.NotNil(t, lease.ClosedAt)

	// Verify provider received settled funds during closure
	finalPayoutBalance := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom)
	require.True(t, finalPayoutBalance.Amount.GT(initialPayoutBalance.Amount),
		"provider should have received settlement during close")
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
		Sender:    tenant.String(),
		LeaseUuid: leaseID,
	})
	require.NoError(t, err)
	require.NotNil(t, closeResp)

	// Verify provider received 30 (all available credit)
	payoutBalance := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom)
	require.Equal(t, sdkmath.NewInt(30), payoutBalance.Amount)

	// Verify credit balance is now zero
	creditBalance := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, denom)
	require.True(t, creditBalance.Amount.IsZero())
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
		Sender:    providerAddr.String(),
		LeaseUuid: leaseID,
	})
	require.NoError(t, err)
	require.NotNil(t, closeResp)

	// Verify lease is closed
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
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
		Sender:    f.Authority.String(),
		LeaseUuid: leaseID,
	})
	require.NoError(t, err)
	require.NotNil(t, closeResp)

	// Verify lease is closed
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
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
			Sender:    tenant.String(),
			LeaseUuid: "01912345-6789-7abc-8def-999999999999",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("fail: unauthorized user cannot close", func(t *testing.T) {
		_, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
			Sender:    unauthorizedUser.String(),
			LeaseUuid: leaseID,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unauthorized")
	})

	t.Run("fail: close already closed lease", func(t *testing.T) {
		// First close the lease
		_, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
			Sender:    tenant.String(),
			LeaseUuid: leaseID,
		})
		require.NoError(t, err)

		// Try to close again
		_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
			Sender:    tenant.String(),
			LeaseUuid: leaseID,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not active")
	})
}

// TestMsgWithdrawAll_ErrorCases tests error scenarios for WithdrawAll.
func TestMsgWithdrawAll_ErrorCases(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	unauthorizedUser := f.TestAccs[3]

	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())

	t.Run("fail: withdraw from non-existent provider", func(t *testing.T) {
		_, err := msgServer.WithdrawAll(f.Ctx, &types.MsgWithdrawAll{
			Sender:       providerAddr.String(),
			ProviderUuid: "01912345-6789-7abc-8def-999999999999",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("fail: unauthorized user cannot withdraw all", func(t *testing.T) {
		_, err := msgServer.WithdrawAll(f.Ctx, &types.MsgWithdrawAll{
			Sender:       unauthorizedUser.String(),
			ProviderUuid: provider.Uuid,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unauthorized")
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

	// Close the lease
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:    tenant.String(),
		LeaseUuid: leaseID,
	})
	require.NoError(t, err)

	// Verify the lease is closed
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)

	// Try to withdraw from closed lease - should fail
	_, err = msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:    providerAddr.String(),
		LeaseUuid: leaseID,
	})
	require.Error(t, err, "should fail to withdraw from closed lease")
}

// =============================================================================
// Multi-Denom Tests
// =============================================================================

// createTestSKUWithDenom creates a SKU with a specific denom for testing multi-denom scenarios.
func (f *testFixture) createTestSKUWithDenom(t *testing.T, providerUUID string, priceAmount int64, denom string) skutypes.SKU {
	t.Helper()

	skuUUID, err := f.App.SKUKeeper.GenerateSKUUUID(f.Ctx)
	require.NoError(t, err)

	sku := skutypes.SKU{
		Uuid:         skuUUID,
		ProviderUuid: providerUUID,
		Name:         "Test SKU " + denom,
		Unit:         skutypes.Unit_UNIT_PER_HOUR,
		BasePrice:    sdk.NewCoin(denom, sdkmath.NewInt(priceAmount)),
		Active:       true,
	}
	err = f.App.SKUKeeper.SetSKU(f.Ctx, sku)
	require.NoError(t, err)
	return sku
}

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

		resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
			Sender:    providerAddr.String(),
			LeaseUuid: leaseID,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, payoutAddr.String(), resp.PayoutAddress)

		// Verify provider received both denoms
		require.False(t, resp.Amounts.IsZero())

		// Check actual balances increased
		newBalance1 := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom)
		newBalance2 := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom2)

		require.True(t, newBalance1.Amount.GT(initialBalance1.Amount),
			"provider should receive testDenom funds")
		require.True(t, newBalance2.Amount.GT(initialBalance2.Amount),
			"provider should receive testDenom2 funds")
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
			Sender:    tenant.String(),
			LeaseUuid: leaseID,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Verify lease is closed
		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)

		// Verify provider received both denoms via settlement
		newBalance1 := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom)
		newBalance2 := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom2)

		require.True(t, newBalance1.Amount.GT(initialBalance1.Amount),
			"provider should receive testDenom settlement")
		require.True(t, newBalance2.Amount.GT(initialBalance2.Amount),
			"provider should receive testDenom2 settlement")
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
			CreatedAt:     f.Ctx.BlockTime(),
			LastSettledAt: f.Ctx.BlockTime(),
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
			Sender:    providerAddr.String(),
			LeaseUuid: leaseID,
			Reason:    "Resource unavailable",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.False(t, resp.RejectedAt.IsZero())

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
			Sender:    unauthorizedUser.String(),
			LeaseUuid: leaseID2,
			Reason:    "Test",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unauthorized")
	})

	t.Run("fail: cannot reject already rejected lease", func(t *testing.T) {
		_, err := msgServer.RejectLease(f.Ctx, &types.MsgRejectLease{
			Sender:    providerAddr.String(),
			LeaseUuid: leaseID, // Already rejected above
			Reason:    "Test",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not in PENDING state")
	})

	t.Run("success: authority can reject lease", func(t *testing.T) {
		resp, err := msgServer.RejectLease(f.Ctx, &types.MsgRejectLease{
			Sender:    f.Authority.String(),
			LeaseUuid: leaseID2,
			Reason:    "Admin rejection",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
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
			Tenant:    tenant.String(),
			LeaseUuid: leaseID,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.False(t, resp.CancelledAt.IsZero())

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

	t.Run("fail: other tenant cannot cancel", func(t *testing.T) {
		_, err := msgServer.CancelLease(f.Ctx, &types.MsgCancelLease{
			Tenant:    otherTenant.String(),
			LeaseUuid: leaseID2,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unauthorized")
	})

	t.Run("fail: cannot cancel non-existent lease", func(t *testing.T) {
		_, err := msgServer.CancelLease(f.Ctx, &types.MsgCancelLease{
			Tenant:    tenant.String(),
			LeaseUuid: "01912345-6789-7abc-8def-999999999999",
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
			Tenant:    tenant.String(),
			LeaseUuid: leaseID2,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not in PENDING state")
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
		Sender:    providerAddr.String(),
		LeaseUuid: createResp2.LeaseUuid,
		Reason:    "Test rejection",
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
			LastSettledAt:  baseTime,
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
			Uuid:          leaseUUID,
			Tenant:        tenant.String(),
			ProviderUuid:  provider.Uuid,
			Items:         []types.LeaseItem{{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewCoin(denom, sdkmath.NewInt(1))}},
			State:         types.LEASE_STATE_PENDING,
			CreatedAt:     baseTime, // Will be expired
			LastSettledAt: baseTime,
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
			Uuid:          leaseUUID,
			Tenant:        tenant.String(),
			ProviderUuid:  provider.Uuid,
			Items:         []types.LeaseItem{{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewCoin(denom, sdkmath.NewInt(1))}},
			State:         types.LEASE_STATE_PENDING,
			CreatedAt:     futureTime, // Created 30s later, won't be expired yet
			LastSettledAt: futureTime,
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
