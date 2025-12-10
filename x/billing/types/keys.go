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
)

const (
	ModuleName = "billing"

	StoreKey = ModuleName

	QuerierRoute = ModuleName
)

// CreditAccountAddressPrefix is the prefix used to derive credit account addresses.
const CreditAccountAddressPrefix = "billing/credit/"

// Event types for the billing module.
const (
	EventTypeCreditFunded        = "credit_funded"
	EventTypeLeaseCreated        = "lease_created"
	EventTypeLeaseClosed         = "lease_closed"
	EventTypeLeaseSettled        = "lease_settled"
	EventTypeProviderWithdraw    = "provider_withdraw"
	EventTypeProviderWithdrawAll = "provider_withdraw_all"
	EventTypeParamsUpdated       = "params_updated"

	// Attribute keys for events.
	AttributeKeyTenant           = "tenant"
	AttributeKeyCreditAddress    = "credit_address"
	AttributeKeyLeaseID          = "lease_id"
	AttributeKeyProviderID       = "provider_id"
	AttributeKeyAmount           = "amount"
	AttributeKeySettledAmount    = "settled_amount"
	AttributeKeyPayoutAddress    = "payout_address"
	AttributeKeyLeaseCount       = "lease_count"
	AttributeKeySender           = "sender"
	AttributeKeyNewBalance       = "new_balance"
	AttributeKeyItemCount        = "item_count"
	AttributeKeyTotalRate        = "total_rate_per_second"
	AttributeKeyClosedBy         = "closed_by"
	AttributeKeyDuration         = "duration_seconds"
	AttributeKeyActiveLeaseCount = "active_lease_count"
	AttributeKeyCreatedBy        = "created_by" // "tenant" or "authority"
)
