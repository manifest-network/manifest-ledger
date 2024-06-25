package types

import (
	"testing"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

const prefix = "/liftedinit.manifest.v1."

func TestCodecRegisterInterfaces(t *testing.T) {
	registry := codectypes.NewInterfaceRegistry()
	registry.RegisterInterface(sdk.MsgInterfaceProtoName, (*sdk.Msg)(nil))
	RegisterInterfaces(registry)

	impls := registry.ListImplementations(sdk.MsgInterfaceProtoName)

	require.Len(t, impls, 3)
	require.ElementsMatch(t, []string{
		prefix + "MsgPayout",
		prefix + "MsgBurnHeldBalance",
		prefix + "MsgUpdateParams",
	}, impls)
}
