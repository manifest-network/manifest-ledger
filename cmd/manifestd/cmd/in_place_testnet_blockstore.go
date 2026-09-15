package cmd

import (
	"fmt"
	"math"

	cmtcfg "github.com/cometbft/cometbft/config"
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

func validateTestnetSourceHeights(appHeight, stateHeight, storeHeight int64) error {
	// SDK v0.50.14-liftedinit.2 accepts aligned commits, a stored uncommitted
	// block (including --halt-height), or an app commit before Comet state save.
	// Conversion writes validator history through the reconciled height + 2.
	if stateHeight < 1 || storeHeight < 1 || storeHeight > math.MaxInt64-2 ||
		storeHeight < stateHeight || storeHeight-stateHeight > 1 ||
		(appHeight != stateHeight && (appHeight != storeHeight || storeHeight != stateHeight+1)) {
		return fmt.Errorf("unsupported source application/Comet/blockstore heights %d/%d/%d; recover the source node before copying it", appHeight, stateHeight, storeHeight)
	}
	return nil
}
