package types_test

import (
	"encoding/binary"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

var propertyDenoms = [...]string{"ualpha", "ubeta", "udelta", "ugamma"}

// Check conservation, protection of unrelated claims, and maximal payment
// directly. These laws do not duplicate the planner's ordered-coin algorithm.
func checkReservationSpendProperties(t *testing.T, data []byte) {
	t.Helper()
	var amounts [16]byte
	copy(amounts[:], data)
	var balance, total, own, charge sdk.Coins
	for i, denom := range propertyDenoms {
		allocation, other := int64(amounts[4*i]), int64(amounts[4*i+1])
		free, due := int64(amounts[4*i+2]), int64(amounts[4*i+3])
		balance = append(balance, sdk.NewInt64Coin(denom, allocation+other+free))
		total = append(total, sdk.NewInt64Coin(denom, allocation+other))
		own = append(own, sdk.NewInt64Coin(denom, allocation))
		charge = append(charge, sdk.NewInt64Coin(denom, due))
	}
	balance, total = sdk.NewCoins(balance...), sdk.NewCoins(total...)
	own, charge = sdk.NewCoins(own...), sdk.NewCoins(charge...)
	snapshot := [...]string{balance.String(), total.String(), own.String(), charge.String()}
	plan, err := types.PlanReservationSpend(balance, total, own, charge)
	require.NoError(t, err)
	require.Equal(t, snapshot, [...]string{balance.String(), total.String(), own.String(), charge.String()})
	for _, coins := range []sdk.Coins{
		plan.Spendable, plan.Transfer, plan.Consumed, plan.BalanceAfter, plan.TotalReservedAfter, plan.AllocationAfter,
	} {
		require.NoError(t, coins.Validate(), "all output coin sets must remain canonical")
	}
	for _, denom := range propertyDenoms {
		paid, consumed := plan.Transfer.AmountOf(denom).Int64(), plan.Consumed.AmountOf(denom).Int64()
		after, reservedAfter := plan.BalanceAfter.AmountOf(denom).Int64(), plan.TotalReservedAfter.AmountOf(denom).Int64()
		ownAfter, other := plan.AllocationAfter.AmountOf(denom).Int64(), total.AmountOf(denom).Int64()-own.AmountOf(denom).Int64()
		require.Equal(t, balance.AmountOf(denom).Int64(), paid+after, "bank conservation")
		require.Equal(t, total.AmountOf(denom).Int64(), consumed+reservedAfter, "aggregate conservation")
		require.Equal(t, own.AmountOf(denom).Int64(), consumed+ownAfter, "lease conservation")
		require.Equal(t, other, reservedAfter-ownAfter, "unrelated claims are untouched")
		require.GreaterOrEqual(t, after, reservedAfter, "all claims remain bank-backed")
		require.LessOrEqual(t, paid, charge.AmountOf(denom).Int64(), "no overcharge")
		require.LessOrEqual(t, consumed, paid, "reservation consumption must fund an actual payment")
		if ownAfter > 0 {
			require.Equal(t, paid, consumed, "own allocation must be consumed before free credit")
		}
		if paid < charge.AmountOf(denom).Int64() {
			require.Equal(t, other, after, "unpaid charge is permitted only after eligible credit is exhausted")
		}
	}
}

func FuzzPlanReservationSpend(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{100, 100, 0, 150, 0, 100, 50, 80})
	f.Add([]byte{255, 255, 255, 255, 1, 0, 0, 1, 0, 0, 1, 255})
	f.Fuzz(checkReservationSpendProperties)
}

func TestReservationSpendPropertiesAndIndependentLeaseOrder(t *testing.T) {
	rng := rand.New(rand.NewPCG(0x20260909, 0xb1111)) //nolint:gosec // Reproducible property-test inputs, not secrets.
	for range 256 {
		data := make([]byte, 16)
		for i := 0; i < len(data); i += 8 {
			binary.LittleEndian.PutUint64(data[i:i+8], rng.Uint64())
		}
		checkReservationSpendProperties(t, data)

		// When the bank holds only individually attributed reservations, a
		// lease's payment must be independent of which other lease settles first.
		// Free pooled credit is excluded because competition for it is intentional.
		const leaseCount = 8
		allocations, charges := make([]sdk.Coins, leaseCount), make([]sdk.Coins, leaseCount)
		var balance sdk.Coins
		for i := range leaseCount {
			for _, denom := range propertyDenoms {
				allocations[i] = allocations[i].Add(sdk.NewInt64Coin(denom, rng.Int64N(256)))
				charges[i] = charges[i].Add(sdk.NewInt64Coin(denom, rng.Int64N(512)))
			}
			balance = balance.Add(allocations[i]...)
		}
		settle := func(order []int) ([]sdk.Coins, sdk.Coins) {
			remainingBalance, remainingReserved := balance, balance
			paid := make([]sdk.Coins, leaseCount)
			for _, i := range order {
				plan, err := types.PlanReservationSpend(remainingBalance, remainingReserved, allocations[i], charges[i])
				require.NoError(t, err)
				paid[i] = plan.Transfer
				remainingBalance, remainingReserved = plan.BalanceAfter, plan.TotalReservedAfter
			}
			require.True(t, remainingBalance.Equal(remainingReserved))
			return paid, remainingBalance
		}
		order := make([]int, leaseCount)
		for i := range order {
			order[i] = i
		}
		forwardPaid, forwardBalance := settle(order)
		shuffled := slices.Clone(order)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		shuffledPaid, shuffledBalance := settle(shuffled)
		require.Equal(t, forwardPaid, shuffledPaid)
		require.True(t, forwardBalance.Equal(shuffledBalance))
	}
}
