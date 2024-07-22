package types

import (
	"testing"

	"github.com/stretchr/testify/require"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
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
