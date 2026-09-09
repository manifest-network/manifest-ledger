package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"

	"github.com/manifest-network/manifest-ledger/x/sku/client/cli"
	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

type capturedUUIDQuery struct {
	method string
	uuid   string
}

type uuidCaptureServer struct {
	types.UnimplementedQueryServer
	requests chan capturedUUIDQuery
}

func (s *uuidCaptureServer) Provider(_ context.Context, req *types.QueryProviderRequest) (*types.QueryProviderResponse, error) {
	s.requests <- capturedUUIDQuery{method: "provider", uuid: req.Uuid}
	return &types.QueryProviderResponse{}, nil
}

func (s *uuidCaptureServer) SKU(_ context.Context, req *types.QuerySKURequest) (*types.QuerySKUResponse, error) {
	s.requests <- capturedUUIDQuery{method: "sku", uuid: req.Uuid}
	return &types.QuerySKUResponse{}, nil
}

func (s *uuidCaptureServer) SKUsByProvider(_ context.Context, req *types.QuerySKUsByProviderRequest) (*types.QuerySKUsByProviderResponse, error) {
	s.requests <- capturedUUIDQuery{method: "skus-by-provider", uuid: req.ProviderUuid}
	return &types.QuerySKUsByProviderResponse{}, nil
}

func TestUUIDQueryCommandsForwardNonCanonicalIDs(t *testing.T) {
	server := &uuidCaptureServer{requests: make(chan capturedUUIDQuery, 1)}
	clientCtx := newQueryClientContext(t, server)
	nonCanonicalUUIDs := []string{
		"not-a-canonical-uuid",
		strings.ToUpper("01912345-6789-7abc-8def-0123456789ab"),
		"01912345-6789-4abc-8def-0123456789ab",
	}
	tests := []struct {
		name    string
		method  string
		command func() *cobra.Command
	}{
		{name: "provider", method: "provider", command: cli.GetCmdQueryProvider},
		{name: "SKU", method: "sku", command: cli.GetCmdQuerySKU},
		{name: "SKUs by provider", method: "skus-by-provider", command: cli.GetCmdQuerySKUsByProvider},
	}

	for _, test := range tests {
		for _, nonCanonicalUUID := range nonCanonicalUUIDs {
			t.Run(test.name+"/"+nonCanonicalUUID, func(t *testing.T) {
				cmd := test.command()
				cmd.SetContext(t.Context())
				require.NoError(t, client.SetCmdClientContext(cmd, clientCtx))
				cmd.SetArgs([]string{nonCanonicalUUID})

				require.NoError(t, cmd.Execute())
				require.Equal(t, capturedUUIDQuery{
					method: test.method,
					uuid:   nonCanonicalUUID,
				}, <-server.requests)
			})
		}
	}
}

type activeSKUQueryRPC struct {
	client.CometRPC
	request types.QuerySKUsByProviderRequest
	height  int64
	path    string
}

func (s *activeSKUQueryRPC) ABCIQueryWithOptions(_ context.Context, path string, data cmtbytes.HexBytes, options rpcclient.ABCIQueryOptions) (*coretypes.ResultABCIQuery, error) {
	if err := s.request.Unmarshal(data); err != nil {
		return nil, err
	}
	s.height = options.Height
	s.path = path
	response, err := (&types.QuerySKUsByProviderResponse{}).Marshal()
	if err != nil {
		return nil, err
	}
	return &coretypes.ResultABCIQuery{Response: abci.ResponseQuery{Value: response, Height: options.Height}}, nil
}

func TestSKUsByProviderCascadeCompletionQuery(t *testing.T) {
	const providerUUID = "01902a9b-1234-7000-8000-000000000003"
	server := &activeSKUQueryRPC{}
	var out bytes.Buffer
	clientCtx := client.Context{}.
		WithCodec(codec.NewProtoCodec(codectypes.NewInterfaceRegistry())).
		WithClient(server).
		WithOutput(&out).
		WithOutputFormat("json")
	cmd := cli.GetCmdQuerySKUsByProvider()
	cmd.SetContext(t.Context())
	require.NoError(t, client.SetCmdClientContext(cmd, clientCtx))
	cmd.SetArgs([]string{providerUUID, "--active-only", "--limit", "1", "--height", "42", "-o", "json"})
	require.NoError(t, cmd.Execute())
	require.Equal(t, providerUUID, server.request.ProviderUuid)
	require.True(t, server.request.ActiveOnly)
	require.Equal(t, uint64(1), server.request.Pagination.Limit)
	require.Equal(t, int64(42), server.height)
	require.Equal(t, "/liftedinit.sku.v1.Query/SKUsByProvider", server.path)
	var decoded struct {
		SKUs []json.RawMessage `json:"skus"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &decoded))
	require.NotNil(t, decoded.SKUs, "the documented jq stop condition requires an empty array")
	require.Empty(t, decoded.SKUs)
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
		"passthrough:///sku-query-test",
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
