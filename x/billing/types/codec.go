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
	legacy.RegisterAminoMsg(cdc, &MsgSetItemCustomDomain{}, "lifted/billing/MsgSetItemCustomDomain")
	legacy.RegisterAminoMsg(cdc, &MsgUpdateLease{}, "lifted/billing/MsgUpdateLease")
	// Abbreviated verb: amino caps msg names at 39 characters for Ledger nano
	// signing and the unabbreviated name is 40. Must stay in sync with the
	// amino.name option on MsgAcknowledgeLeaseUpdate in tx.proto.
	legacy.RegisterAminoMsg(cdc, &MsgAcknowledgeLeaseUpdate{}, "lifted/billing/MsgAckLeaseUpdate")
	legacy.RegisterAminoMsg(cdc, &MsgRejectLeaseUpdate{}, "lifted/billing/MsgRejectLeaseUpdate")
	legacy.RegisterAminoMsg(cdc, &MsgCancelLeaseUpdate{}, "lifted/billing/MsgCancelLeaseUpdate")
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
		&MsgSetItemCustomDomain{},
		&MsgUpdateLease{},
		&MsgAcknowledgeLeaseUpdate{},
		&MsgRejectLeaseUpdate{},
		&MsgCancelLeaseUpdate{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
