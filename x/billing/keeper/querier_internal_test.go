package keeper

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/manifest-network/manifest-ledger/pkg/pagination"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestPaginationErrorCodes(t *testing.T) {
	require.Equal(t, codes.ResourceExhausted,
		status.Code(paginationResultError(pagination.ErrPaginationScanLimitExceeded)))
	require.Equal(t, codes.ResourceExhausted,
		status.Code(paginationRequestError(pagination.ErrPaginationScanLimitExceeded)))
	require.Equal(t, codes.InvalidArgument,
		status.Code(paginationRequestError(pagination.ErrPageKeyAndOffset)))
}

func TestCheckedCreditEstimateItemCount(t *testing.T) {
	count, err := checkedCreditEstimateItemCount(
		"tenant",
		types.MaxCreditEstimateLeaseItems-1,
		1,
	)
	require.NoError(t, err)
	require.Equal(t, types.MaxCreditEstimateLeaseItems, count)

	count, err = checkedCreditEstimateItemCount("tenant", count, 1)
	require.Equal(t, types.MaxCreditEstimateLeaseItems, count)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	require.ErrorContains(t, err, types.ErrLeaseQueryLimitExceeded.Error())

	_, err = checkedCreditEstimateItemCount(
		"tenant",
		types.MaxCreditEstimateLeaseItems+1,
		0,
	)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))

	_, err = checkedCreditEstimateItemCount("tenant", 0, math.MaxInt)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	require.ErrorContains(t, err, types.ErrLeaseQueryLimitExceeded.Error())

	_, err = checkedCreditEstimateItemCount("tenant", 0, -1)
	require.Equal(t, codes.Internal, status.Code(err))
	require.ErrorContains(t, err, types.ErrReservationInvariant.Error())
}
