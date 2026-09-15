package app_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"

	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
)

func TestSimulationCommitPersistsPostFinalizeOperations(t *testing.T) {
	for _, persistOperations := range []bool{false, true} {
		name := "pinned SDK discards post-finalize operations"
		if persistOperations {
			name = "simulation hook preserves post-finalize operations"
		}
		t.Run(name, func(t *testing.T) {
			db := dbm.NewMemDB()
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			options := []func(*baseapp.BaseApp){baseapp.SetChainID(SimAppChainID)}
			if persistOperations {
				options = append(options, simulationCommitOpt)
			}
			bApp := baseapp.NewBaseApp("simulation-commit-regression", log.NewNopLogger(), db, nil, options...)
			key := storetypes.NewKVStoreKey("simulation-state")
			bApp.MountKVStores(map[string]*storetypes.KVStoreKey{key.Name(): key})
			require.NoError(t, bApp.LoadLatestVersion())
			_, err := bApp.InitChain(&abci.RequestInitChain{ChainId: SimAppChainID})
			require.NoError(t, err)

			counterKey := []byte("completed-operations")
			blockTime := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
			for height := int64(1); height <= 3; height++ {
				_, err := bApp.FinalizeBlock(&abci.RequestFinalizeBlock{Height: height, Time: blockTime.Add(time.Duration(height) * time.Second)})
				require.NoError(t, err)
				ctx := bApp.GetContextForFinalizeBlock(nil)
				kv := ctx.KVStore(key)
				if persistOperations && height > 1 {
					require.Equal(t, strconv.FormatInt(height-1, 10), string(kv.Get(counterKey)), "later blocks must see earlier simulated operations")
				} else {
					require.Nil(t, kv.Get(counterKey))
				}

				// The SDK simulator delivers operations after FinalizeBlock, when
				// workingHash has already flushed this cache. Successful SimDeliver
				// writes its transaction branch into this same finalize-state cache.
				kv.Set(counterKey, []byte(strconv.FormatInt(height, 10)))
				_, err = bApp.Commit()
				require.NoError(t, err)

				committed := bApp.GetContextForCheckTx(nil).KVStore(key).Get(counterKey)
				if persistOperations {
					require.Equal(t, strconv.FormatInt(height, 10), string(committed), "Commit must persist operations from the current block")
				} else {
					require.Nil(t, committed, "the control reproduces the pinned simulator's lost writes")
				}
			}
		})
	}
}
