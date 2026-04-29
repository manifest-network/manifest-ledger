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

	pkguuid "github.com/manifest-network/manifest-ledger/pkg/uuid"
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
		NewAcknowledgeLeaseCmd(),
		NewRejectLeaseCmd(),
		NewCancelLeaseCmd(),
		NewCloseLeaseCmd(),
		NewWithdrawCmd(),
		NewUpdateParamsCmd(),
		NewSetLeaseItemCustomDomainCmd(),
	)

	return cmd
}

// parseMetaHashFlag parses and validates the --meta-hash flag value.
// Returns nil if the flag is empty. Returns an error if the value is not valid hex
// or exceeds the maximum allowed length.
func parseMetaHashFlag(cmd *cobra.Command) ([]byte, error) {
	metaHashStr, _ := cmd.Flags().GetString("meta-hash")
	if metaHashStr == "" {
		return nil, nil
	}
	// Defense-in-depth: check hex string length before decoding
	if len(metaHashStr) > types.MaxMetaHashLength*2 {
		return nil, fmt.Errorf("meta-hash too long: max %d hex characters", types.MaxMetaHashLength*2)
	}
	metaHash, err := hex.DecodeString(metaHashStr)
	if err != nil {
		return nil, fmt.Errorf("invalid meta-hash: must be hex-encoded: %w", err)
	}
	if len(metaHash) > types.MaxMetaHashLength {
		return nil, fmt.Errorf("meta-hash exceeds maximum length of %d bytes", types.MaxMetaHashLength)
	}
	return metaHash, nil
}

// parseLeaseItemInputs parses CLI arguments into LeaseItemInput values.
// Format: sku_uuid:quantity or sku_uuid:quantity:service_name
func parseLeaseItemInputs(args []string) ([]types.LeaseItemInput, error) {
	items := make([]types.LeaseItemInput, 0, len(args))
	for _, arg := range args {
		parts := strings.SplitN(arg, ":", 3)
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid item format '%s': expected sku_uuid:quantity[:service_name]", arg)
		}
		skuUUID := parts[0]
		if !pkguuid.IsValidUUID(skuUUID) {
			return nil, fmt.Errorf("invalid sku_uuid format: %s", skuUUID)
		}
		quantity, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid quantity in '%s': %w", arg, err)
		}
		item := types.LeaseItemInput{
			SkuUuid:  skuUUID,
			Quantity: quantity,
		}
		if len(parts) == 3 {
			if parts[2] == "" {
				return nil, fmt.Errorf("invalid item format '%s': service_name cannot be empty (omit the trailing colon for legacy mode)", arg)
			}
			item.ServiceName = parts[2]
		}
		items = append(items, item)
	}
	return items, nil
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
		Use:   "create-lease [sku-uuid:quantity[:service_name]] ...",
		Short: "Create a new lease with the specified SKUs",
		Long: `Create a new lease with one or more SKU items. Each item is specified as sku_uuid:quantity
or sku_uuid:quantity:service_name for stack deployments. When service_name is used, all items
must have one and the same SKU may appear multiple times with different service names.
All SKUs must belong to the same provider.
Use --meta-hash to include a hash/reference to off-chain deployment data (hex-encoded, max 64 bytes).`,
		Example: `create-lease 01902a9b-1234-7000-8000-000000000001:2 01902a9b-1234-7000-8000-000000000002:1
create-lease 01902a9b-1234-7000-8000-000000000005:10 --from mykey
create-lease 01902a9b-1234-7000-8000-000000000001:1:web 01902a9b-1234-7000-8000-000000000001:1:db --from mykey
create-lease 01902a9b-1234-7000-8000-000000000001:1 --meta-hash a1b2c3d4e5f6... --from mykey`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			items, err := parseLeaseItemInputs(args)
			if err != nil {
				return err
			}

			// Parse optional meta_hash
			metaHash, err := parseMetaHashFlag(cmd)
			if err != nil {
				return err
			}

			msg := &types.MsgCreateLease{
				Tenant:   clientCtx.GetFromAddress().String(),
				Items:    items,
				MetaHash: metaHash,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String("meta-hash", "", "Hex-encoded hash/reference to off-chain deployment data (max 64 bytes)")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// NewCreateLeaseForTenantCmd returns the command to create a lease on behalf of a tenant.
func NewCreateLeaseForTenantCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-lease-for-tenant [tenant] [sku-uuid:quantity[:service_name]] ...",
		Short: "Create a new lease on behalf of a tenant (authority only)",
		Long: `Create a new lease on behalf of a tenant. This command is used by the authority
to migrate off-chain leases to on-chain. Each item is specified as sku_uuid:quantity
or sku_uuid:quantity:service_name for stack deployments. When service_name is used, all items
must have one and the same SKU may appear multiple times with different service names.
All SKUs must belong to the same provider. The tenant's credit account must be pre-funded.
Use --meta-hash to include a hash/reference to off-chain deployment data (hex-encoded, max 64 bytes).`,
		Example: `create-lease-for-tenant manifest1abc... 01902a9b-1234-7000-8000-000000000001:2 01902a9b-1234-7000-8000-000000000002:1 --from authority
create-lease-for-tenant manifest1xyz... 01902a9b-1234-7000-8000-000000000005:10 --from authority
create-lease-for-tenant manifest1abc... 01902a9b-1234-7000-8000-000000000001:1:web 01902a9b-1234-7000-8000-000000000001:1:db --from authority
create-lease-for-tenant manifest1abc... 01902a9b-1234-7000-8000-000000000001:1 --meta-hash a1b2c3d4e5f6... --from authority`,
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

			items, err := parseLeaseItemInputs(args[1:])
			if err != nil {
				return err
			}

			// Parse optional meta_hash
			metaHash, err := parseMetaHashFlag(cmd)
			if err != nil {
				return err
			}

			msg := &types.MsgCreateLeaseForTenant{
				Authority: clientCtx.GetFromAddress().String(),
				Tenant:    tenant,
				Items:     items,
				MetaHash:  metaHash,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String("meta-hash", "", "Hex-encoded hash/reference to off-chain deployment data (max 64 bytes)")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// NewCloseLeaseCmd returns the command to close one or more leases.
func NewCloseLeaseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "close-lease [lease-uuid]...",
		Short: "Close one or more active leases",
		Long: `Close one or more active leases atomically.
The sender must be authorized for each lease (tenant, provider, or authority).
All leases must be in ACTIVE state.
Use --reason to provide a closure reason (applied to all leases).
This is an atomic operation: all leases succeed or all fail.`,
		Example: `# Close a single lease
close-lease 01902a9b-1234-7000-8000-000000000001 --from mykey

# Close multiple leases with reason (max 100)
close-lease 01902a9b-1234-7000-8000-000000000001 01902a9b-1234-7000-8000-000000000002 --reason "service no longer needed" --from mykey`,
		Args: cobra.RangeArgs(1, types.MaxBatchLeaseSize),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			// Validate all UUIDs
			for _, uuid := range args {
				if !pkguuid.IsValidUUID(uuid) {
					return fmt.Errorf("invalid lease_uuid format: %s", uuid)
				}
			}

			reason, _ := cmd.Flags().GetString("reason")

			msg := &types.MsgCloseLease{
				Sender:     clientCtx.GetFromAddress().String(),
				LeaseUuids: args,
				Reason:     reason,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String("reason", "", "Reason for closing the leases (max 256 characters, applied to all)")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// NewWithdrawCmd returns the command to withdraw from leases.
func NewWithdrawCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "withdraw [lease-uuid]...",
		Short: "Withdraw accrued funds from leases",
		Long: fmt.Sprintf(`Withdraw accrued funds from leases. Only the provider or authority can withdraw.

Two modes are supported:
1. Specific leases: provide lease UUIDs as arguments (1-%d leases)
2. Provider-wide: use --provider flag for paginated withdrawal from all provider's leases

In specific lease mode, all leases must belong to the same provider and
withdrawals are processed atomically - either all succeed or all fail.

In provider-wide mode, leases are processed with pagination. Use --limit to
control batch size (default %d, max %d). When has_more is true in the response,
call withdraw again to process remaining leases.`, types.MaxBatchLeaseSize,
			types.DefaultProviderWithdrawLimit, types.MaxBatchLeaseSize),
		Example: `# Withdraw from specific leases
withdraw 01902a9b-1234-7000-8000-000000000001 --from provider-key
withdraw 01902a9b-1234-7000-8000-000000000001 01902a9b-1234-7000-8000-000000000002 --from provider-key

# Withdraw from all provider's leases (paginated)
withdraw --provider 01902a9b-1234-7000-8000-000000000001 --from provider-key
withdraw --provider 01902a9b-1234-7000-8000-000000000001 --limit 100 --from provider-key`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			providerUUID, err := cmd.Flags().GetString("provider")
			if err != nil {
				return err
			}

			limit, err := cmd.Flags().GetUint64("limit")
			if err != nil {
				return err
			}

			// Validate mutually exclusive modes
			if len(args) > 0 && providerUUID != "" {
				return fmt.Errorf("cannot specify both lease UUIDs and --provider flag")
			}
			if len(args) == 0 && providerUUID == "" {
				return fmt.Errorf("must specify lease UUIDs or --provider flag")
			}

			// Mode 1: Specific leases
			if len(args) > 0 {
				if len(args) > types.MaxBatchLeaseSize {
					return fmt.Errorf("cannot withdraw from more than %d leases at once", types.MaxBatchLeaseSize)
				}
				for _, leaseUUID := range args {
					if !pkguuid.IsValidUUID(leaseUUID) {
						return fmt.Errorf("invalid lease_uuid format: %s", leaseUUID)
					}
				}

				msg := &types.MsgWithdraw{
					Sender:     clientCtx.GetFromAddress().String(),
					LeaseUuids: args,
				}
				return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
			}

			// Mode 2: Provider-wide
			if !pkguuid.IsValidUUID(providerUUID) {
				return fmt.Errorf("invalid provider_uuid format: %s", providerUUID)
			}

			msg := &types.MsgWithdraw{
				Sender:       clientCtx.GetFromAddress().String(),
				ProviderUuid: providerUUID,
				Limit:        limit,
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String("provider", "", "Provider UUID for paginated withdrawal from all leases")
	cmd.Flags().Uint64("limit", 0, fmt.Sprintf("Maximum leases to process in provider mode (0 = default %d, max %d)", types.DefaultProviderWithdrawLimit, types.MaxBatchLeaseSize))
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// NewUpdateParamsCmd returns the command to update billing module parameters.
func NewUpdateParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-params [max-leases-per-tenant] [max-items-per-lease] [min-lease-duration] [max-pending-leases-per-tenant] [pending-timeout]",
		Short: "Update billing module parameters (authority only)",
		Long: `Update the billing module parameters. Only the module authority can execute this command.
All numeric parameters must be provided.

--allowed-list and --reserved-domain-suffixes are PRESERVE-on-omit: when the flag
is not provided on the command line, the current on-chain value is queried and
re-submitted unchanged so existing operator workflows (e.g. bumping a numeric
param) cannot accidentally wipe seeded entries. Pass the flag with an empty
value (e.g. --reserved-domain-suffixes="") to explicitly clear the list.

min-lease-duration is in seconds (e.g., 3600 for 1 hour).
pending-timeout is the duration in seconds that a lease can remain in PENDING state (60-86400).`,
		Example: `# Update only numeric params (allowed_list and reserved_domain_suffixes preserved):
update-params 100 20 3600 10 1800 --from authority

# Update numeric params and overwrite allowed_list:
update-params 100 20 3600 10 1800 --allowed-list manifest1abc...,manifest1xyz... --from authority

# Explicitly clear reserved_domain_suffixes:
update-params 100 20 3600 10 1800 --reserved-domain-suffixes="" --from authority`,
		Args: cobra.ExactArgs(5),
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

			maxPendingLeasesPerTenant, err := strconv.ParseUint(args[3], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid max_pending_leases_per_tenant: %w", err)
			}

			pendingTimeout, err := strconv.ParseUint(args[4], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid pending_timeout: %w", err)
			}

			// MsgUpdateParams is replace-style: omitted list flags would silently
			// wipe seeded values. Distinguish "flag absent" (preserve current) from
			// "flag present with empty value" (explicit clear) via Flags.Changed().
			// On absent, query the current on-chain value to round-trip.
			var (
				allowedList      []string
				reservedSuffixes []string
				preservedParams  *types.Params
			)
			needsPreservedParams := !cmd.Flags().Changed("allowed-list") || !cmd.Flags().Changed("reserved-domain-suffixes")
			if needsPreservedParams {
				queryClient := types.NewQueryClient(clientCtx)
				res, err := queryClient.Params(cmd.Context(), &types.QueryParamsRequest{})
				if err != nil {
					return fmt.Errorf("query current params to preserve omitted list flags: %w", err)
				}
				preservedParams = &res.Params
			}

			if cmd.Flags().Changed("allowed-list") {
				allowedListStr, _ := cmd.Flags().GetString("allowed-list")
				if allowedListStr != "" {
					allowedList = splitAndTrim(allowedListStr)
				}
			} else {
				allowedList = preservedParams.AllowedList
			}

			if cmd.Flags().Changed("reserved-domain-suffixes") {
				reservedSuffixesStr, _ := cmd.Flags().GetString("reserved-domain-suffixes")
				if reservedSuffixesStr != "" {
					reservedSuffixes = splitAndTrim(reservedSuffixesStr)
				}
			} else {
				reservedSuffixes = preservedParams.ReservedDomainSuffixes
			}

			msg := &types.MsgUpdateParams{
				Authority: clientCtx.GetFromAddress().String(),
				Params: types.Params{
					MaxLeasesPerTenant:        maxLeasesPerTenant,
					AllowedList:               allowedList,
					MaxItemsPerLease:          maxItemsPerLease,
					MinLeaseDuration:          minLeaseDuration,
					MaxPendingLeasesPerTenant: maxPendingLeasesPerTenant,
					PendingTimeout:            pendingTimeout,
					ReservedDomainSuffixes:    reservedSuffixes,
				},
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String("allowed-list", "", "Comma-separated list of addresses allowed to create leases for tenants. Omit to preserve current; pass empty string to clear.")
	cmd.Flags().String("reserved-domain-suffixes", "", "Comma-separated list of reserved domain suffixes (each must begin with '.'). Omit to preserve current; pass empty string to clear.")
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

// NewAcknowledgeLeaseCmd returns the command to acknowledge one or more pending leases.
func NewAcknowledgeLeaseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "acknowledge-lease [lease-uuid]...",
		Short: "Acknowledge one or more pending leases (provider only)",
		Long: `Acknowledge one or more pending leases to transition them to active state.
Only the provider of the leases or the module authority can acknowledge.
All leases must belong to the same provider and be in PENDING state.
This is an atomic operation: all leases succeed or all fail.
Billing starts from the acknowledgement time.`,
		Example: `# Acknowledge a single lease
acknowledge-lease 01902a9b-1234-7000-8000-000000000001 --from provider-key

# Acknowledge multiple leases (max 100)
acknowledge-lease 01902a9b-1234-7000-8000-000000000001 01902a9b-1234-7000-8000-000000000002 --from provider-key`,
		Args: cobra.RangeArgs(1, types.MaxBatchLeaseSize),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			// Validate all UUIDs
			for _, uuid := range args {
				if !pkguuid.IsValidUUID(uuid) {
					return fmt.Errorf("invalid lease_uuid format: %s", uuid)
				}
			}

			msg := &types.MsgAcknowledgeLease{
				Sender:     clientCtx.GetFromAddress().String(),
				LeaseUuids: args,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// NewRejectLeaseCmd returns the command to reject one or more pending leases.
func NewRejectLeaseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reject-lease [lease-uuid]...",
		Short: "Reject one or more pending leases (provider only)",
		Long: `Reject one or more pending leases atomically. This will transition the leases
to rejected state and release the tenants' locked credit.
Only the provider of the leases or the module authority can reject.
All leases must belong to the same provider and be in PENDING state.
Use --reason to provide a rejection reason (applied to all leases).
This is an atomic operation: all leases succeed or all fail.`,
		Example: `# Reject a single lease
reject-lease 01902a9b-1234-7000-8000-000000000001 --from provider-key

# Reject multiple leases with reason (max 100)
reject-lease 01902a9b-1234-7000-8000-000000000001 01902a9b-1234-7000-8000-000000000002 --reason "insufficient resources" --from provider-key`,
		Args: cobra.RangeArgs(1, types.MaxBatchLeaseSize),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			// Validate all UUIDs
			for _, uuid := range args {
				if !pkguuid.IsValidUUID(uuid) {
					return fmt.Errorf("invalid lease_uuid format: %s", uuid)
				}
			}

			reason, _ := cmd.Flags().GetString("reason")

			msg := &types.MsgRejectLease{
				Sender:     clientCtx.GetFromAddress().String(),
				LeaseUuids: args,
				Reason:     reason,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	cmd.Flags().String("reason", "", "Reason for rejecting the leases (max 256 characters, applied to all)")
	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// NewCancelLeaseCmd returns the command to cancel one or more pending leases.
func NewCancelLeaseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cancel-lease [lease-uuid]...",
		Short: "Cancel one or more pending leases (tenant only)",
		Long: `Cancel one or more pending leases that you created. This will transition
the leases to rejected state and release your locked credit. Only the tenant
who created the leases can cancel them. All leases are cancelled atomically -
either all succeed or all fail.`,
		Example: `# Cancel a single lease
cancel-lease 01902a9b-1234-7000-8000-000000000001 --from tenant-key

# Cancel multiple leases in one transaction
cancel-lease 01902a9b-1234-7000-8000-000000000001 01902a9b-1234-7000-8000-000000000002 --from tenant-key`,
		Args: cobra.RangeArgs(1, types.MaxBatchLeaseSize),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			// Validate all UUIDs
			for _, leaseUUID := range args {
				if !pkguuid.IsValidUUID(leaseUUID) {
					return fmt.Errorf("invalid lease_uuid format: %s", leaseUUID)
				}
			}

			msg := &types.MsgCancelLease{
				Tenant:     clientCtx.GetFromAddress().String(),
				LeaseUuids: args,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

// NewSetLeaseItemCustomDomainCmd returns the set-item-custom-domain command.
func NewSetLeaseItemCustomDomainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-item-custom-domain [lease-uuid] [service-name] [domain]",
		Short: "Set or clear the custom_domain on a specific lease item",
		Long: `Set or clear the custom_domain on a specific LeaseItem identified by
service_name. For a 1-item legacy lease (item.service_name unset), pass "" for
[service-name]. Multi-item legacy leases cannot use custom_domain — recreate the
lease in service-name mode. Pass "" for [domain] to clear the field. Authorised
senders are the lease tenant, the module authority, or any address in
params.allowed_list.`,
		Example: `# 1-item lease, item has no service_name
set-item-custom-domain 01902a9b-1234-7000-8000-000000000001 "" app.example.com --from tenant-key

# multi-item lease, target the "web" item
set-item-custom-domain 01902a9b-1234-7000-8000-000000000001 web app.example.com --from tenant-key

# clear the domain on the "web" item
set-item-custom-domain 01902a9b-1234-7000-8000-000000000001 web "" --from tenant-key`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			leaseUUID := args[0]
			if !pkguuid.IsValidUUID(leaseUUID) {
				return fmt.Errorf("invalid lease_uuid format: %s", leaseUUID)
			}

			msg := &types.MsgSetLeaseItemCustomDomain{
				Sender:       clientCtx.GetFromAddress().String(),
				LeaseUuid:    leaseUUID,
				ServiceName:  args[1],
				CustomDomain: args[2],
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
