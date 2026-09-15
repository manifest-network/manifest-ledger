package helpers

import (
	"context"
	"fmt"
	"strings"

	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"

	sdk "github.com/cosmos/cosmos-sdk/types"

	manifesttypes "github.com/manifest-network/manifest-ledger/x/manifest/types"
)

// ManifestStakeholderPayout distributes manifest funds to the configured stakeholders.
func ManifestStakeholderPayout(ctx context.Context, chain *cosmos.CosmosChain, poaAdmin ibc.Wallet, payouts []manifesttypes.PayoutPair, flags ...string) (sdk.TxResponse, error) {
	output := ""
	for _, payout := range payouts {
		output += fmt.Sprintf("%s:%s%s,", payout.Address, payout.Coin.Amount.String(), payout.Coin.Denom)
	}

	if strings.HasSuffix(output, ",") {
		output = strings.Trim(output, ",")
	}

	txCmd := []string{"tx", "manifest", "payout", output}
	fmt.Println("ManifestStakeholderPayout", txCmd)
	cmd := TxCommandBuilder(ctx, chain, txCmd, poaAdmin.KeyName(), flags...)
	return ExecuteTransaction(ctx, chain, cmd)
}

// ManifestBurnTokens burns the requested amount from the named key.
func ManifestBurnTokens(ctx context.Context, chain *cosmos.CosmosChain, keyName string, amount string, flags ...string) (sdk.TxResponse, error) {
	txCmd := []string{"tx", "manifest", "burn-coins", amount}
	fmt.Println("ManifestBurnTokens", txCmd)
	cmd := TxCommandBuilder(ctx, chain, txCmd, keyName, flags...)
	return ExecuteTransaction(ctx, chain, cmd)
}
