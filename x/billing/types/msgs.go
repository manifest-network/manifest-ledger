package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.Msg = &MsgFundCredit{}
	_ sdk.Msg = &MsgCreateLease{}
	_ sdk.Msg = &MsgCloseLease{}
	_ sdk.Msg = &MsgWithdraw{}
	_ sdk.Msg = &MsgWithdrawAll{}
	_ sdk.Msg = &MsgUpdateParams{}
)

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

	// Note: Denom validation against module params happens in the keeper
	// because ValidateBasic cannot access module state.

	return nil
}

// ValidateBasic performs basic validation for MsgCreateLease.
func (m *MsgCreateLease) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Tenant); err != nil {
		return ErrInvalidLease.Wrapf("invalid tenant address: %s", err)
	}

	if len(m.Items) == 0 {
		return ErrEmptyLeaseItems
	}

	seenSKUs := make(map[uint64]bool)
	for i, item := range m.Items {
		if item.SkuId == 0 {
			return ErrInvalidLease.Wrapf("item %d has zero sku_id", i)
		}
		if item.Quantity == 0 {
			return ErrInvalidQuantity.Wrapf("item %d has zero quantity", i)
		}
		if seenSKUs[item.SkuId] {
			return ErrDuplicateSKU.Wrapf("sku_id %d appears multiple times", item.SkuId)
		}
		seenSKUs[item.SkuId] = true
	}

	return nil
}

// ValidateBasic performs basic validation for MsgCloseLease.
func (m *MsgCloseLease) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Sender); err != nil {
		return ErrInvalidLease.Wrapf("invalid sender address: %s", err)
	}

	if m.LeaseId == 0 {
		return ErrInvalidLease.Wrap("lease_id cannot be zero")
	}

	return nil
}

// ValidateBasic performs basic validation for MsgWithdraw.
func (m *MsgWithdraw) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Sender); err != nil {
		return ErrUnauthorized.Wrapf("invalid sender address: %s", err)
	}

	if m.LeaseId == 0 {
		return ErrInvalidLease.Wrap("lease_id cannot be zero")
	}

	return nil
}

// ValidateBasic performs basic validation for MsgWithdrawAll.
func (m *MsgWithdrawAll) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Sender); err != nil {
		return ErrUnauthorized.Wrapf("invalid sender address: %s", err)
	}

	// provider_id can be zero if sender is the provider address itself
	// validation of provider ownership happens in the keeper

	return nil
}

// ValidateBasic performs basic validation for MsgUpdateParams.
func (m *MsgUpdateParams) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return ErrUnauthorized.Wrapf("invalid authority address: %s", err)
	}

	return m.Params.Validate()
}
