package keeper_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"

	sdkcodec "github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestReservationMigrationPreflightMatchesSequentialActiveHaircut(t *testing.T) {
	f := initFixture(t)
	tenant := f.TestAccs[0]
	creditAddress := types.DeriveCreditAddress(tenant)
	now := f.Ctx.BlockTime()

	lease := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       strings.ToUpper(tenant.String()),
		ProviderUuid: testProviderUUID,
		Items: []types.LeaseItem{{
			SkuUuid:     testSKUUUID,
			Quantity:    1,
			LockedPrice: sdk.NewInt64Coin(testDenom, 10),
		}},
		State:                      types.LEASE_STATE_ACTIVE,
		CreatedAt:                  now,
		LastSettledAt:              now,
		MinLeaseDurationAtCreation: 1,
	}
	account := types.CreditAccount{
		Tenant:           strings.ToUpper(tenant.String()),
		CreditAddress:    strings.ToUpper(creditAddress.String()),
		ActiveLeaseCount: 0, // v2 cached-count drift is repaired before planning.
		ReservedAmounts:  sdk.NewCoins(sdk.NewInt64Coin(testDenom, 10)),
	}
	bankBalance := sdk.NewCoins(sdk.NewInt64Coin(testDenom, 5))
	billingGenesis := &types.GenesisState{
		Params:         types.DefaultParams(),
		Leases:         []types.Lease{lease},
		CreditAccounts: []types.CreditAccount{account},
		LeaseSequence:  1,
	}
	bankGenesis := banktypes.DefaultGenesisState()
	bankGenesis.Balances = []banktypes.Balance{{
		Address: creditAddress.String(),
		Coins:   bankBalance,
	}}

	report, err := keeper.BuildReservationMigrationPreflight(
		now,
		billingGenesis,
		bankGenesis,
	)
	require.NoError(t, err)
	require.Equal(t, keeper.ReservationPreflightStatePreV4, report.BillingState)
	require.Equal(t, uint64(1), report.ReservationChangeTenantCount)
	require.Zero(t, report.ExpiringModernPendingTenantCount)
	require.Zero(t, report.ExpiringModernPendingLeaseCount)
	require.Equal(t, []keeper.ReservationMigrationTenantPreflight{{
		Tenant:                      tenant.String(),
		CreditAddress:               creditAddress.String(),
		HasPlannedReservationChange: true,
		Denominations: []keeper.ReservationMigrationDenomPreflight{{
			Denom:                              testDenom,
			SourceReservationAggregate:         "10",
			PreCutoverReservationAggregate:     "10",
			PostCutoverReservationAggregate:    "5",
			PreCutoverUnattributedReservation:  "0",
			PostCutoverUnattributedReservation: "0",
			BankBalance:                        "5",
			ModernPendingRequired:              "0",
			ModernPendingShortfall:             "0",
		}},
		ModernActiveLeases: []keeper.ReservationMigrationActivePreflight{{
			LeaseUUID: testLeaseUUID1,
			NominalAmounts: []keeper.ReservationMigrationAmountPreflight{{
				Denom: testDenom, Amount: "10",
			}},
			PlannedRemainingAmounts: []keeper.ReservationMigrationAmountPreflight{{
				Denom: testDenom, Amount: "5",
			}},
		}},
		ModernPendingLeaseUUIDs:         []string{},
		ExpiringModernPendingLeaseUUIDs: []string{},
	}}, report.Tenants)

	// Populate equivalent v2-shaped on-chain state, including the legacy public
	// protobuf value encodings while retaining the byte-addressed indexes.
	require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, lease))
	overwriteLeaseWithLegacyEncoding(t, f, lease)
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, account))
	legacyAccount, err := sdkcodec.CollValue[types.CreditAccount](f.EncodingCfg.Codec).Encode(account)
	require.NoError(t, err)
	accountKey, err := collections.EncodeKeyWithPrefix(
		types.CreditAccountKey.Bytes(),
		sdk.AccAddressKey,
		tenant,
	)
	require.NoError(t, err)
	f.Ctx.KVStore(f.App.GetKey(types.StoreKey)).Set(accountKey, legacyAccount)
	f.fundAccount(t, creditAddress, bankBalance)

	migrator := keeper.NewMigrator(f.App.BillingKeeper)
	require.NoError(t, migrator.Migrate2to3(f.Ctx))
	v3Account, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(10), v3Account.ReservedAmounts.AmountOf(testDenom))

	require.NoError(t, migrator.Migrate3to4(f.Ctx))
	migratedLease, err := f.App.BillingKeeper.GetLease(f.Ctx, lease.Uuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, migratedLease.State)
	require.NotNil(t, migratedLease.Reservation)
	require.Equal(t,
		sdkmath.NewInt(5),
		migratedLease.Reservation.RemainingAmounts.AmountOf(testDenom),
	)
	migratedAccount, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(5), migratedAccount.ReservedAmounts.AmountOf(testDenom))
	require.True(t, migratedAccount.UnattributedReservedAmounts.IsZero())
}
