package keeper

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/spf13/cast"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"

	appparams "github.com/manifest-network/manifest-ledger/app/params"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// The estimate benchmark uses real billing codecs, indexes and an SDK gas
// store; fixed bank balances isolate the cost of scanning and aggregation.
// Separate vesting benchmarks measure the SDK's in-memory schedule loop.
type queryWorkBank struct{ types.BankKeeper }

func (queryWorkBank) GetBalance(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
	return sdk.NewInt64Coin(denom, 1_000_000_000)
}

func (queryWorkBank) LockedCoins(context.Context, sdk.AccAddress) sdk.Coins { return sdk.NewCoins() }

func setupCreditEstimateWork(tb testing.TB, leaseCount, itemsPerLease, denomCount int) (Querier, sdk.Context, string) {
	tb.Helper()
	appparams.SetAddressPrefixes()
	key := storetypes.NewKVStoreKey(types.StoreKey)
	fixture := testutil.DefaultContextWithDB(tb, key, storetypes.NewTransientStoreKey("query-work"))
	tb.Cleanup(func() { require.NoError(tb, fixture.DB.Close()) })
	ctx := fixture.Ctx.WithBlockTime(time.Unix(1_700_000_000, 0).UTC())
	cfg := moduletestutil.MakeTestEncodingConfig()
	k := NewKeeper(cfg.Codec, runtime.NewKVStoreService(key), log.NewNopLogger(), "", nil, queryWorkBank{}, nil)
	tenant := sdk.AccAddress(bytes.Repeat([]byte{0x47}, 20))
	activeLeaseCount, err := cast.ToUint64E(leaseCount)
	require.NoError(tb, err)
	require.NoError(tb, k.SetCreditAccount(ctx, types.CreditAccount{
		Tenant: tenant.String(), CreditAddress: types.DeriveCreditAddress(tenant).String(),
		ActiveLeaseCount: activeLeaseCount,
	}))
	for leaseIndex := range leaseCount {
		items := make([]types.LeaseItem, itemsPerLease)
		for itemIndex := range items {
			denomIndex := (leaseIndex*itemsPerLease + itemIndex) % denomCount
			items[itemIndex] = types.LeaseItem{
				SkuUuid:  fmt.Sprintf("01912345-6789-7abc-8def-%012x", denomIndex+1),
				Quantity: 1, LockedPrice: sdk.NewInt64Coin(fmt.Sprintf("udenom%06d", denomIndex), 1),
			}
		}
		require.NoError(tb, k.SetLease(ctx, types.Lease{
			Uuid: fmt.Sprintf("01912345-6789-7abc-8abc-%012x", leaseIndex+1), Tenant: tenant.String(),
			ProviderUuid: "01912345-6789-7abc-8def-0123456789ad", Items: items,
			State: types.LEASE_STATE_ACTIVE, CreatedAt: ctx.BlockTime(), LastSettledAt: ctx.BlockTime(),
			AcknowledgedAt: new(ctx.BlockTime()), MinLeaseDurationAtCreation: 1,
			Reservation: &types.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
		}))
	}
	return NewQuerier(k), ctx, tenant.String()
}

func BenchmarkCreditEstimateWork(b *testing.B) {
	for _, size := range []struct{ leases, items, denoms int }{
		{100, 100, 100}, {1_000, 100, 1_000}, {11_000, 9, 1_000}, {1_000, 100, 100_000},
	} {
		b.Run(fmt.Sprintf("leases=%d/items=%d/denoms=%d", size.leases, size.items, size.denoms), func(b *testing.B) {
			q, ctx, tenant := setupCreditEstimateWork(b, size.leases, size.items, size.denoms)
			request := &types.QueryCreditEstimateRequest{Tenant: tenant}
			var totalGas uint64
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				queryCtx := ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
				response, err := q.CreditEstimate(queryCtx, request)
				if err != nil || len(response.TotalRatePerSecond) != size.denoms {
					b.Fatalf("unexpected estimate: %v", err)
				}
				totalGas += queryCtx.GasMeter().GasConsumed()
			}
			b.ReportMetric(float64(totalGas)/float64(b.N), "gas/op")
		})
	}
}

func BenchmarkPeriodicVestingLockedCoins(b *testing.B) {
	for _, size := range []struct{ periods, denoms int }{{1_000, 1}, {10_000, 1}, {100_000, 1}, {2_000, 2_000}, {39_000, 39_000}} {
		b.Run(fmt.Sprintf("periods=%d/denoms=%d", size.periods, size.denoms), func(b *testing.B) {
			periods := make([]vestingtypes.Period, size.periods)
			amounts := make([]sdk.Coin, size.periods)
			for index := range periods {
				coin := sdk.NewInt64Coin(fmt.Sprintf("udenom%06d", index%size.denoms), 1)
				periods[index] = vestingtypes.Period{Length: 1, Amount: sdk.NewCoins(coin)}
				amounts[index] = coin
			}
			original, err := types.SafeAggregateCoins(amounts)
			require.NoError(b, err)
			account, err := vestingtypes.NewPeriodicVestingAccount(
				authtypes.NewBaseAccountWithAddress(sdk.AccAddress(bytes.Repeat([]byte{0x48}, 20))),
				original, 1_700_000_000, periods,
			)
			require.NoError(b, err)
			at := time.Unix(account.EndTime-1, 0).UTC()
			b.ReportAllocs()
			b.ResetTimer()
			b.ReportMetric(float64(account.Size()), "account-bytes")
			for range b.N {
				locked := account.LockedCoins(at)
				if len(locked) != 1 || !locked[0].Amount.Equal(sdk.NewInt64Coin("udenom000000", 1).Amount) {
					b.Fatalf("unexpected lock at penultimate period: %s", locked)
				}
			}
		})
	}
}
