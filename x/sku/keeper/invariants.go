package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/collections/indexes"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

const stateInvariantRoute = "state"

// RegisterInvariants registers SKU's primary-state invariant.
func RegisterInvariants(registry sdk.InvariantRegistry, keeper Keeper) {
	registry.RegisterRoute(types.ModuleName, stateInvariantRoute, StateInvariant(keeper))
}

// StateInvariant validates the complete exported state, verifies that each
// collection key agrees with the UUID stored in its value, and checks both
// directions of every Collections-managed secondary index.
func StateInvariant(keeper Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var providerCount uint64
		if err := keeper.Providers.Walk(ctx, nil, func(uuid string, provider types.Provider) (bool, error) {
			if uuid != provider.Uuid {
				return true, fmt.Errorf("provider key %s does not match stored UUID %s", uuid, provider.Uuid)
			}
			providerCount++
			return false, nil
		}); err != nil {
			return sdk.FormatInvariant(types.ModuleName, stateInvariantRoute, err.Error()), true
		}

		var skuCount uint64
		if err := keeper.SKUs.Walk(ctx, nil, func(uuid string, sku types.SKU) (bool, error) {
			if uuid != sku.Uuid {
				return true, fmt.Errorf("SKU key %s does not match stored UUID %s", uuid, sku.Uuid)
			}
			skuCount++
			return false, nil
		}); err != nil {
			return sdk.FormatInvariant(types.ModuleName, stateInvariantRoute, err.Error()), true
		}
		if err := keeper.validateSecondaryIndexes(ctx, providerCount, skuCount); err != nil {
			return sdk.FormatInvariant(types.ModuleName, stateInvariantRoute, err.Error()), true
		}

		genesis, err := keeper.exportGenesis(ctx)
		if err != nil {
			return sdk.FormatInvariant(
				types.ModuleName,
				stateInvariantRoute,
				fmt.Sprintf("failed to export SKU state: %v", err),
			), true
		}
		// Validate the Params exactly as stored before import-safe genesis
		// validation canonicalizes historical Bech32 aliases in a copy. Live
		// duplicate or over-limit Params are corruption and must remain visible.
		if err := genesis.Params.Validate(); err != nil {
			return sdk.FormatInvariant(
				types.ModuleName,
				stateInvariantRoute,
				fmt.Sprintf("invalid stored SKU params: %v", err),
			), true
		}
		if err := genesis.Validate(); err != nil {
			return sdk.FormatInvariant(
				types.ModuleName,
				stateInvariantRoute,
				fmt.Sprintf("invalid SKU state: %v", err),
			), true
		}
		return sdk.FormatInvariant(types.ModuleName, stateInvariantRoute, "SKU state is valid"), false
	}
}

func (k Keeper) validateSecondaryIndexes(ctx context.Context, providerCount, skuCount uint64) error {
	if err := validateMultiIndex(
		ctx,
		"provider address",
		providerCount,
		k.Providers.Indexes.Address,
		k.Providers.Get,
		func(provider types.Provider) (sdk.AccAddress, error) {
			address, err := sdk.AccAddressFromBech32(provider.Address)
			if err != nil {
				return nil, fmt.Errorf("provider %s has invalid address: %w", provider.Uuid, err)
			}
			return address, nil
		},
		func(indexed, expected sdk.AccAddress) bool { return indexed.Equals(expected) },
		func(reference sdk.AccAddress) string { return reference.String() },
	); err != nil {
		return err
	}
	if err := validateMultiIndex(
		ctx,
		"provider active",
		providerCount,
		k.Providers.Indexes.Active,
		k.Providers.Get,
		func(provider types.Provider) (bool, error) { return provider.Active, nil },
		func(indexed, expected bool) bool { return indexed == expected },
		func(reference bool) string { return fmt.Sprintf("%t", reference) },
	); err != nil {
		return err
	}
	if err := validateMultiIndex(
		ctx,
		"SKU provider",
		skuCount,
		k.SKUs.Indexes.Provider,
		k.SKUs.Get,
		func(sku types.SKU) (string, error) { return sku.ProviderUuid, nil },
		func(indexed, expected string) bool { return indexed == expected },
		func(reference string) string { return fmt.Sprintf("%q", reference) },
	); err != nil {
		return err
	}
	if err := validateMultiIndex(
		ctx,
		"SKU active",
		skuCount,
		k.SKUs.Indexes.Active,
		k.SKUs.Get,
		func(sku types.SKU) (bool, error) { return sku.Active, nil },
		func(indexed, expected bool) bool { return indexed == expected },
		func(reference bool) string { return fmt.Sprintf("%t", reference) },
	); err != nil {
		return err
	}
	return validateMultiIndex(
		ctx,
		"SKU provider-active",
		skuCount,
		k.SKUs.Indexes.ProviderActive,
		k.SKUs.Get,
		func(sku types.SKU) (collections.Pair[string, bool], error) {
			return collections.Join(sku.ProviderUuid, sku.Active), nil
		},
		func(indexed, expected collections.Pair[string, bool]) bool {
			return indexed.K1() == expected.K1() && indexed.K2() == expected.K2()
		},
		func(reference collections.Pair[string, bool]) string {
			return fmt.Sprintf("(%q, %t)", reference.K1(), reference.K2())
		},
	)
}

// validateMultiIndex proves both directions without materializing or ranging
// over a Go map. Every index row must resolve to a primary value whose derived
// reference key matches, and an equal row count then proves that every primary
// value has its one unique (reference, primary) KeySet entry.
func validateMultiIndex[ReferenceKey, Value any](
	ctx context.Context,
	name string,
	expectedCount uint64,
	index *indexes.Multi[ReferenceKey, string, Value],
	getValue func(context.Context, string) (Value, error),
	expectedReference func(Value) (ReferenceKey, error),
	equal func(ReferenceKey, ReferenceKey) bool,
	formatReference func(ReferenceKey) string,
) error {
	var actualCount uint64
	err := index.Walk(ctx, nil, func(indexedReference ReferenceKey, primaryKey string) (bool, error) {
		value, err := getValue(ctx, primaryKey)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				return true, fmt.Errorf("%s index references missing primary key %s", name, primaryKey)
			}
			return true, fmt.Errorf("read %s index primary key %s: %w", name, primaryKey, err)
		}
		expected, err := expectedReference(value)
		if err != nil {
			return true, err
		}
		if !equal(indexedReference, expected) {
			return true, fmt.Errorf(
				"%s index key %s for primary key %s does not match derived key %s",
				name,
				formatReference(indexedReference),
				primaryKey,
				formatReference(expected),
			)
		}
		actualCount++
		return false, nil
	})
	if err != nil {
		return err
	}
	if actualCount != expectedCount {
		return fmt.Errorf(
			"%s index contains %d entries, expected %d",
			name,
			actualCount,
			expectedCount,
		)
	}
	return nil
}
