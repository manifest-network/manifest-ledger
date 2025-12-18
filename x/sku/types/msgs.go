package types

import (
	"net/url"

	"cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	pkguuid "github.com/manifest-network/manifest-ledger/pkg/uuid"
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
	apiURL string,
) *MsgCreateProvider {
	return &MsgCreateProvider{
		Authority:     authority,
		Address:       address,
		PayoutAddress: payoutAddress,
		MetaHash:      metaHash,
		ApiUrl:        apiURL,
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

	// Validate api_url if provided
	if msg.ApiUrl != "" {
		if err := ValidateAPIURL(msg.ApiUrl); err != nil {
			return err
		}
	}

	return nil
}

// NewMsgUpdateProvider creates a new MsgUpdateProvider instance.
func NewMsgUpdateProvider(
	authority string,
	uuid string,
	address string,
	payoutAddress string,
	metaHash []byte,
	active bool,
	apiURL string,
) *MsgUpdateProvider {
	return &MsgUpdateProvider{
		Authority:     authority,
		Uuid:          uuid,
		Address:       address,
		PayoutAddress: payoutAddress,
		MetaHash:      metaHash,
		Active:        active,
		ApiUrl:        apiURL,
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

	if err := pkguuid.ValidateUUIDv7(msg.Uuid); err != nil {
		return errors.Wrap(ErrInvalidProvider, err.Error())
	}

	if _, err := sdk.AccAddressFromBech32(msg.Address); err != nil {
		return errors.Wrap(err, "invalid provider address")
	}

	if _, err := sdk.AccAddressFromBech32(msg.PayoutAddress); err != nil {
		return errors.Wrap(err, "invalid payout address")
	}

	// Validate api_url if provided (empty means keep existing)
	if msg.ApiUrl != "" {
		if err := ValidateAPIURL(msg.ApiUrl); err != nil {
			return err
		}
	}

	return nil
}

// NewMsgDeactivateProvider creates a new MsgDeactivateProvider instance.
func NewMsgDeactivateProvider(
	authority string,
	uuid string,
) *MsgDeactivateProvider {
	return &MsgDeactivateProvider{
		Authority: authority,
		Uuid:      uuid,
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

	if err := pkguuid.ValidateUUIDv7(msg.Uuid); err != nil {
		return errors.Wrap(ErrInvalidProvider, err.Error())
	}

	return nil
}

// NewMsgCreateSKU creates a new MsgCreateSKU instance.
func NewMsgCreateSKU(
	authority string,
	providerUUID string,
	name string,
	unit Unit,
	basePrice sdk.Coin,
	metaHash []byte,
) *MsgCreateSKU {
	return &MsgCreateSKU{
		Authority:    authority,
		ProviderUuid: providerUUID,
		Name:         name,
		Unit:         unit,
		BasePrice:    basePrice,
		MetaHash:     metaHash,
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

	if err := pkguuid.ValidateUUIDv7(msg.ProviderUuid); err != nil {
		return errors.Wrap(ErrInvalidSKU, "invalid provider_uuid: "+err.Error())
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
	uuid string,
	providerUUID string,
	name string,
	unit Unit,
	basePrice sdk.Coin,
	metaHash []byte,
	active bool,
) *MsgUpdateSKU {
	return &MsgUpdateSKU{
		Authority:    authority,
		Uuid:         uuid,
		ProviderUuid: providerUUID,
		Name:         name,
		Unit:         unit,
		BasePrice:    basePrice,
		MetaHash:     metaHash,
		Active:       active,
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

	if err := pkguuid.ValidateUUIDv7(msg.Uuid); err != nil {
		return errors.Wrap(ErrInvalidSKU, "invalid uuid: "+err.Error())
	}

	if err := pkguuid.ValidateUUIDv7(msg.ProviderUuid); err != nil {
		return errors.Wrap(ErrInvalidSKU, "invalid provider_uuid: "+err.Error())
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
	uuid string,
) *MsgDeactivateSKU {
	return &MsgDeactivateSKU{
		Authority: authority,
		Uuid:      uuid,
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

	if err := pkguuid.ValidateUUIDv7(msg.Uuid); err != nil {
		return errors.Wrap(ErrInvalidSKU, "invalid uuid: "+err.Error())
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

// ValidateAPIURL validates that the API URL is a valid HTTPS URL.
func ValidateAPIURL(apiURL string) error {
	if len(apiURL) > MaxAPIURLLength {
		return ErrInvalidAPIURL.Wrapf("api_url exceeds maximum length of %d characters", MaxAPIURLLength)
	}

	parsedURL, err := url.Parse(apiURL)
	if err != nil {
		return ErrInvalidAPIURL.Wrapf("failed to parse api_url: %s", err)
	}

	if parsedURL.Scheme != "https" {
		return ErrInvalidAPIURL.Wrap("api_url must use HTTPS scheme")
	}

	if parsedURL.Host == "" {
		return ErrInvalidAPIURL.Wrap("api_url must have a valid host")
	}

	// Reject URLs with user info (credentials in URL)
	if parsedURL.User != nil {
		return ErrInvalidAPIURL.Wrap("api_url must not contain user credentials")
	}

	return nil
}
