package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	billingkeeper "github.com/manifest-network/manifest-ledger/x/billing/keeper"
	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
)

const (
	billingMigrationPreflightSchemaVersion = 1
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
	ReservationChangeTenantCount     uint64                                              `json:"reservation_change_tenant_count"`
	ExpiringModernPendingTenantCount uint64                                              `json:"expiring_modern_pending_tenant_count"`
	ExpiringModernPendingLeaseCount  uint64                                              `json:"expiring_modern_pending_lease_count"`
	Tenants                          []billingkeeper.ReservationMigrationTenantPreflight `json:"tenants"`
}

func newBillingMigrationPreflightCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "preflight-billing-v4 [exported-genesis.json]",
		Short: "Predict the billing v4 reservation cutover from an exported genesis",
		Long: `Read an exported genesis without opening application state and emit a
deterministic JSON report of every billing tenant's pre/post aggregates,
modern ACTIVE allocations, opaque legacy-cohort allocation, and modern PENDING
lease UUIDs that the v4 reservation migration would expire.

The report is specific to the exported snapshot. The command never writes the
genesis file or application state. It previews reservation migration only; it
does not certify block-time validation, SKU references, or full InitGenesis.`,
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

	var billingGenesis billingtypes.GenesisState
	if err := cdc.UnmarshalJSON(billingJSON, &billingGenesis); err != nil {
		return fmt.Errorf("decode billing genesis for migration preflight: %w", err)
	}
	var bankGenesis banktypes.GenesisState
	if err := cdc.UnmarshalJSON(bankJSON, &bankGenesis); err != nil {
		return fmt.Errorf("decode bank genesis for billing migration preflight: %w", err)
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
