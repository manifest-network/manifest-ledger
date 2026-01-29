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
		Use:   "create-provider [address] [payout-address]",
		Short: "Create a new provider",
		Long: `Create a new provider with the given management and payout addresses.

The api-url is optional and must be a valid HTTPS URL where the provider's
off-chain API is hosted for tenant authentication and connection details.`,
		Example: "create-provider manifest1abc... manifest1def... --api-url https://api.provider.com",
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

			apiURL, _ := cmd.Flags().GetString("api-url")

			msg := types.NewMsgCreateProvider(
				authority.String(),
				address,
				payoutAddress,
				metaHash,
				apiURL,
			)

			if err := msg.Validate(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String("meta-hash", "", "Hex-encoded hash of off-chain metadata")
	cmd.Flags().String("api-url", "", "HTTPS endpoint where the provider's off-chain API is hosted")
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// MsgUpdateProvider returns a CLI command handler for updating a Provider.
func MsgUpdateProvider() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-provider [uuid] [address] [payout-address] [active]",
		Short: "Update an existing provider",
		Long: `Update an existing provider with the given parameters.

Active values:
  true or false

The api-url is the HTTPS endpoint where the provider's off-chain API is hosted.`,
		Example: "update-provider 01912345-6789-7abc-8def-0123456789ab manifest1abc... manifest1def... true --api-url https://api.provider.com",
		Args:    cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			authority := clientCtx.GetFromAddress()

			uuid := args[0]
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

			apiURL, _ := cmd.Flags().GetString("api-url")

			msg := types.NewMsgUpdateProvider(
				authority.String(),
				uuid,
				address,
				payoutAddress,
				metaHash,
				active,
				apiURL,
			)

			if err := msg.Validate(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String("meta-hash", "", "Hex-encoded hash of off-chain metadata")
	cmd.Flags().String("api-url", "", "HTTPS endpoint where the provider's off-chain API is hosted")
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// MsgDeactivateProvider returns a CLI command handler for deactivating a Provider.
func MsgDeactivateProvider() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deactivate-provider [uuid]",
		Short: "Deactivate a provider (soft delete)",
		Long: fmt.Sprintf(`Deactivate a provider. This is a soft delete - the provider remains in state but is marked inactive.
Inactive providers cannot create new SKUs but existing SKUs continue to work.

SKU deactivation is paginated to prevent gas exhaustion with many SKUs.
If has_more is true in the response, call again to continue deactivating SKUs.

Use --limit to control how many SKUs are deactivated per call (default %d, max %d).`,
			types.DefaultDeactivateSKULimit, types.MaxDeactivateSKULimit),
		Example: "deactivate-provider 01912345-6789-7abc-8def-0123456789ab --limit 50",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			authority := clientCtx.GetFromAddress()
			uuid := args[0]

			limit, err := cmd.Flags().GetUint64("limit")
			if err != nil {
				return err
			}

			msg := types.NewMsgDeactivateProvider(
				authority.String(),
				uuid,
				limit,
			)

			if err := msg.Validate(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().Uint64("limit", 0, fmt.Sprintf("Maximum SKUs to deactivate per call (0 = default %d, max %d)", types.DefaultDeactivateSKULimit, types.MaxDeactivateSKULimit))
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// MsgCreateSKU returns a CLI command handler for creating a SKU.
func MsgCreateSKU() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-sku [provider-uuid] [name] [unit] [base-price]",
		Short: "Create a new SKU",
		Long: `Create a new SKU with the given parameters.

Unit values:
  1 = per hour
  2 = per day`,
		Example: "create-sku 01912345-6789-7abc-8def-0123456789ab \"Compute Instance\" 1 100umfx --meta-hash deadbeef",
		Args:    cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			authority := clientCtx.GetFromAddress()

			providerUUID := args[0]
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
				providerUUID,
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
		Use:   "update-sku [uuid] [provider-uuid] [name] [unit] [base-price] [active]",
		Short: "Update an existing SKU",
		Long: `Update an existing SKU with the given parameters.

Unit values:
  1 = per hour
  2 = per day

Active values:
  true or false`,
		Example: "update-sku 01912345-6789-7abc-8def-0123456789ab 01912345-6789-7abc-8def-0123456789ab \"Updated Name\" 2 200umfx true --meta-hash deadbeef",
		Args:    cobra.ExactArgs(6),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			authority := clientCtx.GetFromAddress()

			uuid := args[0]
			providerUUID := args[1]
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
				uuid,
				providerUUID,
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
		Use:   "deactivate-sku [uuid]",
		Short: "Deactivate a SKU (soft delete)",
		Long: `Deactivate a SKU. This is a soft delete - the SKU remains in state but is marked inactive.
Inactive SKUs cannot be used for new leases but existing leases continue with their locked prices.`,
		Example: "deactivate-sku 01912345-6789-7abc-8def-0123456789ab",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			authority := clientCtx.GetFromAddress()

			uuid := args[0]

			msg := types.NewMsgDeactivateSKU(
				authority.String(),
				uuid,
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
