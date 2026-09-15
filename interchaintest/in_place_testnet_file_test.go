package interchaintest

import (
	"archive/tar"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dockerclient "github.com/moby/moby/client"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type inPlaceTestnetRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn inPlaceTestnetRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

// Exercise the actual upload through Docker's archive API with no daemon or
// network. Any helper-container creation or recursive chown would fail this test.
func TestInPlaceTestnetLiveFileUpload(t *testing.T) {
	chain := cosmos.NewCosmosChain(t.Name(), ibc.ChainConfig{Name: "fork-home", ChainID: "source-chain"}, 1, 0, zap.NewNop())
	node := &cosmos.ChainNode{Chain: chain, TestName: t.Name(), Validator: true, Image: ibc.DockerImage{UIDGID: "1025:1026"}}
	content := []byte("\x00asm\x01\x00\x00\x00")
	requests := 0
	client, err := dockerclient.NewClientWithOpts(
		dockerclient.WithHost("http://docker.invalid"), dockerclient.WithVersion("1.44"),
		dockerclient.WithHTTPClient(&http.Client{Transport: inPlaceTestnetRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests++
			require.Equal(t, http.MethodPut, request.Method)
			require.Equal(t, "/v1.44/containers/"+node.Name()+"/archive", request.URL.Path)
			require.Equal(t, node.HomeDir(), request.URL.Query().Get("path"))
			reader := tar.NewReader(request.Body)
			header, err := reader.Next()
			require.NoError(t, err)
			require.Equal(t, "counter.wasm", header.Name)
			require.Equal(t, byte(tar.TypeReg), header.Typeflag)
			require.Equal(t, int64(0o600), header.Mode)
			require.Equal(t, 1025, header.Uid)
			require.Equal(t, 1026, header.Gid)
			require.Equal(t, int64(len(content)), header.Size)
			actual, err := io.ReadAll(reader)
			require.NoError(t, err)
			require.Equal(t, content, actual)
			_, err = reader.Next()
			require.ErrorIs(t, err, io.EOF)
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
		})}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	node.DockerClient = client
	require.NoError(t, writeLiveNodeFile(context.Background(), node, "counter.wasm", content))
	require.Equal(t, 1, requests)
	for _, tc := range []struct{ name, file, owner string }{
		{"empty name", "", "1025:1026"},
		{"directory", ".", "1025:1026"},
		{"parent", "..", "1025:1026"},
		{"relative path", "../counter.wasm", "1025:1026"},
		{"absolute path", "/counter.wasm", "1025:1026"},
		{"invalid tar name", "bad\x00name", "1025:1026"},
		{"missing gid", "counter.wasm", "1025"},
		{"invalid uid", "counter.wasm", "user:1026"},
		{"negative uid", "counter.wasm", "-1:1026"},
		{"invalid gid", "counter.wasm", "1025:group"},
		{"negative gid", "counter.wasm", "1025:-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node.Image.UIDGID = tc.owner
			require.Error(t, writeLiveNodeFile(context.Background(), node, tc.file, content))
			require.Equal(t, 1, requests, "invalid input must not reach the Docker API")
		})
	}
}

func TestInPlaceTestnetLiveContractUploadFailure(t *testing.T) {
	for _, missingFile := range []bool{true, false} {
		name := "archive API failure"
		if missingFile {
			name = "missing local contract"
		}
		t.Run(name, func(t *testing.T) {
			chain := cosmos.NewCosmosChain(t.Name(), ibc.ChainConfig{Name: "live-home", ChainID: "live-chain"}, 1, 0, zap.NewNop())
			node := &cosmos.ChainNode{Chain: chain, TestName: t.Name(), Validator: true, Image: ibc.DockerImage{UIDGID: "1025:1026"}}
			fileName := filepath.Join(t.TempDir(), "counter.wasm")
			content := []byte("\x00asm\x01\x00\x00\x00")
			if !missingFile {
				require.NoError(t, os.WriteFile(fileName, content, 0o600))
			}
			requests := 0
			client, err := dockerclient.NewClientWithOpts(
				dockerclient.WithHost("http://docker.invalid"), dockerclient.WithVersion("1.44"),
				dockerclient.WithHTTPClient(&http.Client{Transport: inPlaceTestnetRoundTripFunc(func(request *http.Request) (*http.Response, error) {
					requests++
					require.Equal(t, http.MethodPut, request.Method, "upload failure must not create a helper or start a transaction")
					require.Equal(t, "/v1.44/containers/"+node.Name()+"/archive", request.URL.Path)
					require.Equal(t, node.HomeDir(), request.URL.Query().Get("path"))
					reader := tar.NewReader(request.Body)
					header, err := reader.Next()
					require.NoError(t, err)
					require.Equal(t, "counter.wasm", header.Name)
					actual, err := io.ReadAll(reader)
					require.NoError(t, err)
					require.Equal(t, content, actual)
					_, err = reader.Next()
					require.ErrorIs(t, err, io.EOF)
					return &http.Response{StatusCode: http.StatusInternalServerError, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"message":"archive refused"}`))}, nil
				})}),
			)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, client.Close()) })
			node.DockerClient = client
			codeID, err := storeLiveNodeContract(context.Background(), node, "uploader", fileName)
			require.ErrorContains(t, err, "writing contract file to docker volume")
			require.Empty(t, codeID)
			if missingFile {
				require.ErrorIs(t, err, os.ErrNotExist)
				require.Zero(t, requests)
			} else {
				require.ErrorContains(t, err, "archive refused")
				require.Equal(t, 1, requests)
			}
		})
	}
}
