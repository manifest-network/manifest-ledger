package keeper_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

const (
	historicalLegacyMinDuration = uint64(1800)
	currentLegacyMinDuration    = uint64(3600)
	legacyLeaseUUID1            = "01912345-6789-7abc-8def-000000000001"
	legacyLeaseUUID2            = "01912345-6789-7abc-8def-000000000002"
	modernLeaseUUID             = "01912345-6789-7abc-8def-000000000003"
)

type legacyReservationScenario struct {
	f                 *testFixture
	provider          skutypes.Provider
	sku               skutypes.SKU
	tenant            sdk.AccAddress
	creditAddress     sdk.AccAddress
	legacyLeaseUUIDs  []string
	modernReservation sdk.Coins
	totalReservation  sdk.Coins
}

func setupLegacyReservationScenario(
	t *testing.T,
	state types.LeaseState,
	legacyLeaseCount int,
	expireFirstLegacy bool,
) legacyReservationScenario {
	t.Helper()
	require.Contains(t, []int{1, 2}, legacyLeaseCount)

	f := initFixture(t)
	k := f.App.BillingKeeper
	providerAddr := f.TestAccs[1]
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)
	tenant := f.TestAccs[0]
	creditAddress := types.DeriveCreditAddress(tenant)
	f.fundAccount(t, creditAddress, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))

	params := types.DefaultParams()
	params.MinLeaseDuration = currentLegacyMinDuration

	createdAt := f.Ctx.BlockTime()
	legacyCreatedAt := createdAt
	if expireFirstLegacy {
		legacyCreatedAt = createdAt.Add(-params.PendingTimeoutDuration() - time.Second)
	}

	items := []types.LeaseItem{{
		SkuUuid:     sku.Uuid,
		Quantity:    1,
		LockedPrice: sdk.NewCoin(testDenom, sdkmath.OneInt()),
	}}
	legacyReservation := calculateLeaseReservation(t, items, historicalLegacyMinDuration)
	modernReservation := calculateLeaseReservation(t, items, currentLegacyMinDuration)
	totalReservation := sdk.NewCoins(modernReservation...)

	legacyUUIDs := []string{legacyLeaseUUID1, legacyLeaseUUID2}
	leases := make([]types.Lease, 0, legacyLeaseCount+1)
	for i := range legacyLeaseCount {
		leaseCreatedAt := createdAt
		if i == 0 {
			leaseCreatedAt = legacyCreatedAt
		}
		leases = append(leases, types.Lease{
			Uuid:                       legacyUUIDs[i],
			Tenant:                     tenant.String(),
			ProviderUuid:               provider.Uuid,
			Items:                      items,
			State:                      state,
			CreatedAt:                  leaseCreatedAt,
			LastSettledAt:              leaseCreatedAt,
			MinLeaseDurationAtCreation: 0,
		})
		totalReservation = totalReservation.Add(legacyReservation...)
	}
	leases = append(leases, types.Lease{
		Uuid:                       modernLeaseUUID,
		Tenant:                     tenant.String(),
		ProviderUuid:               provider.Uuid,
		Items:                      items,
		State:                      state,
		CreatedAt:                  createdAt,
		LastSettledAt:              createdAt,
		MinLeaseDurationAtCreation: currentLegacyMinDuration,
	})

	creditAccount := types.CreditAccount{
		Tenant:          tenant.String(),
		CreditAddress:   creditAddress.String(),
		ReservedAmounts: totalReservation,
	}
	if state == types.LEASE_STATE_ACTIVE {
		creditAccount.ActiveLeaseCount = uint64(len(leases))
	} else {
		creditAccount.PendingLeaseCount = uint64(len(leases))
	}

	genesis := &types.GenesisState{
		Params:         params,
		Leases:         leases,
		CreditAccounts: []types.CreditAccount{creditAccount},
		LeaseSequence:  uint64(len(leases)),
	}
	require.NoError(t, genesis.Validate())
	require.NoError(t, k.InitGenesis(f.Ctx, genesis))

	return legacyReservationScenario{
		f:                 f,
		provider:          provider,
		sku:               sku,
		tenant:            tenant,
		creditAddress:     creditAddress,
		legacyLeaseUUIDs:  legacyUUIDs[:legacyLeaseCount],
		modernReservation: modernReservation,
		totalReservation:  totalReservation,
	}
}

func (s legacyReservationScenario) assertExportReimports(t *testing.T, expectedReservation sdk.Coins) {
	t.Helper()

	exported := s.f.App.BillingKeeper.ExportGenesis(s.f.Ctx)
	require.NoError(t, exported.Validate())

	fresh := initFixture(t)
	fresh.Ctx = fresh.Ctx.WithBlockTime(s.f.Ctx.BlockTime())
	require.NoError(t, fresh.App.SKUKeeper.SetProvider(fresh.Ctx, s.provider))
	require.NoError(t, fresh.App.SKUKeeper.SetSKU(fresh.Ctx, s.sku))
	if !expectedReservation.IsZero() {
		fresh.fundAccount(t, types.DeriveCreditAddress(s.tenant), expectedReservation)
	}
	require.NoError(t, fresh.App.BillingKeeper.InitGenesis(fresh.Ctx, exported))

	importedAccount, err := fresh.App.BillingKeeper.GetCreditAccount(fresh.Ctx, s.tenant.String())
	require.NoError(t, err)
	require.True(t, expectedReservation.Equal(importedAccount.ReservedAmounts),
		"expected reservation %s after re-import, got %s",
		expectedReservation, importedAccount.ReservedAmounts,
	)
}

func TestLegacyReservationReleaseDefersUntilLastLiveLegacy(t *testing.T) {
	s := setupLegacyReservationScenario(t, types.LEASE_STATE_ACTIVE, 2, false)
	msgServer := keeper.NewMsgServerImpl(s.f.App.BillingKeeper)

	_, err := msgServer.CloseLease(s.f.Ctx, &types.MsgCloseLease{
		Sender:     s.f.Authority.String(),
		LeaseUuids: []string{s.legacyLeaseUUIDs[0]},
	})
	require.NoError(t, err)

	account, err := s.f.App.BillingKeeper.GetCreditAccount(s.f.Ctx, s.tenant.String())
	require.NoError(t, err)
	require.True(t, s.totalReservation.Equal(account.ReservedAmounts),
		"closing one of multiple live legacy leases must preserve the unallocatable aggregate",
	)
	require.Equal(t, uint64(2), account.ActiveLeaseCount)

	_, err = msgServer.CloseLease(s.f.Ctx, &types.MsgCloseLease{
		Sender:     s.f.Authority.String(),
		LeaseUuids: []string{s.legacyLeaseUUIDs[1]},
	})
	require.NoError(t, err)

	account, err = s.f.App.BillingKeeper.GetCreditAccount(s.f.Ctx, s.tenant.String())
	require.NoError(t, err)
	require.True(t, s.modernReservation.Equal(account.ReservedAmounts),
		"closing the last live legacy lease must reconcile to the verifiable modern floor",
	)
	require.Equal(t, uint64(1), account.ActiveLeaseCount)
	s.assertExportReimports(t, s.modernReservation)
}

func TestLegacyReservationReleaseMixedBatchOrderIsInvariant(t *testing.T) {
	tests := []struct {
		name  string
		order []string
	}{
		{
			name:  "legacy then modern",
			order: []string{legacyLeaseUUID1, modernLeaseUUID},
		},
		{
			name:  "modern then legacy",
			order: []string{modernLeaseUUID, legacyLeaseUUID1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := setupLegacyReservationScenario(t, types.LEASE_STATE_ACTIVE, 1, false)
			msgServer := keeper.NewMsgServerImpl(s.f.App.BillingKeeper)

			_, err := msgServer.CloseLease(s.f.Ctx, &types.MsgCloseLease{
				Sender:     s.f.Authority.String(),
				LeaseUuids: tc.order,
			})
			require.NoError(t, err)

			account, err := s.f.App.BillingKeeper.GetCreditAccount(s.f.Ctx, s.tenant.String())
			require.NoError(t, err)
			require.True(t, account.ReservedAmounts.IsZero())
			require.Zero(t, account.ActiveLeaseCount)
			s.assertExportReimports(t, sdk.NewCoins())
		})
	}
}

func TestLegacyReservationReleaseProtectsModernLeaseOnPendingTransitions(t *testing.T) {
	tests := []struct {
		name       string
		transition func(*testing.T, legacyReservationScenario)
		wantState  types.LeaseState
	}{
		{
			name: "provider rejection",
			transition: func(t *testing.T, s legacyReservationScenario) {
				_, err := keeper.NewMsgServerImpl(s.f.App.BillingKeeper).RejectLease(s.f.Ctx, &types.MsgRejectLease{
					Sender:     s.provider.Address,
					LeaseUuids: []string{s.legacyLeaseUUIDs[0]},
					Reason:     "capacity unavailable",
				})
				require.NoError(t, err)
			},
			wantState: types.LEASE_STATE_REJECTED,
		},
		{
			name: "pending expiry",
			transition: func(t *testing.T, s legacyReservationScenario) {
				require.NoError(t, s.f.App.BillingKeeper.EndBlocker(s.f.Ctx))
			},
			wantState: types.LEASE_STATE_EXPIRED,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := setupLegacyReservationScenario(
				t,
				types.LEASE_STATE_PENDING,
				1,
				tc.wantState == types.LEASE_STATE_EXPIRED,
			)
			tc.transition(t, s)

			legacyLease, err := s.f.App.BillingKeeper.GetLease(s.f.Ctx, s.legacyLeaseUUIDs[0])
			require.NoError(t, err)
			require.Equal(t, tc.wantState, legacyLease.State)

			modernLease, err := s.f.App.BillingKeeper.GetLease(s.f.Ctx, modernLeaseUUID)
			require.NoError(t, err)
			require.Equal(t, types.LEASE_STATE_PENDING, modernLease.State)

			account, err := s.f.App.BillingKeeper.GetCreditAccount(s.f.Ctx, s.tenant.String())
			require.NoError(t, err)
			require.True(t, s.modernReservation.Equal(account.ReservedAmounts))
			require.Equal(t, uint64(1), account.PendingLeaseCount)
			s.assertExportReimports(t, s.modernReservation)
		})
	}
}

func TestReservationInvariantDetectsPreexistingAttributedShortfall(t *testing.T) {
	s := setupLegacyReservationScenario(t, types.LEASE_STATE_ACTIVE, 1, false)
	account, err := s.f.App.BillingKeeper.GetCreditAccount(s.f.Ctx, s.tenant.String())
	require.NoError(t, err)
	account.ReservedAmounts = sdk.NewCoins(
		sdk.NewCoin(testDenom, sdkmath.NewIntFromUint64(historicalLegacyMinDuration)),
	)
	require.False(t, account.ReservedAmounts.IsAllGTE(s.modernReservation))
	require.NoError(t, s.f.App.BillingKeeper.SetCreditAccount(s.f.Ctx, account))

	message, broken := keeper.ReservationAccountingInvariant(s.f.App.BillingKeeper)(s.f.Ctx)
	require.True(t, broken)
	require.Contains(t, message, "consumable reservations sum")
}

func TestLegacyReservationGenesisRejectsCountsAbovePendingStateBound(t *testing.T) {
	f := initFixture(t)
	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)
	createdAt := f.Ctx.BlockTime()
	items := []types.LeaseItem{{
		SkuUuid:     sku.Uuid,
		Quantity:    1,
		LockedPrice: sdk.NewCoin(testDenom, sdkmath.OneInt()),
	}}
	legacyLease := types.Lease{
		Uuid:                       "01912345-6789-7abc-8def-f00000000000",
		Tenant:                     tenant.String(),
		ProviderUuid:               provider.Uuid,
		Items:                      items,
		State:                      types.LEASE_STATE_PENDING,
		CreatedAt:                  createdAt,
		LastSettledAt:              createdAt,
		MinLeaseDurationAtCreation: 0,
	}
	leases := []types.Lease{legacyLease}
	modernReservation := calculateLeaseReservation(t, items, types.DefaultMinLeaseDuration)
	totalReservation := calculateLeaseReservation(t, items, historicalLegacyMinDuration)
	modernLeaseCount := types.MaxPendingLeasesPerTenantUpperBound + 1
	for i := uint64(0); i < modernLeaseCount; i++ {
		leases = append(leases, types.Lease{
			Uuid: fmt.Sprintf(
				"01912345-6789-7abc-8def-%012x",
				i+1,
			),
			Tenant:                     tenant.String(),
			ProviderUuid:               provider.Uuid,
			Items:                      items,
			State:                      types.LEASE_STATE_PENDING,
			CreatedAt:                  createdAt,
			LastSettledAt:              createdAt,
			MinLeaseDurationAtCreation: types.DefaultMinLeaseDuration,
		})
		totalReservation = totalReservation.Add(modernReservation...)
	}

	genesis := &types.GenesisState{
		Params: types.DefaultParams(),
		Leases: leases,
		CreditAccounts: []types.CreditAccount{{
			Tenant:            tenant.String(),
			CreditAddress:     types.DeriveCreditAddress(tenant).String(),
			PendingLeaseCount: uint64(len(leases)),
			ReservedAmounts:   totalReservation,
		}},
		LeaseSequence: uint64(len(leases)),
	}
	err := genesis.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "pending_lease_count")
	require.ErrorContains(t, err, "above hard upper bound")
}
