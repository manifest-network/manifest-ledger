package cmd

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/manifest-network/manifest-ledger/app/params"
	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestWriteBillingMigrationPreflightHasStableJSON(t *testing.T) {
	encodingConfig := params.MakeEncodingConfig()
	billingJSON, err := encodingConfig.Codec.MarshalJSON(billingtypes.DefaultGenesis())
	require.NoError(t, err)
	bankJSON, err := encodingConfig.Codec.MarshalJSON(banktypes.DefaultGenesisState())
	require.NoError(t, err)

	documents := []string{
		fmt.Sprintf(
			`{"chain_id":"manifest-test","initial_height":4321,"genesis_time":"2030-01-02T03:04:05Z","app_state":{"billing":%s,"bank":%s}}`,
			billingJSON,
			bankJSON,
		),
		fmt.Sprintf(
			`{"app_state":{"bank":%s,"billing":%s},"genesis_time":"2030-01-02T03:04:05.000Z","initial_height":4321,"chain_id":"manifest-test"}`,
			bankJSON,
			billingJSON,
		),
	}
	const expected = `{
  "schema_version": 1,
  "source_chain_id": "manifest-test",
  "source_initial_height": 4321,
  "input_genesis_time": "2030-01-02T03:04:05Z",
  "billing_state": "consumable_v4",
  "reservation_change_tenant_count": 0,
  "expiring_modern_pending_tenant_count": 0,
  "expiring_modern_pending_lease_count": 0,
  "tenants": []
}
`
	for _, document := range documents {
		var output bytes.Buffer
		require.NoError(t, writeBillingMigrationPreflight(
			encodingConfig.Codec,
			bytes.NewBufferString(document),
			&output,
		))
		require.Equal(t, expected, output.String())
	}
}

func TestWriteBillingMigrationPreflightHasStableReservationChangeJSON(t *testing.T) {
	encodingConfig := params.MakeEncodingConfig()
	plannerTime := time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC)
	tenant := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	creditAddress := billingtypes.DeriveCreditAddress(tenant)
	const (
		leaseUUID    = "01912345-6789-7abc-8def-0123456789a0"
		providerUUID = "01912345-6789-7abc-8def-0123456789b0"
		skuUUID      = "01912345-6789-7abc-8def-0123456789c0"
	)
	billingGenesis := &billingtypes.GenesisState{
		Params: billingtypes.DefaultParams(),
		Leases: []billingtypes.Lease{{
			Uuid:         leaseUUID,
			Tenant:       tenant.String(),
			ProviderUuid: providerUUID,
			Items: []billingtypes.LeaseItem{{
				SkuUuid:     skuUUID,
				Quantity:    1,
				LockedPrice: sdk.NewInt64Coin("umfx", 10),
			}},
			State:                      billingtypes.LEASE_STATE_ACTIVE,
			CreatedAt:                  plannerTime,
			LastSettledAt:              plannerTime,
			MinLeaseDurationAtCreation: 1,
		}},
		CreditAccounts: []billingtypes.CreditAccount{{
			Tenant:           tenant.String(),
			CreditAddress:    creditAddress.String(),
			ActiveLeaseCount: 1,
			ReservedAmounts:  sdk.NewCoins(sdk.NewInt64Coin("umfx", 10)),
		}},
		LeaseSequence: 1,
	}
	bankGenesis := banktypes.DefaultGenesisState()
	bankGenesis.Balances = []banktypes.Balance{{
		Address: creditAddress.String(),
		Coins:   sdk.NewCoins(sdk.NewInt64Coin("umfx", 5)),
	}}
	billingJSON, err := encodingConfig.Codec.MarshalJSON(billingGenesis)
	require.NoError(t, err)
	bankJSON, err := encodingConfig.Codec.MarshalJSON(bankGenesis)
	require.NoError(t, err)
	document := fmt.Sprintf(
		`{"chain_id":"manifest-test","initial_height":4321,"genesis_time":"2030-01-02T03:04:05Z","app_state":{"billing":%s,"bank":%s}}`,
		billingJSON,
		bankJSON,
	)

	var output bytes.Buffer
	require.NoError(t, writeBillingMigrationPreflight(
		encodingConfig.Codec,
		bytes.NewBufferString(document),
		&output,
	))
	expected := fmt.Sprintf(`{
  "schema_version": 1,
  "source_chain_id": "manifest-test",
  "source_initial_height": 4321,
  "input_genesis_time": "2030-01-02T03:04:05Z",
  "billing_state": "pre_v4_aggregate",
  "reservation_change_tenant_count": 1,
  "expiring_modern_pending_tenant_count": 0,
  "expiring_modern_pending_lease_count": 0,
  "tenants": [
    {
      "tenant": %q,
      "credit_address": %q,
      "has_planned_reservation_change": true,
      "denominations": [
        {
          "denom": "umfx",
          "source_reservation_aggregate": "10",
          "pre_cutover_reservation_aggregate": "10",
          "post_cutover_reservation_aggregate": "5",
          "pre_cutover_unattributed_reservation": "0",
          "post_cutover_unattributed_reservation": "0",
          "bank_balance": "5",
          "modern_pending_required": "0",
          "modern_pending_shortfall": "0"
        }
      ],
      "modern_active_leases": [
        {
          "lease_uuid": %q,
          "nominal_amounts": [
            {
              "denom": "umfx",
              "amount": "10"
            }
          ],
          "planned_remaining_amounts": [
            {
              "denom": "umfx",
              "amount": "5"
            }
          ]
        }
      ],
      "modern_pending_lease_uuids": [],
      "expiring_modern_pending_lease_uuids": []
    }
  ]
}`, tenant.String(), creditAddress.String(), leaseUUID) + "\n"
	require.Equal(t, expected, output.String())
}

func TestWriteBillingMigrationPreflightFailsClosedOnMissingModules(t *testing.T) {
	encodingConfig := params.MakeEncodingConfig()
	tests := []struct {
		name     string
		document string
		contains string
	}{
		{
			name:     "missing app state",
			document: `{"chain_id":"manifest-test","genesis_time":"2030-01-02T03:04:05Z"}`,
			contains: "missing app_state",
		},
		{
			name:     "missing billing",
			document: `{"chain_id":"manifest-test","genesis_time":"2030-01-02T03:04:05Z","app_state":{"bank":{}}}`,
			contains: `missing "billing" module`,
		},
		{
			name:     "missing bank",
			document: `{"chain_id":"manifest-test","genesis_time":"2030-01-02T03:04:05Z","app_state":{"billing":{}}}`,
			contains: `missing "bank" module`,
		},
		{
			name:     "missing chain ID",
			document: `{"genesis_time":"2030-01-02T03:04:05Z","app_state":{}}`,
			contains: "missing chain_id",
		},
		{
			name:     "negative initial height",
			document: `{"chain_id":"manifest-test","initial_height":-1,"genesis_time":"2030-01-02T03:04:05Z","app_state":{}}`,
			contains: "negative initial_height",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			err := writeBillingMigrationPreflight(
				encodingConfig.Codec,
				bytes.NewBufferString(tt.document),
				&output,
			)
			require.ErrorContains(t, err, tt.contains)
			require.Empty(t, output.String())
		})
	}
}
