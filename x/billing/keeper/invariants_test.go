package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

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
