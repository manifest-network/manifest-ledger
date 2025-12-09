/*
Package types contains unit tests for SKU message validation.

Test Coverage:
- MsgCreateSKU.Validate: validates all fields including price/unit combination
- MsgUpdateSKU.Validate: validates all fields including price/unit combination
- MsgCreateProvider.Validate: validates authority and addresses
- MsgUpdateProvider.Validate: validates authority, ID and addresses
- MsgDeactivateSKU.Validate: validates authority and ID
- MsgDeactivateProvider.Validate: validates authority and ID
*/
package types

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestMsgCreateSKUValidate(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	const testDenom = "upwr"

	tests := []struct {
		name      string
		msg       *MsgCreateSKU
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid: per hour with sufficient price",
			msg: &MsgCreateSKU{
				Authority:  authority.String(),
				ProviderId: 1,
				Name:       "Test SKU",
				Unit:       Unit_UNIT_PER_HOUR,
				BasePrice:  sdk.NewCoin(testDenom, math.NewInt(3600)),
			},
			expectErr: false,
		},
		{
			name: "valid: per day with sufficient price",
			msg: &MsgCreateSKU{
				Authority:  authority.String(),
				ProviderId: 1,
				Name:       "Test SKU",
				Unit:       Unit_UNIT_PER_DAY,
				BasePrice:  sdk.NewCoin(testDenom, math.NewInt(86400)),
			},
			expectErr: false,
		},
		{
			name: "invalid: per hour with too low price (zero per-second rate)",
			msg: &MsgCreateSKU{
				Authority:  authority.String(),
				ProviderId: 1,
				Name:       "Test SKU",
				Unit:       Unit_UNIT_PER_HOUR,
				BasePrice:  sdk.NewCoin(testDenom, math.NewInt(100)),
			},
			expectErr: true,
			errMsg:    "zero per-second rate",
		},
		{
			name: "invalid: per day with too low price (zero per-second rate)",
			msg: &MsgCreateSKU{
				Authority:  authority.String(),
				ProviderId: 1,
				Name:       "Test SKU",
				Unit:       Unit_UNIT_PER_DAY,
				BasePrice:  sdk.NewCoin(testDenom, math.NewInt(1000)),
			},
			expectErr: true,
			errMsg:    "zero per-second rate",
		},
		{
			name: "invalid: authority address",
			msg: &MsgCreateSKU{
				Authority:  "invalid",
				ProviderId: 1,
				Name:       "Test SKU",
				Unit:       Unit_UNIT_PER_HOUR,
				BasePrice:  sdk.NewCoin(testDenom, math.NewInt(3600)),
			},
			expectErr: true,
			errMsg:    "invalid authority address",
		},
		{
			name: "invalid: zero provider ID",
			msg: &MsgCreateSKU{
				Authority:  authority.String(),
				ProviderId: 0,
				Name:       "Test SKU",
				Unit:       Unit_UNIT_PER_HOUR,
				BasePrice:  sdk.NewCoin(testDenom, math.NewInt(3600)),
			},
			expectErr: true,
			errMsg:    "provider_id cannot be zero",
		},
		{
			name: "invalid: empty name",
			msg: &MsgCreateSKU{
				Authority:  authority.String(),
				ProviderId: 1,
				Name:       "",
				Unit:       Unit_UNIT_PER_HOUR,
				BasePrice:  sdk.NewCoin(testDenom, math.NewInt(3600)),
			},
			expectErr: true,
			errMsg:    "name cannot be empty",
		},
		{
			name: "invalid: unspecified unit",
			msg: &MsgCreateSKU{
				Authority:  authority.String(),
				ProviderId: 1,
				Name:       "Test SKU",
				Unit:       Unit_UNIT_UNSPECIFIED,
				BasePrice:  sdk.NewCoin(testDenom, math.NewInt(3600)),
			},
			expectErr: true,
			errMsg:    "unit cannot be unspecified",
		},
		{
			name: "invalid: zero base price",
			msg: &MsgCreateSKU{
				Authority:  authority.String(),
				ProviderId: 1,
				Name:       "Test SKU",
				Unit:       Unit_UNIT_PER_HOUR,
				BasePrice:  sdk.NewCoin(testDenom, math.NewInt(0)),
			},
			expectErr: true,
			errMsg:    "base price must be valid and non-zero",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.Validate()
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgUpdateSKUValidate(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	const testDenom = "upwr"

	tests := []struct {
		name      string
		msg       *MsgUpdateSKU
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid: per hour with sufficient price",
			msg: &MsgUpdateSKU{
				Authority:  authority.String(),
				Id:         1,
				ProviderId: 1,
				Name:       "Updated SKU",
				Unit:       Unit_UNIT_PER_HOUR,
				BasePrice:  sdk.NewCoin(testDenom, math.NewInt(3600)),
				Active:     true,
			},
			expectErr: false,
		},
		{
			name: "valid: per day with sufficient price",
			msg: &MsgUpdateSKU{
				Authority:  authority.String(),
				Id:         1,
				ProviderId: 1,
				Name:       "Updated SKU",
				Unit:       Unit_UNIT_PER_DAY,
				BasePrice:  sdk.NewCoin(testDenom, math.NewInt(86400)),
				Active:     true,
			},
			expectErr: false,
		},
		{
			name: "invalid: per hour with too low price (zero per-second rate)",
			msg: &MsgUpdateSKU{
				Authority:  authority.String(),
				Id:         1,
				ProviderId: 1,
				Name:       "Updated SKU",
				Unit:       Unit_UNIT_PER_HOUR,
				BasePrice:  sdk.NewCoin(testDenom, math.NewInt(100)),
				Active:     true,
			},
			expectErr: true,
			errMsg:    "zero per-second rate",
		},
		{
			name: "invalid: per day with too low price (zero per-second rate)",
			msg: &MsgUpdateSKU{
				Authority:  authority.String(),
				Id:         1,
				ProviderId: 1,
				Name:       "Updated SKU",
				Unit:       Unit_UNIT_PER_DAY,
				BasePrice:  sdk.NewCoin(testDenom, math.NewInt(1000)),
				Active:     true,
			},
			expectErr: true,
			errMsg:    "zero per-second rate",
		},
		{
			name: "invalid: zero SKU ID",
			msg: &MsgUpdateSKU{
				Authority:  authority.String(),
				Id:         0,
				ProviderId: 1,
				Name:       "Updated SKU",
				Unit:       Unit_UNIT_PER_HOUR,
				BasePrice:  sdk.NewCoin(testDenom, math.NewInt(3600)),
				Active:     true,
			},
			expectErr: true,
			errMsg:    "id cannot be zero",
		},
		{
			name: "invalid: zero provider ID",
			msg: &MsgUpdateSKU{
				Authority:  authority.String(),
				Id:         1,
				ProviderId: 0,
				Name:       "Updated SKU",
				Unit:       Unit_UNIT_PER_HOUR,
				BasePrice:  sdk.NewCoin(testDenom, math.NewInt(3600)),
				Active:     true,
			},
			expectErr: true,
			errMsg:    "provider_id cannot be zero",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.Validate()
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgCreateProviderValidate(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()

	tests := []struct {
		name      string
		msg       *MsgCreateProvider
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid",
			msg: &MsgCreateProvider{
				Authority:     authority.String(),
				Address:       providerAddr.String(),
				PayoutAddress: payoutAddr.String(),
			},
			expectErr: false,
		},
		{
			name: "invalid: authority address",
			msg: &MsgCreateProvider{
				Authority:     "invalid",
				Address:       providerAddr.String(),
				PayoutAddress: payoutAddr.String(),
			},
			expectErr: true,
			errMsg:    "invalid authority address",
		},
		{
			name: "invalid: provider address",
			msg: &MsgCreateProvider{
				Authority:     authority.String(),
				Address:       "invalid",
				PayoutAddress: payoutAddr.String(),
			},
			expectErr: true,
			errMsg:    "invalid provider address",
		},
		{
			name: "invalid: payout address",
			msg: &MsgCreateProvider{
				Authority:     authority.String(),
				Address:       providerAddr.String(),
				PayoutAddress: "invalid",
			},
			expectErr: true,
			errMsg:    "invalid payout address",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.Validate()
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgUpdateProviderValidate(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()
	_, _, providerAddr := testdata.KeyTestPubAddr()
	_, _, payoutAddr := testdata.KeyTestPubAddr()

	tests := []struct {
		name      string
		msg       *MsgUpdateProvider
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid",
			msg: &MsgUpdateProvider{
				Authority:     authority.String(),
				Id:            1,
				Address:       providerAddr.String(),
				PayoutAddress: payoutAddr.String(),
				Active:        true,
			},
			expectErr: false,
		},
		{
			name: "invalid: zero ID",
			msg: &MsgUpdateProvider{
				Authority:     authority.String(),
				Id:            0,
				Address:       providerAddr.String(),
				PayoutAddress: payoutAddr.String(),
				Active:        true,
			},
			expectErr: true,
			errMsg:    "id cannot be zero",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.Validate()
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgDeactivateSKUValidate(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()

	tests := []struct {
		name      string
		msg       *MsgDeactivateSKU
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid",
			msg: &MsgDeactivateSKU{
				Authority: authority.String(),
				Id:        1,
			},
			expectErr: false,
		},
		{
			name: "invalid: authority address",
			msg: &MsgDeactivateSKU{
				Authority: "invalid",
				Id:        1,
			},
			expectErr: true,
			errMsg:    "invalid authority address",
		},
		{
			name: "invalid: zero ID",
			msg: &MsgDeactivateSKU{
				Authority: authority.String(),
				Id:        0,
			},
			expectErr: true,
			errMsg:    "id cannot be zero",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.Validate()
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgDeactivateProviderValidate(t *testing.T) {
	_, _, authority := testdata.KeyTestPubAddr()

	tests := []struct {
		name      string
		msg       *MsgDeactivateProvider
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid",
			msg: &MsgDeactivateProvider{
				Authority: authority.String(),
				Id:        1,
			},
			expectErr: false,
		},
		{
			name: "invalid: authority address",
			msg: &MsgDeactivateProvider{
				Authority: "invalid",
				Id:        1,
			},
			expectErr: true,
			errMsg:    "invalid authority address",
		},
		{
			name: "invalid: zero ID",
			msg: &MsgDeactivateProvider{
				Authority: authority.String(),
				Id:        0,
			},
			expectErr: true,
			errMsg:    "id cannot be zero",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.Validate()
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
