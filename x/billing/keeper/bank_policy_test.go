package keeper_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
	skukeeper "github.com/manifest-network/manifest-ledger/x/sku/keeper"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

func TestLeaseAdmissionRejectsHistoricalBlockedPayoutBeforeWrites(t *testing.T) {
	for _, role := range []string{"tenant", "authority", "allowed list"} {
		t.Run(role, func(t *testing.T) {
			setup := newAcknowledgementTestSetup(t, 1, func(*types.Params) {})
			f := setup.f
			sku, err := f.App.SKUKeeper.GetSKU(f.Ctx, setup.skuUUID)
			require.NoError(t, err)
			provider, err := f.App.SKUKeeper.GetProvider(f.Ctx, sku.ProviderUuid)
			require.NoError(t, err)
			payout := provider.PayoutAddress
			provider.PayoutAddress = authtypes.NewModuleAddress(distrtypes.ModuleName).String()
			require.NoError(t, f.App.SKUKeeper.SetProvider(f.Ctx, provider), "model a historically accepted provider")

			sender := f.Authority
			if role == "allowed list" {
				sender = f.TestAccs[3]
				params, err := f.App.BillingKeeper.GetParams(f.Ctx)
				require.NoError(t, err)
				params.AllowedList = []string{sender.String()}
				require.NoError(t, f.App.BillingKeeper.SetParams(f.Ctx, params))
			}
			ctx := f.Ctx.WithEventManager(sdk.NewEventManager())
			// Snapshot primary records, derived indexes and UUID sequences, plus
			// bank state, so rejected admission cannot leave hidden side effects.
			snapshot := func() map[string]map[string][]byte {
				stores := make(map[string]map[string][]byte)
				for _, name := range []string{types.StoreKey, skutypes.StoreKey, banktypes.StoreKey} {
					iter := ctx.KVStore(f.App.GetKey(name)).Iterator(nil, nil)
					values := make(map[string][]byte)
					for ; iter.Valid(); iter.Next() {
						values[string(iter.Key())] = bytes.Clone(iter.Value())
					}
					require.NoError(t, iter.Close())
					stores[name] = values
				}
				return stores
			}
			create := func() (string, error) {
				items := []types.LeaseItemInput{{SkuUuid: setup.skuUUID, Quantity: 1}}
				if role == "tenant" {
					response, err := setup.msgServer.CreateLease(ctx, &types.MsgCreateLease{Tenant: setup.tenants[0].String(), Items: items})
					if err != nil {
						return "", err
					}
					return response.LeaseUuid, nil
				}
				response, err := setup.msgServer.CreateLeaseForTenant(ctx, &types.MsgCreateLeaseForTenant{
					Authority: sender.String(), Tenant: setup.tenants[0].String(), Items: items,
				})
				if err != nil {
					return "", err
				}
				return response.LeaseUuid, nil
			}
			before := snapshot()

			leaseUUID, err := create()

			require.ErrorIs(t, err, types.ErrInvalidCreditOperation)
			require.ErrorContains(t, err, "blocked from receiving funds")
			require.Empty(t, leaseUUID)
			require.Equal(t, before, snapshot())
			require.Empty(t, ctx.EventManager().Events())

			// Repair through the supported provider update, then retry admission.
			f.App.SKUKeeper.SetAuthority(f.Authority.String())
			_, err = skukeeper.NewMsgServerImpl(f.App.SKUKeeper).UpdateProvider(ctx, &skutypes.MsgUpdateProvider{
				Authority: f.Authority.String(), Uuid: provider.Uuid, Address: provider.Address,
				PayoutAddress: payout, Active: true,
			})
			require.NoError(t, err)
			leaseUUID, err = create()
			require.NoError(t, err)
			lease, err := f.App.BillingKeeper.GetLease(ctx, leaseUUID)
			require.NoError(t, err)
			require.Equal(t, types.LEASE_STATE_PENDING, lease.State)
			message, broken := keeper.ReservationAccountingInvariant(f.App.BillingKeeper)(ctx)
			require.False(t, broken, message)
		})
	}
}

func TestFundCreditHonorsBankSendRestrictions(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, defaultDisabled := range []bool{false, true} {
			name := "new account/denom disabled"
			if existing {
				name = "existing account/denom disabled"
			}
			if defaultDisabled {
				name += " by default"
			}
			t.Run(name, func(t *testing.T) {
				f := initFixture(t)
				msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)
				sender, tenant := f.TestAccs[0], f.TestAccs[1]
				creditAddress := types.DeriveCreditAddress(tenant)
				amount := sdk.NewInt64Coin(testDenom, 100)
				f.fundAccount(t, sender, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 1000)))
				msg := &types.MsgFundCredit{Sender: sender.String(), Tenant: tenant.String(), Amount: amount}
				if existing {
					_, err := msgServer.FundCredit(f.Ctx, msg)
					require.NoError(t, err)
				}
				if defaultDisabled {
					params := f.App.BankKeeper.GetParams(f.Ctx)
					params.DefaultSendEnabled = false
					require.NoError(t, f.App.BankKeeper.SetParams(f.Ctx, params))
				} else {
					f.App.BankKeeper.SetSendEnabled(f.Ctx, testDenom, false)
				}
				accountBefore, accountErrBefore := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
				authBefore := f.App.AccountKeeper.GetAccount(f.Ctx, creditAddress)
				senderBefore := f.App.BankKeeper.GetAllBalances(f.Ctx, sender)
				creditBefore := f.App.BankKeeper.GetAllBalances(f.Ctx, creditAddress)
				ctx := f.Ctx.WithEventManager(sdk.NewEventManager())

				response, err := msgServer.FundCredit(ctx, msg)

				require.ErrorIs(t, err, banktypes.ErrSendDisabled)
				require.Nil(t, response)
				accountAfter, accountErrAfter := f.App.BillingKeeper.GetCreditAccount(ctx, tenant.String())
				require.Equal(t, accountBefore, accountAfter)
				if existing {
					require.NoError(t, accountErrBefore)
					require.NoError(t, accountErrAfter)
				} else {
					require.ErrorIs(t, accountErrBefore, types.ErrCreditAccountNotFound)
					require.ErrorIs(t, accountErrAfter, types.ErrCreditAccountNotFound)
				}
				hasIndex, err := f.App.BillingKeeper.CreditAddressIndex.Has(ctx, creditAddress)
				require.NoError(t, err)
				require.Equal(t, existing, hasIndex)
				require.Equal(t, authBefore, f.App.AccountKeeper.GetAccount(ctx, creditAddress))
				require.Equal(t, senderBefore, f.App.BankKeeper.GetAllBalances(ctx, sender))
				require.Equal(t, creditBefore, f.App.BankKeeper.GetAllBalances(ctx, creditAddress))
				require.Empty(t, ctx.EventManager().Events())

				// An explicit enable also overrides DefaultSendEnabled=false.
				f.App.BankKeeper.SetSendEnabled(ctx, testDenom, true)
				response, err = msgServer.FundCredit(ctx, msg)
				require.NoError(t, err)
				require.Equal(t, creditBefore.AmountOf(testDenom).Add(amount.Amount), response.NewBalance.Amount)
				require.Equal(t, senderBefore.Sub(amount), f.App.BankKeeper.GetAllBalances(ctx, sender))
			})
		}
	}
}

func TestDisabledDepositDenomDoesNotPreventSettlement(t *testing.T) {
	setup := newAcknowledgementTestSetup(t, 1, func(*types.Params) {})
	f := setup.f
	leaseUUID := f.createAndAcknowledgeLease(t, setup.msgServer, setup.tenants[0], setup.providerAddr,
		[]types.LeaseItemInput{{SkuUuid: setup.skuUUID, Quantity: 1}})
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseUUID)
	require.NoError(t, err)
	provider, err := f.App.SKUKeeper.GetProvider(f.Ctx, lease.ProviderUuid)
	require.NoError(t, err)
	payoutAddress, err := sdk.AccAddressFromBech32(provider.PayoutAddress)
	require.NoError(t, err)
	f.App.BankKeeper.SetSendEnabled(f.Ctx, testDenom, false)
	ctx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(time.Second))

	response, err := setup.msgServer.Withdraw(ctx, &types.MsgWithdraw{
		Sender: setup.providerAddr.String(), LeaseUuids: []string{leaseUUID},
	})

	require.NoError(t, err)
	require.Empty(t, response.FailedLeaseUuids)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 1)), response.TotalAmounts)
	require.Equal(t, response.TotalAmounts, f.App.BankKeeper.GetAllBalances(ctx, payoutAddress))
	message, broken := keeper.ReservationAccountingInvariant(f.App.BillingKeeper)(ctx)
	require.False(t, broken, message)
}

func TestSettlementRejectsBlockedPayoutWithoutMutation(t *testing.T) {
	for _, silent := range []bool{false, true} {
		name := "strict"
		if silent {
			name = "silent"
		}
		t.Run(name, func(t *testing.T) {
			setup := newAcknowledgementTestSetup(t, 1, func(*types.Params) {})
			f := setup.f
			leaseUUID := f.createAndAcknowledgeLease(t, setup.msgServer, setup.tenants[0], setup.providerAddr,
				[]types.LeaseItemInput{{SkuUuid: setup.skuUUID, Quantity: 1}})
			leaseBefore, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseUUID)
			require.NoError(t, err)
			accountBefore, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, leaseBefore.Tenant)
			require.NoError(t, err)
			provider, err := f.App.SKUKeeper.GetProvider(f.Ctx, leaseBefore.ProviderUuid)
			require.NoError(t, err)
			blocked := authtypes.NewModuleAddress(distrtypes.ModuleName)
			require.True(t, f.App.BankKeeper.BlockedAddr(blocked))
			// Model a provider already stored by an older binary. New provider
			// messages reject this address before it can be persisted.
			provider.PayoutAddress = blocked.String()
			require.NoError(t, f.App.SKUKeeper.SetProvider(f.Ctx, provider))
			creditAddress := types.DeriveCreditAddress(setup.tenants[0])
			balanceBefore := f.App.BankKeeper.GetAllBalances(f.Ctx, creditAddress)
			blockedBefore := f.App.BankKeeper.GetAllBalances(f.Ctx, blocked)
			lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseUUID)
			require.NoError(t, err)
			account, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, lease.Tenant)
			require.NoError(t, err)
			ctx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(time.Second)).WithEventManager(sdk.NewEventManager())
			settle := f.App.BillingKeeper.PerformSettlement
			if silent {
				settle = f.App.BillingKeeper.PerformSettlementSilent
			}

			result, err := settle(ctx, &lease, &account, ctx.BlockTime())

			require.ErrorIs(t, err, types.ErrInvalidCreditOperation)
			require.ErrorContains(t, err, "blocked from receiving funds")
			require.Nil(t, result)
			require.Equal(t, leaseBefore, lease, "failed transfer must not consume the in-memory lease reservation")
			require.Equal(t, accountBefore, account, "failed transfer must not consume the in-memory account reservation")
			require.Equal(t, balanceBefore, f.App.BankKeeper.GetAllBalances(ctx, creditAddress))
			require.Equal(t, blockedBefore, f.App.BankKeeper.GetAllBalances(ctx, blocked))
			require.Empty(t, ctx.EventManager().Events())
			message, broken := distrkeeper.ModuleAccountInvariant(f.App.DistrKeeper)(ctx)
			require.False(t, broken, message)
		})
	}
}

func TestBlockedPayoutCanBeRepairedWithoutLosingAccrual(t *testing.T) {
	tests := []struct {
		name     string
		elapsed  time.Duration
		close    bool
		provider bool
	}{
		{name: "specific withdrawal", elapsed: time.Second},
		{name: "provider withdrawal", elapsed: time.Second, provider: true},
		{name: "tenant close", elapsed: time.Second, close: true},
		{name: "auto close", elapsed: 2_000_000_000 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup := newAcknowledgementTestSetup(t, 1, func(*types.Params) {})
			f := setup.f
			leaseUUID := f.createAndAcknowledgeLease(t, setup.msgServer, setup.tenants[0], setup.providerAddr,
				[]types.LeaseItemInput{{SkuUuid: setup.skuUUID, Quantity: 1}})
			leaseBefore, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseUUID)
			require.NoError(t, err)
			accountBefore, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, leaseBefore.Tenant)
			require.NoError(t, err)
			provider, err := f.App.SKUKeeper.GetProvider(f.Ctx, leaseBefore.ProviderUuid)
			require.NoError(t, err)
			payout := provider.PayoutAddress
			blocked := authtypes.NewModuleAddress(distrtypes.ModuleName)
			provider.PayoutAddress = blocked.String()
			require.NoError(t, f.App.SKUKeeper.SetProvider(f.Ctx, provider))
			creditAddress := types.DeriveCreditAddress(setup.tenants[0])
			balanceBefore := f.App.BankKeeper.GetAllBalances(f.Ctx, creditAddress)
			blockedBefore := f.App.BankKeeper.GetAllBalances(f.Ctx, blocked)
			ctx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(tt.elapsed)).WithEventManager(sdk.NewEventManager())
			perform := func(ctx context.Context) (*types.MsgWithdrawResponse, error) {
				if tt.close {
					_, err := setup.msgServer.CloseLease(ctx, &types.MsgCloseLease{
						Sender: setup.tenants[0].String(), LeaseUuids: []string{leaseUUID},
					})
					return nil, err
				}
				msg := &types.MsgWithdraw{Sender: setup.providerAddr.String(), LeaseUuids: []string{leaseUUID}}
				if tt.provider {
					msg.LeaseUuids = nil
					msg.ProviderUuid = provider.Uuid
				}
				return setup.msgServer.Withdraw(ctx, msg)
			}

			response, err := perform(ctx)
			if tt.provider {
				require.NoError(t, err, "provider-wide withdrawal reports per-lease failures in its successful response")
				require.Equal(t, []string{leaseUUID}, response.FailedLeaseUuids)
				require.Zero(t, response.WithdrawalCount)
				require.True(t, response.TotalAmounts.IsZero())
			} else {
				require.ErrorIs(t, err, types.ErrInvalidCreditOperation)
			}
			leaseAfter, err := f.App.BillingKeeper.GetLease(ctx, leaseUUID)
			require.NoError(t, err)
			accountAfter, err := f.App.BillingKeeper.GetCreditAccount(ctx, leaseBefore.Tenant)
			require.NoError(t, err)
			require.Equal(t, leaseBefore, leaseAfter)
			require.Equal(t, accountBefore, accountAfter)
			require.Equal(t, balanceBefore, f.App.BankKeeper.GetAllBalances(ctx, creditAddress))
			require.Equal(t, blockedBefore, f.App.BankKeeper.GetAllBalances(ctx, blocked))
			if !tt.provider {
				require.Empty(t, ctx.EventManager().Events())
			}
			message, broken := distrkeeper.ModuleAccountInvariant(f.App.DistrKeeper)(ctx)
			require.False(t, broken, message)

			// Repair the historical configuration through the supported message
			// path, then retry the original operation at the same block time.
			f.App.SKUKeeper.SetAuthority(f.Authority.String())
			_, err = skukeeper.NewMsgServerImpl(f.App.SKUKeeper).UpdateProvider(ctx, &skutypes.MsgUpdateProvider{
				Authority: f.Authority.String(), Uuid: provider.Uuid, Address: provider.Address,
				PayoutAddress: payout, Active: true,
			})
			require.NoError(t, err)
			response, err = perform(ctx)
			require.NoError(t, err)
			if response != nil {
				require.Empty(t, response.FailedLeaseUuids)
			}
			payoutAddress, err := sdk.AccAddressFromBech32(payout)
			require.NoError(t, err)
			expected := sdk.NewCoins(sdk.NewInt64Coin(testDenom, 1))
			if tt.elapsed > time.Second {
				expected = balanceBefore
			}
			require.Equal(t, expected, f.App.BankKeeper.GetAllBalances(ctx, payoutAddress))
			require.True(t, balanceBefore.Sub(expected...).Equal(f.App.BankKeeper.GetAllBalances(ctx, creditAddress)))
			message, broken = keeper.ReservationAccountingInvariant(f.App.BillingKeeper)(ctx)
			require.False(t, broken, message)
		})
	}
}
