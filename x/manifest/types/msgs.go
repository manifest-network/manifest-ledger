package types

import (
	fmt "fmt"

	"cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ sdk.Msg = &MsgUpdateParams{}

// NewMsgUpdateParams creates new instance of MsgUpdateParams
func NewMsgUpdateParams(
	sender sdk.Address,
) *MsgUpdateParams {
	return &MsgUpdateParams{
		Authority: sender.String(),
		Params:    NewParams(),
	}
}

// Route returns the name of the module
func (msg MsgUpdateParams) Route() string { return ModuleName }

// Type returns the the action
func (msg MsgUpdateParams) Type() string { return "update_params" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgUpdateParams) GetSignBytes() []byte {
	return sdk.MustSortJSON(amino.MustMarshalJSON(&msg))
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

var _ sdk.Msg = &MsgPayout{}

func NewMsgPayout(
	sender sdk.Address,
	payouts []PayoutPair,
) *MsgPayout {
	return &MsgPayout{
		Authority:   sender.String(),
		PayoutPairs: payouts,
	}
}

func NewPayoutPair(addr sdk.AccAddress, denom string, amt int64) PayoutPair {
	return PayoutPair{
		Address: addr.String(),
		Coin:    sdk.NewCoin(denom, math.NewInt(amt)),
	}
}

// Route returns the name of the module
func (msg MsgPayout) Route() string { return ModuleName }

// Type returns the the action
func (msg MsgPayout) Type() string { return "payout" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgPayout) GetSignBytes() []byte {
	return sdk.MustSortJSON(amino.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for the message.
func (msg *MsgPayout) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Authority)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgPayout) Validate() error {
	if _, err := sdk.AccAddressFromBech32(msg.Authority); err != nil {
		return errors.Wrap(err, "invalid authority address")
	}

	if len(msg.PayoutPairs) == 0 {
		return fmt.Errorf("payouts cannot be empty")
	}

	for _, p := range msg.PayoutPairs {
		p := p

		addr := p.Address
		coin := p.Coin

		if _, err := sdk.AccAddressFromBech32(addr); err != nil {
			return errors.Wrapf(err, "invalid address %s", addr)
		}

		if coin.IsZero() {
			return fmt.Errorf("coin cannot be zero for address: %s", addr)
		}

		if coin.IsNegative() {
			return fmt.Errorf("coin cannot be negative for address: %s", addr)
		}

		if err := coin.Validate(); err != nil {
			return errors.Wrapf(err, "invalid coin: %v for address: %s", coin, addr)
		}
	}

	return nil
}

var _ sdk.Msg = &MsgBurnHeldBalance{}

func NewMsgBurnHeldBalance(
	sender sdk.Address,
	coins sdk.Coins,
) *MsgBurnHeldBalance {
	return &MsgBurnHeldBalance{
		Sender:    sender.String(),
		BurnCoins: coins,
	}
}

// Route returns the name of the module
func (msg MsgBurnHeldBalance) Route() string { return ModuleName }

// Type returns the the action
func (msg MsgBurnHeldBalance) Type() string { return "burn_coins" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgBurnHeldBalance) GetSignBytes() []byte {
	return sdk.MustSortJSON(amino.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for the message.
func (msg *MsgBurnHeldBalance) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Sender)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgBurnHeldBalance) Validate() error {
	if _, err := sdk.AccAddressFromBech32(msg.Sender); err != nil {
		return errors.Wrap(err, "invalid authority address")
	}

	if msg.BurnCoins.Len() == 0 {
		return fmt.Errorf("burn coins cannot be empty")
	}

	return msg.BurnCoins.Validate()
}
