package keeper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestInitGenesisNormalizesAggregateReservationsWithMigrationPolicy(t *testing.T) {
	f := initFixture(t)
	tenant := f.TestAccs[0]
	providerAddress := f.TestAccs[1]
	provider := f.createTestProvider(t, providerAddress.String(), providerAddress.String())
	sku := f.createTestSKU(t, provider.Uuid, 1)
	creditAddress := types.DeriveCreditAddress(tenant)
	now := f.Ctx.BlockTime()

	const (
		pendingUUID  = "01912345-6789-7abc-8def-0123456789a0"
		terminalUUID = "01912345-6789-7abc-8def-0123456789b0"
		legacyUUID   = "01912345-6789-7abc-8def-0123456789b1"
	)
	lease := func(uuid string, state types.LeaseState, amount int64, minimum uint64) types.Lease {
		return types.Lease{
			Uuid:         uuid,
			Tenant:       tenant.String(),
			ProviderUuid: provider.Uuid,
			Items: []types.LeaseItem{{
				SkuUuid:     sku.Uuid,
				Quantity:    1,
				LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(amount)),
			}},
			State:                      state,
			CreatedAt:                  now,
			LastSettledAt:              now,
			MinLeaseDurationAtCreation: minimum,
		}
	}

	// Deliberately put equal ACTIVE claims in reverse UUID order. The old
	// aggregate of 10 comprises exact PENDING 2, modern ACTIVE claims 3+3,
	// and an opaque legacy cohort claim of 2. With only 7 bank-backed, the
	// remaining 5 is allocated 2:2:1 after funding PENDING first.
	genesis := &types.GenesisState{
		Params: types.DefaultParams(),
		Leases: []types.Lease{
			lease(testLeaseUUID2, types.LEASE_STATE_ACTIVE, 3, 1),
			lease(legacyUUID, types.LEASE_STATE_ACTIVE, 1, 0),
			lease(terminalUUID, types.LEASE_STATE_REJECTED, 1, 1),
			lease(pendingUUID, types.LEASE_STATE_PENDING, 2, 1),
			lease(testLeaseUUID1, types.LEASE_STATE_ACTIVE, 3, 1),
		},
		CreditAccounts: []types.CreditAccount{{
			Tenant:            tenant.String(),
			CreditAddress:     creditAddress.String(),
			ActiveLeaseCount:  3,
			PendingLeaseCount: 1,
			ReservedAmounts: sdk.NewCoins(
				sdk.NewCoin(testDenom, sdkmath.NewInt(10)),
			),
		}},
		LeaseSequence: 5,
	}
	require.NoError(t, genesis.Validate())

	bankBalance := sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(7)))
	f.fundAccount(t, creditAddress, bankBalance)
	bankBefore := f.App.BankKeeper.GetAllBalances(f.Ctx, creditAddress)
	supplyBefore := f.App.BankKeeper.GetSupply(f.Ctx, testDenom)

	require.NoError(t, f.App.BillingKeeper.InitGenesis(f.Ctx, genesis))
	require.True(t, bankBefore.Equal(f.App.BankKeeper.GetAllBalances(f.Ctx, creditAddress)))
	require.Equal(t, supplyBefore, f.App.BankKeeper.GetSupply(f.Ctx, testDenom))

	assertRemaining := func(uuid string, amount int64) {
		t.Helper()
		stored, err := f.App.BillingKeeper.GetLease(f.Ctx, uuid)
		require.NoError(t, err)
		require.NotNil(t, stored.Reservation)
		require.Equal(t, sdkmath.NewInt(amount), stored.Reservation.RemainingAmounts.AmountOf(testDenom))
	}
	assertRemaining(pendingUUID, 2)
	assertRemaining(testLeaseUUID1, 2)
	assertRemaining(testLeaseUUID2, 2)
	assertRemaining(legacyUUID, 0)
	assertRemaining(terminalUUID, 0)

	account, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(7), account.ReservedAmounts.AmountOf(testDenom))
	require.Equal(t, sdkmath.OneInt(), account.UnattributedReservedAmounts.AmountOf(testDenom))
	require.Equal(t, uint64(1), account.UnattributedLeaseCount)

	exported := f.App.BillingKeeper.ExportGenesis(f.Ctx)
	require.NoError(t, exported.Validate())
	legacy, err := exported.HasLegacyReservationState()
	require.NoError(t, err)
	require.False(t, legacy)

	exported.CreditAccounts[0].UnattributedLeaseCount = 0
	require.ErrorContains(t, exported.Validate(), "has 1 live legacy leases")
}

func TestInitGenesisRepairsLegacyAggregateBelowModernPendingFloor(t *testing.T) {
	f := initFixture(t)
	tenant := f.TestAccs[0]
	providerAddress := f.TestAccs[1]
	provider := f.createTestProvider(t, providerAddress.String(), providerAddress.String())
	sku := f.createTestSKU(t, provider.Uuid, 1)
	creditAddress := types.DeriveCreditAddress(tenant)
	now := f.Ctx.BlockTime()
	const (
		pendingUUID = "01912345-6789-7abc-8def-0123456789a0"
		legacyUUID  = "01912345-6789-7abc-8def-0123456789b0"
	)

	pending := types.Lease{
		Uuid:         pendingUUID,
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{{
			SkuUuid: sku.Uuid, Quantity: 1,
			LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(5)),
		}},
		State: types.LEASE_STATE_PENDING, CreatedAt: now, LastSettledAt: now,
		MinLeaseDurationAtCreation: 1,
	}
	legacy := types.Lease{
		Uuid:         legacyUUID,
		Tenant:       tenant.String(),
		ProviderUuid: provider.Uuid,
		Items: []types.LeaseItem{{
			SkuUuid: sku.Uuid, Quantity: 1,
			LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(2)),
		}},
		State:         types.LEASE_STATE_ACTIVE,
		CreatedAt:     now,
		LastSettledAt: now,
		// A zero stored creation duration makes this an opaque pre-v4 lease.
		MinLeaseDurationAtCreation: 0,
	}
	genesis := &types.GenesisState{
		Params: types.DefaultParams(),
		Leases: []types.Lease{legacy, pending},
		CreditAccounts: []types.CreditAccount{{
			Tenant:            tenant.String(),
			CreditAddress:     creditAddress.String(),
			ActiveLeaseCount:  1,
			PendingLeaseCount: 1,
			// A historical legacy release could clamp this below the modern
			// PENDING floor. Import repair must raise one to the provable five.
			ReservedAmounts: sdk.NewCoins(
				sdk.NewCoin(testDenom, sdkmath.OneInt()),
			),
		}},
		LeaseSequence: 2,
	}
	paramsBefore := genesis.Params
	leasesBefore := append([]types.Lease(nil), genesis.Leases...)
	accountsBefore := append([]types.CreditAccount(nil), genesis.CreditAccounts...)

	f.fundAccount(t, creditAddress, sdk.NewCoins(
		sdk.NewCoin(testDenom, sdkmath.NewInt(5)),
	))
	bankBefore := f.App.BankKeeper.GetAllBalances(f.Ctx, creditAddress)
	require.NoError(t, genesis.Validate())
	require.ErrorIs(t, genesis.ValidateStrict(), types.ErrInvalidCreditOperation)
	require.NoError(t, f.App.BillingKeeper.InitGenesis(f.Ctx, genesis))
	require.Equal(t, paramsBefore, genesis.Params)
	require.Equal(t, leasesBefore, genesis.Leases)
	require.Equal(t, accountsBefore, genesis.CreditAccounts,
		"import repair must not mutate the exported aggregate in its source value")
	require.True(t, bankBefore.Equal(f.App.BankKeeper.GetAllBalances(f.Ctx, creditAddress)))

	pendingAfter, err := f.App.BillingKeeper.GetLease(f.Ctx, pendingUUID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_PENDING, pendingAfter.State)
	require.Nil(t, pendingAfter.ExpiredAt)
	require.NotNil(t, pendingAfter.Reservation)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(5))),
		pendingAfter.Reservation.RemainingAmounts)
	legacyAfter, err := f.App.BillingKeeper.GetLease(f.Ctx, legacyUUID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, legacyAfter.State)
	require.NotNil(t, legacyAfter.Reservation)
	require.True(t, legacyAfter.Reservation.RemainingAmounts.IsZero())

	account, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(1), account.ActiveLeaseCount)
	require.Equal(t, uint64(1), account.PendingLeaseCount)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(5))), account.ReservedAmounts)
	require.True(t, account.UnattributedReservedAmounts.IsZero())
	require.Equal(t, uint64(1), account.UnattributedLeaseCount)
	require.NoError(t, f.App.BillingKeeper.ExportGenesis(f.Ctx).Validate())
}

func TestInitGenesisExpiresAllModernPendingWhenOneDenomIsUnderbacked(t *testing.T) {
	f := initFixture(t)
	tenant := f.TestAccs[0]
	providerAddress := f.TestAccs[1]
	provider := f.createTestProvider(t, providerAddress.String(), providerAddress.String())
	sku := f.createTestSKU(t, provider.Uuid, 1)
	creditAddress := types.DeriveCreditAddress(tenant)
	now := f.Ctx.BlockTime()
	const (
		pendingUUID1 = "01912345-6789-7abc-8def-0123456789a0"
		pendingUUID2 = "01912345-6789-7abc-8def-0123456789a1"
	)

	lease := func(uuid string, state types.LeaseState, denom string, amount int64) types.Lease {
		return types.Lease{
			Uuid:         uuid,
			Tenant:       tenant.String(),
			ProviderUuid: provider.Uuid,
			Items: []types.LeaseItem{{
				SkuUuid:     sku.Uuid,
				Quantity:    1,
				LockedPrice: sdk.NewCoin(denom, sdkmath.NewInt(amount)),
			}},
			State:                      state,
			CreatedAt:                  now,
			LastSettledAt:              now,
			MinLeaseDurationAtCreation: 1,
		}
	}
	active := lease(testLeaseUUID1, types.LEASE_STATE_ACTIVE, testDenom, 3)
	pending1 := lease(pendingUUID1, types.LEASE_STATE_PENDING, testDenom, 5)
	pending2 := lease(pendingUUID2, types.LEASE_STATE_PENDING, testDenom2, 7)
	genesis := &types.GenesisState{
		Params: types.DefaultParams(),
		// Reverse UUID order exercises the shared planner's canonical ordering;
		// the all-or-none decision must not depend on genesis slice order.
		Leases: []types.Lease{pending2, active, pending1},
		CreditAccounts: []types.CreditAccount{{
			Tenant:            tenant.String(),
			CreditAddress:     creditAddress.String(),
			ActiveLeaseCount:  1,
			PendingLeaseCount: 2,
			ReservedAmounts: sdk.NewCoins(
				sdk.NewCoin(testDenom, sdkmath.NewInt(8)),
				sdk.NewCoin(testDenom2, sdkmath.NewInt(7)),
			),
		}},
		LeaseSequence: 3,
	}
	genesisBefore := *genesis
	genesisBefore.Params = genesis.Params
	if genesis.Params.AllowedList != nil {
		genesisBefore.Params.AllowedList = append([]string{}, genesis.Params.AllowedList...)
	}
	if genesis.Params.ReservedDomainSuffixes != nil {
		genesisBefore.Params.ReservedDomainSuffixes = append(
			[]string{},
			genesis.Params.ReservedDomainSuffixes...,
		)
	}
	genesisBefore.Leases = append([]types.Lease(nil), genesis.Leases...)
	for index := range genesisBefore.Leases {
		genesisBefore.Leases[index].Items = append(
			[]types.LeaseItem(nil),
			genesis.Leases[index].Items...,
		)
	}
	genesisBefore.CreditAccounts = append([]types.CreditAccount(nil), genesis.CreditAccounts...)
	for index := range genesisBefore.CreditAccounts {
		genesisBefore.CreditAccounts[index].ReservedAmounts = append(
			sdk.Coins(nil),
			genesis.CreditAccounts[index].ReservedAmounts...,
		)
	}
	require.NoError(t, genesis.Validate())

	f.fundAccount(t, creditAddress, sdk.NewCoins(
		sdk.NewCoin(testDenom, sdkmath.NewInt(8)),
		sdk.NewCoin(testDenom2, sdkmath.NewInt(6)),
	))
	bankBefore := f.App.BankKeeper.GetAllBalances(f.Ctx, creditAddress)
	require.NoError(t, f.App.BillingKeeper.InitGenesis(f.Ctx, genesis))
	require.Equal(t, &genesisBefore, genesis, "InitGenesis must not mutate its input")
	require.True(t, bankBefore.Equal(f.App.BankKeeper.GetAllBalances(f.Ctx, creditAddress)),
		"genesis cutover must not mint, burn, or transfer bank balances")

	for _, uuid := range []string{pendingUUID1, pendingUUID2} {
		stored, err := f.App.BillingKeeper.GetLease(f.Ctx, uuid)
		require.NoError(t, err)
		require.Equal(t, types.LEASE_STATE_EXPIRED, stored.State)
		require.NotNil(t, stored.ExpiredAt)
		require.Equal(t, now, *stored.ExpiredAt)
		require.NotNil(t, stored.Reservation)
		require.True(t, stored.Reservation.RemainingAmounts.IsZero())
	}
	activeAfter, err := f.App.BillingKeeper.GetLease(f.Ctx, active.Uuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, activeAfter.State)
	require.NotNil(t, activeAfter.Reservation)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(3))),
		activeAfter.Reservation.RemainingAmounts)

	account, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, uint64(1), account.ActiveLeaseCount)
	require.Zero(t, account.PendingLeaseCount)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(3))), account.ReservedAmounts)
	require.True(t, account.UnattributedReservedAmounts.IsZero())
	require.Zero(t, account.UnattributedLeaseCount)

	exported := f.App.BillingKeeper.ExportGenesis(f.Ctx)
	require.NoError(t, exported.Validate())
	require.NoError(t, exported.ValidateWithBlockTime(now))
}

func TestInitGenesisRejectsUnderbackedConsumableReservationsBeforeWrites(t *testing.T) {
	f := initFixture(t)
	tenant := f.TestAccs[0]
	providerAddress := f.TestAccs[1]
	provider := f.createTestProvider(t, providerAddress.String(), providerAddress.String())
	sku := f.createTestSKU(t, provider.Uuid, 5)
	creditAddress := types.DeriveCreditAddress(tenant)
	now := f.Ctx.BlockTime()
	reservation := sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(5)))

	paramsBefore, err := f.App.BillingKeeper.GetParams(f.Ctx)
	require.NoError(t, err)
	sequenceBefore, err := f.App.BillingKeeper.LeaseSequence.Peek(f.Ctx)
	require.NoError(t, err)
	genesisParams := paramsBefore
	genesisParams.MaxLeasesPerTenant++
	genesis := &types.GenesisState{
		Params: genesisParams,
		Leases: []types.Lease{{
			Uuid:         testLeaseUUID1,
			Tenant:       tenant.String(),
			ProviderUuid: provider.Uuid,
			Items: []types.LeaseItem{{
				SkuUuid:     sku.Uuid,
				Quantity:    1,
				LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(5)),
			}},
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  now,
			LastSettledAt:              now,
			MinLeaseDurationAtCreation: 1,
			Reservation: &types.LeaseReservation{
				RemainingAmounts: reservation,
			},
		}},
		CreditAccounts: []types.CreditAccount{{
			Tenant:           tenant.String(),
			CreditAddress:    creditAddress.String(),
			ActiveLeaseCount: 1,
			ReservedAmounts:  reservation,
		}},
		LeaseSequence: 1,
	}
	require.NoError(t, genesis.Validate())
	require.NoError(t, genesis.ValidateWithBlockTime(now))
	f.fundAccount(t, creditAddress, sdk.NewCoins(
		sdk.NewCoin(testDenom, sdkmath.NewInt(4)),
	))

	err = f.App.BillingKeeper.InitGenesis(f.Ctx, genesis)
	require.ErrorIs(t, err, types.ErrReservationInvariant)
	require.ErrorContains(t, err, "bank balance")

	paramsAfter, err := f.App.BillingKeeper.GetParams(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, paramsBefore, paramsAfter)
	sequenceAfter, err := f.App.BillingKeeper.LeaseSequence.Peek(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, sequenceBefore, sequenceAfter)
	_, err = f.App.BillingKeeper.GetLease(f.Ctx, testLeaseUUID1)
	require.ErrorIs(t, err, types.ErrLeaseNotFound)
	_, err = f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.ErrorIs(t, err, types.ErrCreditAccountNotFound)
}

func TestInitGenesisRejectsMixedReservationFormatsBeforeWrites(t *testing.T) {
	f := initFixture(t)
	tenant := f.TestAccs[0]
	now := f.Ctx.BlockTime()
	paramsBefore, err := f.App.BillingKeeper.GetParams(f.Ctx)
	require.NoError(t, err)
	genesisParams := paramsBefore
	genesisParams.MaxLeasesPerTenant++

	terminalLease := func(uuid string) types.Lease {
		return types.Lease{
			Uuid:         uuid,
			Tenant:       tenant.String(),
			ProviderUuid: testProviderUUID,
			Items: []types.LeaseItem{{
				SkuUuid:     testSKUUUID,
				Quantity:    1,
				LockedPrice: sdk.NewCoin(testDenom, sdkmath.OneInt()),
			}},
			State:                      types.LEASE_STATE_REJECTED,
			CreatedAt:                  now,
			LastSettledAt:              now,
			MinLeaseDurationAtCreation: 1,
		}
	}
	legacy := terminalLease(testLeaseUUID1)
	consumable := terminalLease(testLeaseUUID2)
	consumable.Reservation = &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()}
	genesis := &types.GenesisState{
		Params:         genesisParams,
		Leases:         []types.Lease{legacy, consumable},
		CreditAccounts: []types.CreditAccount{},
		LeaseSequence:  2,
	}

	err = f.App.BillingKeeper.InitGenesis(f.Ctx, genesis)
	require.ErrorIs(t, err, types.ErrReservationInvariant)
	require.ErrorContains(t, err, "mixes leases with legacy and consumable reservation state")
	paramsAfter, err := f.App.BillingKeeper.GetParams(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, paramsBefore, paramsAfter)
	_, err = f.App.BillingKeeper.GetLease(f.Ctx, legacy.Uuid)
	require.ErrorIs(t, err, types.ErrLeaseNotFound)
	_, err = f.App.BillingKeeper.GetLease(f.Ctx, consumable.Uuid)
	require.ErrorIs(t, err, types.ErrLeaseNotFound)
}

func TestExportGenesisAfterPartialSettlementValidatesAndReimports(t *testing.T) {
	source := initFixture(t)
	tenant := source.TestAccs[0]
	providerAddress := source.TestAccs[1]
	payoutAddress := source.TestAccs[2]
	provider := source.createTestProvider(t, providerAddress.String(), payoutAddress.String())
	sku := source.createTestSKU(t, provider.Uuid, 1)
	creditAddress := types.DeriveCreditAddress(tenant)
	now := source.Ctx.BlockTime()
	reservation := sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100)))
	source.fundAccount(t, creditAddress, sdk.NewCoins(
		sdk.NewCoin(testDenom, sdkmath.NewInt(200)),
	))

	genesis := &types.GenesisState{
		Params: types.DefaultParams(),
		Leases: []types.Lease{{
			Uuid:         testLeaseUUID1,
			Tenant:       tenant.String(),
			ProviderUuid: provider.Uuid,
			Items: []types.LeaseItem{{
				SkuUuid:     sku.Uuid,
				Quantity:    1,
				LockedPrice: sdk.NewCoin(testDenom, sdkmath.OneInt()),
			}},
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  now,
			LastSettledAt:              now,
			MinLeaseDurationAtCreation: 100,
			Reservation: &types.LeaseReservation{
				RemainingAmounts: reservation,
			},
		}},
		CreditAccounts: []types.CreditAccount{{
			Tenant:           tenant.String(),
			CreditAddress:    creditAddress.String(),
			ActiveLeaseCount: 1,
			ReservedAmounts:  reservation,
		}},
		LeaseSequence: 1,
	}
	require.NoError(t, source.App.BillingKeeper.InitGenesis(source.Ctx, genesis))

	settleCtx := source.Ctx.WithBlockTime(now.Add(40 * time.Second))
	response, err := keeper.NewMsgServerImpl(source.App.BillingKeeper).Withdraw(
		settleCtx,
		&types.MsgWithdraw{
			Sender:     providerAddress.String(),
			LeaseUuids: []string{testLeaseUUID1},
		},
	)
	require.NoError(t, err)
	require.Equal(t, uint64(1), response.WithdrawalCount)
	require.Equal(t, sdkmath.NewInt(40), response.TotalAmounts.AmountOf(testDenom))

	exported := source.App.BillingKeeper.ExportGenesis(settleCtx)
	require.NoError(t, exported.Validate())
	require.NoError(t, exported.ValidateWithBlockTime(settleCtx.BlockTime()))
	require.Len(t, exported.Leases, 1)
	require.NotNil(t, exported.Leases[0].Reservation)
	require.Equal(t, sdkmath.NewInt(60), exported.Leases[0].Reservation.RemainingAmounts.AmountOf(testDenom))
	require.Len(t, exported.CreditAccounts, 1)
	require.Equal(t, sdkmath.NewInt(60), exported.CreditAccounts[0].ReservedAmounts.AmountOf(testDenom))

	creditBalance := source.App.BankKeeper.GetAllBalances(settleCtx, creditAddress)
	require.Equal(t, sdkmath.NewInt(160), creditBalance.AmountOf(testDenom))

	destination := initFixture(t)
	importCtx := destination.Ctx.WithBlockTime(settleCtx.BlockTime())
	require.NoError(t, destination.App.SKUKeeper.SetProvider(importCtx, provider))
	require.NoError(t, destination.App.SKUKeeper.SetSKU(importCtx, sku))
	destination.fundAccount(t, creditAddress, creditBalance)
	require.NoError(t, destination.App.BillingKeeper.InitGenesis(importCtx, exported))

	importedLease, err := destination.App.BillingKeeper.GetLease(importCtx, testLeaseUUID1)
	require.NoError(t, err)
	require.Equal(t, exported.Leases[0], importedLease)
	importedAccount, err := destination.App.BillingKeeper.GetCreditAccount(importCtx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, exported.CreditAccounts[0], importedAccount)
	require.True(t, creditBalance.Equal(
		destination.App.BankKeeper.GetAllBalances(importCtx, creditAddress),
	))

	reexported := destination.App.BillingKeeper.ExportGenesis(importCtx)
	require.NoError(t, reexported.Validate())
	require.NoError(t, reexported.ValidateWithBlockTime(importCtx.BlockTime()))
}
