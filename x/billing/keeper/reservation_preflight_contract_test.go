package keeper

import (
	"bytes"
	"encoding/json"
	"math/big"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

type reservationPreflightInput struct {
	at       time.Time
	billing  *types.GenesisState
	bank     *banktypes.GenesisState
	accounts authtypes.GenesisAccounts
}

func newReservationPreflightInput() reservationPreflightInput {
	at := time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC)
	tenant := sdk.AccAddress(bytes.Repeat([]byte{41}, 20))
	credit := types.DeriveCreditAddress(tenant)
	billing := types.DefaultGenesis()
	billing.LeaseSequence = 1
	billing.Leases = []types.Lease{{
		Uuid: preflightPendingUUID1, Tenant: tenant.String(), ProviderUuid: preflightProviderUUID,
		Items: []types.LeaseItem{{SkuUuid: preflightSKUUUID1, Quantity: 1, LockedPrice: sdk.NewInt64Coin("umfx", 1)}},
		State: types.LEASE_STATE_ACTIVE, CreatedAt: at.Add(-time.Hour), LastSettledAt: at.Add(-time.Hour),
		MinLeaseDurationAtCreation: 100,
		Reservation:                &types.LeaseReservation{RemainingAmounts: sdk.NewCoins(sdk.NewInt64Coin("umfx", 40))},
	}}
	billing.CreditAccounts = []types.CreditAccount{{
		Tenant: tenant.String(), CreditAddress: credit.String(), ActiveLeaseCount: 1,
		ReservedAmounts: sdk.NewCoins(sdk.NewInt64Coin("umfx", 40)),
	}}
	bank := banktypes.DefaultGenesisState()
	bank.Balances = []banktypes.Balance{{Address: credit.String(), Coins: sdk.NewCoins(sdk.NewInt64Coin("umfx", 100))}}
	return reservationPreflightInput{at: at, billing: billing, bank: bank, accounts: authtypes.GenesisAccounts{authtypes.NewBaseAccountWithAddress(credit)}}
}

// Exercise the public offline boundary with malformed exported inputs. These
// failures must return errors and retain the caller's source graph, rather than
// silently repair a forbidden reservation shape or expose a success report.
func TestReservationPreflightRejectsInvalidInputWithoutMutation(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*reservationPreflightInput)
		err    string
	}{
		{"zero valuation time", func(input *reservationPreflightInput) { input.at = time.Time{} }, "requires a non-zero planner time"},
		{"missing billing", func(input *reservationPreflightInput) { input.billing = nil }, "requires billing genesis state"},
		{"missing bank", func(input *reservationPreflightInput) { input.bank = nil }, "requires bank genesis state"},
		{"invalid bank address", func(input *reservationPreflightInput) { input.bank.Balances[0].Address = "invalid" }, "validate bank genesis"},
		{"invalid bank coins", func(input *reservationPreflightInput) {
			input.bank.Balances[0].Coins = sdk.Coins{{Denom: "umfx", Amount: sdkmath.NewInt(-1)}}
		}, "validate bank genesis"},
		{"duplicate auth account", func(input *reservationPreflightInput) {
			input.accounts = append(input.accounts, authtypes.NewBaseAccountWithAddress(input.accounts[0].GetAddress()))
		}, "duplicate account found in genesis state"},
		{"nil auth account", func(input *reservationPreflightInput) { input.accounts = append(input.accounts, nil) }, "invalid auth account data"},
		{"mixed reservation formats", func(input *reservationPreflightInput) {
			other := input.billing.Leases[0]
			other.Uuid, other.Reservation = preflightPendingUUID2, nil
			input.billing.Leases = append(input.billing.Leases, other)
			input.billing.LeaseSequence++
		}, "detect billing reservation format"},
		{"pre-v4 state with v4 cohort", func(input *reservationPreflightInput) {
			input.billing.Leases[0].Reservation = nil
			input.billing.CreditAccounts[0].UnattributedReservedAmounts = sdk.NewCoins(sdk.NewInt64Coin("umfx", 1))
		}, "legacy reservation state"},
		{"lease-free invalid allowed list", func(input *reservationPreflightInput) {
			input.billing = types.DefaultGenesis()
			input.billing.Params.AllowedList = []string{"invalid"}
		}, "normalize billing allowed list for lease-free migration preflight"},
		{"negative legacy source aggregate", func(input *reservationPreflightInput) {
			input.billing.Leases[0].Reservation = nil
			input.billing.CreditAccounts[0].ReservedAmounts = sdk.Coins{{Denom: "umfx", Amount: sdkmath.NewInt(-1)}}
		}, "repair credit account"},
		{"invalid legacy cohort amount", func(input *reservationPreflightInput) {
			input.billing.CreditAccounts[0].UnattributedReservedAmounts = sdk.Coins{{Denom: "umfx", Amount: sdkmath.NewInt(-1)}}
		}, "invalid unattributed_reserved_amounts"},
		{"negative lease remainder", func(input *reservationPreflightInput) {
			input.billing.Leases[0].Reservation.RemainingAmounts = sdk.Coins{{Denom: "umfx", Amount: sdkmath.NewInt(-1)}}
		}, "invalid remaining reservation"},
		{"terminal lease retains claim", func(input *reservationPreflightInput) {
			input.billing.Leases[0].State = types.LEASE_STATE_REJECTED
		}, "terminal lease"},
		{"legacy lease claims modern allocation", func(input *reservationPreflightInput) {
			input.billing.Leases[0].MinLeaseDurationAtCreation = 0
		}, "has attributed reservation"},
		{"pending claim is partially consumed", func(input *reservationPreflightInput) {
			input.billing.Leases[0].State = types.LEASE_STATE_PENDING
		}, "expected 100umfx"},
		{"active remainder exceeds nominal", func(input *reservationPreflightInput) {
			input.billing.Leases[0].Reservation.RemainingAmounts = sdk.NewCoins(sdk.NewInt64Coin("umfx", 101))
		}, "exceeds nominal reservation"},
		{"v4 modern allocation sum overflows", func(input *reservationPreflightInput) {
			addOverflowingPreflightClaims(input, false, false)
		}, "sum attributed reservations"},
		{"v4 modern plus legacy sum overflows", func(input *reservationPreflightInput) {
			addOverflowingPreflightClaims(input, false, true)
		}, "sum reservations for tenant"},
		{"pre-v4 known claim sum overflows", func(input *reservationPreflightInput) {
			addOverflowingPreflightClaims(input, true, false)
		}, "sum known import reservations"},
		{"live reservation without account", func(input *reservationPreflightInput) {
			input.billing.CreditAccounts = nil
		}, "live reservation claims but no credit account"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := newReservationPreflightInput()
			test.mutate(&input)
			before, err := json.Marshal([]any{input.billing, input.bank, input.accounts})
			require.NoError(t, err)
			var report ReservationMigrationPreflight
			require.NotPanics(t, func() {
				report, err = BuildReservationMigrationPreflight(input.at, input.billing, input.bank, input.accounts)
			})
			require.ErrorContains(t, err, test.err)
			require.Empty(t, report.Tenants, "invalid input must not be reported as an audited tenant")
			after, err := json.Marshal([]any{input.billing, input.bank, input.accounts})
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func addOverflowingPreflightClaims(input *reservationPreflightInput, preV4, legacySibling bool) {
	halfCeiling := sdkmath.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 255))
	claim := sdk.NewCoins(sdk.NewCoin("umfx", halfCeiling))
	first := &input.billing.Leases[0]
	first.MinLeaseDurationAtCreation = 1
	first.Items[0].LockedPrice = claim[0]
	first.Reservation.RemainingAmounts = claim
	second := *first
	second.Uuid = preflightPendingUUID2
	second.Reservation = &types.LeaseReservation{RemainingAmounts: slices.Clone(claim)}
	if legacySibling {
		second.MinLeaseDurationAtCreation = 0
		second.Reservation.RemainingAmounts = sdk.NewCoins()
		input.billing.CreditAccounts[0].UnattributedReservedAmounts = slices.Clone(claim)
		input.billing.CreditAccounts[0].UnattributedLeaseCount = 1
	}
	if preV4 {
		first.Reservation = nil
		second.Reservation = nil
	}
	input.billing.Leases = append(input.billing.Leases, second)
	input.billing.LeaseSequence++
}

func TestReservationPreflightCurrentStateReportsConsumedActiveClaims(t *testing.T) {
	input := newReservationPreflightInput()
	second := input.billing.Leases[0]
	second.Uuid = preflightPendingUUID2
	second.Reservation = &types.LeaseReservation{RemainingAmounts: sdk.NewCoins(sdk.NewInt64Coin("umfx", 10))}
	terminal := second
	terminal.Uuid = "01912345-6789-7abc-8def-0123456789a2"
	terminal.State = types.LEASE_STATE_REJECTED
	terminal.Reservation = &types.LeaseReservation{}
	// Reverse primary order to exercise canonical ACTIVE report ordering.
	input.billing.Leases = []types.Lease{terminal, second, input.billing.Leases[0]}
	input.billing.LeaseSequence = 3
	input.billing.CreditAccounts[0].ActiveLeaseCount = 2
	input.billing.CreditAccounts[0].ReservedAmounts = sdk.NewCoins(sdk.NewInt64Coin("umfx", 50))
	require.NoError(t, input.billing.ValidateCurrentState())
	before, err := json.Marshal([]any{input.billing, input.bank, input.accounts})
	require.NoError(t, err)
	report, err := BuildReservationMigrationPreflight(input.at, input.billing, input.bank, input.accounts)
	require.NoError(t, err)
	require.Equal(t, ReservationPreflightStateV4, report.BillingState)
	require.Equal(t, ReservationPreflightPathNone, report.MigrationPath)
	require.Zero(t, report.ReservationChangeTenantCount, "a consumed current-state tranche must not be replanned as a migration haircut")
	require.Len(t, report.Tenants, 1)
	tenant := report.Tenants[0]
	require.False(t, tenant.HasPlannedReservationChange)
	require.Empty(t, tenant.ModernPendingLeaseUUIDs)
	require.Empty(t, tenant.ExpiringModernPendingLeaseUUIDs)
	require.Equal(t, []ReservationMigrationActivePreflight{
		{LeaseUUID: preflightPendingUUID1, NominalAmounts: []ReservationMigrationAmountPreflight{{Denom: "umfx", Amount: "100"}}, PlannedRemainingAmounts: []ReservationMigrationAmountPreflight{{Denom: "umfx", Amount: "40"}}},
		{LeaseUUID: preflightPendingUUID2, NominalAmounts: []ReservationMigrationAmountPreflight{{Denom: "umfx", Amount: "100"}}, PlannedRemainingAmounts: []ReservationMigrationAmountPreflight{{Denom: "umfx", Amount: "10"}}},
	}, tenant.ModernActiveLeases)
	require.Len(t, tenant.Denominations, 1)
	denom := tenant.Denominations[0]
	require.Equal(t, "50", denom.SourceReservationAggregate)
	require.Equal(t, "50", denom.PreCutoverReservationAggregate)
	require.Equal(t, "50", denom.PostCutoverReservationAggregate)
	require.Equal(t, "100", denom.BankBalance)
	require.Equal(t, "100", denom.SpendableBalance)
	after, err := json.Marshal([]any{input.billing, input.bank, input.accounts})
	require.NoError(t, err)
	require.Equal(t, before, after)
}
