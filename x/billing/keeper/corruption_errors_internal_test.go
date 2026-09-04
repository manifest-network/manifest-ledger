package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	errorsmod "cosmossdk.io/errors"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

func TestStoredProviderPayoutAddressClassifiesInvalidState(t *testing.T) {
	_, err := storedProviderPayoutAddress(skutypes.Provider{
		Uuid:          "01912345-6789-7abc-8def-0123456789ab",
		PayoutAddress: "not-a-bech32-address",
	})

	require.ErrorIs(t, err, types.ErrInternalCorruption)
	codespace, code, _ := errorsmod.ABCIInfo(err, false)
	require.Equal(t, types.ModuleName, codespace)
	require.Equal(t, uint32(40), code)
}
