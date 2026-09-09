package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

const (
	reservationInvariantRoute = "reservation-accounting"
	derivedIndexesRoute       = "derived-indexes"
)

// RegisterInvariants registers billing's cross-record accounting invariant.
func RegisterInvariants(registry sdk.InvariantRegistry, keeper Keeper) {
	registry.RegisterRoute(
		types.ModuleName,
		reservationInvariantRoute,
		ReservationAccountingInvariant(keeper),
	)
	registry.RegisterRoute(
		types.ModuleName,
		derivedIndexesRoute,
		DerivedIndexesInvariant(keeper),
	)
}

// DerivedIndexesInvariant verifies that every managed and manually maintained
// index is an exact bidirectional projection of primary billing state.
func DerivedIndexesInvariant(keeper Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		if err := keeper.validateDerivedIndexes(ctx); err != nil {
			return sdk.FormatInvariant(
				types.ModuleName,
				derivedIndexesRoute,
				fmt.Sprintf("invalid derived index state: %v", err),
			), true
		}
		return sdk.FormatInvariant(
			types.ModuleName,
			derivedIndexesRoute,
			"derived index state is valid",
		), false
	}
}

// ReservationAccountingInvariant verifies the exact v4 lease/account identity
// and that every aggregate reservation remains fully bank-backed.
func ReservationAccountingInvariant(keeper Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		genesis, err := keeper.exportGenesis(ctx)
		if err != nil {
			return sdk.FormatInvariant(
				types.ModuleName,
				reservationInvariantRoute,
				fmt.Sprintf("failed to export billing state: %v", err),
			), true
		}
		// Import validation repairs legacy derived state in a copy. Runtime
		// validation must instead require the current representation and inspect
		// stored counts, params, and reservation aggregates without any repair.
		if err := genesis.ValidateCurrentState(); err != nil {
			return sdk.FormatInvariant(
				types.ModuleName,
				reservationInvariantRoute,
				fmt.Sprintf("invalid reservation state: %v", err),
			), true
		}
		if err := keeper.validateGenesisReservationBacking(ctx, genesis); err != nil {
			return sdk.FormatInvariant(
				types.ModuleName,
				reservationInvariantRoute,
				fmt.Sprintf("under-backed reservation state: %v", err),
			), true
		}
		return sdk.FormatInvariant(
			types.ModuleName,
			reservationInvariantRoute,
			"reservation state is valid",
		), false
	}
}
