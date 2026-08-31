package types

// Credit Reservation System
//
// The credit reservation system prevents overbooking by tracking reserved amounts
// per tenant. This ensures that when a lease is created, sufficient credit is
// guaranteed for at least min_lease_duration seconds of operation.
//
// # Invariant
//
// For tenants whose PENDING and ACTIVE leases all store their creation-time
// minimum duration, the following invariant must hold:
//
//	CreditAccount.ReservedAmounts == SUM(GetLeaseReservationAmount(lease, params.MinLeaseDuration))
//	                                 for all PENDING and ACTIVE leases of the tenant
//
// A legacy lease may have a zero MinLeaseDurationAtCreation because that field
// was not persisted when the reservation was made. Its historical contribution
// cannot be reconstructed after parameter changes, and a terminal transition
// may have left a residual after using a later parameter value. Genesis imports
// therefore require the stored aggregate to cover every verifiable non-legacy
// lease rather than guessing an exact legacy amount.
//
// # Reservation Lifecycle
//
//   - ADDED: When a lease is created (enters PENDING state)
//   - MAINTAINED: When a lease is acknowledged (transitions to ACTIVE state)
//   - RELEASED: When a non-legacy lease transitions to CLOSED, REJECTED, or EXPIRED
//   - DEFERRED: Legacy release waits until the last live legacy lease terminates
//
// # Available Credit Calculation
//
//	AvailableCredit = CreditBalance - ReservedAmounts
//
// New leases can only be created if AvailableCredit >= NewLeaseReservation for all denoms.
//
// # Parameter Change Protection
//
// New leases store MinLeaseDurationAtCreation to ensure consistent reservation
// calculation regardless of subsequent governance changes to MinLeaseDuration.
// Strict validation and debugging calculations may use the current parameter
// as an explicit fallback for a legacy lease. Lifecycle release never guesses
// that unknown historical amount: it defers release while another live legacy
// lease remains, then reconciles the aggregate to the exact non-legacy floor
// when the last live legacy lease terminates. Imported state exceeding the
// fixed per-tenant scan ceiling instead preserves the aggregate conservatively,
// without assuming a later retry, rather than blocking the lifecycle transition
// or performing an unbounded scan. The v2→v3 migration reconstructs cached
// counts and repairs reservations to the exact known floor while preserving any
// unprovable live-legacy excess.

import (
	"crypto/sha256"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/address"
)

// DeriveCreditAddress derives a deterministic credit account address from a tenant address.
// The address is derived by hashing the module name prefix with the tenant address.
func DeriveCreditAddress(tenant sdk.AccAddress) sdk.AccAddress {
	key := append([]byte(CreditAccountAddressPrefix), tenant.Bytes()...)
	hash := sha256.Sum256(key)
	return address.Module(ModuleName, hash[:])
}

// DeriveCreditAddressFromBech32 derives a credit account address from a bech32 tenant address string.
func DeriveCreditAddressFromBech32(tenant string) (sdk.AccAddress, error) {
	tenantAddr, err := sdk.AccAddressFromBech32(tenant)
	if err != nil {
		return nil, err
	}
	return DeriveCreditAddress(tenantAddr), nil
}

// GetAvailableCredit returns the balance minus reserved amounts per denom.
// This represents the credit available for creating new leases.
// For denoms that exist in balance but not in reserved, the full balance is available.
// For denoms that exist in reserved but not in balance, zero is available.
func GetAvailableCredit(balance, reserved sdk.Coins) sdk.Coins {
	if reserved.IsZero() {
		return balance
	}

	available := sdk.NewCoins()
	for _, coin := range balance {
		reservedAmount := reserved.AmountOf(coin.Denom)
		if coin.Amount.GT(reservedAmount) {
			// balance is canonical, so preserving its iteration order keeps the
			// result canonical without another arithmetic merge.
			available = append(available, sdk.Coin{
				Denom:  coin.Denom,
				Amount: coin.Amount.Sub(reservedAmount),
			})
		}
		// If balance <= reserved, available is 0 for that denom (implicitly not added)
	}

	return available
}

// AddReservation adds amounts to reserved (for lease creation).
// Returns the new reserved amounts or an error if the aggregate cannot be
// represented by math.Int.
func AddReservation(reserved, toAdd sdk.Coins) (sdk.Coins, error) {
	return SafeAddCoins(reserved, toAdd)
}

// SubtractReservation subtracts amounts from reserved (for lease closure).
// Returns the new reserved amounts. If a denom would go negative, it's set to zero.
func SubtractReservation(reserved, toSubtract sdk.Coins) sdk.Coins {
	if reserved.IsZero() {
		return sdk.NewCoins()
	}

	result := sdk.NewCoins()
	for _, coin := range reserved {
		subtractAmount := toSubtract.AmountOf(coin.Denom)
		if coin.Amount.GT(subtractAmount) {
			result = append(result, sdk.Coin{
				Denom:  coin.Denom,
				Amount: coin.Amount.Sub(subtractAmount),
			})
		}
		// If amount <= subtractAmount, don't add the coin (effectively zero)
	}

	return result
}

// CalculateLeaseReservation calculates the reservation amount for a lease.
// reservation = sum(rate_per_second * quantity) * min_lease_duration for each denom.
func CalculateLeaseReservation(items []LeaseItem, minLeaseDuration uint64) (sdk.Coins, error) {
	if len(items) == 0 {
		return sdk.NewCoins(), nil
	}
	for itemIndex, item := range items {
		if err := ValidateLeaseItemPricing(item.LockedPrice, item.Quantity); err != nil {
			return nil, errorsmod.Wrapf(err, "validate pricing for lease item %d", itemIndex)
		}
	}
	if minLeaseDuration == 0 {
		return sdk.NewCoins(), nil
	}

	// Calculate total rates per denom
	totalRates := sdk.NewCoins()
	for itemIndex, item := range items {
		// Rate = locked_price * quantity
		itemRate, err := SafeMultiplyCoin(item.LockedPrice, sdkmath.NewIntFromUint64(item.Quantity))
		if err != nil {
			return nil, errorsmod.Wrapf(err, "calculate rate for lease item %d", itemIndex)
		}
		if itemRate.IsZero() {
			continue
		}
		totalRates, err = SafeAddCoins(totalRates, sdk.Coins{itemRate})
		if err != nil {
			return nil, errorsmod.Wrapf(err, "sum rate for lease item %d", itemIndex)
		}
	}

	return CalculateLeaseReservationFromRates(totalRates, minLeaseDuration)
}

// CalculateLeaseReservationFromRates calculates the reservation from pre-computed rates.
// This is useful when total rates are already calculated during lease creation.
func CalculateLeaseReservationFromRates(totalRatesPerSecond sdk.Coins, minLeaseDuration uint64) (sdk.Coins, error) {
	canonicalRates, err := SafeAddCoins(sdk.NewCoins(), totalRatesPerSecond)
	if err != nil {
		return nil, errorsmod.Wrap(err, "validate total rates per second")
	}
	if canonicalRates.IsZero() || minLeaseDuration == 0 {
		return sdk.NewCoins(), nil
	}

	reservation := make(sdk.Coins, 0, len(canonicalRates))
	minDuration := sdkmath.NewIntFromUint64(minLeaseDuration)
	for rateIndex, rate := range canonicalRates {
		amount, err := SafeMultiplyCoin(rate, minDuration)
		if err != nil {
			return nil, errorsmod.Wrapf(err, "calculate reservation for rate %d", rateIndex)
		}
		if amount.IsPositive() {
			reservation = append(reservation, amount)
		}
	}

	return reservation, nil
}

// GetLeaseReservationAmount returns the reservation amount for a lease.
// It uses the stored MinLeaseDurationAtCreation for consistency with the original reservation.
// For legacy leases without stored duration, it falls back to the current minLeaseDuration param.
func GetLeaseReservationAmount(lease *Lease, minLeaseDuration uint64) (sdk.Coins, error) {
	// Use stored duration if available (preferred - consistent with creation)
	duration := lease.MinLeaseDurationAtCreation
	if duration == 0 {
		// Fallback for legacy leases created before duration storage was added
		duration = minLeaseDuration
	}

	return CalculateLeaseReservation(lease.Items, duration)
}

// CheckReservationRelease checks if releasing a reservation would cause underflow.
// Returns a map of denoms that would underflow and the amount of underflow for each.
// An empty map indicates the release is safe with no underflow.
// This is useful for observability/logging at the keeper level.
func CheckReservationRelease(reserved, toRelease sdk.Coins) map[string]sdkmath.Int {
	underflows := make(map[string]sdkmath.Int)

	for _, coin := range toRelease {
		reservedAmount := reserved.AmountOf(coin.Denom)
		if coin.Amount.GT(reservedAmount) {
			// Would underflow: releasing more than reserved
			underflows[coin.Denom] = coin.Amount.Sub(reservedAmount)
		}
	}

	return underflows
}

// CalculateExpectedReservationsByTenant computes the expected total reservation per tenant
// from a list of leases. Only PENDING and ACTIVE leases contribute to reservations.
// This is useful for genesis validation and debugging/testing.
func CalculateExpectedReservationsByTenant(leases []Lease, fallbackMinLeaseDuration uint64) (map[string]sdk.Coins, error) {
	expected := make(map[string]sdk.Coins)

	for i := range leases {
		lease := &leases[i]
		if lease.State == LEASE_STATE_PENDING || lease.State == LEASE_STATE_ACTIVE {
			reservation, err := GetLeaseReservationAmount(lease, fallbackMinLeaseDuration)
			if err != nil {
				return nil, errorsmod.Wrapf(err, "calculate reservation for lease %s", lease.Uuid)
			}
			if existing, ok := expected[lease.Tenant]; ok {
				expected[lease.Tenant], err = SafeAddCoins(existing, reservation)
				if err != nil {
					return nil, errorsmod.Wrapf(err, "sum reservations for tenant %s", lease.Tenant)
				}
			} else {
				expected[lease.Tenant] = reservation
			}
		}
	}

	return expected, nil
}
