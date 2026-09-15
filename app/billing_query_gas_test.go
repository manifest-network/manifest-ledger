package app

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	abci "github.com/cometbft/cometbft/abci/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"

	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
)

// Exercise the SDK's exposed query wrappers, not an injected keeper meter.
// The 100-item leases consume about 20k gas each through real stores/codecs.
func TestBillingQueryGasBoundsABCIAndNativeGRPC(t *testing.T) {
	ctx, manifest := setupCrisisApp(t)
	const limit uint64 = 5_000_000
	baseapp.SetQueryGasLimit(limit)(manifest.BaseApp)
	tenants := make([]sdk.AccAddress, 3)
	tenantBytes := []byte{0x51, 0x52, 0x53}
	for index, count := range []uint64{100, 300, 1} {
		tenant := sdk.AccAddress(bytes.Repeat([]byte{tenantBytes[index]}, 20))
		tenants[index] = tenant
		require.NoError(t, manifest.BillingKeeper.SetCreditAccount(ctx, billingtypes.CreditAccount{
			Tenant: tenant.String(), CreditAddress: billingtypes.DeriveCreditAddress(tenant).String(), ActiveLeaseCount: count,
		}))
		for leaseIndex := range count {
			items := make([]billingtypes.LeaseItem, 100)
			for itemIndex := range items {
				items[itemIndex] = billingtypes.LeaseItem{
					SkuUuid: fmt.Sprintf("01912345-6789-7abc-8def-%012x", itemIndex+1), Quantity: 1,
					LockedPrice: sdk.NewInt64Coin(fmt.Sprintf("udenom%06d", itemIndex), 1),
				}
			}
			require.NoError(t, manifest.BillingKeeper.SetLease(ctx, billingtypes.Lease{
				Uuid:   fmt.Sprintf("01912345-6789-7abc-8ab%d-%012x", index, leaseIndex+1),
				Tenant: tenant.String(), ProviderUuid: "01912345-6789-7abc-8def-0123456789ad", Items: items,
				State: billingtypes.LEASE_STATE_ACTIVE, CreatedAt: ctx.BlockTime(), LastSettledAt: ctx.BlockTime(),
				AcknowledgedAt: new(ctx.BlockTime()), MinLeaseDurationAtCreation: 1,
				Reservation: &billingtypes.LeaseReservation{RemainingAmounts: sdk.NewCoins()},
			}))
		}
	}
	// A short lease scan can still reach a large auth account. This valid
	// schedule fits under the historical 22MB transaction byte ceiling; its
	// encoded state read alone must exceed the 5M query budget before decoding.
	const periodCount = 150_000
	periods := make(vestingtypes.Periods, periodCount)
	for index := range periods {
		periods[index] = vestingtypes.Period{Length: 1, Amount: sdk.NewCoins(sdk.NewInt64Coin("umfx", 1))}
	}
	creditAddress := billingtypes.DeriveCreditAddress(tenants[2])
	original := sdk.NewCoins(sdk.NewInt64Coin("umfx", periodCount))
	vesting, err := vestingtypes.NewPeriodicVestingAccount(
		authtypes.NewBaseAccountWithAddress(creditAddress), original, ctx.BlockTime().Unix()-periodCount+10, periods,
	)
	require.NoError(t, err)
	require.Greater(t, vesting.Size(), int(limit/3), "SDK charges three gas per read byte before account decoding")
	manifest.AccountKeeper.SetAccount(ctx, manifest.AccountKeeper.NewAccount(ctx, vesting))
	require.NoError(t, manifest.BankKeeper.MintCoins(ctx, "mint", original))
	require.NoError(t, manifest.BankKeeper.SendCoinsFromModuleToAccount(ctx, "mint", creditAddress, original))
	t.Logf("periodic account: %d periods, %d encoded bytes", periodCount, vesting.Size())

	_, err = manifest.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 2, Time: ctx.BlockTime().Add(time.Second)})
	require.NoError(t, err)
	_, err = manifest.Commit()
	require.NoError(t, err)

	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	manifest.RegisterGRPCServer(server)
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); require.NoError(t, listener.Close()); <-serveErr })
	connection, err := grpc.NewClient("passthrough:///billing-query-gas", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	client := billingtypes.NewQueryClient(connection)

	for index, tenant := range tenants {
		request := &billingtypes.QueryCreditEstimateRequest{Tenant: tenant.String()}
		encoded, err := manifest.AppCodec().Marshal(request)
		require.NoError(t, err)
		abciResponse, err := manifest.Query(context.Background(), &abci.RequestQuery{
			Path: "/liftedinit.billing.v1.Query/CreditEstimate", Data: encoded,
		})
		require.NoError(t, err)
		transport, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		grpcResponse, grpcErr := client.CreditEstimate(transport, request)
		cancel()
		if index == 0 {
			require.Zero(t, abciResponse.Code, abciResponse.Log)
			require.NoError(t, grpcErr)
			require.Equal(t, uint64(100), grpcResponse.ActiveLeaseCount)
		} else {
			require.Equal(t, sdkerrors.ErrPanic.ABCICode(), abciResponse.Code, abciResponse.Log)
			require.Contains(t, abciResponse.Log, "ReadPerByte", "SDK out-of-gas panic identifies the exhausted read charge")
			require.Nil(t, grpcResponse)
			// The pinned SDK native server recovers out-of-gas panics as Internal.
			require.Equal(t, codes.Internal, status.Code(grpcErr))
			require.Contains(t, grpcErr.Error(), "ReadPerByte")
		}
	}
}
