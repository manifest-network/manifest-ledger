package keeper

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

type cancellingEstimateBank struct {
	queryWorkBank
	cancel       context.CancelFunc
	balanceReads int
}

func (b *cancellingEstimateBank) GetBalance(ctx context.Context, address sdk.AccAddress, denom string) sdk.Coin {
	b.balanceReads++
	if b.balanceReads == 2 {
		b.cancel()
	}
	return b.queryWorkBank.GetBalance(ctx, address, denom)
}

// This context cancels synchronously at a chosen checkpoint so the regression
// exercises cancellation during item scanning without timing or goroutines.
type estimateCheckpointContext struct {
	context.Context
	cancel     context.CancelFunc
	checks     int
	cancelWhen int
}

func (c *estimateCheckpointContext) Err() error {
	c.checks++
	if c.checks == c.cancelWhen {
		c.cancel()
	}
	return c.Context.Err()
}

func TestCreditEstimateCancellation(t *testing.T) {
	q, ctx, tenant := setupCreditEstimateWork(t, 2, 4, 4)
	request := &types.QueryCreditEstimateRequest{Tenant: tenant}
	t.Run("cancelled transport before any SDK access", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		response, err := q.CreditEstimate(cancelled, request)
		require.Nil(t, response)
		require.Equal(t, codes.Canceled, status.Code(err))
	})
	t.Run("deadline", func(t *testing.T) {
		expired, cancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
		defer cancel()
		response, err := q.CreditEstimate(expired, request)
		require.Nil(t, response)
		require.Equal(t, codes.DeadlineExceeded, status.Code(err))
	})
	t.Run("during item scan", func(t *testing.T) {
		parent, cancel := context.WithCancel(context.WithValue(context.Background(), sdk.SdkContextKey, ctx))
		defer cancel()
		checkpoint := &estimateCheckpointContext{Context: parent, cancel: cancel, cancelWhen: 4}
		response, err := q.CreditEstimate(checkpoint, request)
		require.Nil(t, response)
		require.Equal(t, codes.Canceled, status.Code(err))
		require.Equal(t, 4, checkpoint.checks, "stop at the item checkpoint that cancelled the request")
	})
	t.Run("during bank reads", func(t *testing.T) {
		parent, cancel := context.WithCancel(context.WithValue(context.Background(), sdk.SdkContextKey, ctx))
		defer cancel()
		bank := &cancellingEstimateBank{cancel: cancel}
		query := q
		query.k.bankKeeper = bank
		response, err := query.CreditEstimate(parent, request)
		require.Nil(t, response)
		require.Equal(t, codes.Canceled, status.Code(err))
		require.Equal(t, 2, bank.balanceReads)
	})
}

func TestCreditEstimateStreamingRates(t *testing.T) {
	q, ctx, tenant := setupCreditEstimateWork(t, 4, 2, 3)
	response, err := q.CreditEstimate(ctx, &types.QueryCreditEstimateRequest{Tenant: tenant})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(
		sdk.NewInt64Coin("udenom000000", 3), sdk.NewInt64Coin("udenom000001", 3), sdk.NewInt64Coin("udenom000002", 2),
	), response.TotalRatePerSecond)
	require.Equal(t, uint64(333_333_333), response.EstimatedDurationSeconds)

	lease, err := q.k.Leases.Get(ctx, "01912345-6789-7abc-8abc-000000000001")
	require.NoError(t, err)
	maximum := sdkmath.NewIntFromBigInt(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)))
	lease.Items[0].LockedPrice.Amount = maximum
	require.NoError(t, q.k.SetLease(ctx, lease))
	response, err = q.CreditEstimate(ctx, &types.QueryCreditEstimateRequest{Tenant: tenant})
	require.Nil(t, response)
	require.Equal(t, codes.Internal, status.Code(err))
	require.ErrorContains(t, err, types.ErrArithmeticOverflow.Error())
}
