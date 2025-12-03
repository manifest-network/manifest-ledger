// Package types_test contains unit tests for the SKU module types.
//
// Tests in this file cover:
//
// Params:
//   - TestParamsValidate: Validates Params.Validate() with empty lists, valid/invalid addresses, and duplicates
//   - TestParamsIsAllowed: Tests Params.IsAllowed() for checking address membership in allowed list
//   - TestDefaultParams: Verifies DefaultParams() returns valid empty defaults
//
// Genesis:
//   - TestGenesisStateValidate: Validates GenesisState with valid/invalid SKUs, duplicate IDs,
//     ID >= NextId violations, empty fields, invalid prices, and bad params
//   - TestNewGenesisState: Tests NewGenesisState() constructor
//
// Messages:
//   - TestMsgCreateSKUValidate: Validates MsgCreateSKU with valid messages and all error cases
//     (invalid authority, empty provider/name, unspecified unit, zero price)
//   - TestMsgUpdateSKUValidate: Validates MsgUpdateSKU with valid messages and error cases
//     (invalid authority, zero ID, empty provider)
//   - TestMsgDeleteSKUValidate: Validates MsgDeleteSKU with valid messages and error cases
//   - TestMsgUpdateParamsValidate: Validates MsgUpdateParams with valid/invalid authority and params
//   - TestMsgGetSigners: Verifies GetSigners() returns correct signer for all message types
//   - TestMsgRouteAndType: Verifies Route() and Type() return correct values for all messages
//   - TestNewMsgConstructors: Tests all New*() message constructors
//
// Unit Enum:
//   - TestUnitJSONMarshaling: Tests Unit.MarshalJSON() for all unit values
//   - TestUnitJSONUnmarshaling: Tests Unit.UnmarshalJSON() with string names, integers, and invalid inputs
package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	appparams "github.com/manifest-network/manifest-ledger/app/params"
	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

func init() {
	appparams.SetAddressPrefixes()
}

func TestParamsValidate(t *testing.T) {
	tests := []struct {
		name    string
		params  types.Params
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid empty allowed list",
			params:  types.Params{AllowedList: []string{}},
			wantErr: false,
		},
		{
			name:    "valid single address",
			params:  types.Params{AllowedList: []string{"manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct"}},
			wantErr: false,
		},
		{
			name: "valid multiple addresses",
			params: types.Params{AllowedList: []string{
				"manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct",
				"manifest1efd63aw40lxf3n4mhf7dzhjkr453axurm6rp3z",
			}},
			wantErr: false,
		},
		{
			name:    "invalid address format",
			params:  types.Params{AllowedList: []string{"invalid-address"}},
			wantErr: true,
			errMsg:  "invalid address",
		},
		{
			name: "duplicate addresses",
			params: types.Params{AllowedList: []string{
				"manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct",
				"manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct",
			}},
			wantErr: true,
			errMsg:  "duplicate address",
		},
		{
			name: "one valid one invalid",
			params: types.Params{AllowedList: []string{
				"manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct",
				"bad-address",
			}},
			wantErr: true,
			errMsg:  "invalid address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.params.Validate()
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestParamsIsAllowed(t *testing.T) {
	addr1 := "manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct"
	addr2 := "manifest1efd63aw40lxf3n4mhf7dzhjkr453axurm6rp3z"
	addr3 := "manifest1sc0aw0e6mcrm7ex5v3ll5gh4dq5whn3acmkupn"

	params := types.Params{AllowedList: []string{addr1, addr2}}

	require.True(t, params.IsAllowed(addr1))
	require.True(t, params.IsAllowed(addr2))
	require.False(t, params.IsAllowed(addr3))
	require.False(t, params.IsAllowed(""))
}

func TestDefaultParams(t *testing.T) {
	params := types.DefaultParams()
	require.Empty(t, params.AllowedList)
	require.NoError(t, params.Validate())
}

func TestGenesisStateValidate(t *testing.T) {
	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	tests := []struct {
		name    string
		genesis *types.GenesisState
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid default genesis",
			genesis: types.DefaultGenesis(),
			wantErr: false,
		},
		{
			name: "valid genesis with SKUs",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Skus: []types.SKU{
					{
						Id:        1,
						Provider:  "provider1",
						Name:      "SKU 1",
						Unit:      types.Unit_UNIT_PER_HOUR,
						BasePrice: basePrice,
						Active:    true,
					},
					{
						Id:        2,
						Provider:  "provider2",
						Name:      "SKU 2",
						Unit:      types.Unit_UNIT_PER_DAY,
						BasePrice: basePrice,
						Active:    false,
					},
				},
				NextId: 3,
			},
			wantErr: false,
		},
		{
			name: "invalid: duplicate SKU IDs",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Skus: []types.SKU{
					{Id: 1, Provider: "p1", Name: "SKU 1", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: true},
					{Id: 1, Provider: "p2", Name: "SKU 2", Unit: types.Unit_UNIT_PER_DAY, BasePrice: basePrice, Active: true},
				},
				NextId: 2,
			},
			wantErr: true,
			errMsg:  "duplicate sku id",
		},
		{
			name: "invalid: SKU ID >= NextId",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Skus: []types.SKU{
					{Id: 5, Provider: "p1", Name: "SKU", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: true},
				},
				NextId: 3,
			},
			wantErr: true,
			errMsg:  "greater than or equal to next_id",
		},
		{
			name: "invalid: empty provider",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Skus: []types.SKU{
					{Id: 1, Provider: "", Name: "SKU", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: true},
				},
				NextId: 2,
			},
			wantErr: true,
			errMsg:  "empty provider",
		},
		{
			name: "invalid: empty name",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Skus: []types.SKU{
					{Id: 1, Provider: "p1", Name: "", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: true},
				},
				NextId: 2,
			},
			wantErr: true,
			errMsg:  "empty name",
		},
		{
			name: "invalid: unspecified unit",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Skus: []types.SKU{
					{Id: 1, Provider: "p1", Name: "SKU", Unit: types.Unit_UNIT_UNSPECIFIED, BasePrice: basePrice, Active: true},
				},
				NextId: 2,
			},
			wantErr: true,
			errMsg:  "unspecified unit",
		},
		{
			name: "invalid: zero base price",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Skus: []types.SKU{
					{Id: 1, Provider: "p1", Name: "SKU", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: sdk.NewCoin("umfx", sdkmath.NewInt(0)), Active: true},
				},
				NextId: 2,
			},
			wantErr: true,
			errMsg:  "invalid or zero base price",
		},
		{
			name: "invalid: params with bad address",
			genesis: &types.GenesisState{
				Params: types.Params{AllowedList: []string{"invalid-address"}},
				Skus:   []types.SKU{},
				NextId: 1,
			},
			wantErr: true,
			errMsg:  "invalid params",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.genesis.Validate()
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNewGenesisState(t *testing.T) {
	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))
	params := types.Params{AllowedList: []string{"manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct"}}
	skus := []types.SKU{
		{Id: 1, Provider: "p1", Name: "SKU 1", Unit: types.Unit_UNIT_PER_HOUR, BasePrice: basePrice, Active: true},
	}

	gs := types.NewGenesisState(params, skus, 2)

	require.Equal(t, params, gs.Params)
	require.Equal(t, skus, gs.Skus)
	require.Equal(t, uint64(2), gs.NextId)
}

func TestMsgCreateSKUValidate(t *testing.T) {
	validAddr := "manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct"
	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	tests := []struct {
		name    string
		msg     *types.MsgCreateSKU
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid message",
			msg: &types.MsgCreateSKU{
				Authority: validAddr,
				Provider:  "provider1",
				Name:      "Test SKU",
				Unit:      types.Unit_UNIT_PER_HOUR,
				BasePrice: basePrice,
			},
			wantErr: false,
		},
		{
			name: "invalid authority address",
			msg: &types.MsgCreateSKU{
				Authority: "invalid",
				Provider:  "provider1",
				Name:      "Test SKU",
				Unit:      types.Unit_UNIT_PER_HOUR,
				BasePrice: basePrice,
			},
			wantErr: true,
			errMsg:  "invalid authority address",
		},
		{
			name: "empty provider",
			msg: &types.MsgCreateSKU{
				Authority: validAddr,
				Provider:  "",
				Name:      "Test SKU",
				Unit:      types.Unit_UNIT_PER_HOUR,
				BasePrice: basePrice,
			},
			wantErr: true,
			errMsg:  "provider cannot be empty",
		},
		{
			name: "empty name",
			msg: &types.MsgCreateSKU{
				Authority: validAddr,
				Provider:  "provider1",
				Name:      "",
				Unit:      types.Unit_UNIT_PER_HOUR,
				BasePrice: basePrice,
			},
			wantErr: true,
			errMsg:  "name cannot be empty",
		},
		{
			name: "unspecified unit",
			msg: &types.MsgCreateSKU{
				Authority: validAddr,
				Provider:  "provider1",
				Name:      "Test SKU",
				Unit:      types.Unit_UNIT_UNSPECIFIED,
				BasePrice: basePrice,
			},
			wantErr: true,
			errMsg:  "unit cannot be unspecified",
		},
		{
			name: "zero base price",
			msg: &types.MsgCreateSKU{
				Authority: validAddr,
				Provider:  "provider1",
				Name:      "Test SKU",
				Unit:      types.Unit_UNIT_PER_HOUR,
				BasePrice: sdk.NewCoin("umfx", sdkmath.NewInt(0)),
			},
			wantErr: true,
			errMsg:  "base price must be valid and non-zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.Validate()
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgUpdateSKUValidate(t *testing.T) {
	validAddr := "manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct"
	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	tests := []struct {
		name    string
		msg     *types.MsgUpdateSKU
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid message",
			msg: &types.MsgUpdateSKU{
				Authority: validAddr,
				Provider:  "provider1",
				Id:        1,
				Name:      "Updated SKU",
				Unit:      types.Unit_UNIT_PER_DAY,
				BasePrice: basePrice,
				Active:    true,
			},
			wantErr: false,
		},
		{
			name: "invalid authority address",
			msg: &types.MsgUpdateSKU{
				Authority: "invalid",
				Provider:  "provider1",
				Id:        1,
				Name:      "Updated SKU",
				Unit:      types.Unit_UNIT_PER_DAY,
				BasePrice: basePrice,
				Active:    true,
			},
			wantErr: true,
			errMsg:  "invalid authority address",
		},
		{
			name: "zero ID",
			msg: &types.MsgUpdateSKU{
				Authority: validAddr,
				Provider:  "provider1",
				Id:        0,
				Name:      "Updated SKU",
				Unit:      types.Unit_UNIT_PER_DAY,
				BasePrice: basePrice,
				Active:    true,
			},
			wantErr: true,
			errMsg:  "id cannot be zero",
		},
		{
			name: "empty provider",
			msg: &types.MsgUpdateSKU{
				Authority: validAddr,
				Provider:  "",
				Id:        1,
				Name:      "Updated SKU",
				Unit:      types.Unit_UNIT_PER_DAY,
				BasePrice: basePrice,
				Active:    true,
			},
			wantErr: true,
			errMsg:  "provider cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.Validate()
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgDeleteSKUValidate(t *testing.T) {
	validAddr := "manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct"

	tests := []struct {
		name    string
		msg     *types.MsgDeleteSKU
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid message",
			msg: &types.MsgDeleteSKU{
				Authority: validAddr,
				Provider:  "provider1",
				Id:        1,
			},
			wantErr: false,
		},
		{
			name: "invalid authority address",
			msg: &types.MsgDeleteSKU{
				Authority: "invalid",
				Provider:  "provider1",
				Id:        1,
			},
			wantErr: true,
			errMsg:  "invalid authority address",
		},
		{
			name: "zero ID",
			msg: &types.MsgDeleteSKU{
				Authority: validAddr,
				Provider:  "provider1",
				Id:        0,
			},
			wantErr: true,
			errMsg:  "id cannot be zero",
		},
		{
			name: "empty provider",
			msg: &types.MsgDeleteSKU{
				Authority: validAddr,
				Provider:  "",
				Id:        1,
			},
			wantErr: true,
			errMsg:  "provider cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.Validate()
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMsgUpdateParamsValidate(t *testing.T) {
	validAddr := "manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct"

	tests := []struct {
		name    string
		msg     *types.MsgUpdateParams
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid message with empty allowed list",
			msg: &types.MsgUpdateParams{
				Authority: validAddr,
				Params:    types.Params{AllowedList: []string{}},
			},
			wantErr: false,
		},
		{
			name: "valid message with allowed list",
			msg: &types.MsgUpdateParams{
				Authority: validAddr,
				Params:    types.Params{AllowedList: []string{validAddr}},
			},
			wantErr: false,
		},
		{
			name: "invalid authority address",
			msg: &types.MsgUpdateParams{
				Authority: "invalid",
				Params:    types.Params{AllowedList: []string{}},
			},
			wantErr: true,
			errMsg:  "invalid authority address",
		},
		{
			name: "invalid params - bad address in allowed list",
			msg: &types.MsgUpdateParams{
				Authority: validAddr,
				Params:    types.Params{AllowedList: []string{"bad-address"}},
			},
			wantErr: true,
			errMsg:  "invalid address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.Validate()
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestUnitJSONMarshaling(t *testing.T) {
	tests := []struct {
		name     string
		unit     types.Unit
		expected string
	}{
		{
			name:     "UNIT_UNSPECIFIED",
			unit:     types.Unit_UNIT_UNSPECIFIED,
			expected: `"UNIT_UNSPECIFIED"`,
		},
		{
			name:     "UNIT_PER_HOUR",
			unit:     types.Unit_UNIT_PER_HOUR,
			expected: `"UNIT_PER_HOUR"`,
		},
		{
			name:     "UNIT_PER_DAY",
			unit:     types.Unit_UNIT_PER_DAY,
			expected: `"UNIT_PER_DAY"`,
		},
		{
			name:     "UNIT_PER_MONTH",
			unit:     types.Unit_UNIT_PER_MONTH,
			expected: `"UNIT_PER_MONTH"`,
		},
		{
			name:     "UNIT_PER_UNIT",
			unit:     types.Unit_UNIT_PER_UNIT,
			expected: `"UNIT_PER_UNIT"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := tt.unit.MarshalJSON()
			require.NoError(t, err)
			require.Equal(t, tt.expected, string(data))
		})
	}
}

func TestUnitJSONUnmarshaling(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected types.Unit
		wantErr  bool
	}{
		{
			name:     "string UNIT_UNSPECIFIED",
			input:    `"UNIT_UNSPECIFIED"`,
			expected: types.Unit_UNIT_UNSPECIFIED,
		},
		{
			name:     "string UNIT_PER_HOUR",
			input:    `"UNIT_PER_HOUR"`,
			expected: types.Unit_UNIT_PER_HOUR,
		},
		{
			name:     "string UNIT_PER_DAY",
			input:    `"UNIT_PER_DAY"`,
			expected: types.Unit_UNIT_PER_DAY,
		},
		{
			name:     "string UNIT_PER_MONTH",
			input:    `"UNIT_PER_MONTH"`,
			expected: types.Unit_UNIT_PER_MONTH,
		},
		{
			name:     "string UNIT_PER_UNIT",
			input:    `"UNIT_PER_UNIT"`,
			expected: types.Unit_UNIT_PER_UNIT,
		},
		{
			name:     "integer 0",
			input:    `0`,
			expected: types.Unit_UNIT_UNSPECIFIED,
		},
		{
			name:     "integer 1",
			input:    `1`,
			expected: types.Unit_UNIT_PER_HOUR,
		},
		{
			name:     "integer 2",
			input:    `2`,
			expected: types.Unit_UNIT_PER_DAY,
		},
		{
			name:    "invalid string",
			input:   `"INVALID_UNIT"`,
			wantErr: true,
		},
		{
			name:    "invalid type",
			input:   `true`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var unit types.Unit
			err := unit.UnmarshalJSON([]byte(tt.input))
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, unit)
			}
		})
	}
}

func TestMsgGetSigners(t *testing.T) {
	validAddr := "manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct"
	expectedAddr, _ := sdk.AccAddressFromBech32(validAddr)

	t.Run("MsgCreateSKU", func(t *testing.T) {
		msg := &types.MsgCreateSKU{Authority: validAddr}
		signers := msg.GetSigners()
		require.Len(t, signers, 1)
		require.Equal(t, expectedAddr, signers[0])
	})

	t.Run("MsgUpdateSKU", func(t *testing.T) {
		msg := &types.MsgUpdateSKU{Authority: validAddr}
		signers := msg.GetSigners()
		require.Len(t, signers, 1)
		require.Equal(t, expectedAddr, signers[0])
	})

	t.Run("MsgDeleteSKU", func(t *testing.T) {
		msg := &types.MsgDeleteSKU{Authority: validAddr}
		signers := msg.GetSigners()
		require.Len(t, signers, 1)
		require.Equal(t, expectedAddr, signers[0])
	})

	t.Run("MsgUpdateParams", func(t *testing.T) {
		msg := &types.MsgUpdateParams{Authority: validAddr}
		signers := msg.GetSigners()
		require.Len(t, signers, 1)
		require.Equal(t, expectedAddr, signers[0])
	})
}

func TestMsgRouteAndType(t *testing.T) {
	t.Run("MsgCreateSKU", func(t *testing.T) {
		msg := &types.MsgCreateSKU{}
		require.Equal(t, types.ModuleName, msg.Route())
		require.Equal(t, "create_sku", msg.Type())
	})

	t.Run("MsgUpdateSKU", func(t *testing.T) {
		msg := &types.MsgUpdateSKU{}
		require.Equal(t, types.ModuleName, msg.Route())
		require.Equal(t, "update_sku", msg.Type())
	})

	t.Run("MsgDeleteSKU", func(t *testing.T) {
		msg := &types.MsgDeleteSKU{}
		require.Equal(t, types.ModuleName, msg.Route())
		require.Equal(t, "delete_sku", msg.Type())
	})

	t.Run("MsgUpdateParams", func(t *testing.T) {
		msg := &types.MsgUpdateParams{}
		require.Equal(t, types.ModuleName, msg.Route())
		require.Equal(t, "update_params", msg.Type())
	})
}

func TestNewMsgConstructors(t *testing.T) {
	validAddr := "manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct"
	basePrice := sdk.NewCoin("umfx", sdkmath.NewInt(100))

	t.Run("NewMsgCreateSKU", func(t *testing.T) {
		msg := types.NewMsgCreateSKU(validAddr, "provider", "name", types.Unit_UNIT_PER_HOUR, basePrice, []byte("hash"))
		require.Equal(t, validAddr, msg.Authority)
		require.Equal(t, "provider", msg.Provider)
		require.Equal(t, "name", msg.Name)
		require.Equal(t, types.Unit_UNIT_PER_HOUR, msg.Unit)
		require.Equal(t, basePrice, msg.BasePrice)
		require.Equal(t, []byte("hash"), msg.MetaHash)
	})

	t.Run("NewMsgUpdateSKU", func(t *testing.T) {
		msg := types.NewMsgUpdateSKU(validAddr, "provider", 1, "name", types.Unit_UNIT_PER_DAY, basePrice, []byte("hash"), true)
		require.Equal(t, validAddr, msg.Authority)
		require.Equal(t, "provider", msg.Provider)
		require.Equal(t, uint64(1), msg.Id)
		require.Equal(t, "name", msg.Name)
		require.Equal(t, types.Unit_UNIT_PER_DAY, msg.Unit)
		require.Equal(t, basePrice, msg.BasePrice)
		require.Equal(t, []byte("hash"), msg.MetaHash)
		require.True(t, msg.Active)
	})

	t.Run("NewMsgDeleteSKU", func(t *testing.T) {
		msg := types.NewMsgDeleteSKU(validAddr, "provider", 1)
		require.Equal(t, validAddr, msg.Authority)
		require.Equal(t, "provider", msg.Provider)
		require.Equal(t, uint64(1), msg.Id)
	})

	t.Run("NewMsgUpdateParams", func(t *testing.T) {
		params := types.Params{AllowedList: []string{validAddr}}
		msg := types.NewMsgUpdateParams(validAddr, params)
		require.Equal(t, validAddr, msg.Authority)
		require.Equal(t, params, msg.Params)
	})
}
