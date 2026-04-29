package keeper

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// HasAdminPrivileges reports whether sender holds module-wide administrative
// rights — i.e. is the module authority or appears in params.AllowedList.
// Returns ("", nil) when the sender has no admin privileges.
func (k *Keeper) HasAdminPrivileges(ctx context.Context, sender string) (string, error) {
	if sender == k.GetAuthority() {
		return types.AttributeValueRoleAuthority, nil
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return "", err
	}
	if params.IsAllowed(sender) {
		return types.AttributeValueRoleAllowed, nil
	}
	return "", nil
}

// IsAuthorizedForTenant reports whether sender may act on behalf of a specific
// tenant: either by being the tenant, by being the module authority, or by
// appearing in params.AllowedList. The tenant argument is required (non-empty);
// callers who only need the admin check should call HasAdminPrivileges directly.
// Returns ("", nil) when the sender is not authorised.
func (k *Keeper) IsAuthorizedForTenant(ctx context.Context, sender, tenant string) (string, error) {
	if tenant == "" {
		return "", fmt.Errorf("IsAuthorizedForTenant requires non-empty tenant; use HasAdminPrivileges for tenant-agnostic checks")
	}
	if sender == tenant {
		return types.AttributeValueRoleTenant, nil
	}
	return k.HasAdminPrivileges(ctx, sender)
}

// GetLeaseByCustomDomain returns the lease and the service_name of the item
// that claims the given custom_domain. The third return is false (with nil
// error) when no item has claimed the domain.
//
// The input is normalised (lower-cased and trimmed) so callers don't need to
// remember to do it themselves — the index keys are always written in
// canonical form by SetItemCustomDomain, and a non-canonical query string
// would otherwise miss a real match.
func (k *Keeper) GetLeaseByCustomDomain(ctx context.Context, domain string) (types.Lease, string, bool, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	target, err := k.CustomDomainIndex.Get(ctx, domain)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.Lease{}, "", false, nil
		}
		return types.Lease{}, "", false, err
	}
	lease, err := k.GetLease(ctx, target.LeaseUuid)
	if err != nil {
		return types.Lease{}, "", false, err
	}
	return lease, target.ServiceName, true, nil
}

// findLeaseItemByServiceName returns the index of the unique LeaseItem whose
// ServiceName equals serviceName. Zero matches yields ErrLeaseItemNotFound;
// more than one yields ErrAmbiguousLeaseItem.
//
// Defensive guard: a multi-item lease in legacy mode (no item has a
// service_name, by the all-or-nothing rule on ValidateLeaseItems) cannot be
// addressed for custom_domain — every match key is empty, so msg.service_name
// can only be "" and would match all items. Reject at the lookup site rather
// than relying on construction-time invariants enforced elsewhere
// (ValidateLeaseItems, genesis defensive check, lease.Items immutability). The
// match-counting loop below would also produce >1 matches in this exact case,
// but the explicit guard documents the addressing contract here.
func findLeaseItemByServiceName(lease types.Lease, serviceName string) (int, error) {
	if len(lease.Items) > 1 && lease.Items[0].ServiceName == "" {
		return -1, types.ErrAmbiguousLeaseItem.Wrapf(
			"lease %s is in legacy mode with %d items; multi-item leases must be created in service-name mode to use custom_domain",
			lease.Uuid, len(lease.Items),
		)
	}
	idx := -1
	matches := 0
	for i := range lease.Items {
		if lease.Items[i].ServiceName == serviceName {
			matches++
			idx = i
		}
	}
	switch matches {
	case 0:
		return -1, types.ErrLeaseItemNotFound.Wrapf("lease %s has no item with service_name %q", lease.Uuid, serviceName)
	case 1:
		return idx, nil
	default:
		// Unreachable in practice: the defensive guard above catches multi-item
		// legacy mode (the only configuration that produces duplicate
		// service_names; service-mode validates uniqueness). Kept as a fail-safe
		// in case a future change relaxes the upstream invariants.
		return -1, types.ErrAmbiguousLeaseItem.Wrapf(
			"lease %s has %d items matching service_name %q",
			lease.Uuid, matches, serviceName,
		)
	}
}

// SetItemCustomDomain sets or clears the custom_domain on a specific
// LeaseItem identified by serviceName. An empty domain clears the field.
// Returns the role under which the call was authorised ("tenant" / "authority"
// / "allowed") so the msg server can include it in the emitted event.
//
// Index maintenance is delegated to SetLease — this method only validates
// input and writes the new field value. The pre-flight uniqueness check
// surfaces a friendly ErrCustomDomainAlreadyClaimed before mutation; SetLease's
// storage-level uniqueness enforcement is the defence-in-depth.
func (k *Keeper) SetItemCustomDomain(ctx context.Context, sender, leaseUUID, serviceName, domain string) (string, error) {
	lease, err := k.GetLease(ctx, leaseUUID)
	if err != nil {
		return "", err
	}

	role, err := k.IsAuthorizedForTenant(ctx, sender, lease.Tenant)
	if err != nil {
		return "", err
	}
	if role == "" {
		return "", types.ErrUnauthorized.Wrapf("sender %s is not authorised to edit custom_domain on lease %s", sender, leaseUUID)
	}

	if lease.State != types.LEASE_STATE_PENDING && lease.State != types.LEASE_STATE_ACTIVE {
		return "", types.ErrLeaseNotEditable.Wrapf("lease %s is in state %s", leaseUUID, lease.State)
	}

	itemIdx, err := findLeaseItemByServiceName(lease, serviceName)
	if err != nil {
		return "", err
	}
	// Use the resolved item's ServiceName (canonical) for the index value and
	// event attributes — same as msg.service_name in every case except the
	// 1-item legacy lookup (both are "").
	itemServiceName := lease.Items[itemIdx].ServiceName

	domain = strings.ToLower(strings.TrimSpace(domain))
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	if domain == "" {
		previous := lease.Items[itemIdx].CustomDomain
		if previous == "" {
			// Idempotent clear: nothing to update, nothing to emit. Caller still
			// receives the role so they can confirm authorisation independently
			// of event presence.
			return role, nil
		}
		lease.Items[itemIdx].CustomDomain = ""
		if err := k.SetLease(ctx, lease); err != nil {
			return "", err
		}
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeLeaseCustomDomainCleared,
				sdk.NewAttribute(types.AttributeKeyLeaseUUID, lease.Uuid),
				sdk.NewAttribute(types.AttributeKeyTenant, lease.Tenant),
				sdk.NewAttribute(types.AttributeKeyServiceName, itemServiceName),
				sdk.NewAttribute(types.AttributeKeyCustomDomain, previous),
				sdk.NewAttribute(types.AttributeKeySetBy, role),
			),
		)
		return role, nil
	}

	if err := types.IsValidFQDN(domain); err != nil {
		return "", err
	}

	params, err := k.GetParams(ctx)
	if err != nil {
		return "", err
	}
	if types.MatchesReservedSuffix(domain, params.ReservedDomainSuffixes) {
		return "", types.ErrInvalidCustomDomain.Wrapf("domain %q matches a reserved provider suffix", domain)
	}

	// Pre-flight uniqueness with three branches: idempotent re-set on the
	// same (lease, item), reject within-lease cross-item collision with a
	// helpful message, reject cross-lease collision with a helpful message.
	target, err := k.CustomDomainIndex.Get(ctx, domain)
	switch {
	case err == nil:
		switch {
		case target.LeaseUuid == leaseUUID && target.ServiceName == itemServiceName:
			// Idempotent re-set: the index already points at this exact item
			// with this exact domain. No state change, no event. Returning
			// early avoids a wasted SetLease cycle and keeps the audit log
			// honest (no Set event for a non-change).
			return role, nil
		case target.LeaseUuid == leaseUUID:
			return "", types.ErrCustomDomainAlreadyClaimed.Wrapf(
				"domain %q is already claimed by item %q on this lease",
				domain, target.ServiceName,
			)
		default:
			return "", types.ErrCustomDomainAlreadyClaimed.Wrapf(
				"domain %q is already claimed by lease %s",
				domain, target.LeaseUuid,
			)
		}
	case errors.Is(err, collections.ErrNotFound):
		// no existing claim; proceed.
	default:
		return "", err
	}

	lease.Items[itemIdx].CustomDomain = domain
	if err := k.SetLease(ctx, lease); err != nil {
		return "", err
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeLeaseCustomDomainSet,
			sdk.NewAttribute(types.AttributeKeyLeaseUUID, lease.Uuid),
			sdk.NewAttribute(types.AttributeKeyTenant, lease.Tenant),
			sdk.NewAttribute(types.AttributeKeyServiceName, itemServiceName),
			sdk.NewAttribute(types.AttributeKeyCustomDomain, domain),
			sdk.NewAttribute(types.AttributeKeySetBy, role),
		),
	)
	return role, nil
}
