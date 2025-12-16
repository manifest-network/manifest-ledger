package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// NewTxCmd returns the transaction commands for the billing module.
func NewTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Billing transaction subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		NewFundCreditCmd(),
		NewCreateLeaseCmd(),
		NewCreateLeaseForTenantCmd(),
		NewCloseLeaseCmd(),
		NewWithdrawCmd(),
		NewWithdrawAllCmd(),
		NewUpdateParamsCmd(),
	)

	return cmd
}

// NewFundCreditCmd returns the command to fund a credit account.
func NewFundCreditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fund-credit [tenant] [amount]",
		Short: "Fund a tenant's credit account",
		Long:  `Fund a tenant's credit account with the specified amount. The credit account can hold any token denomination.`,
		Example: `fund-credit manifest1abc... 1000000factory/manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj/upwr
fund-credit manifest1abc... 5000000umfx --from mykey`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			tenant := args[0]
			if _, err := sdk.AccAddressFromBech32(tenant); err != nil {
				return fmt.Errorf("invalid tenant address: %w", err)
			}

			amount, err := sdk.ParseCoinNormalized(args[1])
			if err != nil {
				return fmt.Errorf("invalid amount: %w", err)
			}

			msg := &types.MsgFundCredit{
				Sender: clientCtx.GetFromAddress().String(),
				Tenant: tenant,
				Amount: amount,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// NewCreateLeaseCmd returns the command to create a lease.
func NewCreateLeaseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-lease [sku-id:quantity] [sku-id:quantity] ...",
		Short: "Create a new lease with the specified SKUs",
		Long: `Create a new lease with one or more SKU items. Each item is specified as sku_id:quantity.
All SKUs must belong to the same provider.`,
		Example: `create-lease 1:2 2:1
create-lease 5:10 --from mykey`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			items := make([]types.LeaseItemInput, 0, len(args))
			for _, arg := range args {
				var skuID, quantity uint64
				_, err := fmt.Sscanf(arg, "%d:%d", &skuID, &quantity)
				if err != nil {
					return fmt.Errorf("invalid item format '%s': expected sku_id:quantity (e.g., 1:2)", arg)
				}
				items = append(items, types.LeaseItemInput{
					SkuId:    skuID,
					Quantity: quantity,
				})
			}

			msg := &types.MsgCreateLease{
				Tenant: clientCtx.GetFromAddress().String(),
				Items:  items,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// NewCreateLeaseForTenantCmd returns the command to create a lease on behalf of a tenant.
func NewCreateLeaseForTenantCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-lease-for-tenant [tenant] [sku-id:quantity] [sku-id:quantity] ...",
		Short: "Create a new lease on behalf of a tenant (authority only)",
		Long: `Create a new lease on behalf of a tenant. This command is used by the authority
to migrate off-chain leases to on-chain. Each item is specified as sku_id:quantity.
All SKUs must belong to the same provider. The tenant's credit account must be pre-funded.`,
		Example: `create-lease-for-tenant manifest1abc... 1:2 2:1 --from authority
create-lease-for-tenant manifest1xyz... 5:10 --from authority`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			tenant := args[0]
			if _, err := sdk.AccAddressFromBech32(tenant); err != nil {
				return fmt.Errorf("invalid tenant address: %w", err)
			}

			items := make([]types.LeaseItemInput, 0, len(args)-1)
			for _, arg := range args[1:] {
				var skuID, quantity uint64
				_, err := fmt.Sscanf(arg, "%d:%d", &skuID, &quantity)
				if err != nil {
					return fmt.Errorf("invalid item format '%s': expected sku_id:quantity (e.g., 1:2)", arg)
				}
				items = append(items, types.LeaseItemInput{
					SkuId:    skuID,
					Quantity: quantity,
				})
			}

			msg := &types.MsgCreateLeaseForTenant{
				Authority: clientCtx.GetFromAddress().String(),
				Tenant:    tenant,
				Items:     items,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// NewCloseLeaseCmd returns the command to close a lease.
func NewCloseLeaseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "close-lease [lease-id]",
		Short:   "Close an active lease",
		Long:    `Close an active lease. The sender must be the tenant, the provider, or the module authority.`,
		Example: `close-lease 1 --from mykey`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			leaseID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid lease_id: %w", err)
			}

			msg := &types.MsgCloseLease{
				Sender:  clientCtx.GetFromAddress().String(),
				LeaseId: leaseID,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// NewWithdrawCmd returns the command to withdraw from a lease.
func NewWithdrawCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "withdraw [lease-id]",
		Short:   "Withdraw accrued funds from a lease",
		Long:    `Withdraw accrued funds from a specific lease. Only the provider or authority can withdraw.`,
		Example: `withdraw 1 --from provider-key`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			leaseID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid lease_id: %w", err)
			}

			msg := &types.MsgWithdraw{
				Sender:  clientCtx.GetFromAddress().String(),
				LeaseId: leaseID,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// NewWithdrawAllCmd returns the command to withdraw from all leases.
func NewWithdrawAllCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "withdraw-all [provider-id]",
		Short: "Withdraw all accrued funds from all leases for a provider",
		Long: fmt.Sprintf(`Withdraw all accrued funds from all leases belonging to a provider.
If the sender is the provider's address, provider-id can be 0 or omitted.
If the sender is the authority, provider-id must be specified.

Use --limit to process leases in batches. Default limit is %d leases per call.
Maximum allowed limit is %d to prevent gas exhaustion.
When has_more is true in the response, call withdraw-all again to process remaining leases.`,
			types.DefaultWithdrawAllLimit, types.MaxWithdrawAllLimit),
		Example: `withdraw-all 1 --from provider-key
withdraw-all 1 --limit 50 --from provider-key`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			var providerID uint64
			if len(args) > 0 {
				providerID, err = strconv.ParseUint(args[0], 10, 64)
				if err != nil {
					return fmt.Errorf("invalid provider_id: %w", err)
				}
			}

			limit, err := cmd.Flags().GetUint64("limit")
			if err != nil {
				return err
			}

			msg := &types.MsgWithdrawAll{
				Sender:     clientCtx.GetFromAddress().String(),
				ProviderId: providerID,
				Limit:      limit,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().Uint64("limit", 0, fmt.Sprintf("Maximum number of leases to process (0 = default %d, max %d)", types.DefaultWithdrawAllLimit, types.MaxWithdrawAllLimit))
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// NewUpdateParamsCmd returns the command to update billing module parameters.
func NewUpdateParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-params [max-leases-per-tenant] [max-items-per-lease] [min-lease-duration]",
		Short: "Update billing module parameters (authority only)",
		Long: `Update the billing module parameters. Only the module authority can execute this command.
All parameters must be provided. Use --allowed-list to set addresses allowed to create leases for tenants.
min-lease-duration is in seconds (e.g., 3600 for 1 hour).`,
		Example: `update-params 100 20 3600 --from authority
update-params 100 20 3600 --allowed-list manifest1abc...,manifest1xyz... --from authority`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			maxLeasesPerTenant, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid max_leases_per_tenant: %w", err)
			}

			maxItemsPerLease, err := strconv.ParseUint(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid max_items_per_lease: %w", err)
			}

			minLeaseDuration, err := strconv.ParseUint(args[2], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid min_lease_duration: %w", err)
			}

			allowedListStr, _ := cmd.Flags().GetString("allowed-list")
			var allowedList []string
			if allowedListStr != "" {
				allowedList = splitAndTrim(allowedListStr)
			}

			msg := &types.MsgUpdateParams{
				Authority: clientCtx.GetFromAddress().String(),
				Params: types.Params{
					MaxLeasesPerTenant: maxLeasesPerTenant,
					AllowedList:        allowedList,
					MaxItemsPerLease:   maxItemsPerLease,
					MinLeaseDuration:   minLeaseDuration,
				},
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String("allowed-list", "", "Comma-separated list of addresses allowed to create leases for tenants")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// splitAndTrim splits a comma-separated string and trims whitespace from each element.
func splitAndTrim(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
