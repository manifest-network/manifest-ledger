package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	"github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers concrete types on the LegacyAmino codec.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	legacy.RegisterAminoMsg(cdc, &MsgFundCredit{}, "lifted/billing/MsgFundCredit")
	legacy.RegisterAminoMsg(cdc, &MsgCreateLease{}, "lifted/billing/MsgCreateLease")
	legacy.RegisterAminoMsg(cdc, &MsgCreateLeaseForTenant{}, "lifted/billing/MsgCreateLeaseForTenant")
	legacy.RegisterAminoMsg(cdc, &MsgCloseLease{}, "lifted/billing/MsgCloseLease")
	legacy.RegisterAminoMsg(cdc, &MsgWithdraw{}, "lifted/billing/MsgWithdraw")
	legacy.RegisterAminoMsg(cdc, &MsgUpdateParams{}, "lifted/billing/MsgUpdateParams")
	legacy.RegisterAminoMsg(cdc, &MsgAcknowledgeLease{}, "lifted/billing/MsgAcknowledgeLease")
	legacy.RegisterAminoMsg(cdc, &MsgRejectLease{}, "lifted/billing/MsgRejectLease")
	legacy.RegisterAminoMsg(cdc, &MsgCancelLease{}, "lifted/billing/MsgCancelLease")
	legacy.RegisterAminoMsg(cdc, &MsgSetLeaseCustomDomain{}, "lifted/billing/MsgSetLeaseCustomDomain")
}

// RegisterInterfaces registers the module's interface types.
func RegisterInterfaces(registry types.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgFundCredit{},
		&MsgCreateLease{},
		&MsgCreateLeaseForTenant{},
		&MsgCloseLease{},
		&MsgWithdraw{},
		&MsgUpdateParams{},
		&MsgAcknowledgeLease{},
		&MsgRejectLease{},
		&MsgCancelLease{},
		&MsgSetLeaseCustomDomain{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
