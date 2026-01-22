package types

// Credit Reservation System
//
// The credit reservation system prevents overbooking by tracking reserved amounts
// per tenant. This ensures that when a lease is created, sufficient credit is
// guaranteed for at least min_lease_duration seconds of operation.
//
// # Invariant
//
// The following invariant must always hold for each tenant:
//
//	CreditAccount.ReservedAmounts == SUM(GetLeaseReservationAmount(lease, params.MinLeaseDuration))
//	                                 for all PENDING and ACTIVE leases of the tenant
//
// # Reservation Lifecycle
//
//   - ADDED: When a lease is created (enters PENDING state)
//   - MAINTAINED: When a lease is acknowledged (transitions to ACTIVE state)
//   - RELEASED: When a lease transitions to CLOSED, REJECTED, or EXPIRED
//
// # Available Credit Calculation
//
//	AvailableCredit = CreditBalance - ReservedAmounts
//
// New leases can only be created if AvailableCredit >= NewLeaseReservation for all denoms.
//
// # Parameter Change Protection
//
// Each lease stores MinLeaseDurationAtCreation to ensure consistent reservation
// calculation regardless of subsequent governance changes to the MinLeaseDuration parameter.

import (
	"crypto/sha256"

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
			available = available.Add(sdk.NewCoin(coin.Denom, coin.Amount.Sub(reservedAmount)))
		}
		// If balance <= reserved, available is 0 for that denom (implicitly not added)
	}

	return available
}

// AddReservation adds amounts to reserved (for lease creation).
// Returns the new reserved amounts.
func AddReservation(reserved, toAdd sdk.Coins) sdk.Coins {
	return reserved.Add(toAdd...)
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
			result = result.Add(sdk.NewCoin(coin.Denom, coin.Amount.Sub(subtractAmount)))
		}
		// If amount <= subtractAmount, don't add the coin (effectively zero)
	}

	return result
}

// CalculateLeaseReservation calculates the reservation amount for a lease.
// reservation = sum(rate_per_second * quantity) * min_lease_duration for each denom.
func CalculateLeaseReservation(items []LeaseItem, minLeaseDuration uint64) sdk.Coins {
	if len(items) == 0 || minLeaseDuration == 0 {
		return sdk.NewCoins()
	}

	// Calculate total rates per denom
	totalRates := sdk.NewCoins()
	for _, item := range items {
		// Rate = locked_price * quantity
		itemRate := sdk.NewCoin(
			item.LockedPrice.Denom,
			item.LockedPrice.Amount.Mul(sdkmath.NewIntFromUint64(item.Quantity)),
		)
		totalRates = totalRates.Add(itemRate)
	}

	// Multiply by min_lease_duration to get reservation
	reservation := sdk.NewCoins()
	minDuration := sdkmath.NewIntFromUint64(minLeaseDuration)
	for _, rate := range totalRates {
		reservation = reservation.Add(sdk.NewCoin(rate.Denom, rate.Amount.Mul(minDuration)))
	}

	return reservation
}

// CalculateLeaseReservationFromRates calculates the reservation from pre-computed rates.
// This is useful when total rates are already calculated during lease creation.
func CalculateLeaseReservationFromRates(totalRatesPerSecond sdk.Coins, minLeaseDuration uint64) sdk.Coins {
	if totalRatesPerSecond.IsZero() || minLeaseDuration == 0 {
		return sdk.NewCoins()
	}

	reservation := sdk.NewCoins()
	minDuration := sdkmath.NewIntFromUint64(minLeaseDuration)
	for _, rate := range totalRatesPerSecond {
		reservation = reservation.Add(sdk.NewCoin(rate.Denom, rate.Amount.Mul(minDuration)))
	}

	return reservation
}

// GetLeaseReservationAmount returns the reservation amount for a lease.
// It uses the stored MinLeaseDurationAtCreation for consistency with the original reservation.
// For legacy leases without stored duration, it falls back to the current minLeaseDuration param.
func GetLeaseReservationAmount(lease *Lease, minLeaseDuration uint64) sdk.Coins {
	// Use stored duration if available (preferred - consistent with creation)
	duration := lease.MinLeaseDurationAtCreation
	if duration == 0 {
		// Fallback for legacy leases created before duration storage was added
		duration = minLeaseDuration
	}

	return CalculateLeaseReservation(lease.Items, duration)
}

// ReleaseLeaseReservation releases the reservation for a lease from a credit account.
// This is called when a lease transitions out of PENDING or ACTIVE state (close, reject, cancel, expire).
// The credit account's ReservedAmounts is updated in place.
func ReleaseLeaseReservation(creditAccount *CreditAccount, lease *Lease, minLeaseDuration uint64) {
	reservationAmount := GetLeaseReservationAmount(lease, minLeaseDuration)
	creditAccount.ReservedAmounts = SubtractReservation(creditAccount.ReservedAmounts, reservationAmount)
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
func CalculateExpectedReservationsByTenant(leases []Lease, fallbackMinLeaseDuration uint64) map[string]sdk.Coins {
	expected := make(map[string]sdk.Coins)

	for i := range leases {
		lease := &leases[i]
		if lease.State == LEASE_STATE_PENDING || lease.State == LEASE_STATE_ACTIVE {
			reservation := GetLeaseReservationAmount(lease, fallbackMinLeaseDuration)
			if existing, ok := expected[lease.Tenant]; ok {
				expected[lease.Tenant] = existing.Add(reservation...)
			} else {
				expected[lease.Tenant] = reservation
			}
		}
	}

	return expected
}
