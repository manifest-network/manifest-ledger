package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

const reservationInvariantRoute = "reservation-accounting"

// RegisterInvariants registers billing's cross-record accounting invariant.
func RegisterInvariants(registry sdk.InvariantRegistry, keeper Keeper) {
	registry.RegisterRoute(
		types.ModuleName,
		reservationInvariantRoute,
		ReservationAccountingInvariant(keeper),
	)
}

// ReservationAccountingInvariant verifies the exact v4 lease/account identity
// and that every aggregate reservation remains fully bank-backed.
func ReservationAccountingInvariant(keeper Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		genesis := keeper.ExportGenesis(ctx)
		if err := genesis.Validate(); err != nil {
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
