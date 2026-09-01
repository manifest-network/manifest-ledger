package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestReservationAccountingInvariant(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)
	tenant := f.TestAccs[0]
	providerAddress := f.TestAccs[1]
	provider := f.createTestProvider(t, providerAddress.String(), providerAddress.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)
	creditAddress := types.DeriveCreditAddress(tenant)
	f.fundAccount(t, creditAddress, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 1_000_000)))
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddress.String(),
	}))
	f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddress, []types.LeaseItemInput{{
		SkuUuid:  sku.Uuid,
		Quantity: 1,
	}})

	message, broken := keeper.ReservationAccountingInvariant(f.App.BillingKeeper)(f.Ctx)
	require.False(t, broken, message)

	account, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	account.ActiveLeaseCount = 0
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, account))
	message, broken = keeper.ReservationAccountingInvariant(f.App.BillingKeeper)(f.Ctx)
	require.True(t, broken)
	require.Contains(t, message, "invalid credit-account lease counts")
	require.Contains(t, message, "active_lease_count 0 but has 1 active leases")

	account.ActiveLeaseCount = 1
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, account))
	validReserved := append(sdk.Coins(nil), account.ReservedAmounts...)
	account.ReservedAmounts = sdk.NewCoins(sdk.NewCoin(
		testDenom,
		validReserved.AmountOf(testDenom).SubRaw(1),
	))
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, account))
	message, broken = keeper.ReservationAccountingInvariant(f.App.BillingKeeper)(f.Ctx)
	require.True(t, broken)
	require.Contains(t, message, "consumable reservations sum")

	account.ReservedAmounts = validReserved
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, account))
	balance := f.App.BankKeeper.GetBalance(f.Ctx, creditAddress, testDenom).Amount
	leave := validReserved.AmountOf(testDenom).SubRaw(1)
	drain := balance.Sub(leave)
	require.True(t, drain.IsPositive())
	require.NoError(t, f.App.BankKeeper.SendCoins(
		f.Ctx,
		creditAddress,
		f.TestAccs[2],
		sdk.NewCoins(sdk.NewCoin(testDenom, drain)),
	))

	message, broken = keeper.ReservationAccountingInvariant(f.App.BillingKeeper)(f.Ctx)
	require.True(t, broken)
	require.Contains(t, message, "under-backed reservation state")
}

func TestDerivedIndexesInvariant(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)
	tenant := f.TestAccs[0]
	providerAddress := f.TestAccs[1]
	provider := f.createTestProvider(t, providerAddress.String(), providerAddress.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)
	creditAddress := types.DeriveCreditAddress(tenant)
	f.fundAccount(t, creditAddress, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 1_000_000)))
	account := types.CreditAccount{Tenant: tenant.String(), CreditAddress: creditAddress.String()}
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, account))
	leaseUUID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddress, []types.LeaseItemInput{{
		SkuUuid:  sku.Uuid,
		Quantity: 1,
	}})
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseUUID)
	require.NoError(t, err)
	lease.Items[0].CustomDomain = "service.example.com"
	require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, lease))

	assertValid := func() {
		t.Helper()
		message, broken := keeper.DerivedIndexesInvariant(f.App.BillingKeeper)(f.Ctx)
		require.False(t, broken, message)
	}
	assertBroken := func(contains string) {
		t.Helper()
		message, broken := keeper.DerivedIndexesInvariant(f.App.BillingKeeper)(f.Ctx)
		require.True(t, broken)
		require.Contains(t, message, contains)
	}

	assertValid()

	require.NoError(t, f.App.BillingKeeper.LeaseBySKUIndex.Remove(
		f.Ctx,
		collections.Join(sku.Uuid, leaseUUID),
	))
	assertBroken("missing from the SKU index")
	require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, lease))
	assertValid()

	require.NoError(t, f.App.BillingKeeper.CreditAddressIndex.Remove(f.Ctx, creditAddress))
	assertBroken("missing from the reverse index")
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, account))
	assertValid()

	require.NoError(t, f.App.BillingKeeper.CustomDomainIndex.Remove(f.Ctx, lease.Items[0].CustomDomain))
	assertBroken("missing from the reverse index")
	require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, lease))
	assertValid()

	require.NoError(t, f.App.BillingKeeper.CustomDomainIndex.Set(f.Ctx, "stale.example.com", types.CustomDomainTarget{
		LeaseUuid:   "00000000-0000-7000-8000-000000000099",
		ServiceName: "service",
	}))
	assertBroken("references missing lease")
}
