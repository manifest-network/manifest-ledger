package types

import "cosmossdk.io/collections"

// Storage prefixes for collections.
var (
	// ParamsKey saves the module parameters.
	ParamsKey = collections.NewPrefix(0)

	// LeaseKey saves the Leases.
	LeaseKey = collections.NewPrefix(1)

	// LeaseSequenceKey saves the next Lease id.
	LeaseSequenceKey = collections.NewPrefix(2)

	// LeaseByTenantIndexKey saves the tenant index for Leases.
	LeaseByTenantIndexKey = collections.NewPrefix(3)

	// LeaseByProviderIndexKey saves the provider index for Leases.
	LeaseByProviderIndexKey = collections.NewPrefix(4)

	// CreditAccountKey saves the CreditAccounts.
	CreditAccountKey = collections.NewPrefix(5)

	// CreditAddressIndexKey saves the reverse lookup from derived credit address to tenant.
	// This enables O(1) lookup to check if an address is a credit account.
	CreditAddressIndexKey = collections.NewPrefix(6)

	// LeaseByStateIndexKey saves the state index for Leases.
	// This enables efficient queries for leases by state (e.g., all pending leases).
	LeaseByStateIndexKey = collections.NewPrefix(7)

	// LeaseByProviderStateIndexKey saves the compound (provider, state) index for Leases.
	// This enables O(1) lookup of leases by provider and state combined (e.g., pending leases for a provider).
	LeaseByProviderStateIndexKey = collections.NewPrefix(8)

	// LeaseByTenantStateIndexKey saves the compound (tenant, state) index for Leases.
	// This enables O(1) lookup of leases by tenant and state combined (e.g., active leases for a tenant).
	LeaseByTenantStateIndexKey = collections.NewPrefix(9)

	// LeaseBySKUIndexKey saves the SKU → Lease index for many-to-many relationship.
	// Since a lease can contain multiple SKUs, this is managed as a separate Map collection
	// rather than as part of LeaseIndexes.
	LeaseBySKUIndexKey = collections.NewPrefix(10)

	// LeaseByStateCreatedAtIndexKey saves the compound (state, created_at) index for Leases.
	// This enables efficient time-based queries for leases in a specific state,
	// particularly for EndBlocker pending lease expiration (O(e) expired instead of O(p) all pending).
	LeaseByStateCreatedAtIndexKey = collections.NewPrefix(11)
)

const (
	ModuleName = "billing"

	StoreKey = ModuleName

	QuerierRoute = ModuleName
)

// CreditAccountAddressPrefix is the prefix used to derive credit account addresses.
const CreditAccountAddressPrefix = "billing/credit/"

// Provider-wide withdraw limits to prevent DoS attacks.
const (
	// DefaultProviderWithdrawLimit is the default limit when limit=0 is specified
	// for provider-wide withdrawal mode. This prevents unbounded iterations over all leases.
	DefaultProviderWithdrawLimit uint64 = 50
)

// Query limits to prevent DoS attacks on RPC nodes.
const (
	// DefaultProviderWithdrawableQueryLimit is the default limit for ProviderWithdrawable queries.
	DefaultProviderWithdrawableQueryLimit uint64 = 100

	// MaxProviderWithdrawableQueryLimit is the maximum limit for ProviderWithdrawable queries.
	// This prevents queries from iterating over unbounded numbers of leases.
	MaxProviderWithdrawableQueryLimit uint64 = 1000

	// MaxCreditEstimateLeases is the maximum number of active leases to process
	// in a CreditEstimate query. This prevents DoS on tenants with many leases.
	MaxCreditEstimateLeases uint64 = 100
)

// EndBlocker limits to prevent DoS attacks.
const (
	// MaxPendingLeaseExpirationsPerBlock is the maximum number of pending leases
	// that can be expired in a single block. This prevents DoS attacks where
	// many pending leases expire simultaneously, causing block timeouts.
	MaxPendingLeaseExpirationsPerBlock = 100
)

// Event types for the billing module.
const (
	EventTypeCreditFunded      = "credit_funded"
	EventTypeLeaseCreated      = "lease_created"
	EventTypeLeaseClosed       = "lease_closed"
	EventTypeLeaseAutoClose    = "lease_auto_closed"
	EventTypeLeaseAcknowledged = "lease_acknowledged"
	EventTypeBatchAcknowledged = "batch_acknowledged"
	EventTypeLeaseRejected     = "lease_rejected"
	EventTypeBatchRejected     = "batch_rejected"
	EventTypeBatchClosed       = "batch_closed"
	EventTypeLeaseCancelled    = "lease_cancelled"
	EventTypeBatchCancelled    = "batch_cancelled"
	EventTypeLeaseExpired      = "lease_expired"
	EventTypeProviderWithdraw  = "provider_withdraw"
	EventTypeBatchWithdraw     = "batch_withdraw"
	EventTypeParamsUpdated     = "params_updated"

	// Attribute keys for events.
	AttributeKeyTenant            = "tenant"
	AttributeKeyCreditAddress     = "credit_address"
	AttributeKeyLeaseUUID         = "lease_uuid"
	AttributeKeyProviderUUID      = "provider_uuid"
	AttributeKeyAmount            = "amount"
	AttributeKeySettledAmount     = "settled_amount"
	AttributeKeySettledAmounts    = "settled_amounts"
	AttributeKeyPayoutAddress     = "payout_address"
	AttributeKeyLeaseCount        = "lease_count"
	AttributeKeySender            = "sender"
	AttributeKeyNewBalance        = "new_balance"
	AttributeKeyItemCount         = "item_count"
	AttributeKeyTotalRate         = "total_rate_per_second"
	AttributeKeyClosedBy          = "closed_by"
	AttributeKeyDuration          = "duration_seconds"
	AttributeKeyActiveLeaseCount  = "active_lease_count"
	AttributeKeyPendingLeaseCount = "pending_lease_count"
	AttributeKeyCreatedBy         = "created_by"
	AttributeKeyReason            = "reason"
	AttributeKeyAcknowledgedBy    = "acknowledged_by"
	AttributeKeyRejectedBy        = "rejected_by"
	AttributeKeyRejectionReason   = "rejection_reason"
	AttributeKeyClosureReason     = "closure_reason"
	AttributeKeyCancelledBy       = "cancelled_by"
	AttributeKeyAutoClosed        = "auto_closed"
	AttributeKeyMetaHash          = "meta_hash"
)

// Rejection reasons for lease cancellation/rejection.
const (
	// RejectionReasonCancelledByTenant is the reason set when a tenant cancels their own pending lease.
	RejectionReasonCancelledByTenant = "cancelled by tenant"
)

// Closure reasons for lease closure.
const (
	// ClosureReasonCreditExhausted is the reason set when a lease is auto-closed due to credit exhaustion.
	ClosureReasonCreditExhausted = "credit exhausted"
)
