package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"net"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"

	"github.com/cosmos/cosmos-sdk/client"
	clitestutil "github.com/cosmos/cosmos-sdk/testutil/cli"
	sdk "github.com/cosmos/cosmos-sdk/types"
	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"
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

type paramsSnapshotRPC struct {
	client.CometRPC
	params  types.Params
	heights []int64
}

func (s *paramsSnapshotRPC) ABCIQueryWithOptions(_ context.Context, path string, _ cmtbytes.HexBytes, options rpcclient.ABCIQueryOptions) (*coretypes.ResultABCIQuery, error) {
	if path != "/liftedinit.billing.v1.Query/Params" {
		panic("unexpected query: " + path)
	}
	s.heights = append(s.heights, options.Height)
	value, err := (&types.QueryParamsResponse{Params: s.params}).Marshal()
	if err != nil {
		return nil, err
	}
	return &coretypes.ResultABCIQuery{Response: abci.ResponseQuery{Value: value, Height: 42}}, nil
}

func TestUpdateParamsConstructionSnapshot(t *testing.T) {
	sender := sdk.AccAddress("params-cli-sender!!!").String()
	allowed := sdk.AccAddress("params-allowed-list!").String()
	encoding := moduletestutil.MakeTestEncodingConfig()
	types.RegisterInterfaces(encoding.InterfaceRegistry)
	for _, tc := range []struct {
		name         string
		flags        []string
		wantHeights  []int64
		wantAllowed  []string
		wantSuffixes []string
		err          string
	}{
		{name: "latest omitted lists", wantHeights: []int64{0}, wantAllowed: []string{allowed}, wantSuffixes: []string{".internal"}},
		{name: "pinned omitted lists", flags: []string{"--height=42"}, wantHeights: []int64{42}, wantAllowed: []string{allowed}, wantSuffixes: []string{".internal"}},
		{name: "explicit clear snapshots other list", flags: []string{"--allowed-list="}, wantHeights: []int64{0}, wantSuffixes: []string{".internal"}},
		{name: "explicit lists do not query", flags: []string{"--allowed-list=", "--reserved-domain-suffixes=.local"}, wantSuffixes: []string{".local"}},
		{name: "negative height", flags: []string{"--height=-1"}, err: "must not be negative"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := types.DefaultParams()
			params.AllowedList = []string{allowed}
			params.ReservedDomainSuffixes = []string{".internal"}
			rpc := &paramsSnapshotRPC{params: params}
			var out, diagnostics bytes.Buffer
			clientCtx := client.Context{}.WithCodec(encoding.Codec).WithInterfaceRegistry(encoding.InterfaceRegistry).
				WithTxConfig(encoding.TxConfig).WithLegacyAmino(encoding.Amino).WithClient(rpc).WithOutput(&out)
			cmd := NewUpdateParamsCmd()
			cmd.SetContext(t.Context())
			cmd.SetOut(&out)
			cmd.SetErr(&diagnostics)
			require.NoError(t, client.SetCmdClientContext(cmd, clientCtx))
			cmd.SetArgs(slices.Concat([]string{"100", "20", "3600", "10", "1800", "--from", sender, "--generate-only", "--offline", "--account-number=0", "--sequence=0"}, tc.flags))
			err := cmd.Execute()
			require.Equal(t, tc.wantHeights, rpc.heights)
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				require.NotContains(t, out.String(), `"messages"`)
				return
			}
			require.NoError(t, err)
			txn, err := encoding.TxConfig.TxJSONDecoder()(out.Bytes())
			require.NoError(t, err, out.String())
			require.Len(t, txn.GetMsgs(), 1)
			msg, ok := txn.GetMsgs()[0].(*types.MsgUpdateParams)
			require.True(t, ok)
			require.Equal(t, sender, msg.Authority)
			require.Equal(t, tc.wantAllowed, msg.Params.AllowedList)
			require.Equal(t, tc.wantSuffixes, msg.Params.ReservedDomainSuffixes)
			if len(tc.wantHeights) > 0 {
				require.Contains(t, diagnostics.String(), "execution replaces both lists")
				require.Contains(t, diagnostics.String(), "allowed_list=")
				require.Contains(t, diagnostics.String(), "reserved_domain_suffixes=")
			} else {
				require.Empty(t, diagnostics.String())
			}
		})
	}
}

type paramsSnapshotGRPC struct {
	types.UnimplementedQueryServer
	params   types.Params
	metadata chan metadata.MD
}

func (s *paramsSnapshotGRPC) Params(ctx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	s.metadata <- md.Copy()
	return &types.QueryParamsResponse{Params: s.params}, nil
}

func TestUpdateParamsGRPCSnapshotHeightOverridesInheritedMetadata(t *testing.T) {
	for _, tc := range []struct {
		name   string
		flags  []string
		height string
	}{
		{"pinned", []string{"--height=42"}, "42"},
		{"explicit latest", []string{"--height=0"}, "0"},
		{"default latest", nil, "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encoding := moduletestutil.MakeTestEncodingConfig()
			types.RegisterInterfaces(encoding.InterfaceRegistry)
			params := types.DefaultParams()
			params.AllowedList = []string{sdk.AccAddress("snapshot-allowed!!!").String()}
			params.ReservedDomainSuffixes = []string{".internal"}
			capture := &paramsSnapshotGRPC{params: params, metadata: make(chan metadata.MD, 1)}
			listener := bufconn.Listen(1024 * 1024)
			server := grpc.NewServer()
			types.RegisterQueryServer(server, capture)
			go func() { _ = server.Serve(listener) }()
			connection, err := grpc.NewClient("passthrough:///params-snapshot-test",
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
			)
			require.NoError(t, err)
			t.Cleanup(func() {
				require.NoError(t, connection.Close())
				server.Stop()
				require.NoError(t, listener.Close())
			})
			var out, diagnostics bytes.Buffer
			clientCtx := client.Context{}.WithCodec(encoding.Codec).WithInterfaceRegistry(encoding.InterfaceRegistry).
				WithTxConfig(encoding.TxConfig).WithLegacyAmino(encoding.Amino).WithGRPCClient(connection).WithOutput(&out)
			inherited := metadata.Pairs(grpctypes.GRPCBlockHeightHeader, "999", grpctypes.GRPCBlockHeightHeader, "1000", "x-review-marker", "preserved")
			cmd := NewUpdateParamsCmd()
			cmd.SetContext(metadata.NewOutgoingContext(t.Context(), inherited))
			cmd.SetOut(&out)
			cmd.SetErr(&diagnostics)
			require.NoError(t, client.SetCmdClientContext(cmd, clientCtx))
			cmd.SetArgs(slices.Concat([]string{"100", "20", "3600", "10", "1800", "--from", sdk.AccAddress("snapshot-sender!!!!").String(), "--generate-only", "--offline", "--account-number=0", "--sequence=0"}, tc.flags))
			require.NoError(t, cmd.Execute())
			received := <-capture.metadata
			require.Equal(t, []string{tc.height}, received.Get(grpctypes.GRPCBlockHeightHeader))
			require.Equal(t, []string{"preserved"}, received.Get("x-review-marker"))
			require.Equal(t, []string{"999", "1000"}, inherited.Get(grpctypes.GRPCBlockHeightHeader), "query height must not mutate inherited metadata")
			txn, err := encoding.TxConfig.TxJSONDecoder()(out.Bytes())
			require.NoError(t, err)
			require.Len(t, txn.GetMsgs(), 1)
			message, ok := txn.GetMsgs()[0].(*types.MsgUpdateParams)
			require.True(t, ok)
			require.Equal(t, params.AllowedList, message.Params.AllowedList)
			require.Equal(t, params.ReservedDomainSuffixes, message.Params.ReservedDomainSuffixes)
			require.Contains(t, diagnostics.String(), "query height "+tc.height)
		})
	}
}
