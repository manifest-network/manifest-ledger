package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/manifest-network/manifest-ledger/app"
	"github.com/manifest-network/manifest-ledger/app/params"
	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

func TestWriteBillingMigrationPreflightHasStableJSON(t *testing.T) {
	encodingConfig := params.MakeEncodingConfig()
	billingJSON, err := encodingConfig.Codec.MarshalJSON(billingtypes.DefaultGenesis())
	require.NoError(t, err)
	bankJSON, err := encodingConfig.Codec.MarshalJSON(banktypes.DefaultGenesisState())
	require.NoError(t, err)

	documents := []string{
		fmt.Sprintf(
			`{"chain_id":"manifest-test","initial_height":4321,"genesis_time":"2030-01-02T03:04:05Z","app_state":{"billing":%s,"bank":%s,"sku":{}}}`,
			billingJSON,
			bankJSON,
		),
		fmt.Sprintf(
			`{"app_state":{"bank":%s,"billing":%s,"sku":{}},"genesis_time":"2030-01-02T03:04:05.000Z","initial_height":4321,"chain_id":"manifest-test"}`,
			bankJSON,
			billingJSON,
		),
	}
	const expected = `{
  "schema_version": 3,
  "source_chain_id": "manifest-test",
  "source_initial_height": 4321,
  "input_genesis_time": "2030-01-02T03:04:05Z",
  "billing_state": "consumable_v4",
  "provider_count": 0,
  "blocked_provider_count": 0,
  "blocked_providers": [],
  "payout_credit_collision_count": 0,
  "payout_credit_collisions": [],
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

func TestWriteBillingMigrationPreflightHasStableReservationChangeAndCreditCollisionJSON(t *testing.T) {
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
			Tenant:       strings.ToUpper(tenant.String()),
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
	skuGenesis := skutypes.DefaultGenesis()
	skuGenesis.Providers = []skutypes.Provider{{
		Uuid: providerUUID, Address: sdk.AccAddress(bytes.Repeat([]byte{2}, 20)).String(),
		PayoutAddress: strings.ToUpper(creditAddress.String()), Active: true,
	}}
	skuGenesis.ProviderSequence = 1
	require.NoError(t, skuGenesis.Validate())
	skuJSON, err := encodingConfig.Codec.MarshalJSON(skuGenesis)
	require.NoError(t, err)
	document := fmt.Sprintf(
		`{"chain_id":"manifest-test","initial_height":4321,"genesis_time":"2030-01-02T03:04:05Z","app_state":{"billing":%s,"bank":%s,"sku":%s}}`,
		billingJSON,
		bankJSON,
		skuJSON,
	)

	var output bytes.Buffer
	require.NoError(t, writeBillingMigrationPreflight(
		encodingConfig.Codec,
		bytes.NewBufferString(document),
		&output,
	))
	expected := fmt.Sprintf(`{
  "schema_version": 3,
  "source_chain_id": "manifest-test",
  "source_initial_height": 4321,
  "input_genesis_time": "2030-01-02T03:04:05Z",
  "billing_state": "pre_v4_aggregate",
  "provider_count": 1,
  "blocked_provider_count": 0,
  "blocked_providers": [],
  "payout_credit_collision_count": 1,
  "payout_credit_collisions": [
    {
      "lease_uuid": %q,
      "provider_uuid": %q,
      "tenant": %q,
      "credit_address": %q,
      "state": "LEASE_STATE_ACTIVE"
    }
  ],
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
}`, leaseUUID, providerUUID, tenant.String(), creditAddress.String(), tenant.String(), creditAddress.String(), leaseUUID) + "\n"
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
			name:     "missing sku",
			document: `{"chain_id":"manifest-test","genesis_time":"2030-01-02T03:04:05Z","app_state":{"billing":{},"bank":{}}}`,
			contains: `missing "sku" module`,
		},
		{
			name:     "null sku",
			document: `{"chain_id":"manifest-test","genesis_time":"2030-01-02T03:04:05Z","app_state":{"billing":{},"bank":{},"sku":null}}`,
			contains: `missing "sku" module`,
		},
		{
			name:     "malformed sku",
			document: `{"chain_id":"manifest-test","genesis_time":"2030-01-02T03:04:05Z","app_state":{"billing":{},"bank":{},"sku":{"providers":"invalid"}}}`,
			contains: "decode sku genesis for billing migration preflight",
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

func TestAuditProviderPayoutsUsesBankPolicyAndSourceLeaseStates(t *testing.T) {
	const (
		distributionUUID = "01912345-6789-7abc-8def-0123456789b0"
		governanceUUID   = "01912345-6789-7abc-8def-0123456789b1"
		allowedUUID      = "01912345-6789-7abc-8def-0123456789b2"
		leaseUUID1       = "01912345-6789-7abc-8def-0123456789c0"
		leaseUUID2       = "01912345-6789-7abc-8def-0123456789c1"
		leaseUUID3       = "01912345-6789-7abc-8def-0123456789c2"
	)
	distribution := authtypes.NewModuleAddress(distrtypes.ModuleName).String()
	governance := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	allowed := sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String()
	policy := app.BlockedAddresses()
	require.True(t, policy[distribution])
	// The audit follows the application's receiving policy, including its
	// governance exemption, rather than blocking every module account.
	require.False(t, policy[governance])
	require.False(t, policy[allowed])
	providers := []skutypes.Provider{
		{Uuid: allowedUUID, PayoutAddress: allowed, Active: true},
		{Uuid: governanceUUID, PayoutAddress: governance, Active: false},
		{Uuid: distributionUUID, PayoutAddress: strings.ToUpper(distribution), Active: true, ApiUrl: "https://:443"},
	}
	leases := []billingtypes.Lease{
		{Uuid: leaseUUID3, ProviderUuid: distributionUUID, State: billingtypes.LEASE_STATE_ACTIVE},
		{Uuid: leaseUUID2, ProviderUuid: distributionUUID, State: billingtypes.LEASE_STATE_PENDING},
		{Uuid: leaseUUID1, ProviderUuid: distributionUUID, State: billingtypes.LEASE_STATE_ACTIVE},
		{Uuid: "closed", ProviderUuid: distributionUUID, State: billingtypes.LEASE_STATE_CLOSED},
		{Uuid: "expired", ProviderUuid: distributionUUID, State: billingtypes.LEASE_STATE_EXPIRED},
		{Uuid: "allowed", ProviderUuid: allowedUUID, State: billingtypes.LEASE_STATE_ACTIVE},
	}
	expected := []blockedProviderPayoutPreflight{
		{
			ProviderUUID: distributionUUID, PayoutAddress: distribution, Active: true,
			ActiveLeaseUUIDs: []string{leaseUUID1, leaseUUID3}, PendingLeaseUUIDs: []string{leaseUUID2},
		},
	}
	originalProviders := slices.Clone(providers)
	originalLeases := slices.Clone(leases)
	actual, err := auditProviderPayouts(providers, leases)
	require.NoError(t, err)
	require.Equal(t, expected, actual)
	require.Equal(t, originalProviders, providers)
	require.Equal(t, originalLeases, leases)
	slices.Reverse(providers)
	slices.Reverse(leases)
	actual, err = auditProviderPayouts(providers, leases)
	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

func TestWriteBillingMigrationPreflightReportsBlockedPayoutWithoutChangingExport(t *testing.T) {
	encodingConfig := params.MakeEncodingConfig()
	billingJSON, err := encodingConfig.Codec.MarshalJSON(billingtypes.DefaultGenesis())
	require.NoError(t, err)
	bankJSON, err := encodingConfig.Codec.MarshalJSON(banktypes.DefaultGenesisState())
	require.NoError(t, err)
	distribution := authtypes.NewModuleAddress(distrtypes.ModuleName).String()
	skuGenesis := skutypes.DefaultGenesis()
	skuGenesis.Providers = []skutypes.Provider{{
		Uuid: "01912345-6789-7abc-8def-0123456789b0", Address: sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String(),
		PayoutAddress: strings.ToUpper(distribution), Active: false, ApiUrl: "https://:443",
	}}
	skuGenesis.ProviderSequence = 1
	// Both blocked payouts and historical URL metadata remain importable.
	require.NoError(t, skuGenesis.Validate())
	skuJSON, err := encodingConfig.Codec.MarshalJSON(skuGenesis)
	require.NoError(t, err)
	document := fmt.Sprintf(
		`{"chain_id":"manifest-test","initial_height":4321,"genesis_time":"2030-01-02T03:04:05Z","app_state":{"billing":%s,"bank":%s,"sku":%s}}`,
		billingJSON, bankJSON, skuJSON,
	)
	exportRoot, err := os.OpenRoot(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, exportRoot.Close()) })
	require.NoError(t, exportRoot.WriteFile("export.json", []byte(document), 0o600))
	input, err := exportRoot.Open("export.json")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, input.Close()) })
	var output bytes.Buffer
	require.NoError(t, writeBillingMigrationPreflight(encodingConfig.Codec, input, &output))
	var report billingMigrationPreflightOutput
	require.NoError(t, json.Unmarshal(output.Bytes(), &report))
	require.EqualValues(t, 3, report.SchemaVersion)
	require.EqualValues(t, 1, report.ProviderCount)
	require.EqualValues(t, 1, report.BlockedProviderCount)
	require.Equal(t, []blockedProviderPayoutPreflight{{
		ProviderUUID: skuGenesis.Providers[0].Uuid, PayoutAddress: distribution, Active: false,
		ActiveLeaseUUIDs: []string{}, PendingLeaseUUIDs: []string{},
	}}, report.BlockedProviders)
	unchanged, err := exportRoot.ReadFile("export.json")
	require.NoError(t, err)
	require.Equal(t, document, string(unchanged))
}

func TestWriteBillingMigrationPreflightRejectsAmbiguousPayoutAudit(t *testing.T) {
	encodingConfig := params.MakeEncodingConfig()
	provider := skutypes.Provider{
		Uuid: "01912345-6789-7abc-8def-0123456789b0", PayoutAddress: authtypes.NewModuleAddress(distrtypes.ModuleName).String(),
	}
	tests := []struct {
		name      string
		providers []skutypes.Provider
		contains  string
	}{
		{name: "duplicate UUID", providers: []skutypes.Provider{provider, provider}, contains: "duplicate provider UUID"},
		{
			name: "uppercase UUID", providers: []skutypes.Provider{{Uuid: strings.ToUpper(provider.Uuid), PayoutAddress: provider.PayoutAddress}},
			contains: "invalid provider UUID",
		},
		{
			name: "invalid payout", providers: []skutypes.Provider{provider, {Uuid: "01912345-6789-7abc-8def-0123456789b1", PayoutAddress: "invalid"}},
			contains: "invalid payout address",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			skuJSON, err := encodingConfig.Codec.MarshalJSON(&skutypes.GenesisState{Providers: tt.providers})
			require.NoError(t, err)
			document := fmt.Sprintf(
				`{"chain_id":"manifest-test","genesis_time":"2030-01-02T03:04:05Z","app_state":{"billing":{},"bank":{},"sku":%s}}`, skuJSON,
			)
			var output bytes.Buffer
			err = writeBillingMigrationPreflight(encodingConfig.Codec, strings.NewReader(document), &output)
			require.ErrorContains(t, err, tt.contains)
			require.Empty(t, output.String())
		})
	}
}

func TestAuditProviderCreditCollisionsMatchesProviderAndTenantWithoutMutation(t *testing.T) {
	const (
		providerUUID1 = "01912345-6789-7abc-8def-0123456789b0"
		providerUUID2 = "01912345-6789-7abc-8def-0123456789b1"
		leaseUUID1    = "01912345-6789-7abc-8def-0123456789c0"
		leaseUUID2    = "01912345-6789-7abc-8def-0123456789c1"
	)
	tenant1 := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	tenant2 := sdk.AccAddress(bytes.Repeat([]byte{2}, 20))
	credit1 := billingtypes.DeriveCreditAddress(tenant1)
	providers := []skutypes.Provider{
		{Uuid: providerUUID2, PayoutAddress: tenant2.String(), Active: true},
		{Uuid: providerUUID1, PayoutAddress: strings.ToUpper(credit1.String()), Active: false},
	}
	leases := []billingtypes.Lease{
		{Uuid: leaseUUID2, ProviderUuid: providerUUID1, Tenant: tenant1.String(), State: billingtypes.LEASE_STATE_PENDING},
		{Uuid: leaseUUID1, ProviderUuid: providerUUID1, Tenant: strings.ToUpper(tenant1.String()), State: billingtypes.LEASE_STATE_ACTIVE},
		{Uuid: "other-tenant", ProviderUuid: providerUUID1, Tenant: tenant2.String(), State: billingtypes.LEASE_STATE_ACTIVE},
		{Uuid: "other-provider", ProviderUuid: providerUUID2, Tenant: tenant1.String(), State: billingtypes.LEASE_STATE_ACTIVE},
	}
	for _, state := range []billingtypes.LeaseState{
		billingtypes.LEASE_STATE_CLOSED, billingtypes.LEASE_STATE_EXPIRED,
		billingtypes.LEASE_STATE_REJECTED,
	} {
		leases = append(leases, billingtypes.Lease{Uuid: state.String(), ProviderUuid: providerUUID1, Tenant: tenant1.String(), State: state})
	}
	expected := []providerCreditCollisionPreflight{
		{LeaseUUID: leaseUUID1, ProviderUUID: providerUUID1, Tenant: tenant1.String(), CreditAddress: credit1.String(), State: "LEASE_STATE_ACTIVE"},
		{LeaseUUID: leaseUUID2, ProviderUUID: providerUUID1, Tenant: tenant1.String(), CreditAddress: credit1.String(), State: "LEASE_STATE_PENDING"},
	}
	originalProviders := slices.Clone(providers)
	originalLeases := slices.Clone(leases)
	actual, err := auditProviderCreditCollisions(providers, leases)
	require.NoError(t, err)
	require.Equal(t, expected, actual)
	require.Equal(t, originalProviders, providers)
	require.Equal(t, originalLeases, leases)
	slices.Reverse(providers)
	slices.Reverse(leases)
	actual, err = auditProviderCreditCollisions(providers, leases)
	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

func TestWriteBillingMigrationPreflightRejectsMalformedTenantWithoutPartialAudit(t *testing.T) {
	encodingConfig := params.MakeEncodingConfig()
	const (
		providerUUID           = "01912345-6789-7abc-8def-0123456789b0"
		invalidTenantLeaseUUID = "01912345-6789-7abc-8def-0123456789c1"
	)
	tenant := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	skuJSON, err := encodingConfig.Codec.MarshalJSON(&skutypes.GenesisState{
		Providers: []skutypes.Provider{{Uuid: providerUUID, PayoutAddress: billingtypes.DeriveCreditAddress(tenant).String()}},
	})
	require.NoError(t, err)
	for _, state := range []billingtypes.LeaseState{billingtypes.LEASE_STATE_ACTIVE, billingtypes.LEASE_STATE_PENDING} {
		t.Run(state.String(), func(t *testing.T) {
			billingJSON, err := encodingConfig.Codec.MarshalJSON(&billingtypes.GenesisState{Leases: []billingtypes.Lease{
				{Uuid: "01912345-6789-7abc-8def-0123456789c0", ProviderUuid: providerUUID, Tenant: tenant.String(), State: state},
				{Uuid: invalidTenantLeaseUUID, ProviderUuid: providerUUID, Tenant: "invalid", State: state},
			}})
			require.NoError(t, err)
			document := fmt.Sprintf(
				`{"chain_id":"manifest-test","genesis_time":"2030-01-02T03:04:05Z","app_state":{"billing":%s,"bank":{},"sku":%s}}`, billingJSON, skuJSON,
			)
			var output bytes.Buffer
			err = writeBillingMigrationPreflight(encodingConfig.Codec, strings.NewReader(document), &output)
			require.ErrorContains(t, err, fmt.Sprintf("audit provider credit collisions: lease %s has invalid tenant:", invalidTenantLeaseUUID))
			require.Empty(t, output.String())
		})
	}
}
