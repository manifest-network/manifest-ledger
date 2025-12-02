package types

import (
	"cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.Msg = &MsgCreateSKU{}
	_ sdk.Msg = &MsgUpdateSKU{}
	_ sdk.Msg = &MsgDeleteSKU{}
)

// NewMsgCreateSKU creates a new MsgCreateSKU instance.
func NewMsgCreateSKU(
	authority string,
	provider string,
	name string,
	unit Unit,
	basePrice sdk.Coin,
	metaHash []byte,
) *MsgCreateSKU {
	return &MsgCreateSKU{
		Authority: authority,
		Provider:  provider,
		Name:      name,
		Unit:      unit,
		BasePrice: basePrice,
		MetaHash:  metaHash,
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
	name string,
	unit Unit,
	basePrice sdk.Coin,
	metaHash []byte,
	active bool,
) *MsgUpdateSKU {
	return &MsgUpdateSKU{
		Authority: authority,
		Provider:  provider,
		Id:        id,
		Name:      name,
		Unit:      unit,
		BasePrice: basePrice,
		MetaHash:  metaHash,
		Active:    active,
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

// NewMsgDeleteSKU creates a new MsgDeleteSKU instance.
func NewMsgDeleteSKU(
	authority string,
	provider string,
	id uint64,
) *MsgDeleteSKU {
	return &MsgDeleteSKU{
		Authority: authority,
		Provider:  provider,
		Id:        id,
	}
}

// Route returns the message route.
func (msg *MsgDeleteSKU) Route() string { return ModuleName }

// Type returns the message type.
func (msg *MsgDeleteSKU) Type() string { return "delete_sku" }

// GetSigners returns the expected signers for the message.
func (msg *MsgDeleteSKU) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Authority)
	return []sdk.AccAddress{addr}
}

// Validate performs basic validation.
func (msg *MsgDeleteSKU) Validate() error {
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
