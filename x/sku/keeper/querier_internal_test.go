package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/manifest-network/manifest-ledger/pkg/pagination"
)

func TestPaginationErrorCodes(t *testing.T) {
	require.Equal(t, codes.ResourceExhausted,
		status.Code(paginationResultError(pagination.ErrPaginationScanLimitExceeded)))
	require.Equal(t, codes.ResourceExhausted,
		status.Code(paginationRequestError(pagination.ErrPaginationScanLimitExceeded)))
	require.Equal(t, codes.InvalidArgument,
		status.Code(paginationRequestError(pagination.ErrPageKeyAndOffset)))
}
