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
		MsgCreateProvider(),
		MsgUpdateProvider(),
		MsgDeactivateProvider(),
		MsgCreateSKU(),
		MsgUpdateSKU(),
		MsgDeactivateSKU(),
		MsgUpdateParams(),
	)

	return txCmd
}

// MsgCreateProvider returns a CLI command handler for creating a Provider.
func MsgCreateProvider() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "create-provider [address] [payout-address]",
		Short:   "Create a new provider",
		Long:    `Create a new provider with the given management and payout addresses.`,
		Example: "create-provider manifest1abc... manifest1def... --meta-hash deadbeef",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			authority := clientCtx.GetFromAddress()
			address := args[0]
			payoutAddress := args[1]

			metaHashStr, _ := cmd.Flags().GetString("meta-hash")
			var metaHash []byte
			if metaHashStr != "" {
				metaHash, err = hex.DecodeString(metaHashStr)
				if err != nil {
					return fmt.Errorf("invalid meta-hash (must be hex): %w", err)
				}
			}

			msg := types.NewMsgCreateProvider(
				authority.String(),
				address,
				payoutAddress,
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

// MsgUpdateProvider returns a CLI command handler for updating a Provider.
func MsgUpdateProvider() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-provider [id] [address] [payout-address] [active]",
		Short: "Update an existing provider",
		Long: `Update an existing provider with the given parameters.

Active values:
  true or false`,
		Example: "update-provider 1 manifest1abc... manifest1def... true --meta-hash deadbeef",
		Args:    cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			authority := clientCtx.GetFromAddress()

			id, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid provider ID: %w", err)
			}

			address := args[1]
			payoutAddress := args[2]

			active, err := strconv.ParseBool(args[3])
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

			msg := types.NewMsgUpdateProvider(
				authority.String(),
				id,
				address,
				payoutAddress,
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

// MsgDeactivateProvider returns a CLI command handler for deactivating a Provider.
func MsgDeactivateProvider() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deactivate-provider [id]",
		Short: "Deactivate a provider (soft delete)",
		Long: `Deactivate a provider. This is a soft delete - the provider remains in state but is marked inactive.
Inactive providers cannot create new SKUs but existing SKUs continue to work.`,
		Example: "deactivate-provider 1",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			authority := clientCtx.GetFromAddress()

			id, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid provider ID: %w", err)
			}

			msg := types.NewMsgDeactivateProvider(
				authority.String(),
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

// MsgCreateSKU returns a CLI command handler for creating a SKU.
func MsgCreateSKU() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-sku [provider-id] [name] [unit] [base-price]",
		Short: "Create a new SKU",
		Long: `Create a new SKU with the given parameters.

Unit values:
  1 = per hour
  2 = per day`,
		Example: "create-sku 1 \"Compute Instance\" 1 100umfx --meta-hash deadbeef",
		Args:    cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			authority := clientCtx.GetFromAddress()

			providerID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid provider ID: %w", err)
			}

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
				providerID,
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
		Use:   "update-sku [id] [provider-id] [name] [unit] [base-price] [active]",
		Short: "Update an existing SKU",
		Long: `Update an existing SKU with the given parameters.

Unit values:
  1 = per hour
  2 = per day

Active values:
  true or false`,
		Example: "update-sku 1 1 \"Updated Name\" 2 200umfx true --meta-hash deadbeef",
		Args:    cobra.ExactArgs(6),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			authority := clientCtx.GetFromAddress()

			id, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid SKU ID: %w", err)
			}

			providerID, err := strconv.ParseUint(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid provider ID: %w", err)
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
				id,
				providerID,
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

// MsgDeactivateSKU returns a CLI command handler for deactivating a SKU.
func MsgDeactivateSKU() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deactivate-sku [id]",
		Short: "Deactivate a SKU (soft delete)",
		Long: `Deactivate a SKU. This is a soft delete - the SKU remains in state but is marked inactive.
Inactive SKUs cannot be used for new leases but existing leases continue with their locked prices.`,
		Example: "deactivate-sku 1",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			authority := clientCtx.GetFromAddress()

			id, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid SKU ID: %w", err)
			}

			msg := types.NewMsgDeactivateSKU(
				authority.String(),
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
		RunE: func(cmd *cobra.Command, _ []string) error {
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
