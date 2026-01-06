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
				Authority:    authority.String(),
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Name:         "Test SKU",
				Unit:         Unit_UNIT_PER_HOUR,
				BasePrice:    sdk.NewCoin(testDenom, math.NewInt(3600)),
			},
			expectErr: false,
		},
		{
			name: "valid: per day with sufficient price",
			msg: &MsgCreateSKU{
				Authority:    authority.String(),
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Name:         "Test SKU",
				Unit:         Unit_UNIT_PER_DAY,
				BasePrice:    sdk.NewCoin(testDenom, math.NewInt(86400)),
			},
			expectErr: false,
		},
		{
			name: "invalid: per hour with too low price (zero per-second rate)",
			msg: &MsgCreateSKU{
				Authority:    authority.String(),
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Name:         "Test SKU",
				Unit:         Unit_UNIT_PER_HOUR,
				BasePrice:    sdk.NewCoin(testDenom, math.NewInt(100)),
			},
			expectErr: true,
			errMsg:    "zero per-second rate",
		},
		{
			name: "invalid: per day with too low price (zero per-second rate)",
			msg: &MsgCreateSKU{
				Authority:    authority.String(),
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Name:         "Test SKU",
				Unit:         Unit_UNIT_PER_DAY,
				BasePrice:    sdk.NewCoin(testDenom, math.NewInt(1000)),
			},
			expectErr: true,
			errMsg:    "zero per-second rate",
		},
		{
			name: "invalid: per hour not evenly divisible (3601)",
			msg: &MsgCreateSKU{
				Authority:    authority.String(),
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Name:         "Test SKU",
				Unit:         Unit_UNIT_PER_HOUR,
				BasePrice:    sdk.NewCoin(testDenom, math.NewInt(3601)),
			},
			expectErr: true,
			errMsg:    "not evenly divisible",
		},
		{
			name: "invalid: per day not evenly divisible (86401)",
			msg: &MsgCreateSKU{
				Authority:    authority.String(),
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Name:         "Test SKU",
				Unit:         Unit_UNIT_PER_DAY,
				BasePrice:    sdk.NewCoin(testDenom, math.NewInt(86401)),
			},
			expectErr: true,
			errMsg:    "not evenly divisible",
		},
		{
			name: "invalid: per hour not evenly divisible (5000)",
			msg: &MsgCreateSKU{
				Authority:    authority.String(),
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Name:         "Test SKU",
				Unit:         Unit_UNIT_PER_HOUR,
				BasePrice:    sdk.NewCoin(testDenom, math.NewInt(5000)),
			},
			expectErr: true,
			errMsg:    "not evenly divisible",
		},
		{
			name: "invalid: authority address",
			msg: &MsgCreateSKU{
				Authority:    "invalid",
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Name:         "Test SKU",
				Unit:         Unit_UNIT_PER_HOUR,
				BasePrice:    sdk.NewCoin(testDenom, math.NewInt(3600)),
			},
			expectErr: true,
			errMsg:    "invalid authority address",
		},
		{
			name: "invalid: zero provider ID",
			msg: &MsgCreateSKU{
				Authority:    authority.String(),
				ProviderUuid: "",
				Name:         "Test SKU",
				Unit:         Unit_UNIT_PER_HOUR,
				BasePrice:    sdk.NewCoin(testDenom, math.NewInt(3600)),
			},
			expectErr: true,
			errMsg:    "invalid provider_uuid",
		},
		{
			name: "invalid: empty name",
			msg: &MsgCreateSKU{
				Authority:    authority.String(),
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Name:         "",
				Unit:         Unit_UNIT_PER_HOUR,
				BasePrice:    sdk.NewCoin(testDenom, math.NewInt(3600)),
			},
			expectErr: true,
			errMsg:    "name cannot be empty",
		},
		{
			name: "invalid: name exceeds max length",
			msg: &MsgCreateSKU{
				Authority:    authority.String(),
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Name:         string(make([]byte, MaxSKUNameLength+1)),
				Unit:         Unit_UNIT_PER_HOUR,
				BasePrice:    sdk.NewCoin(testDenom, math.NewInt(3600)),
			},
			expectErr: true,
			errMsg:    "exceeds maximum length",
		},
		{
			name: "invalid: unspecified unit",
			msg: &MsgCreateSKU{
				Authority:    authority.String(),
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Name:         "Test SKU",
				Unit:         Unit_UNIT_UNSPECIFIED,
				BasePrice:    sdk.NewCoin(testDenom, math.NewInt(3600)),
			},
			expectErr: true,
			errMsg:    "unit cannot be unspecified",
		},
		{
			name: "invalid: zero base price",
			msg: &MsgCreateSKU{
				Authority:    authority.String(),
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Name:         "Test SKU",
				Unit:         Unit_UNIT_PER_HOUR,
				BasePrice:    sdk.NewCoin(testDenom, math.NewInt(0)),
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
				Authority:    authority.String(),
				Uuid:         "01912345-6789-7abc-8def-0123456789ac",
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Name:         "Updated SKU",
				Unit:         Unit_UNIT_PER_HOUR,
				BasePrice:    sdk.NewCoin(testDenom, math.NewInt(3600)),
				Active:       true,
			},
			expectErr: false,
		},
		{
			name: "valid: per day with sufficient price",
			msg: &MsgUpdateSKU{
				Authority:    authority.String(),
				Uuid:         "01912345-6789-7abc-8def-0123456789ac",
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Name:         "Updated SKU",
				Unit:         Unit_UNIT_PER_DAY,
				BasePrice:    sdk.NewCoin(testDenom, math.NewInt(86400)),
				Active:       true,
			},
			expectErr: false,
		},
		{
			name: "invalid: per hour with too low price (zero per-second rate)",
			msg: &MsgUpdateSKU{
				Authority:    authority.String(),
				Uuid:         "01912345-6789-7abc-8def-0123456789ac",
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Name:         "Updated SKU",
				Unit:         Unit_UNIT_PER_HOUR,
				BasePrice:    sdk.NewCoin(testDenom, math.NewInt(100)),
				Active:       true,
			},
			expectErr: true,
			errMsg:    "zero per-second rate",
		},
		{
			name: "invalid: per day with too low price (zero per-second rate)",
			msg: &MsgUpdateSKU{
				Authority:    authority.String(),
				Uuid:         "01912345-6789-7abc-8def-0123456789ac",
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Name:         "Updated SKU",
				Unit:         Unit_UNIT_PER_DAY,
				BasePrice:    sdk.NewCoin(testDenom, math.NewInt(1000)),
				Active:       true,
			},
			expectErr: true,
			errMsg:    "zero per-second rate",
		},
		{
			name: "invalid: per hour not evenly divisible (3601)",
			msg: &MsgUpdateSKU{
				Authority:    authority.String(),
				Uuid:         "01912345-6789-7abc-8def-0123456789ac",
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Name:         "Updated SKU",
				Unit:         Unit_UNIT_PER_HOUR,
				BasePrice:    sdk.NewCoin(testDenom, math.NewInt(3601)),
				Active:       true,
			},
			expectErr: true,
			errMsg:    "not evenly divisible",
		},
		{
			name: "invalid: per day not evenly divisible (86401)",
			msg: &MsgUpdateSKU{
				Authority:    authority.String(),
				Uuid:         "01912345-6789-7abc-8def-0123456789ac",
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Name:         "Updated SKU",
				Unit:         Unit_UNIT_PER_DAY,
				BasePrice:    sdk.NewCoin(testDenom, math.NewInt(86401)),
				Active:       true,
			},
			expectErr: true,
			errMsg:    "not evenly divisible",
		},
		{
			name: "invalid: per hour not evenly divisible (5000)",
			msg: &MsgUpdateSKU{
				Authority:    authority.String(),
				Uuid:         "01912345-6789-7abc-8def-0123456789ac",
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Name:         "Updated SKU",
				Unit:         Unit_UNIT_PER_HOUR,
				BasePrice:    sdk.NewCoin(testDenom, math.NewInt(5000)),
				Active:       true,
			},
			expectErr: true,
			errMsg:    "not evenly divisible",
		},
		{
			name: "invalid: zero SKU ID",
			msg: &MsgUpdateSKU{
				Authority:    authority.String(),
				Uuid:         "",
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Name:         "Updated SKU",
				Unit:         Unit_UNIT_PER_HOUR,
				BasePrice:    sdk.NewCoin(testDenom, math.NewInt(3600)),
				Active:       true,
			},
			expectErr: true,
			errMsg:    "uuid cannot be empty",
		},
		{
			name: "invalid: zero provider ID",
			msg: &MsgUpdateSKU{
				Authority:    authority.String(),
				Uuid:         "01912345-6789-7abc-8def-0123456789ac",
				ProviderUuid: "",
				Name:         "Updated SKU",
				Unit:         Unit_UNIT_PER_HOUR,
				BasePrice:    sdk.NewCoin(testDenom, math.NewInt(3600)),
				Active:       true,
			},
			expectErr: true,
			errMsg:    "invalid provider_uuid",
		},
		{
			name: "invalid: name exceeds max length",
			msg: &MsgUpdateSKU{
				Authority:    authority.String(),
				Uuid:         "01912345-6789-7abc-8def-0123456789ac",
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Name:         string(make([]byte, MaxSKUNameLength+1)),
				Unit:         Unit_UNIT_PER_HOUR,
				BasePrice:    sdk.NewCoin(testDenom, math.NewInt(3600)),
				Active:       true,
			},
			expectErr: true,
			errMsg:    "exceeds maximum length",
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
				Uuid:          "01912345-6789-7abc-8def-0123456789ac",
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
				Uuid:          "",
				Address:       providerAddr.String(),
				PayoutAddress: payoutAddr.String(),
				Active:        true,
			},
			expectErr: true,
			errMsg:    "uuid cannot be empty",
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
				Uuid:      "01912345-6789-7abc-8def-0123456789ac",
			},
			expectErr: false,
		},
		{
			name: "invalid: authority address",
			msg: &MsgDeactivateSKU{
				Authority: "invalid",
				Uuid:      "01912345-6789-7abc-8def-0123456789ac",
			},
			expectErr: true,
			errMsg:    "invalid authority address",
		},
		{
			name: "invalid: zero ID",
			msg: &MsgDeactivateSKU{
				Authority: authority.String(),
				Uuid:      "",
			},
			expectErr: true,
			errMsg:    "uuid cannot be empty",
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
				Uuid:      "01912345-6789-7abc-8def-0123456789ac",
			},
			expectErr: false,
		},
		{
			name: "invalid: authority address",
			msg: &MsgDeactivateProvider{
				Authority: "invalid",
				Uuid:      "01912345-6789-7abc-8def-0123456789ac",
			},
			expectErr: true,
			errMsg:    "invalid authority address",
		},
		{
			name: "invalid: zero ID",
			msg: &MsgDeactivateProvider{
				Authority: authority.String(),
				Uuid:      "",
			},
			expectErr: true,
			errMsg:    "uuid cannot be empty",
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

func TestValidateAPIURL(t *testing.T) {
	tests := []struct {
		name      string
		apiURL    string
		expectErr bool
		errMsg    string
	}{
		{
			name:      "valid: simple HTTPS URL",
			apiURL:    "https://example.com",
			expectErr: false,
		},
		{
			name:      "valid: HTTPS URL with path",
			apiURL:    "https://api.example.com/v1/leases",
			expectErr: false,
		},
		{
			name:      "valid: HTTPS URL with port",
			apiURL:    "https://example.com:8443",
			expectErr: false,
		},
		{
			name:      "valid: HTTPS URL with query params",
			apiURL:    "https://example.com/api?version=1",
			expectErr: false,
		},
		{
			name:      "invalid: HTTP URL (not HTTPS)",
			apiURL:    "http://example.com",
			expectErr: true,
			errMsg:    "must use HTTPS scheme",
		},
		{
			name:      "invalid: no scheme",
			apiURL:    "example.com",
			expectErr: true,
			errMsg:    "must use HTTPS scheme",
		},
		{
			name:      "invalid: empty host",
			apiURL:    "https://",
			expectErr: true,
			errMsg:    "must have a valid host",
		},
		{
			name:      "invalid: FTP scheme",
			apiURL:    "ftp://example.com",
			expectErr: true,
			errMsg:    "must use HTTPS scheme",
		},
		{
			name:      "invalid: contains credentials",
			apiURL:    "https://user:pass@example.com",
			expectErr: true,
			errMsg:    "must not contain user credentials",
		},
		{
			name:      "invalid: URL too long",
			apiURL:    "https://example.com/" + string(make([]byte, MaxAPIURLLength)),
			expectErr: true,
			errMsg:    "exceeds maximum length",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateAPIURL(tc.apiURL)
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgCreateProviderValidateNewFields(t *testing.T) {
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
			name: "valid: with api_url",
			msg: &MsgCreateProvider{
				Authority:     authority.String(),
				Address:       providerAddr.String(),
				PayoutAddress: payoutAddr.String(),
				ApiUrl:        "https://api.provider.com",
			},
			expectErr: false,
		},
		{
			name: "valid: without optional fields (defaults)",
			msg: &MsgCreateProvider{
				Authority:     authority.String(),
				Address:       providerAddr.String(),
				PayoutAddress: payoutAddr.String(),
			},
			expectErr: false,
		},
		{
			name: "invalid: HTTP API URL",
			msg: &MsgCreateProvider{
				Authority:     authority.String(),
				Address:       providerAddr.String(),
				PayoutAddress: payoutAddr.String(),
				ApiUrl:        "http://api.provider.com",
			},
			expectErr: true,
			errMsg:    "must use HTTPS scheme",
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

func TestMsgUpdateProviderValidateNewFields(t *testing.T) {
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
			name: "valid: with api_url",
			msg: &MsgUpdateProvider{
				Authority:     authority.String(),
				Uuid:          "01912345-6789-7abc-8def-0123456789ac",
				Address:       providerAddr.String(),
				PayoutAddress: payoutAddr.String(),
				Active:        true,
				ApiUrl:        "https://api.provider.com",
			},
			expectErr: false,
		},
		{
			name: "valid: without optional fields (keep existing)",
			msg: &MsgUpdateProvider{
				Authority:     authority.String(),
				Uuid:          "01912345-6789-7abc-8def-0123456789ac",
				Address:       providerAddr.String(),
				PayoutAddress: payoutAddr.String(),
				Active:        true,
			},
			expectErr: false,
		},
		{
			name: "invalid: HTTP API URL",
			msg: &MsgUpdateProvider{
				Authority:     authority.String(),
				Uuid:          "01912345-6789-7abc-8def-0123456789ac",
				Address:       providerAddr.String(),
				PayoutAddress: payoutAddr.String(),
				Active:        true,
				ApiUrl:        "http://api.provider.com",
			},
			expectErr: true,
			errMsg:    "must use HTTPS scheme",
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
