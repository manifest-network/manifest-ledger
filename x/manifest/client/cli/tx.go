package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/manifest/types"
)

// NewTxCmd returns a root CLI command handler for certain modules
// transaction commands.
func NewTxCmd() *cobra.Command {
	txCmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      types.ModuleName + " subcommands.",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	txCmd.AddCommand(
		MsgBurnCoins(),
		MsgPayout(),
	)
	return txCmd
}

// MsgPayout returns a CLI command handler for paying out wallets.
func MsgPayout() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "payout [address:coin_amount,...]",
		Short:   "Payout (authority)",
		Example: `payout manifest1abc:50_000umfx,manifest1xyz:1_000_000umfx`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cliCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			authority := cliCtx.GetFromAddress()

			payoutPairs, err := fromStrToPayout(args[0])
			if err != nil {
				return err
			}

			msg := types.NewMsgPayout(authority, payoutPairs)
			if err := msg.Validate(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(cliCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// MsgBurnCoins returns a CLI command handler for burning held coins.
func MsgBurnCoins() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "burn-coins [coins]",
		Short:   "Burn held coins",
		Example: `burn-coins 50000umfx,100other`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cliCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			sender := cliCtx.GetFromAddress()

			coins, err := sdk.ParseCoinsNormalized(args[0])
			if err != nil {
				return err
			}

			msg := types.NewMsgBurnHeldBalance(sender, coins)
			if err := msg.Validate(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(cliCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// fromStrToPayout converts a string to a slice of StakeHolders.
// ex: manifest1abc:50_000umfx,manifest1xyz:1_000_000umfx
func fromStrToPayout(s string) ([]types.PayoutPair, error) {
	payouts := make([]types.PayoutPair, 0)

	s = strings.ReplaceAll(s, "_", "")

	for _, pairing := range strings.Split(s, ",") {
		parts := strings.Split(pairing, ":")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid pairing: %s", pairing)
		}

		strAddr := parts[0]
		if _, err := sdk.AccAddressFromBech32(strAddr); err != nil {
			return nil, fmt.Errorf("invalid address: %s", strAddr)
		}

		strCoin := parts[1]
		coin, err := sdk.ParseCoinNormalized(strCoin)
		if err != nil {
			return nil, fmt.Errorf("invalid coin: %s", strCoin)
		}

		if err := coin.Validate(); err != nil {
			return nil, fmt.Errorf("invalid coin: %s for address: %s", strCoin, strAddr)
		}

		payouts = append(payouts, types.PayoutPair{
			Address: strAddr,
			Coin:    coin,
		})
	}

	return payouts, nil
}
