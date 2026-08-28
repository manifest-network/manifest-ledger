package keeper_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdkcodec "github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

const (
	testParamsStoragePrefix        = "\x00billing/params/v1"
	testLeaseStoragePrefix         = "\x00billing/lease/v1"
	testCreditAccountStoragePrefix = "\x00billing/credit-account/v1"
)

func TestMigratorMigrate2to3RewritesLegacyAddressStrings(t *testing.T) {
	f := initFixture(t)
	tenant := f.TestAccs[0]
	allowed := f.TestAccs[1]
	credit := types.DeriveCreditAddress(tenant)
	upperTenant := strings.ToUpper(tenant.String())
	upperAllowed := strings.ToUpper(allowed.String())
	upperCredit := strings.ToUpper(credit.String())

	params := types.DefaultParams()
	params.AllowedList = []string{upperAllowed}
	lease := types.Lease{
		Uuid:         testLeaseUUID1,
		Tenant:       upperTenant,
		ProviderUuid: testProviderUUID,
		Items: []types.LeaseItem{{
			SkuUuid:     testSKUUUID,
			Quantity:    1,
			LockedPrice: sdk.NewCoin(testDenom, math.NewInt(2)),
		}},
		State:                      types.LEASE_STATE_ACTIVE,
		CreatedAt:                  f.Ctx.BlockTime(),
		LastSettledAt:              f.Ctx.BlockTime(),
		MinLeaseDurationAtCreation: types.DefaultMinLeaseDuration,
	}
	account := types.CreditAccount{
		Tenant:           upperTenant,
		CreditAddress:    upperCredit,
		ActiveLeaseCount: 1,
		ReservedAmounts:  types.CalculateLeaseReservation(lease.Items, lease.MinLeaseDurationAtCreation),
	}

	// Seed the indexes through the current keeper, then replace only the three
	// primary values with their exact legacy CollValue encodings.
	require.NoError(t, f.App.BillingKeeper.SetParams(f.Ctx, params))
	require.NoError(t, f.App.BillingKeeper.SetLease(f.Ctx, lease))
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, account))

	legacyParams, err := sdkcodec.CollValue[types.Params](f.EncodingCfg.Codec).Encode(params)
	require.NoError(t, err)
	legacyLease, err := sdkcodec.CollValue[types.Lease](f.EncodingCfg.Codec).Encode(lease)
	require.NoError(t, err)
	legacyAccount, err := sdkcodec.CollValue[types.CreditAccount](f.EncodingCfg.Codec).Encode(account)
	require.NoError(t, err)

	store := f.Ctx.KVStore(f.App.GetKey(types.StoreKey))
	leaseKey, err := collections.EncodeKeyWithPrefix(types.LeaseKey.Bytes(), collections.StringKey, lease.Uuid)
	require.NoError(t, err)
	accountKey, err := collections.EncodeKeyWithPrefix(types.CreditAccountKey.Bytes(), sdk.AccAddressKey, tenant)
	require.NoError(t, err)
	store.Set(types.ParamsKey.Bytes(), legacyParams)
	store.Set(leaseKey, legacyLease)
	store.Set(accountKey, legacyAccount)
	require.True(t, bytes.Contains(store.Get(types.ParamsKey.Bytes()), []byte(upperAllowed)))
	require.True(t, bytes.Contains(store.Get(leaseKey), []byte(upperTenant)))
	require.True(t, bytes.Contains(store.Get(accountKey), []byte(upperCredit)))

	indexedBefore, err := f.App.BillingKeeper.GetLeasesByTenantAndState(f.Ctx, tenant.String(), types.LEASE_STATE_ACTIVE)
	require.NoError(t, err)
	require.Len(t, indexedBefore, 1)

	require.NoError(t, keeper.NewMigrator(f.App.BillingKeeper).Migrate2to3(f.Ctx))

	rawParams := store.Get(types.ParamsKey.Bytes())
	rawLease := store.Get(leaseKey)
	rawAccount := store.Get(accountKey)
	assertRawAddressStorage(t, rawParams, testParamsStoragePrefix, allowed)
	assertRawAddressStorage(t, rawLease, testLeaseStoragePrefix, tenant)
	assertRawAddressStorage(t, rawAccount, testCreditAccountStoragePrefix, tenant)
	require.True(t, bytes.Contains(rawAccount, credit.Bytes()))
	require.False(t, bytes.Contains(rawAccount, []byte(credit.String())))
	require.False(t, bytes.Contains(rawAccount, []byte(upperCredit)))

	gotParams, err := f.App.BillingKeeper.GetParams(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, []string{allowed.String()}, gotParams.AllowedList)
	gotLease, err := f.App.BillingKeeper.GetLease(f.Ctx, lease.Uuid)
	require.NoError(t, err)
	require.Equal(t, tenant.String(), gotLease.Tenant)
	gotAccount, err := f.App.BillingKeeper.GetCreditAccount(f.Ctx, tenant.String())
	require.NoError(t, err)
	require.Equal(t, tenant.String(), gotAccount.Tenant)
	require.Equal(t, credit.String(), gotAccount.CreditAddress)

	indexedAfter, err := f.App.BillingKeeper.GetLeasesByTenantAndState(f.Ctx, tenant.String(), types.LEASE_STATE_ACTIVE)
	require.NoError(t, err)
	require.Len(t, indexedAfter, 1)
	require.Equal(t, lease.Uuid, indexedAfter[0].Uuid)

	// Re-running the migration is byte-for-byte stable.
	paramsSnapshot := bytes.Clone(rawParams)
	leaseSnapshot := bytes.Clone(rawLease)
	accountSnapshot := bytes.Clone(rawAccount)
	require.NoError(t, keeper.NewMigrator(f.App.BillingKeeper).Migrate2to3(f.Ctx))
	require.Equal(t, paramsSnapshot, store.Get(types.ParamsKey.Bytes()))
	require.Equal(t, leaseSnapshot, store.Get(leaseKey))
	require.Equal(t, accountSnapshot, store.Get(accountKey))
}

func TestMigratorMigrate2to3ProcessesMultipleOrderedPages(t *testing.T) {
	f := initFixture(t)
	tenant := f.TestAccs[0]
	upperTenant := strings.ToUpper(tenant.String())
	store := f.Ctx.KVStore(f.App.GetKey(types.StoreKey))
	legacyCodec := sdkcodec.CollValue[types.Lease](f.EncodingCfg.Codec)

	const leaseCount = 1001
	keys := make([][]byte, 0, leaseCount)
	for i := 0; i < leaseCount; i++ {
		uuid := fmt.Sprintf("migration-lease-%04d", i)
		legacy, err := legacyCodec.Encode(types.Lease{Uuid: uuid, Tenant: upperTenant})
		require.NoError(t, err)
		key, err := collections.EncodeKeyWithPrefix(types.LeaseKey.Bytes(), collections.StringKey, uuid)
		require.NoError(t, err)
		store.Set(key, legacy)
		keys = append(keys, key)
	}

	require.NoError(t, keeper.NewMigrator(f.App.BillingKeeper).Migrate2to3(f.Ctx))
	for _, key := range keys {
		assertRawAddressStorage(t, store.Get(key), testLeaseStoragePrefix, tenant)
	}
}

func TestMigratorMigrate2to3RejectsMismatchedCreditAccountKey(t *testing.T) {
	f := initFixture(t)
	tenant := f.TestAccs[0]
	otherTenant := f.TestAccs[1]
	account := types.CreditAccount{
		Tenant:        otherTenant.String(),
		CreditAddress: types.DeriveCreditAddress(otherTenant).String(),
	}
	legacy, err := sdkcodec.CollValue[types.CreditAccount](f.EncodingCfg.Codec).Encode(account)
	require.NoError(t, err)
	key, err := collections.EncodeKeyWithPrefix(types.CreditAccountKey.Bytes(), sdk.AccAddressKey, tenant)
	require.NoError(t, err)
	f.Ctx.KVStore(f.App.GetKey(types.StoreKey)).Set(key, legacy)

	err = keeper.NewMigrator(f.App.BillingKeeper).Migrate2to3(f.Ctx)
	require.ErrorContains(t, err, "does not match its store key")
}

func assertRawAddressStorage(t *testing.T, encoded []byte, prefix string, address sdk.AccAddress) {
	t.Helper()
	require.True(t, bytes.HasPrefix(encoded, []byte(prefix)))
	require.True(t, bytes.Contains(encoded, address.Bytes()))
	require.False(t, bytes.Contains(encoded, []byte(address.String())))
	require.False(t, bytes.Contains(encoded, []byte(strings.ToUpper(address.String()))))
}
