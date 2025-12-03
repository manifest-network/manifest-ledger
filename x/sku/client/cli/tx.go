package cli

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/sku/types"
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
		MsgCreateSKU(),
		MsgUpdateSKU(),
		MsgDeleteSKU(),
		MsgUpdateParams(),
	)

	return txCmd
}

// MsgCreateSKU returns a CLI command handler for creating a SKU.
func MsgCreateSKU() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-sku [provider] [name] [unit] [base-price]",
		Short: "Create a new SKU",
		Long: `Create a new SKU with the given parameters.

Unit values:
  1 = per hour
  2 = per day
  3 = per month
  4 = per unit`,
		Example: "create-sku provider1 \"Compute Instance\" 1 100umfx --meta-hash deadbeef",
		Args:    cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			authority := clientCtx.GetFromAddress()
			provider := args[0]
			name := args[1]

			unitInt, err := strconv.ParseInt(args[2], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid unit: %w", err)
			}
			unit := types.Unit(unitInt)

			basePrice, err := sdk.ParseCoinNormalized(args[3])
			if err != nil {
				return fmt.Errorf("invalid base price: %w", err)
			}

			metaHashStr, _ := cmd.Flags().GetString("meta-hash")
			var metaHash []byte
			if metaHashStr != "" {
				metaHash, err = hex.DecodeString(metaHashStr)
				if err != nil {
					return fmt.Errorf("invalid meta-hash (must be hex): %w", err)
				}
			}

			msg := types.NewMsgCreateSKU(
				authority.String(),
				provider,
				name,
				unit,
				basePrice,
				metaHash,
			)

			if err := msg.Validate(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String("meta-hash", "", "Hex-encoded hash of off-chain metadata")
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// MsgUpdateSKU returns a CLI command handler for updating a SKU.
func MsgUpdateSKU() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-sku [provider] [id] [name] [unit] [base-price] [active]",
		Short: "Update an existing SKU",
		Long: `Update an existing SKU with the given parameters.

Unit values:
  1 = per hour
  2 = per day
  3 = per month
  4 = per unit

Active values:
  true or false`,
		Example: "update-sku provider1 1 \"Updated Name\" 2 200umfx true --meta-hash deadbeef",
		Args:    cobra.ExactArgs(6),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			authority := clientCtx.GetFromAddress()
			provider := args[0]

			id, err := strconv.ParseUint(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid SKU ID: %w", err)
			}

			name := args[2]

			unitInt, err := strconv.ParseInt(args[3], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid unit: %w", err)
			}
			unit := types.Unit(unitInt)

			basePrice, err := sdk.ParseCoinNormalized(args[4])
			if err != nil {
				return fmt.Errorf("invalid base price: %w", err)
			}

			active, err := strconv.ParseBool(args[5])
			if err != nil {
				return fmt.Errorf("invalid active value (must be true or false): %w", err)
			}

			metaHashStr, _ := cmd.Flags().GetString("meta-hash")
			var metaHash []byte
			if metaHashStr != "" {
				metaHash, err = hex.DecodeString(metaHashStr)
				if err != nil {
					return fmt.Errorf("invalid meta-hash (must be hex): %w", err)
				}
			}

			msg := types.NewMsgUpdateSKU(
				authority.String(),
				provider,
				id,
				name,
				unit,
				basePrice,
				metaHash,
				active,
			)

			if err := msg.Validate(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String("meta-hash", "", "Hex-encoded hash of off-chain metadata")
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// MsgDeleteSKU returns a CLI command handler for deleting a SKU.
func MsgDeleteSKU() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete-sku [provider] [id]",
		Short:   "Delete a SKU",
		Example: "delete-sku provider1 1",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			authority := clientCtx.GetFromAddress()
			provider := args[0]

			id, err := strconv.ParseUint(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid SKU ID: %w", err)
			}

			msg := types.NewMsgDeleteSKU(
				authority.String(),
				provider,
				id,
			)

			if err := msg.Validate(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// MsgUpdateParams returns a CLI command handler for updating the module parameters.
func MsgUpdateParams() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-params",
		Short: "Update the module parameters",
		Long: `Update the module parameters including the allowed list.
Only the module authority can execute this command.`,
		Example: "update-params --allowed-list manifest1abc...,manifest1def...",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			authority := clientCtx.GetFromAddress()

			allowedListStr, _ := cmd.Flags().GetString("allowed-list")
			var allowedList []string
			if allowedListStr != "" {
				allowedList = splitAddresses(allowedListStr)
			}

			params := types.Params{
				AllowedList: allowedList,
			}

			msg := types.NewMsgUpdateParams(authority.String(), params)

			if err := msg.Validate(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String("allowed-list", "", "Comma-separated list of addresses allowed to manage SKUs")
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// splitAddresses splits a comma-separated string into a slice of addresses.
func splitAddresses(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	for _, addr := range strings.Split(s, ",") {
		addr = strings.TrimSpace(addr)
		if addr != "" {
			result = append(result, addr)
		}
	}
	return result
}
