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
		MinCreditBalance:   math.NewInt(1000000),
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
		MinCreditBalance:   math.NewInt(1000000),
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
		MinCreditBalance:   math.NewInt(1000000),
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
			MinCreditBalance:   math.NewInt(1000000),
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
