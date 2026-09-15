package keeper

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	"cosmossdk.io/collections/colltest"
	"cosmossdk.io/core/store"
	"cosmossdk.io/log"

	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestHealthyDerivedIndexesDoNotReloadLeasePerItem(t *testing.T) {
	for _, itemCount := range []int{1, 20, 100} {
		t.Run(fmt.Sprintf("%d items", itemCount), func(t *testing.T) {
			service, ctx := colltest.MockStore()
			counter := &leaseReadCountingStore{KVStore: service.OpenKVStore(ctx)}
			encoding := moduletestutil.MakeTestEncodingConfig()
			tenant := sdk.AccAddress([]byte("12345678901234567890")).String()
			k := NewKeeper(encoding.Codec, leaseReadCountingService{counter}, log.NewNopLogger(), tenant, nil, nil, nil)
			lease := types.Lease{
				Uuid:         "00000000-0000-7000-8000-000000000001",
				Tenant:       tenant,
				ProviderUuid: "00000000-0000-7000-8000-000000000002",
				State:        types.LEASE_STATE_ACTIVE,
				CreatedAt:    time.Unix(1, 0).UTC(),
			}
			for i := range itemCount {
				item := types.LeaseItem{
					SkuUuid:      fmt.Sprintf("00000000-0000-7000-8000-%012d", i+10),
					ServiceName:  fmt.Sprintf("service%d", i),
					CustomDomain: fmt.Sprintf("service%d.example.com", i),
					Quantity:     1,
					LockedPrice:  sdk.NewInt64Coin("umfx", 1),
				}
				lease.Items = append(lease.Items, item)
				require.NoError(t, k.LeaseBySKUIndex.Set(ctx, collections.Join(item.SkuUuid, lease.Uuid), true))
				require.NoError(t, k.CustomDomainIndex.Set(ctx, item.CustomDomain, types.CustomDomainTarget{
					LeaseUuid: lease.Uuid, ServiceName: item.ServiceName,
				}))
			}
			require.NoError(t, k.Leases.Set(ctx, lease.Uuid, lease))
			counter.leaseReads = 0
			require.NoError(t, k.validateDerivedIndexes(ctx))
			// Each managed index may resolve its single row to the lease; SKU
			// and domain index checks must not add full-lease reads per item.
			require.LessOrEqual(t, counter.leaseReads, 6)
		})
	}
}

type leaseReadCountingService struct{ store.KVStore }

func (s leaseReadCountingService) OpenKVStore(context.Context) store.KVStore { return s.KVStore }

type leaseReadCountingStore struct {
	store.KVStore
	leaseReads int
}

func (s *leaseReadCountingStore) Get(key []byte) ([]byte, error) {
	if bytes.HasPrefix(key, types.LeaseKey.Bytes()) {
		s.leaseReads++
	}
	return s.KVStore.Get(key)
}
