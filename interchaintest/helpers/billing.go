package helpers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"

	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
)

// LeaseItemJSON is a JSON-compatible version of LeaseItem.
// Quantity is a string in CLI JSON output.
type LeaseItemJSON struct {
	SkuUuid     string   `json:"sku_uuid,omitempty"`
	Quantity    string   `json:"quantity,omitempty"`
	LockedPrice sdk.Coin `json:"locked_price"`
	ServiceName string   `json:"service_name,omitempty"`
}

// LeaseJSON is a JSON-compatible version of Lease.
// State is output as a string by the CLI (e.g., "LEASE_STATE_ACTIVE"),
// not as the numeric enum value that the proto type expects.
type LeaseJSON struct {
	Uuid            string          `json:"uuid,omitempty"`
	Tenant          string          `json:"tenant,omitempty"`
	ProviderUuid    string          `json:"provider_uuid,omitempty"`
	Items           []LeaseItemJSON `json:"items"`
	State           string          `json:"state,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	ClosedAt        *time.Time      `json:"closed_at,omitempty"`
	LastSettledAt   time.Time       `json:"last_settled_at"`
	AcknowledgedAt  *time.Time      `json:"acknowledged_at,omitempty"`
	RejectedAt      *time.Time      `json:"rejected_at,omitempty"`
	RejectionReason string          `json:"rejection_reason,omitempty"`
	ExpiredAt       *time.Time      `json:"expired_at,omitempty"`
	ClosureReason   string          `json:"closure_reason,omitempty"`
	MetaHash        []byte          `json:"meta_hash,omitempty"`
	CustomDomain    string          `json:"custom_domain,omitempty"`
}

// GetState returns the LeaseState enum value from the string state.
func (l *LeaseJSON) GetState() billingtypes.LeaseState {
	switch l.State {
	case "LEASE_STATE_PENDING":
		return billingtypes.LEASE_STATE_PENDING
	case "LEASE_STATE_ACTIVE":
		return billingtypes.LEASE_STATE_ACTIVE
	case "LEASE_STATE_CLOSED":
		return billingtypes.LEASE_STATE_CLOSED
	case "LEASE_STATE_REJECTED":
		return billingtypes.LEASE_STATE_REJECTED
	case "LEASE_STATE_EXPIRED":
		return billingtypes.LEASE_STATE_EXPIRED
	default:
		return billingtypes.LEASE_STATE_UNSPECIFIED
	}
}

// GetClosureReason returns the closure reason for the lease.
func (l *LeaseJSON) GetClosureReason() string {
	return l.ClosureReason
}

// Billing transaction helpers

// BillingFundCredit funds a tenant's credit account.
func BillingFundCredit(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, tenant, amount string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "billing", "fund-credit", tenant, amount}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// BillingCreateLease creates a new lease with the specified SKU items.
// items should be in the format "sku_uuid:quantity[:service_name]" (e.g., "uuid1:2", "uuid2:1:web")
func BillingCreateLease(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, items []string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "billing", "create-lease"}
	cmd = append(cmd, items...)
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// BillingCreateLeaseWithMetaHash creates a new lease with the specified SKU items and meta_hash.
// items should be in the format "sku_uuid:quantity[:service_name]" (e.g., "uuid1:2", "uuid2:1:web")
// metaHash should be a hex-encoded string (e.g., "a1b2c3d4...")
func BillingCreateLeaseWithMetaHash(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, items []string, metaHash string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "billing", "create-lease"}
	cmd = append(cmd, items...)
	if metaHash != "" {
		cmd = append(cmd, "--meta-hash", metaHash)
	}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// BillingCreateLeaseForTenant creates a new lease on behalf of a tenant (authority only).
// items should be in the format "sku_uuid:quantity[:service_name]" (e.g., "uuid1:2", "uuid2:1:web")
func BillingCreateLeaseForTenant(ctx context.Context, chain *cosmos.CosmosChain, authority ibc.Wallet, tenant string, items []string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "billing", "create-lease-for-tenant", tenant}
	cmd = append(cmd, items...)
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, authority.KeyName(), flags...))
}

// BillingCloseLease closes an active lease.
func BillingCloseLease(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, leaseID string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "billing", "close-lease", leaseID}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// BillingCloseLeaseWithReason closes an active lease with a closure reason.
func BillingCloseLeaseWithReason(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, leaseID, reason string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "billing", "close-lease", leaseID}
	if reason != "" {
		cmd = append(cmd, "--reason", reason)
	}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// BillingCloseLeases closes multiple active leases atomically.
func BillingCloseLeases(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, leaseUUIDs []string, flags ...string) (sdk.TxResponse, error) {
	cmd := append([]string{"tx", "billing", "close-lease"}, leaseUUIDs...)
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// BillingCloseLeasesWithReason closes multiple active leases atomically with a closure reason.
func BillingCloseLeasesWithReason(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, leaseUUIDs []string, reason string, flags ...string) (sdk.TxResponse, error) {
	cmd := append([]string{"tx", "billing", "close-lease"}, leaseUUIDs...)
	if reason != "" {
		cmd = append(cmd, "--reason", reason)
	}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// BillingAcknowledgeLease acknowledges a pending lease (provider only).
func BillingAcknowledgeLease(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, leaseUUID string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "billing", "acknowledge-lease", leaseUUID}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// BillingAcknowledgeLeases acknowledges multiple pending leases atomically (provider only).
func BillingAcknowledgeLeases(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, leaseUUIDs []string, flags ...string) (sdk.TxResponse, error) {
	cmd := append([]string{"tx", "billing", "acknowledge-lease"}, leaseUUIDs...)
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// BillingRejectLease rejects a pending lease (provider only).
func BillingRejectLease(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, leaseUUID string, reason string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "billing", "reject-lease", leaseUUID}
	if reason != "" {
		cmd = append(cmd, "--reason", reason)
	}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// BillingRejectLeases rejects multiple pending leases atomically (provider only).
func BillingRejectLeases(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, leaseUUIDs []string, reason string, flags ...string) (sdk.TxResponse, error) {
	cmd := append([]string{"tx", "billing", "reject-lease"}, leaseUUIDs...)
	if reason != "" {
		cmd = append(cmd, "--reason", reason)
	}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// BillingCancelLease cancels a pending lease (tenant only).
func BillingCancelLease(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, leaseUUID string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "billing", "cancel-lease", leaseUUID}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// BillingSetLeaseCustomDomain sets or clears the custom_domain on a lease.
// An empty domain clears the field; sender must be tenant, authority, or
// in params.allowed_list.
func BillingSetLeaseCustomDomain(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, leaseUUID, domain string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "billing", "set-custom-domain", leaseUUID, domain}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// BillingCreateAndAcknowledgeLease creates a new lease and immediately acknowledges it.
// This is a convenience function for tests where an active lease is needed.
// Returns the lease UUID and any error.
func BillingCreateAndAcknowledgeLease(ctx context.Context, chain *cosmos.CosmosChain, tenant ibc.Wallet, provider ibc.Wallet, items []string, flags ...string) (string, error) {
	// Create the lease
	res, err := BillingCreateLease(ctx, chain, tenant, items, flags...)
	if err != nil {
		return "", fmt.Errorf("failed to create lease: %w", err)
	}

	txRes, err := chain.GetTransaction(res.TxHash)
	if err != nil {
		return "", fmt.Errorf("failed to get create tx: %w", err)
	}
	if txRes.Code != 0 {
		return "", fmt.Errorf("lease creation failed: %s", txRes.RawLog)
	}

	// Get the lease UUID from the transaction
	leaseUUID, err := GetLeaseIDFromTxHash(ctx, chain, res.TxHash)
	if err != nil {
		return "", fmt.Errorf("failed to get lease UUID: %w", err)
	}

	// Acknowledge the lease
	ackRes, err := BillingAcknowledgeLease(ctx, chain, provider, leaseUUID)
	if err != nil {
		return leaseUUID, fmt.Errorf("failed to acknowledge lease: %w", err)
	}

	ackTxRes, err := chain.GetTransaction(ackRes.TxHash)
	if err != nil {
		return leaseUUID, fmt.Errorf("failed to get ack tx: %w", err)
	}
	if ackTxRes.Code != 0 {
		return leaseUUID, fmt.Errorf("lease acknowledgement failed: %s", ackTxRes.RawLog)
	}

	return leaseUUID, nil
}

// BillingWithdraw withdraws accrued funds from a specific lease.
func BillingWithdraw(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, leaseID string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "billing", "withdraw", leaseID}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// BillingWithdrawByProvider withdraws all accrued funds from all leases for a provider.
// Uses the --provider flag for provider-wide withdrawal mode.
// limit=0 uses the default limit (50).
func BillingWithdrawByProvider(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, providerUUID string, limit uint64, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "billing", "withdraw", "--provider", providerUUID}
	if limit > 0 {
		cmd = append(cmd, "--limit", strconv.FormatUint(limit, 10))
	}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// BillingUpdateParams updates the billing module parameters.
func BillingUpdateParams(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, maxLeasesPerTenant uint64, maxItemsPerLease uint64, minLeaseDuration uint64, maxPendingLeasesPerTenant uint64, pendingTimeout uint64, allowedList []string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{
		"tx", "billing", "update-params",
		strconv.FormatUint(maxLeasesPerTenant, 10),
		strconv.FormatUint(maxItemsPerLease, 10),
		strconv.FormatUint(minLeaseDuration, 10),
		strconv.FormatUint(maxPendingLeasesPerTenant, 10),
		strconv.FormatUint(pendingTimeout, 10),
	}
	if len(allowedList) > 0 {
		cmd = append(cmd, "--allowed-list", strings.Join(allowedList, ","))
	}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// Billing query helpers

// LeasesResponseJSON wraps lease queries that include pagination.
// Uses LeaseJSON because CLI outputs LeaseState as a string, not numeric enum.
type LeasesResponseJSON struct {
	Leases     []LeaseJSON       `json:"leases"`
	Pagination *PageResponseJSON `json:"pagination,omitempty"`
}

// LeaseResponseJSON wraps single lease queries.
// Uses LeaseJSON because CLI outputs LeaseState as a string, not numeric enum.
type LeaseResponseJSON struct {
	Lease LeaseJSON `json:"lease"`
}

// BillingQueryParams queries the billing module parameters.
func BillingQueryParams(ctx context.Context, chain *cosmos.CosmosChain) (*billingtypes.QueryParamsResponse, error) {
	var res billingtypes.QueryParamsResponse
	cmd := []string{"query", "billing", "params"}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// BillingQueryLease queries a lease by UUID.
func BillingQueryLease(ctx context.Context, chain *cosmos.CosmosChain, leaseUUID string) (*LeaseResponseJSON, error) {
	var res LeaseResponseJSON
	cmd := []string{"query", "billing", "lease", leaseUUID}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// BillingQueryLeaseByCustomDomain queries the lease that has claimed a given custom_domain.
func BillingQueryLeaseByCustomDomain(ctx context.Context, chain *cosmos.CosmosChain, domain string) (*LeaseResponseJSON, error) {
	var res LeaseResponseJSON
	cmd := []string{"query", "billing", "lease-by-domain", domain}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// BillingQueryLeases queries all leases with optional state filter.
// state can be "", "pending", "active", "closed", "rejected", or "expired".
// Empty string returns all leases.
func BillingQueryLeases(ctx context.Context, chain *cosmos.CosmosChain, state string) (*LeasesResponseJSON, error) {
	var res LeasesResponseJSON
	cmd := []string{"query", "billing", "leases"}
	if state != "" {
		cmd = append(cmd, "--state", state)
	}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// BillingQueryLeasesByTenant queries leases by tenant address with optional state filter.
func BillingQueryLeasesByTenant(ctx context.Context, chain *cosmos.CosmosChain, tenant, state string) (*LeasesResponseJSON, error) {
	var res LeasesResponseJSON
	cmd := []string{"query", "billing", "leases-by-tenant", tenant}
	if state != "" {
		cmd = append(cmd, "--state", state)
	}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// BillingQueryLeasesByProvider queries leases by provider UUID with optional state filter.
func BillingQueryLeasesByProvider(ctx context.Context, chain *cosmos.CosmosChain, providerUUID, state string) (*LeasesResponseJSON, error) {
	var res LeasesResponseJSON
	cmd := []string{"query", "billing", "leases-by-provider", providerUUID}
	if state != "" {
		cmd = append(cmd, "--state", state)
	}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// GetNextKeyString returns the base64-encoded next key for CLI pagination.
func (r *LeasesResponseJSON) GetNextKeyString() string {
	if r.Pagination != nil {
		return r.Pagination.NextKey
	}
	return ""
}

// BillingQueryLeasesPaginated queries leases with pagination.
func BillingQueryLeasesPaginated(ctx context.Context, chain *cosmos.CosmosChain, state string, limit uint64, key string) (*LeasesResponseJSON, string, error) {
	var res LeasesResponseJSON
	cmd := []string{"query", "billing", "leases", "--limit", strconv.FormatUint(limit, 10)}
	if state != "" {
		cmd = append(cmd, "--state", state)
	}
	if key != "" {
		cmd = append(cmd, "--page-key", key)
	}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, "", err
	}
	return &res, res.GetNextKeyString(), nil
}

// BillingQueryLeasesByTenantPaginated queries leases by tenant with pagination.
func BillingQueryLeasesByTenantPaginated(ctx context.Context, chain *cosmos.CosmosChain, tenant, state string, limit uint64, key string) (*LeasesResponseJSON, string, error) {
	var res LeasesResponseJSON
	cmd := []string{"query", "billing", "leases-by-tenant", tenant, "--limit", strconv.FormatUint(limit, 10)}
	if state != "" {
		cmd = append(cmd, "--state", state)
	}
	if key != "" {
		cmd = append(cmd, "--page-key", key)
	}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, "", err
	}
	return &res, res.GetNextKeyString(), nil
}

// BillingQueryLeasesByProviderPaginated queries leases by provider with pagination.
func BillingQueryLeasesByProviderPaginated(ctx context.Context, chain *cosmos.CosmosChain, providerUUID, state string, limit uint64, key string) (*LeasesResponseJSON, string, error) {
	var res LeasesResponseJSON
	cmd := []string{"query", "billing", "leases-by-provider", providerUUID, "--limit", strconv.FormatUint(limit, 10)}
	if state != "" {
		cmd = append(cmd, "--state", state)
	}
	if key != "" {
		cmd = append(cmd, "--page-key", key)
	}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, "", err
	}
	return &res, res.GetNextKeyString(), nil
}

// BillingQueryCreditAccount queries a tenant's credit account.
func BillingQueryCreditAccount(ctx context.Context, chain *cosmos.CosmosChain, tenant string) (*billingtypes.QueryCreditAccountResponse, error) {
	var res billingtypes.QueryCreditAccountResponse
	cmd := []string{"query", "billing", "credit-account", tenant}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// BillingQueryCreditAddress derives the credit address for a tenant.
func BillingQueryCreditAddress(ctx context.Context, chain *cosmos.CosmosChain, tenant string) (*billingtypes.QueryCreditAddressResponse, error) {
	var res billingtypes.QueryCreditAddressResponse
	cmd := []string{"query", "billing", "credit-address", tenant}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// BillingQueryWithdrawable queries the withdrawable amount for a lease.
func BillingQueryWithdrawable(ctx context.Context, chain *cosmos.CosmosChain, leaseUUID string) (*billingtypes.QueryWithdrawableAmountResponse, error) {
	var res billingtypes.QueryWithdrawableAmountResponse
	cmd := []string{"query", "billing", "withdrawable", leaseUUID}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// BillingQueryProviderWithdrawable queries the total withdrawable amount for a provider.
func BillingQueryProviderWithdrawable(ctx context.Context, chain *cosmos.CosmosChain, providerUUID string) (*billingtypes.QueryProviderWithdrawableResponse, error) {
	var res billingtypes.QueryProviderWithdrawableResponse
	cmd := []string{"query", "billing", "provider-withdrawable", providerUUID}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// CreditAccountsResponseJSON wraps credit accounts query that includes pagination.
type CreditAccountsResponseJSON struct {
	CreditAccounts []billingtypes.CreditAccount `json:"credit_accounts"`
	Pagination     *PageResponseJSON            `json:"pagination,omitempty"`
}

// BillingQueryCreditAccounts queries all credit accounts with pagination.
func BillingQueryCreditAccounts(ctx context.Context, chain *cosmos.CosmosChain) (*CreditAccountsResponseJSON, error) {
	var res CreditAccountsResponseJSON
	cmd := []string{"query", "billing", "credit-accounts"}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// BillingQueryCreditAccountsPaginated queries all credit accounts with pagination.
func BillingQueryCreditAccountsPaginated(ctx context.Context, chain *cosmos.CosmosChain, limit uint64, key string) (*CreditAccountsResponseJSON, string, error) {
	var res CreditAccountsResponseJSON
	cmd := []string{"query", "billing", "credit-accounts", "--limit", strconv.FormatUint(limit, 10)}
	if key != "" {
		cmd = append(cmd, "--page-key", key)
	}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, "", err
	}
	nextKey := ""
	if res.Pagination != nil {
		nextKey = res.Pagination.NextKey
	}
	return &res, nextKey, nil
}

// BillingQueryLeasesBySKU queries leases by SKU UUID with optional state filter.
func BillingQueryLeasesBySKU(ctx context.Context, chain *cosmos.CosmosChain, skuUUID, state string) (*LeasesResponseJSON, error) {
	var res LeasesResponseJSON
	cmd := []string{"query", "billing", "leases-by-sku", skuUUID}
	if state != "" {
		cmd = append(cmd, "--state", state)
	}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// BillingQueryLeasesBySKUPaginated queries leases by SKU UUID with pagination.
func BillingQueryLeasesBySKUPaginated(ctx context.Context, chain *cosmos.CosmosChain, skuUUID, state string, limit uint64, key string) (*LeasesResponseJSON, string, error) {
	var res LeasesResponseJSON
	cmd := []string{"query", "billing", "leases-by-sku", skuUUID, "--limit", strconv.FormatUint(limit, 10)}
	if state != "" {
		cmd = append(cmd, "--state", state)
	}
	if key != "" {
		cmd = append(cmd, "--page-key", key)
	}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, "", err
	}
	return &res, res.GetNextKeyString(), nil
}

// BillingQueryCreditEstimate queries the credit estimate for a tenant.
func BillingQueryCreditEstimate(ctx context.Context, chain *cosmos.CosmosChain, tenant string) (*billingtypes.QueryCreditEstimateResponse, error) {
	var res billingtypes.QueryCreditEstimateResponse
	cmd := []string{"query", "billing", "credit-estimate", tenant}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Event extraction helpers

// GetLeaseIDFromTxHash queries a transaction and extracts the lease UUID from it.
func GetLeaseIDFromTxHash(_ context.Context, chain *cosmos.CosmosChain, txHash string) (string, error) {
	txRes, err := chain.GetTransaction(txHash)
	if err != nil {
		return "", err
	}

	if txRes.Code != 0 {
		return "", fmt.Errorf("tx %s failed with code %d: %s", txHash, txRes.Code, txRes.RawLog)
	}

	eventNames := []string{"lease_created", "liftedinit.billing.v1.EventLeaseCreated"}
	for _, event := range txRes.Events {
		for _, eventName := range eventNames {
			if event.Type == eventName {
				for _, attr := range event.Attributes {
					if attr.Key == "lease_uuid" {
						return attr.Value, nil
					}
				}
			}
		}
	}

	eventTypes := make([]string, 0, len(txRes.Events))
	for _, event := range txRes.Events {
		eventTypes = append(eventTypes, event.Type)
	}
	return "", fmt.Errorf("lease_uuid not found in tx %s events (found events: %v)", txHash, eventTypes)
}
