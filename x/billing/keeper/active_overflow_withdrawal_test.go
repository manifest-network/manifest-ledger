package keeper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	tfkeeper "github.com/strangelove-ventures/tokenfactory/x/tokenfactory/keeper"
	tftypes "github.com/strangelove-ventures/tokenfactory/x/tokenfactory/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
	skukeeper "github.com/manifest-network/manifest-ledger/x/sku/keeper"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

// A valid initial reservation can fit math.Int while a later charge does not.
// Exercise normal message handlers throughout setup rather than planting an
// overflowing rate in storage: ACTIVE withdrawal must auto-close before it
// reaches the checked settlement helper, preserving every sibling's allocation.
func TestMsgWithdrawActiveAccrualOverflowAutoCloses(t *testing.T) {
	f := initFixture(t)
	tenant, provider, payout := f.TestAccs[0], f.TestAccs[1], f.TestAccs[2]
	k := f.App.BillingKeeper
	server := keeper.NewMsgServerImpl(k)
	factory := tfkeeper.NewMsgServerImpl(f.App.TokenFactoryKeeper)
	f.fundAccount(t, tenant, f.App.TokenFactoryKeeper.GetParams(f.Ctx).DenomCreationFee)
	createdDenom, err := factory.CreateDenom(f.Ctx, &tftypes.MsgCreateDenom{
		Sender: tenant.String(), Subdenom: "overflow",
	})
	require.NoError(t, err)
	denom := createdDenom.NewTokenDenom

	// The hourly SKU price and one-hour reservation are below half the integer
	// ceiling. Three hours at the locked per-second rate exceed that ceiling.
	rate := maxBillingTestInt().QuoRaw(7200)
	price := sdk.NewCoin(denom, rate.MulRaw(3600))
	siblingPrice := sdk.NewInt64Coin(denom, 3600)
	deposit := price.Add(siblingPrice)
	_, err = factory.Mint(f.Ctx, &tftypes.MsgMint{
		Sender: tenant.String(), Amount: deposit, MintToAddress: tenant.String(),
	})
	require.NoError(t, err)
	_, err = server.FundCredit(f.Ctx, &types.MsgFundCredit{
		Sender: tenant.String(), Tenant: tenant.String(), Amount: deposit,
	})
	require.NoError(t, err)

	catalog := skukeeper.NewMsgServerImpl(f.App.SKUKeeper)
	authority := f.App.SKUKeeper.GetAuthority()
	createdProvider, err := catalog.CreateProvider(f.Ctx, &skutypes.MsgCreateProvider{
		Authority: authority, Address: provider.String(), PayoutAddress: payout.String(),
	})
	require.NoError(t, err)
	createLease := func(name string, basePrice sdk.Coin) string {
		t.Helper()
		createdSKU, createErr := catalog.CreateSKU(f.Ctx, &skutypes.MsgCreateSKU{
			Authority: authority, ProviderUuid: createdProvider.Uuid, Name: name,
			Unit: skutypes.Unit_UNIT_PER_HOUR, BasePrice: basePrice,
		})
		require.NoError(t, createErr)
		return f.createAndAcknowledgeLease(t, server, tenant, provider, []types.LeaseItemInput{
			{SkuUuid: createdSKU.Uuid, Quantity: 1},
		})
	}
	leaseID := createLease("large valid rate", price)
	siblingID := createLease("protected sibling", siblingPrice)
	lease, err := k.GetLease(f.Ctx, leaseID)
	require.NoError(t, err)
	require.Equal(t, rate, lease.Items[0].LockedPrice.Amount)
	require.Equal(t, sdk.NewCoins(price), lease.Reservation.RemainingAmounts)
	siblingBefore, err := k.GetLease(f.Ctx, siblingID)
	require.NoError(t, err)
	account, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.NoError(t, k.ExportGenesis(f.Ctx).ValidateCurrentState())
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(3 * time.Hour)).WithEventManager(sdk.NewEventManager())

	// The standalone checked helper still rejects this exact interval. The
	// public withdrawal succeeds because its earlier ACTIVE guard intercepts
	// the overflow and uses the terminal capped settlement path instead.
	beforeChecked := snapshotPayoutStores(t, f, f.Ctx)
	checked, err := k.PerformSettlement(f.Ctx, &lease, &account, f.Ctx.BlockTime())
	require.ErrorIs(t, err, types.ErrArithmeticOverflow)
	require.Nil(t, checked)
	require.Equal(t, beforeChecked, snapshotPayoutStores(t, f, f.Ctx))
	require.Empty(t, f.Ctx.EventManager().Events())

	response, err := server.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender: provider.String(), LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)
	require.Equal(t, uint64(1), response.WithdrawalCount)
	require.Equal(t, sdk.NewCoins(price), response.TotalAmounts)
	closed, err := k.GetLease(f.Ctx, leaseID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, closed.State)
	require.Equal(t, types.ClosureReasonCreditExhausted, closed.ClosureReason)
	require.NotNil(t, closed.ClosedAt)
	require.Equal(t, f.Ctx.BlockTime(), *closed.ClosedAt)
	require.Equal(t, *closed.ClosedAt, closed.LastSettledAt)
	require.Empty(t, closed.Reservation.RemainingAmounts)
	require.Equal(t, price, f.App.BankKeeper.GetBalance(f.Ctx, payout, denom))
	require.Equal(t, siblingPrice, f.App.BankKeeper.GetBalance(f.Ctx, types.DeriveCreditAddress(tenant), denom))
	siblingAfter, err := k.GetLease(f.Ctx, siblingID)
	require.NoError(t, err)
	require.Equal(t, siblingBefore, siblingAfter)
	accountAfter, err := k.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(1), accountAfter.ActiveLeaseCount)
	require.Zero(t, accountAfter.PendingLeaseCount)
	require.Equal(t, siblingBefore.Reservation.RemainingAmounts, accountAfter.ReservedAmounts)
	event := findEvent(t, f.Ctx, types.EventTypeProviderWithdraw)
	require.Equal(t, leaseID, attrValue(t, event, types.AttributeKeyLeaseUUID))
	require.Equal(t, price.String(), attrValue(t, event, types.AttributeKeyAmount))
	require.Equal(t, "true", attrValue(t, event, types.AttributeKeyAutoClosed))
	message, broken := keeper.ReservationAccountingInvariant(k)(f.Ctx)
	require.False(t, broken, message)
}
