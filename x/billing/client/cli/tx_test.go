package cli

import (
	"encoding/base64"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/client"
	clitestutil "github.com/cosmos/cosmos-sdk/testutil/cli"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestWithdrawCmd(t *testing.T) {
	const (
		leaseUUID1   = "01902a9b-1234-7000-8000-000000000001"
		leaseUUID2   = "01902a9b-1234-7000-8000-000000000002"
		providerUUID = "01902a9b-1234-7000-8000-000000000003"
	)
	sender := sdk.AccAddress("withdraw-cli-sender!").String()
	cursor := []byte(leaseUUID1)
	encodedCursor := base64.StdEncoding.EncodeToString(cursor)
	encoding := moduletestutil.MakeTestEncodingConfig()
	types.RegisterInterfaces(encoding.InterfaceRegistry)
	clientCtx := client.Context{}.
		WithCodec(encoding.Codec).
		WithInterfaceRegistry(encoding.InterfaceRegistry).
		WithTxConfig(encoding.TxConfig).
		WithLegacyAmino(encoding.Amino)

	for _, tc := range []struct {
		name string
		args []string
		want *types.MsgWithdraw
		err  string
	}{
		{
			name: "specific lease with default flags",
			args: []string{leaseUUID1},
			want: &types.MsgWithdraw{Sender: sender, LeaseUuids: []string{leaseUUID1}},
		},
		{
			name: "multiple specific leases",
			args: []string{leaseUUID1, leaseUUID2},
			want: &types.MsgWithdraw{Sender: sender, LeaseUuids: []string{leaseUUID1, leaseUUID2}},
		},
		{
			name: "provider with default pagination",
			args: []string{"--provider", providerUUID},
			want: &types.MsgWithdraw{Sender: sender, ProviderUuid: providerUUID},
		},
		{
			name: "provider with cursor and limit",
			args: []string{"--provider", providerUUID, "--key", encodedCursor, "--limit", "25"},
			want: &types.MsgWithdraw{Sender: sender, ProviderUuid: providerUUID, Key: cursor, Limit: 25},
		},
		{
			name: "provider with explicit default pagination",
			args: []string{"--provider", providerUUID, "--key=", "--limit=0"},
			want: &types.MsgWithdraw{Sender: sender, ProviderUuid: providerUUID},
		},
		{
			name: "specific lease with cursor",
			args: []string{leaseUUID1, "--key", encodedCursor},
			err:  "--key and --limit require --provider",
		},
		{
			name: "specific lease with empty cursor",
			args: []string{leaseUUID1, "--key="},
			err:  "--key and --limit require --provider",
		},
		{
			name: "specific lease with limit",
			args: []string{leaseUUID1, "--limit", "25"},
			err:  "--key and --limit require --provider",
		},
		{
			name: "specific lease with zero limit",
			args: []string{leaseUUID1, "--limit=0"},
			err:  "--key and --limit require --provider",
		},
		{
			name: "mixed modes",
			args: []string{leaseUUID1, "--provider", providerUUID},
			err:  "cannot specify both lease UUIDs and --provider flag",
		},
		{
			name: "missing mode",
			err:  "must specify lease UUIDs or --provider flag",
		},
		{
			name: "provider with malformed cursor",
			args: []string{"--provider", providerUUID, "--key", "not-base64"},
			err:  "invalid --key",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := slices.Concat(tc.args, []string{"--from", sender, "--generate-only", "--offline", "--account-number=0", "--sequence=0"})
			out, err := clitestutil.ExecTestCLICmd(clientCtx, NewWithdrawCmd(), args)
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				require.NotContains(t, out.String(), `"messages"`, "invalid flags must not generate a transaction")
				return
			}
			require.NoError(t, err)
			txn, err := encoding.TxConfig.TxJSONDecoder()(out.Bytes())
			require.NoError(t, err)
			require.Len(t, txn.GetMsgs(), 1)
			require.Equal(t, tc.want, txn.GetMsgs()[0])
		})
	}
}
