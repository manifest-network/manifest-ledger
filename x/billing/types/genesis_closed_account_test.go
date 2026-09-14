package types_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestGenesisRetainedClosedIntervalRequiresCreditAccount(t *testing.T) {
	for _, test := range []struct {
		name    string
		state   types.LeaseState
		lag     time.Duration
		account string
		invalid bool
	}{
		{name: "whole second without account", state: types.LEASE_STATE_CLOSED, lag: time.Second, invalid: true},
		{name: "subsecond without account", state: types.LEASE_STATE_CLOSED, lag: time.Nanosecond, invalid: true},
		{name: "unrelated account", state: types.LEASE_STATE_CLOSED, lag: time.Second, account: "other", invalid: true},
		{name: "whole second with account", state: types.LEASE_STATE_CLOSED, lag: time.Second, account: "same"},
		{name: "subsecond with account", state: types.LEASE_STATE_CLOSED, lag: time.Nanosecond, account: "same"},
		{name: "equivalent account address", state: types.LEASE_STATE_CLOSED, lag: time.Second, account: "alias"},
		{name: "finalized without account", state: types.LEASE_STATE_CLOSED},
		{name: "rejected without account", state: types.LEASE_STATE_REJECTED},
		{name: "expired without account", state: types.LEASE_STATE_EXPIRED},
	} {
		t.Run(test.name, func(t *testing.T) {
			genesis := lifecycleGenesis(test.state)
			if test.state == types.LEASE_STATE_CLOSED {
				closedAt := genesis.Leases[0].LastSettledAt.Add(test.lag)
				genesis.Leases[0].ClosedAt = &closedAt
			}
			switch test.account {
			case "same":
			case "alias":
				genesis.CreditAccounts[0].Tenant = strings.ToUpper(genesis.CreditAccounts[0].Tenant)
			case "other":
				other := sdk.AccAddress([]byte("other-closed-tenant!"))
				genesis.CreditAccounts[0].Tenant = other.String()
				genesis.CreditAccounts[0].CreditAddress = types.DeriveCreditAddress(other).String()
			default:
				genesis.CreditAccounts = nil
			}
			errorText := ""
			if test.invalid {
				errorText = "retained final interval but no credit account"
			}
			before := genesis.String()
			assertLifecycleValidation(t, genesis, errorText)
			require.Equal(t, before, genesis.String())

			// Complete aggregate-only imports obey the same account precondition;
			// terminal history without a retained interval remains compatible.
			genesis.Leases[0].Reservation = nil
			for _, validate := range []func() error{genesis.Validate, genesis.ValidateStrict} {
				if test.invalid {
					require.ErrorContains(t, validate(), errorText)
				} else {
					require.NoError(t, validate())
				}
			}
		})
	}
}
