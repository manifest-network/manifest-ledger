package simulation

import (
	"context"
	"errors"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections/colltest"
	"cosmossdk.io/core/store"
	"cosmossdk.io/log"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

func TestSimulationLeaseReadFailuresAreNotSuccessfulNoOps(t *testing.T) {
	for _, build := range []func(client.TxConfig, keeper.Keeper) simtypes.Operation{
		SimulateMsgCancelLease,
		func(tx client.TxConfig, k keeper.Keeper) simtypes.Operation { return SimulateMsgCloseLease(tx, k, nil) },
		SimulateMsgSetItemCustomDomain,
		func(tx client.TxConfig, k keeper.Keeper) simtypes.Operation {
			return SimulateMsgAcknowledgeLease(tx, k, nil)
		},
		func(tx client.TxConfig, k keeper.Keeper) simtypes.Operation {
			return SimulateMsgRejectLease(tx, k, nil)
		},
		func(tx client.TxConfig, k keeper.Keeper) simtypes.Operation { return SimulateMsgWithdraw(tx, k, nil) },
	} {
		for _, scenario := range []string{"empty", "iterator failure", "corrupt lease"} {
			t.Run(scenario, func(t *testing.T) {
				k, ctx, kv, accounts := billingSimulationReadFixture(t)
				switch scenario {
				case "iterator failure":
					kv.iteratorErr = errors.New("billing simulation iterator failed")
				case "corrupt lease":
					require.NoError(t, kv.Set(append(types.LeaseKey.Bytes(), []byte("corrupt")...), []byte{0xff}))
				}
				op, future, err := build(nil, k)(rand.New(rand.NewSource(1)), nil, ctx, accounts, "test-chain") //nolint:gosec // deterministic simulation PRNG
				require.False(t, op.OK)
				require.Empty(t, future)
				if scenario == "empty" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
					require.Equal(t, "failed to get leases", op.Comment)
					if kv.iteratorErr != nil {
						require.ErrorIs(t, err, kv.iteratorErr)
					}
				}
			})
		}
	}
}

func TestSimulationSKUReadFailuresAreNotSuccessfulNoOps(t *testing.T) {
	for _, build := range []func(client.TxConfig, keeper.Keeper, SKUKeeper) simtypes.Operation{
		SimulateMsgFundCredit, SimulateMsgCreateLease, SimulateMsgCreateLeaseForTenant,
	} {
		k, ctx, _, accounts := billingSimulationReadFixture(t)
		readErr := errors.New("SKU codec failed")
		sk := failingSimulationSKUKeeper{err: readErr}
		op, future, err := build(nil, k, sk)(rand.New(rand.NewSource(1)), nil, ctx, accounts, "test-chain") //nolint:gosec // deterministic simulation PRNG
		require.ErrorIs(t, err, readErr)
		require.False(t, op.OK)
		require.Empty(t, future)
		require.Equal(t, "failed to get SKUs", op.Comment)
	}
}

type failingSimulationSKUKeeper struct {
	SKUKeeper
	err error
}

func (s failingSimulationSKUKeeper) GetAllSKUs(context.Context) ([]skutypes.SKU, error) {
	return nil, s.err
}

func billingSimulationReadFixture(t *testing.T) (keeper.Keeper, sdk.Context, *billingSimulationReadStore, []simtypes.Account) {
	t.Helper()
	service, storeCtx := colltest.MockStore()
	kv := &billingSimulationReadStore{KVStore: service.OpenKVStore(storeCtx)}
	ctx := sdk.Context{}.WithContext(storeCtx)
	accounts := billingTestSimulationAccounts(1)
	encoding := moduletestutil.MakeTestEncodingConfig()
	k := keeper.NewKeeper(encoding.Codec, billingSimulationReadStoreService{kv}, log.NewNopLogger(), accounts[0].Address.String(), nil, nil, nil)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))
	return k, ctx, kv, accounts
}

type billingSimulationReadStore struct {
	store.KVStore
	iteratorErr error
}

func (s *billingSimulationReadStore) Iterator(start, end []byte) (store.Iterator, error) {
	if s.iteratorErr != nil {
		return nil, s.iteratorErr
	}
	return s.KVStore.Iterator(start, end)
}

type billingSimulationReadStoreService struct{ store.KVStore }

func (s billingSimulationReadStoreService) OpenKVStore(context.Context) store.KVStore {
	return s.KVStore
}
