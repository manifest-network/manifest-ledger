package cmd

import (
	"bytes"
	"fmt"
	"math"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtcfg "github.com/cometbft/cometbft/config"
	cmtstateproto "github.com/cometbft/cometbft/proto/tendermint/state"
	cmtstate "github.com/cometbft/cometbft/state"
	cmtstore "github.com/cometbft/cometbft/store"
)

func readTestnetBlockStoreHeight(cfg *cmtcfg.Config) (_ int64, err error) {
	// Comet's descriptor loader only reads, but panics on a malformed record.
	// Keep corrupt copies on the error-returning side of the mutation boundary.
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("malformed persisted Comet blockstore state")
		}
	}()
	db, err := openTestnetReadOnlyDB("blockstore", cfg.DBDir())
	if err != nil {
		return 0, fmt.Errorf("read source blockstore: %w", err)
	}
	defer db.Close()
	state := cmtstore.LoadBlockStoreState(db)
	if state.Base < 1 || state.Height < state.Base {
		return 0, fmt.Errorf("invalid persisted Comet blockstore range %d/%d", state.Base, state.Height)
	}
	return state.Height, nil
}

// Mirror SDK .3's source reconciliation checks before creating the journal or
// opening any database for writes. Height agreement alone cannot establish that
// the application, Comet state, blockstore and recovery response are one copy.
func validateTestnetSourceCommit(cfg *cmtcfg.Config, state *cmtstate.State, appHeight int64, appHash []byte) (err error) {
	// Comet's block loaders panic on corrupt records instead of returning errors.
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("malformed persisted source block or recovery response")
		}
	}()
	db, err := openTestnetReadOnlyDB("blockstore", cfg.DBDir())
	if err != nil {
		return err
	}
	defer db.Close()
	store := cmtstore.NewBlockStore(db)
	block, meta := store.LoadBlock(appHeight), store.LoadBlockMeta(appHeight)
	if block == nil || meta == nil || !meta.BlockID.IsComplete() {
		return fmt.Errorf("source requires the full local block and metadata at height %d", appHeight)
	}
	if block.Height != appHeight || block.ChainID != state.ChainID || !bytes.Equal(meta.BlockID.Hash, block.Hash()) {
		return fmt.Errorf("source block at height %d disagrees with Comet state or metadata", appHeight)
	}
	if appHeight == state.LastBlockHeight {
		if !bytes.Equal(appHash, state.AppHash) {
			return fmt.Errorf("source application hash disagrees with Comet state at height %d; recover the source node before copying it", appHeight)
		}
		if !state.LastBlockID.Equals(meta.BlockID) {
			return fmt.Errorf("source last block ID disagrees with Comet state at height %d", appHeight)
		}
		return nil
	}
	if !block.LastBlockID.Equals(state.LastBlockID) || !bytes.Equal(block.AppHash, state.AppHash) {
		return fmt.Errorf("committed source block at height %d does not extend Comet state", appHeight)
	}
	response, err := readTestnetLastFinalizeBlockResponse(cfg, appHeight)
	if err != nil {
		return fmt.Errorf("read committed source block response at height %d: %w", appHeight, err)
	}
	if response == nil || len(response.TxResults) != len(block.Txs) {
		return fmt.Errorf("invalid committed source block response at height %d", appHeight)
	}
	for _, result := range response.TxResults {
		if result == nil {
			return fmt.Errorf("missing source transaction result at height %d", appHeight)
		}
	}
	// The legacy response converted after a v0.37 upgrade has no application
	// hash; the SDK uses the application commit hash in that case.
	if len(response.AppHash) > 0 && !bytes.Equal(response.AppHash, appHash) {
		return fmt.Errorf("committed source response hash disagrees with application at height %d", appHeight)
	}
	if updates := response.ConsensusParamUpdates; updates != nil {
		if err := state.ConsensusParams.Update(updates).ValidateBasic(); err != nil {
			return fmt.Errorf("invalid committed source consensus params: %w", err)
		}
		if err := state.ConsensusParams.ValidateUpdate(updates, appHeight); err != nil {
			return fmt.Errorf("invalid committed source consensus parameter update: %w", err)
		}
	}
	return nil
}

func readTestnetLastFinalizeBlockResponse(cfg *cmtcfg.Config, height int64) (*abci.ResponseFinalizeBlock, error) {
	db, err := openTestnetReadOnlyDB("state", cfg.DBDir())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	// Comet exits the process on malformed protobuf and panics on an empty
	// payload. Decode first, then use its public loader for legacy conversion.
	bz, err := db.Get([]byte("lastABCIResponseKey"))
	if err != nil {
		return nil, err
	}
	if len(bz) == 0 {
		return nil, fmt.Errorf("no last ABCI response has been persisted")
	}
	var info cmtstateproto.ABCIResponsesInfo
	if err := info.Unmarshal(bz); err != nil {
		return nil, fmt.Errorf("decode last ABCI response: %w", err)
	}
	if info.ResponseFinalizeBlock == nil && info.LegacyAbciResponses == nil {
		return nil, fmt.Errorf("last ABCI response contains no response")
	}
	return cmtstate.NewStore(db, cmtstate.StoreOptions{}).LoadLastFinalizeBlockResponse(height)
}

func validateTestnetSourceHeights(appHeight, stateHeight, storeHeight int64) error {
	// SDK v0.50.14-liftedinit.3 accepts aligned commits, a stored uncommitted
	// block (including --halt-height), or an app commit before Comet state save.
	// Conversion writes validator history through the reconciled height + 2.
	if stateHeight < 1 || storeHeight < 1 || storeHeight > math.MaxInt64-2 ||
		storeHeight < stateHeight || storeHeight-stateHeight > 1 ||
		(appHeight != stateHeight && (appHeight != storeHeight || storeHeight != stateHeight+1)) {
		return fmt.Errorf("unsupported source application/Comet/blockstore heights %d/%d/%d; recover the source node before copying it", appHeight, stateHeight, storeHeight)
	}
	return nil
}
