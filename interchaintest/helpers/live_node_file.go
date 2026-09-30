package helpers

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
)

// WriteLiveNodeFile uploads one file into a running node without changing other
// files. The upstream WriteFile recursively chowns the entire volume, racing live
// database compaction. Set ownership on this archive entry only. Use the stable
// container name because an in-place fork has a separate container lifecycle.
func WriteLiveNodeFile(ctx context.Context, node *cosmos.ChainNode, name string, content []byte) error {
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
