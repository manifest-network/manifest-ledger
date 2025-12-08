/*
Package keeper_test contains unit tests for the billing module message server.

Test Coverage:
- MsgFundCredit: funding credit accounts, denom validation, balance tracking
- MsgUpdateParams: parameter updates, authority validation
- MsgCreateLease: stub behavior (full implementation in Phase 2)
- MsgCloseLease: stub behavior (full implementation in Phase 2)
- MsgWithdraw: stub behavior (full implementation in Phase 2)
- MsgWithdrawAll: stub behavior (full implementation in Phase 2)
*/
package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/testutil/testdata"
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

func TestMsgCreateLeaseStub(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]

	// Initialize the NextLeaseID sequence
	err := f.App.BillingKeeper.NextLeaseID.Set(f.Ctx, 1)
	require.NoError(t, err)

	// CreateLease is currently a stub - just verify it doesn't panic
	msg := &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{
				SkuId:    1,
				Quantity: 1,
			},
		},
	}

	resp, err := msgServer.CreateLease(f.Ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Greater(t, resp.LeaseId, uint64(0))
}

func TestMsgCloseLeaseStub(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	_, _, sender := testdata.KeyTestPubAddr()

	// CloseLease is currently a stub - just verify it doesn't panic
	msg := &types.MsgCloseLease{
		Sender:  sender.String(),
		LeaseId: 1,
	}

	resp, err := msgServer.CloseLease(f.Ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestMsgWithdrawStub(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	_, _, sender := testdata.KeyTestPubAddr()

	// Withdraw is currently a stub - just verify it doesn't panic
	msg := &types.MsgWithdraw{
		Sender:  sender.String(),
		LeaseId: 1,
	}

	resp, err := msgServer.Withdraw(f.Ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestMsgWithdrawAllStub(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	_, _, sender := testdata.KeyTestPubAddr()

	// WithdrawAll is currently a stub - just verify it doesn't panic
	msg := &types.MsgWithdrawAll{
		Sender:     sender.String(),
		ProviderId: 1,
	}

	resp, err := msgServer.WithdrawAll(f.Ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)
}
