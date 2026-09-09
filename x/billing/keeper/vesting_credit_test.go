package keeper_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"

	sdkcodec "github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/auth/vesting"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// Anyone can pre-create a vesting account at a future credit address. Billing
// must accept subsequent unlocked deposits while never reserving the lock.
func TestPermanentLockedCreditCanSettleAndClose(t *testing.T) {
	for _, leaseCount := range []int{1, 2} {
		t.Run(fmt.Sprintf("%d leases", leaseCount), func(t *testing.T) {
			f := initFixture(t)
			tenant, providerAddress, attacker := f.TestAccs[0], f.TestAccs[1], f.TestAccs[2]
			creditAddress := types.DeriveCreditAddress(tenant)
			f.fundAccount(t, attacker, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 1)))
			vestingServer := vesting.NewMsgServerImpl(f.App.AccountKeeper, f.App.BankKeeper)
			_, err := vestingServer.CreatePermanentLockedAccount(f.Ctx,
				vestingtypes.NewMsgCreatePermanentLockedAccount(attacker, creditAddress, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 1))))
			require.NoError(t, err)
			require.IsType(t, &vestingtypes.PermanentLockedAccount{}, f.App.AccountKeeper.GetAccount(f.Ctx, creditAddress))
			f.fundAccount(t, tenant, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 7_200)))
			k := f.App.BillingKeeper
			ms := keeper.NewMsgServerImpl(k)
			funded, err := ms.FundCredit(f.Ctx, &types.MsgFundCredit{
				Sender: tenant.String(), Tenant: tenant.String(), Amount: sdk.NewInt64Coin(testDenom, 7_200),
			})
			require.NoError(t, err)
			require.Equal(t, sdk.NewInt64Coin(testDenom, 7_200), funded.NewBalance)
			balance, err := k.GetCreditBalance(f.Ctx, tenant.String(), testDenom)
			require.NoError(t, err)
			require.Equal(t, funded.NewBalance, balance)
			credit, err := keeper.NewQuerier(k).CreditAccount(f.Ctx, &types.QueryCreditAccountRequest{Tenant: tenant.String()})
			require.NoError(t, err)
			require.Equal(t, sdk.NewCoins(balance), credit.Balances)
			require.Equal(t, credit.Balances, credit.AvailableBalances)

			provider := f.createTestProvider(t, providerAddress.String(), providerAddress.String())
			sku := f.createTestSKU(t, provider.Uuid, 3_600)
			leases := make([]string, 0, leaseCount)
			for range leaseCount {
				leases = append(leases, f.createAndAcknowledgeLease(t, ms, tenant, providerAddress,
					[]types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}}))
			}
			settleCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(3 * time.Hour))
			for index, uuid := range leases {
				withdrawable, err := k.CalculateWithdrawableForLease(settleCtx, mustGetLease(t, k, settleCtx, uuid))
				require.NoError(t, err)
				want := int64(7_200)
				if leaseCount == 2 {
					want = 3_600
				}
				require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(testDenom, want)), withdrawable)
				_, err = ms.CloseLease(settleCtx, &types.MsgCloseLease{Sender: tenant.String(), LeaseUuids: []string{uuid}})
				require.NoError(t, err)
				closed := mustGetLease(t, k, settleCtx, uuid)
				require.Equal(t, types.LEASE_STATE_CLOSED, closed.State)
				if index+1 < leaseCount {
					account, err := k.GetCreditAccount(settleCtx, tenant.String())
					require.NoError(t, err)
					require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 3_600)), account.ReservedAmounts)
				}
			}
			require.Equal(t, sdk.NewInt64Coin(testDenom, 7_200), f.App.BankKeeper.GetBalance(settleCtx, providerAddress, testDenom))
			require.Equal(t, sdk.NewInt64Coin(testDenom, 1), f.App.BankKeeper.GetBalance(settleCtx, creditAddress, testDenom))
			account, err := k.GetCreditAccount(settleCtx, tenant.String())
			require.NoError(t, err)
			require.Zero(t, account.ActiveLeaseCount)
			require.Empty(t, account.ReservedAmounts)
			message, broken := keeper.ReservationAccountingInvariant(k)(settleCtx)
			require.False(t, broken, message)
		})
	}
}

func TestPermanentLockedCreditDoesNotSatisfyAdmission(t *testing.T) {
	f := initFixture(t)
	tenant, providerAddress, attacker := f.TestAccs[0], f.TestAccs[1], f.TestAccs[2]
	creditAddress := types.DeriveCreditAddress(tenant)
	f.fundAccount(t, attacker, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 1)))
	_, err := vesting.NewMsgServerImpl(f.App.AccountKeeper, f.App.BankKeeper).CreatePermanentLockedAccount(f.Ctx,
		vestingtypes.NewMsgCreatePermanentLockedAccount(attacker, creditAddress, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 1))))
	require.NoError(t, err)
	f.fundAccount(t, tenant, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 3_600)))
	ms := keeper.NewMsgServerImpl(f.App.BillingKeeper)
	_, err = ms.FundCredit(f.Ctx, &types.MsgFundCredit{Sender: tenant.String(), Tenant: tenant.String(), Amount: sdk.NewInt64Coin(testDenom, 3_599)})
	require.NoError(t, err)
	provider := f.createTestProvider(t, providerAddress.String(), providerAddress.String())
	sku := f.createTestSKU(t, provider.Uuid, 3_600)
	_, err = ms.CreateLease(f.Ctx, &types.MsgCreateLease{Tenant: tenant.String(), Items: []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}}})
	require.ErrorIs(t, err, types.ErrInsufficientCredit)
	_, err = ms.FundCredit(f.Ctx, &types.MsgFundCredit{Sender: tenant.String(), Tenant: tenant.String(), Amount: sdk.NewInt64Coin(testDenom, 1)})
	require.NoError(t, err)
	f.createAndAcknowledgeLease(t, ms, tenant, providerAddress, []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}})
}

func TestCreditBalanceTracksVestingTimeAndPagination(t *testing.T) {
	for _, kind := range []string{"delayed", "continuous", "periodic"} {
		t.Run(kind, func(t *testing.T) {
			f := initFixture(t)
			tenant, donor := f.TestAccs[0], f.TestAccs[1]
			creditAddress := types.DeriveCreditAddress(tenant)
			locked := sdk.NewCoins(sdk.NewInt64Coin("ualpha", 10), sdk.NewInt64Coin("ubeta", 10))
			f.fundAccount(t, donor, locked)
			start := f.Ctx.BlockTime()
			vestingServer := vesting.NewMsgServerImpl(f.App.AccountKeeper, f.App.BankKeeper)
			var err error
			if kind == "periodic" {
				_, err = vestingServer.CreatePeriodicVestingAccount(f.Ctx, vestingtypes.NewMsgCreatePeriodicVestingAccount(donor, creditAddress, start.Unix(), []vestingtypes.Period{
					{Length: 10, Amount: sdk.NewCoins(sdk.NewInt64Coin("ualpha", 5), sdk.NewInt64Coin("ubeta", 5))},
					{Length: 10, Amount: sdk.NewCoins(sdk.NewInt64Coin("ualpha", 5), sdk.NewInt64Coin("ubeta", 5))},
				}))
			} else {
				_, err = vestingServer.CreateVestingAccount(f.Ctx, vestingtypes.NewMsgCreateVestingAccount(donor, creditAddress, locked, start.Add(20*time.Second).Unix(), kind == "delayed"))
			}
			require.NoError(t, err)
			// The second denom gets an ordinary unlocked deposit. A first page
			// consisting entirely of locked coins must still expose the next key.
			f.fundAccount(t, tenant, sdk.NewCoins(sdk.NewInt64Coin("ubeta", 3)))
			k := f.App.BillingKeeper
			_, err = keeper.NewMsgServerImpl(k).FundCredit(f.Ctx, &types.MsgFundCredit{Sender: tenant.String(), Tenant: tenant.String(), Amount: sdk.NewInt64Coin("ubeta", 3)})
			require.NoError(t, err)
			q := keeper.NewQuerier(k)
			page, err := q.CreditAccount(f.Ctx, &types.QueryCreditAccountRequest{Tenant: tenant.String(), Pagination: &query.PageRequest{Limit: 1}})
			require.NoError(t, err)
			require.Empty(t, page.Balances)
			require.NotEmpty(t, page.Pagination.NextKey)
			page, err = q.CreditAccount(f.Ctx, &types.QueryCreditAccountRequest{Tenant: tenant.String(), Pagination: &query.PageRequest{Limit: 1, Key: page.Pagination.NextKey}})
			require.NoError(t, err)
			require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("ubeta", 3)), page.Balances)
			for _, elapsed := range []time.Duration{0, 10 * time.Second, 20 * time.Second} {
				ctx := f.Ctx.WithBlockTime(start.Add(elapsed))
				for _, denom := range []string{"ualpha", "ubeta"} {
					balance, err := k.GetCreditBalance(ctx, tenant.String(), denom)
					require.NoError(t, err)
					require.True(t, f.App.BankKeeper.SpendableCoin(ctx, creditAddress, denom).IsEqual(balance))
				}
			}
		})
	}
}

// Both supported legacy normalization routes use the same spendable budget as
// preflight, including a real pre-created vesting account. Current state is
// deliberately rejected rather than silently reallocating sibling guarantees.
func TestVestingReservationMigrationPreflightAndGenesisParity(t *testing.T) {
	for _, state := range []types.LeaseState{types.LEASE_STATE_ACTIVE, types.LEASE_STATE_PENDING} {
		t.Run(state.String(), func(t *testing.T) {
			f := initFixture(t)
			tenant, providerAddress, donor := f.TestAccs[0], f.TestAccs[1], f.TestAccs[2]
			creditAddress := types.DeriveCreditAddress(tenant)
			f.fundAccount(t, donor, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 5)))
			_, err := vesting.NewMsgServerImpl(f.App.AccountKeeper, f.App.BankKeeper).CreatePermanentLockedAccount(f.Ctx,
				vestingtypes.NewMsgCreatePermanentLockedAccount(donor, creditAddress, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 5))))
			require.NoError(t, err)
			f.fundAccount(t, creditAddress, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 5)))
			provider := f.createTestProvider(t, providerAddress.String(), providerAddress.String())
			sku := f.createTestSKU(t, provider.Uuid, 10)
			genesis := &types.GenesisState{
				Params:        types.DefaultParams(),
				LeaseSequence: 1,
				Leases: []types.Lease{{
					Uuid:                       testLeaseUUID1,
					Tenant:                     tenant.String(),
					ProviderUuid:               provider.Uuid,
					Items:                      []types.LeaseItem{{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewInt64Coin(testDenom, 10)}},
					State:                      state,
					CreatedAt:                  f.Ctx.BlockTime(),
					LastSettledAt:              f.Ctx.BlockTime(),
					MinLeaseDurationAtCreation: 1,
				}},
				CreditAccounts: []types.CreditAccount{{Tenant: tenant.String(), CreditAddress: creditAddress.String(), ReservedAmounts: sdk.NewCoins(sdk.NewInt64Coin(testDenom, 10))}},
			}
			bankGenesis := banktypes.DefaultGenesisState()
			bankGenesis.Balances = []banktypes.Balance{{Address: creditAddress.String(), Coins: f.App.BankKeeper.GetAllBalances(f.Ctx, creditAddress)}}
			authAccounts := authtypes.GenesisAccounts{f.App.AccountKeeper.GetAccount(f.Ctx, creditAddress).(authtypes.GenesisAccount)}
			report, err := keeper.BuildReservationMigrationPreflight(f.Ctx.BlockTime(), genesis, bankGenesis, authAccounts)
			require.NoError(t, err)
			require.Equal(t, "10", report.Tenants[0].Denominations[0].BankBalance)
			require.Equal(t, "5", report.Tenants[0].Denominations[0].SpendableBalance)
			// Rehearse v2→v3→v4 in a discarded cache, then compare the genesis
			// normalization path against the same exported primary records.
			originalCtx := f.Ctx
			f.Ctx, _ = f.Ctx.CacheContext()
			require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, genesis.Leases[0]))
			overwriteLeaseWithLegacyEncoding(t, f, genesis.Leases[0])
			require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, genesis.CreditAccounts[0]))
			legacyAccount, err := sdkcodec.CollValue[types.CreditAccount](f.EncodingCfg.Codec).Encode(genesis.CreditAccounts[0])
			require.NoError(t, err)
			accountKey, err := collections.EncodeKeyWithPrefix(types.CreditAccountKey.Bytes(), sdk.AccAddressKey, tenant)
			require.NoError(t, err)
			f.Ctx.KVStore(f.App.GetKey(types.StoreKey)).Set(accountKey, legacyAccount)
			migrator := keeper.NewMigrator(f.App.BillingKeeper)
			require.NoError(t, migrator.Migrate2to3(f.Ctx))
			require.NoError(t, migrator.Migrate3to4(f.Ctx))
			migratedLease := mustGetLease(t, f.App.BillingKeeper, f.Ctx, testLeaseUUID1)
			f.Ctx = originalCtx
			require.NoError(t, f.App.BillingKeeper.InitGenesis(f.Ctx, genesis))
			lease := mustGetLease(t, f.App.BillingKeeper, f.Ctx, testLeaseUUID1)
			require.Equal(t, migratedLease, lease)
			if state == types.LEASE_STATE_PENDING {
				require.Equal(t, types.LEASE_STATE_EXPIRED, lease.State)
				require.Equal(t, uint64(1), report.ExpiringModernPendingLeaseCount)
			} else {
				require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 5)), lease.Reservation.RemainingAmounts)
				require.Equal(t, "5", report.Tenants[0].ModernActiveLeases[0].PlannedRemainingAmounts[0].Amount)
			}
			current := f.App.BillingKeeper.ExportGenesis(f.Ctx)
			if state == types.LEASE_STATE_ACTIVE {
				current.Leases[0].Reservation.RemainingAmounts = sdk.NewCoins(sdk.NewInt64Coin(testDenom, 10))
				current.CreditAccounts[0].ReservedAmounts = sdk.NewCoins(sdk.NewInt64Coin(testDenom, 10))
				_, err = keeper.BuildReservationMigrationPreflight(f.Ctx.BlockTime(), current, bankGenesis, authAccounts)
				require.ErrorContains(t, err, "under-backed")
				require.ErrorContains(t, f.App.BillingKeeper.InitGenesis(f.Ctx, current), "spendable bank balance")
			}
		})
	}
}

func mustGetLease(t *testing.T, k keeper.Keeper, ctx sdk.Context, uuid string) types.Lease {
	t.Helper()
	lease, err := k.GetLease(ctx, uuid)
	require.NoError(t, err)
	return lease
}
