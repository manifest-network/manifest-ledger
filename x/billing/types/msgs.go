package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	pkguuid "github.com/manifest-network/manifest-ledger/pkg/uuid"
)

var (
	_ sdk.Msg = &MsgFundCredit{}
	_ sdk.Msg = &MsgCreateLease{}
	_ sdk.Msg = &MsgCreateLeaseForTenant{}
	_ sdk.Msg = &MsgCloseLease{}
	_ sdk.Msg = &MsgWithdraw{}
	_ sdk.Msg = &MsgUpdateParams{}
	_ sdk.Msg = &MsgAcknowledgeLease{}
	_ sdk.Msg = &MsgRejectLease{}
	_ sdk.Msg = &MsgCancelLease{}
)

// IsValidDNSLabel checks whether name is a valid DNS label per RFC 1123:
// 1-63 characters, lowercase alphanumeric and hyphens, must start and end with alphanumeric.
func IsValidDNSLabel(name string) bool {
	n := len(name)
	if n == 0 || n > MaxServiceNameLength {
		return false
	}
	for i := 0; i < n; i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-':
			if i == 0 || i == n-1 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// ValidateLeaseItems validates a slice of LeaseItemInput for use in lease creation messages.
// It checks for empty items, hard limit violations, UUID validity, zero quantities, and duplicates.
//
// Dual-mode uniqueness:
//   - If any item has service_name set, all must have it. Uniqueness is enforced on service_name
//     (the same SKU may appear more than once).
//   - If no items have service_name, uniqueness is enforced on sku_uuid (original behaviour).
func ValidateLeaseItems(items []LeaseItemInput) error {
	if len(items) == 0 {
		return ErrEmptyLeaseItems
	}

	// Note: Full max_items_per_lease validation happens in keeper where params are accessible.
	// Basic sanity check here to prevent obviously malicious transactions.
	if len(items) > MaxItemsPerLeaseHardLimit {
		return ErrTooManyLeaseItems.Wrapf("lease has %d items, maximum allowed is %d", len(items), MaxItemsPerLeaseHardLimit)
	}

	// First pass: validate per-item fields and detect service_name mode.
	hasServiceName := 0
	for i, item := range items {
		if item.SkuUuid == "" {
			return ErrInvalidLease.Wrapf("item %d has empty sku_uuid", i)
		}
		if !pkguuid.IsValidUUID(item.SkuUuid) {
			return ErrInvalidLease.Wrapf("item %d has invalid sku_uuid format: %s", i, item.SkuUuid)
		}
		if item.Quantity == 0 {
			return ErrInvalidQuantity.Wrapf("item %d has zero quantity", i)
		}
		if item.Quantity > MaxQuantityPerItem {
			return ErrInvalidQuantity.Wrapf("item %d quantity %d exceeds maximum %d", i, item.Quantity, MaxQuantityPerItem)
		}
		if item.ServiceName != "" {
			hasServiceName++
		}
	}

	// All-or-nothing: either every item has a service_name or none do.
	if hasServiceName > 0 && hasServiceName != len(items) {
		return ErrInvalidServiceName.Wrap("all items must have service_name or none")
	}

	if hasServiceName > 0 {
		// Service-name mode: validate DNS labels and enforce service_name uniqueness.
		seenNames := make(map[string]bool, len(items))
		for i, item := range items {
			if !IsValidDNSLabel(item.ServiceName) {
				return ErrInvalidServiceName.Wrapf("item %d has invalid service_name: %q", i, item.ServiceName)
			}
			if seenNames[item.ServiceName] {
				return ErrInvalidServiceName.Wrapf("duplicate service_name %q", item.ServiceName)
			}
			seenNames[item.ServiceName] = true
		}
	} else {
		// Legacy mode: enforce sku_uuid uniqueness.
		seenSKUs := make(map[string]bool, len(items))
		for _, item := range items {
			if seenSKUs[item.SkuUuid] {
				return ErrDuplicateSKU.Wrapf("sku_uuid %s appears multiple times", item.SkuUuid)
			}
			seenSKUs[item.SkuUuid] = true
		}
	}

	return nil
}

// ValidateBasic performs basic validation for MsgFundCredit.
func (m *MsgFundCredit) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Sender); err != nil {
		return ErrInvalidCreditOperation.Wrapf("invalid sender address: %s", err)
	}

	if _, err := sdk.AccAddressFromBech32(m.Tenant); err != nil {
		return ErrInvalidCreditOperation.Wrapf("invalid tenant address: %s", err)
	}

	if !m.Amount.IsValid() || m.Amount.IsZero() {
		return ErrInvalidCreditOperation.Wrap("amount must be positive")
	}

	// Note: Any valid bank denom is accepted. The denom only needs to match
	// a SKU's base_price denom to be usable for leases with that SKU.

	return nil
}

// ValidateBasic performs basic validation for MsgCreateLease.
func (m *MsgCreateLease) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Tenant); err != nil {
		return ErrInvalidLease.Wrapf("invalid tenant address: %s", err)
	}

	if len(m.MetaHash) > MaxMetaHashLength {
		return ErrInvalidMetaHash.Wrapf("meta_hash exceeds maximum length of %d bytes", MaxMetaHashLength)
	}

	return ValidateLeaseItems(m.Items)
}

// ValidateBasic performs basic validation for MsgCreateLeaseForTenant.
func (m *MsgCreateLeaseForTenant) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return ErrUnauthorized.Wrapf("invalid authority address: %s", err)
	}

	if _, err := sdk.AccAddressFromBech32(m.Tenant); err != nil {
		return ErrInvalidLease.Wrapf("invalid tenant address: %s", err)
	}

	if len(m.MetaHash) > MaxMetaHashLength {
		return ErrInvalidMetaHash.Wrapf("meta_hash exceeds maximum length of %d bytes", MaxMetaHashLength)
	}

	return ValidateLeaseItems(m.Items)
}

// ValidateBasic performs basic validation for MsgCloseLease.
func (m *MsgCloseLease) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Sender); err != nil {
		return ErrInvalidLease.Wrapf("invalid sender address: %s", err)
	}

	if err := ValidateBatchLeaseUUIDs(m.LeaseUuids); err != nil {
		return err
	}

	if len(m.Reason) > MaxClosureReasonLength {
		return ErrInvalidClosureReason.Wrapf("reason exceeds maximum length of %d characters", MaxClosureReasonLength)
	}

	return nil
}

// ValidateBasic performs basic validation for MsgWithdraw.
// Supports two mutually exclusive modes:
// 1. Specific leases: lease_uuids must be set
// 2. Provider-wide: provider_uuid must be set
func (m *MsgWithdraw) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Sender); err != nil {
		return ErrUnauthorized.Wrapf("invalid sender address: %s", err)
	}

	// Check for mutually exclusive modes
	hasLeases := len(m.LeaseUuids) > 0
	hasProvider := m.ProviderUuid != ""

	if hasLeases && hasProvider {
		return ErrInvalidRequest.Wrap("cannot specify both lease_uuids and provider_uuid")
	}
	if !hasLeases && !hasProvider {
		return ErrInvalidRequest.Wrap("must specify either lease_uuids or provider_uuid")
	}

	// Mode 1: Specific leases
	if hasLeases {
		return ValidateBatchLeaseUUIDs(m.LeaseUuids)
	}

	// Mode 2: Provider-wide
	if !pkguuid.IsValidUUID(m.ProviderUuid) {
		return ErrProviderNotFound.Wrapf("invalid provider_uuid format: %s", m.ProviderUuid)
	}

	// Enforce maximum limit to prevent DoS attacks
	if m.Limit > MaxBatchLeaseSize {
		return ErrInvalidLease.Wrapf("limit %d exceeds maximum allowed %d", m.Limit, MaxBatchLeaseSize)
	}

	return nil
}

// ValidateBasic performs basic validation for MsgUpdateParams.
func (m *MsgUpdateParams) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return ErrUnauthorized.Wrapf("invalid authority address: %s", err)
	}

	return m.Params.Validate()
}

// MaxBatchLeaseSize is the maximum number of leases that can be processed in a single batch operation.
const MaxBatchLeaseSize = 100

// ValidateBatchLeaseUUIDs validates a slice of lease UUIDs for batch operations.
// It checks for empty slice, max batch size, UUID format validity, and duplicates.
func ValidateBatchLeaseUUIDs(uuids []string) error {
	if len(uuids) == 0 {
		return ErrInvalidLease.Wrap("lease_uuids cannot be empty")
	}

	if len(uuids) > MaxBatchLeaseSize {
		return ErrInvalidLease.Wrapf("too many leases: %d exceeds maximum %d", len(uuids), MaxBatchLeaseSize)
	}

	seen := make(map[string]bool, len(uuids))
	for i, uuid := range uuids {
		if uuid == "" {
			return ErrInvalidLease.Wrapf("lease_uuids[%d] is empty", i)
		}
		if !pkguuid.IsValidUUID(uuid) {
			return ErrInvalidLease.Wrapf("lease_uuids[%d] invalid format: %s", i, uuid)
		}
		if seen[uuid] {
			return ErrInvalidLease.Wrapf("duplicate lease_uuid: %s", uuid)
		}
		seen[uuid] = true
	}

	return nil
}

// ValidateBasic performs basic validation for MsgAcknowledgeLease.
func (m *MsgAcknowledgeLease) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Sender); err != nil {
		return ErrUnauthorized.Wrapf("invalid sender address: %s", err)
	}

	return ValidateBatchLeaseUUIDs(m.LeaseUuids)
}

// ValidateBasic performs basic validation for MsgRejectLease.
func (m *MsgRejectLease) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Sender); err != nil {
		return ErrUnauthorized.Wrapf("invalid sender address: %s", err)
	}

	if err := ValidateBatchLeaseUUIDs(m.LeaseUuids); err != nil {
		return err
	}

	if len(m.Reason) > MaxRejectionReasonLength {
		return ErrInvalidRejectionReason.Wrapf("reason exceeds maximum length of %d characters", MaxRejectionReasonLength)
	}

	return nil
}

// ValidateBasic performs basic validation for MsgCancelLease.
func (m *MsgCancelLease) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Tenant); err != nil {
		return ErrInvalidLease.Wrapf("invalid tenant address: %s", err)
	}

	return ValidateBatchLeaseUUIDs(m.LeaseUuids)
}
