package types

import (
	"cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ sdk.Msg = &MsgUpdateParams{}

// NewMsgUpdateParams creates new instance of MsgUpdateParams
func NewMsgUpdateParams(
	sender sdk.Address,
	stakeHolders []*StakeHolders,
) *MsgUpdateParams {
	return &MsgUpdateParams{
		Authority: sender.String(),
		Params: Params{
			StakeHolders: stakeHolders,
		},
	}
}

// Route returns the name of the module
func (msg MsgUpdateParams) Route() string { return ModuleName }

// Type returns the the action
func (msg MsgUpdateParams) Type() string { return "update_params" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgUpdateParams) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgUpdateParams message.
func (msg *MsgUpdateParams) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Authority)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgUpdateParams) Validate() error {
	if _, err := sdk.AccAddressFromBech32(msg.Authority); err != nil {
		return errors.Wrap(err, "invalid authority address")
	}

	return msg.Params.Validate()
}

var _ sdk.Msg = &MsgPayoutStakeholders{}

func NewMsgPayoutStakeholders(
	sender sdk.Address,
	coin sdk.Coin,
) *MsgPayoutStakeholders {
	return &MsgPayoutStakeholders{
		Authority: sender.String(),
		Payout:    coin,
	}
}

// Route returns the name of the module
func (msg MsgPayoutStakeholders) Route() string { return ModuleName }

// Type returns the the action
func (msg MsgPayoutStakeholders) Type() string { return "payout" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgPayoutStakeholders) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for the message.
func (msg *MsgPayoutStakeholders) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Authority)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgPayoutStakeholders) Validate() error {
	if _, err := sdk.AccAddressFromBech32(msg.Authority); err != nil {
		return errors.Wrap(err, "invalid authority address")
	}

	return msg.Payout.Validate()
}
