package keeper

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	errorsmod "cosmossdk.io/errors"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

type invalidPayoutSKUKeeper struct {
	provider skutypes.Provider
}

func (s invalidPayoutSKUKeeper) GetProvider(context.Context, string) (skutypes.Provider, error) {
	return s.provider, nil
}

func (invalidPayoutSKUKeeper) GetSKU(context.Context, string) (skutypes.SKU, error) {
	panic("unexpected GetSKU call")
}

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

func TestProviderWithdrawableClassifiesInvalidDependencyPayoutAddress(t *testing.T) {
	const providerUUID = "01912345-6789-7abc-8def-0123456789ab"
	querier := NewQuerier(Keeper{skuKeeper: invalidPayoutSKUKeeper{provider: skutypes.Provider{
		Uuid:          providerUUID,
		PayoutAddress: "not-a-bech32-address",
	}}})

	_, err := querier.ProviderWithdrawable(context.Background(), &types.QueryProviderWithdrawableRequest{
		ProviderUuid: providerUUID,
	})

	require.Equal(t, codes.Internal, status.Code(err))
	require.Contains(t, err.Error(), types.ErrInternalCorruption.Error())
	require.NotContains(t, err.Error(), types.ErrProviderNotFound.Error())
}
