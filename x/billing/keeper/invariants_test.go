package keeper_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	"cosmossdk.io/collections/indexes"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestReservationAccountingInvariant(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)
	tenant := f.TestAccs[0]
	providerAddress := f.TestAccs[1]
	provider := f.createTestProvider(t, providerAddress.String(), providerAddress.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)
	creditAddress := types.DeriveCreditAddress(tenant)
	f.fundAccount(t, creditAddress, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 1_000_000)))
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddress.String(),
	}))
	f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddress, []types.LeaseItemInput{{
		SkuUuid:  sku.Uuid,
		Quantity: 1,
	}})

	message, broken := keeper.ReservationAccountingInvariant(f.App.BillingKeeper)(f.Ctx)
	require.False(t, broken, message)

	account, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	account.ActiveLeaseCount = 0
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, account))
	message, broken = keeper.ReservationAccountingInvariant(f.App.BillingKeeper)(f.Ctx)
	require.True(t, broken)
	require.Contains(t, message, "invalid credit-account lease counts")
	require.Contains(t, message, "active_lease_count 0 but has 1 active leases")

	account.ActiveLeaseCount = 1
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, account))
	validReserved := append(sdk.Coins(nil), account.ReservedAmounts...)
	account.ReservedAmounts = sdk.NewCoins(sdk.NewCoin(
		testDenom,
		validReserved.AmountOf(testDenom).SubRaw(1),
	))
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, account))
	message, broken = keeper.ReservationAccountingInvariant(f.App.BillingKeeper)(f.Ctx)
	require.True(t, broken)
	require.Contains(t, message, "consumable reservations sum")

	account.ReservedAmounts = validReserved
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, account))
	balance := f.App.BankKeeper.GetBalance(f.Ctx, creditAddress, testDenom).Amount
	leave := validReserved.AmountOf(testDenom).SubRaw(1)
	drain := balance.Sub(leave)
	require.True(t, drain.IsPositive())
	require.NoError(t, f.App.BankKeeper.SendCoins(
		f.Ctx,
		creditAddress,
		f.TestAccs[2],
		sdk.NewCoins(sdk.NewCoin(testDenom, drain)),
	))

	message, broken = keeper.ReservationAccountingInvariant(f.App.BillingKeeper)(f.Ctx)
	require.True(t, broken)
	require.Contains(t, message, "under-backed reservation state")
}

func TestReservationAccountingInvariantReportsCorruptParamsWithoutPanicking(t *testing.T) {
	f := initFixture(t)
	f.Ctx.KVStore(f.App.GetKey(types.StoreKey)).Set(types.ParamsKey.Bytes(), []byte{0xff})

	message, broken := keeper.ReservationAccountingInvariant(f.App.BillingKeeper)(f.Ctx)
	require.True(t, broken)
	require.Contains(t, message, "failed to export billing state")
	require.Contains(t, message, "read billing params")
}

func TestReservationAccountingInvariantValidatesRawStoredParams(t *testing.T) {
	tests := []struct {
		name   string
		params types.Params
		want   string
	}{
		{
			name: "duplicate decoded identity",
			params: func() types.Params {
				params := types.DefaultParams()
				address := sdk.AccAddress([]byte("duplicate-identity__")).String()
				params.AllowedList = []string{address, address}
				return params
			}(),
			want: "duplicate address in allowed list",
		},
		{
			name: "over cap equivalent aliases",
			params: func() types.Params {
				params := types.DefaultParams()
				address := sdk.AccAddress([]byte("over-cap-identity___")).String()
				params.AllowedList = make([]string, types.MaxAllowedListEntries+1)
				for i := range params.AllowedList {
					params.AllowedList[i] = address
				}
				return params
			}(),
			want: "allowed list has 101 entries, maximum allowed is 100",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := initFixture(t)
			require.NoError(t, f.App.BillingKeeper.SetParams(f.Ctx, test.params))

			message, broken := keeper.ReservationAccountingInvariant(f.App.BillingKeeper)(f.Ctx)
			require.True(t, broken)
			require.Contains(t, message, "invalid stored billing params")
			require.Contains(t, message, test.want)
		})
	}
}

func TestDerivedIndexesInvariant(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)
	tenant := f.TestAccs[0]
	providerAddress := f.TestAccs[1]
	provider := f.createTestProvider(t, providerAddress.String(), providerAddress.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)
	creditAddress := types.DeriveCreditAddress(tenant)
	f.fundAccount(t, creditAddress, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 1_000_000)))
	account := types.CreditAccount{Tenant: tenant.String(), CreditAddress: creditAddress.String()}
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, account))
	leaseUUID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddress, []types.LeaseItemInput{{
		SkuUuid:  sku.Uuid,
		Quantity: 1,
	}})
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseUUID)
	require.NoError(t, err)
	lease.Items[0].CustomDomain = "service.example.com"
	require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, lease))

	assertValid := func() {
		t.Helper()
		message, broken := keeper.DerivedIndexesInvariant(f.App.BillingKeeper)(f.Ctx)
		require.False(t, broken, message)
	}
	assertBroken := func(contains string) {
		t.Helper()
		message, broken := keeper.DerivedIndexesInvariant(f.App.BillingKeeper)(f.Ctx)
		require.True(t, broken)
		require.Contains(t, message, contains)
	}

	assertValid()

	require.NoError(t, f.App.BillingKeeper.LeaseBySKUIndex.Remove(
		f.Ctx,
		collections.Join(sku.Uuid, leaseUUID),
	))
	assertBroken("missing from the SKU index")
	require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, lease))
	assertValid()

	require.NoError(t, f.App.BillingKeeper.CreditAddressIndex.Remove(f.Ctx, creditAddress))
	assertBroken("missing from the reverse index")
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, account))
	assertValid()

	require.NoError(t, f.App.BillingKeeper.CustomDomainIndex.Remove(f.Ctx, lease.Items[0].CustomDomain))
	assertBroken("missing from the reverse index")
	require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, lease))
	assertValid()

	require.NoError(t, f.App.BillingKeeper.CustomDomainIndex.Set(f.Ctx, "stale.example.com", types.CustomDomainTarget{
		LeaseUuid:   "00000000-0000-7000-8000-000000000099",
		ServiceName: "service",
	}))
	assertBroken("references missing lease")
}

func TestDerivedIndexesInvariantDetectsManagedLeaseIndexDrift(t *testing.T) {
	setup := func(t *testing.T) (*testFixture, types.Lease) {
		t.Helper()
		f := initFixture(t)
		msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)
		tenant := f.TestAccs[0]
		providerAddress := f.TestAccs[1]
		provider := f.createTestProvider(t, providerAddress.String(), providerAddress.String())
		sku := f.createTestSKU(t, provider.Uuid, 3600)
		creditAddress := types.DeriveCreditAddress(tenant)
		f.fundAccount(t, creditAddress, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 1_000_000)))
		require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
			Tenant:        tenant.String(),
			CreditAddress: creditAddress.String(),
		}))
		leaseUUID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddress, []types.LeaseItemInput{{
			SkuUuid:  sku.Uuid,
			Quantity: 1,
		}})
		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseUUID)
		require.NoError(t, err)
		return f, lease
	}

	tests := []struct {
		name            string
		mismatch        func(*testFixture, *types.Lease)
		alterIndex      func(*testFixture, types.Lease, types.Lease, bool) error
		formatReference func(types.Lease) string
	}{
		{
			name: "tenant",
			mismatch: func(f *testFixture, indexed *types.Lease) {
				indexed.Tenant = f.TestAccs[2].String()
			},
			alterIndex: func(f *testFixture, current, indexed types.Lease, missing bool) error {
				return alterManagedLeaseIndex(
					f.Ctx, f.App.BillingKeeper.Leases.Indexes.Tenant, current, indexed, missing,
				)
			},
			formatReference: func(lease types.Lease) string {
				return lease.Tenant
			},
		},
		{
			name: "provider",
			mismatch: func(_ *testFixture, indexed *types.Lease) {
				indexed.ProviderUuid = "00000000-0000-7000-8000-000000000099"
			},
			alterIndex: func(f *testFixture, current, indexed types.Lease, missing bool) error {
				return alterManagedLeaseIndex(
					f.Ctx, f.App.BillingKeeper.Leases.Indexes.Provider, current, indexed, missing,
				)
			},
			formatReference: func(lease types.Lease) string {
				return fmt.Sprintf("%q", lease.ProviderUuid)
			},
		},
		{
			name: "state",
			mismatch: func(_ *testFixture, indexed *types.Lease) {
				indexed.State = types.LEASE_STATE_PENDING
			},
			alterIndex: func(f *testFixture, current, indexed types.Lease, missing bool) error {
				return alterManagedLeaseIndex(
					f.Ctx, f.App.BillingKeeper.Leases.Indexes.State, current, indexed, missing,
				)
			},
			formatReference: func(lease types.Lease) string {
				return fmt.Sprintf("%d", lease.State)
			},
		},
		{
			name: "provider-state",
			mismatch: func(_ *testFixture, indexed *types.Lease) {
				indexed.State = types.LEASE_STATE_PENDING
			},
			alterIndex: func(f *testFixture, current, indexed types.Lease, missing bool) error {
				return alterManagedLeaseIndex(
					f.Ctx, f.App.BillingKeeper.Leases.Indexes.ProviderState, current, indexed, missing,
				)
			},
			formatReference: func(lease types.Lease) string {
				return fmt.Sprintf("(%q, %d)", lease.ProviderUuid, lease.State)
			},
		},
		{
			name: "tenant-state",
			mismatch: func(_ *testFixture, indexed *types.Lease) {
				indexed.State = types.LEASE_STATE_PENDING
			},
			alterIndex: func(f *testFixture, current, indexed types.Lease, missing bool) error {
				return alterManagedLeaseIndex(
					f.Ctx, f.App.BillingKeeper.Leases.Indexes.TenantState, current, indexed, missing,
				)
			},
			formatReference: func(lease types.Lease) string {
				return fmt.Sprintf("(%s, %d)", lease.Tenant, lease.State)
			},
		},
		{
			name: "state-created-at",
			mismatch: func(_ *testFixture, indexed *types.Lease) {
				indexed.CreatedAt = indexed.CreatedAt.Add(time.Nanosecond)
			},
			alterIndex: func(f *testFixture, current, indexed types.Lease, missing bool) error {
				return alterManagedLeaseIndex(
					f.Ctx, f.App.BillingKeeper.Leases.Indexes.StateCreatedAt, current, indexed, missing,
				)
			},
			formatReference: func(lease types.Lease) string {
				return fmt.Sprintf(
					"(%d, %s)",
					lease.State,
					lease.CreatedAt.UTC().Format(time.RFC3339Nano),
				)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, missing := range []bool{true, false} {
				name := "mismatched"
				if missing {
					name = "missing"
				}
				t.Run(name, func(t *testing.T) {
					f, lease := setup(t)
					const decoyLeaseUUID = "00000000-0000-7000-8000-000000000001"
					if missing {
						// Put a valid lease before the damaged lease in primary-key
						// order. This proves localization scans past healthy rows and
						// reports the lease whose index row is actually absent.
						require.Greater(t, lease.Uuid, decoyLeaseUUID)
						decoy := lease
						decoy.Uuid = decoyLeaseUUID
						require.NoError(t, f.App.BillingKeeper.Leases.Set(f.Ctx, decoy.Uuid, decoy))
					}
					indexed := lease
					if !missing {
						test.mismatch(f, &indexed)
					}
					require.NoError(t, test.alterIndex(f, lease, indexed, missing))

					message, broken := keeper.DerivedIndexesInvariant(f.App.BillingKeeper)(f.Ctx)
					require.True(t, broken, message)
					if missing {
						require.Equal(t, fmt.Sprintf(
							"billing: derived-indexes invariant\n"+
								"invalid derived index state: lease %s index is missing derived key %s for lease %s\n",
							test.name,
							test.formatReference(lease),
							lease.Uuid,
						), message)
						require.NotContains(t, message, "for lease "+decoyLeaseUUID)
						return
					}
					require.Equal(t, fmt.Sprintf(
						"billing: derived-indexes invariant\n"+
							"invalid derived index state: lease %s index key %s for primary key %s does not match derived key %s\n",
						test.name,
						test.formatReference(indexed),
						lease.Uuid,
						test.formatReference(lease),
					), message)
				})
			}
		})
	}

	t.Run("primary key differs from stored UUID", func(t *testing.T) {
		f, lease := setup(t)
		primaryKey := lease.Uuid
		lease.Uuid = "00000000-0000-7000-8000-000000000099"
		require.NoError(t, f.App.BillingKeeper.Leases.Set(f.Ctx, primaryKey, lease))

		message, broken := keeper.DerivedIndexesInvariant(f.App.BillingKeeper)(f.Ctx)
		require.True(t, broken, message)
		require.Contains(t, message, "lease key "+primaryKey+" does not match stored UUID "+lease.Uuid)
	})

	t.Run("index row references missing primary", func(t *testing.T) {
		f, lease := setup(t)
		missingUUID := "00000000-0000-7000-8000-000000000099"
		require.NoError(t, f.App.BillingKeeper.Leases.Indexes.Provider.Reference(
			f.Ctx,
			missingUUID,
			lease,
			func() (types.Lease, error) { return types.Lease{}, collections.ErrNotFound },
		))

		message, broken := keeper.DerivedIndexesInvariant(f.App.BillingKeeper)(f.Ctx)
		require.True(t, broken, message)
		require.Contains(t, message, "lease provider index references missing primary key "+missingUUID)
	})
}

// alterManagedLeaseIndex removes the correct index reference and optionally
// replaces it with a row derived from a mismatched value. Using the public
// index API keeps this regression coupled to the invariant contract rather
// than to Collections' internal key encoding.
func alterManagedLeaseIndex[ReferenceKey any](
	ctx sdk.Context,
	index *indexes.Multi[ReferenceKey, string, types.Lease],
	current,
	indexed types.Lease,
	missing bool,
) error {
	if err := index.Unreference(
		ctx,
		current.Uuid,
		func() (types.Lease, error) { return current, nil },
	); err != nil {
		return err
	}
	if missing {
		return nil
	}
	return index.Reference(
		ctx,
		current.Uuid,
		indexed,
		func() (types.Lease, error) { return types.Lease{}, collections.ErrNotFound },
	)
}
