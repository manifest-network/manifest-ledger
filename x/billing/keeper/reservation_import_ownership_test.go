package keeper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestReservationImportNormalizationPreservesCallerInput(t *testing.T) {
	for _, outcome := range []string{"success", "future cursor", "missing SKU"} {
		t.Run(outcome, func(t *testing.T) {
			f := initFixture(t)
			k := f.App.BillingKeeper
			provider := f.createTestProvider(t, f.TestAccs[2].String(), f.TestAccs[3].String())
			sku := f.createTestSKU(t, provider.Uuid, 3600)
			genesis := &types.GenesisState{Params: types.DefaultParams(), LeaseSequence: 2}
			genesis.Params.MaxLeasesPerTenant++
			for index, uuid := range []string{testLeaseUUID1, testLeaseUUID2} {
				tenant := f.TestAccs[index]
				genesis.Leases = append(genesis.Leases, types.Lease{
					Uuid: uuid, Tenant: tenant.String(), ProviderUuid: provider.Uuid,
					Items: []types.LeaseItem{{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewInt64Coin(testDenom, 1)}},
					State: types.LEASE_STATE_ACTIVE, CreatedAt: f.Ctx.BlockTime(), LastSettledAt: f.Ctx.BlockTime(),
					MinLeaseDurationAtCreation: 100,
				})
				genesis.CreditAccounts = append(genesis.CreditAccounts, types.CreditAccount{
					Tenant: tenant.String(), CreditAddress: types.DeriveCreditAddress(tenant).String(),
					// Historical counts are repaired in the import copy, alongside
					// the haircut from a 100-unit claim to 50 bank-backed units.
					ReservedAmounts: sdk.NewCoins(sdk.NewInt64Coin(testDenom, 100)),
				})
				f.fundAccount(t, types.DeriveCreditAddress(tenant), sdk.NewCoins(sdk.NewInt64Coin(testDenom, 50)))
			}
			switch outcome {
			case "future cursor":
				genesis.Leases[1].LastSettledAt = f.Ctx.BlockTime().Add(time.Second)
			case "missing SKU":
				genesis.Leases[1].Items[0].SkuUuid = testSKUUUID
			}
			// Both failure cases pass initial structural/accounting preparation;
			// they fail only after reservation conversion has been planned.
			_, err := genesis.PrepareForImport()
			require.NoError(t, err)
			inputBefore, err := genesis.Marshal()
			require.NoError(t, err)
			storesBefore := snapshotPayoutStores(t, f, f.Ctx)
			err = k.InitGenesis(f.Ctx, genesis)
			if outcome == "success" {
				require.NoError(t, err)
				for _, lease := range genesis.Leases {
					stored, getErr := k.GetLease(f.Ctx, lease.Uuid)
					require.NoError(t, getErr)
					require.NotNil(t, stored.Reservation)
					require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 50)), stored.Reservation.RemainingAmounts)
					account, getErr := k.GetCreditAccount(f.Ctx, lease.Tenant)
					require.NoError(t, getErr)
					require.Equal(t, uint64(1), account.ActiveLeaseCount)
					require.Equal(t, stored.Reservation.RemainingAmounts, account.ReservedAmounts)
				}
				require.NoError(t, k.ExportGenesis(f.Ctx).ValidateCurrentState())
			} else {
				require.Error(t, err)
				if outcome == "future cursor" {
					require.ErrorContains(t, err, "future")
				} else {
					require.ErrorContains(t, err, "SKU")
				}
				require.Equal(t, storesBefore, snapshotPayoutStores(t, f, f.Ctx), "failed import must not persist any planned reservation, count, parameter, or index changes")
			}
			inputAfter, err := genesis.Marshal()
			require.NoError(t, err)
			require.Equal(t, inputBefore, inputAfter, "normalization owns its copy on success and late validation failure")
		})
	}
}
