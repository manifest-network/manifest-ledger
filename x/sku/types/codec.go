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
	legacy.RegisterAminoMsg(cdc, &MsgCreateProvider{}, "lifted/sku/MsgCreateProvider")
	legacy.RegisterAminoMsg(cdc, &MsgUpdateProvider{}, "lifted/sku/MsgUpdateProvider")
	legacy.RegisterAminoMsg(cdc, &MsgDeactivateProvider{}, "lifted/sku/MsgDeactivateProvider")
	legacy.RegisterAminoMsg(cdc, &MsgCreateSKU{}, "lifted/sku/MsgCreateSKU")
	legacy.RegisterAminoMsg(cdc, &MsgUpdateSKU{}, "lifted/sku/MsgUpdateSKU")
	legacy.RegisterAminoMsg(cdc, &MsgDeactivateSKU{}, "lifted/sku/MsgDeactivateSKU")
	legacy.RegisterAminoMsg(cdc, &MsgUpdateParams{}, "lifted/sku/MsgUpdateParams")
}

// RegisterInterfaces registers the module's interface types.
func RegisterInterfaces(registry types.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgCreateProvider{},
		&MsgUpdateProvider{},
		&MsgDeactivateProvider{},
		&MsgCreateSKU{},
		&MsgUpdateSKU{},
		&MsgDeactivateSKU{},
		&MsgUpdateParams{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
