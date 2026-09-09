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
The payout address must be permitted by bank policy; protected module accounts are rejected.

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

			metaHash, err := parseMetaHash(cmd)
			if err != nil {
				return err
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
  true  - keep active or reactivate an inactive provider after its SKU cascade completes
  false - keep an already-inactive provider inactive

Note: To deactivate an active provider, use the 'deactivate-provider' command which
properly cascades deactivation to all associated SKUs.
Finish all cascade pages before reactivating; then reactivate desired SKUs individually.
The payout address must be permitted by bank policy; protected module accounts are rejected.

The api-url is the HTTPS endpoint where the provider's off-chain API is hosted.
Omit --api-url to preserve the existing URL, or use --clear-api-url to remove it.
--clear-api-url cannot be combined with a non-empty --api-url.`,
		Example: `update-provider 01912345-6789-7abc-8def-0123456789ab manifest1abc... manifest1def... true --api-url https://api.provider.com
update-provider 01912345-6789-7abc-8def-0123456789ab manifest1abc... manifest1def... true --clear-api-url`,
		Args: cobra.ExactArgs(4),
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

			metaHash, err := parseMetaHash(cmd)
			if err != nil {
				return err
			}

			apiURL, err := cmd.Flags().GetString("api-url")
			if err != nil {
				return err
			}
			clearAPIURL, err := cmd.Flags().GetBool("clear-api-url")
			if err != nil {
				return err
			}

			msg := types.NewMsgUpdateProvider(
				authority.String(),
				uuid,
				address,
				payoutAddress,
				metaHash,
				active,
				apiURL,
			)
			msg.ClearApiUrl = clearAPIURL

			if err := msg.Validate(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String("meta-hash", "", "Hex-encoded hash of off-chain metadata")
	cmd.Flags().String("api-url", "", "HTTPS endpoint where the provider's off-chain API is hosted (empty preserves the existing URL)")
	cmd.Flags().Bool("clear-api-url", false, "Clear the provider's existing API URL (cannot be combined with a non-empty --api-url)")
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// MsgDeactivateProvider returns a CLI command handler for deactivating a Provider.
func MsgDeactivateProvider() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deactivate-provider [uuid]",
		Short: "Deactivate a provider (soft delete)",
		Long: fmt.Sprintf(`Deactivate a provider. This is a soft delete - the provider remains in state but is marked inactive.
Inactive providers cannot have new SKUs or leases created. Existing leases continue operating.

SKU deactivation is paginated to prevent gas exhaustion with many SKUs.
After each transaction is committed successfully, query:
  manifestd query sku skus-by-provider [uuid] --active-only --limit 1 -o json
Repeat deactivation while the query returns any SKUs. Use the same RPC and query
at the committed transaction height or later. Sync broadcast output contains an
SDK admission response, not the module's has_more field; check the committed
transaction's code before continuing.
The cascade must finish before the provider can be reactivated.

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
  2 = per day

Prices must be positive multiples of 3600 (hourly) or 86400 (daily) base units.`,
		Example: "create-sku 01912345-6789-7abc-8def-0123456789ab \"Compute Instance\" 1 3600umfx --meta-hash deadbeef",
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

			metaHash, err := parseMetaHash(cmd)
			if err != nil {
				return err
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
  true  - keep active or reactivate an inactive SKU (requires active provider)
  false - keep an already-inactive SKU inactive

Note: To deactivate an active SKU, use the 'deactivate-sku' command.
Prices must be positive multiples of 3600 (hourly) or 86400 (daily) base units.`,
		Example: "update-sku 01912345-6789-7abc-8def-0123456789ab 01912345-6789-7abc-8def-0123456789ab \"Updated Name\" 2 86400umfx true --meta-hash deadbeef",
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

			metaHash, err := parseMetaHash(cmd)
			if err != nil {
				return err
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
		Long: `Replace the module parameters. Only the module authority can execute this command.
--allowed-list is required. Pass an empty value (--allowed-list="") to explicitly
clear the list; omitting the flag never clears existing permissions.`,
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
	if err := cmd.MarkFlagRequired("allowed-list"); err != nil {
		panic(err)
	}
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

// parseMetaHash parses a hex-encoded meta-hash string from command flags.
func parseMetaHash(cmd *cobra.Command) ([]byte, error) {
	metaHashStr, _ := cmd.Flags().GetString("meta-hash")
	if metaHashStr == "" {
		return nil, nil
	}
	metaHash, err := hex.DecodeString(metaHashStr)
	if err != nil {
		return nil, fmt.Errorf("invalid meta-hash (must be hex): %w", err)
	}
	return metaHash, nil
}
