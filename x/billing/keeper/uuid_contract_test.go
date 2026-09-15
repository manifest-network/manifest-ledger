package keeper_test

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// TestCreateLeaseUUIDContract pins the real keeper's namespace and sequence
// allocation to identifiers captured from the manual implementation at 85d1602.
func TestCreateLeaseUUIDContract(t *testing.T) {
	f := initFixture(t)
	headerHash, err := hex.DecodeString("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	f.Ctx = f.Ctx.
		WithBlockTime(time.Date(2024, 1, 15, 10, 30, 0, 123456789, time.UTC)).
		WithHeaderHash(headerHash).
		WithChainID("manifest-1")

	tenant, providerAddress, payoutAddress := f.TestAccs[0], f.TestAccs[1], f.TestAccs[2]
	provider := f.createTestProvider(t, providerAddress.String(), payoutAddress.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)
	creditAddress := types.DeriveCreditAddress(tenant)
	f.fundAccount(t, creditAddress, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 1_000_000)))
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddress.String(),
	}))
	require.NoError(t, f.App.BillingKeeper.LeaseSequence.Set(f.Ctx, 4095))
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	for _, expected := range []string{
		"018d0cab-c4bb-7fff-9773-a4cb522415e7",
		"018d0cab-c4bb-7000-9746-7bcb51fce251",
	} {
		response, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)
		require.Equal(t, expected, response.LeaseUuid)

		lease, err := f.App.BillingKeeper.GetLease(f.Ctx, expected)
		require.NoError(t, err)
		require.Equal(t, expected, lease.Uuid, "stored identity must match the returned UUID")
	}

	next, err := f.App.BillingKeeper.LeaseSequence.Peek(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(4097), next)
}
