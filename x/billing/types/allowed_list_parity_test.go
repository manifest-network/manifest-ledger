package types_test

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/bech32"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

func TestAllowedListMatchesSDKAddressIdentity(t *testing.T) {
	// The oracle is the pinned SDK's address decoder, independent of the new
	// string-membership implementation. Its strict Bech32 decoder accepts only
	// canonical lowercase or all-uppercase, never mixed case or bad padding.
	reference := func(list []string, input string) bool {
		candidate, err := sdk.AccAddressFromBech32(input)
		if err != nil {
			return false
		}
		for _, item := range list {
			allowed, err := sdk.AccAddressFromBech32(item)
			if err == nil && candidate.Equals(allowed) {
				return true
			}
		}
		return false
	}
	rng := rand.NewChaCha8([32]byte{19, 63}) // deterministic address corpus; no secret keys are generated
	for sample := range 32 {
		address := make(sdk.AccAddress, 20+sample%13)
		_, err := rng.Read(address)
		require.NoError(t, err)
		canonical := address.String()
		upper := strings.ToUpper(canonical)
		wrongHRP, err := bech32.ConvertAndEncode("wrongprefix", address)
		require.NoError(t, err)
		badChecksum := canonical[:len(canonical)-1] + "q"
		if badChecksum == canonical {
			badChecksum = canonical[:len(canonical)-1] + "p"
		}
		variants := []string{canonical, upper, strings.ToUpper(canonical[:1]) + canonical[1:], wrongHRP, badChecksum, " " + canonical, canonical + " ", "", "invalid"}
		for _, allowed := range variants {
			for _, input := range variants {
				list := []string{"invalid", allowed}
				want := reference(list, input)
				require.Equal(t, want, (types.Params{AllowedList: list}).IsAllowed(input), "billing input=%q list=%q", input, list)
				require.Equal(t, want, (skutypes.Params{AllowedList: list}).IsAllowed(input), "sku input=%q list=%q", input, list)
			}
		}
	}
}
