package types

import (
	"cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.Msg = &MsgCreateSKU{}
	_ sdk.Msg = &MsgUpdateSKU{}
	_ sdk.Msg = &MsgDeactivateSKU{}
	_ sdk.Msg = &MsgUpdateParams{}
)

// NewMsgCreateSKU creates a new MsgCreateSKU instance.
func NewMsgCreateSKU(
	authority string,
	provider string,
	payoutAddress string,
	name string,
	unit Unit,
	basePrice sdk.Coin,
	metaHash []byte,
) *MsgCreateSKU {
	return &MsgCreateSKU{
		Authority:     authority,
		Provider:      provider,
		PayoutAddress: payoutAddress,
		Name:          name,
		Unit:          unit,
		BasePrice:     basePrice,
		MetaHash:      metaHash,
	}
}

// Route returns the message route.
func (msg *MsgCreateSKU) Route() string { return ModuleName }

// Type returns the message type.
func (msg *MsgCreateSKU) Type() string { return "create_sku" }

// GetSigners returns the expected signers for the message.
func (msg *MsgCreateSKU) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Authority)
	return []sdk.AccAddress{addr}
}

// Validate performs basic validation.
func (msg *MsgCreateSKU) Validate() error {
	if _, err := sdk.AccAddressFromBech32(msg.Authority); err != nil {
		return errors.Wrap(err, "invalid authority address")
	}

	if msg.Provider == "" {
		return errors.Wrap(ErrInvalidSKU, "provider cannot be empty")
	}

	if _, err := sdk.AccAddressFromBech32(msg.PayoutAddress); err != nil {
		return errors.Wrap(err, "invalid payout address")
	}

	if msg.Name == "" {
		return errors.Wrap(ErrInvalidSKU, "name cannot be empty")
	}

	if msg.Unit == Unit_UNIT_UNSPECIFIED {
		return errors.Wrap(ErrInvalidSKU, "unit cannot be unspecified")
	}

	if !msg.BasePrice.IsValid() || msg.BasePrice.IsZero() {
		return errors.Wrap(ErrInvalidSKU, "base price must be valid and non-zero")
	}

	return nil
}

// NewMsgUpdateSKU creates a new MsgUpdateSKU instance.
func NewMsgUpdateSKU(
	authority string,
	provider string,
	id uint64,
	payoutAddress string,
	name string,
	unit Unit,
	basePrice sdk.Coin,
	metaHash []byte,
	active bool,
) *MsgUpdateSKU {
	return &MsgUpdateSKU{
		Authority:     authority,
		Provider:      provider,
		Id:            id,
		PayoutAddress: payoutAddress,
		Name:          name,
		Unit:          unit,
		BasePrice:     basePrice,
		MetaHash:      metaHash,
		Active:        active,
	}
}

// Route returns the message route.
func (msg *MsgUpdateSKU) Route() string { return ModuleName }

// Type returns the message type.
func (msg *MsgUpdateSKU) Type() string { return "update_sku" }

// GetSigners returns the expected signers for the message.
func (msg *MsgUpdateSKU) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Authority)
	return []sdk.AccAddress{addr}
}

// Validate performs basic validation.
func (msg *MsgUpdateSKU) Validate() error {
	if _, err := sdk.AccAddressFromBech32(msg.Authority); err != nil {
		return errors.Wrap(err, "invalid authority address")
	}

	if msg.Provider == "" {
		return errors.Wrap(ErrInvalidSKU, "provider cannot be empty")
	}

	if msg.Id == 0 {
		return errors.Wrap(ErrInvalidSKU, "id cannot be zero")
	}

	if _, err := sdk.AccAddressFromBech32(msg.PayoutAddress); err != nil {
		return errors.Wrap(err, "invalid payout address")
	}

	if msg.Name == "" {
		return errors.Wrap(ErrInvalidSKU, "name cannot be empty")
	}

	if msg.Unit == Unit_UNIT_UNSPECIFIED {
		return errors.Wrap(ErrInvalidSKU, "unit cannot be unspecified")
	}

	if !msg.BasePrice.IsValid() || msg.BasePrice.IsZero() {
		return errors.Wrap(ErrInvalidSKU, "base price must be valid and non-zero")
	}

	return nil
}

// NewMsgDeactivateSKU creates a new MsgDeactivateSKU instance.
func NewMsgDeactivateSKU(
	authority string,
	provider string,
	id uint64,
) *MsgDeactivateSKU {
	return &MsgDeactivateSKU{
		Authority: authority,
		Provider:  provider,
		Id:        id,
	}
}

// Route returns the message route.
func (msg *MsgDeactivateSKU) Route() string { return ModuleName }

// Type returns the message type.
func (msg *MsgDeactivateSKU) Type() string { return "deactivate_sku" }

// GetSigners returns the expected signers for the message.
func (msg *MsgDeactivateSKU) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Authority)
	return []sdk.AccAddress{addr}
}

// Validate performs basic validation.
func (msg *MsgDeactivateSKU) Validate() error {
	if _, err := sdk.AccAddressFromBech32(msg.Authority); err != nil {
		return errors.Wrap(err, "invalid authority address")
	}

	if msg.Provider == "" {
		return errors.Wrap(ErrInvalidSKU, "provider cannot be empty")
	}

	if msg.Id == 0 {
		return errors.Wrap(ErrInvalidSKU, "id cannot be zero")
	}

	return nil
}

// NewMsgUpdateParams creates a new MsgUpdateParams instance.
func NewMsgUpdateParams(authority string, params Params) *MsgUpdateParams {
	return &MsgUpdateParams{
		Authority: authority,
		Params:    params,
	}
}

// Route returns the message route.
func (msg *MsgUpdateParams) Route() string { return ModuleName }

// Type returns the message type.
func (msg *MsgUpdateParams) Type() string { return "update_params" }

// GetSigners returns the expected signers for the message.
func (msg *MsgUpdateParams) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Authority)
	return []sdk.AccAddress{addr}
}

// Validate performs basic validation.
func (msg *MsgUpdateParams) Validate() error {
	if _, err := sdk.AccAddressFromBech32(msg.Authority); err != nil {
		return errors.Wrap(err, "invalid authority address")
	}

	return msg.Params.Validate()
}
