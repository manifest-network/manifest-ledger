package keeper

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	_ "github.com/manifest-network/manifest-ledger/app/params"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

const (
	preflightPendingUUID1 = "01912345-6789-7abc-8def-0123456789a0"
	preflightPendingUUID2 = "01912345-6789-7abc-8def-0123456789a1"
	preflightProviderUUID = "01912345-6789-7abc-8def-0123456789b0"
	preflightSKUUUID1     = "01912345-6789-7abc-8def-0123456789c0"
	preflightSKUUUID2     = "01912345-6789-7abc-8def-0123456789c1"
)

func TestBuildReservationMigrationPreflightIsDeterministicAndReadOnly(t *testing.T) {
	cutoverTime := time.Date(2030, time.January, 2, 3, 4, 5, 600, time.FixedZone("test", -5*60*60))
	tenant1 := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	tenant2 := sdk.AccAddress(bytes.Repeat([]byte{2}, 20))
	creditAddress1 := types.DeriveCreditAddress(tenant1)
	now := cutoverTime.Add(-time.Hour)
	lease := func(uuid, skuUUID, denom string, amount int64) types.Lease {
		return types.Lease{
			Uuid:         uuid,
			Tenant:       tenant1.String(),
			ProviderUuid: preflightProviderUUID,
			Items: []types.LeaseItem{{
				SkuUuid:     skuUUID,
				Quantity:    1,
				LockedPrice: sdk.NewCoin(denom, sdkmath.NewInt(amount)),
			}},
			State:                      types.LEASE_STATE_PENDING,
			CreatedAt:                  now,
			LastSettledAt:              now,
			MinLeaseDurationAtCreation: 1,
		}
	}
	first := lease(preflightPendingUUID1, preflightSKUUUID1, "ualpha", 5)
	second := lease(preflightPendingUUID2, preflightSKUUUID2, "ubeta", 7)
	account1 := types.CreditAccount{
		Tenant:            tenant1.String(),
		CreditAddress:     creditAddress1.String(),
		PendingLeaseCount: 2,
		ReservedAmounts: sdk.NewCoins(
			sdk.NewInt64Coin("ualpha", 5),
			sdk.NewInt64Coin("ubeta", 7),
		),
	}
	account2 := types.CreditAccount{
		Tenant:        tenant2.String(),
		CreditAddress: types.DeriveCreditAddress(tenant2).String(),
	}
	billingGenesis := &types.GenesisState{
		Params:         types.DefaultParams(),
		Leases:         []types.Lease{second, first},
		CreditAccounts: []types.CreditAccount{account2, account1},
		LeaseSequence:  2,
	}
	bankGenesis := banktypes.DefaultGenesisState()
	bankGenesis.Balances = []banktypes.Balance{
		{
			Address: sdk.AccAddress(bytes.Repeat([]byte{3}, 20)).String(),
			Coins:   sdk.NewCoins(sdk.NewInt64Coin("uother", 99)),
		},
		{
			Address: creditAddress1.String(),
			Coins: sdk.NewCoins(
				sdk.NewInt64Coin("ualpha", 5),
				sdk.NewInt64Coin("ubeta", 6),
			),
		},
	}
	// Serialize the complete protobuf graphs so nested item/coin slices cannot
	// alias the snapshots and hide an in-place mutation.
	originalBillingJSON, err := json.Marshal(billingGenesis)
	require.NoError(t, err)
	originalBankJSON, err := json.Marshal(bankGenesis)
	require.NoError(t, err)

	report, err := BuildReservationMigrationPreflight(cutoverTime, billingGenesis, bankGenesis)
	require.NoError(t, err)
	actualBillingJSON, err := json.Marshal(billingGenesis)
	require.NoError(t, err)
	actualBankJSON, err := json.Marshal(bankGenesis)
	require.NoError(t, err)
	require.Equal(t, originalBillingJSON, actualBillingJSON)
	require.Equal(t, originalBankJSON, actualBankJSON)
	require.Equal(t, ReservationMigrationPreflight{
		BillingState:                     ReservationPreflightStatePreV4,
		ReservationChangeTenantCount:     1,
		ExpiringModernPendingTenantCount: 1,
		ExpiringModernPendingLeaseCount:  2,
		Tenants: []ReservationMigrationTenantPreflight{
			{
				Tenant:                      tenant1.String(),
				CreditAddress:               creditAddress1.String(),
				HasPlannedReservationChange: true,
				Denominations: []ReservationMigrationDenomPreflight{
					{
						Denom:                              "ualpha",
						SourceReservationAggregate:         "5",
						PreCutoverReservationAggregate:     "5",
						PostCutoverReservationAggregate:    "0",
						PreCutoverUnattributedReservation:  "0",
						PostCutoverUnattributedReservation: "0",
						BankBalance:                        "5",
						ModernPendingRequired:              "5",
						ModernPendingShortfall:             "0",
					},
					{
						Denom:                              "ubeta",
						SourceReservationAggregate:         "7",
						PreCutoverReservationAggregate:     "7",
						PostCutoverReservationAggregate:    "0",
						PreCutoverUnattributedReservation:  "0",
						PostCutoverUnattributedReservation: "0",
						BankBalance:                        "6",
						ModernPendingRequired:              "7",
						ModernPendingShortfall:             "1",
					},
				},
				ModernActiveLeases: []ReservationMigrationActivePreflight{},
				ModernPendingLeaseUUIDs: []string{
					preflightPendingUUID1,
					preflightPendingUUID2,
				},
				ExpiringModernPendingLeaseUUIDs: []string{
					preflightPendingUUID1,
					preflightPendingUUID2,
				},
			},
			{
				Tenant:                          tenant2.String(),
				CreditAddress:                   types.DeriveCreditAddress(tenant2).String(),
				HasPlannedReservationChange:     false,
				Denominations:                   []ReservationMigrationDenomPreflight{},
				ModernActiveLeases:              []ReservationMigrationActivePreflight{},
				ModernPendingLeaseUUIDs:         []string{},
				ExpiringModernPendingLeaseUUIDs: []string{},
			},
		},
	}, report)

	reorderedBilling := *billingGenesis
	reorderedBilling.Leases = []types.Lease{first, second}
	reorderedBilling.CreditAccounts = []types.CreditAccount{account1, account2}
	reorderedBank := *bankGenesis
	reorderedBank.Balances = slices.Clone(bankGenesis.Balances)
	slices.Reverse(reorderedBank.Balances)
	reorderedReport, err := BuildReservationMigrationPreflight(
		cutoverTime,
		&reorderedBilling,
		&reorderedBank,
	)
	require.NoError(t, err)
	require.Equal(t, report, reorderedReport)
	reportJSON, err := json.Marshal(report)
	require.NoError(t, err)
	reorderedReportJSON, err := json.Marshal(reorderedReport)
	require.NoError(t, err)
	require.Equal(t, reportJSON, reorderedReportJSON,
		"reordered genesis slices must produce byte-identical machine output")
}

func TestBuildReservationMigrationPreflightRejectsUnderbackedV4State(t *testing.T) {
	cutoverTime := time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC)
	tenant := sdk.AccAddress(bytes.Repeat([]byte{4}, 20))
	creditAddress := types.DeriveCreditAddress(tenant)
	lease := types.Lease{
		Uuid:         preflightPendingUUID1,
		Tenant:       tenant.String(),
		ProviderUuid: preflightProviderUUID,
		Items: []types.LeaseItem{{
			SkuUuid:     preflightSKUUUID1,
			Quantity:    1,
			LockedPrice: sdk.NewInt64Coin("ualpha", 5),
		}},
		State:     types.LEASE_STATE_PENDING,
		CreatedAt: cutoverTime.Add(-time.Hour), LastSettledAt: cutoverTime.Add(-time.Hour),
		MinLeaseDurationAtCreation: 1,
		Reservation: &types.LeaseReservation{
			RemainingAmounts: sdk.NewCoins(sdk.NewInt64Coin("ualpha", 5)),
		},
	}
	billingGenesis := &types.GenesisState{
		Params: types.DefaultParams(),
		Leases: []types.Lease{lease},
		CreditAccounts: []types.CreditAccount{{
			Tenant: tenant.String(), CreditAddress: creditAddress.String(),
			PendingLeaseCount: 1,
			ReservedAmounts:   sdk.NewCoins(sdk.NewInt64Coin("ualpha", 5)),
		}},
		LeaseSequence: 1,
	}
	bankGenesis := banktypes.DefaultGenesisState()
	bankGenesis.Balances = []banktypes.Balance{{
		Address: creditAddress.String(),
		Coins:   sdk.NewCoins(sdk.NewInt64Coin("ualpha", 4)),
	}}

	_, err := BuildReservationMigrationPreflight(cutoverTime, billingGenesis, bankGenesis)
	require.ErrorIs(t, err, types.ErrReservationInvariant)
	require.ErrorContains(t, err, "consumable v4 billing state")
	require.ErrorContains(t, err, "is under-backed")
}

func TestBuildReservationMigrationPreflightReportsOpaqueLegacyCohort(t *testing.T) {
	plannerTime := time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC)
	tenant := sdk.AccAddress(bytes.Repeat([]byte{6}, 20))
	creditAddress := types.DeriveCreditAddress(tenant)
	modernActive := types.Lease{
		Uuid:         preflightPendingUUID1,
		Tenant:       tenant.String(),
		ProviderUuid: preflightProviderUUID,
		Items: []types.LeaseItem{{
			SkuUuid:     preflightSKUUUID1,
			Quantity:    1,
			LockedPrice: sdk.NewInt64Coin("ualpha", 3),
		}},
		State:                      types.LEASE_STATE_ACTIVE,
		CreatedAt:                  plannerTime,
		LastSettledAt:              plannerTime,
		MinLeaseDurationAtCreation: 1,
	}
	legacyActive := types.Lease{
		Uuid:         preflightPendingUUID2,
		Tenant:       tenant.String(),
		ProviderUuid: preflightProviderUUID,
		Items: []types.LeaseItem{{
			SkuUuid:     preflightSKUUUID2,
			Quantity:    1,
			LockedPrice: sdk.NewInt64Coin("ualpha", 1),
		}},
		State:         types.LEASE_STATE_ACTIVE,
		CreatedAt:     plannerTime,
		LastSettledAt: plannerTime,
	}
	billingGenesis := &types.GenesisState{
		Params: types.DefaultParams(),
		Leases: []types.Lease{legacyActive, modernActive},
		CreditAccounts: []types.CreditAccount{{
			Tenant:           tenant.String(),
			CreditAddress:    creditAddress.String(),
			ActiveLeaseCount: 2,
			ReservedAmounts:  sdk.NewCoins(sdk.NewInt64Coin("ualpha", 10)),
		}},
		LeaseSequence: 2,
	}
	bankGenesis := banktypes.DefaultGenesisState()
	bankGenesis.Balances = []banktypes.Balance{{
		Address: creditAddress.String(),
		Coins:   sdk.NewCoins(sdk.NewInt64Coin("ualpha", 7)),
	}}

	report, err := BuildReservationMigrationPreflight(
		plannerTime,
		billingGenesis,
		bankGenesis,
	)
	require.NoError(t, err)
	require.Equal(t, uint64(1), report.ReservationChangeTenantCount)
	require.Len(t, report.Tenants, 1)
	require.True(t, report.Tenants[0].HasPlannedReservationChange)
	require.Equal(t, []ReservationMigrationDenomPreflight{{
		Denom:                              "ualpha",
		SourceReservationAggregate:         "10",
		PreCutoverReservationAggregate:     "10",
		PostCutoverReservationAggregate:    "7",
		PreCutoverUnattributedReservation:  "7",
		PostCutoverUnattributedReservation: "5",
		BankBalance:                        "7",
		ModernPendingRequired:              "0",
		ModernPendingShortfall:             "0",
	}}, report.Tenants[0].Denominations)
	require.Equal(t, []ReservationMigrationActivePreflight{{
		LeaseUUID: preflightPendingUUID1,
		NominalAmounts: []ReservationMigrationAmountPreflight{{
			Denom: "ualpha", Amount: "3",
		}},
		PlannedRemainingAmounts: []ReservationMigrationAmountPreflight{{
			Denom: "ualpha", Amount: "2",
		}},
	}}, report.Tenants[0].ModernActiveLeases)
}

func TestBuildReservationMigrationPreflightRejectsDuplicateBankByteIdentity(t *testing.T) {
	cutoverTime := time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC)
	address := sdk.AccAddress(bytes.Repeat([]byte{5}, 20))
	bankGenesis := banktypes.DefaultGenesisState()
	bankGenesis.Balances = []banktypes.Balance{
		{Address: address.String(), Coins: sdk.NewCoins(sdk.NewInt64Coin("ualpha", 1))},
		{Address: strings.ToUpper(address.String()), Coins: sdk.NewCoins(sdk.NewInt64Coin("ubeta", 1))},
	}

	_, err := BuildReservationMigrationPreflight(
		cutoverTime,
		types.DefaultGenesis(),
		bankGenesis,
	)
	require.ErrorContains(t, err, "duplicate decoded address identity")
}
