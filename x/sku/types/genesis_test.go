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
		{
			name: "invalid: provider meta_hash exceeds max length",
			genesis: &GenesisState{
				Params: DefaultParams(),
				Providers: []Provider{
					{
						Uuid:          "01912345-6789-7abc-8def-0123456789ab",
						Address:       providerAddr,
						PayoutAddress: payoutAddr,
						MetaHash:      make([]byte, MaxMetaHashLength+1),
						Active:        true,
					},
				},
				Skus:             []SKU{},
				ProviderSequence: 1,
			},
			expectErr: true,
			errMsg:    "meta_hash exceeds maximum length",
		},
		{
			name: "valid: provider meta_hash at max length",
			genesis: &GenesisState{
				Params: DefaultParams(),
				Providers: []Provider{
					{
						Uuid:          "01912345-6789-7abc-8def-0123456789ab",
						Address:       providerAddr,
						PayoutAddress: payoutAddr,
						MetaHash:      make([]byte, MaxMetaHashLength),
						Active:        true,
					},
				},
				Skus:             []SKU{},
				ProviderSequence: 1,
			},
			expectErr: false,
		},
		{
			name: "invalid: SKU meta_hash exceeds max length",
			genesis: &GenesisState{
				Params:    DefaultParams(),
				Providers: []Provider{validProvider},
				Skus: []SKU{
					{
						Uuid:         "01912345-6789-7abc-8def-0123456789ac",
						ProviderUuid: validProvider.Uuid,
						Name:         "Test SKU",
						Unit:         Unit_UNIT_PER_HOUR,
						BasePrice:    sdk.NewCoin("umfx", math.NewInt(3600)),
						MetaHash:     make([]byte, MaxMetaHashLength+1),
						Active:       true,
					},
				},
				ProviderSequence: 1,
				SkuSequence:      1,
			},
			expectErr: true,
			errMsg:    "meta_hash exceeds maximum length",
		},
		{
			name: "valid: SKU meta_hash at max length",
			genesis: &GenesisState{
				Params:    DefaultParams(),
				Providers: []Provider{validProvider},
				Skus: []SKU{
					{
						Uuid:         "01912345-6789-7abc-8def-0123456789ac",
						ProviderUuid: validProvider.Uuid,
						Name:         "Test SKU",
						Unit:         Unit_UNIT_PER_HOUR,
						BasePrice:    sdk.NewCoin("umfx", math.NewInt(3600)),
						MetaHash:     make([]byte, MaxMetaHashLength),
						Active:       true,
					},
				},
				ProviderSequence: 1,
				SkuSequence:      1,
			},
			expectErr: false,
		},
		{
			name: "invalid: second SKU meta_hash exceeds max length",
			genesis: &GenesisState{
				Params:    DefaultParams(),
				Providers: []Provider{validProvider},
				Skus: []SKU{
					validSKU,
					{
						Uuid:         "01912345-6789-7abc-8def-0123456789ad",
						ProviderUuid: validProvider.Uuid,
						Name:         "Second SKU",
						Unit:         Unit_UNIT_PER_HOUR,
						BasePrice:    sdk.NewCoin("umfx", math.NewInt(3600)),
						MetaHash:     make([]byte, MaxMetaHashLength+1),
						Active:       true,
					},
				},
				ProviderSequence: 1,
				SkuSequence:      2,
			},
			expectErr: true,
			errMsg:    "meta_hash exceeds maximum length",
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

func TestGenesisStatePrepareForImportCanonicalizesHistoricalAddresses(t *testing.T) {
	manager := sdk.AccAddress([]byte("provider_address____"))
	payout := sdk.AccAddress([]byte("payout_address______"))
	allowed := sdk.AccAddress([]byte("allowed_address_____"))
	allowedAliases := make([]string, MaxAllowedListEntries+1)
	for i := range allowedAliases {
		if i%2 == 0 {
			allowedAliases[i] = strings.ToUpper(allowed.String())
		} else {
			allowedAliases[i] = allowed.String()
		}
	}
	genesis := &GenesisState{
		Params: Params{AllowedList: allowedAliases},
		Providers: []Provider{{
			Uuid:          "01912345-6789-7abc-8def-0123456789ab",
			Address:       strings.ToUpper(manager.String()),
			PayoutAddress: strings.ToUpper(payout.String()),
			Active:        true,
		}},
		ProviderSequence: 1,
	}

	prepared, err := genesis.PrepareForImport()
	require.NoError(t, err)
	require.Equal(t, []string{allowed.String()}, prepared.Params.AllowedList)
	require.Equal(t, manager.String(), prepared.Providers[0].Address)
	require.Equal(t, payout.String(), prepared.Providers[0].PayoutAddress)
	require.Len(t, genesis.Params.AllowedList, MaxAllowedListEntries+1, "source must not be mutated")
	require.Equal(t, strings.ToUpper(manager.String()), genesis.Providers[0].Address, "source must not be mutated")
	require.NoError(t, genesis.Validate(), "historical equivalent aliases remain importable")
}
