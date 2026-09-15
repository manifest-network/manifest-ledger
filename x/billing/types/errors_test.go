package types

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/errors"
)

// ABCI codes are a public client contract. Pin explicit numbers, including
// after SDK wrapping, so reordering or reusing registrations
// cannot silently change transaction error handling in generated clients.
func TestModuleErrorABCICodes(t *testing.T) {
	tests := []struct {
		err  *errors.Error
		code uint32
	}{
		{ErrInvalidParams, 1},
		{ErrLeaseNotFound, 2},
		{ErrLeaseNotActive, 3},
		{ErrInsufficientCredit, 4},
		{ErrMaxLeasesReached, 5},
		{ErrUnauthorized, 6},
		{ErrReserved7, 7},
		{ErrCreditAccountNotFound, 8},
		{ErrInvalidLease, 9},
		{ErrSKUNotFound, 10},
		{ErrSKUNotActive, 11},
		{ErrProviderNotFound, 12},
		{ErrProviderNotActive, 13},
		{ErrMixedProviders, 14},
		{ErrNoWithdrawableAmount, 15},
		{ErrEmptyLeaseItems, 16},
		{ErrInvalidQuantity, 17},
		{ErrDuplicateSKU, 18},
		{ErrInvalidCreditOperation, 19},
		{ErrArithmeticOverflow, 20},
		{ErrTooManyLeaseItems, 21},
		{ErrLeaseNotPending, 22},
		{ErrMaxPendingLeasesReached, 23},
		{ErrInvalidRejectionReason, 24},
		{ErrInvalidRequest, 25},
		{ErrInvalidClosureReason, 26},
		{ErrInvalidMetaHash, 27},
		{ErrInvalidServiceName, 28},
		{ErrInvalidCustomDomain, 29},
		{ErrCustomDomainAlreadyClaimed, 30},
		{ErrLeaseNotEditable, 31},
		{ErrLeaseItemNotFound, 32},
		{ErrAmbiguousLeaseItem, 33},
		{ErrLeaseAcknowledgementDeadlineExceeded, 34},
		{ErrLeaseAcknowledgementActiveCapExceeded, 35},
		{ErrReservationInvariant, 36},
		{ErrLeaseQueryLimitExceeded, 37},
		{ErrReservationDenomLimitExceeded, 38},
		{ErrSequenceExhausted, 39},
		{ErrInternalCorruption, 40},
	}
	for _, test := range tests {
		t.Run(test.err.Error(), func(t *testing.T) {
			for _, err := range []error{test.err, test.err.Wrap("context"), errors.Wrap(test.err.Wrap("inner context"), "outer context")} {
				codespace, code, _ := errors.ABCIInfo(err, false)
				require.Equal(t, "billing", codespace)
				require.Equal(t, test.code, code)
			}
		})
	}

	require.Same(t, ErrArithmeticOverflow, ErrReserved20, "the deprecated name must keep its ABCI identity")
}
