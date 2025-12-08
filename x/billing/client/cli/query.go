package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// GetQueryCmd returns the query commands for the billing module.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Querying commands for the billing module",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		GetParamsCmd(),
		GetLeaseCmd(),
		GetLeasesCmd(),
		GetLeasesByTenantCmd(),
		GetLeasesByProviderCmd(),
		GetCreditAccountCmd(),
		GetCreditAddressCmd(),
		GetWithdrawableAmountCmd(),
		GetProviderWithdrawableCmd(),
	)

	return cmd
}

// GetParamsCmd returns the command to query module parameters.
func GetParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "params",
		Short:   "Query the billing module parameters",
		Example: `params`,
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

// GetLeaseCmd returns the command to query a lease by ID.
func GetLeaseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "lease [lease-id]",
		Short:   "Query a lease by ID",
		Example: `lease 1`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			leaseID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid lease_id: %w", err)
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.Lease(cmd.Context(), &types.QueryLeaseRequest{LeaseId: leaseID})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

// GetLeasesCmd returns the command to query all leases.
func GetLeasesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "leases",
		Short:   "Query all leases with pagination",
		Example: `leases --active-only --limit 10`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			activeOnly, _ := cmd.Flags().GetBool("active-only")
			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.Leases(cmd.Context(), &types.QueryLeasesRequest{
				Pagination: pageReq,
				ActiveOnly: activeOnly,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	cmd.Flags().Bool("active-only", false, "Filter to only active leases")
	flags.AddQueryFlagsToCmd(cmd)
	flags.AddPaginationFlagsToCmd(cmd, "leases")

	return cmd
}

// GetLeasesByTenantCmd returns the command to query leases by tenant.
func GetLeasesByTenantCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "leases-by-tenant [tenant]",
		Short:   "Query leases by tenant address",
		Example: `leases-by-tenant manifest1abc... --active-only`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			activeOnly, _ := cmd.Flags().GetBool("active-only")
			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.LeasesByTenant(cmd.Context(), &types.QueryLeasesByTenantRequest{
				Tenant:     args[0],
				Pagination: pageReq,
				ActiveOnly: activeOnly,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	cmd.Flags().Bool("active-only", false, "Filter to only active leases")
	flags.AddQueryFlagsToCmd(cmd)
	flags.AddPaginationFlagsToCmd(cmd, "leases")

	return cmd
}

// GetLeasesByProviderCmd returns the command to query leases by provider.
func GetLeasesByProviderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "leases-by-provider [provider-id]",
		Short:   "Query leases by provider ID",
		Example: `leases-by-provider 1 --active-only`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			providerID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid provider_id: %w", err)
			}

			activeOnly, _ := cmd.Flags().GetBool("active-only")
			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.LeasesByProvider(cmd.Context(), &types.QueryLeasesByProviderRequest{
				ProviderId: providerID,
				Pagination: pageReq,
				ActiveOnly: activeOnly,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	cmd.Flags().Bool("active-only", false, "Filter to only active leases")
	flags.AddQueryFlagsToCmd(cmd)
	flags.AddPaginationFlagsToCmd(cmd, "leases")

	return cmd
}

// GetCreditAccountCmd returns the command to query a credit account.
func GetCreditAccountCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "credit-account [tenant]",
		Short:   "Query a tenant's credit account",
		Example: `credit-account manifest1abc...`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.CreditAccount(cmd.Context(), &types.QueryCreditAccountRequest{
				Tenant: args[0],
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

// GetCreditAddressCmd returns the command to derive a credit address.
func GetCreditAddressCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "credit-address [tenant]",
		Short:   "Derive the credit address for a tenant",
		Example: `credit-address manifest1abc...`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.CreditAddress(cmd.Context(), &types.QueryCreditAddressRequest{
				Tenant: args[0],
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

// GetWithdrawableAmountCmd returns the command to query withdrawable amount for a lease.
func GetWithdrawableAmountCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "withdrawable [lease-id]",
		Short:   "Query the withdrawable amount for a lease",
		Example: `withdrawable 1`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			leaseID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid lease_id: %w", err)
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.WithdrawableAmount(cmd.Context(), &types.QueryWithdrawableAmountRequest{
				LeaseId: leaseID,
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

// GetProviderWithdrawableCmd returns the command to query total withdrawable for a provider.
func GetProviderWithdrawableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "provider-withdrawable [provider-id]",
		Short:   "Query the total withdrawable amount for a provider across all leases",
		Example: `provider-withdrawable 1`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			providerID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid provider_id: %w", err)
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.ProviderWithdrawable(cmd.Context(), &types.QueryProviderWithdrawableRequest{
				ProviderId: providerID,
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
