package helpers

import (
	"context"
	"fmt"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"

	manifesttypes "github.com/liftedinit/manifest-ledger/x/manifest/types"
)

func ManifestStakeholderPayout(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, poaAdmin ibc.Wallet, payouts []manifesttypes.PayoutPair, flags ...string) (sdk.TxResponse, error) {
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

func ManifestBurnTokens(t *testing.T, ctx context.Context, chain *cosmos.CosmosChain, keyName string, amount string, flags ...string) (sdk.TxResponse, error) {
	txCmd := []string{"tx", "manifest", "burn-coins", amount}
	fmt.Println("ManifestBurnTokens", txCmd)
	cmd := TxCommandBuilder(ctx, chain, txCmd, keyName, flags...)
	return ExecuteTransaction(ctx, chain, cmd)
}
