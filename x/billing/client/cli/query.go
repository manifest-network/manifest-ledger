package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"

	pkguuid "github.com/manifest-network/manifest-ledger/pkg/uuid"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// parseLeaseState parses a string into a LeaseState enum value.
// An empty string returns LEASE_STATE_UNSPECIFIED (no filter).
// A non-empty string that doesn't match a known state returns an error.
func parseLeaseState(s string) (types.LeaseState, error) {
	switch strings.ToLower(s) {
	case "":
		return types.LEASE_STATE_UNSPECIFIED, nil
	case "pending":
		return types.LEASE_STATE_PENDING, nil
	case "active":
		return types.LEASE_STATE_ACTIVE, nil
	case "closed":
		return types.LEASE_STATE_CLOSED, nil
	case "rejected":
		return types.LEASE_STATE_REJECTED, nil
	case "expired":
		return types.LEASE_STATE_EXPIRED, nil
	default:
		return types.LEASE_STATE_UNSPECIFIED, fmt.Errorf("unknown lease state %q: valid values are pending, active, closed, rejected, expired", s)
	}
}

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
		GetLeasesBySKUCmd(),
		GetCreditAccountCmd(),
		GetCreditAccountsCmd(),
		GetCreditAddressCmd(),
		GetCreditEstimateCmd(),
		GetWithdrawableAmountCmd(),
		GetProviderWithdrawableCmd(),
		GetLeaseByCustomDomainCmd(),
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

// GetLeaseCmd returns the command to query a lease by UUID.
func GetLeaseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "lease [lease-uuid]",
		Short:   "Query a lease by UUID",
		Example: `lease 01902a9b-1234-7000-8000-000000000001`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			leaseUUID := args[0]
			if !pkguuid.IsValidUUID(leaseUUID) {
				return fmt.Errorf("invalid lease_uuid format: %s", leaseUUID)
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.Lease(cmd.Context(), &types.QueryLeaseRequest{LeaseUuid: leaseUUID})
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
		Example: `leases --state pending --limit 10`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			stateStr, err := cmd.Flags().GetString("state")
			if err != nil {
				return err
			}
			stateFilter, err := parseLeaseState(stateStr)
			if err != nil {
				return err
			}

			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.Leases(cmd.Context(), &types.QueryLeasesRequest{
				Pagination:  pageReq,
				StateFilter: stateFilter,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	cmd.Flags().String("state", "", "Filter by lease state (pending, active, closed, rejected, expired)")
	flags.AddQueryFlagsToCmd(cmd)
	flags.AddPaginationFlagsToCmd(cmd, "leases")

	return cmd
}

// GetLeasesByTenantCmd returns the command to query leases by tenant.
func GetLeasesByTenantCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "leases-by-tenant [tenant]",
		Short:   "Query leases by tenant address",
		Example: `leases-by-tenant manifest1abc... --state active`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			stateStr, err := cmd.Flags().GetString("state")
			if err != nil {
				return err
			}
			stateFilter, err := parseLeaseState(stateStr)
			if err != nil {
				return err
			}

			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.LeasesByTenant(cmd.Context(), &types.QueryLeasesByTenantRequest{
				Tenant:      args[0],
				Pagination:  pageReq,
				StateFilter: stateFilter,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	cmd.Flags().String("state", "", "Filter by lease state (pending, active, closed, rejected, expired)")
	flags.AddQueryFlagsToCmd(cmd)
	flags.AddPaginationFlagsToCmd(cmd, "leases")

	return cmd
}

// GetLeasesByProviderCmd returns the command to query leases by provider.
func GetLeasesByProviderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "leases-by-provider [provider-uuid]",
		Short:   "Query leases by provider UUID",
		Example: `leases-by-provider 01902a9b-1234-7000-8000-000000000001 --state pending`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			providerUUID := args[0]
			if !pkguuid.IsValidUUID(providerUUID) {
				return fmt.Errorf("invalid provider_uuid format: %s", providerUUID)
			}

			stateStr, err := cmd.Flags().GetString("state")
			if err != nil {
				return err
			}
			stateFilter, err := parseLeaseState(stateStr)
			if err != nil {
				return err
			}

			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.LeasesByProvider(cmd.Context(), &types.QueryLeasesByProviderRequest{
				ProviderUuid: providerUUID,
				Pagination:   pageReq,
				StateFilter:  stateFilter,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	cmd.Flags().String("state", "", "Filter by lease state (pending, active, closed, rejected, expired)")
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
		Use:     "withdrawable [lease-uuid]",
		Short:   "Query the withdrawable amount for a lease",
		Example: `withdrawable 01902a9b-1234-7000-8000-000000000001`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			leaseUUID := args[0]
			if !pkguuid.IsValidUUID(leaseUUID) {
				return fmt.Errorf("invalid lease_uuid format: %s", leaseUUID)
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.WithdrawableAmount(cmd.Context(), &types.QueryWithdrawableAmountRequest{
				LeaseUuid: leaseUUID,
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
		Use:   "provider-withdrawable [provider-uuid]",
		Short: "Query the total withdrawable amount for a provider across all leases",
		Long: `Query the total withdrawable amount for a provider across all leases.

This query uses pagination to prevent timeouts for providers with many leases.
Use --limit to control how many leases to process (default: 100, max: 1000).
Check the has_more field in the response to see if more leases exist.`,
		Example: `provider-withdrawable 01902a9b-1234-7000-8000-000000000001
provider-withdrawable 01902a9b-1234-7000-8000-000000000001 --limit 500`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			providerUUID := args[0]
			if !pkguuid.IsValidUUID(providerUUID) {
				return fmt.Errorf("invalid provider_uuid format: %s", providerUUID)
			}

			limit, err := cmd.Flags().GetUint64("limit")
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.ProviderWithdrawable(cmd.Context(), &types.QueryProviderWithdrawableRequest{
				ProviderUuid: providerUUID,
				Limit:        limit,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	cmd.Flags().Uint64("limit", 0, "Maximum leases to process; 0 means server default of 100 (max: 1000)")
	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

// GetCreditAccountsCmd returns the command to query all credit accounts.
func GetCreditAccountsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "credit-accounts",
		Short:   "Query all credit accounts with pagination",
		Example: `credit-accounts --limit 10`,
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

			res, err := queryClient.CreditAccounts(cmd.Context(), &types.QueryCreditAccountsRequest{
				Pagination: pageReq,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	flags.AddQueryFlagsToCmd(cmd)
	flags.AddPaginationFlagsToCmd(cmd, "credit-accounts")

	return cmd
}

// GetLeasesBySKUCmd returns the command to query leases by SKU UUID.
func GetLeasesBySKUCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "leases-by-sku [sku-uuid]",
		Short: "Query leases by SKU UUID",
		Long: `Query leases that contain a specific SKU.

Uses the LeasesBySKU index for efficient lookup.
Use the --state filter to narrow results.
Use pagination flags (--limit, --page-key) to page through results.`,
		Example: `leases-by-sku 01902a9b-1234-7000-8000-000000000001 --state active`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			skuUUID := args[0]
			if !pkguuid.IsValidUUID(skuUUID) {
				return fmt.Errorf("invalid sku_uuid format: %s", skuUUID)
			}

			stateStr, err := cmd.Flags().GetString("state")
			if err != nil {
				return err
			}
			stateFilter, err := parseLeaseState(stateStr)
			if err != nil {
				return err
			}

			pageReq, err := client.ReadPageRequest(cmd.Flags())
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.LeasesBySKU(cmd.Context(), &types.QueryLeasesBySKURequest{
				SkuUuid:     skuUUID,
				Pagination:  pageReq,
				StateFilter: stateFilter,
			})
			if err != nil {
				return err
			}

			return clientCtx.PrintProto(res)
		},
	}

	cmd.Flags().String("state", "", "Filter by lease state (pending, active, closed, rejected, expired)")
	flags.AddQueryFlagsToCmd(cmd)
	flags.AddPaginationFlagsToCmd(cmd, "leases")

	return cmd
}

// GetCreditEstimateCmd returns the command to estimate remaining lease duration.
func GetCreditEstimateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "credit-estimate [tenant]",
		Short: "Estimate remaining lease duration for a tenant",
		Long: `Estimate how long a tenant's credit balance will last based on current active leases.

Returns:
  - Current credit balance
  - Total burn rate per second across all active leases
  - Estimated duration until credit exhaustion
  - Number of active leases`,
		Example: `credit-estimate manifest1abc...`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.CreditEstimate(cmd.Context(), &types.QueryCreditEstimateRequest{
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

// GetLeaseByCustomDomainCmd returns the command to look up a lease by its custom_domain.
func GetLeaseByCustomDomainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "lease-by-domain [domain]",
		Short:   "Query the lease that has claimed a given custom_domain",
		Example: `lease-by-domain app.example.com`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			queryClient := types.NewQueryClient(clientCtx)
			res, err := queryClient.LeaseByCustomDomain(cmd.Context(), &types.QueryLeaseByCustomDomainRequest{
				CustomDomain: args[0],
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
