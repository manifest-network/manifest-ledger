package types

// Credit Reservation System
//
// The credit reservation system prevents overbooking by tracking a tenant-wide
// aggregate together with the consumable tranche owned by each modern lease.
// This ensures that settlement for one lease cannot spend credit reserved for
// another lease.
//
// # Invariant
//
// In v4 state, the following invariant must hold for every tenant:
//
//	CreditAccount.ReservedAmounts ==
//	    SUM(Lease.Reservation.RemainingAmounts for live modern leases) +
//	    CreditAccount.UnattributedReservedAmounts
//
// CreditAccount.UnattributedLeaseCount must equal the number of live legacy
// leases sharing that account-owned cohort, including when the cohort is empty.
//
// A modern lease has a non-zero MinLeaseDurationAtCreation and exclusively owns
// its remaining tranche. A legacy lease has a zero creation-time duration and
// owns no attributed tranche; live legacy leases share only the account's
// UnattributedReservedAmounts compatibility cohort. Terminal leases and legacy
// leases must keep an initialized but empty Lease.Reservation.
//
// # Reservation Lifecycle
//
//   - ADDED: When a lease is created (enters PENDING state)
//   - MAINTAINED: When a lease is acknowledged (transitions to ACTIVE state)
//   - CONSUMED: Successful settlement reduces both the lease tranche and account aggregate
//   - RELEASED: A terminal transition releases the modern lease's exact remaining tranche
//   - DEFERRED: Each legacy termination decrements UnattributedLeaseCount; the cohort is released at zero
//
// # Available Credit Calculation
//
//	AvailableCredit = CreditBalance - ReservedAmounts
//
// New leases can only be created if AvailableCredit >= NewLeaseReservation for all denoms.
// A lease's settlement spendable amount is its own remaining tranche plus
// genuinely unreserved credit; every other attributed tranche and the legacy
// unattributed cohort remain protected.
//
// # Parameter Change Protection
//
// New leases store MinLeaseDurationAtCreation and their exact reservation at
// creation. Parameter changes therefore do not alter existing tranches. The
// v3→v4 migration attributes only provably bank-backed credit to modern leases;
// any live legacy share remains explicit in UnattributedReservedAmounts and its
// membership in UnattributedLeaseCount. Runtime settlement and release consume
// these stored values and never recompute them from the current parameter.

import (
	"crypto/sha256"
	"slices"

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
	reservedIndex := 0
	for _, coin := range balance {
		for reservedIndex < len(reserved) && reserved[reservedIndex].Denom < coin.Denom {
			reservedIndex++
		}
		reservedAmount := sdkmath.ZeroInt()
		if reservedIndex < len(reserved) && reserved[reservedIndex].Denom == coin.Denom {
			reservedAmount = reserved[reservedIndex].Amount
		}
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
	subtractIndex := 0
	for _, coin := range reserved {
		for subtractIndex < len(toSubtract) && toSubtract[subtractIndex].Denom < coin.Denom {
			subtractIndex++
		}
		subtractAmount := sdkmath.ZeroInt()
		if subtractIndex < len(toSubtract) && toSubtract[subtractIndex].Denom == coin.Denom {
			subtractAmount = toSubtract[subtractIndex].Amount
		}
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

	reservedIndex := 0
	for _, coin := range toRelease {
		for reservedIndex < len(reserved) && reserved[reservedIndex].Denom < coin.Denom {
			reservedIndex++
		}
		reservedAmount := sdkmath.ZeroInt()
		if reservedIndex < len(reserved) && reserved[reservedIndex].Denom == coin.Denom {
			reservedAmount = reserved[reservedIndex].Amount
		}
		if coin.Amount.GT(reservedAmount) {
			// Would underflow: releasing more than reserved
			underflows[coin.Denom] = coin.Amount.Sub(reservedAmount)
		}
	}

	return underflows
}

// CalculateAttributedReservationsByTenant sums the attributed remaining
// reservations of live modern leases by canonical tenant identity. It validates
// the v4 lease-side invariant: every lease has an initialized Reservation, and
// terminal or legacy leases have an empty attributed tranche. The returned
// totals deliberately exclude CreditAccount.UnattributedReservedAmounts; callers
// checking the full account invariant must add that account-owned cohort.
func CalculateAttributedReservationsByTenant(leases []Lease) (map[string]sdk.Coins, error) {
	reservationCoins := make(map[string][]sdk.Coin)
	tenantOrder := make([]string, 0)
	seenTenants := make(map[string]struct{})

	for i := range leases {
		lease := &leases[i]
		tenant, err := sdk.AccAddressFromBech32(lease.Tenant)
		if err != nil {
			return nil, ErrReservationInvariant.Wrapf(
				"lease %s has invalid tenant address: %s",
				lease.Uuid, err,
			)
		}
		tenantKey := tenant.String()

		if lease.Reservation == nil {
			return nil, ErrReservationInvariant.Wrapf(
				"lease %s has no initialized reservation",
				lease.Uuid,
			)
		}
		remaining, err := SafeAddCoins(sdk.NewCoins(), lease.Reservation.RemainingAmounts)
		if err != nil {
			return nil, ErrReservationInvariant.Wrapf(
				"lease %s has invalid remaining reservation: %s",
				lease.Uuid, err,
			)
		}

		live := lease.State == LEASE_STATE_PENDING || lease.State == LEASE_STATE_ACTIVE
		if lease.MinLeaseDurationAtCreation == 0 {
			if !remaining.IsZero() {
				return nil, ErrReservationInvariant.Wrapf(
					"legacy lease %s has an attributed reservation",
					lease.Uuid,
				)
			}
			continue
		}
		if !live {
			if !remaining.IsZero() {
				return nil, ErrReservationInvariant.Wrapf(
					"terminal lease %s has a non-empty reservation",
					lease.Uuid,
				)
			}
			continue
		}
		if remaining.IsZero() {
			continue
		}

		if _, seen := seenTenants[tenantKey]; !seen {
			seenTenants[tenantKey] = struct{}{}
			tenantOrder = append(tenantOrder, tenantKey)
		}
		reservationCoins[tenantKey] = append(reservationCoins[tenantKey], remaining...)
	}

	slices.Sort(tenantOrder)
	expected := make(map[string]sdk.Coins, len(tenantOrder))
	for _, tenantKey := range tenantOrder {
		reservation, err := SafeAggregateCoins(reservationCoins[tenantKey])
		if err != nil {
			return nil, errorsmod.Wrapf(err, "sum reservations for tenant %s", tenantKey)
		}
		expected[tenantKey] = reservation
	}

	return expected, nil
}

// CalculateExpectedReservationsByTenant delegates to
// CalculateAttributedReservationsByTenant.
//
// Deprecated: fallbackMinLeaseDuration is retained for source compatibility but
// is ignored. V4 accounting uses stored consumable tranches and never reconstructs
// reservation amounts from pricing or the current parameter.
func CalculateExpectedReservationsByTenant(leases []Lease, _ uint64) (map[string]sdk.Coins, error) {
	return CalculateAttributedReservationsByTenant(leases)
}
