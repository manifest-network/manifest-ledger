package types

import (
	"cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.Msg = &MsgCreateProvider{}
	_ sdk.Msg = &MsgUpdateProvider{}
	_ sdk.Msg = &MsgDeactivateProvider{}
	_ sdk.Msg = &MsgCreateSKU{}
	_ sdk.Msg = &MsgUpdateSKU{}
	_ sdk.Msg = &MsgDeactivateSKU{}
	_ sdk.Msg = &MsgUpdateParams{}
)

// NewMsgCreateProvider creates a new MsgCreateProvider instance.
func NewMsgCreateProvider(
	authority string,
	address string,
	payoutAddress string,
	metaHash []byte,
) *MsgCreateProvider {
	return &MsgCreateProvider{
		Authority:     authority,
		Address:       address,
		PayoutAddress: payoutAddress,
		MetaHash:      metaHash,
	}
}

// Route returns the message route.
func (msg *MsgCreateProvider) Route() string { return ModuleName }

// Type returns the message type.
func (msg *MsgCreateProvider) Type() string { return "create_provider" }

// GetSigners returns the expected signers for the message.
func (msg *MsgCreateProvider) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Authority)
	return []sdk.AccAddress{addr}
}

// Validate performs basic validation.
func (msg *MsgCreateProvider) Validate() error {
	if _, err := sdk.AccAddressFromBech32(msg.Authority); err != nil {
		return errors.Wrap(err, "invalid authority address")
	}

	if _, err := sdk.AccAddressFromBech32(msg.Address); err != nil {
		return errors.Wrap(err, "invalid provider address")
	}

	if _, err := sdk.AccAddressFromBech32(msg.PayoutAddress); err != nil {
		return errors.Wrap(err, "invalid payout address")
	}

	return nil
}

// NewMsgUpdateProvider creates a new MsgUpdateProvider instance.
func NewMsgUpdateProvider(
	authority string,
	id uint64,
	address string,
	payoutAddress string,
	metaHash []byte,
	active bool,
) *MsgUpdateProvider {
	return &MsgUpdateProvider{
		Authority:     authority,
		Id:            id,
		Address:       address,
		PayoutAddress: payoutAddress,
		MetaHash:      metaHash,
		Active:        active,
	}
}

// Route returns the message route.
func (msg *MsgUpdateProvider) Route() string { return ModuleName }

// Type returns the message type.
func (msg *MsgUpdateProvider) Type() string { return "update_provider" }

// GetSigners returns the expected signers for the message.
func (msg *MsgUpdateProvider) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Authority)
	return []sdk.AccAddress{addr}
}

// Validate performs basic validation.
func (msg *MsgUpdateProvider) Validate() error {
	if _, err := sdk.AccAddressFromBech32(msg.Authority); err != nil {
		return errors.Wrap(err, "invalid authority address")
	}

	if msg.Id == 0 {
		return errors.Wrap(ErrInvalidProvider, "id cannot be zero")
	}

	if _, err := sdk.AccAddressFromBech32(msg.Address); err != nil {
		return errors.Wrap(err, "invalid provider address")
	}

	if _, err := sdk.AccAddressFromBech32(msg.PayoutAddress); err != nil {
		return errors.Wrap(err, "invalid payout address")
	}

	return nil
}

// NewMsgDeactivateProvider creates a new MsgDeactivateProvider instance.
func NewMsgDeactivateProvider(
	authority string,
	id uint64,
) *MsgDeactivateProvider {
	return &MsgDeactivateProvider{
		Authority: authority,
		Id:        id,
	}
}

// Route returns the message route.
func (msg *MsgDeactivateProvider) Route() string { return ModuleName }

// Type returns the message type.
func (msg *MsgDeactivateProvider) Type() string { return "deactivate_provider" }

// GetSigners returns the expected signers for the message.
func (msg *MsgDeactivateProvider) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Authority)
	return []sdk.AccAddress{addr}
}

// Validate performs basic validation.
func (msg *MsgDeactivateProvider) Validate() error {
	if _, err := sdk.AccAddressFromBech32(msg.Authority); err != nil {
		return errors.Wrap(err, "invalid authority address")
	}

	if msg.Id == 0 {
		return errors.Wrap(ErrInvalidProvider, "id cannot be zero")
	}

	return nil
}

// NewMsgCreateSKU creates a new MsgCreateSKU instance.
func NewMsgCreateSKU(
	authority string,
	providerID uint64,
	name string,
	unit Unit,
	basePrice sdk.Coin,
	metaHash []byte,
) *MsgCreateSKU {
	return &MsgCreateSKU{
		Authority:  authority,
		ProviderId: providerID,
		Name:       name,
		Unit:       unit,
		BasePrice:  basePrice,
		MetaHash:   metaHash,
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

	if msg.ProviderId == 0 {
		return errors.Wrap(ErrInvalidSKU, "provider_id cannot be zero")
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

	// Validate that price and unit combination produces a non-zero per-second rate
	if err := ValidatePriceAndUnit(msg.BasePrice, msg.Unit); err != nil {
		return errors.Wrap(ErrInvalidSKU, err.Error())
	}

	return nil
}

// NewMsgUpdateSKU creates a new MsgUpdateSKU instance.
func NewMsgUpdateSKU(
	authority string,
	id uint64,
	providerID uint64,
	name string,
	unit Unit,
	basePrice sdk.Coin,
	metaHash []byte,
	active bool,
) *MsgUpdateSKU {
	return &MsgUpdateSKU{
		Authority:  authority,
		Id:         id,
		ProviderId: providerID,
		Name:       name,
		Unit:       unit,
		BasePrice:  basePrice,
		MetaHash:   metaHash,
		Active:     active,
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

	if msg.Id == 0 {
		return errors.Wrap(ErrInvalidSKU, "id cannot be zero")
	}

	if msg.ProviderId == 0 {
		return errors.Wrap(ErrInvalidSKU, "provider_id cannot be zero")
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

	// Validate that price and unit combination produces a non-zero per-second rate
	if err := ValidatePriceAndUnit(msg.BasePrice, msg.Unit); err != nil {
		return errors.Wrap(ErrInvalidSKU, err.Error())
	}

	return nil
}

// NewMsgDeactivateSKU creates a new MsgDeactivateSKU instance.
func NewMsgDeactivateSKU(
	authority string,
	id uint64,
) *MsgDeactivateSKU {
	return &MsgDeactivateSKU{
		Authority: authority,
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
