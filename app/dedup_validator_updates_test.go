package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"
	cryptoproto "github.com/cometbft/cometbft/proto/tendermint/crypto"

	"cosmossdk.io/log"
)

func ed25519Update(b byte, power int64) abci.ValidatorUpdate {
	key := make([]byte, 32)
	for i := range key {
		key[i] = b
	}
	return abci.ValidatorUpdate{
		PubKey: cryptoproto.PublicKey{Sum: &cryptoproto.PublicKey_Ed25519{Ed25519: key}},
		Power:  power,
	}
}

func requireNoDuplicates(t *testing.T, updates []abci.ValidatorUpdate) {
	t.Helper()
	seen := map[string]struct{}{}
	for _, u := range updates {
		k, err := u.PubKey.Marshal()
		require.NoError(t, err)
		_, dup := seen[string(k)]
		require.Falsef(t, dup, "duplicate consensus pubkey in validator updates")
		seen[string(k)] = struct{}{}
	}
}

// TestDedupValidatorUpdates_HaltCase reproduces the 2026-06-04 mainnet halt at
// height 6914435: the EndBlock validator-update set listed one consensus key
// (mainnet-ledger-025) three times, which CometBFT rejects irrecoverably.
func TestDedupValidatorUpdates_HaltCase(t *testing.T) {
	in := []abci.ValidatorUpdate{
		ed25519Update(0x01, 1),
		ed25519Update(0x02, 1),
		ed25519Update(0x03, 1),
		ed25519Update(0x09, 1), // the unjailed validator...
		ed25519Update(0x09, 1), // ...emitted twice more
		ed25519Update(0x09, 1),
	}
	out := dedupValidatorUpdates(log.NewNopLogger(), in)
	require.Len(t, out, 4, "each consensus key must appear exactly once")
	requireNoDuplicates(t, out)
}

func TestDedupValidatorUpdates_KeepsLastValue(t *testing.T) {
	out := dedupValidatorUpdates(log.NewNopLogger(), []abci.ValidatorUpdate{
		ed25519Update(0x09, 5),
		ed25519Update(0x09, 0), // last write wins (zero-power removal is the intended final state)
	})
	require.Len(t, out, 1)
	require.Equal(t, int64(0), out[0].Power)
}

// TestDedupValidatorUpdates_Interleaved covers a duplicate separated by a
// distinct key. This exercises the pos-map lookup across a gap and pins both
// keep-last and first-seen order in a single case (the adjacent-duplicate
// tests above cannot distinguish a write-back-into-place bug from one that
// keeps the wrong slot).
func TestDedupValidatorUpdates_Interleaved(t *testing.T) {
	out := dedupValidatorUpdates(log.NewNopLogger(), []abci.ValidatorUpdate{
		ed25519Update(0x09, 5),
		ed25519Update(0x01, 1),
		ed25519Update(0x09, 0), // last value for 0x09 wins
	})
	require.Len(t, out, 2)
	requireNoDuplicates(t, out)

	// first-seen order preserved: 0x09 stays at index 0, 0x01 at index 1
	require.Equal(t, ed25519Update(0x09, 0).PubKey, out[0].PubKey)
	require.Equal(t, int64(0), out[0].Power, "keep-last power for the duplicated key")
	require.Equal(t, ed25519Update(0x01, 1).PubKey, out[1].PubKey)
	require.Equal(t, int64(1), out[1].Power)
}

func TestDedupValidatorUpdates_NoOp(t *testing.T) {
	in := []abci.ValidatorUpdate{ed25519Update(0x01, 1), ed25519Update(0x02, 1)}
	require.Equal(t, in, dedupValidatorUpdates(log.NewNopLogger(), in))
	require.Empty(t, dedupValidatorUpdates(log.NewNopLogger(), nil))
}
