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
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	"github.com/manifest-network/manifest-ledger/app"
	pkguuid "github.com/manifest-network/manifest-ledger/pkg/uuid"
	billingkeeper "github.com/manifest-network/manifest-ledger/x/billing/keeper"
	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

const (
	billingMigrationPreflightSchemaVersion = 6
	jsonNull                               = "null"
)

// billingMigrationPreflightOutput is the stable CLI JSON envelope. Source
// document provenance stays here rather than coupling the keeper's pure
// reservation preview to the SDK AppGenesis type.
type billingMigrationPreflightOutput struct {
	SchemaVersion                    uint32                                              `json:"schema_version"`
	SourceChainID                    string                                              `json:"source_chain_id"`
	SourceInitialHeight              int64                                               `json:"source_initial_height"`
	PlannerTime                      string                                              `json:"planner_time"`
	InputGenesisTime                 string                                              `json:"input_genesis_time"`
	BillingState                     string                                              `json:"billing_state"`
	MigrationPath                    string                                              `json:"migration_path"`
	ProviderCount                    uint64                                              `json:"provider_count"`
	BlockedProviderCount             uint64                                              `json:"blocked_provider_count"`
	BlockedProviders                 []blockedProviderPayoutPreflight                    `json:"blocked_providers"`
	PayoutCreditCollisionCount       uint64                                              `json:"payout_credit_collision_count"`
	PayoutCreditCollisions           []providerCreditCollisionPreflight                  `json:"payout_credit_collisions"`
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

// providerCreditCollisionPreflight identifies a live source lease whose payout
// would send credit back to the same tenant credit account.
type providerCreditCollisionPreflight struct {
	LeaseUUID     string `json:"lease_uuid"`
	ProviderUUID  string `json:"provider_uuid"`
	Tenant        string `json:"tenant"`
	CreditAddress string `json:"credit_address"`
	State         string `json:"state"`
}

func newBillingMigrationPreflightCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "preflight-billing-v4 [exported-genesis.json]",
		Short: "Preview the sequential billing v2-to-v4 reservation upgrade",
		Long: `Read an exported genesis without opening application state and emit a
deterministic JSON report of every billing tenant's pre/post aggregates,
modern ACTIVE allocations, opaque legacy-cohort allocation, and modern PENDING
lease UUIDs that the v4 reservation migration would expire. The report also
audits every SKU provider's payout against this binary's blocked bank addresses
and lists its source-state ACTIVE and PENDING leases when blocked. A separate
report identifies ACTIVE and PENDING leases whose provider payout equals the
tenant's derived credit address.

Aggregate-only exports are interpreted as billing v2 state and preview the
sequential v2-to-v3 repair followed by the v3-to-v4 cutover. The JSON
migration_path is v2_to_v3_to_v4. This command does not preview a direct v3-to-v4
upgrade: v2 and v3 exports have the same format but different repair semantics.
Already-v4 reservations are audited without migration (migration_path: none).

The report is specific to the exported snapshot. The command never writes the
genesis file or application state. Payout findings do not fail the command:
operators must require both blocked_provider_count == 0 and
payout_credit_collision_count == 0 before upgrade.
The report does not certify block-time validation, SKU references, or full
InitGenesis. Billing, bank, auth, and SKU genesis modules are required.

--at is required and specifies the RFC3339 time at which to evaluate the cutover
and vesting locks. Use the intended cutover block time, or the exported block's
time for a snapshot audit. The document's genesis_time may be the original
chain start time and is not used as the valuation time. bank_balance is total
bank funds; spendable_balance excludes coins still locked at --at and is the
reservation planner's backing input. Vesting schedules can change the result
at a later cutover time.`,
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

			at, err := cmd.Flags().GetString("at")
			if err != nil {
				return err
			}
			plannerTime, err := time.Parse(time.RFC3339Nano, at)
			if err != nil || plannerTime.IsZero() {
				return fmt.Errorf("--at requires a non-zero RFC3339 cutover or snapshot time")
			}
			return writeBillingMigrationPreflight(clientCtx.Codec, file, cmd.OutOrStdout(), plannerTime)
		},
	}
	cmd.Flags().String("at", "", "RFC3339 cutover or snapshot time for reservation and vesting-lock evaluation (required)")
	_ = cmd.MarkFlagRequired("at")
	return cmd
}

func writeBillingMigrationPreflight(cdc codec.JSONCodec, input io.Reader, output io.Writer, plannerTime time.Time) error {
	if plannerTime.IsZero() {
		return fmt.Errorf("billing migration preflight requires an explicit non-zero planner time")
	}
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

	authJSON, ok := appState[authtypes.ModuleName]
	if !ok || len(authJSON) == 0 || string(authJSON) == jsonNull {
		return fmt.Errorf("exported genesis app_state has missing %q module", authtypes.ModuleName)
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
	authAccounts, err := decodePreflightAuthAccounts(cdc, authJSON)
	if err != nil {
		return err
	}

	blockedProviders, err := auditProviderPayouts(skuGenesis.Providers, billingGenesis.Leases)
	if err != nil {
		return err
	}
	creditCollisions, err := auditProviderCreditCollisions(skuGenesis.Providers, billingGenesis.Leases)
	if err != nil {
		return err
	}

	reservationReport, err := billingkeeper.BuildReservationMigrationPreflight(
		plannerTime,
		&billingGenesis,
		&bankGenesis,
		authAccounts,
	)
	if err != nil {
		return err
	}

	report := billingMigrationPreflightOutput{
		SchemaVersion:                    billingMigrationPreflightSchemaVersion,
		SourceChainID:                    document.ChainID,
		SourceInitialHeight:              document.InitialHeight,
		PlannerTime:                      plannerTime.UTC().Format(time.RFC3339Nano),
		InputGenesisTime:                 document.GenesisTime.UTC().Format(time.RFC3339Nano),
		BillingState:                     reservationReport.BillingState,
		MigrationPath:                    reservationReport.MigrationPath,
		ProviderCount:                    uint64(len(skuGenesis.Providers)),
		BlockedProviderCount:             uint64(len(blockedProviders)),
		BlockedProviders:                 blockedProviders,
		PayoutCreditCollisionCount:       uint64(len(creditCollisions)),
		PayoutCreditCollisions:           creditCollisions,
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

func auditProviderCreditCollisions(providers []skutypes.Provider, leases []billingtypes.Lease) ([]providerCreditCollisionPreflight, error) {
	payoutsByProvider := make(map[string]sdk.AccAddress, len(providers))
	for _, provider := range providers {
		payout, err := sdk.AccAddressFromBech32(provider.PayoutAddress)
		if err != nil {
			return nil, fmt.Errorf("audit provider credit collisions: provider %s has invalid payout address: %w", provider.Uuid, err)
		}
		payoutsByProvider[provider.Uuid] = payout
	}
	collisions := make([]providerCreditCollisionPreflight, 0)
	for _, lease := range leases {
		if lease.State != billingtypes.LEASE_STATE_ACTIVE && lease.State != billingtypes.LEASE_STATE_PENDING {
			continue
		}
		tenant, err := sdk.AccAddressFromBech32(lease.Tenant)
		if err != nil {
			return nil, fmt.Errorf("audit provider credit collisions: lease %s has invalid tenant: %w", lease.Uuid, err)
		}
		payout, found := payoutsByProvider[lease.ProviderUuid]
		if !found {
			continue
		}
		creditAddress := billingtypes.DeriveCreditAddress(tenant)
		if !creditAddress.Equals(payout) {
			continue
		}
		collisions = append(collisions, providerCreditCollisionPreflight{
			LeaseUUID:     lease.Uuid,
			ProviderUUID:  lease.ProviderUuid,
			Tenant:        tenant.String(),
			CreditAddress: creditAddress.String(),
			State:         lease.State.String(),
		})
	}
	slices.SortFunc(collisions, func(a, b providerCreditCollisionPreflight) int {
		return cmp.Compare(a.LeaseUUID, b.LeaseUUID)
	})
	return collisions, nil
}

// decodePreflightAuthAccounts contains SDK Any decoding at an offline input
// boundary. Null or structurally incomplete accounts must fail the report rather
// than crash the command; this recovery never runs in consensus message paths.
func decodePreflightAuthAccounts(cdc codec.JSONCodec, input []byte) (accounts authtypes.GenesisAccounts, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			accounts = nil
			err = fmt.Errorf("invalid auth genesis for billing migration preflight: %v", recovered)
		}
	}()
	var genesis authtypes.GenesisState
	if err := cdc.UnmarshalJSON(input, &genesis); err != nil {
		return nil, fmt.Errorf("decode auth genesis for billing migration preflight: %w", err)
	}
	for index, account := range genesis.Accounts {
		if account == nil {
			return nil, fmt.Errorf("auth genesis contains null account at index %d", index)
		}
	}
	accounts, err = authtypes.UnpackAccounts(genesis.Accounts)
	if err != nil {
		return nil, fmt.Errorf("unpack auth accounts for billing migration preflight: %w", err)
	}
	return accounts, nil
}
