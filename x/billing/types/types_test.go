/*
Package types tests for the billing module.

Test Coverage:
1. Params - Parameter validation (denom, min_credit_balance, max_leases_per_tenant)
2. Msgs - ValidateBasic for all message types
3. Credit - Credit address derivation determinism and correctness
4. Genesis - Genesis state validation including leases and credit accounts
*/
package types_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

const (
	testDenom   = "factory/manifest1afk9zr2hn2jsac63h4hm60vl9z3e5u69gndzf7c99cqge3vzwjzsfmy9qj/upwr"
	invalidAddr = "invalid-address"
)

// ============================================================================
// Params Tests
// ============================================================================

func TestParams_DefaultParams(t *testing.T) {
	params := types.DefaultParams()

	require.Equal(t, types.DefaultDenom, params.Denom)
	require.Equal(t, types.DefaultMinCreditBalance, params.MinCreditBalance)
	require.Equal(t, types.DefaultMaxLeasesPerTenant, params.MaxLeasesPerTenant)
}

func TestParams_NewParams(t *testing.T) {
	denom := "utest"
	minBalance := math.NewInt(1000)
	maxLeases := uint64(50)

	params := types.NewParams(denom, minBalance, maxLeases)

	require.Equal(t, denom, params.Denom)
	require.Equal(t, minBalance, params.MinCreditBalance)
	require.Equal(t, maxLeases, params.MaxLeasesPerTenant)
}

func TestParams_Validate(t *testing.T) {
	tests := []struct {
		name      string
		params    types.Params
		expectErr bool
		errMsg    string
	}{
		{
			name:      "valid default params",
			params:    types.DefaultParams(),
			expectErr: false,
		},
		{
			name:      "valid custom params",
			params:    types.NewParams("utest", math.NewInt(100), 10),
			expectErr: false,
		},
		{
			name:      "valid zero min credit balance",
			params:    types.NewParams(testDenom, math.ZeroInt(), 10),
			expectErr: false,
		},
		{
			name:      "empty denom",
			params:    types.NewParams("", math.NewInt(100), 10),
			expectErr: true,
			errMsg:    "denom cannot be empty",
		},
		{
			name:      "nil min credit balance",
			params:    types.Params{Denom: testDenom, MaxLeasesPerTenant: 10},
			expectErr: true,
			errMsg:    "min_credit_balance cannot be nil or negative",
		},
		{
			name:      "negative min credit balance",
			params:    types.NewParams(testDenom, math.NewInt(-1), 10),
			expectErr: true,
			errMsg:    "min_credit_balance cannot be nil or negative",
		},
		{
			name:      "zero max leases per tenant",
			params:    types.NewParams(testDenom, math.NewInt(100), 0),
			expectErr: true,
			errMsg:    "max_leases_per_tenant must be greater than zero",
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

// ============================================================================
// MsgFundCredit Tests
// ============================================================================

func TestMsgFundCredit_ValidateBasic(t *testing.T) {
	_, _, senderAddr := testdata.KeyTestPubAddr()
	_, _, tenantAddr := testdata.KeyTestPubAddr()
	sender := senderAddr.String()
	tenant := tenantAddr.String()

	tests := []struct {
		name      string
		msg       types.MsgFundCredit
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid message",
			msg: types.MsgFundCredit{
				Sender: sender,
				Tenant: tenant,
				Amount: sdk.NewCoin(testDenom, math.NewInt(1000)),
			},
			expectErr: false,
		},
		{
			name: "invalid sender address",
			msg: types.MsgFundCredit{
				Sender: invalidAddr,
				Tenant: tenant,
				Amount: sdk.NewCoin(testDenom, math.NewInt(1000)),
			},
			expectErr: true,
			errMsg:    "invalid sender address",
		},
		{
			name: "empty sender address",
			msg: types.MsgFundCredit{
				Sender: "",
				Tenant: tenant,
				Amount: sdk.NewCoin(testDenom, math.NewInt(1000)),
			},
			expectErr: true,
			errMsg:    "invalid sender address",
		},
		{
			name: "invalid tenant address",
			msg: types.MsgFundCredit{
				Sender: sender,
				Tenant: invalidAddr,
				Amount: sdk.NewCoin(testDenom, math.NewInt(1000)),
			},
			expectErr: true,
			errMsg:    "invalid tenant address",
		},
		{
			name: "empty tenant address",
			msg: types.MsgFundCredit{
				Sender: sender,
				Tenant: "",
				Amount: sdk.NewCoin(testDenom, math.NewInt(1000)),
			},
			expectErr: true,
			errMsg:    "invalid tenant address",
		},
		{
			name: "zero amount",
			msg: types.MsgFundCredit{
				Sender: sender,
				Tenant: tenant,
				Amount: sdk.NewCoin(testDenom, math.ZeroInt()),
			},
			expectErr: true,
			errMsg:    "amount must be positive",
		},
		{
			name: "negative amount",
			msg: types.MsgFundCredit{
				Sender: sender,
				Tenant: tenant,
				Amount: sdk.Coin{Denom: testDenom, Amount: math.NewInt(-100)},
			},
			expectErr: true,
			errMsg:    "amount must be positive",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ============================================================================
// MsgCreateLease Tests
// ============================================================================

func TestMsgCreateLease_ValidateBasic(t *testing.T) {
	_, _, tenantAddr := testdata.KeyTestPubAddr()
	tenant := tenantAddr.String()

	validItems := []types.LeaseItemInput{
		{SkuId: 1, Quantity: 2},
		{SkuId: 2, Quantity: 1},
	}

	tests := []struct {
		name      string
		msg       types.MsgCreateLease
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid message",
			msg: types.MsgCreateLease{
				Tenant: tenant,
				Items:  validItems,
			},
			expectErr: false,
		},
		{
			name: "valid single item",
			msg: types.MsgCreateLease{
				Tenant: tenant,
				Items:  []types.LeaseItemInput{{SkuId: 1, Quantity: 1}},
			},
			expectErr: false,
		},
		{
			name: "invalid tenant address",
			msg: types.MsgCreateLease{
				Tenant: invalidAddr,
				Items:  validItems,
			},
			expectErr: true,
			errMsg:    "invalid tenant address",
		},
		{
			name: "empty tenant address",
			msg: types.MsgCreateLease{
				Tenant: "",
				Items:  validItems,
			},
			expectErr: true,
			errMsg:    "invalid tenant address",
		},
		{
			name: "empty items",
			msg: types.MsgCreateLease{
				Tenant: tenant,
				Items:  []types.LeaseItemInput{},
			},
			expectErr: true,
			errMsg:    "lease must contain at least one item",
		},
		{
			name: "nil items",
			msg: types.MsgCreateLease{
				Tenant: tenant,
				Items:  nil,
			},
			expectErr: true,
			errMsg:    "lease must contain at least one item",
		},
		{
			name: "item with zero sku_id",
			msg: types.MsgCreateLease{
				Tenant: tenant,
				Items:  []types.LeaseItemInput{{SkuId: 0, Quantity: 1}},
			},
			expectErr: true,
			errMsg:    "has zero sku_id",
		},
		{
			name: "item with zero quantity",
			msg: types.MsgCreateLease{
				Tenant: tenant,
				Items:  []types.LeaseItemInput{{SkuId: 1, Quantity: 0}},
			},
			expectErr: true,
			errMsg:    "has zero quantity",
		},
		{
			name: "duplicate sku_id",
			msg: types.MsgCreateLease{
				Tenant: tenant,
				Items: []types.LeaseItemInput{
					{SkuId: 1, Quantity: 1},
					{SkuId: 1, Quantity: 2},
				},
			},
			expectErr: true,
			errMsg:    "appears multiple times",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ============================================================================
// MsgCloseLease Tests
// ============================================================================

func TestMsgCloseLease_ValidateBasic(t *testing.T) {
	_, _, senderAddr := testdata.KeyTestPubAddr()
	sender := senderAddr.String()

	tests := []struct {
		name      string
		msg       types.MsgCloseLease
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid message",
			msg: types.MsgCloseLease{
				Sender:  sender,
				LeaseId: 1,
			},
			expectErr: false,
		},
		{
			name: "invalid sender address",
			msg: types.MsgCloseLease{
				Sender:  invalidAddr,
				LeaseId: 1,
			},
			expectErr: true,
			errMsg:    "invalid sender address",
		},
		{
			name: "empty sender address",
			msg: types.MsgCloseLease{
				Sender:  "",
				LeaseId: 1,
			},
			expectErr: true,
			errMsg:    "invalid sender address",
		},
		{
			name: "zero lease_id",
			msg: types.MsgCloseLease{
				Sender:  sender,
				LeaseId: 0,
			},
			expectErr: true,
			errMsg:    "lease_id cannot be zero",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ============================================================================
// MsgWithdraw Tests
// ============================================================================

func TestMsgWithdraw_ValidateBasic(t *testing.T) {
	_, _, senderAddr := testdata.KeyTestPubAddr()
	sender := senderAddr.String()

	tests := []struct {
		name      string
		msg       types.MsgWithdraw
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid message",
			msg: types.MsgWithdraw{
				Sender:  sender,
				LeaseId: 1,
			},
			expectErr: false,
		},
		{
			name: "invalid sender address",
			msg: types.MsgWithdraw{
				Sender:  invalidAddr,
				LeaseId: 1,
			},
			expectErr: true,
			errMsg:    "invalid sender address",
		},
		{
			name: "empty sender address",
			msg: types.MsgWithdraw{
				Sender:  "",
				LeaseId: 1,
			},
			expectErr: true,
			errMsg:    "invalid sender address",
		},
		{
			name: "zero lease_id",
			msg: types.MsgWithdraw{
				Sender:  sender,
				LeaseId: 0,
			},
			expectErr: true,
			errMsg:    "lease_id cannot be zero",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ============================================================================
// MsgWithdrawAll Tests
// ============================================================================

func TestMsgWithdrawAll_ValidateBasic(t *testing.T) {
	_, _, senderAddr := testdata.KeyTestPubAddr()
	sender := senderAddr.String()

	tests := []struct {
		name      string
		msg       types.MsgWithdrawAll
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid message with provider_id",
			msg: types.MsgWithdrawAll{
				Sender:     sender,
				ProviderId: 1,
			},
			expectErr: false,
		},
		{
			name: "valid message with zero provider_id (sender is provider)",
			msg: types.MsgWithdrawAll{
				Sender:     sender,
				ProviderId: 0,
			},
			expectErr: false,
		},
		{
			name: "invalid sender address",
			msg: types.MsgWithdrawAll{
				Sender:     invalidAddr,
				ProviderId: 1,
			},
			expectErr: true,
			errMsg:    "invalid sender address",
		},
		{
			name: "empty sender address",
			msg: types.MsgWithdrawAll{
				Sender:     "",
				ProviderId: 1,
			},
			expectErr: true,
			errMsg:    "invalid sender address",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ============================================================================
// MsgUpdateParams Tests
// ============================================================================

func TestMsgUpdateParams_ValidateBasic(t *testing.T) {
	_, _, authorityAddr := testdata.KeyTestPubAddr()
	authority := authorityAddr.String()
	validParams := types.DefaultParams()

	tests := []struct {
		name      string
		msg       types.MsgUpdateParams
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid message",
			msg: types.MsgUpdateParams{
				Authority: authority,
				Params:    validParams,
			},
			expectErr: false,
		},
		{
			name: "invalid authority address",
			msg: types.MsgUpdateParams{
				Authority: invalidAddr,
				Params:    validParams,
			},
			expectErr: true,
			errMsg:    "invalid authority address",
		},
		{
			name: "empty authority address",
			msg: types.MsgUpdateParams{
				Authority: "",
				Params:    validParams,
			},
			expectErr: true,
			errMsg:    "invalid authority address",
		},
		{
			name: "invalid params - empty denom",
			msg: types.MsgUpdateParams{
				Authority: authority,
				Params:    types.NewParams("", math.NewInt(100), 10),
			},
			expectErr: true,
			errMsg:    "denom cannot be empty",
		},
		{
			name: "invalid params - zero max leases",
			msg: types.MsgUpdateParams{
				Authority: authority,
				Params:    types.NewParams(testDenom, math.NewInt(100), 0),
			},
			expectErr: true,
			errMsg:    "max_leases_per_tenant must be greater than zero",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ============================================================================
// Credit Address Tests
// ============================================================================

func TestDeriveCreditAddress(t *testing.T) {
	_, _, tenantAddr := testdata.KeyTestPubAddr()
	_, _, tenant2Addr := testdata.KeyTestPubAddr()

	// Test determinism - same input should produce same output
	addr1 := types.DeriveCreditAddress(tenantAddr)
	addr2 := types.DeriveCreditAddress(tenantAddr)
	require.Equal(t, addr1, addr2, "credit address derivation should be deterministic")

	// Test different tenants produce different addresses
	addr3 := types.DeriveCreditAddress(tenant2Addr)
	require.NotEqual(t, addr1, addr3, "different tenants should have different credit addresses")

	// Test address is valid
	require.NotEmpty(t, addr1)
	require.NotEmpty(t, addr1.String())
}

func TestDeriveCreditAddressFromBech32(t *testing.T) {
	_, _, tenantAddr := testdata.KeyTestPubAddr()
	tenant := tenantAddr.String()

	tests := []struct {
		name      string
		tenant    string
		expectErr bool
	}{
		{
			name:      "valid address",
			tenant:    tenant,
			expectErr: false,
		},
		{
			name:      "invalid address",
			tenant:    invalidAddr,
			expectErr: true,
		},
		{
			name:      "empty address",
			tenant:    "",
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			addr, err := types.DeriveCreditAddressFromBech32(tc.tenant)
			if tc.expectErr {
				require.Error(t, err)
				require.Nil(t, addr)
			} else {
				require.NoError(t, err)
				require.NotNil(t, addr)
				require.NotEmpty(t, addr.String())
			}
		})
	}
}

func TestCreditAddressConsistency(t *testing.T) {
	_, _, tenantAddr := testdata.KeyTestPubAddr()
	tenant := tenantAddr.String()

	// Verify that DeriveCreditAddress and DeriveCreditAddressFromBech32 produce the same result
	addr1 := types.DeriveCreditAddress(tenantAddr)
	addr2, err := types.DeriveCreditAddressFromBech32(tenant)
	require.NoError(t, err)

	require.Equal(t, addr1, addr2, "both derivation methods should produce the same address")
}

// ============================================================================
// Genesis Tests
// ============================================================================

func TestGenesisState_DefaultGenesis(t *testing.T) {
	gs := types.DefaultGenesis()

	require.NotNil(t, gs)
	require.Equal(t, types.DefaultParams(), gs.Params)
	require.Empty(t, gs.Leases)
	require.Empty(t, gs.CreditAccounts)
	require.Equal(t, uint64(1), gs.NextLeaseId)

	// Default genesis should be valid
	require.NoError(t, gs.Validate())
}

func TestGenesisState_NewGenesisState(t *testing.T) {
	_, _, tenantAddr := testdata.KeyTestPubAddr()
	tenant := tenantAddr.String()

	params := types.NewParams("utest", math.NewInt(100), 50)
	now := time.Now().UTC()

	creditAddr := types.DeriveCreditAddress(tenantAddr)

	leases := []types.Lease{
		{
			Id:         1,
			Tenant:     tenant,
			ProviderId: 1,
			Items: []types.LeaseItem{
				{SkuId: 1, Quantity: 1, LockedPrice: math.NewInt(100)},
			},
			State:         types.LEASE_STATE_ACTIVE,
			CreatedAt:     now,
			LastSettledAt: now,
		},
	}

	creditAccounts := []types.CreditAccount{
		{
			Tenant:        tenant,
			CreditAddress: creditAddr.String(),
		},
	}

	gs := types.NewGenesisState(params, leases, creditAccounts, 2)

	require.Equal(t, params, gs.Params)
	require.Equal(t, leases, gs.Leases)
	require.Equal(t, creditAccounts, gs.CreditAccounts)
	require.Equal(t, uint64(2), gs.NextLeaseId)
}

func TestGenesisState_Validate(t *testing.T) {
	_, _, tenantAddr := testdata.KeyTestPubAddr()
	_, _, tenant2Addr := testdata.KeyTestPubAddr()
	tenant := tenantAddr.String()
	tenant2 := tenant2Addr.String()

	now := time.Now().UTC()
	closedAt := now.Add(time.Hour)

	creditAddr := types.DeriveCreditAddress(tenantAddr)
	creditAddr2 := types.DeriveCreditAddress(tenant2Addr)

	validLease := types.Lease{
		Id:         1,
		Tenant:     tenant,
		ProviderId: 1,
		Items: []types.LeaseItem{
			{SkuId: 1, Quantity: 1, LockedPrice: math.NewInt(100)},
		},
		State:         types.LEASE_STATE_ACTIVE,
		CreatedAt:     now,
		LastSettledAt: now,
	}

	validCreditAccount := types.CreditAccount{
		Tenant:        tenant,
		CreditAddress: creditAddr.String(),
	}

	tests := []struct {
		name      string
		genesis   *types.GenesisState
		expectErr bool
		errMsg    string
	}{
		{
			name:      "valid default genesis",
			genesis:   types.DefaultGenesis(),
			expectErr: false,
		},
		{
			name: "valid genesis with data",
			genesis: &types.GenesisState{
				Params:         types.DefaultParams(),
				Leases:         []types.Lease{validLease},
				CreditAccounts: []types.CreditAccount{validCreditAccount},
				NextLeaseId:    2,
			},
			expectErr: false,
		},
		{
			name: "invalid params",
			genesis: &types.GenesisState{
				Params:      types.NewParams("", math.NewInt(100), 10),
				NextLeaseId: 1,
			},
			expectErr: true,
			errMsg:    "invalid params",
		},
		{
			name: "zero next_lease_id",
			genesis: &types.GenesisState{
				Params:      types.DefaultParams(),
				NextLeaseId: 0,
			},
			expectErr: true,
			errMsg:    "next_lease_id cannot be zero",
		},
		{
			name: "duplicate lease id",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					validLease,
					{
						Id:         1, // duplicate
						Tenant:     tenant2,
						ProviderId: 1,
						Items: []types.LeaseItem{
							{SkuId: 2, Quantity: 1, LockedPrice: math.NewInt(100)},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
				NextLeaseId: 3,
			},
			expectErr: true,
			errMsg:    "duplicate lease id",
		},
		{
			name: "lease id >= next_lease_id",
			genesis: &types.GenesisState{
				Params:      types.DefaultParams(),
				Leases:      []types.Lease{validLease},
				NextLeaseId: 1, // should be > lease.Id
			},
			expectErr: true,
			errMsg:    "greater than or equal to next_lease_id",
		},
		{
			name: "lease with empty tenant",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Id:         1,
						Tenant:     "",
						ProviderId: 1,
						Items: []types.LeaseItem{
							{SkuId: 1, Quantity: 1, LockedPrice: math.NewInt(100)},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
				NextLeaseId: 2,
			},
			expectErr: true,
			errMsg:    "has empty tenant",
		},
		{
			name: "lease with invalid tenant address",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Id:         1,
						Tenant:     invalidAddr,
						ProviderId: 1,
						Items: []types.LeaseItem{
							{SkuId: 1, Quantity: 1, LockedPrice: math.NewInt(100)},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
				NextLeaseId: 2,
			},
			expectErr: true,
			errMsg:    "invalid tenant address",
		},
		{
			name: "lease with zero provider_id",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Id:         1,
						Tenant:     tenant,
						ProviderId: 0,
						Items: []types.LeaseItem{
							{SkuId: 1, Quantity: 1, LockedPrice: math.NewInt(100)},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
				NextLeaseId: 2,
			},
			expectErr: true,
			errMsg:    "has zero provider_id",
		},
		{
			name: "lease with no items",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Id:            1,
						Tenant:        tenant,
						ProviderId:    1,
						Items:         []types.LeaseItem{},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
				NextLeaseId: 2,
			},
			expectErr: true,
			errMsg:    "has no items",
		},
		{
			name: "lease item with zero sku_id",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Id:         1,
						Tenant:     tenant,
						ProviderId: 1,
						Items: []types.LeaseItem{
							{SkuId: 0, Quantity: 1, LockedPrice: math.NewInt(100)},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
				NextLeaseId: 2,
			},
			expectErr: true,
			errMsg:    "has zero sku_id",
		},
		{
			name: "lease item with zero quantity",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Id:         1,
						Tenant:     tenant,
						ProviderId: 1,
						Items: []types.LeaseItem{
							{SkuId: 1, Quantity: 0, LockedPrice: math.NewInt(100)},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
				NextLeaseId: 2,
			},
			expectErr: true,
			errMsg:    "has zero quantity",
		},
		{
			name: "lease item with nil locked_price",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Id:         1,
						Tenant:     tenant,
						ProviderId: 1,
						Items: []types.LeaseItem{
							{SkuId: 1, Quantity: 1},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
				NextLeaseId: 2,
			},
			expectErr: true,
			errMsg:    "invalid locked_price",
		},
		{
			name: "lease item with zero locked_price",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Id:         1,
						Tenant:     tenant,
						ProviderId: 1,
						Items: []types.LeaseItem{
							{SkuId: 1, Quantity: 1, LockedPrice: math.ZeroInt()},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
				NextLeaseId: 2,
			},
			expectErr: true,
			errMsg:    "invalid locked_price",
		},
		{
			name: "lease item with negative locked_price",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Id:         1,
						Tenant:     tenant,
						ProviderId: 1,
						Items: []types.LeaseItem{
							{SkuId: 1, Quantity: 1, LockedPrice: math.NewInt(-100)},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
				NextLeaseId: 2,
			},
			expectErr: true,
			errMsg:    "invalid locked_price",
		},
		{
			name: "lease with unspecified state",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Id:         1,
						Tenant:     tenant,
						ProviderId: 1,
						Items: []types.LeaseItem{
							{SkuId: 1, Quantity: 1, LockedPrice: math.NewInt(100)},
						},
						State:         types.LEASE_STATE_UNSPECIFIED,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
				NextLeaseId: 2,
			},
			expectErr: true,
			errMsg:    "has unspecified state",
		},
		{
			name: "inactive lease without closed_at",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Id:         1,
						Tenant:     tenant,
						ProviderId: 1,
						Items: []types.LeaseItem{
							{SkuId: 1, Quantity: 1, LockedPrice: math.NewInt(100)},
						},
						State:         types.LEASE_STATE_INACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
						ClosedAt:      nil,
					},
				},
				NextLeaseId: 2,
			},
			expectErr: true,
			errMsg:    "inactive but has no closed_at timestamp",
		},
		{
			name: "valid inactive lease with closed_at",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Id:         1,
						Tenant:     tenant,
						ProviderId: 1,
						Items: []types.LeaseItem{
							{SkuId: 1, Quantity: 1, LockedPrice: math.NewInt(100)},
						},
						State:         types.LEASE_STATE_INACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
						ClosedAt:      &closedAt,
					},
				},
				NextLeaseId: 2,
			},
			expectErr: false,
		},
		{
			name: "duplicate credit account tenant",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				CreditAccounts: []types.CreditAccount{
					validCreditAccount,
					{
						Tenant:        tenant, // duplicate
						CreditAddress: creditAddr2.String(),
					},
				},
				NextLeaseId: 1,
			},
			expectErr: true,
			errMsg:    "duplicate credit account for tenant",
		},
		{
			name: "credit account with empty tenant",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				CreditAccounts: []types.CreditAccount{
					{
						Tenant:        "",
						CreditAddress: creditAddr.String(),
					},
				},
				NextLeaseId: 1,
			},
			expectErr: true,
			errMsg:    "credit account has empty tenant",
		},
		{
			name: "credit account with invalid tenant address",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				CreditAccounts: []types.CreditAccount{
					{
						Tenant:        invalidAddr,
						CreditAddress: creditAddr.String(),
					},
				},
				NextLeaseId: 1,
			},
			expectErr: true,
			errMsg:    "invalid tenant address",
		},
		{
			name: "credit account with empty credit_address",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				CreditAccounts: []types.CreditAccount{
					{
						Tenant:        tenant,
						CreditAddress: "",
					},
				},
				NextLeaseId: 1,
			},
			expectErr: true,
			errMsg:    "has empty credit_address",
		},
		{
			name: "credit account with invalid credit_address",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				CreditAccounts: []types.CreditAccount{
					{
						Tenant:        tenant,
						CreditAddress: invalidAddr,
					},
				},
				NextLeaseId: 1,
			},
			expectErr: true,
			errMsg:    "invalid credit_address",
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
