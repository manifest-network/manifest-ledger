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
		{ErrInvalidSKU, 1},
		{ErrSKUNotFound, 2},
		{ErrUnauthorized, 3},
		{ErrInvalidConfig, 4},
		{ErrInvalidProvider, 5},
		{ErrProviderNotFound, 6},
		{ErrInvalidAPIURL, 7},
		{ErrSequenceExhausted, 8},
		{ErrInternalCorruption, 9},
	}
	for _, test := range tests {
		t.Run(test.err.Error(), func(t *testing.T) {
			for _, err := range []error{test.err, test.err.Wrap("context"), errors.Wrap(test.err.Wrap("inner context"), "outer context")} {
				codespace, code, _ := errors.ABCIInfo(err, false)
				require.Equal(t, "sku", codespace)
				require.Equal(t, test.code, code)
			}
		})
	}
}
