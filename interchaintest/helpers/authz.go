package helpers

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AuthzExec ports the pinned interchaintest ChainNode.AuthzExec, uploading the
// generated message with WriteLiveNodeFile instead of the recursive chown.
func AuthzExec(ctx context.Context, node *cosmos.ChainNode, grantee ibc.Wallet, nestedMsgCmd []string) (*sdk.TxResponse, error) {
	const fileName = "authz.json"
	if !strings.Contains(strings.Join(nestedMsgCmd, " "), "--generate-only") {
		nestedMsgCmd = append(nestedMsgCmd, "--generate-only")
	}
	res, resErr, err := node.Exec(ctx, nestedMsgCmd, node.Chain.Config().Env)
	if resErr != nil {
		return nil, fmt.Errorf("failed to generate msg: %s", resErr)
	}
	if err != nil {
		return nil, err
	}
	if err := WriteLiveNodeFile(ctx, node, fileName, res); err != nil {
		return nil, err
	}
	txHash, err := node.ExecTx(ctx, grantee.KeyName(), "authz", "exec", path.Join(node.HomeDir(), fileName))
	if err != nil {
		return nil, err
	}
	return node.TxHashToResponse(ctx, txHash)
}
