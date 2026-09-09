package types_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func lifecycleGenesis(state types.LeaseState) *types.GenesisState {
	tenant := sdk.AccAddress([]byte("lifecycle-test-tenant"))
	now := time.Unix(1_700_000_000, 0).UTC()
	lease := types.Lease{
		Uuid: "01912345-6789-7abc-8def-0123456789ab", Tenant: tenant.String(),
		ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
		Items: []types.LeaseItem{{
			SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1,
			LockedPrice: sdk.NewInt64Coin(testDenom, 1),
		}},
		State: state, CreatedAt: now, LastSettledAt: now,
		MinLeaseDurationAtCreation: 1, Reservation: &types.LeaseReservation{},
	}
	account := types.CreditAccount{Tenant: tenant.String(), CreditAddress: types.DeriveCreditAddress(tenant).String()}
	switch state {
	case types.LEASE_STATE_PENDING:
		account.PendingLeaseCount = 1
	case types.LEASE_STATE_ACTIVE:
		account.ActiveLeaseCount = 1
		lease.AcknowledgedAt = &now
	case types.LEASE_STATE_CLOSED:
		lease.ClosedAt = &now
	case types.LEASE_STATE_REJECTED:
		lease.RejectedAt = &now
	case types.LEASE_STATE_EXPIRED:
		lease.ExpiredAt = &now
	}
	if state == types.LEASE_STATE_PENDING || state == types.LEASE_STATE_ACTIVE {
		lease.Reservation.RemainingAmounts = sdk.NewCoins(sdk.NewInt64Coin(testDenom, 1))
		account.ReservedAmounts = sdk.NewCoins(sdk.NewInt64Coin(testDenom, 1))
	}
	return types.NewGenesisState(types.DefaultParams(), []types.Lease{lease}, []types.CreditAccount{account}, 1)
}

func assertLifecycleValidation(t *testing.T, genesis *types.GenesisState, errorText string) {
	t.Helper()
	checks := []struct {
		name     string
		validate func() error
	}{
		{"import", genesis.Validate},
		{"strict authoring", genesis.ValidateStrict},
		{"current state", genesis.ValidateCurrentState},
		{"import preparation", func() error { _, err := genesis.PrepareForImport(); return err }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if errorText == "" {
				require.NoError(t, check.validate())
			} else {
				require.ErrorContains(t, check.validate(), errorText)
			}
		})
	}
}

func TestGenesisClosedTimestampMatchesLifecycleState(t *testing.T) {
	for _, state := range []types.LeaseState{
		types.LEASE_STATE_PENDING, types.LEASE_STATE_ACTIVE, types.LEASE_STATE_CLOSED,
		types.LEASE_STATE_REJECTED, types.LEASE_STATE_EXPIRED,
	} {
		t.Run(state.String(), func(t *testing.T) {
			genesis := lifecycleGenesis(state)
			assertLifecycleValidation(t, genesis, "")
			if state == types.LEASE_STATE_CLOSED {
				genesis.Leases[0].ClosedAt = nil
				assertLifecycleValidation(t, genesis, "closed but has no closed_at")
			} else {
				closedAt := genesis.Leases[0].CreatedAt.Add(time.Hour)
				genesis.Leases[0].ClosedAt = &closedAt
				assertLifecycleValidation(t, genesis, "closed_at timestamp in non-closed state")
			}
		})
	}
}

func TestGenesisReasonsEnforceMessageByteLimits(t *testing.T) {
	for _, reason := range []struct {
		name  string
		state types.LeaseState
		set   func(*types.Lease, string)
		limit int
	}{
		{"rejection_reason", types.LEASE_STATE_REJECTED, func(l *types.Lease, s string) { l.RejectionReason = s }, types.MaxRejectionReasonLength},
		{"closure_reason", types.LEASE_STATE_CLOSED, func(l *types.Lease, s string) { l.ClosureReason = s }, types.MaxClosureReasonLength},
	} {
		t.Run(reason.name, func(t *testing.T) {
			genesis := lifecycleGenesis(reason.state)
			value := strings.Repeat("é", reason.limit/2)
			require.Len(t, value, reason.limit)
			reason.set(&genesis.Leases[0], value)
			assertLifecycleValidation(t, genesis, "")
			reason.set(&genesis.Leases[0], value+"a")
			assertLifecycleValidation(t, genesis, reason.name+" exceeding maximum length")
		})
	}
}
