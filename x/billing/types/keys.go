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
)

const (
	ModuleName = "billing"

	StoreKey = ModuleName

	QuerierRoute = ModuleName
)

// CreditAccountAddressPrefix is the prefix used to derive credit account addresses.
const CreditAccountAddressPrefix = "billing/credit/"

// WithdrawAll limits to prevent DoS attacks.
const (
	// DefaultWithdrawAllLimit is the default limit when limit=0 is specified.
	// This prevents unbounded iterations over all leases.
	DefaultWithdrawAllLimit uint64 = 50

	// MaxWithdrawAllLimit is the maximum allowed limit for WithdrawAll operations.
	// This prevents a single transaction from processing too many leases and
	// causing gas exhaustion or block timeouts.
	MaxWithdrawAllLimit uint64 = 100
)

// Event types for the billing module.
const (
	EventTypeCreditFunded        = "credit_funded"
	EventTypeLeaseCreated        = "lease_created"
	EventTypeLeaseClosed         = "lease_closed"
	EventTypeLeaseAutoClose      = "lease_auto_closed"
	EventTypeLeaseAcknowledged   = "lease_acknowledged"
	EventTypeLeaseRejected       = "lease_rejected"
	EventTypeLeaseCancelled      = "lease_cancelled"
	EventTypeLeaseExpired        = "lease_expired"
	EventTypeProviderWithdraw    = "provider_withdraw"
	EventTypeProviderWithdrawAll = "provider_withdraw_all"
	EventTypeParamsUpdated       = "params_updated"

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
	AttributeKeyCancelledBy       = "cancelled_by"
)
