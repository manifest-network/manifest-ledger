package types

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestGenesisState_Validate(t *testing.T) {
	// Generate valid test addresses using deterministic bytes
	providerAddr := sdk.AccAddress([]byte("provider_address____")).String()
	payoutAddr := sdk.AccAddress([]byte("payout_address______")).String()

	validProvider := Provider{
		Uuid:          "01912345-6789-7abc-8def-0123456789ab",
		Address:       providerAddr,
		PayoutAddress: payoutAddr,
		Active:        true,
	}

	validSKU := SKU{
		Uuid:         "01912345-6789-7abc-8def-0123456789ac",
		ProviderUuid: validProvider.Uuid,
		Name:         "Test SKU",
		Unit:         Unit_UNIT_PER_HOUR,
		BasePrice:    sdk.NewCoin("umfx", math.NewInt(3600)),
		Active:       true,
	}

	tests := []struct {
		name      string
		genesis   *GenesisState
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid: default genesis",
			genesis: &GenesisState{
				Params:    DefaultParams(),
				Providers: []Provider{},
				Skus:      []SKU{},
			},
			expectErr: false,
		},
		{
			name: "valid: with provider and SKU",
			genesis: &GenesisState{
				Params:           DefaultParams(),
				Providers:        []Provider{validProvider},
				Skus:             []SKU{validSKU},
				ProviderSequence: 1,
				SkuSequence:      1,
			},
			expectErr: false,
		},
		{
			name: "invalid: SKU name exceeds max length",
			genesis: &GenesisState{
				Params:    DefaultParams(),
				Providers: []Provider{validProvider},
				Skus: []SKU{
					{
						Uuid:         "01912345-6789-7abc-8def-0123456789ac",
						ProviderUuid: validProvider.Uuid,
						Name:         strings.Repeat("a", MaxSKUNameLength+1),
						Unit:         Unit_UNIT_PER_HOUR,
						BasePrice:    sdk.NewCoin("umfx", math.NewInt(3600)),
						Active:       true,
					},
				},
			},
			expectErr: true,
			errMsg:    "exceeds maximum length",
		},
		{
			name: "valid: SKU name at max length",
			genesis: &GenesisState{
				Params:    DefaultParams(),
				Providers: []Provider{validProvider},
				Skus: []SKU{
					{
						Uuid:         "01912345-6789-7abc-8def-0123456789ac",
						ProviderUuid: validProvider.Uuid,
						Name:         strings.Repeat("a", MaxSKUNameLength),
						Unit:         Unit_UNIT_PER_HOUR,
						BasePrice:    sdk.NewCoin("umfx", math.NewInt(3600)),
						Active:       true,
					},
				},
				ProviderSequence: 1,
				SkuSequence:      1,
			},
			expectErr: false,
		},
		{
			name: "invalid: empty SKU name",
			genesis: &GenesisState{
				Params:    DefaultParams(),
				Providers: []Provider{validProvider},
				Skus: []SKU{
					{
						Uuid:         "01912345-6789-7abc-8def-0123456789ac",
						ProviderUuid: validProvider.Uuid,
						Name:         "",
						Unit:         Unit_UNIT_PER_HOUR,
						BasePrice:    sdk.NewCoin("umfx", math.NewInt(3600)),
						Active:       true,
					},
				},
			},
			expectErr: true,
			errMsg:    "empty name",
		},
		{
			name: "invalid: SKU references non-existent provider",
			genesis: &GenesisState{
				Params:    DefaultParams(),
				Providers: []Provider{},
				Skus:      []SKU{validSKU},
			},
			expectErr: true,
			errMsg:    "references non-existent provider",
		},
		{
			name: "invalid: duplicate provider UUID",
			genesis: &GenesisState{
				Params:    DefaultParams(),
				Providers: []Provider{validProvider, validProvider},
				Skus:      []SKU{},
			},
			expectErr: true,
			errMsg:    "duplicate provider uuid",
		},
		{
			name: "invalid: provider address bad bech32",
			genesis: &GenesisState{
				Params: DefaultParams(),
				Providers: []Provider{
					{
						Uuid:          "01912345-6789-7abc-8def-0123456789ab",
						Address:       "invalid-bech32-address",
						PayoutAddress: payoutAddr,
						Active:        true,
					},
				},
				Skus: []SKU{},
			},
			expectErr: true,
			errMsg:    "invalid address",
		},
		{
			name: "invalid: provider payout address bad bech32",
			genesis: &GenesisState{
				Params: DefaultParams(),
				Providers: []Provider{
					{
						Uuid:          "01912345-6789-7abc-8def-0123456789ab",
						Address:       providerAddr,
						PayoutAddress: "invalid-payout-address",
						Active:        true,
					},
				},
				Skus: []SKU{},
			},
			expectErr: true,
			errMsg:    "invalid payout address",
		},
		{
			name: "invalid: provider API URL not HTTPS",
			genesis: &GenesisState{
				Params: DefaultParams(),
				Providers: []Provider{
					{
						Uuid:          "01912345-6789-7abc-8def-0123456789ab",
						Address:       providerAddr,
						PayoutAddress: payoutAddr,
						ApiUrl:        "http://example.com/api",
						Active:        true,
					},
				},
				Skus: []SKU{},
			},
			expectErr: true,
			errMsg:    "invalid api_url",
		},
		{
			name: "valid: provider with valid HTTPS API URL",
			genesis: &GenesisState{
				Params: DefaultParams(),
				Providers: []Provider{
					{
						Uuid:          "01912345-6789-7abc-8def-0123456789ab",
						Address:       providerAddr,
						PayoutAddress: payoutAddr,
						ApiUrl:        "https://example.com/api",
						Active:        true,
					},
				},
				Skus:             []SKU{},
				ProviderSequence: 1,
			},
			expectErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.genesis.Validate()
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
