package types

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func deterministicAllowedList(size int) []string {
	addresses := make([]string, size)
	for i := range addresses {
		addresses[i] = sdk.AccAddress(bytes.Repeat([]byte{byte(i + 1)}, 20)).String()
	}
	return addresses
}

func TestParams_Validate(t *testing.T) {
	_, _, addr1 := testdata.KeyTestPubAddr()
	_, _, addr2 := testdata.KeyTestPubAddr()

	tests := []struct {
		name      string
		params    Params
		expectErr bool
		errMsg    string
	}{
		{
			name:      "valid: empty allowed list",
			params:    Params{AllowedList: []string{}},
			expectErr: false,
		},
		{
			name:      "valid: single address",
			params:    Params{AllowedList: []string{addr1.String()}},
			expectErr: false,
		},
		{
			name:      "valid: multiple addresses",
			params:    Params{AllowedList: []string{addr1.String(), addr2.String()}},
			expectErr: false,
		},
		{
			name:      "invalid: malformed address",
			params:    Params{AllowedList: []string{"invalid_address"}},
			expectErr: true,
			errMsg:    "invalid address in allowed list",
		},
		{
			name:      "invalid: empty string address",
			params:    Params{AllowedList: []string{""}},
			expectErr: true,
			errMsg:    "invalid address in allowed list",
		},
		{
			name:      "invalid: duplicate address",
			params:    Params{AllowedList: []string{addr1.String(), addr1.String()}},
			expectErr: true,
			errMsg:    "duplicate address in allowed list",
		},
		{
			name:      "invalid: duplicate among multiple",
			params:    Params{AllowedList: []string{addr1.String(), addr2.String(), addr1.String()}},
			expectErr: true,
			errMsg:    "duplicate address in allowed list",
		},
		{
			name:      "invalid: equivalent Bech32 spelling",
			params:    Params{AllowedList: []string{addr1.String(), strings.ToUpper(addr1.String())}},
			expectErr: true,
			errMsg:    "duplicate address in allowed list",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.params.Validate()
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestParams_IsAllowed(t *testing.T) {
	_, _, addr1 := testdata.KeyTestPubAddr()
	_, _, addr2 := testdata.KeyTestPubAddr()
	_, _, addr3 := testdata.KeyTestPubAddr()

	tests := []struct {
		name        string
		allowedList []string
		address     string
		expected    bool
	}{
		{
			name:        "empty list allows none",
			allowedList: []string{},
			address:     addr1.String(),
			expected:    false,
		},
		{
			name:        "address in list returns true",
			allowedList: []string{addr1.String(), addr2.String()},
			address:     addr1.String(),
			expected:    true,
		},
		{
			name:        "equivalent Bech32 spelling returns true",
			allowedList: []string{addr1.String()},
			address:     strings.ToUpper(addr1.String()),
			expected:    true,
		},
		{
			name:        "address not in list returns false",
			allowedList: []string{addr1.String()},
			address:     addr2.String(),
			expected:    false,
		},
		{
			name:        "second address in list returns true",
			allowedList: []string{addr1.String(), addr2.String()},
			address:     addr2.String(),
			expected:    true,
		},
		{
			name:        "third address not in two-element list",
			allowedList: []string{addr1.String(), addr2.String()},
			address:     addr3.String(),
			expected:    false,
		},
		{
			name:        "empty address never allowed",
			allowedList: []string{addr1.String()},
			address:     "",
			expected:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			params := Params{AllowedList: tc.allowedList}
			result := params.IsAllowed(tc.address)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestParamsValidateAllowedListCardinality(t *testing.T) {
	atLimit := Params{AllowedList: deterministicAllowedList(MaxAllowedListEntries)}
	require.NoError(t, atLimit.Validate())

	overLimit := Params{AllowedList: deterministicAllowedList(MaxAllowedListEntries + 1)}
	err := overLimit.Validate()
	require.ErrorIs(t, err, ErrInvalidConfig)
	require.Contains(t, err.Error(), "allowed list has 101 entries, maximum allowed is 100")
	require.ErrorIs(t, (&GenesisState{Params: overLimit}).Validate(), ErrInvalidConfig)
}

func TestParamsCanonicalizeAllowedList(t *testing.T) {
	_, _, addr1 := testdata.KeyTestPubAddr()
	_, _, addr2 := testdata.KeyTestPubAddr()
	params := Params{AllowedList: []string{
		strings.ToUpper(addr1.String()),
		addr2.String(),
		addr1.String(),
	}}

	canonical, err := params.CanonicalizeAllowedList()
	require.NoError(t, err)
	require.Equal(t, []string{addr1.String(), addr2.String()}, canonical.AllowedList)
	require.Equal(t, strings.ToUpper(addr1.String()), params.AllowedList[0], "receiver must not be mutated")
}

func TestDefaultParams(t *testing.T) {
	params := DefaultParams()
	require.Empty(t, params.AllowedList, "default allowed list should be empty")
	require.NoError(t, params.Validate(), "default params should be valid")
}
