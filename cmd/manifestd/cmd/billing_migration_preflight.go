package cmd

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	"github.com/manifest-network/manifest-ledger/app"
	pkguuid "github.com/manifest-network/manifest-ledger/pkg/uuid"
	billingkeeper "github.com/manifest-network/manifest-ledger/x/billing/keeper"
	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

const (
	billingMigrationPreflightSchemaVersion = 2
	jsonNull                               = "null"
)

// billingMigrationPreflightOutput is the stable CLI JSON envelope. Source
// document provenance stays here rather than coupling the keeper's pure
// reservation preview to the SDK AppGenesis type.
type billingMigrationPreflightOutput struct {
	SchemaVersion                    uint32                                              `json:"schema_version"`
	SourceChainID                    string                                              `json:"source_chain_id"`
	SourceInitialHeight              int64                                               `json:"source_initial_height"`
	InputGenesisTime                 string                                              `json:"input_genesis_time"`
	BillingState                     string                                              `json:"billing_state"`
	ProviderCount                    uint64                                              `json:"provider_count"`
	BlockedProviderCount             uint64                                              `json:"blocked_provider_count"`
	BlockedProviders                 []blockedProviderPayoutPreflight                    `json:"blocked_providers"`
	ReservationChangeTenantCount     uint64                                              `json:"reservation_change_tenant_count"`
	ExpiringModernPendingTenantCount uint64                                              `json:"expiring_modern_pending_tenant_count"`
	ExpiringModernPendingLeaseCount  uint64                                              `json:"expiring_modern_pending_lease_count"`
	Tenants                          []billingkeeper.ReservationMigrationTenantPreflight `json:"tenants"`
}

// blockedProviderPayoutPreflight describes source-state exposure to the target
// binary's bank policy. Lease lists are not predictions of migration outcomes.
type blockedProviderPayoutPreflight struct {
	ProviderUUID      string   `json:"provider_uuid"`
	PayoutAddress     string   `json:"payout_address"`
	Active            bool     `json:"active"`
	ActiveLeaseUUIDs  []string `json:"active_lease_uuids"`
	PendingLeaseUUIDs []string `json:"pending_lease_uuids"`
}

func newBillingMigrationPreflightCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "preflight-billing-v4 [exported-genesis.json]",
		Short: "Predict the billing v4 reservation cutover from an exported genesis",
		Long: `Read an exported genesis without opening application state and emit a
deterministic JSON report of every billing tenant's pre/post aggregates,
modern ACTIVE allocations, opaque legacy-cohort allocation, and modern PENDING
lease UUIDs that the v4 reservation migration would expire. The report also
audits every SKU provider's payout against this binary's blocked bank addresses
and lists its source-state ACTIVE and PENDING leases when blocked.

The report is specific to the exported snapshot. The command never writes the
genesis file or application state. Blocked payouts are reported without failing
the command: operators must require blocked_provider_count == 0 before upgrade.
The report does not certify block-time validation, SKU references, or full
InitGenesis. Billing, bank, and SKU genesis modules are required.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)
			if clientCtx.Codec == nil {
				return fmt.Errorf("billing migration preflight requires an application codec")
			}

			file, err := os.Open(args[0])
			if err != nil {
				return fmt.Errorf("open exported genesis %q: %w", args[0], err)
			}
			defer func() { _ = file.Close() }()

			return writeBillingMigrationPreflight(clientCtx.Codec, file, cmd.OutOrStdout())
		},
	}
	return cmd
}

func writeBillingMigrationPreflight(cdc codec.JSONCodec, input io.Reader, output io.Writer) error {
	decoder := json.NewDecoder(input)
	var document genutiltypes.AppGenesis
	if err := decoder.Decode(&document); err != nil {
		return fmt.Errorf("decode exported genesis document: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decode exported genesis document: unexpected trailing JSON value")
		}
		return fmt.Errorf("decode exported genesis document trailer: %w", err)
	}
	if document.GenesisTime.IsZero() {
		return fmt.Errorf("exported genesis has missing or zero genesis_time")
	}
	if document.ChainID == "" {
		return fmt.Errorf("exported genesis has missing chain_id")
	}
	if len(document.ChainID) > genutiltypes.MaxChainIDLen {
		return fmt.Errorf(
			"exported genesis chain_id exceeds maximum length %d",
			genutiltypes.MaxChainIDLen,
		)
	}
	if document.InitialHeight < 0 {
		return fmt.Errorf("exported genesis has negative initial_height %d", document.InitialHeight)
	}
	if len(document.AppState) == 0 || string(document.AppState) == jsonNull {
		return fmt.Errorf("exported genesis has missing app_state")
	}

	var appState map[string]json.RawMessage
	if err := json.Unmarshal(document.AppState, &appState); err != nil {
		return fmt.Errorf("decode exported genesis app_state: %w", err)
	}
	billingJSON, ok := appState[billingtypes.ModuleName]
	if !ok || len(billingJSON) == 0 || string(billingJSON) == jsonNull {
		return fmt.Errorf("exported genesis app_state has missing %q module", billingtypes.ModuleName)
	}
	bankJSON, ok := appState[banktypes.ModuleName]
	if !ok || len(bankJSON) == 0 || string(bankJSON) == jsonNull {
		return fmt.Errorf("exported genesis app_state has missing %q module", banktypes.ModuleName)
	}
	skuJSON, ok := appState[skutypes.ModuleName]
	if !ok || len(skuJSON) == 0 || string(skuJSON) == jsonNull {
		return fmt.Errorf("exported genesis app_state has missing %q module", skutypes.ModuleName)
	}

	var billingGenesis billingtypes.GenesisState
	if err := cdc.UnmarshalJSON(billingJSON, &billingGenesis); err != nil {
		return fmt.Errorf("decode billing genesis for migration preflight: %w", err)
	}
	var bankGenesis banktypes.GenesisState
	if err := cdc.UnmarshalJSON(bankJSON, &bankGenesis); err != nil {
		return fmt.Errorf("decode bank genesis for billing migration preflight: %w", err)
	}
	var skuGenesis skutypes.GenesisState
	if err := cdc.UnmarshalJSON(skuJSON, &skuGenesis); err != nil {
		return fmt.Errorf("decode sku genesis for billing migration preflight: %w", err)
	}
	blockedProviders, err := auditProviderPayouts(skuGenesis.Providers, billingGenesis.Leases)
	if err != nil {
		return err
	}

	reservationReport, err := billingkeeper.BuildReservationMigrationPreflight(
		document.GenesisTime,
		&billingGenesis,
		&bankGenesis,
	)
	if err != nil {
		return err
	}

	report := billingMigrationPreflightOutput{
		SchemaVersion:                    billingMigrationPreflightSchemaVersion,
		SourceChainID:                    document.ChainID,
		SourceInitialHeight:              document.InitialHeight,
		InputGenesisTime:                 document.GenesisTime.UTC().Format(time.RFC3339Nano),
		BillingState:                     reservationReport.BillingState,
		ProviderCount:                    uint64(len(skuGenesis.Providers)),
		BlockedProviderCount:             uint64(len(blockedProviders)),
		BlockedProviders:                 blockedProviders,
		ReservationChangeTenantCount:     reservationReport.ReservationChangeTenantCount,
		ExpiringModernPendingTenantCount: reservationReport.ExpiringModernPendingTenantCount,
		ExpiringModernPendingLeaseCount:  reservationReport.ExpiringModernPendingLeaseCount,
		Tenants:                          reservationReport.Tenants,
	}

	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("encode billing migration preflight report: %w", err)
	}
	return nil
}

func auditProviderPayouts(providers []skutypes.Provider, leases []billingtypes.Lease) ([]blockedProviderPayoutPreflight, error) {
	blockedAddresses := app.BlockedAddresses()
	blocked := make([]blockedProviderPayoutPreflight, 0)
	seen := make(map[string]bool, len(providers))
	blockedByUUID := make(map[string]int)
	for _, provider := range providers {
		if err := pkguuid.ValidateUUIDv7(provider.Uuid); err != nil {
			return nil, fmt.Errorf("audit provider payouts: invalid provider UUID %q: %w", provider.Uuid, err)
		}
		if seen[provider.Uuid] {
			return nil, fmt.Errorf("audit provider payouts: duplicate provider UUID %q", provider.Uuid)
		}
		seen[provider.Uuid] = true
		payoutAddress, err := sdk.AccAddressFromBech32(provider.PayoutAddress)
		if err != nil {
			return nil, fmt.Errorf("audit provider payouts: provider %s has invalid payout address: %w", provider.Uuid, err)
		}
		// Normalize historical Bech32 aliases before looking up the policy map.
		// Do not run message validation: historical metadata and blocked payouts
		// remain importable, and the latter must appear as actionable findings.
		if !blockedAddresses[payoutAddress.String()] {
			continue
		}
		blockedByUUID[provider.Uuid] = len(blocked)
		blocked = append(blocked, blockedProviderPayoutPreflight{
			ProviderUUID:      provider.Uuid,
			PayoutAddress:     payoutAddress.String(),
			Active:            provider.Active,
			ActiveLeaseUUIDs:  []string{},
			PendingLeaseUUIDs: []string{},
		})
	}
	for _, lease := range leases {
		i, found := blockedByUUID[lease.ProviderUuid]
		if !found {
			continue
		}
		switch lease.State {
		case billingtypes.LEASE_STATE_ACTIVE:
			blocked[i].ActiveLeaseUUIDs = append(blocked[i].ActiveLeaseUUIDs, lease.Uuid)
		case billingtypes.LEASE_STATE_PENDING:
			blocked[i].PendingLeaseUUIDs = append(blocked[i].PendingLeaseUUIDs, lease.Uuid)
		}
	}
	for i := range blocked {
		slices.Sort(blocked[i].ActiveLeaseUUIDs)
		slices.Sort(blocked[i].PendingLeaseUUIDs)
	}
	slices.SortFunc(blocked, func(a, b blockedProviderPayoutPreflight) int {
		return cmp.Compare(a.ProviderUUID, b.ProviderUUID)
	})
	return blocked, nil
}
