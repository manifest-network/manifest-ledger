package interchaintest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"

	"github.com/manifest-network/manifest-ledger/interchaintest/helpers"
)

// Preserve the pinned interchaintest StoreContract transaction and code-ID
// lookup, replacing only its CopyFile/WriteFile transfer into the live volume.
func storeLiveNodeContract(ctx context.Context, node *cosmos.ChainNode, keyName, fileName string, extraExecTxArgs ...string) (string, error) {
	content, err := os.ReadFile(fileName)
	if err != nil {
		return "", fmt.Errorf("writing contract file to docker volume: %w", err)
	}
	file := filepath.Base(fileName)
	if err := helpers.WriteLiveNodeFile(ctx, node, file, content); err != nil {
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
