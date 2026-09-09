package keeper_test

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/sku/keeper"
	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

// TestCreateProviderUUIDContract pins the real keeper's namespace and sequence
// allocation to identifiers captured from the manual implementation at 85d1602.
func TestCreateProviderUUIDContract(t *testing.T) {
	f := initFixture(t)
	headerHash, err := hex.DecodeString("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	f.Ctx = f.Ctx.
		WithBlockTime(time.Date(2024, 1, 15, 10, 30, 0, 123456789, time.UTC)).
		WithHeaderHash(headerHash).
		WithChainID("manifest-1")

	authority, payoutAddress := f.TestAccs[0], f.TestAccs[3]
	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	require.NoError(t, k.ProviderSequence.Set(f.Ctx, 4095))
	msgServer := keeper.NewMsgServerImpl(k)

	for i, expected := range []string{
		"018d0cab-c4bb-7fff-845d-bf8a4a149b4d",
		"018d0cab-c4bb-7000-84c6-1a8a4a6c6bf7",
	} {
		response, err := msgServer.CreateProvider(f.Ctx, &types.MsgCreateProvider{
			Authority:     authority.String(),
			Address:       f.TestAccs[i+1].String(),
			PayoutAddress: payoutAddress.String(),
		})
		require.NoError(t, err)
		require.Equal(t, expected, response.Uuid)

		provider, err := k.GetProvider(f.Ctx, expected)
		require.NoError(t, err)
		require.Equal(t, expected, provider.Uuid, "stored identity must match the returned UUID")
	}

	next, err := k.ProviderSequence.Peek(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(4097), next)
}

// TestCreateSKUUUIDContract pins the SKU creation path independently of provider
// creation, using identifiers from the manual implementation at 85d1602.
func TestCreateSKUUUIDContract(t *testing.T) {
	f := initFixture(t)
	headerHash, err := hex.DecodeString("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	f.Ctx = f.Ctx.
		WithBlockTime(time.Date(2024, 1, 15, 10, 30, 0, 123456789, time.UTC)).
		WithHeaderHash(headerHash).
		WithChainID("manifest-1")

	authority, providerAddress, payoutAddress := f.TestAccs[0], f.TestAccs[1], f.TestAccs[2]
	k := f.App.SKUKeeper
	k.SetAuthority(authority.String())
	require.NoError(t, k.SetProvider(f.Ctx, types.Provider{
		Uuid:          testProviderUUID,
		Address:       providerAddress.String(),
		PayoutAddress: payoutAddress.String(),
		Active:        true,
	}))
	require.NoError(t, k.SKUSequence.Set(f.Ctx, 4095))
	msgServer := keeper.NewMsgServerImpl(k)

	for _, expected := range []string{
		"018d0cab-c4bb-7fff-8527-a6f2ec2924e5",
		"018d0cab-c4bb-7000-855b-91f2ec560dcf",
	} {
		response, err := msgServer.CreateSKU(f.Ctx, &types.MsgCreateSKU{
			Authority:    authority.String(),
			ProviderUuid: testProviderUUID,
			Name:         "UUID contract SKU",
			Unit:         types.Unit_UNIT_PER_HOUR,
			BasePrice:    sdk.NewInt64Coin("umfx", 3600),
		})
		require.NoError(t, err)
		require.Equal(t, expected, response.Uuid)

		sku, err := k.GetSKU(f.Ctx, expected)
		require.NoError(t, err)
		require.Equal(t, expected, sku.Uuid, "stored identity must match the returned UUID")
	}

	next, err := k.SKUSequence.Peek(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(4097), next)
}
