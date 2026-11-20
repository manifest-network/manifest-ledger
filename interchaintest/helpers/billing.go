package helpers

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	sdkmath "cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"

	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
)

// LeaseItemJSON is a JSON-compatible version of LeaseItem.
// Quantity is a string in CLI JSON output.
type LeaseItemJSON struct {
	SkuUuid      string   `json:"sku_uuid,omitempty"`
	Quantity     string   `json:"quantity,omitempty"`
	LockedPrice  sdk.Coin `json:"locked_price"`
	ServiceName  string   `json:"service_name,omitempty"`
	CustomDomain string   `json:"custom_domain,omitempty"`
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
	AcknowledgedAt  *time.Time      `json:"acknowledged_at,omitempty"`
	RejectedAt      *time.Time      `json:"rejected_at,omitempty"`
	RejectionReason string          `json:"rejection_reason,omitempty"`
	ExpiredAt       *time.Time      `json:"expired_at,omitempty"`
	ClosureReason   string          `json:"closure_reason,omitempty"`
	MetaHash        []byte          `json:"meta_hash,omitempty"`
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

// StreamStateJSON is a JSON-compatible version of StreamState: the settlement
// aggregate of one (provider_uuid, tenant, denom) stream.
type StreamStateJSON struct {
	// Rate is the summed per-second rate of the stream's ACTIVE leases.
	Rate sdkmath.Int `json:"rate"`
	// LastSettledAt is the time up to which the stream's accrual has been
	// settled: the exact time of the block that last settled it.
	LastSettledAt time.Time `json:"last_settled_at"`
	// LeaseCount is the number of ACTIVE leases contributing to Rate.
	LeaseCount uint64 `json:"lease_count,omitempty,string"`
	// ExhaustedAt is set once a settlement found the tenant's credit exhausted;
	// the EndBlocker then closes the stream's leases.
	ExhaustedAt *time.Time `json:"exhausted_at,omitempty"`
	// Remainder is the accrual that did not amount to a whole unit of the denom
	// at the last settlement, in denom units × nanoseconds. It is always less
	// than NanosPerSecond and is carried to the next settlement.
	Remainder sdkmath.Int `json:"remainder"`
}

// NanosPerSecond is the scale of StreamStateJSON.Remainder: rates are per
// second and time is tracked to the nanosecond.
const NanosPerSecond = int64(time.Second)

// IsExhausted reports whether the stream is frozen for exhausted credit.
func (s StreamStateJSON) IsExhausted() bool {
	return s.ExhaustedAt != nil
}

// AccrualAt returns what settling the stream at the block time now pays, and
// the remainder the settlement leaves, ignoring the tenant's credit balance:
//
//	total     = remainder + rate × (now − last_settled_at, in nanoseconds)
//	paid      = total / 1e9
//	remainder = total % 1e9
//
// A frozen stream, or one last settled at or after now, accrues nothing.
func (s StreamStateJSON) AccrualAt(now time.Time) (paid, remainder sdkmath.Int) {
	remainder = s.Remainder
	if remainder.IsNil() {
		remainder = sdkmath.ZeroInt()
	}
	elapsed := now.Sub(s.LastSettledAt)
	if s.IsExhausted() || elapsed <= 0 {
		return sdkmath.ZeroInt(), remainder
	}
	total := s.Rate.MulRaw(int64(elapsed)).Add(remainder)
	return total.QuoRaw(NanosPerSecond), total.ModRaw(NanosPerSecond)
}

// StreamJSON is a JSON-compatible version of Stream, the shape returned by the
// streams-by-provider and streams-by-tenant queries.
type StreamJSON struct {
	ProviderUuid string          `json:"provider_uuid,omitempty"`
	Tenant       string          `json:"tenant,omitempty"`
	Denom        string          `json:"denom,omitempty"`
	State        StreamStateJSON `json:"state"`
}

// StreamsResponseJSON wraps the stream queries, which include pagination.
// PageResponseJSON is used for the same reason as in the lease queries.
type StreamsResponseJSON struct {
	Streams    []StreamJSON      `json:"streams"`
	Pagination *PageResponseJSON `json:"pagination,omitempty"`
}

// GetNextKeyString returns the base64-encoded next key for CLI pagination.
func (r *StreamsResponseJSON) GetNextKeyString() string {
	if r.Pagination != nil {
		return r.Pagination.NextKey
	}
	return ""
}

// Find returns the stream of (providerUUID, tenant, denom), or nil when the
// response does not contain it.
func (r *StreamsResponseJSON) Find(providerUUID, tenant, denom string) *StreamJSON {
	for i := range r.Streams {
		s := &r.Streams[i]
		if s.ProviderUuid == providerUUID && s.Tenant == tenant && s.Denom == denom {
			return s
		}
	}
	return nil
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

// BillingSetItemCustomDomain sets or clears the custom_domain on a
// specific LeaseItem. For a 1-item legacy lease, pass "" for serviceName.
// An empty domain clears the field; sender must be tenant, authority, or
// in params.allowed_list.
func BillingSetItemCustomDomain(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, leaseUUID, serviceName, domain string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "billing", "set-item-custom-domain", leaseUUID, serviceName, domain}
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

// BillingWithdrawByProvider settles a provider's streams and pays what they
// accrued to the provider's payout address. This is the only withdrawal mode:
// settlement is per (provider, tenant, denom) stream, so a single lease can no
// longer be withdrawn on its own.
// limit counts streams, not leases; limit=0 uses the default limit (50).
func BillingWithdrawByProvider(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, providerUUID string, limit uint64, flags ...string) (sdk.TxResponse, error) {
	return BillingWithdrawByProviderPaginated(ctx, chain, user, providerUUID, limit, "", flags...)
}

// BillingWithdrawByProviderPaginated is BillingWithdrawByProvider resuming
// after key, the base64 next_key of the previous withdraw response. An empty
// key starts from the provider's first stream.
func BillingWithdrawByProviderPaginated(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, providerUUID string, limit uint64, key string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "billing", "withdraw", "--provider", providerUUID}
	if limit > 0 {
		cmd = append(cmd, "--limit", strconv.FormatUint(limit, 10))
	}
	if key != "" {
		cmd = append(cmd, "--key", key)
	}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// BillingWithdrawLeasesLegacy invokes the removed lease mode of the withdraw
// command (`withdraw <lease-uuid>...`). The CLI rejects it before broadcasting,
// so this only exists to assert that rejection.
func BillingWithdrawLeasesLegacy(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, leaseUUIDs []string, flags ...string) (sdk.TxResponse, error) {
	cmd := append([]string{"tx", "billing", "withdraw"}, leaseUUIDs...)
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// BillingUpdateParams updates the billing module parameters using the v1
// field set only. ReservedDomainSuffixes is left untouched: the CLI's
// preserve-on-omit semantic keeps the existing on-chain value when the flag
// is absent. Callers that want to set or clear ReservedDomainSuffixes should
// use BillingUpdateParamsFull directly.
func BillingUpdateParams(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, maxLeasesPerTenant uint64, maxItemsPerLease uint64, minLeaseDuration uint64, maxPendingLeasesPerTenant uint64, pendingTimeout uint64, allowedList []string, flags ...string) (sdk.TxResponse, error) {
	return BillingUpdateParamsFull(ctx, chain, user, maxLeasesPerTenant, maxItemsPerLease, minLeaseDuration, maxPendingLeasesPerTenant, pendingTimeout, allowedList, nil, flags...)
}

// BillingUpdateParamsFull updates the billing module parameters including the
// reserved_domain_suffixes list.
//
// Slice arguments use Go nil-vs-empty distinction to match the CLI's
// preserve-on-omit semantic:
//   - nil slice: omit the corresponding flag → CLI preserves the existing on-chain value.
//   - non-nil slice (including length zero): pass the flag with the joined value
//     → CLI sets the value (an empty []string{} explicitly clears).
func BillingUpdateParamsFull(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, maxLeasesPerTenant uint64, maxItemsPerLease uint64, minLeaseDuration uint64, maxPendingLeasesPerTenant uint64, pendingTimeout uint64, allowedList []string, reservedDomainSuffixes []string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{
		"tx", "billing", "update-params",
		strconv.FormatUint(maxLeasesPerTenant, 10),
		strconv.FormatUint(maxItemsPerLease, 10),
		strconv.FormatUint(minLeaseDuration, 10),
		strconv.FormatUint(maxPendingLeasesPerTenant, 10),
		strconv.FormatUint(pendingTimeout, 10),
	}
	if allowedList != nil {
		cmd = append(cmd, "--allowed-list", strings.Join(allowedList, ","))
	}
	if reservedDomainSuffixes != nil {
		cmd = append(cmd, "--reserved-domain-suffixes", strings.Join(reservedDomainSuffixes, ","))
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

// LeaseByCustomDomainResponseJSON wraps the LeaseByCustomDomain query response,
// which includes the service_name of the matching item alongside the lease.
type LeaseByCustomDomainResponseJSON struct {
	Lease       LeaseJSON `json:"lease"`
	ServiceName string    `json:"service_name,omitempty"`
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

// BillingQueryLeaseByCustomDomain queries the lease and matching service_name
// for a given custom_domain.
func BillingQueryLeaseByCustomDomain(ctx context.Context, chain *cosmos.CosmosChain, domain string) (*LeaseByCustomDomainResponseJSON, error) {
	var res LeaseByCustomDomainResponseJSON
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

// BillingQueryStreamsByProvider queries the settlement streams of a provider:
// one per (tenant, denom) with at least one ACTIVE lease.
func BillingQueryStreamsByProvider(ctx context.Context, chain *cosmos.CosmosChain, providerUUID string) (*StreamsResponseJSON, error) {
	var res StreamsResponseJSON
	cmd := []string{"query", "billing", "streams-by-provider", providerUUID}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// BillingQueryStreamsByProviderPaginated queries a provider's streams with
// pagination. key is the base64 next_key exactly as the previous page printed
// it: stream keys hold raw address bytes, so the stream queries decode
// --page-key from base64 instead of taking it as raw bytes.
func BillingQueryStreamsByProviderPaginated(ctx context.Context, chain *cosmos.CosmosChain, providerUUID string, limit uint64, key string) (*StreamsResponseJSON, string, error) {
	var res StreamsResponseJSON
	cmd := []string{"query", "billing", "streams-by-provider", providerUUID, "--limit", strconv.FormatUint(limit, 10)}
	if key != "" {
		cmd = append(cmd, "--page-key", key)
	}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, "", err
	}
	return &res, res.GetNextKeyString(), nil
}

// BillingQueryStreamsByTenant queries the settlement streams paid by a tenant:
// one per (provider, denom) the tenant has at least one ACTIVE lease with.
func BillingQueryStreamsByTenant(ctx context.Context, chain *cosmos.CosmosChain, tenant string) (*StreamsResponseJSON, error) {
	var res StreamsResponseJSON
	cmd := []string{"query", "billing", "streams-by-tenant", tenant}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// BillingQueryStreamsByTenantPaginated queries a tenant's streams with
// pagination. key is the base64 next_key of the previous page, as printed.
func BillingQueryStreamsByTenantPaginated(ctx context.Context, chain *cosmos.CosmosChain, tenant string, limit uint64, key string) (*StreamsResponseJSON, string, error) {
	var res StreamsResponseJSON
	cmd := []string{"query", "billing", "streams-by-tenant", tenant, "--limit", strconv.FormatUint(limit, 10)}
	if key != "" {
		cmd = append(cmd, "--page-key", key)
	}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, "", err
	}
	return &res, res.GetNextKeyString(), nil
}

// BillingQueryStream returns the stream of (providerUUID, tenant, denom), or
// nil when it does not exist (the tenant has no ACTIVE lease with the provider
// in that denom).
func BillingQueryStream(ctx context.Context, chain *cosmos.CosmosChain, providerUUID, tenant, denom string) (*StreamJSON, error) {
	res, err := BillingQueryStreamsByTenant(ctx, chain, tenant)
	if err != nil {
		return nil, err
	}
	return res.Find(providerUUID, tenant, denom), nil
}

// ProviderWithdrawableResponseJSON wraps the provider-withdrawable query. It
// returns a pagination block whose uint64 `total` is emitted as a JSON string by
// proto3 JSON; the SDK's query.PageResponse (no `,string` tag) cannot unmarshal
// that via encoding/json, so we use PageResponseJSON here (same reason the leases
// and credit-account paginated helpers do).
//
// The query sums over the provider's streams: LeaseCount is the number of
// leases covered by streams with a non-zero withdrawable amount.
type ProviderWithdrawableResponseJSON struct {
	Amounts    sdk.Coins         `json:"amounts"`
	LeaseCount uint64            `json:"lease_count,omitempty,string"`
	Pagination *PageResponseJSON `json:"pagination,omitempty"`
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

// BillingQueryProviderWithdrawablePaginated queries one page of a provider's
// withdrawable amount, paginated over its streams. key is the base64 next_key
// of the previous page, as printed.
func BillingQueryProviderWithdrawablePaginated(ctx context.Context, chain *cosmos.CosmosChain, providerUUID string, limit uint64, key string) (*ProviderWithdrawableResponseJSON, string, error) {
	var res ProviderWithdrawableResponseJSON
	cmd := []string{"query", "billing", "provider-withdrawable", providerUUID, "--limit", strconv.FormatUint(limit, 10)}
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

// EventAttributes holds the attributes of one emitted event, by key.
type EventAttributes map[string]string

// FilterEvents returns the attributes of every event of eventType, in emission
// order.
func FilterEvents(events []abci.Event, eventType string) []EventAttributes {
	var matches []EventAttributes
	for _, event := range events {
		if event.Type != eventType {
			continue
		}
		attrs := make(EventAttributes, len(event.Attributes))
		for _, attr := range event.Attributes {
			attrs[attr.Key] = attr.Value
		}
		matches = append(matches, attrs)
	}
	return matches
}

// FindEvent returns the first event of eventType whose attribute key equals
// value, and whether one was found.
func FindEvent(events []abci.Event, eventType, key, value string) (EventAttributes, bool) {
	for _, attrs := range FilterEvents(events, eventType) {
		if attrs[key] == value {
			return attrs, true
		}
	}
	return nil, false
}

// BlockInfo is what the billing tests need to know about a committed block.
type BlockInfo struct {
	// Time is the block header time: the time every transaction and the
	// EndBlocker of that block settle and close leases at.
	Time time.Time
	// Events are the events emitted outside of transactions (BeginBlock and
	// EndBlock). The EndBlocker's lease_auto_closed events are found here, not in
	// the events of the withdraw transaction that froze the stream.
	Events []abci.Event
}

// GetBlockInfo returns the header time and the non-transaction events of the
// block at height.
func GetBlockInfo(ctx context.Context, chain *cosmos.CosmosChain, height int64) (*BlockInfo, error) {
	client := chain.GetNode().Client
	block, err := client.Block(ctx, &height)
	if err != nil {
		return nil, fmt.Errorf("failed to get block %d: %w", height, err)
	}
	results, err := client.BlockResults(ctx, &height)
	if err != nil {
		return nil, fmt.Errorf("failed to get block results %d: %w", height, err)
	}
	return &BlockInfo{Time: block.Block.Time, Events: results.FinalizeBlockEvents}, nil
}

// GetWithdrawResponse decodes the MsgWithdrawResponse of a committed withdraw
// transaction from its result data. has_more and next_key are only available
// there: the batch_withdraw event does not carry the pagination cursor.
func GetWithdrawResponse(txRes *sdk.TxResponse) (*billingtypes.MsgWithdrawResponse, error) {
	bz, err := hex.DecodeString(txRes.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode tx %s data: %w", txRes.TxHash, err)
	}

	var msgData sdk.TxMsgData
	if err := msgData.Unmarshal(bz); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tx %s data: %w", txRes.TxHash, err)
	}

	wantTypeURL := sdk.MsgTypeURL(&billingtypes.MsgWithdrawResponse{})
	for _, msgResponse := range msgData.MsgResponses {
		if msgResponse.TypeUrl != wantTypeURL {
			continue
		}
		var res billingtypes.MsgWithdrawResponse
		if err := res.Unmarshal(msgResponse.Value); err != nil {
			return nil, fmt.Errorf("failed to unmarshal withdraw response of tx %s: %w", txRes.TxHash, err)
		}
		return &res, nil
	}
	return nil, fmt.Errorf("no %s in tx %s (code %d): %s", wantTypeURL, txRes.TxHash, txRes.Code, txRes.RawLog)
}

// GetLatestBlockTime returns the header time of the latest committed block.
func GetLatestBlockTime(ctx context.Context, chain *cosmos.CosmosChain) (time.Time, error) {
	block, err := chain.GetNode().Client.Block(ctx, nil)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to get latest block: %w", err)
	}
	return block.Block.Time, nil
}

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
