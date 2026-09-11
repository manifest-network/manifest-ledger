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
)

// TestTokenFactoryCannotDebitBillingCredit uses the app's real tokenfactory bank
// dependency, including both privileged message handlers. A debit of just one
// unreserved token must fail as well as a debit that would break reservations.
func TestTokenFactoryCannotDebitBillingCredit(t *testing.T) {
	for _, action := range []string{"burn", "force transfer"} {
		t.Run(action, func(t *testing.T) {
			f := initFixture(t)
			tenant, providerAddr, denomAdmin, nextAdmin := f.TestAccs[0], f.TestAccs[1], f.TestAccs[2], f.TestAccs[3]
			tf := tfkeeper.NewMsgServerImpl(f.App.TokenFactoryKeeper)
			f.fundAccount(t, denomAdmin, f.App.TokenFactoryKeeper.GetParams(f.Ctx).DenomCreationFee)
			created, err := tf.CreateDenom(f.Ctx, &tftypes.MsgCreateDenom{Sender: denomAdmin.String(), Subdenom: "credit"})
			require.NoError(t, err)
			denom := created.NewTokenDenom
			_, err = tf.Mint(f.Ctx, &tftypes.MsgMint{Sender: denomAdmin.String(), Amount: sdk.NewInt64Coin(denom, 20_000), MintToAddress: tenant.String()})
			require.NoError(t, err)
			f.fundAccount(t, tenant, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 10_000)))

			k := f.App.BillingKeeper
			ms := keeper.NewMsgServerImpl(k)
			for _, depositDenom := range []string{denom, testDenom} {
				_, err = ms.FundCredit(f.Ctx, &types.MsgFundCredit{
					Sender: tenant.String(), Tenant: tenant.String(), Amount: sdk.NewInt64Coin(depositDenom, 10_000),
				})
				require.NoError(t, err)
			}
			creditAddr := types.DeriveCreditAddress(tenant)
			registered, err := k.CreditAddressIndex.Has(f.Ctx, creditAddr)
			require.NoError(t, err)
			require.True(t, registered)

			// Every inbound route remains allowed after account registration.
			_, err = tf.Mint(f.Ctx, &tftypes.MsgMint{Sender: denomAdmin.String(), Amount: sdk.NewInt64Coin(denom, 1_000), MintToAddress: creditAddr.String()})
			require.NoError(t, err)
			_, err = tf.ForceTransfer(f.Ctx, &tftypes.MsgForceTransfer{
				Sender: denomAdmin.String(), Amount: sdk.NewInt64Coin(denom, 1_000),
				TransferFromAddress: tenant.String(), TransferToAddress: creditAddr.String(),
			})
			require.NoError(t, err)
			require.NoError(t, f.App.BankKeeper.SendCoins(f.Ctx, tenant, creditAddr, sdk.NewCoins(sdk.NewInt64Coin(denom, 1_000))))

			debit := func(from sdk.AccAddress, amount int64) error {
				if action == "burn" {
					_, debitErr := tf.Burn(f.Ctx, &tftypes.MsgBurn{
						Sender: denomAdmin.String(), Amount: sdk.NewInt64Coin(denom, amount), BurnFromAddress: from.String(),
					})
					return debitErr
				}
				_, debitErr := tf.ForceTransfer(f.Ctx, &tftypes.MsgForceTransfer{
					Sender: denomAdmin.String(), Amount: sdk.NewInt64Coin(denom, amount),
					TransferFromAddress: from.String(), TransferToAddress: denomAdmin.String(),
				})
				return debitErr
			}
			assertProtected := func() {
				t.Helper()
				for _, amount := range []int64{1, f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, denom).Amount.Int64()} {
					before := snapshotPayoutStores(t, f, f.Ctx)
					eventsBefore := len(f.Ctx.EventManager().Events())
					require.ErrorIs(t, debit(creditAddr, amount), types.ErrInvalidCreditOperation)
					require.Equal(t, before, snapshotPayoutStores(t, f, f.Ctx), "denied handler must not change bank or billing state even without a transaction cache")
					require.Len(t, f.Ctx.EventManager().Events(), eventsBefore)
				}
			}

			// An account with no leases is fully protected. Ordinary wallets keep
			// the issuer's existing burn-from and force-transfer policy.
			account, err := k.GetCreditAccount(f.Ctx, tenant.String())
			require.NoError(t, err)
			require.True(t, account.ReservedAmounts.IsZero())
			assertProtected()
			require.NoError(t, debit(tenant, 100))

			provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
			factorySKU := f.createTestSKUWithDenom(t, provider.Uuid, 3600, denom)
			ordinarySKU := f.createTestSKUWithDenom(t, provider.Uuid, 3600, testDenom)
			mixedLease := f.createAndAcknowledgeLease(t, ms, tenant, providerAddr, []types.LeaseItemInput{
				{SkuUuid: factorySKU.Uuid, Quantity: 1}, {SkuUuid: ordinarySKU.Uuid, Quantity: 1},
			})
			ordinaryLease := f.createAndAcknowledgeLease(t, ms, tenant, providerAddr, []types.LeaseItemInput{{SkuUuid: ordinarySKU.Uuid, Quantity: 1}})
			account, err = k.GetCreditAccount(f.Ctx, tenant.String())
			require.NoError(t, err)
			require.Equal(t, int64(3600), account.ReservedAmounts.AmountOf(denom).Int64())
			require.Equal(t, int64(7200), account.ReservedAmounts.AmountOf(testDenom).Int64())
			assertProtected()

			// A later administrator change must not remove account protection.
			_, err = tf.ChangeAdmin(f.Ctx, &tftypes.MsgChangeAdmin{Sender: denomAdmin.String(), Denom: denom, NewAdmin: nextAdmin.String()})
			require.NoError(t, err)
			denomAdmin = nextAdmin
			assertProtected()
			require.NoError(t, debit(tenant, 100))

			// Billing still settles and closes both mixed and independent leases
			// through its own bank dependency. The issuer can debit the payout
			// once it reaches an ordinary provider account.
			f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(time.Minute))
			_, err = ms.CloseLease(f.Ctx, &types.MsgCloseLease{Sender: tenant.String(), LeaseUuids: []string{mixedLease, ordinaryLease}})
			require.NoError(t, err)
			require.True(t, f.App.BankKeeper.GetBalance(f.Ctx, providerAddr, denom).IsPositive())
			require.True(t, f.App.BankKeeper.GetBalance(f.Ctx, providerAddr, testDenom).IsPositive())
			for _, leaseID := range []string{mixedLease, ordinaryLease} {
				lease, getErr := k.GetLease(f.Ctx, leaseID)
				require.NoError(t, getErr)
				require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
			}
			account, err = k.GetCreditAccount(f.Ctx, tenant.String())
			require.NoError(t, err)
			require.True(t, account.ReservedAmounts.IsZero())
			assertProtected()
			require.NoError(t, debit(providerAddr, 1))
			message, broken := keeper.ReservationAccountingInvariant(k)(f.Ctx)
			require.False(t, broken, message)
		})
	}
}

func TestTokenFactoryCreditProtectionUsesTransactionContext(t *testing.T) {
	f := initFixture(t)
	tenant, denomAdmin := f.TestAccs[0], f.TestAccs[1]
	tf := tfkeeper.NewMsgServerImpl(f.App.TokenFactoryKeeper)
	f.fundAccount(t, denomAdmin, f.App.TokenFactoryKeeper.GetParams(f.Ctx).DenomCreationFee)
	created, err := tf.CreateDenom(f.Ctx, &tftypes.MsgCreateDenom{Sender: denomAdmin.String(), Subdenom: "cachedcredit"})
	require.NoError(t, err)
	amount := sdk.NewInt64Coin(created.NewTokenDenom, 100)
	_, err = tf.Mint(f.Ctx, &tftypes.MsgMint{Sender: denomAdmin.String(), Amount: amount, MintToAddress: tenant.String()})
	require.NoError(t, err)
	creditAddr := types.DeriveCreditAddress(tenant)
	ms := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	// A funding message commits its own nested cache into the surrounding
	// transaction cache. Later messages in the transaction must see the new
	// protection, while a discarded transaction must leave no registration.
	for _, commit := range []bool{false, true} {
		ctx, write := f.Ctx.CacheContext()
		_, err = ms.FundCredit(ctx, &types.MsgFundCredit{Sender: tenant.String(), Tenant: tenant.String(), Amount: amount})
		require.NoError(t, err)
		registered, lookupErr := f.App.BillingKeeper.CreditAddressIndex.Has(f.Ctx, creditAddr)
		require.NoError(t, lookupErr)
		require.False(t, registered)
		before := snapshotPayoutStores(t, f, ctx)
		eventsBefore := len(ctx.EventManager().Events())
		_, err = tf.Burn(ctx, &tftypes.MsgBurn{Sender: denomAdmin.String(), Amount: amount, BurnFromAddress: creditAddr.String()})
		require.ErrorIs(t, err, types.ErrInvalidCreditOperation)
		_, err = tf.ForceTransfer(ctx, &tftypes.MsgForceTransfer{
			Sender: denomAdmin.String(), Amount: amount,
			TransferFromAddress: creditAddr.String(), TransferToAddress: denomAdmin.String(),
		})
		require.ErrorIs(t, err, types.ErrInvalidCreditOperation)
		require.Equal(t, before, snapshotPayoutStores(t, f, ctx))
		require.Len(t, ctx.EventManager().Events(), eventsBefore)
		if commit {
			write()
		}
		registered, lookupErr = f.App.BillingKeeper.CreditAddressIndex.Has(f.Ctx, creditAddr)
		require.NoError(t, lookupErr)
		require.Equal(t, commit, registered)
		require.Equal(t, commit, f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, amount.Denom).IsPositive())
	}
}
