package cli

import (
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
		GetCmdQueryParams(),
		GetCmdQueryProvider(),
		GetCmdQueryProviderByAddress(),
		GetCmdQueryProviders(),
		GetCmdQuerySKU(),
		GetCmdQuerySKUs(),
		GetCmdQuerySKUsByProvider(),
	)

	return queryCmd
}

// GetCmdQueryParams returns the command to query the module parameters.
func GetCmdQueryParams() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "params",
		Short:   "Query the module parameters",
		Example: "params",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.Params(cmd.Context(), &types.QueryParamsRequest{})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// GetCmdQueryProvider returns the command to query a Provider by ID.
func GetCmdQueryProvider() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "provider [uuid]",
		Short:   "Query a provider by UUID",
		Example: "provider 01912345-6789-7abc-8def-0123456789ab",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			uuid := args[0]

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.Provider(cmd.Context(), &types.QueryProviderRequest{Uuid: uuid})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// GetCmdQueryProviders returns the command to query all Providers.
func GetCmdQueryProviders() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "providers",
		Short:   "Query all providers",
		Example: "providers --active-only",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			flagSet, err := client.FlagSetWithPageKeyDecoded(cmd.Flags())
			if err != nil {
				return err
			}

			pageReq, err := client.ReadPageRequest(flagSet)
			if err != nil {
				return err
			}

			activeOnly, err := cmd.Flags().GetBool("active-only")
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.Providers(cmd.Context(), &types.QueryProvidersRequest{
				Pagination: pageReq,
				ActiveOnly: activeOnly,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	cmd.Flags().Bool("active-only", false, "Filter to return only active providers")
	flags.AddQueryFlagsToCmd(cmd)
	flags.AddPaginationFlagsToCmd(cmd, "providers")
	return cmd
}

// GetCmdQuerySKU returns the command to query a SKU by UUID.
func GetCmdQuerySKU() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "sku [uuid]",
		Short:   "Query a SKU by UUID",
		Example: "sku 01912345-6789-7abc-8def-0123456789ab",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			uuid := args[0]

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.SKU(cmd.Context(), &types.QuerySKURequest{Uuid: uuid})
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
		Example: "skus --active-only",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			flagSet, err := client.FlagSetWithPageKeyDecoded(cmd.Flags())
			if err != nil {
				return err
			}

			pageReq, err := client.ReadPageRequest(flagSet)
			if err != nil {
				return err
			}

			activeOnly, err := cmd.Flags().GetBool("active-only")
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.SKUs(cmd.Context(), &types.QuerySKUsRequest{
				Pagination: pageReq,
				ActiveOnly: activeOnly,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	cmd.Flags().Bool("active-only", false, "Filter to return only active SKUs")
	flags.AddQueryFlagsToCmd(cmd)
	flags.AddPaginationFlagsToCmd(cmd, "skus")
	return cmd
}

// GetCmdQuerySKUsByProvider returns the command to query SKUs by provider ID.
func GetCmdQuerySKUsByProvider() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "skus-by-provider [provider-uuid]",
		Short:   "Query SKUs by provider UUID",
		Example: "skus-by-provider 01912345-6789-7abc-8def-0123456789ab --active-only",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			providerUUID := args[0]

			flagSet, err := client.FlagSetWithPageKeyDecoded(cmd.Flags())
			if err != nil {
				return err
			}

			pageReq, err := client.ReadPageRequest(flagSet)
			if err != nil {
				return err
			}

			activeOnly, err := cmd.Flags().GetBool("active-only")
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.SKUsByProvider(cmd.Context(), &types.QuerySKUsByProviderRequest{
				ProviderUuid: providerUUID,
				Pagination:   pageReq,
				ActiveOnly:   activeOnly,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	cmd.Flags().Bool("active-only", false, "Filter to return only active SKUs")
	flags.AddQueryFlagsToCmd(cmd)
	flags.AddPaginationFlagsToCmd(cmd, "skus-by-provider")
	return cmd
}

// GetCmdQueryProviderByAddress returns the command to query a Provider by address.
func GetCmdQueryProviderByAddress() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provider-by-address [address]",
		Short: "Query a provider by management address",
		Long: `Query a provider by its management address.

This is useful when you know your address but not your provider UUID.`,
		Example: "provider-by-address manifest1abc...",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.ProviderByAddress(cmd.Context(), &types.QueryProviderByAddressRequest{
				Address: args[0],
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}
