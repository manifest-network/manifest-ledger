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
	_ sdk.Msg = &MsgWithdrawAll{}
	_ sdk.Msg = &MsgUpdateParams{}
	_ sdk.Msg = &MsgAcknowledgeLease{}
	_ sdk.Msg = &MsgRejectLease{}
	_ sdk.Msg = &MsgCancelLease{}
)

// ValidateLeaseItems validates a slice of LeaseItemInput for use in lease creation messages.
// It checks for empty items, hard limit violations, UUID validity, zero quantities, and duplicates.
func ValidateLeaseItems(items []LeaseItemInput) error {
	if len(items) == 0 {
		return ErrEmptyLeaseItems
	}

	// Note: Full max_items_per_lease validation happens in keeper where params are accessible.
	// Basic sanity check here to prevent obviously malicious transactions.
	if len(items) > MaxItemsPerLeaseHardLimit {
		return ErrTooManyLeaseItems.Wrapf("lease has %d items, maximum allowed is %d", len(items), MaxItemsPerLeaseHardLimit)
	}

	seenSKUs := make(map[string]bool)
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
		if seenSKUs[item.SkuUuid] {
			return ErrDuplicateSKU.Wrapf("sku_uuid %s appears multiple times", item.SkuUuid)
		}
		seenSKUs[item.SkuUuid] = true
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

	return ValidateLeaseItems(m.Items)
}

// ValidateBasic performs basic validation for MsgCloseLease.
func (m *MsgCloseLease) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Sender); err != nil {
		return ErrInvalidLease.Wrapf("invalid sender address: %s", err)
	}

	if m.LeaseUuid == "" {
		return ErrInvalidLease.Wrap("lease_uuid cannot be empty")
	}

	if !pkguuid.IsValidUUID(m.LeaseUuid) {
		return ErrInvalidLease.Wrapf("invalid lease_uuid format: %s", m.LeaseUuid)
	}

	return nil
}

// ValidateBasic performs basic validation for MsgWithdraw.
func (m *MsgWithdraw) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Sender); err != nil {
		return ErrUnauthorized.Wrapf("invalid sender address: %s", err)
	}

	if m.LeaseUuid == "" {
		return ErrInvalidLease.Wrap("lease_uuid cannot be empty")
	}

	if !pkguuid.IsValidUUID(m.LeaseUuid) {
		return ErrInvalidLease.Wrapf("invalid lease_uuid format: %s", m.LeaseUuid)
	}

	return nil
}

// ValidateBasic performs basic validation for MsgWithdrawAll.
func (m *MsgWithdrawAll) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Sender); err != nil {
		return ErrUnauthorized.Wrapf("invalid sender address: %s", err)
	}

	if m.ProviderUuid == "" {
		return ErrProviderNotFound.Wrap("provider_uuid cannot be empty")
	}

	if !pkguuid.IsValidUUID(m.ProviderUuid) {
		return ErrProviderNotFound.Wrapf("invalid provider_uuid format: %s", m.ProviderUuid)
	}

	// Enforce maximum limit to prevent DoS attacks
	if m.Limit > MaxWithdrawAllLimit {
		return ErrInvalidLease.Wrapf("limit %d exceeds maximum allowed %d", m.Limit, MaxWithdrawAllLimit)
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

// ValidateBasic performs basic validation for MsgAcknowledgeLease.
func (m *MsgAcknowledgeLease) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Sender); err != nil {
		return ErrUnauthorized.Wrapf("invalid sender address: %s", err)
	}

	if m.LeaseUuid == "" {
		return ErrInvalidLease.Wrap("lease_uuid cannot be empty")
	}

	if !pkguuid.IsValidUUID(m.LeaseUuid) {
		return ErrInvalidLease.Wrapf("invalid lease_uuid format: %s", m.LeaseUuid)
	}

	return nil
}

// ValidateBasic performs basic validation for MsgRejectLease.
func (m *MsgRejectLease) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Sender); err != nil {
		return ErrUnauthorized.Wrapf("invalid sender address: %s", err)
	}

	if m.LeaseUuid == "" {
		return ErrInvalidLease.Wrap("lease_uuid cannot be empty")
	}

	if !pkguuid.IsValidUUID(m.LeaseUuid) {
		return ErrInvalidLease.Wrapf("invalid lease_uuid format: %s", m.LeaseUuid)
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

	if m.LeaseUuid == "" {
		return ErrInvalidLease.Wrap("lease_uuid cannot be empty")
	}

	if !pkguuid.IsValidUUID(m.LeaseUuid) {
		return ErrInvalidLease.Wrapf("invalid lease_uuid format: %s", m.LeaseUuid)
	}

	return nil
}
