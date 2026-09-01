package keeper

import (
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

const storageMigrationPageSize = 1000

// Migrator is a wrapper around Keeper used to register state migrations.
type Migrator struct {
	keeper Keeper
}

// NewMigrator returns a Migrator for the given keeper.
func NewMigrator(k Keeper) Migrator {
	return Migrator{keeper: k}
}

// Migrate1to2 marks the v1→v2 consensus-version bump for the custom_domain
// feature. It is a no-op: the new Params.ReservedDomainSuffixes field defaults
// to an empty slice (proto3 zero value) and the new CustomDomainIndex
// collection lives at a fresh store prefix, so no on-chain state needs
// rewriting.
//
// Operators are responsible for seeding Params.ReservedDomainSuffixes with
// the network's provider wildcard zones either:
//   - in the upgrade plan's genesis overlay at upgrade time, or
//   - via MsgUpdateParams from the module authority post-upgrade.
//
// Provider-zone defaults are intentionally NOT baked into the binary; once a
// hostname ships in a release tag it cannot be unshipped from chains that
// have run the upgrade. See ENG-82 for the planned automation
// (provider-declared wildcard zones in x/sku) that will replace manual
// reservation for the common case.
func (m Migrator) Migrate1to2(_ sdk.Context) error {
	return nil
}

// Migrate2to3 rewrites billing values so account identities are persisted as
// raw bytes instead of Bech32 strings. It also raises any historically
// under-backed credit-account reservation to the exact floor provable from
// live non-legacy leases. Fully verifiable accounts are reconciled exactly;
// accounts with live legacy leases preserve any unknown excess and are raised
// only where they fall below the known floor. Cached live-lease counts are
// rebuilt by decoded address identity. Collection keys and secondary indexes
// were already byte-addressed and are deliberately left untouched. Equivalent
// allowed-list Bech32 spellings are collapsed in first-seen slice order.
//
// Pages are read in key order and closed before writes begin, satisfying the KV
// iterator contract without relying on cache-store behavior. The migration is
// idempotent because the value codecs decode both v2 and v3 but always encode v3.
func (m Migrator) Migrate2to3(ctx sdk.Context) error {
	params, err := m.keeper.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("read billing params: %w", err)
	}
	params.AllowedList, err = types.CanonicalUniqueAddresses(params.AllowedList)
	if err != nil {
		return fmt.Errorf("repair billing params allowed list: %w", err)
	}
	if err := m.keeper.Params.Set(ctx, params); err != nil {
		return fmt.Errorf("rewrite billing params: %w", err)
	}

	if err := m.rewriteLeaseValues(ctx); err != nil {
		return err
	}
	if err := m.rewriteCreditAccountValues(ctx); err != nil {
		return err
	}
	return nil
}

// Migrate3to4 initializes consumable per-lease reservations without creating
// credit. A tenant's modern PENDING cohort is kept in full when every claim is
// bank-backed; otherwise the entire cohort expires at the cutover block. The
// bank-backed historical budget that remains is shared proportionally between
// modern ACTIVE nominal claims and one opaque live legacy cohort. See
// migrateConsumableReservations for the deterministic largest-remainder
// allocation and compatibility details.
func (m Migrator) Migrate3to4(ctx sdk.Context) error {
	return m.migrateConsumableReservations(ctx)
}

type leaseMigrationEntry struct {
	key   string
	value types.Lease
}

func (m Migrator) rewriteLeaseValues(ctx sdk.Context) error {
	var (
		lastKey  string
		hasStart bool
	)
	store := m.keeper.storeService.OpenKVStore(ctx)

	for {
		var keyRange collections.Ranger[string]
		if hasStart {
			keyRange = new(collections.Range[string]).StartExclusive(lastKey)
		}
		iterator, err := m.keeper.Leases.Iterate(ctx, keyRange)
		if err != nil {
			return fmt.Errorf("iterate billing leases: %w", err)
		}

		entries := make([]leaseMigrationEntry, 0, storageMigrationPageSize)
		for iterator.Valid() && len(entries) < storageMigrationPageSize {
			entry, entryErr := iterator.KeyValue()
			if entryErr != nil {
				_ = iterator.Close()
				return fmt.Errorf("decode billing lease migration entry: %w", entryErr)
			}
			entries = append(entries, leaseMigrationEntry{key: entry.Key, value: entry.Value})
			iterator.Next()
		}
		hasMore := iterator.Valid()
		if err := iterator.Close(); err != nil {
			return fmt.Errorf("close billing lease migration iterator: %w", err)
		}

		for _, entry := range entries {
			encoded, err := m.keeper.Leases.ValueCodec().Encode(entry.value)
			if err != nil {
				return fmt.Errorf("encode billing lease %q: %w", entry.key, err)
			}
			storeKey, err := collections.EncodeKeyWithPrefix(types.LeaseKey.Bytes(), collections.StringKey, entry.key)
			if err != nil {
				return fmt.Errorf("encode billing lease key %q: %w", entry.key, err)
			}
			if err := store.Set(storeKey, encoded); err != nil {
				return fmt.Errorf("rewrite billing lease %q: %w", entry.key, err)
			}
		}

		if !hasMore {
			return nil
		}
		lastKey = entries[len(entries)-1].key
		hasStart = true
	}
}

type creditAccountMigrationEntry struct {
	key   sdk.AccAddress
	value types.CreditAccount
}

type creditAccountMigrationRepair struct {
	knownReservationFloor sdk.Coins
	activeLeaseCount      uint64
	pendingLeaseCount     uint64
	hasLiveLegacy         bool
}

func (m Migrator) rewriteCreditAccountValues(ctx sdk.Context) error {
	var (
		lastKey  sdk.AccAddress
		hasStart bool
	)
	store := m.keeper.storeService.OpenKVStore(ctx)

	for {
		var keyRange collections.Ranger[sdk.AccAddress]
		if hasStart {
			keyRange = new(collections.Range[sdk.AccAddress]).StartExclusive(lastKey)
		}
		iterator, err := m.keeper.CreditAccounts.Iterate(ctx, keyRange)
		if err != nil {
			return fmt.Errorf("iterate billing credit accounts: %w", err)
		}

		entries := make([]creditAccountMigrationEntry, 0, storageMigrationPageSize)
		for iterator.Valid() && len(entries) < storageMigrationPageSize {
			entry, entryErr := iterator.KeyValue()
			if entryErr != nil {
				_ = iterator.Close()
				return fmt.Errorf("decode billing credit-account migration entry: %w", entryErr)
			}
			tenant, entryErr := sdk.AccAddressFromBech32(entry.Value.Tenant)
			if entryErr != nil {
				_ = iterator.Close()
				return fmt.Errorf("decode billing credit-account tenant %q: %w", entry.Value.Tenant, entryErr)
			}
			if !tenant.Equals(entry.Key) {
				_ = iterator.Close()
				return fmt.Errorf("billing credit-account tenant %q does not match its store key", entry.Value.Tenant)
			}
			entries = append(entries, creditAccountMigrationEntry{
				key:   append(sdk.AccAddress(nil), entry.Key...),
				value: entry.Value,
			})
			iterator.Next()
		}
		hasMore := iterator.Valid()
		if err := iterator.Close(); err != nil {
			return fmt.Errorf("close billing credit-account migration iterator: %w", err)
		}

		for _, entry := range entries {
			repair, err := m.calculateCreditAccountMigrationRepair(ctx, entry.key)
			if err != nil {
				return err
			}
			originalReservations := entry.value.ReservedAmounts
			entry.value.ReservedAmounts, err = types.ReconcilePreV4ReservationAggregate(
				entry.value.ReservedAmounts,
				repair.knownReservationFloor,
				repair.hasLiveLegacy,
			)
			if err != nil {
				return errorsmod.Wrapf(err, "repair billing reservations for tenant %q", entry.key.String())
			}
			if !entry.value.ReservedAmounts.Equal(originalReservations) {
				m.keeper.logger.Warn("repaired billing credit reservation during v2 to v3 migration",
					"tenant", entry.key.String(),
					"previous_reserved", originalReservations.String(),
					"repaired_reserved", entry.value.ReservedAmounts.String(),
					"known_reservation_floor", repair.knownReservationFloor.String(),
				)
			}
			if entry.value.ActiveLeaseCount != repair.activeLeaseCount ||
				entry.value.PendingLeaseCount != repair.pendingLeaseCount {
				m.keeper.logger.Warn("repaired billing credit-account lease counts during v2 to v3 migration",
					"tenant", entry.key.String(),
					"previous_active", entry.value.ActiveLeaseCount,
					"repaired_active", repair.activeLeaseCount,
					"previous_pending", entry.value.PendingLeaseCount,
					"repaired_pending", repair.pendingLeaseCount,
				)
			}
			entry.value.ActiveLeaseCount = repair.activeLeaseCount
			entry.value.PendingLeaseCount = repair.pendingLeaseCount

			encoded, err := m.keeper.CreditAccounts.ValueCodec().Encode(entry.value)
			if err != nil {
				return fmt.Errorf("encode billing credit account %q: %w", entry.key.String(), err)
			}
			storeKey, err := collections.EncodeKeyWithPrefix(types.CreditAccountKey.Bytes(), sdk.AccAddressKey, entry.key)
			if err != nil {
				return fmt.Errorf("encode billing credit-account key %q: %w", entry.key.String(), err)
			}
			if err := store.Set(storeKey, encoded); err != nil {
				return fmt.Errorf("rewrite billing credit account %q: %w", entry.key.String(), err)
			}
		}

		if !hasMore {
			return nil
		}
		lastKey = append(lastKey[:0], entries[len(entries)-1].key...)
		hasStart = true
	}
}

// calculateCreditAccountMigrationRepair reconstructs derived account fields
// from the existing byte-addressed tenant-state index. PENDING and ACTIVE are
// scanned in fixed order, and each index scan is ordered by lease primary key.
// Terminal leases cannot affect the repair and are deliberately not decoded.
func (m Migrator) calculateCreditAccountMigrationRepair(
	ctx sdk.Context,
	tenant sdk.AccAddress,
) (creditAccountMigrationRepair, error) {
	repair := creditAccountMigrationRepair{
		knownReservationFloor: sdk.NewCoins(),
	}
	knownReservationCoins := make([]sdk.Coin, 0)

	liveStates := [...]types.LeaseState{
		types.LEASE_STATE_PENDING,
		types.LEASE_STATE_ACTIVE,
	}
	for _, state := range liveStates {
		key := collections.Join(tenant, int32(state))
		iterator, err := m.keeper.Leases.Indexes.TenantState.MatchExact(ctx, key)
		if err != nil {
			return repair, fmt.Errorf(
				"iterate %s billing leases for tenant %q: %w",
				state.String(), tenant.String(), err,
			)
		}

		for ; iterator.Valid(); iterator.Next() {
			leaseUUID, err := iterator.PrimaryKey()
			if err != nil {
				_ = iterator.Close()
				return repair, fmt.Errorf(
					"decode %s billing tenant-state index entry for %q: %w",
					state.String(), tenant.String(), err,
				)
			}
			lease, err := m.keeper.Leases.Get(ctx, leaseUUID)
			if err != nil {
				_ = iterator.Close()
				return repair, fmt.Errorf("read billing lease %q for account repair: %w", leaseUUID, err)
			}
			leaseTenant, err := sdk.AccAddressFromBech32(lease.Tenant)
			if err != nil {
				_ = iterator.Close()
				return repair, fmt.Errorf("decode billing lease %q tenant %q: %w", leaseUUID, lease.Tenant, err)
			}
			if !tenant.Equals(leaseTenant) || lease.State != state {
				// Account aggregates can be rebuilt safely from a consistent
				// TenantState index, but silently accepting index drift could omit
				// or double-count a live reservation. Index repair requires a full
				// primary-state rebuild and is deliberately outside this migration.
				_ = iterator.Close()
				return repair, fmt.Errorf(
					"billing lease %q does not match its %s tenant-state index entry",
					leaseUUID, state.String(),
				)
			}

			switch state {
			case types.LEASE_STATE_PENDING:
				repair.pendingLeaseCount++
			case types.LEASE_STATE_ACTIVE:
				repair.activeLeaseCount++
			}

			if lease.MinLeaseDurationAtCreation == 0 {
				repair.hasLiveLegacy = true
				continue
			}
			reservation, err := types.CalculateLeaseReservation(lease.Items, lease.MinLeaseDurationAtCreation)
			if err != nil {
				_ = iterator.Close()
				return repair, errorsmod.Wrapf(err, "calculate billing lease %q reservation", leaseUUID)
			}
			knownReservationCoins = append(knownReservationCoins, reservation...)
		}

		if err := iterator.Close(); err != nil {
			return repair, fmt.Errorf(
				"close %s billing tenant-state iterator for %q: %w",
				state.String(), tenant.String(), err,
			)
		}
	}
	var err error
	repair.knownReservationFloor, err = types.SafeAggregateCoins(knownReservationCoins)
	if err != nil {
		return repair, errorsmod.Wrapf(err, "sum billing reservations for tenant %q", tenant.String())
	}
	return repair, nil
}
