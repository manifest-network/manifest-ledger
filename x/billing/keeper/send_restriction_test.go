package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

const testPWRDenom = "factory/manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj/upwr"

func TestCreditAccountSendRestriction(t *testing.T) {
	f := initFixture(t)

	denom := testPWRDenom

	// Set up params with specific denom
	params := types.Params{
		Denom:              denom,
		MaxLeasesPerTenant: 100,
		AllowedList:        []string{},
	}
	err := f.App.BillingKeeper.SetParams(f.Ctx, params)
	require.NoError(t, err)

	// Create a tenant with a credit account
	tenant := sdk.AccAddress([]byte("tenant_addr_________"))
	creditAddr := types.DeriveCreditAddress(tenant)

	// Create the credit account in state
	creditAccount := types.CreditAccount{
		Tenant:           tenant.String(),
		ActiveLeaseCount: 0,
	}
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, creditAccount)
	require.NoError(t, err)

	sender := sdk.AccAddress([]byte("sender_addr_________"))
	nonCreditAddr := sdk.AccAddress([]byte("normal_addr_________"))

	t.Run("allow_transfer_to_non_credit_account", func(t *testing.T) {
		// Any denom should be allowed to non-credit addresses
		coins := sdk.NewCoins(sdk.NewCoin("umfx", math.NewInt(1000)))
		resultAddr, err := f.App.BillingKeeper.CreditAccountSendRestriction(f.Ctx, sender, nonCreditAddr, coins)
		require.NoError(t, err)
		require.Equal(t, nonCreditAddr, resultAddr)

		// Also test with the billing denom
		coins = sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(1000)))
		resultAddr, err = f.App.BillingKeeper.CreditAccountSendRestriction(f.Ctx, sender, nonCreditAddr, coins)
		require.NoError(t, err)
		require.Equal(t, nonCreditAddr, resultAddr)
	})

	t.Run("allow_correct_denom_to_credit_account", func(t *testing.T) {
		coins := sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(1000)))
		resultAddr, err := f.App.BillingKeeper.CreditAccountSendRestriction(f.Ctx, sender, creditAddr, coins)
		require.NoError(t, err)
		require.Equal(t, creditAddr, resultAddr)
	})

	t.Run("reject_wrong_denom_to_credit_account", func(t *testing.T) {
		coins := sdk.NewCoins(sdk.NewCoin("umfx", math.NewInt(1000)))
		_, err := f.App.BillingKeeper.CreditAccountSendRestriction(f.Ctx, sender, creditAddr, coins)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot send umfx to credit account")
		require.Contains(t, err.Error(), denom)
	})

	t.Run("reject_multiple_denoms_with_wrong_denom_to_credit_account", func(t *testing.T) {
		// Even if one denom is correct, if another is wrong, reject
		coins := sdk.NewCoins(
			sdk.NewCoin(denom, math.NewInt(1000)),
			sdk.NewCoin("umfx", math.NewInt(500)),
		)
		_, err := f.App.BillingKeeper.CreditAccountSendRestriction(f.Ctx, sender, creditAddr, coins)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot send umfx to credit account")
	})

	t.Run("allow_multiple_correct_denom_to_credit_account", func(t *testing.T) {
		// Multiple coins of the same correct denom should be allowed
		// (though typically you'd have one coin per denom)
		coins := sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(1000)))
		resultAddr, err := f.App.BillingKeeper.CreditAccountSendRestriction(f.Ctx, sender, creditAddr, coins)
		require.NoError(t, err)
		require.Equal(t, creditAddr, resultAddr)
	})
}

func TestCreditAccountSendRestriction_NoCreditAccountsExist(t *testing.T) {
	f := initFixture(t)

	denom := testPWRDenom

	// Set up params with specific denom
	params := types.Params{
		Denom:              denom,
		MaxLeasesPerTenant: 100,
		AllowedList:        []string{},
	}
	err := f.App.BillingKeeper.SetParams(f.Ctx, params)
	require.NoError(t, err)

	sender := sdk.AccAddress([]byte("sender_addr_________"))
	randomAddr := sdk.AccAddress([]byte("random_addr_________"))

	t.Run("allow_any_transfer_when_no_credit_accounts", func(t *testing.T) {
		// When no credit accounts exist, any transfer should be allowed
		coins := sdk.NewCoins(sdk.NewCoin("umfx", math.NewInt(1000)))
		resultAddr, err := f.App.BillingKeeper.CreditAccountSendRestriction(f.Ctx, sender, randomAddr, coins)
		require.NoError(t, err)
		require.Equal(t, randomAddr, resultAddr)
	})
}

func TestCreditAccountSendRestriction_Integration(t *testing.T) {
	f := initFixture(t)

	denom := testPWRDenom

	// Set up params with specific denom
	params := types.Params{
		Denom:              denom,
		MaxLeasesPerTenant: 100,
		AllowedList:        []string{},
	}
	err := f.App.BillingKeeper.SetParams(f.Ctx, params)
	require.NoError(t, err)

	// Create a tenant and fund them
	tenant := f.TestAccs[0]
	fundAmount := math.NewInt(10000000)

	// Fund tenant with both correct and wrong denoms
	f.fundAccount(t, tenant, sdk.NewCoins(
		sdk.NewCoin(denom, fundAmount),
		sdk.NewCoin("umfx", fundAmount),
	))

	// Create credit account by funding it
	creditAddr := types.DeriveCreditAddress(tenant)

	// Create the credit account in state
	creditAccount := types.CreditAccount{
		Tenant:           tenant.String(),
		ActiveLeaseCount: 0,
	}
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, creditAccount)
	require.NoError(t, err)

	t.Run("bank_send_correct_denom_succeeds", func(t *testing.T) {
		// The send restriction is registered at app level, so we test through the keeper method
		coins := sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(1000)))
		resultAddr, err := f.App.BillingKeeper.CreditAccountSendRestriction(f.Ctx, tenant, creditAddr, coins)
		require.NoError(t, err)
		require.Equal(t, creditAddr, resultAddr)
	})

	t.Run("bank_send_wrong_denom_fails", func(t *testing.T) {
		coins := sdk.NewCoins(sdk.NewCoin("umfx", math.NewInt(1000)))
		_, err := f.App.BillingKeeper.CreditAccountSendRestriction(f.Ctx, tenant, creditAddr, coins)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrInvalidDenomination)
	})
}

func TestIsCreditAccountAddress(t *testing.T) {
	f := initFixture(t)

	// Create some tenants with credit accounts
	tenant1 := sdk.AccAddress([]byte("tenant1_addr________"))
	tenant2 := sdk.AccAddress([]byte("tenant2_addr________"))
	randomAddr := sdk.AccAddress([]byte("random_addr_________"))

	creditAddr1 := types.DeriveCreditAddress(tenant1)
	creditAddr2 := types.DeriveCreditAddress(tenant2)

	// Create credit accounts in state
	err := f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:           tenant1.String(),
		ActiveLeaseCount: 0,
	})
	require.NoError(t, err)

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:           tenant2.String(),
		ActiveLeaseCount: 0,
	})
	require.NoError(t, err)

	t.Run("detect_credit_account_1", func(t *testing.T) {
		// Use the send restriction to indirectly test
		denom := testPWRDenom
		params := types.Params{
			Denom:              denom,
			MaxLeasesPerTenant: 100,
			AllowedList:        []string{},
		}
		err := f.App.BillingKeeper.SetParams(f.Ctx, params)
		require.NoError(t, err)

		// Wrong denom to credit account should fail
		coins := sdk.NewCoins(sdk.NewCoin("umfx", math.NewInt(1000)))
		_, err = f.App.BillingKeeper.CreditAccountSendRestriction(f.Ctx, randomAddr, creditAddr1, coins)
		require.Error(t, err) // Should fail because it's a credit account
	})

	t.Run("detect_credit_account_2", func(t *testing.T) {
		coins := sdk.NewCoins(sdk.NewCoin("umfx", math.NewInt(1000)))
		_, err := f.App.BillingKeeper.CreditAccountSendRestriction(f.Ctx, randomAddr, creditAddr2, coins)
		require.Error(t, err) // Should fail because it's a credit account
	})

	t.Run("non_credit_account_allows_any_denom", func(t *testing.T) {
		coins := sdk.NewCoins(sdk.NewCoin("umfx", math.NewInt(1000)))
		resultAddr, err := f.App.BillingKeeper.CreditAccountSendRestriction(f.Ctx, tenant1, randomAddr, coins)
		require.NoError(t, err)
		require.Equal(t, randomAddr, resultAddr)
	})
}

func TestCreditAddressIndexSurvivesParamsUpdate(t *testing.T) {
	f := initFixture(t)

	denom := testPWRDenom
	newDenom := "factory/manifest1xyz/newpwr"

	// Set up initial params
	params := types.Params{
		Denom:              denom,
		MaxLeasesPerTenant: 100,
		AllowedList:        []string{},
		MaxItemsPerLease:   10,
	}
	err := f.App.BillingKeeper.SetParams(f.Ctx, params)
	require.NoError(t, err)

	// Create a tenant with a credit account
	tenant := sdk.AccAddress([]byte("tenant_addr_________"))
	creditAddr := types.DeriveCreditAddress(tenant)

	// Create the credit account in state
	creditAccount := types.CreditAccount{
		Tenant:           tenant.String(),
		ActiveLeaseCount: 0,
	}
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, creditAccount)
	require.NoError(t, err)

	// Verify credit address is indexed and restriction works
	sender := sdk.AccAddress([]byte("sender_addr_________"))
	coins := sdk.NewCoins(sdk.NewCoin("umfx", math.NewInt(1000)))
	_, err = f.App.BillingKeeper.CreditAccountSendRestriction(f.Ctx, sender, creditAddr, coins)
	require.Error(t, err, "wrong denom should be rejected before params update")
	require.ErrorIs(t, err, types.ErrInvalidDenomination)

	// Update params with a new denom
	newParams := types.Params{
		Denom:              newDenom,
		MaxLeasesPerTenant: 200,
		AllowedList:        []string{},
		MaxItemsPerLease:   20,
	}
	err = f.App.BillingKeeper.SetParams(f.Ctx, newParams)
	require.NoError(t, err)

	// Verify credit address is still indexed after params update
	// The old denom should now be rejected
	_, err = f.App.BillingKeeper.CreditAccountSendRestriction(f.Ctx, sender, creditAddr, coins)
	require.Error(t, err, "wrong denom should be rejected after params update")

	// The new denom should be accepted
	newDenomCoins := sdk.NewCoins(sdk.NewCoin(newDenom, math.NewInt(1000)))
	resultAddr, err := f.App.BillingKeeper.CreditAccountSendRestriction(f.Ctx, sender, creditAddr, newDenomCoins)
	require.NoError(t, err, "new denom should be accepted after params update")
	require.Equal(t, creditAddr, resultAddr)

	// Verify credit account still exists and is accessible
	retrievedAccount, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, tenant.String(), retrievedAccount.Tenant)
}

func TestCreditAddressIndexCreatedOnFunding(t *testing.T) {
	f := initFixture(t)

	denom := testPWRDenom

	// Set up params
	params := types.Params{
		Denom:              denom,
		MaxLeasesPerTenant: 100,
		AllowedList:        []string{},
		MaxItemsPerLease:   10,
	}
	err := f.App.BillingKeeper.SetParams(f.Ctx, params)
	require.NoError(t, err)

	// Create a new tenant
	tenant := f.TestAccs[0]
	creditAddr := types.DeriveCreditAddress(tenant)

	// Fund the tenant with PWR tokens
	fundAmount := math.NewInt(100000000)
	f.fundAccount(t, tenant, sdk.NewCoins(sdk.NewCoin(denom, fundAmount)))

	// Before funding credit account, verify send restriction allows any denom
	// (because no credit account exists yet for this tenant)
	sender := sdk.AccAddress([]byte("sender_addr_________"))
	wrongDenomCoins := sdk.NewCoins(sdk.NewCoin("umfx", math.NewInt(1000)))
	resultAddr, err := f.App.BillingKeeper.CreditAccountSendRestriction(f.Ctx, sender, creditAddr, wrongDenomCoins)
	require.NoError(t, err, "before credit account creation, any denom should be allowed to derived address")
	require.Equal(t, creditAddr, resultAddr)

	// Now create and fund the credit account directly
	creditAccount := types.CreditAccount{
		Tenant:           tenant.String(),
		ActiveLeaseCount: 0,
	}
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, creditAccount)
	require.NoError(t, err)

	// After creating credit account, the credit address should be indexed and restriction should apply
	_, err = f.App.BillingKeeper.CreditAccountSendRestriction(f.Ctx, sender, creditAddr, wrongDenomCoins)
	require.Error(t, err, "after credit account creation, wrong denom should be rejected")
	require.ErrorIs(t, err, types.ErrInvalidDenomination)

	// Correct denom should still be allowed
	correctDenomCoins := sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(1000)))
	resultAddr, err = f.App.BillingKeeper.CreditAccountSendRestriction(f.Ctx, sender, creditAddr, correctDenomCoins)
	require.NoError(t, err, "correct denom should be allowed to credit account")
	require.Equal(t, creditAddr, resultAddr)
}
