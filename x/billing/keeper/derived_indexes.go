package keeper

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/collections/indexes"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/internal/collectionsutil"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// validateDerivedIndexes verifies both directions of every billing index.
// Collections-managed indexes are normally updated atomically by IndexedMap,
// but validating them here makes historical drift and unsafe internal writes
// observable before an index-backed consensus path such as EndBlock relies on
// them.
func (k Keeper) validateDerivedIndexes(ctx context.Context) error {
	if err := k.validateManagedLeaseIndexes(ctx); err != nil {
		return err
	}
	if err := k.validateCreditAddressIndex(ctx); err != nil {
		return err
	}
	if err := k.validateLeaseBySKUIndex(ctx); err != nil {
		return err
	}
	return k.validateCustomDomainIndex(ctx)
}

func (k Keeper) validateManagedLeaseIndexes(ctx context.Context) error {
	scanLeases := func(validate func(string, types.Lease) error) (uint64, error) {
		return collectionsutil.ValidateMap(
			ctx,
			"lease collection",
			k.Leases.Iterate,
			strconv.Quote,
			validate,
		)
	}
	leaseCount, err := scanLeases(func(uuid string, lease types.Lease) error {
		if uuid != lease.Uuid {
			return fmt.Errorf("lease key %s does not match stored UUID %s", uuid, lease.Uuid)
		}
		return nil
	})
	if err != nil {
		return err
	}
	walkLeases := func(validate func(string, types.Lease) error) error {
		_, err := scanLeases(validate)
		return err
	}

	if err := validateLeaseMultiIndex(
		ctx,
		"tenant",
		leaseCount,
		k.Leases.Indexes.Tenant,
		k.Leases.Get,
		walkLeases,
		func(lease types.Lease) (sdk.AccAddress, error) {
			tenant, err := sdk.AccAddressFromBech32(lease.Tenant)
			if err != nil {
				return nil, fmt.Errorf("lease %s has invalid tenant: %w", lease.Uuid, err)
			}
			return tenant, nil
		},
		func(indexed, expected sdk.AccAddress) bool { return indexed.Equals(expected) },
		func(reference sdk.AccAddress) string { return reference.String() },
	); err != nil {
		return err
	}
	if err := validateLeaseMultiIndex(
		ctx,
		"provider",
		leaseCount,
		k.Leases.Indexes.Provider,
		k.Leases.Get,
		walkLeases,
		func(lease types.Lease) (string, error) { return lease.ProviderUuid, nil },
		func(indexed, expected string) bool { return indexed == expected },
		func(reference string) string { return fmt.Sprintf("%q", reference) },
	); err != nil {
		return err
	}
	if err := validateLeaseMultiIndex(
		ctx,
		"state",
		leaseCount,
		k.Leases.Indexes.State,
		k.Leases.Get,
		walkLeases,
		func(lease types.Lease) (int32, error) { return int32(lease.State), nil },
		func(indexed, expected int32) bool { return indexed == expected },
		func(reference int32) string { return fmt.Sprintf("%d", reference) },
	); err != nil {
		return err
	}
	if err := validateLeaseMultiIndex(
		ctx,
		"provider-state",
		leaseCount,
		k.Leases.Indexes.ProviderState,
		k.Leases.Get,
		walkLeases,
		func(lease types.Lease) (collections.Pair[string, int32], error) {
			return collections.Join(lease.ProviderUuid, int32(lease.State)), nil
		},
		func(indexed, expected collections.Pair[string, int32]) bool {
			return indexed.K1() == expected.K1() && indexed.K2() == expected.K2()
		},
		func(reference collections.Pair[string, int32]) string {
			return fmt.Sprintf("(%q, %d)", reference.K1(), reference.K2())
		},
	); err != nil {
		return err
	}
	if err := validateLeaseMultiIndex(
		ctx,
		"tenant-state",
		leaseCount,
		k.Leases.Indexes.TenantState,
		k.Leases.Get,
		walkLeases,
		func(lease types.Lease) (collections.Pair[sdk.AccAddress, int32], error) {
			tenant, err := sdk.AccAddressFromBech32(lease.Tenant)
			if err != nil {
				return collections.Pair[sdk.AccAddress, int32]{}, fmt.Errorf(
					"lease %s has invalid tenant: %w",
					lease.Uuid,
					err,
				)
			}
			return collections.Join(tenant, int32(lease.State)), nil
		},
		func(indexed, expected collections.Pair[sdk.AccAddress, int32]) bool {
			return indexed.K1().Equals(expected.K1()) && indexed.K2() == expected.K2()
		},
		func(reference collections.Pair[sdk.AccAddress, int32]) string {
			return fmt.Sprintf("(%s, %d)", reference.K1(), reference.K2())
		},
	); err != nil {
		return err
	}
	return validateLeaseMultiIndex(
		ctx,
		"state-created-at",
		leaseCount,
		k.Leases.Indexes.StateCreatedAt,
		k.Leases.Get,
		walkLeases,
		func(lease types.Lease) (collections.Pair[int32, time.Time], error) {
			return collections.Join(int32(lease.State), lease.CreatedAt), nil
		},
		func(indexed, expected collections.Pair[int32, time.Time]) bool {
			return indexed.K1() == expected.K1() && indexed.K2().Equal(expected.K2())
		},
		func(reference collections.Pair[int32, time.Time]) string {
			return fmt.Sprintf(
				"(%d, %s)",
				reference.K1(),
				reference.K2().UTC().Format(time.RFC3339Nano),
			)
		},
	)
}

// validateLeaseMultiIndex proves both index directions without materializing
// or ranging over a Go map. Every row must match its primary lease, and an
// equal row count then proves that every primary has its unique expected row.
func validateLeaseMultiIndex[ReferenceKey any](
	ctx context.Context,
	name string,
	expectedCount uint64,
	index *indexes.Multi[ReferenceKey, string, types.Lease],
	getLease func(context.Context, string) (types.Lease, error),
	walkLeases func(func(string, types.Lease) error) error,
	expectedReference func(types.Lease) (ReferenceKey, error),
	equal func(ReferenceKey, ReferenceKey) bool,
	formatReference func(ReferenceKey) string,
) error {
	var actualCount uint64
	err := collectionsutil.ValidateMultiIndex(ctx, "lease "+name, index, func(indexedReference ReferenceKey, leaseUUID string) error {
		lease, err := getLease(ctx, leaseUUID)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				return fmt.Errorf("lease %s index references missing primary key %s", name, leaseUUID)
			}
			return fmt.Errorf("read lease %s index primary key %s: %w", name, leaseUUID, err)
		}
		expected, err := expectedReference(lease)
		if err != nil {
			return err
		}
		if !equal(indexedReference, expected) {
			return fmt.Errorf(
				"lease %s index key %s for primary key %s does not match derived key %s",
				name,
				formatReference(indexedReference),
				leaseUUID,
				formatReference(expected),
			)
		}
		actualCount++
		return nil
	})
	if err != nil {
		return err
	}
	if actualCount == expectedCount {
		return nil
	}

	// Keep the healthy path to one index walk. If the counts differ, walk the
	// ordered primary map only to identify the exact lease whose row is absent.
	if err := walkLeases(func(leaseUUID string, lease types.Lease) error {
		expected, err := expectedReference(lease)
		if err != nil {
			return err
		}
		indexed, err := leaseMultiIndexContains(ctx, index, expected, leaseUUID)
		if err != nil {
			return fmt.Errorf(
				"inspect lease %s index key %s for primary key %s: %w",
				name,
				formatReference(expected),
				leaseUUID,
				err,
			)
		}
		if !indexed {
			return fmt.Errorf(
				"lease %s index is missing derived key %s for lease %s",
				name,
				formatReference(expected),
				leaseUUID,
			)
		}
		return nil
	}); err != nil {
		return err
	}

	return fmt.Errorf(
		"lease %s index contains %d entries, expected %d",
		name,
		actualCount,
		expectedCount,
	)
}

func leaseMultiIndexContains[ReferenceKey any](
	ctx context.Context,
	index *indexes.Multi[ReferenceKey, string, types.Lease],
	reference ReferenceKey,
	leaseUUID string,
) (found bool, err error) {
	iterator, err := index.MatchExact(ctx, reference)
	if err != nil {
		return false, err
	}
	defer func() {
		err = errors.Join(err, iterator.Close())
	}()

	for ; iterator.Valid(); iterator.Next() {
		indexedUUID, err := iterator.PrimaryKey()
		if err != nil {
			return false, err
		}
		if indexedUUID == leaseUUID {
			return true, nil
		}
	}
	return false, nil
}

func (k Keeper) validateCreditAddressIndex(ctx context.Context) error {
	_, err := collectionsutil.ValidateMap(
		ctx,
		"credit-account collection",
		k.CreditAccounts.Iterate,
		func(tenant sdk.AccAddress) string { return tenant.String() },
		func(tenant sdk.AccAddress, account types.CreditAccount) error {
			accountTenant, err := sdk.AccAddressFromBech32(account.Tenant)
			if err != nil {
				return fmt.Errorf("credit account %X has invalid tenant: %w", []byte(tenant), err)
			}
			if !tenant.Equals(accountTenant) {
				return fmt.Errorf("credit account key %s does not match tenant %s", tenant, account.Tenant)
			}

			expectedCreditAddress := types.DeriveCreditAddress(tenant)
			accountCreditAddress, err := sdk.AccAddressFromBech32(account.CreditAddress)
			if err != nil {
				return fmt.Errorf("credit account for tenant %s has invalid credit address: %w", tenant, err)
			}
			if !expectedCreditAddress.Equals(accountCreditAddress) {
				return fmt.Errorf(
					"credit account for tenant %s has credit address %s, expected %s",
					tenant,
					account.CreditAddress,
					expectedCreditAddress,
				)
			}

			indexedTenant, err := k.CreditAddressIndex.Get(ctx, expectedCreditAddress)
			if err != nil {
				if errors.Is(err, collections.ErrNotFound) {
					return fmt.Errorf("credit address %s for tenant %s is missing from the reverse index", expectedCreditAddress, tenant)
				}
				return fmt.Errorf(
					"read credit-address reverse index for tenant %s at credit address %s: %w",
					tenant,
					expectedCreditAddress,
					err,
				)
			}
			if !tenant.Equals(indexedTenant) {
				return fmt.Errorf("credit address %s indexes tenant %s, expected %s", expectedCreditAddress, indexedTenant, tenant)
			}
			return nil
		})
	if err != nil {
		return err
	}

	_, err = collectionsutil.ValidateMap(
		ctx,
		"credit-address reverse index",
		k.CreditAddressIndex.Iterate,
		func(creditAddress sdk.AccAddress) string { return creditAddress.String() },
		func(creditAddress, tenant sdk.AccAddress) error {
			expectedCreditAddress := types.DeriveCreditAddress(tenant)
			if !creditAddress.Equals(expectedCreditAddress) {
				return fmt.Errorf(
					"reverse index key %s does not match derived credit address %s for tenant %s",
					creditAddress,
					expectedCreditAddress,
					tenant,
				)
			}
			account, err := k.CreditAccounts.Get(ctx, tenant)
			if err != nil {
				if errors.Is(err, collections.ErrNotFound) {
					return fmt.Errorf("reverse index credit address %s references missing tenant %s", creditAddress, tenant)
				}
				return fmt.Errorf(
					"read credit account for reverse-index tenant %s at credit address %s: %w",
					tenant,
					creditAddress,
					err,
				)
			}
			accountTenant, err := sdk.AccAddressFromBech32(account.Tenant)
			if err != nil || !tenant.Equals(accountTenant) {
				return fmt.Errorf("reverse index credit address %s references mismatched tenant account %q", creditAddress, account.Tenant)
			}
			return nil
		})
	return err
}

func (k Keeper) validateLeaseBySKUIndex(ctx context.Context) error {
	_, err := collectionsutil.ValidateMap(
		ctx,
		"lease collection",
		k.Leases.Iterate,
		strconv.Quote,
		func(uuid string, lease types.Lease) error {
			if lease.Uuid != uuid {
				return fmt.Errorf("lease key %s does not match stored UUID %s", uuid, lease.Uuid)
			}
			for _, item := range lease.Items {
				indexed, err := k.LeaseBySKUIndex.Get(ctx, collections.Join(item.SkuUuid, uuid))
				if err != nil {
					if errors.Is(err, collections.ErrNotFound) {
						return fmt.Errorf("lease %s SKU %s is missing from the SKU index", uuid, item.SkuUuid)
					}
					return fmt.Errorf("read SKU index for lease %s SKU %s: %w", uuid, item.SkuUuid, err)
				}
				if !indexed {
					return fmt.Errorf("lease %s SKU %s has a false SKU-index marker", uuid, item.SkuUuid)
				}
			}
			return nil
		})
	if err != nil {
		return err
	}

	_, err = collectionsutil.ValidateMap(
		ctx,
		"lease-by-SKU reverse index",
		k.LeaseBySKUIndex.Iterate,
		func(key collections.Pair[string, string]) string {
			return fmt.Sprintf("(%q, %q)", key.K1(), key.K2())
		},
		func(key collections.Pair[string, string], indexed bool) error {
			skuUUID, leaseUUID := key.K1(), key.K2()
			if !indexed {
				return fmt.Errorf("lease %s SKU %s has a false SKU-index marker", leaseUUID, skuUUID)
			}
			lease, err := k.Leases.Get(ctx, leaseUUID)
			if err != nil {
				if errors.Is(err, collections.ErrNotFound) {
					return fmt.Errorf("SKU index %s references missing lease %s", skuUUID, leaseUUID)
				}
				return fmt.Errorf("read lease %s referenced by SKU index %s: %w", leaseUUID, skuUUID, err)
			}
			for _, item := range lease.Items {
				if item.SkuUuid == skuUUID {
					return nil
				}
			}
			return fmt.Errorf("SKU index %s references lease %s without that SKU", skuUUID, leaseUUID)
		})
	return err
}

func (k Keeper) validateCustomDomainIndex(ctx context.Context) error {
	_, err := collectionsutil.ValidateMap(
		ctx,
		"lease collection",
		k.Leases.Iterate,
		strconv.Quote,
		func(uuid string, lease types.Lease) error {
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
					return fmt.Errorf(
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
						return fmt.Errorf(
							"lease %s item %s custom domain %s is missing from the reverse index",
							uuid,
							item.ServiceName,
							item.CustomDomain,
						)
					}
				default:
					return fmt.Errorf(
						"read custom-domain reverse index for lease %s item %q domain %q: %w",
						uuid,
						item.ServiceName,
						item.CustomDomain,
						err,
					)
				}
			}
			return nil
		})
	if err != nil {
		return err
	}

	_, err = collectionsutil.ValidateMap(
		ctx,
		"custom-domain reverse index",
		k.CustomDomainIndex.Iterate,
		strconv.Quote,
		func(domain string, target types.CustomDomainTarget) error {
			lease, err := k.Leases.Get(ctx, target.LeaseUuid)
			if err != nil {
				if errors.Is(err, collections.ErrNotFound) {
					return fmt.Errorf("custom domain %s references missing lease %s", domain, target.LeaseUuid)
				}
				return fmt.Errorf("read lease %s referenced by custom domain %q: %w", target.LeaseUuid, domain, err)
			}
			if lease.State != types.LEASE_STATE_PENDING && lease.State != types.LEASE_STATE_ACTIVE {
				return fmt.Errorf("custom domain %s references terminal lease %s in state %s", domain, target.LeaseUuid, lease.State)
			}
			for _, item := range lease.Items {
				if item.ServiceName == target.ServiceName && item.CustomDomain == domain {
					return nil
				}
			}
			return fmt.Errorf("custom domain %s references missing item %s on lease %s", domain, target.ServiceName, target.LeaseUuid)
		})
	return err
}
