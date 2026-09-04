package cli_test

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	query "github.com/cosmos/cosmos-sdk/types/query"

	"github.com/manifest-network/manifest-ledger/x/billing/client/cli"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

const testQueryUUID = "01912345-6789-7abc-8def-0123456789ab"

type paginationCaptureServer struct {
	types.UnimplementedQueryServer
	requests chan *query.PageRequest
}

func (s *paginationCaptureServer) capture(request *query.PageRequest) {
	s.requests <- request
}

func (s *paginationCaptureServer) Leases(_ context.Context, request *types.QueryLeasesRequest) (*types.QueryLeasesResponse, error) {
	s.capture(request.Pagination)
	return &types.QueryLeasesResponse{}, nil
}

func (s *paginationCaptureServer) LeasesByTenant(_ context.Context, request *types.QueryLeasesByTenantRequest) (*types.QueryLeasesByTenantResponse, error) {
	s.capture(request.Pagination)
	return &types.QueryLeasesByTenantResponse{}, nil
}

func (s *paginationCaptureServer) LeasesByProvider(_ context.Context, request *types.QueryLeasesByProviderRequest) (*types.QueryLeasesByProviderResponse, error) {
	s.capture(request.Pagination)
	return &types.QueryLeasesByProviderResponse{}, nil
}

func (s *paginationCaptureServer) CreditAccount(_ context.Context, request *types.QueryCreditAccountRequest) (*types.QueryCreditAccountResponse, error) {
	s.capture(request.Pagination)
	return &types.QueryCreditAccountResponse{}, nil
}

func (s *paginationCaptureServer) ProviderWithdrawable(_ context.Context, request *types.QueryProviderWithdrawableRequest) (*types.QueryProviderWithdrawableResponse, error) {
	s.capture(request.Pagination)
	return &types.QueryProviderWithdrawableResponse{}, nil
}

func (s *paginationCaptureServer) CreditAccounts(_ context.Context, request *types.QueryCreditAccountsRequest) (*types.QueryCreditAccountsResponse, error) {
	s.capture(request.Pagination)
	return &types.QueryCreditAccountsResponse{}, nil
}

func (s *paginationCaptureServer) LeasesBySKU(_ context.Context, request *types.QueryLeasesBySKURequest) (*types.QueryLeasesBySKUResponse, error) {
	s.capture(request.Pagination)
	return &types.QueryLeasesBySKUResponse{}, nil
}

func TestPaginatedQueryCommandsDecodeBase64PageKey(t *testing.T) {
	server := &paginationCaptureServer{requests: make(chan *query.PageRequest, 1)}
	clientCtx := newQueryClientContext(t, server)
	rawKey := []byte{0x00, 0x01, 0x7f, 0x80, 0xff, 'k', 'e', 'y'}
	encodedKey := base64.StdEncoding.EncodeToString(rawKey)

	tests := []struct {
		name    string
		command func() *cobra.Command
		args    []string
	}{
		{name: "leases", command: cli.GetLeasesCmd},
		{name: "leases by tenant", command: cli.GetLeasesByTenantCmd, args: []string{"manifest1tenant"}},
		{name: "leases by provider", command: cli.GetLeasesByProviderCmd, args: []string{testQueryUUID}},
		{name: "credit account", command: cli.GetCreditAccountCmd, args: []string{"manifest1tenant"}},
		{name: "provider withdrawable", command: cli.GetProviderWithdrawableCmd, args: []string{testQueryUUID}},
		{name: "credit accounts", command: cli.GetCreditAccountsCmd},
		{name: "leases by SKU", command: cli.GetLeasesBySKUCmd, args: []string{testQueryUUID}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := test.command()
			cmd.SetContext(t.Context())
			require.NoError(t, client.SetCmdClientContext(cmd, clientCtx))
			cmd.SetArgs(append(test.args, "--page-key", encodedKey))

			require.NoError(t, cmd.Execute())
			request := <-server.requests
			require.NotNil(t, request)
			require.Equal(t, rawKey, request.Key)
		})
	}
}

func TestUUIDQueryCommandsRejectNonCanonicalIDsLocally(t *testing.T) {
	server := &paginationCaptureServer{requests: make(chan *query.PageRequest, 1)}
	clientCtx := newQueryClientContext(t, server)
	tests := []struct {
		name       string
		command    func() *cobra.Command
		wantPrefix string
	}{
		{name: "lease", command: cli.GetLeaseCmd, wantPrefix: "invalid lease_uuid format: "},
		{name: "leases by provider", command: cli.GetLeasesByProviderCmd, wantPrefix: "invalid provider_uuid format: "},
		{name: "withdrawable", command: cli.GetWithdrawableAmountCmd, wantPrefix: "invalid lease_uuid format: "},
		{name: "provider withdrawable", command: cli.GetProviderWithdrawableCmd, wantPrefix: "invalid provider_uuid format: "},
		{name: "leases by SKU", command: cli.GetLeasesBySKUCmd, wantPrefix: "invalid sku_uuid format: "},
	}
	invalidUUIDs := []string{
		"not-a-uuid",
		strings.ToUpper(testQueryUUID),
		"01912345-6789-4abc-8def-0123456789ab",
	}

	for _, test := range tests {
		for _, invalidUUID := range invalidUUIDs {
			t.Run(test.name+"/"+invalidUUID, func(t *testing.T) {
				cmd := test.command()
				cmd.SetContext(t.Context())
				require.NoError(t, client.SetCmdClientContext(cmd, clientCtx))
				cmd.SetArgs([]string{invalidUUID})

				err := cmd.Execute()
				require.EqualError(t, err, test.wantPrefix+invalidUUID)
			})
		}
	}
}

func newQueryClientContext(t *testing.T, queryServer types.QueryServer) client.Context {
	t.Helper()

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	types.RegisterQueryServer(server, queryServer)
	go func() {
		_ = server.Serve(listener)
	}()

	connection, err := grpc.NewClient(
		"passthrough:///billing-query-test",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, connection.Close())
		server.Stop()
		require.NoError(t, listener.Close())
	})

	return client.Context{}.
		WithCodec(codec.NewProtoCodec(codectypes.NewInterfaceRegistry())).
		WithGRPCClient(connection).
		WithOutput(io.Discard).
		WithOutputFormat("json")
}
