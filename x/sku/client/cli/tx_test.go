package cli

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/client"
	clitestutil "github.com/cosmos/cosmos-sdk/testutil/cli"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"

	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

func TestMsgUpdateProviderClearAPIURL(t *testing.T) {
	const providerUUID = "01902a9b-1234-7000-8000-000000000003"
	sender := sdk.AccAddress("provider-cli-sender!").String()
	address := sdk.AccAddress("provider-cli-address").String()
	payout := sdk.AccAddress("provider-cli-payout!").String()
	encoding := moduletestutil.MakeTestEncodingConfig()
	types.RegisterInterfaces(encoding.InterfaceRegistry)
	clientCtx := client.Context{}.
		WithCodec(encoding.Codec).
		WithInterfaceRegistry(encoding.InterfaceRegistry).
		WithTxConfig(encoding.TxConfig).
		WithLegacyAmino(encoding.Amino)

	for _, tc := range []struct {
		name  string
		flags []string
		url   string
		clear bool
		err   string
	}{
		{name: "omitted URL preserves existing value"},
		{name: "explicit empty URL preserves existing value", flags: []string{"--api-url="}},
		{name: "clear URL", flags: []string{"--clear-api-url"}, clear: true},
		{name: "clear with empty URL", flags: []string{"--clear-api-url", "--api-url="}, clear: true},
		{name: "replace URL", flags: []string{"--api-url=https://provider.example"}, url: "https://provider.example"},
		{name: "conflicting flags", flags: []string{"--clear-api-url", "--api-url=https://provider.example"}, err: "clear_api_url cannot be true when api_url is non-empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := slices.Concat([]string{providerUUID, address, payout, "true"}, tc.flags,
				[]string{"--from", sender, "--generate-only", "--offline", "--account-number=0", "--sequence=0"})
			out, err := clitestutil.ExecTestCLICmd(clientCtx, MsgUpdateProvider(), args)
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				require.NotContains(t, out.String(), `"messages"`, "invalid flags must not generate a transaction")
				return
			}
			require.NoError(t, err)
			txn, err := encoding.TxConfig.TxJSONDecoder()(out.Bytes())
			require.NoError(t, err)
			require.Len(t, txn.GetMsgs(), 1)
			require.Equal(t, &types.MsgUpdateProvider{
				Authority: sender, Uuid: providerUUID, Address: address, PayoutAddress: payout,
				Active: true, ApiUrl: tc.url, ClearApiUrl: tc.clear,
			}, txn.GetMsgs()[0])
		})
	}
}

func TestMsgUpdateParamsRequiresExplicitAllowedList(t *testing.T) {
	sender := sdk.AccAddress("sku-params-authority").String()
	allowed := sdk.AccAddress("sku-params-operator").String()
	encoding := moduletestutil.MakeTestEncodingConfig()
	types.RegisterInterfaces(encoding.InterfaceRegistry)
	clientCtx := client.Context{}.WithCodec(encoding.Codec).
		WithInterfaceRegistry(encoding.InterfaceRegistry).WithTxConfig(encoding.TxConfig).
		WithLegacyAmino(encoding.Amino)
	for _, tc := range []struct {
		name string
		args []string
		want []string
		err  bool
	}{
		{name: "omitted list cannot erase permissions", err: true},
		{name: "explicit empty list", args: []string{"--allowed-list="}},
		{name: "explicit replacement", args: []string{"--allowed-list=" + allowed}, want: []string{allowed}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := slices.Concat(tc.args, []string{"--from", sender, "--generate-only", "--offline", "--account-number=0", "--sequence=0"})
			out, err := clitestutil.ExecTestCLICmd(clientCtx, MsgUpdateParams(), args)
			if tc.err {
				require.ErrorContains(t, err, `required flag(s) "allowed-list" not set`)
				require.NotContains(t, out.String(), `"messages"`)
				return
			}
			require.NoError(t, err)
			txn, err := encoding.TxConfig.TxJSONDecoder()(out.Bytes())
			require.NoError(t, err)
			require.Len(t, txn.GetMsgs(), 1)
			msg := txn.GetMsgs()[0].(*types.MsgUpdateParams)
			require.Equal(t, sender, msg.Authority)
			require.Equal(t, tc.want, msg.Params.AllowedList)
		})
	}
}
