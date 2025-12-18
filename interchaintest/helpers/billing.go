package helpers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"

	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
)

// Billing transaction helpers

// BillingFundCredit funds a tenant's credit account.
func BillingFundCredit(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, tenant, amount string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "billing", "fund-credit", tenant, amount}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// BillingCreateLease creates a new lease with the specified SKU items.
// items should be in the format "sku_id:quantity" (e.g., "1:2", "2:1")
func BillingCreateLease(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, items []string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "billing", "create-lease"}
	cmd = append(cmd, items...)
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// BillingCreateLeaseForTenant creates a new lease on behalf of a tenant (authority only).
// items should be in the format "sku_id:quantity" (e.g., "1:2", "2:1")
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

// BillingAcknowledgeLease acknowledges a pending lease (provider only).
func BillingAcknowledgeLease(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, leaseUUID string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "billing", "acknowledge-lease", leaseUUID}
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

// BillingCancelLease cancels a pending lease (tenant only).
func BillingCancelLease(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, leaseUUID string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "billing", "cancel-lease", leaseUUID}
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

// BillingWithdrawAll withdraws all accrued funds from all leases for a provider.
func BillingWithdrawAll(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, providerUUID string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "billing", "withdraw-all", providerUUID}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// BillingWithdrawAllWithLimit withdraws from all leases with a specific limit.
func BillingWithdrawAllWithLimit(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, providerUUID string, limit uint64, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "billing", "withdraw-all", providerUUID, "--limit", strconv.FormatUint(limit, 10)}
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

// BillingQueryParams queries the billing module parameters.
func BillingQueryParams(ctx context.Context, chain *cosmos.CosmosChain) (*billingtypes.QueryParamsResponse, error) {
	var res billingtypes.QueryParamsResponse
	cmd := []string{"query", "billing", "params"}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// LeaseResponseJSON is a JSON-friendly version of QueryLeaseResponse.
type LeaseResponseJSON struct {
	Lease LeaseJSON `json:"lease"`
}

// LeaseJSON is a JSON-friendly version of Lease for proper uint64 parsing.
type LeaseJSON struct {
	Uuid          string          `json:"uuid"`
	Tenant        string          `json:"tenant"`
	ProviderUuid  string          `json:"provider_uuid"`
	Items         []LeaseItemJSON `json:"items"`
	State         string          `json:"state"`
	CreatedAt     string          `json:"created_at"`
	ClosedAt      string          `json:"closed_at,omitempty"`
	LastSettledAt string          `json:"last_settled_at"`
}

// LeaseItemJSON is a JSON-friendly version of LeaseItem.
type LeaseItemJSON struct {
	SkuUuid     string   `json:"sku_uuid"`
	Quantity    string   `json:"quantity"`
	LockedPrice sdk.Coin `json:"locked_price"`
}

// LeasesResponseJSON is a JSON-friendly version of QueryLeasesResponse.
type LeasesResponseJSON struct {
	Leases     []LeaseJSON       `json:"leases"`
	Pagination *PageResponseJSON `json:"pagination,omitempty"`
}

// BillingQueryLease queries a lease by ID.
func BillingQueryLease(ctx context.Context, chain *cosmos.CosmosChain, leaseID string) (*LeaseResponseJSON, error) {
	var res LeaseResponseJSON
	cmd := []string{"query", "billing", "lease", leaseID}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// BillingQueryLeases queries all leases with optional state filter.
func BillingQueryLeases(ctx context.Context, chain *cosmos.CosmosChain, activeOnly bool) (*LeasesResponseJSON, error) {
	var res LeasesResponseJSON
	cmd := []string{"query", "billing", "leases"}
	if activeOnly {
		cmd = append(cmd, "--state", "active")
	}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// BillingQueryLeasesPaginated queries leases with pagination.
func BillingQueryLeasesPaginated(ctx context.Context, chain *cosmos.CosmosChain, activeOnly bool, limit uint64, key string) (*LeasesResponseJSON, string, error) {
	var res LeasesResponseJSON
	cmd := []string{"query", "billing", "leases", "--limit", strconv.FormatUint(limit, 10)}
	if activeOnly {
		cmd = append(cmd, "--state", "active")
	}
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

// BillingQueryLeasesByTenant queries leases by tenant address.
func BillingQueryLeasesByTenant(ctx context.Context, chain *cosmos.CosmosChain, tenant string, activeOnly bool) (*LeasesResponseJSON, error) {
	var res LeasesResponseJSON
	cmd := []string{"query", "billing", "leases-by-tenant", tenant}
	if activeOnly {
		cmd = append(cmd, "--state", "active")
	}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// BillingQueryLeasesByProvider queries leases by provider ID.
func BillingQueryLeasesByProvider(ctx context.Context, chain *cosmos.CosmosChain, providerUUID string, activeOnly bool) (*LeasesResponseJSON, error) {
	var res LeasesResponseJSON
	cmd := []string{"query", "billing", "leases-by-provider", providerUUID}
	if activeOnly {
		cmd = append(cmd, "--state", "active")
	}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// CreditAccountResponseJSON is a JSON-friendly version of QueryCreditAccountResponse.
type CreditAccountResponseJSON struct {
	CreditAccount CreditAccountJSON `json:"credit_account"`
	Balances      sdk.Coins         `json:"balances"`
}

// CreditAccountJSON is a JSON-friendly version of CreditAccount.
type CreditAccountJSON struct {
	Tenant        string `json:"tenant"`
	CreditAddress string `json:"credit_address"`
}

// BillingQueryCreditAccount queries a tenant's credit account.
func BillingQueryCreditAccount(ctx context.Context, chain *cosmos.CosmosChain, tenant string) (*CreditAccountResponseJSON, error) {
	var res CreditAccountResponseJSON
	cmd := []string{"query", "billing", "credit-account", tenant}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// CreditAddressResponseJSON is a JSON-friendly version of QueryCreditAddressResponse.
type CreditAddressResponseJSON struct {
	CreditAddress string `json:"credit_address"`
}

// BillingQueryCreditAddress derives the credit address for a tenant.
func BillingQueryCreditAddress(ctx context.Context, chain *cosmos.CosmosChain, tenant string) (*CreditAddressResponseJSON, error) {
	var res CreditAddressResponseJSON
	cmd := []string{"query", "billing", "credit-address", tenant}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// WithdrawableAmountResponseJSON is a JSON-friendly version of QueryWithdrawableAmountResponse.
type WithdrawableAmountResponseJSON struct {
	Amounts sdk.Coins `json:"amounts"`
}

// BillingQueryWithdrawable queries the withdrawable amount for a lease.
func BillingQueryWithdrawable(ctx context.Context, chain *cosmos.CosmosChain, leaseID string) (*WithdrawableAmountResponseJSON, error) {
	var res WithdrawableAmountResponseJSON
	cmd := []string{"query", "billing", "withdrawable", leaseID}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// ProviderWithdrawableResponseJSON is a JSON-friendly version of QueryProviderWithdrawableResponse.
type ProviderWithdrawableResponseJSON struct {
	Amounts    sdk.Coins `json:"amounts"`
	LeaseCount string    `json:"lease_count"`
}

// BillingQueryProviderWithdrawable queries the total withdrawable amount for a provider.
func BillingQueryProviderWithdrawable(ctx context.Context, chain *cosmos.CosmosChain, providerUUID string) (*ProviderWithdrawableResponseJSON, error) {
	var res ProviderWithdrawableResponseJSON
	cmd := []string{"query", "billing", "provider-withdrawable", providerUUID}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// GetLeaseIDFromTxHash queries a transaction and extracts the lease ID from it.
func GetLeaseIDFromTxHash(_ context.Context, chain *cosmos.CosmosChain, txHash string) (string, error) {
	txRes, err := chain.GetTransaction(txHash)
	if err != nil {
		return "", err
	}

	for _, event := range txRes.Events {
		if event.Type == "lease_created" {
			for _, attr := range event.Attributes {
				if attr.Key == "lease_uuid" {
					return attr.Value, nil
				}
			}
		}
	}
	return "", fmt.Errorf("lease_uuid not found in tx %s events", txHash)
}

// GetLeaseIDFromLeases returns the lease UUID from the first lease in the response.
func GetLeaseIDFromLeases(res *LeasesResponseJSON) (string, error) {
	if len(res.Leases) == 0 {
		return "", fmt.Errorf("no leases found")
	}
	return res.Leases[0].Uuid, nil
}

// ParamsResponseJSON is a JSON-friendly version for billing params.
type ParamsResponseJSON struct {
	Params ParamsJSON `json:"params"`
}

// ParamsJSON is a JSON-friendly version of billing Params.
type ParamsJSON struct {
	MaxLeasesPerTenant        string   `json:"max_leases_per_tenant"`
	MaxItemsPerLease          string   `json:"max_items_per_lease"`
	MinLeaseDuration          string   `json:"min_lease_duration"`
	AllowedList               []string `json:"allowed_list"`
	MaxPendingLeasesPerTenant string   `json:"max_pending_leases_per_tenant"`
	PendingTimeout            string   `json:"pending_timeout"`
}

// BillingQueryParamsJSON queries the billing module parameters with JSON parsing.
func BillingQueryParamsJSON(ctx context.Context, chain *cosmos.CosmosChain) (*ParamsResponseJSON, error) {
	var res ParamsResponseJSON
	cmd := []string{"query", "billing", "params"}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// LeasesByTenantResponseJSON is a wrapper for leases-by-tenant query response.
type LeasesByTenantResponseJSON struct {
	Leases     []LeaseJSON       `json:"leases"`
	Pagination *PageResponseJSON `json:"pagination,omitempty"`
}

// ToProto converts to proto types (for pagination support).
func (r *LeasesByTenantResponseJSON) ToProto() *billingtypes.QueryLeasesByTenantResponse {
	res := &billingtypes.QueryLeasesByTenantResponse{}
	if r.Pagination != nil {
		total, _ := strconv.ParseUint(r.Pagination.Total, 10, 64)
		nextKey, _ := base64.StdEncoding.DecodeString(r.Pagination.NextKey)
		res.Pagination = &query.PageResponse{
			NextKey: nextKey,
			Total:   total,
		}
	}
	return res
}

// LeasesByProviderResponseJSON is a wrapper for leases-by-provider query response.
type LeasesByProviderResponseJSON struct {
	Leases     []LeaseJSON       `json:"leases"`
	Pagination *PageResponseJSON `json:"pagination,omitempty"`
}

// BillingQueryRaw executes a raw billing query and returns the output.
func BillingQueryRaw(ctx context.Context, chain *cosmos.CosmosChain, args ...string) ([]byte, error) {
	cmd := []string{chain.Config().Bin, "query", "billing"}
	cmd = append(cmd, args...)
	cmd = append(cmd, "--node", chain.GetRPCAddress(), "--output=json")
	stdout, _, err := chain.Exec(ctx, cmd, chain.Config().Env)
	return stdout, err
}

// BillingTxRaw executes a raw billing transaction and returns the output.
func BillingTxRaw(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, args ...string) ([]byte, error) {
	cmd := []string{"tx", "billing"}
	cmd = append(cmd, args...)
	fullCmd := TxCommandBuilder(ctx, chain, cmd, user.KeyName())
	stdout, _, err := chain.Exec(ctx, fullCmd, chain.Config().Env)
	return stdout, err
}

// GetWithdrawnAmountFromTxHash extracts the withdrawn amount from a withdraw transaction.
func GetWithdrawnAmountFromTxHash(_ context.Context, chain *cosmos.CosmosChain, txHash string) (sdk.Coin, error) {
	txRes, err := chain.GetTransaction(txHash)
	if err != nil {
		return sdk.Coin{}, err
	}

	for _, event := range txRes.Events {
		if event.Type == "provider_withdrawal" {
			for _, attr := range event.Attributes {
				if attr.Key == "amount" {
					return sdk.ParseCoinNormalized(attr.Value)
				}
			}
		}
	}
	return sdk.Coin{}, fmt.Errorf("amount not found in tx %s events", txHash)
}

// BillingRawResponse is used to parse raw query responses.
type BillingRawResponse struct {
	Code    int             `json:"code,omitempty"`
	Message string          `json:"message,omitempty"`
	Lease   json.RawMessage `json:"lease,omitempty"`
}
