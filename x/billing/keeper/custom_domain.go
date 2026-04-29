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

// GetLeaseByCustomDomain returns the lease that has claimed the given custom_domain.
// The second return is false (with nil error) when no lease has claimed the domain.
func (k *Keeper) GetLeaseByCustomDomain(ctx context.Context, domain string) (types.Lease, bool, error) {
	leaseUUID, err := k.CustomDomainIndex.Get(ctx, domain)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.Lease{}, false, nil
		}
		return types.Lease{}, false, err
	}
	lease, err := k.GetLease(ctx, leaseUUID)
	if err != nil {
		return types.Lease{}, false, err
	}
	return lease, true, nil
}

// SetLeaseCustomDomain sets or clears Lease.custom_domain on behalf of sender.
// An empty domain clears the field. Returns the role under which the call was
// authorised ("tenant" / "authority" / "allowed") so the msg server can include
// it in the emitted event.
//
// Index maintenance is delegated to SetLease — this method only validates input
// and writes the new field value. The pre-flight uniqueness check exists to
// surface a friendly ErrCustomDomainAlreadyClaimed before mutation; SetLease's
// storage-level uniqueness enforcement is the defence-in-depth.
func (k *Keeper) SetLeaseCustomDomain(ctx context.Context, sender, leaseUUID, domain string) (string, error) {
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

	domain = strings.ToLower(strings.TrimSpace(domain))
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	if domain == "" {
		previous := lease.CustomDomain
		lease.CustomDomain = ""
		if err := k.SetLease(ctx, lease); err != nil {
			return "", err
		}
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeLeaseCustomDomainCleared,
				sdk.NewAttribute(types.AttributeKeyLeaseUUID, lease.Uuid),
				sdk.NewAttribute(types.AttributeKeyTenant, lease.Tenant),
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

	// Pre-flight uniqueness so the caller gets a friendly error before mutation.
	// SetLease re-checks at the storage layer as defence-in-depth.
	existing, has, err := k.GetLeaseByCustomDomain(ctx, domain)
	if err != nil {
		return "", err
	}
	if has && existing.Uuid != leaseUUID {
		return "", types.ErrCustomDomainAlreadyClaimed.Wrapf("domain %q is already claimed by lease %s", domain, existing.Uuid)
	}

	lease.CustomDomain = domain
	if err := k.SetLease(ctx, lease); err != nil {
		return "", err
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeLeaseCustomDomainSet,
			sdk.NewAttribute(types.AttributeKeyLeaseUUID, lease.Uuid),
			sdk.NewAttribute(types.AttributeKeyTenant, lease.Tenant),
			sdk.NewAttribute(types.AttributeKeyCustomDomain, domain),
			sdk.NewAttribute(types.AttributeKeySetBy, role),
		),
	)
	return role, nil
}
