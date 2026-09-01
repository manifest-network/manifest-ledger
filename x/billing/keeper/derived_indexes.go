package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// validateDerivedIndexes verifies both directions of every manually managed
// billing index. Collections-managed indexes are updated atomically by
// IndexedMap; these maps require explicit reconciliation and therefore an
// explicit invariant.
func (k Keeper) validateDerivedIndexes(ctx context.Context) error {
	if err := k.validateCreditAddressIndex(ctx); err != nil {
		return err
	}
	if err := k.validateLeaseBySKUIndex(ctx); err != nil {
		return err
	}
	return k.validateCustomDomainIndex(ctx)
}

func (k Keeper) validateCreditAddressIndex(ctx context.Context) error {
	if err := k.CreditAccounts.Walk(ctx, nil, func(tenant sdk.AccAddress, account types.CreditAccount) (bool, error) {
		accountTenant, err := sdk.AccAddressFromBech32(account.Tenant)
		if err != nil {
			return true, fmt.Errorf("credit account %X has invalid tenant: %w", []byte(tenant), err)
		}
		if !tenant.Equals(accountTenant) {
			return true, fmt.Errorf("credit account key %s does not match tenant %s", tenant, account.Tenant)
		}

		expectedCreditAddress := types.DeriveCreditAddress(tenant)
		accountCreditAddress, err := sdk.AccAddressFromBech32(account.CreditAddress)
		if err != nil {
			return true, fmt.Errorf("credit account for tenant %s has invalid credit address: %w", tenant, err)
		}
		if !expectedCreditAddress.Equals(accountCreditAddress) {
			return true, fmt.Errorf(
				"credit account for tenant %s has credit address %s, expected %s",
				tenant,
				account.CreditAddress,
				expectedCreditAddress,
			)
		}

		indexedTenant, err := k.CreditAddressIndex.Get(ctx, expectedCreditAddress)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				return true, fmt.Errorf("credit address %s for tenant %s is missing from the reverse index", expectedCreditAddress, tenant)
			}
			return true, err
		}
		if !tenant.Equals(indexedTenant) {
			return true, fmt.Errorf("credit address %s indexes tenant %s, expected %s", expectedCreditAddress, indexedTenant, tenant)
		}
		return false, nil
	}); err != nil {
		return err
	}

	return k.CreditAddressIndex.Walk(ctx, nil, func(creditAddress, tenant sdk.AccAddress) (bool, error) {
		expectedCreditAddress := types.DeriveCreditAddress(tenant)
		if !creditAddress.Equals(expectedCreditAddress) {
			return true, fmt.Errorf(
				"reverse index key %s does not match derived credit address %s for tenant %s",
				creditAddress,
				expectedCreditAddress,
				tenant,
			)
		}
		account, err := k.CreditAccounts.Get(ctx, tenant)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				return true, fmt.Errorf("reverse index credit address %s references missing tenant %s", creditAddress, tenant)
			}
			return true, err
		}
		accountTenant, err := sdk.AccAddressFromBech32(account.Tenant)
		if err != nil || !tenant.Equals(accountTenant) {
			return true, fmt.Errorf("reverse index credit address %s references mismatched tenant account %q", creditAddress, account.Tenant)
		}
		return false, nil
	})
}

func (k Keeper) validateLeaseBySKUIndex(ctx context.Context) error {
	if err := k.Leases.Walk(ctx, nil, func(uuid string, lease types.Lease) (bool, error) {
		if lease.Uuid != uuid {
			return true, fmt.Errorf("lease key %s does not match stored UUID %s", uuid, lease.Uuid)
		}
		for _, item := range lease.Items {
			indexed, err := k.LeaseBySKUIndex.Get(ctx, collections.Join(item.SkuUuid, uuid))
			if err != nil {
				if errors.Is(err, collections.ErrNotFound) {
					return true, fmt.Errorf("lease %s SKU %s is missing from the SKU index", uuid, item.SkuUuid)
				}
				return true, err
			}
			if !indexed {
				return true, fmt.Errorf("lease %s SKU %s has a false SKU-index marker", uuid, item.SkuUuid)
			}
		}
		return false, nil
	}); err != nil {
		return err
	}

	return k.LeaseBySKUIndex.Walk(ctx, nil, func(key collections.Pair[string, string], indexed bool) (bool, error) {
		skuUUID, leaseUUID := key.K1(), key.K2()
		if !indexed {
			return true, fmt.Errorf("lease %s SKU %s has a false SKU-index marker", leaseUUID, skuUUID)
		}
		lease, err := k.Leases.Get(ctx, leaseUUID)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				return true, fmt.Errorf("SKU index %s references missing lease %s", skuUUID, leaseUUID)
			}
			return true, err
		}
		for _, item := range lease.Items {
			if item.SkuUuid == skuUUID {
				return false, nil
			}
		}
		return true, fmt.Errorf("SKU index %s references lease %s without that SKU", skuUUID, leaseUUID)
	})
}

func (k Keeper) validateCustomDomainIndex(ctx context.Context) error {
	if err := k.Leases.Walk(ctx, nil, func(uuid string, lease types.Lease) (bool, error) {
		editable := lease.State == types.LEASE_STATE_PENDING || lease.State == types.LEASE_STATE_ACTIVE
		for _, item := range lease.Items {
			if item.CustomDomain == "" {
				continue
			}
			target, err := k.CustomDomainIndex.Get(ctx, item.CustomDomain)
			switch {
			case err == nil:
				if editable && target.LeaseUuid == uuid && target.ServiceName == item.ServiceName {
					continue
				}
				if !editable && (target.LeaseUuid != uuid || target.ServiceName != item.ServiceName) {
					continue // A later editable lease legitimately reclaimed the domain.
				}
				return true, fmt.Errorf(
					"custom domain %s has target (%s, %s), incompatible with lease %s item %s in state %s",
					item.CustomDomain,
					target.LeaseUuid,
					target.ServiceName,
					uuid,
					item.ServiceName,
					lease.State,
				)
			case errors.Is(err, collections.ErrNotFound):
				if editable {
					return true, fmt.Errorf(
						"lease %s item %s custom domain %s is missing from the reverse index",
						uuid,
						item.ServiceName,
						item.CustomDomain,
					)
				}
			default:
				return true, err
			}
		}
		return false, nil
	}); err != nil {
		return err
	}

	return k.CustomDomainIndex.Walk(ctx, nil, func(domain string, target types.CustomDomainTarget) (bool, error) {
		lease, err := k.Leases.Get(ctx, target.LeaseUuid)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				return true, fmt.Errorf("custom domain %s references missing lease %s", domain, target.LeaseUuid)
			}
			return true, err
		}
		if lease.State != types.LEASE_STATE_PENDING && lease.State != types.LEASE_STATE_ACTIVE {
			return true, fmt.Errorf("custom domain %s references terminal lease %s in state %s", domain, target.LeaseUuid, lease.State)
		}
		for _, item := range lease.Items {
			if item.ServiceName == target.ServiceName && item.CustomDomain == domain {
				return false, nil
			}
		}
		return true, fmt.Errorf("custom domain %s references missing item %s on lease %s", domain, target.ServiceName, target.LeaseUuid)
	})
}
