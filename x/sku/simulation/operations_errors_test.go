package simulation

import (
	"context"
	"errors"
	"io"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	"cosmossdk.io/collections/colltest"
	"cosmossdk.io/core/store"
	"cosmossdk.io/log"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"github.com/manifest-network/manifest-ledger/x/sku/keeper"
	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

func TestSimulationCatalogReadsDistinguishAbsenceAndFailures(t *testing.T) {
	operations := []struct {
		name   string
		build  func(client.TxConfig, keeper.Keeper) simtypes.Operation
		prefix collections.Prefix
	}{
		{name: "update provider", build: SimulateMsgUpdateProvider, prefix: types.ProviderKey},
		{name: "deactivate provider", build: SimulateMsgDeactivateProvider, prefix: types.ProviderKey},
		{name: "create SKU", build: SimulateMsgCreateSKU, prefix: types.ProviderKey},
		{name: "update SKU", build: SimulateMsgUpdateSKU, prefix: types.SKUKey},
		{name: "deactivate SKU", build: SimulateMsgDeactivateSKU, prefix: types.SKUKey},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			for _, scenario := range []string{"empty", "iterator failure", "corrupt record"} {
				t.Run(scenario, func(t *testing.T) {
					k, ctx, kv, accounts := simulationReadFixture(t)
					switch scenario {
					case "iterator failure":
						kv.iteratorErr = errors.New("simulation store iterator failed")
					case "corrupt record":
						key := append(operation.prefix.Bytes(), []byte("corrupt-record")...)
						require.NoError(t, kv.Set(key, []byte{0xff}))
					}

					op := operation.build(nil, k)
					msg, future, err := op(rand.New(rand.NewSource(1)), nil, ctx, accounts, "test-chain") //nolint:gosec // deterministic simulation PRNG
					require.Empty(t, future)
					if scenario == "empty" {
						require.NoError(t, err)
						require.Contains(t, msg.Comment, "found")
						return
					}
					require.Error(t, err)
					require.Contains(t, msg.Comment, "failed to read")
					if scenario == "iterator failure" {
						require.ErrorIs(t, err, kv.iteratorErr)
					} else {
						require.ErrorIs(t, err, io.ErrUnexpectedEOF)
					}
				})
			}
		})
	}
}

func TestSimulationSKUProviderReadDistinguishesAbsenceAndCorruption(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		name := "absent provider"
		if corrupt {
			name = "corrupt provider"
		}
		t.Run(name, func(t *testing.T) {
			k, ctx, kv, accounts := simulationReadFixture(t)
			sku := types.SKU{
				Uuid:         "01912345-6789-7abc-8def-0123456789ac",
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Name:         "test SKU",
				Unit:         types.Unit_UNIT_PER_HOUR,
				BasePrice:    sdk.NewInt64Coin("stake", types.SecondsPerHour),
				Active:       true,
			}
			require.NoError(t, k.SetSKU(ctx, sku))
			if corrupt {
				key := append(types.ProviderKey.Bytes(), []byte(sku.ProviderUuid)...)
				require.NoError(t, kv.Set(key, []byte{0xff}))
			}

			op := SimulateMsgUpdateSKU(nil, k)
			msg, future, err := op(rand.New(rand.NewSource(1)), nil, ctx, accounts, "test-chain") //nolint:gosec // deterministic simulation PRNG
			require.Empty(t, future)
			if corrupt {
				require.ErrorIs(t, err, types.ErrInternalCorruption)
				require.ErrorContains(t, err, sku.Uuid)
				require.Equal(t, "failed to read provider for SKU", msg.Comment)
			} else {
				require.NoError(t, err)
				require.Equal(t, "provider not found for SKU", msg.Comment)
			}
		})
	}
}

func simulationReadFixture(t *testing.T) (keeper.Keeper, sdk.Context, *simulationReadStore, []simtypes.Account) {
	t.Helper()
	service, storeCtx := colltest.MockStore()
	kv := &simulationReadStore{KVStore: service.OpenKVStore(storeCtx)}
	ctx := sdk.Context{}.WithContext(storeCtx)
	accounts := testSimulationAccounts(1)
	encoding := moduletestutil.MakeTestEncodingConfig()
	k := keeper.NewKeeper(encoding.Codec, simulationReadStoreService{kv}, log.NewNopLogger(), accounts[0].Address.String(), nil, nil)
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))
	return k, ctx, kv, accounts
}

type simulationReadStore struct {
	store.KVStore
	iteratorErr error
}

func (s *simulationReadStore) Iterator(start, end []byte) (store.Iterator, error) {
	if s.iteratorErr != nil {
		return nil, s.iteratorErr
	}
	return s.KVStore.Iterator(start, end)
}

type simulationReadStoreService struct {
	store.KVStore
}

func (s simulationReadStoreService) OpenKVStore(context.Context) store.KVStore {
	return s.KVStore
}
