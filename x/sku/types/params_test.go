package types

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/testutil/testdata"
)

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

func TestDefaultParams(t *testing.T) {
	params := DefaultParams()
	require.Empty(t, params.AllowedList, "default allowed list should be empty")
	require.NoError(t, params.Validate(), "default params should be valid")
}
