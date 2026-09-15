package interchaintest

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
)

// The upstream WriteFile recursively chowns the entire volume, racing live
// database compaction. Set ownership on this archive entry only. Use the stable
// container name because an in-place fork has a separate container lifecycle.
func writeLiveNodeFile(ctx context.Context, node *cosmos.ChainNode, name string, content []byte) error {
	if name == "" || name == "." || name == ".." || path.Base(name) != name {
		return fmt.Errorf("live upload requires a file name, got %q", name)
	}
	uidText, gidText, ok := strings.Cut(node.Image.UIDGID, ":")
	if !ok {
		return fmt.Errorf("live upload requires numeric UID:GID, got %q", node.Image.UIDGID)
	}
	uid, err := strconv.Atoi(uidText)
	if err != nil || uid < 0 {
		return fmt.Errorf("invalid upload UID %q", uidText)
	}
	gid, err := strconv.Atoi(gidText)
	if err != nil || gid < 0 {
		return fmt.Errorf("invalid upload GID %q", gidText)
	}
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(content)), Uid: uid, Gid: gid}); err != nil {
		return err
	}
	if _, err := writer.Write(content); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return node.DockerClient.CopyToContainer(ctx, node.Name(), node.HomeDir(), &archive, container.CopyToContainerOptions{})
}

// Preserve the pinned interchaintest StoreContract transaction and code-ID
// lookup, replacing only its CopyFile/WriteFile transfer into the live volume.
func storeLiveNodeContract(ctx context.Context, node *cosmos.ChainNode, keyName, fileName string, extraExecTxArgs ...string) (string, error) {
	content, err := os.ReadFile(fileName)
	if err != nil {
		return "", fmt.Errorf("writing contract file to docker volume: %w", err)
	}
	file := filepath.Base(fileName)
	if err := writeLiveNodeFile(ctx, node, file, content); err != nil {
		return "", fmt.Errorf("writing contract file to docker volume: %w", err)
	}
	command := []string{"wasm", "store", path.Join(node.HomeDir(), file), "--gas", "auto"}
	command = append(command, extraExecTxArgs...)
	if _, err := node.ExecTx(ctx, keyName, command...); err != nil {
		return "", err
	}
	if err := testutil.WaitForBlocks(ctx, 5, node.Chain); err != nil {
		return "", fmt.Errorf("wait for blocks: %w", err)
	}
	stdout, _, err := node.ExecQuery(ctx, "wasm", "list-code", "--reverse")
	if err != nil {
		return "", err
	}
	var codes cosmos.CodeInfosResponse
	if err := json.Unmarshal(stdout, &codes); err != nil {
		return "", err
	}
	if len(codes.CodeInfos) == 0 || codes.CodeInfos[0].CodeID == "" {
		return "", fmt.Errorf("no code ID returned after storing contract")
	}
	return codes.CodeInfos[0].CodeID, nil
}
