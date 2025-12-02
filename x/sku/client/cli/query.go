package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"

	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

// GetQueryCmd returns the cli query commands for the module.
func GetQueryCmd() *cobra.Command {
	queryCmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Querying commands for " + types.ModuleName,
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	queryCmd.AddCommand(
		GetCmdQuerySKU(),
		GetCmdQuerySKUs(),
		GetCmdQuerySKUsByProvider(),
	)

	return queryCmd
}

// GetCmdQuerySKU returns the command to query a SKU by ID.
func GetCmdQuerySKU() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "sku [id]",
		Short:   "Query a SKU by ID",
		Example: "sku 1",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			id, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid SKU ID: %w", err)
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.SKU(cmd.Context(), &types.QuerySKURequest{Id: id})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// GetCmdQuerySKUs returns the command to query all SKUs.
func GetCmdQuerySKUs() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "skus",
		Short:   "Query all SKUs",
		Example: "skus",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.SKUs(cmd.Context(), &types.QuerySKUsRequest{Pagination: pageReq})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	flags.AddPaginationFlagsToCmd(cmd, "skus")
	return cmd
}

// GetCmdQuerySKUsByProvider returns the command to query SKUs by provider.
func GetCmdQuerySKUsByProvider() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "skus-by-provider [provider]",
		Short:   "Query SKUs by provider",
		Example: "skus-by-provider manifest1...",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.SKUsByProvider(cmd.Context(), &types.QuerySKUsByProviderRequest{
				Provider:   args[0],
				Pagination: pageReq,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	flags.AddPaginationFlagsToCmd(cmd, "skus-by-provider")
	return cmd
}
