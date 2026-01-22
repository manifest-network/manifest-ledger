package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// ============================================================================
// GetAvailableCredit Tests
// ============================================================================

func TestGetAvailableCredit(t *testing.T) {
	tests := []struct {
		name     string
		balance  sdk.Coins
		reserved sdk.Coins
		expected sdk.Coins
	}{
		{
			name:     "no reservations - full balance available",
			balance:  sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000))),
			reserved: sdk.NewCoins(),
			expected: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000))),
		},
		{
			name:     "partial reservation - remaining available",
			balance:  sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000))),
			reserved: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(300))),
			expected: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(700))),
		},
		{
			name:     "full reservation - zero available",
			balance:  sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000))),
			reserved: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000))),
			expected: sdk.NewCoins(),
		},
		{
			name:     "over reservation (shouldn't happen) - zero available",
			balance:  sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000))),
			reserved: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1500))),
			expected: sdk.NewCoins(),
		},
		{
			name: "multi-denom - different availability",
			balance: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(1000)),
				sdk.NewCoin("uatom", math.NewInt(500)),
			),
			reserved: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(300)),
				sdk.NewCoin("uatom", math.NewInt(100)),
			),
			expected: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(700)),
				sdk.NewCoin("uatom", math.NewInt(400)),
			),
		},
		{
			name: "reserved denom not in balance - ignored",
			balance: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(1000)),
			),
			reserved: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(300)),
				sdk.NewCoin("uatom", math.NewInt(100)), // Not in balance
			),
			expected: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(700)),
			),
		},
		{
			name: "balance denom not reserved - full availability",
			balance: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(1000)),
				sdk.NewCoin("uatom", math.NewInt(500)), // Not reserved
			),
			reserved: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(300)),
			),
			expected: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(700)),
				sdk.NewCoin("uatom", math.NewInt(500)),
			),
		},
		{
			name:     "empty balance - nothing available",
			balance:  sdk.NewCoins(),
			reserved: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			expected: sdk.NewCoins(),
		},
		{
			name:     "nil coins - empty result",
			balance:  nil,
			reserved: nil,
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := types.GetAvailableCredit(tc.balance, tc.reserved)
			require.Equal(t, tc.expected, result)
		})
	}
}

// ============================================================================
// AddReservation Tests
// ============================================================================

func TestAddReservation(t *testing.T) {
	tests := []struct {
		name     string
		reserved sdk.Coins
		toAdd    sdk.Coins
		expected sdk.Coins
	}{
		{
			name:     "add to empty reservation",
			reserved: sdk.NewCoins(),
			toAdd:    sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			expected: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
		},
		{
			name:     "add to existing reservation - same denom",
			reserved: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			toAdd:    sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(50))),
			expected: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(150))),
		},
		{
			name:     "add different denom",
			reserved: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			toAdd:    sdk.NewCoins(sdk.NewCoin("uatom", math.NewInt(50))),
			expected: sdk.NewCoins(sdk.NewCoin("uatom", math.NewInt(50)), sdk.NewCoin("upwr", math.NewInt(100))),
		},
		{
			name: "add multi-denom",
			reserved: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(100)),
			),
			toAdd: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(50)),
				sdk.NewCoin("uatom", math.NewInt(30)),
			),
			expected: sdk.NewCoins(
				sdk.NewCoin("uatom", math.NewInt(30)),
				sdk.NewCoin("upwr", math.NewInt(150)),
			),
		},
		{
			name:     "add nothing",
			reserved: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			toAdd:    sdk.NewCoins(),
			expected: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := types.AddReservation(tc.reserved, tc.toAdd)
			require.Equal(t, tc.expected, result)
		})
	}
}

// ============================================================================
// SubtractReservation Tests
// ============================================================================

func TestSubtractReservation(t *testing.T) {
	tests := []struct {
		name       string
		reserved   sdk.Coins
		toSubtract sdk.Coins
		expected   sdk.Coins
	}{
		{
			name:       "subtract from existing reservation",
			reserved:   sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			toSubtract: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(30))),
			expected:   sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(70))),
		},
		{
			name:       "subtract entire reservation",
			reserved:   sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			toSubtract: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			expected:   sdk.NewCoins(),
		},
		{
			name:       "subtract more than reserved - capped at zero",
			reserved:   sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			toSubtract: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(150))),
			expected:   sdk.NewCoins(),
		},
		{
			name:       "subtract non-existent denom - no change",
			reserved:   sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			toSubtract: sdk.NewCoins(sdk.NewCoin("uatom", math.NewInt(50))),
			expected:   sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
		},
		{
			name: "subtract from multi-denom",
			reserved: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(100)),
				sdk.NewCoin("uatom", math.NewInt(50)),
			),
			toSubtract: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(30)),
				sdk.NewCoin("uatom", math.NewInt(50)),
			),
			expected: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(70)),
			),
		},
		{
			name:       "subtract from empty - no panic",
			reserved:   sdk.NewCoins(),
			toSubtract: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(50))),
			expected:   sdk.NewCoins(),
		},
		{
			name:       "subtract nothing",
			reserved:   sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			toSubtract: sdk.NewCoins(),
			expected:   sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := types.SubtractReservation(tc.reserved, tc.toSubtract)
			require.Equal(t, tc.expected, result)
		})
	}
}

// ============================================================================
// CalculateLeaseReservation Tests
// ============================================================================

func TestCalculateLeaseReservation(t *testing.T) {
	tests := []struct {
		name             string
		items            []types.LeaseItem
		minLeaseDuration uint64
		expected         sdk.Coins
	}{
		{
			name: "single item - simple calculation",
			items: []types.LeaseItem{
				{
					SkuUuid:     "01912345-6789-7abc-8def-0123456789ab",
					Quantity:    1,
					LockedPrice: sdk.NewCoin("upwr", math.NewInt(10)), // 10 per second
				},
			},
			minLeaseDuration: 3600,                                                  // 1 hour
			expected:         sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(36000))), // 10 * 1 * 3600
		},
		{
			name: "single item - multiple quantity",
			items: []types.LeaseItem{
				{
					SkuUuid:     "01912345-6789-7abc-8def-0123456789ab",
					Quantity:    5,
					LockedPrice: sdk.NewCoin("upwr", math.NewInt(10)), // 10 per second per unit
				},
			},
			minLeaseDuration: 3600,                                                   // 1 hour
			expected:         sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(180000))), // 10 * 5 * 3600
		},
		{
			name: "multiple items - same denom",
			items: []types.LeaseItem{
				{
					SkuUuid:     "01912345-6789-7abc-8def-0123456789ab",
					Quantity:    2,
					LockedPrice: sdk.NewCoin("upwr", math.NewInt(10)),
				},
				{
					SkuUuid:     "01912345-6789-7abc-8def-0123456789ac",
					Quantity:    3,
					LockedPrice: sdk.NewCoin("upwr", math.NewInt(20)),
				},
			},
			minLeaseDuration: 3600,
			expected:         sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(288000))), // (10*2 + 20*3) * 3600 = 80 * 3600
		},
		{
			name: "multiple items - different denoms",
			items: []types.LeaseItem{
				{
					SkuUuid:     "01912345-6789-7abc-8def-0123456789ab",
					Quantity:    1,
					LockedPrice: sdk.NewCoin("upwr", math.NewInt(10)),
				},
				{
					SkuUuid:     "01912345-6789-7abc-8def-0123456789ac",
					Quantity:    2,
					LockedPrice: sdk.NewCoin("uatom", math.NewInt(5)),
				},
			},
			minLeaseDuration: 3600,
			expected: sdk.NewCoins(
				sdk.NewCoin("uatom", math.NewInt(36000)), // 5 * 2 * 3600
				sdk.NewCoin("upwr", math.NewInt(36000)),  // 10 * 1 * 3600
			),
		},
		{
			name:             "empty items - zero reservation",
			items:            []types.LeaseItem{},
			minLeaseDuration: 3600,
			expected:         sdk.NewCoins(),
		},
		{
			name: "zero duration - zero reservation",
			items: []types.LeaseItem{
				{
					SkuUuid:     "01912345-6789-7abc-8def-0123456789ab",
					Quantity:    1,
					LockedPrice: sdk.NewCoin("upwr", math.NewInt(10)),
				},
			},
			minLeaseDuration: 0,
			expected:         sdk.NewCoins(),
		},
		{
			name:             "nil items - zero reservation",
			items:            nil,
			minLeaseDuration: 3600,
			expected:         sdk.NewCoins(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := types.CalculateLeaseReservation(tc.items, tc.minLeaseDuration)
			require.Equal(t, tc.expected, result)
		})
	}
}

// ============================================================================
// CalculateLeaseReservationFromRates Tests
// ============================================================================

func TestCalculateLeaseReservationFromRates(t *testing.T) {
	tests := []struct {
		name                string
		totalRatesPerSecond sdk.Coins
		minLeaseDuration    uint64
		expected            sdk.Coins
	}{
		{
			name:                "single denom rate",
			totalRatesPerSecond: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			minLeaseDuration:    3600,
			expected:            sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(360000))),
		},
		{
			name: "multi denom rates",
			totalRatesPerSecond: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(100)),
				sdk.NewCoin("uatom", math.NewInt(50)),
			),
			minLeaseDuration: 3600,
			expected: sdk.NewCoins(
				sdk.NewCoin("uatom", math.NewInt(180000)),
				sdk.NewCoin("upwr", math.NewInt(360000)),
			),
		},
		{
			name:                "zero rates - zero reservation",
			totalRatesPerSecond: sdk.NewCoins(),
			minLeaseDuration:    3600,
			expected:            sdk.NewCoins(),
		},
		{
			name:                "zero duration - zero reservation",
			totalRatesPerSecond: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			minLeaseDuration:    0,
			expected:            sdk.NewCoins(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := types.CalculateLeaseReservationFromRates(tc.totalRatesPerSecond, tc.minLeaseDuration)
			require.Equal(t, tc.expected, result)
		})
	}
}

// ============================================================================
// Integration Scenarios
// ============================================================================

func TestReservationScenario_PreventOverbooking(t *testing.T) {
	// Scenario: Tenant has 100 credits, min_lease_duration = 1 second (for simplicity)
	// Each lease reserves 30 credits
	// Lease A: 30 credits reserved
	// Lease B: 30 credits reserved
	// Lease C: 30 credits reserved
	// Lease D: 30 credits needed but only 10 available (should fail)

	balance := sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100)))

	// Use 30 credit per second rate, 1 second duration = 30 reservation each
	ratePerSecond := math.NewInt(30)
	duration := uint64(1) // 1 second for simplicity

	reserved := sdk.NewCoins()

	// Lease A creation: need 30, have 100 available
	leaseAItems := []types.LeaseItem{{SkuUuid: "a", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", ratePerSecond)}}
	leaseAReservation := types.CalculateLeaseReservation(leaseAItems, duration)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(30))), leaseAReservation)

	available := types.GetAvailableCredit(balance, reserved)
	require.True(t, available.AmountOf("upwr").GTE(leaseAReservation.AmountOf("upwr")))
	reserved = types.AddReservation(reserved, leaseAReservation)
	// Reserved: 30, Available: 70

	// Lease B creation: need 30, have 70 available
	leaseBItems := []types.LeaseItem{{SkuUuid: "b", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", ratePerSecond)}}
	leaseBReservation := types.CalculateLeaseReservation(leaseBItems, duration)

	available = types.GetAvailableCredit(balance, reserved)
	require.Equal(t, math.NewInt(70), available.AmountOf("upwr"))
	require.True(t, available.AmountOf("upwr").GTE(leaseBReservation.AmountOf("upwr")))
	reserved = types.AddReservation(reserved, leaseBReservation)
	// Reserved: 60, Available: 40

	// Lease C creation: need 30, have 40 available
	leaseCItems := []types.LeaseItem{{SkuUuid: "c", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", ratePerSecond)}}
	leaseCReservation := types.CalculateLeaseReservation(leaseCItems, duration)

	available = types.GetAvailableCredit(balance, reserved)
	require.Equal(t, math.NewInt(40), available.AmountOf("upwr"))
	require.True(t, available.AmountOf("upwr").GTE(leaseCReservation.AmountOf("upwr")))
	reserved = types.AddReservation(reserved, leaseCReservation)
	// Reserved: 90, Available: 10

	// Lease D creation: need 30, have 10 available - SHOULD FAIL
	leaseDItems := []types.LeaseItem{{SkuUuid: "d", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", ratePerSecond)}}
	leaseDReservation := types.CalculateLeaseReservation(leaseDItems, duration)

	available = types.GetAvailableCredit(balance, reserved)
	require.Equal(t, math.NewInt(10), available.AmountOf("upwr"))
	require.False(t, available.AmountOf("upwr").GTE(leaseDReservation.AmountOf("upwr")))
	// Lease D would be rejected due to insufficient available credit
}

func TestReservationScenario_ReleaseOnClose(t *testing.T) {
	// Scenario: After closing a lease, its reservation should be released
	balance := sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100)))
	ratePerSecond := math.NewInt(40)
	duration := uint64(1)

	reserved := sdk.NewCoins()

	// Lease A creation: reserve 40
	leaseAItems := []types.LeaseItem{{SkuUuid: "a", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", ratePerSecond)}}
	leaseAReservation := types.CalculateLeaseReservation(leaseAItems, duration)
	reserved = types.AddReservation(reserved, leaseAReservation)

	// Lease B creation: reserve 40 (total reserved: 80, available: 20)
	leaseBItems := []types.LeaseItem{{SkuUuid: "b", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", ratePerSecond)}}
	leaseBReservation := types.CalculateLeaseReservation(leaseBItems, duration)
	reserved = types.AddReservation(reserved, leaseBReservation)

	available := types.GetAvailableCredit(balance, reserved)
	require.Equal(t, math.NewInt(20), available.AmountOf("upwr"))

	// Lease C would fail: need 40, only 20 available
	leaseCItems := []types.LeaseItem{{SkuUuid: "c", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", ratePerSecond)}}
	leaseCReservation := types.CalculateLeaseReservation(leaseCItems, duration)
	require.False(t, available.AmountOf("upwr").GTE(leaseCReservation.AmountOf("upwr")))

	// Close Lease A: release its reservation
	reserved = types.SubtractReservation(reserved, leaseAReservation)

	// Now available: 100 - 40 = 60
	available = types.GetAvailableCredit(balance, reserved)
	require.Equal(t, math.NewInt(60), available.AmountOf("upwr"))

	// Lease C can now be created: need 40, have 60 available
	require.True(t, available.AmountOf("upwr").GTE(leaseCReservation.AmountOf("upwr")))
}

// ============================================================================
// GetLeaseReservationAmount Tests
// ============================================================================

func TestGetLeaseReservationAmount(t *testing.T) {
	currentMinDuration := uint64(3600)
	ratePerSecond := math.NewInt(10)

	tests := []struct {
		name           string
		lease          types.Lease
		minLeaseDur    uint64
		expectedAmount math.Int
		expectedDenom  string
	}{
		{
			name: "uses stored min_lease_duration when available",
			lease: types.Lease{
				Items: []types.LeaseItem{
					{SkuUuid: "sku1", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", ratePerSecond)},
				},
				// Stored at creation with different min_lease_duration (7200)
				MinLeaseDurationAtCreation: 7200,
			},
			minLeaseDur:    currentMinDuration, // Current param is 3600, but stored is 7200
			expectedAmount: math.NewInt(72000), // 10 * 7200 = 72000 (uses stored)
			expectedDenom:  "upwr",
		},
		{
			name: "falls back to current param for legacy lease without stored duration",
			lease: types.Lease{
				Items: []types.LeaseItem{
					{SkuUuid: "sku1", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", ratePerSecond)},
				},
				MinLeaseDurationAtCreation: 0, // Zero - legacy lease
			},
			minLeaseDur:    currentMinDuration,
			expectedAmount: math.NewInt(36000), // 10 * 3600 = 36000 (uses current)
			expectedDenom:  "upwr",
		},
		{
			name: "multi-item lease with stored duration",
			lease: types.Lease{
				Items: []types.LeaseItem{
					{SkuUuid: "sku1", Quantity: 2, LockedPrice: sdk.NewCoin("upwr", ratePerSecond)},
					{SkuUuid: "sku2", Quantity: 1, LockedPrice: sdk.NewCoin("umfx", math.NewInt(5))},
				},
				MinLeaseDurationAtCreation: 3600,
			},
			minLeaseDur:    currentMinDuration,
			expectedAmount: math.NewInt(72000), // (10 * 2) * 3600 = 72000 for upwr
			expectedDenom:  "upwr",
		},
		{
			name: "multi-denom calculation",
			lease: types.Lease{
				Items: []types.LeaseItem{
					{SkuUuid: "sku1", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", ratePerSecond)},
					{SkuUuid: "sku2", Quantity: 1, LockedPrice: sdk.NewCoin("umfx", math.NewInt(5))},
				},
				MinLeaseDurationAtCreation: 3600,
			},
			minLeaseDur:    currentMinDuration,
			expectedAmount: math.NewInt(18000), // 5 * 1 * 3600 = 18000 for umfx
			expectedDenom:  "umfx",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := types.GetLeaseReservationAmount(&tc.lease, tc.minLeaseDur)
			require.Equal(t, tc.expectedAmount, result.AmountOf(tc.expectedDenom))
		})
	}
}

// TestGetLeaseReservationAmount_ParamChangeScenario tests the fix for M-01 vulnerability.
// Ensures that parameter changes don't cause inconsistent reservation accounting.
func TestGetLeaseReservationAmount_ParamChangeScenario(t *testing.T) {
	// Scenario: MinLeaseDuration changes between lease creation and closure
	ratePerSecond := math.NewInt(10)

	// At creation time: MinLeaseDuration = 3600
	originalMinDuration := uint64(3600)
	items := []types.LeaseItem{
		{SkuUuid: "sku1", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", ratePerSecond)},
	}

	// Calculate expected reservation at creation
	reservationAtCreation := types.CalculateLeaseReservation(items, originalMinDuration)
	require.Equal(t, math.NewInt(36000), reservationAtCreation.AmountOf("upwr")) // 10 * 3600

	// Create lease with stored min_lease_duration
	lease := types.Lease{
		Items:                      items,
		MinLeaseDurationAtCreation: originalMinDuration, // Store the duration, not the calculated amount
	}

	// Governance changes MinLeaseDuration to 1800
	newMinDuration := uint64(1800)

	// At closure time: GetLeaseReservationAmount should use STORED duration
	releaseAmount := types.GetLeaseReservationAmount(&lease, newMinDuration)

	// Should calculate using stored duration (3600), not current (1800)
	require.Equal(t, math.NewInt(36000), releaseAmount.AmountOf("upwr"))

	// Verify that using current param would give wrong answer
	wrongAmount := types.CalculateLeaseReservation(items, newMinDuration)
	require.Equal(t, math.NewInt(18000), wrongAmount.AmountOf("upwr")) // 10 * 1800 = wrong!

	// The stored duration ensures correct release
	require.NotEqual(t, releaseAmount, wrongAmount)
}

// ============================================================================
// ReleaseLeaseReservation Tests
// ============================================================================

func TestReleaseLeaseReservation(t *testing.T) {
	tests := []struct {
		name                       string
		initialReserved            sdk.Coins
		leaseItems                 []types.LeaseItem
		minLeaseDurationAtCreation uint64
		currentMinDuration         uint64
		expectedReserved           sdk.Coins
	}{
		{
			name:            "release single item reservation",
			initialReserved: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(36000))),
			leaseItems: []types.LeaseItem{
				{SkuUuid: "sku1", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", math.NewInt(10))},
			},
			minLeaseDurationAtCreation: 3600,
			currentMinDuration:         3600,
			expectedReserved:           sdk.NewCoins(),
		},
		{
			name:            "release partial reservation",
			initialReserved: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(72000))), // Two leases worth
			leaseItems: []types.LeaseItem{
				{SkuUuid: "sku1", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", math.NewInt(10))},
			},
			minLeaseDurationAtCreation: 3600,
			currentMinDuration:         3600,
			expectedReserved:           sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(36000))), // One lease remaining
		},
		{
			name:            "release uses stored duration, not current param",
			initialReserved: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(36000))), // Reserved at creation
			leaseItems: []types.LeaseItem{
				{SkuUuid: "sku1", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", math.NewInt(10))},
			},
			minLeaseDurationAtCreation: 3600, // Was 3600 at creation
			currentMinDuration:         1800, // Now 1800 - should be ignored
			expectedReserved:           sdk.NewCoins(),
		},
		{
			name: "release multi-denom reservation",
			initialReserved: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(36000)),
				sdk.NewCoin("uatom", math.NewInt(72000)),
			),
			leaseItems: []types.LeaseItem{
				{SkuUuid: "sku1", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", math.NewInt(10))},
				{SkuUuid: "sku2", Quantity: 2, LockedPrice: sdk.NewCoin("uatom", math.NewInt(10))},
			},
			minLeaseDurationAtCreation: 3600,
			currentMinDuration:         3600,
			expectedReserved:           sdk.NewCoins(),
		},
		{
			name:            "release with multiple quantity",
			initialReserved: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(180000))), // 10 * 5 * 3600
			leaseItems: []types.LeaseItem{
				{SkuUuid: "sku1", Quantity: 5, LockedPrice: sdk.NewCoin("upwr", math.NewInt(10))},
			},
			minLeaseDurationAtCreation: 3600,
			currentMinDuration:         3600,
			expectedReserved:           sdk.NewCoins(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			creditAccount := &types.CreditAccount{
				Tenant:          "manifest1tenant",
				CreditAddress:   "manifest1credit",
				ReservedAmounts: tc.initialReserved,
			}

			lease := &types.Lease{
				Uuid:                       "lease-uuid",
				Tenant:                     "manifest1tenant",
				Items:                      tc.leaseItems,
				MinLeaseDurationAtCreation: tc.minLeaseDurationAtCreation,
			}

			// Call the helper function
			types.ReleaseLeaseReservation(creditAccount, lease, tc.currentMinDuration)

			// Verify the credit account's reserved amounts were updated
			require.True(t, tc.expectedReserved.Equal(creditAccount.ReservedAmounts),
				"expected %s, got %s", tc.expectedReserved.String(), creditAccount.ReservedAmounts.String())
		})
	}
}

// ============================================================================
// CheckReservationRelease Tests
// ============================================================================

func TestCheckReservationRelease(t *testing.T) {
	tests := []struct {
		name              string
		reserved          sdk.Coins
		toRelease         sdk.Coins
		expectedUnderflow map[string]math.Int
	}{
		{
			name:              "no underflow - exact match",
			reserved:          sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000))),
			toRelease:         sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000))),
			expectedUnderflow: map[string]math.Int{},
		},
		{
			name:              "no underflow - releasing less than reserved",
			reserved:          sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000))),
			toRelease:         sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(500))),
			expectedUnderflow: map[string]math.Int{},
		},
		{
			name:              "underflow - releasing more than reserved",
			reserved:          sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(500))),
			toRelease:         sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000))),
			expectedUnderflow: map[string]math.Int{"upwr": math.NewInt(500)},
		},
		{
			name:              "underflow - denom not in reserved",
			reserved:          sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000))),
			toRelease:         sdk.NewCoins(sdk.NewCoin("uatom", math.NewInt(500))),
			expectedUnderflow: map[string]math.Int{"uatom": math.NewInt(500)},
		},
		{
			name:     "multi-denom - partial underflow",
			reserved: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000)), sdk.NewCoin("uatom", math.NewInt(200))),
			toRelease: sdk.NewCoins(
				sdk.NewCoin("upwr", math.NewInt(500)),  // OK
				sdk.NewCoin("uatom", math.NewInt(500)), // Underflow by 300
			),
			expectedUnderflow: map[string]math.Int{"uatom": math.NewInt(300)},
		},
		{
			name:              "empty reserved - any release is underflow",
			reserved:          sdk.NewCoins(),
			toRelease:         sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(100))),
			expectedUnderflow: map[string]math.Int{"upwr": math.NewInt(100)},
		},
		{
			name:              "empty release - no underflow",
			reserved:          sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(1000))),
			toRelease:         sdk.NewCoins(),
			expectedUnderflow: map[string]math.Int{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := types.CheckReservationRelease(tc.reserved, tc.toRelease)

			require.Equal(t, len(tc.expectedUnderflow), len(result),
				"expected %d underflows, got %d", len(tc.expectedUnderflow), len(result))

			for denom, expectedAmount := range tc.expectedUnderflow {
				actualAmount, ok := result[denom]
				require.True(t, ok, "expected underflow for denom %s", denom)
				require.True(t, expectedAmount.Equal(actualAmount),
					"denom %s: expected underflow %s, got %s", denom, expectedAmount.String(), actualAmount.String())
			}
		})
	}
}

// ============================================================================
// CalculateExpectedReservationsByTenant Tests
// ============================================================================

func TestCalculateExpectedReservationsByTenant(t *testing.T) {
	tenant1 := "manifest1tenant1"
	tenant2 := "manifest1tenant2"
	minDuration := uint64(3600)

	tests := []struct {
		name     string
		leases   []types.Lease
		expected map[string]sdk.Coins
	}{
		{
			name:     "empty leases",
			leases:   []types.Lease{},
			expected: map[string]sdk.Coins{},
		},
		{
			name: "single active lease",
			leases: []types.Lease{
				{
					Uuid:   "lease1",
					Tenant: tenant1,
					State:  types.LEASE_STATE_ACTIVE,
					Items: []types.LeaseItem{
						{SkuUuid: "sku1", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", math.NewInt(10))},
					},
				},
			},
			expected: map[string]sdk.Coins{
				tenant1: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(36000))), // 10 * 1 * 3600
			},
		},
		{
			name: "single pending lease",
			leases: []types.Lease{
				{
					Uuid:   "lease1",
					Tenant: tenant1,
					State:  types.LEASE_STATE_PENDING,
					Items: []types.LeaseItem{
						{SkuUuid: "sku1", Quantity: 2, LockedPrice: sdk.NewCoin("upwr", math.NewInt(10))},
					},
				},
			},
			expected: map[string]sdk.Coins{
				tenant1: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(72000))), // 10 * 2 * 3600
			},
		},
		{
			name: "closed lease - no reservation",
			leases: []types.Lease{
				{
					Uuid:   "lease1",
					Tenant: tenant1,
					State:  types.LEASE_STATE_CLOSED,
					Items: []types.LeaseItem{
						{SkuUuid: "sku1", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", math.NewInt(10))},
					},
				},
			},
			expected: map[string]sdk.Coins{},
		},
		{
			name: "rejected lease - no reservation",
			leases: []types.Lease{
				{
					Uuid:   "lease1",
					Tenant: tenant1,
					State:  types.LEASE_STATE_REJECTED,
					Items: []types.LeaseItem{
						{SkuUuid: "sku1", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", math.NewInt(10))},
					},
				},
			},
			expected: map[string]sdk.Coins{},
		},
		{
			name: "multiple leases same tenant",
			leases: []types.Lease{
				{
					Uuid:   "lease1",
					Tenant: tenant1,
					State:  types.LEASE_STATE_ACTIVE,
					Items: []types.LeaseItem{
						{SkuUuid: "sku1", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", math.NewInt(10))},
					},
				},
				{
					Uuid:   "lease2",
					Tenant: tenant1,
					State:  types.LEASE_STATE_PENDING,
					Items: []types.LeaseItem{
						{SkuUuid: "sku2", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", math.NewInt(20))},
					},
				},
			},
			expected: map[string]sdk.Coins{
				tenant1: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(108000))), // (10 + 20) * 1 * 3600
			},
		},
		{
			name: "multiple tenants",
			leases: []types.Lease{
				{
					Uuid:   "lease1",
					Tenant: tenant1,
					State:  types.LEASE_STATE_ACTIVE,
					Items: []types.LeaseItem{
						{SkuUuid: "sku1", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", math.NewInt(10))},
					},
				},
				{
					Uuid:   "lease2",
					Tenant: tenant2,
					State:  types.LEASE_STATE_ACTIVE,
					Items: []types.LeaseItem{
						{SkuUuid: "sku2", Quantity: 2, LockedPrice: sdk.NewCoin("upwr", math.NewInt(15))},
					},
				},
			},
			expected: map[string]sdk.Coins{
				tenant1: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(36000))),  // 10 * 1 * 3600
				tenant2: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(108000))), // 15 * 2 * 3600
			},
		},
		{
			name: "lease with stored min_lease_duration_at_creation",
			leases: []types.Lease{
				{
					Uuid:   "lease1",
					Tenant: tenant1,
					State:  types.LEASE_STATE_ACTIVE,
					Items: []types.LeaseItem{
						{SkuUuid: "sku1", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", math.NewInt(10))},
					},
					MinLeaseDurationAtCreation: 7200, // Override default 3600
				},
			},
			expected: map[string]sdk.Coins{
				tenant1: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(72000))), // 10 * 1 * 7200
			},
		},
		{
			name: "mixed states - only pending and active count",
			leases: []types.Lease{
				{
					Uuid: "lease1", Tenant: tenant1, State: types.LEASE_STATE_PENDING,
					Items: []types.LeaseItem{{SkuUuid: "sku1", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", math.NewInt(10))}},
				},
				{
					Uuid: "lease2", Tenant: tenant1, State: types.LEASE_STATE_ACTIVE,
					Items: []types.LeaseItem{{SkuUuid: "sku2", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", math.NewInt(10))}},
				},
				{
					Uuid: "lease3", Tenant: tenant1, State: types.LEASE_STATE_CLOSED,
					Items: []types.LeaseItem{{SkuUuid: "sku3", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", math.NewInt(10))}},
				},
				{
					Uuid: "lease4", Tenant: tenant1, State: types.LEASE_STATE_REJECTED,
					Items: []types.LeaseItem{{SkuUuid: "sku4", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", math.NewInt(10))}},
				},
				{
					Uuid: "lease5", Tenant: tenant1, State: types.LEASE_STATE_EXPIRED,
					Items: []types.LeaseItem{{SkuUuid: "sku5", Quantity: 1, LockedPrice: sdk.NewCoin("upwr", math.NewInt(10))}},
				},
			},
			expected: map[string]sdk.Coins{
				tenant1: sdk.NewCoins(sdk.NewCoin("upwr", math.NewInt(72000))), // Only 2 leases: (10 + 10) * 1 * 3600
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := types.CalculateExpectedReservationsByTenant(tc.leases, minDuration)

			require.Equal(t, len(tc.expected), len(result),
				"expected %d tenants, got %d", len(tc.expected), len(result))

			for tenant, expectedCoins := range tc.expected {
				actualCoins, ok := result[tenant]
				require.True(t, ok, "expected tenant %s in result", tenant)
				require.True(t, expectedCoins.Equal(actualCoins),
					"tenant %s: expected %s, got %s", tenant, expectedCoins.String(), actualCoins.String())
			}
		})
	}
}
