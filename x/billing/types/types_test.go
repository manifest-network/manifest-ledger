/*
Package types tests for the billing module.

Test Coverage:
1. Params - Parameter validation (max_leases_per_tenant, max_items_per_lease, min_lease_duration)
2. Msgs - ValidateBasic for all message types including MsgCreateLeaseForTenant
3. Credit - Credit address derivation determinism and correctness
4. Genesis - Genesis state validation including leases and credit accounts
*/
package types_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

const (
	testDenom   = "upwr"
	invalidAddr = "invalid-address"
)

// generateManyLeaseItems generates n lease items for testing max items limit.
func generateManyLeaseItems(n uint64) []types.LeaseItemInput {
	items := make([]types.LeaseItemInput, n)
	for i := uint64(0); i < n; i++ {
		items[i] = types.LeaseItemInput{
			SkuUuid:  fmt.Sprintf("01912345-6789-7abc-8def-%012d", i+1),
			Quantity: 1,
		}
	}
	return items
}

// ============================================================================
// Params Tests
// ============================================================================

func TestParams_DefaultParams(t *testing.T) {
	params := types.DefaultParams()

	require.Equal(t, types.DefaultMaxLeasesPerTenant, params.MaxLeasesPerTenant)
	require.Equal(t, types.DefaultMaxItemsPerLease, params.MaxItemsPerLease)
	require.Equal(t, types.DefaultMinLeaseDuration, params.MinLeaseDuration)
}

func TestParams_NewParams(t *testing.T) {
	maxLeases := uint64(50)
	allowedList := []string{}
	maxItems := uint64(10)
	minLeaseDuration := uint64(3600)

	params := types.NewParams(maxLeases, allowedList, maxItems, minLeaseDuration, 10, 1800)

	require.Equal(t, maxLeases, params.MaxLeasesPerTenant)
	require.Equal(t, allowedList, params.AllowedList)
	require.Equal(t, maxItems, params.MaxItemsPerLease)
	require.Equal(t, minLeaseDuration, params.MinLeaseDuration)
}

func TestParams_PendingLeaseDeadline(t *testing.T) {
	params := types.DefaultParams()
	params.PendingTimeout = 60
	createdAt := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	deadline := createdAt.Add(60 * time.Second)

	require.Equal(t, 60*time.Second, params.PendingTimeoutDuration())
	require.Equal(t, deadline, params.PendingLeaseDeadline(createdAt))
	require.False(t, params.PendingLeaseDeadlineExceeded(deadline.Add(-time.Nanosecond), createdAt))
	require.False(t, params.PendingLeaseDeadlineExceeded(deadline, createdAt))
	require.True(t, params.PendingLeaseDeadlineExceeded(deadline.Add(time.Nanosecond), createdAt))
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
			params:    types.NewParams(10, []string{}, 20, 3600, 10, 1800),
			expectErr: false,
		},
		{
			name:      "zero max leases per tenant",
			params:    types.NewParams(0, []string{}, 20, 3600, 10, 1800),
			expectErr: true,
			errMsg:    "max_leases_per_tenant must be greater than zero",
		},
		{
			name:      "max leases per tenant exceeds upper bound",
			params:    types.NewParams(types.MaxLeasesPerTenantUpperBound+1, []string{}, 20, 3600, 10, 1800),
			expectErr: true,
			errMsg:    "exceeds upper bound",
		},
		{
			name:      "max leases per tenant at upper bound",
			params:    types.NewParams(types.MaxLeasesPerTenantUpperBound, []string{}, 20, 3600, 10, 1800),
			expectErr: false,
		},
		{
			name:      "zero max items per lease",
			params:    types.NewParams(10, []string{}, 0, 3600, 10, 1800),
			expectErr: true,
			errMsg:    "max_items_per_lease must be greater than zero",
		},
		{
			name:      "max items per lease exceeds hard limit",
			params:    types.NewParams(10, []string{}, types.MaxItemsPerLeaseHardLimit+1, 3600, 10, 1800),
			expectErr: true,
			errMsg:    "exceeds hard limit",
		},
		{
			name:      "max items per lease at hard limit",
			params:    types.NewParams(10, []string{}, types.MaxItemsPerLeaseHardLimit, 3600, 10, 1800),
			expectErr: false,
		},
		{
			name:      "zero min lease duration",
			params:    types.NewParams(10, []string{}, 20, 0, 10, 1800),
			expectErr: true,
			errMsg:    "min_lease_duration must be greater than zero",
		},
		{
			name:      "min lease duration exceeds upper bound",
			params:    types.NewParams(10, []string{}, 20, types.MaxMinLeaseDuration+1, 10, 1800),
			expectErr: true,
			errMsg:    "exceeds upper bound",
		},
		{
			name:      "min lease duration at upper bound",
			params:    types.NewParams(10, []string{}, 20, types.MaxMinLeaseDuration, 10, 1800),
			expectErr: false,
		},
		{
			name:      "max pending leases per tenant exceeds upper bound",
			params:    types.NewParams(10, []string{}, 20, 3600, types.MaxPendingLeasesPerTenantUpperBound+1, 1800),
			expectErr: true,
			errMsg:    "exceeds upper bound",
		},
		{
			name:      "max pending leases per tenant at upper bound",
			params:    types.NewParams(10, []string{}, 20, 3600, types.MaxPendingLeasesPerTenantUpperBound, 1800),
			expectErr: false,
		},
		{
			name:      "valid params with allowed list",
			params:    types.NewParams(10, []string{"manifest1xyz"}, 20, 3600, 10, 1800),
			expectErr: true, // Invalid address
			errMsg:    "invalid address in allowed list",
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

func TestParams_Validate_DuplicateAllowedList(t *testing.T) {
	// Generate a valid address for testing duplicates
	_, _, addr := testdata.KeyTestPubAddr()
	validAddr := addr.String()

	params := types.NewParams(10, []string{validAddr, validAddr}, 20, 3600, 10, 1800)
	err := params.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate address in allowed list")
}

func TestParams_Validate_DuplicateAllowedListCanonicalIdentity(t *testing.T) {
	_, _, addr := testdata.KeyTestPubAddr()

	params := types.NewParams(10, []string{addr.String(), strings.ToUpper(addr.String())}, 20, 3600, 10, 1800)
	err := params.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate address in allowed list")
}

func TestParams_Validate_ValidAllowedList(t *testing.T) {
	// Generate valid addresses for testing
	_, _, addr1 := testdata.KeyTestPubAddr()
	_, _, addr2 := testdata.KeyTestPubAddr()

	params := types.NewParams(10, []string{addr1.String(), addr2.String()}, 20, 3600, 10, 1800)
	err := params.Validate()
	require.NoError(t, err)
}

func TestParams_IsAllowed(t *testing.T) {
	_, _, addr1 := testdata.KeyTestPubAddr()
	_, _, addr2 := testdata.KeyTestPubAddr()
	_, _, notAllowed := testdata.KeyTestPubAddr()

	params := types.NewParams(10, []string{addr1.String(), addr2.String()}, 20, 3600, 10, 1800)

	require.True(t, params.IsAllowed(addr1.String()))
	require.True(t, params.IsAllowed(strings.ToUpper(addr1.String())))
	require.True(t, params.IsAllowed(addr2.String()))
	require.False(t, params.IsAllowed(notAllowed.String()))
	require.False(t, params.IsAllowed(""))
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
		{SkuUuid: "01912345-6789-7abc-8def-0123456789ab", Quantity: 2},
		{SkuUuid: "01912345-6789-7abc-8def-0123456789ac", Quantity: 1},
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
				Items:  []types.LeaseItemInput{{SkuUuid: "01912345-6789-7abc-8def-0123456789ab", Quantity: 1}},
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
				Items:  []types.LeaseItemInput{{SkuUuid: "", Quantity: 1}},
			},
			expectErr: true,
			errMsg:    "has empty sku_uuid",
		},
		{
			name: "item with zero quantity",
			msg: types.MsgCreateLease{
				Tenant: tenant,
				Items:  []types.LeaseItemInput{{SkuUuid: "01912345-6789-7abc-8def-0123456789ab", Quantity: 0}},
			},
			expectErr: true,
			errMsg:    "has zero quantity",
		},
		{
			name: "duplicate sku_id",
			msg: types.MsgCreateLease{
				Tenant: tenant,
				Items: []types.LeaseItemInput{
					{SkuUuid: "01912345-6789-7abc-8def-0123456789ab", Quantity: 1},
					{SkuUuid: "01912345-6789-7abc-8def-0123456789ab", Quantity: 2},
				},
			},
			expectErr: true,
			errMsg:    "appears multiple times",
		},
		{
			name: "too many items exceeds hard limit",
			msg: types.MsgCreateLease{
				Tenant: tenant,
				Items:  generateManyLeaseItems(types.MaxItemsPerLeaseHardLimit + 1),
			},
			expectErr: true,
			errMsg:    "too many items",
		},
		{
			name: "valid message with meta_hash",
			msg: types.MsgCreateLease{
				Tenant:   tenant,
				Items:    []types.LeaseItemInput{{SkuUuid: "01912345-6789-7abc-8def-0123456789ab", Quantity: 1}},
				MetaHash: make([]byte, 32), // SHA-256 hash length
			},
			expectErr: false,
		},
		{
			name: "valid message with max length meta_hash",
			msg: types.MsgCreateLease{
				Tenant:   tenant,
				Items:    []types.LeaseItemInput{{SkuUuid: "01912345-6789-7abc-8def-0123456789ab", Quantity: 1}},
				MetaHash: make([]byte, types.MaxMetaHashLength), // Max 64 bytes
			},
			expectErr: false,
		},
		{
			name: "valid message with empty meta_hash",
			msg: types.MsgCreateLease{
				Tenant:   tenant,
				Items:    []types.LeaseItemInput{{SkuUuid: "01912345-6789-7abc-8def-0123456789ab", Quantity: 1}},
				MetaHash: []byte{},
			},
			expectErr: false,
		},
		{
			name: "meta_hash exceeds max length",
			msg: types.MsgCreateLease{
				Tenant:   tenant,
				Items:    []types.LeaseItemInput{{SkuUuid: "01912345-6789-7abc-8def-0123456789ab", Quantity: 1}},
				MetaHash: make([]byte, types.MaxMetaHashLength+1), // 65 bytes - too long
			},
			expectErr: true,
			errMsg:    "meta_hash exceeds maximum length",
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
// MsgCreateLeaseForTenant Tests
// ============================================================================

func TestMsgCreateLeaseForTenant_ValidateBasic(t *testing.T) {
	_, _, authorityAddr := testdata.KeyTestPubAddr()
	_, _, tenantAddr := testdata.KeyTestPubAddr()
	authority := authorityAddr.String()
	tenant := tenantAddr.String()

	validItems := []types.LeaseItemInput{
		{SkuUuid: "01912345-6789-7abc-8def-0123456789ab", Quantity: 2},
		{SkuUuid: "01912345-6789-7abc-8def-0123456789ac", Quantity: 1},
	}

	tests := []struct {
		name      string
		msg       types.MsgCreateLeaseForTenant
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid message",
			msg: types.MsgCreateLeaseForTenant{
				Authority: authority,
				Tenant:    tenant,
				Items:     validItems,
			},
			expectErr: false,
		},
		{
			name: "valid single item",
			msg: types.MsgCreateLeaseForTenant{
				Authority: authority,
				Tenant:    tenant,
				Items:     []types.LeaseItemInput{{SkuUuid: "01912345-6789-7abc-8def-0123456789ab", Quantity: 1}},
			},
			expectErr: false,
		},
		{
			name: "invalid authority address",
			msg: types.MsgCreateLeaseForTenant{
				Authority: invalidAddr,
				Tenant:    tenant,
				Items:     validItems,
			},
			expectErr: true,
			errMsg:    "invalid authority address",
		},
		{
			name: "empty authority address",
			msg: types.MsgCreateLeaseForTenant{
				Authority: "",
				Tenant:    tenant,
				Items:     validItems,
			},
			expectErr: true,
			errMsg:    "invalid authority address",
		},
		{
			name: "invalid tenant address",
			msg: types.MsgCreateLeaseForTenant{
				Authority: authority,
				Tenant:    invalidAddr,
				Items:     validItems,
			},
			expectErr: true,
			errMsg:    "invalid tenant address",
		},
		{
			name: "empty tenant address",
			msg: types.MsgCreateLeaseForTenant{
				Authority: authority,
				Tenant:    "",
				Items:     validItems,
			},
			expectErr: true,
			errMsg:    "invalid tenant address",
		},
		{
			name: "empty items",
			msg: types.MsgCreateLeaseForTenant{
				Authority: authority,
				Tenant:    tenant,
				Items:     []types.LeaseItemInput{},
			},
			expectErr: true,
			errMsg:    "lease must contain at least one item",
		},
		{
			name: "nil items",
			msg: types.MsgCreateLeaseForTenant{
				Authority: authority,
				Tenant:    tenant,
				Items:     nil,
			},
			expectErr: true,
			errMsg:    "lease must contain at least one item",
		},
		{
			name: "item with zero sku_id",
			msg: types.MsgCreateLeaseForTenant{
				Authority: authority,
				Tenant:    tenant,
				Items:     []types.LeaseItemInput{{SkuUuid: "", Quantity: 1}},
			},
			expectErr: true,
			errMsg:    "has empty sku_uuid",
		},
		{
			name: "item with zero quantity",
			msg: types.MsgCreateLeaseForTenant{
				Authority: authority,
				Tenant:    tenant,
				Items:     []types.LeaseItemInput{{SkuUuid: "01912345-6789-7abc-8def-0123456789ab", Quantity: 0}},
			},
			expectErr: true,
			errMsg:    "has zero quantity",
		},
		{
			name: "duplicate sku_id",
			msg: types.MsgCreateLeaseForTenant{
				Authority: authority,
				Tenant:    tenant,
				Items: []types.LeaseItemInput{
					{SkuUuid: "01912345-6789-7abc-8def-0123456789ab", Quantity: 1},
					{SkuUuid: "01912345-6789-7abc-8def-0123456789ab", Quantity: 2},
				},
			},
			expectErr: true,
			errMsg:    "appears multiple times",
		},
		{
			name: "too many items exceeds hard limit",
			msg: types.MsgCreateLeaseForTenant{
				Authority: authority,
				Tenant:    tenant,
				Items:     generateManyLeaseItems(types.MaxItemsPerLeaseHardLimit + 1),
			},
			expectErr: true,
			errMsg:    "too many items",
		},
		{
			name: "valid message with meta_hash",
			msg: types.MsgCreateLeaseForTenant{
				Authority: authority,
				Tenant:    tenant,
				Items:     []types.LeaseItemInput{{SkuUuid: "01912345-6789-7abc-8def-0123456789ab", Quantity: 1}},
				MetaHash:  make([]byte, 32), // SHA-256 hash length
			},
			expectErr: false,
		},
		{
			name: "valid message with max length meta_hash",
			msg: types.MsgCreateLeaseForTenant{
				Authority: authority,
				Tenant:    tenant,
				Items:     []types.LeaseItemInput{{SkuUuid: "01912345-6789-7abc-8def-0123456789ab", Quantity: 1}},
				MetaHash:  make([]byte, types.MaxMetaHashLength), // Max 64 bytes
			},
			expectErr: false,
		},
		{
			name: "meta_hash exceeds max length",
			msg: types.MsgCreateLeaseForTenant{
				Authority: authority,
				Tenant:    tenant,
				Items:     []types.LeaseItemInput{{SkuUuid: "01912345-6789-7abc-8def-0123456789ab", Quantity: 1}},
				MetaHash:  make([]byte, types.MaxMetaHashLength+1), // 65 bytes - too long
			},
			expectErr: true,
			errMsg:    "meta_hash exceeds maximum length",
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
				Sender:     sender,
				LeaseUuids: []string{"01912345-6789-7abc-8def-0123456789ab"},
			},
			expectErr: false,
		},
		{
			name: "invalid sender address",
			msg: types.MsgCloseLease{
				Sender:     invalidAddr,
				LeaseUuids: []string{"01912345-6789-7abc-8def-0123456789ab"},
			},
			expectErr: true,
			errMsg:    "invalid sender address",
		},
		{
			name: "empty sender address",
			msg: types.MsgCloseLease{
				Sender:     "",
				LeaseUuids: []string{"01912345-6789-7abc-8def-0123456789ab"},
			},
			expectErr: true,
			errMsg:    "invalid sender address",
		},
		{
			name: "empty lease_uuids",
			msg: types.MsgCloseLease{
				Sender:     sender,
				LeaseUuids: []string{},
			},
			expectErr: true,
			errMsg:    "lease_uuids cannot be empty",
		},
		{
			name: "valid message with reason",
			msg: types.MsgCloseLease{
				Sender:     sender,
				LeaseUuids: []string{"01912345-6789-7abc-8def-0123456789ab"},
				Reason:     "service no longer needed",
			},
			expectErr: false,
		},
		{
			name: "reason exceeds max length",
			msg: types.MsgCloseLease{
				Sender:     sender,
				LeaseUuids: []string{"01912345-6789-7abc-8def-0123456789ab"},
				Reason:     string(make([]byte, types.MaxClosureReasonLength+1)),
			},
			expectErr: true,
			errMsg:    "reason exceeds maximum length",
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
				Sender:     sender,
				LeaseUuids: []string{"01912345-6789-7abc-8def-0123456789ab"},
			},
			expectErr: false,
		},
		{
			name: "invalid sender address",
			msg: types.MsgWithdraw{
				Sender:     invalidAddr,
				LeaseUuids: []string{"01912345-6789-7abc-8def-0123456789ab"},
			},
			expectErr: true,
			errMsg:    "invalid sender address",
		},
		{
			name: "empty sender address",
			msg: types.MsgWithdraw{
				Sender:     "",
				LeaseUuids: []string{"01912345-6789-7abc-8def-0123456789ab"},
			},
			expectErr: true,
			errMsg:    "invalid sender address",
		},
		{
			name: "neither mode specified",
			msg: types.MsgWithdraw{
				Sender:     sender,
				LeaseUuids: []string{},
			},
			expectErr: true,
			errMsg:    "must specify either lease_uuids or provider_uuid",
		},
		{
			name: "both modes specified",
			msg: types.MsgWithdraw{
				Sender:       sender,
				LeaseUuids:   []string{"01912345-6789-7abc-8def-0123456789ab"},
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
			},
			expectErr: true,
			errMsg:    "cannot specify both lease_uuids and provider_uuid",
		},
		{
			name: "valid provider mode",
			msg: types.MsgWithdraw{
				Sender:       sender,
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
			},
			expectErr: false,
		},
		{
			name: "provider mode with limit",
			msg: types.MsgWithdraw{
				Sender:       sender,
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Limit:        50,
			},
			expectErr: false,
		},
		{
			name: "provider mode with max limit",
			msg: types.MsgWithdraw{
				Sender:       sender,
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Limit:        types.MaxBatchLeaseSize,
			},
			expectErr: false,
		},
		{
			name: "provider mode with invalid limit",
			msg: types.MsgWithdraw{
				Sender:       sender,
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Limit:        types.MaxBatchLeaseSize + 1,
			},
			expectErr: true,
			errMsg:    "exceeds maximum allowed",
		},
		{
			name: "provider mode with invalid uuid",
			msg: types.MsgWithdraw{
				Sender:       sender,
				ProviderUuid: "invalid-uuid",
			},
			expectErr: true,
			errMsg:    "invalid provider_uuid format",
		},
		{
			name: "provider mode with cursor key is valid",
			msg: types.MsgWithdraw{
				Sender:       sender,
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Key:          []byte("01912345-6789-7abc-8def-0123456789ac"),
			},
			expectErr: false,
		},
		{
			name: "provider mode with cursor key at max length",
			msg: types.MsgWithdraw{
				Sender:       sender,
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Key:          make([]byte, types.MaxWithdrawCursorLen),
			},
			expectErr: false,
		},
		{
			name: "provider mode with oversized cursor key",
			msg: types.MsgWithdraw{
				Sender:       sender,
				ProviderUuid: "01912345-6789-7abc-8def-0123456789ab",
				Key:          make([]byte, types.MaxWithdrawCursorLen+1),
			},
			expectErr: true,
			errMsg:    "key length",
		},
		{
			name: "lease mode with cursor key rejected",
			msg: types.MsgWithdraw{
				Sender:     sender,
				LeaseUuids: []string{"01912345-6789-7abc-8def-0123456789ab"},
				Key:        []byte("01912345-6789-7abc-8def-0123456789ac"),
			},
			expectErr: true,
			errMsg:    "only valid in provider-wide mode",
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
			name: "invalid params - zero max leases",
			msg: types.MsgUpdateParams{
				Authority: authority,
				Params:    types.NewParams(0, []string{}, 20, 3600, 10, 1800),
			},
			expectErr: true,
			errMsg:    "max_leases_per_tenant must be greater than zero",
		},
		{
			name: "invalid params - zero max items per lease",
			msg: types.MsgUpdateParams{
				Authority: authority,
				Params:    types.NewParams(10, []string{}, 0, 3600, 10, 1800),
			},
			expectErr: true,
			errMsg:    "max_items_per_lease must be greater than zero",
		},
		{
			name: "invalid params - zero min lease duration",
			msg: types.MsgUpdateParams{
				Authority: authority,
				Params:    types.NewParams(10, []string{}, 20, 0, 10, 1800),
			},
			expectErr: true,
			errMsg:    "min_lease_duration must be greater than zero",
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

	// Default genesis should be valid
	require.NoError(t, gs.Validate())
}

func TestGenesisState_NewGenesisState(t *testing.T) {
	_, _, tenantAddr := testdata.KeyTestPubAddr()
	tenant := tenantAddr.String()

	params := types.NewParams(50, []string{}, 20, 3600, 10, 1800)
	now := time.Now().UTC()

	creditAddr := types.DeriveCreditAddress(tenantAddr)

	leases := []types.Lease{
		{
			Uuid:         "01912345-6789-7abc-8def-0123456789ab",
			Tenant:       tenant,
			ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
			Items: []types.LeaseItem{
				{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))},
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

	gs := types.NewGenesisState(params, leases, creditAccounts, 5)

	require.Equal(t, params, gs.Params)
	require.Equal(t, leases, gs.Leases)
	require.Equal(t, creditAccounts, gs.CreditAccounts)
	require.Equal(t, uint64(5), gs.LeaseSequence)
}

func TestGenesisState_ValidateStructuralInvariants(t *testing.T) {
	_, _, tenantAddr := testdata.KeyTestPubAddr()
	_, _, tenant2Addr := testdata.KeyTestPubAddr()
	tenant := tenantAddr.String()
	tenant2 := tenant2Addr.String()

	now := time.Now().UTC()
	closedAt := now.Add(time.Hour)

	creditAddr := types.DeriveCreditAddress(tenantAddr)
	creditAddr2 := types.DeriveCreditAddress(tenant2Addr)

	validLease := types.Lease{
		Uuid:                       "01912345-6789-7abc-8def-0123456789ab",
		Tenant:                     tenant,
		ProviderUuid:               "01912345-6789-7abc-8def-0123456789ac",
		MinLeaseDurationAtCreation: types.DefaultMinLeaseDuration,
		Items: []types.LeaseItem{
			{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))},
		},
		State:         types.LEASE_STATE_ACTIVE,
		CreatedAt:     now,
		LastSettledAt: now,
	}

	// Calculate expected reservation for validLease: locked_price * quantity * min_lease_duration
	// 100 * 1 * 3600 = 360000 (using default MinLeaseDuration)
	validLeaseReservation := sdk.NewCoins(sdk.NewCoin(testDenom, math.NewInt(360000)))

	validCreditAccount := types.CreditAccount{
		Tenant:           tenant,
		CreditAddress:    creditAddr.String(),
		ActiveLeaseCount: 1,                     // validLease is ACTIVE
		ReservedAmounts:  validLeaseReservation, // Must match sum of active/pending lease reservations
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
				LeaseSequence:  1,
			},
			expectErr: false,
		},
		{
			name: "invalid params - zero max leases",
			genesis: &types.GenesisState{
				Params: types.NewParams(0, []string{}, 20, 3600, 10, 1800),
			},
			expectErr: true,
			errMsg:    "invalid params",
		},
		{
			name: "duplicate lease uuid",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					validLease,
					{
						Uuid:         "01912345-6789-7abc-8def-0123456789ab", // duplicate
						Tenant:       tenant2,
						ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
						Items: []types.LeaseItem{
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ae", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
			},
			expectErr: true,
			errMsg:    "duplicate lease uuid",
		},
		{
			name: "lease with empty tenant",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:         "01912345-6789-7abc-8def-0123456789ab",
						Tenant:       "",
						ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
						Items: []types.LeaseItem{
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
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
						Uuid:         "01912345-6789-7abc-8def-0123456789ab",
						Tenant:       invalidAddr,
						ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
						Items: []types.LeaseItem{
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
			},
			expectErr: true,
			errMsg:    "invalid tenant address",
		},
		{
			name: "lease with empty provider_uuid",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:         "01912345-6789-7abc-8def-0123456789ab",
						Tenant:       tenant,
						ProviderUuid: "",
						Items: []types.LeaseItem{
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
			},
			expectErr: true,
			errMsg:    "has empty provider_uuid",
		},
		{
			name: "lease with no items",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:          "01912345-6789-7abc-8def-0123456789ab",
						Tenant:        tenant,
						ProviderUuid:  "01912345-6789-7abc-8def-0123456789ac",
						Items:         []types.LeaseItem{},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
			},
			expectErr: true,
			errMsg:    "has no items",
		},
		{
			name: "lease item with empty sku_uuid",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:         "01912345-6789-7abc-8def-0123456789ab",
						Tenant:       tenant,
						ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
						Items: []types.LeaseItem{
							{SkuUuid: "", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
			},
			expectErr: true,
			errMsg:    "has empty sku_uuid",
		},
		{
			name: "lease item with zero quantity",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:         "01912345-6789-7abc-8def-0123456789ab",
						Tenant:       tenant,
						ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
						Items: []types.LeaseItem{
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 0, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
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
						Uuid:         "01912345-6789-7abc-8def-0123456789ab",
						Tenant:       tenant,
						ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
						Items: []types.LeaseItem{
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
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
						Uuid:         "01912345-6789-7abc-8def-0123456789ab",
						Tenant:       tenant,
						ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
						Items: []types.LeaseItem{
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.ZeroInt())},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
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
						Uuid:         "01912345-6789-7abc-8def-0123456789ab",
						Tenant:       tenant,
						ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
						Items: []types.LeaseItem{
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.Coin{Denom: testDenom, Amount: math.NewInt(-100)}},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
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
						Uuid:         "01912345-6789-7abc-8def-0123456789ab",
						Tenant:       tenant,
						ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
						Items: []types.LeaseItem{
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))},
						},
						State:         types.LEASE_STATE_UNSPECIFIED,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
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
						Uuid:         "01912345-6789-7abc-8def-0123456789ab",
						Tenant:       tenant,
						ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
						Items: []types.LeaseItem{
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))},
						},
						State:         types.LEASE_STATE_CLOSED,
						CreatedAt:     now,
						LastSettledAt: now,
						ClosedAt:      nil,
					},
				},
			},
			expectErr: true,
			errMsg:    "closed but has no closed_at timestamp",
		},
		{
			name: "valid inactive lease with closed_at",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:         "01912345-6789-7abc-8def-0123456789ab",
						Tenant:       tenant,
						ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
						Items: []types.LeaseItem{
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))},
						},
						State:         types.LEASE_STATE_CLOSED,
						CreatedAt:     now,
						LastSettledAt: now,
						ClosedAt:      &closedAt,
					},
				},
				LeaseSequence: 1,
			},
			expectErr: false,
		},
		{
			name: "lease with meta_hash exceeding max length",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:         "01912345-6789-7abc-8def-0123456789ab",
						Tenant:       tenant,
						ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
						Items: []types.LeaseItem{
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
						MetaHash:      make([]byte, types.MaxMetaHashLength+1), // 65 bytes - too long
					},
				},
			},
			expectErr: true,
			errMsg:    "meta_hash exceeding maximum length",
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
			},
			expectErr: true,
			errMsg:    "invalid credit_address",
		},
		{
			name: "credit account with mismatched credit_address",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				CreditAccounts: []types.CreditAccount{
					{
						Tenant:        tenant,
						CreditAddress: creditAddr2.String(), // Wrong: using tenant2's credit address for tenant
					},
				},
			},
			expectErr: true,
			errMsg:    "mismatched credit_address",
		},
		// Cross-validation: reserved_amounts must match lease reservations
		{
			name: "reserved_amounts mismatch - too low",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{validLease}, // Active lease needs 360000 reserved
				CreditAccounts: []types.CreditAccount{
					{
						Tenant:          tenant,
						CreditAddress:   creditAddr.String(),
						ReservedAmounts: sdk.NewCoins(sdk.NewCoin(testDenom, math.NewInt(100000))), // Wrong: too low
					},
				},
			},
			expectErr: true,
			errMsg:    "reserved_amounts",
		},
		{
			name: "reserved_amounts mismatch - too high",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{validLease}, // Active lease needs 360000 reserved
				CreditAccounts: []types.CreditAccount{
					{
						Tenant:          tenant,
						CreditAddress:   creditAddr.String(),
						ReservedAmounts: sdk.NewCoins(sdk.NewCoin(testDenom, math.NewInt(1000000))), // Wrong: too high
					},
				},
			},
			expectErr: true,
			errMsg:    "reserved_amounts",
		},
		{
			name: "reserved_amounts should be empty for closed lease",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:          "01912345-6789-7abc-8def-0123456789ab",
						Tenant:        tenant,
						ProviderUuid:  "01912345-6789-7abc-8def-0123456789ac",
						Items:         []types.LeaseItem{{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))}},
						State:         types.LEASE_STATE_CLOSED, // Closed - no reservation
						CreatedAt:     now,
						LastSettledAt: now,
						ClosedAt:      &closedAt,
					},
				},
				CreditAccounts: []types.CreditAccount{
					{
						Tenant:          tenant,
						CreditAddress:   creditAddr.String(),
						ReservedAmounts: sdk.NewCoins(), // Correct: closed lease has no reservation
					},
				},
				LeaseSequence: 1,
			},
			expectErr: false,
		},
		{
			name: "reserved_amounts required for pending lease",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:                       "01912345-6789-7abc-8def-0123456789ab",
						Tenant:                     tenant,
						ProviderUuid:               "01912345-6789-7abc-8def-0123456789ac",
						Items:                      []types.LeaseItem{{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))}},
						State:                      types.LEASE_STATE_PENDING, // Pending - needs reservation
						CreatedAt:                  now,
						LastSettledAt:              now,
						MinLeaseDurationAtCreation: types.DefaultMinLeaseDuration,
					},
				},
				CreditAccounts: []types.CreditAccount{
					{
						Tenant:          tenant,
						CreditAddress:   creditAddr.String(),
						ReservedAmounts: sdk.NewCoins(), // Wrong: pending lease needs reservation
					},
				},
			},
			expectErr: true,
			errMsg:    "reserved_amounts",
		},
		{
			name: "multiple leases - combined reservation",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:          "01912345-6789-7abc-8def-0123456789ab",
						Tenant:        tenant,
						ProviderUuid:  "01912345-6789-7abc-8def-0123456789ac",
						Items:         []types.LeaseItem{{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))}},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
					{
						Uuid:          "01912345-6789-7abc-8def-0123456789ae",
						Tenant:        tenant,
						ProviderUuid:  "01912345-6789-7abc-8def-0123456789ac",
						Items:         []types.LeaseItem{{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 2, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(50))}},
						State:         types.LEASE_STATE_PENDING,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
				CreditAccounts: []types.CreditAccount{
					{
						Tenant:            tenant,
						CreditAddress:     creditAddr.String(),
						ActiveLeaseCount:  1, // 1 ACTIVE lease
						PendingLeaseCount: 1, // 1 PENDING lease
						// Lease 1: 100 * 1 * 3600 = 360000
						// Lease 2: 50 * 2 * 3600 = 360000
						// Total: 720000
						ReservedAmounts: sdk.NewCoins(sdk.NewCoin(testDenom, math.NewInt(720000))),
					},
				},
				LeaseSequence: 2,
			},
			expectErr: false,
		},
		{
			name: "lease with stored min_lease_duration_at_creation",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(), // Current param is 3600
				Leases: []types.Lease{
					{
						Uuid:                       "01912345-6789-7abc-8def-0123456789ab",
						Tenant:                     tenant,
						ProviderUuid:               "01912345-6789-7abc-8def-0123456789ac",
						Items:                      []types.LeaseItem{{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))}},
						State:                      types.LEASE_STATE_ACTIVE,
						CreatedAt:                  now,
						LastSettledAt:              now,
						MinLeaseDurationAtCreation: 7200, // Was 7200 at creation, not 3600
					},
				},
				CreditAccounts: []types.CreditAccount{
					{
						Tenant:           tenant,
						CreditAddress:    creditAddr.String(),
						ActiveLeaseCount: 1, // 1 ACTIVE lease
						// Should use stored duration: 100 * 1 * 7200 = 720000
						ReservedAmounts: sdk.NewCoins(sdk.NewCoin(testDenom, math.NewInt(720000))),
					},
				},
				LeaseSequence: 1,
			},
			expectErr: false,
		},
		{
			name: "tenant with leases but no credit account",
			genesis: &types.GenesisState{
				Params:         types.DefaultParams(),
				Leases:         []types.Lease{validLease}, // Active lease for tenant
				CreditAccounts: []types.CreditAccount{
					// Missing credit account for tenant
				},
			},
			expectErr: true,
			errMsg:    "no credit account",
		},
		{
			name: "active_lease_count mismatch",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{validLease}, // 1 ACTIVE lease
				CreditAccounts: []types.CreditAccount{
					{
						Tenant:           tenant,
						CreditAddress:    creditAddr.String(),
						ActiveLeaseCount: 5, // Wrong: says 5 but only 1 active
						ReservedAmounts:  validLeaseReservation,
					},
				},
			},
			expectErr: true,
			errMsg:    "active_lease_count 5 but has 1 active leases",
		},
		{
			name: "pending_lease_count mismatch",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:         "01912345-6789-7abc-8def-0123456789ab",
						Tenant:       tenant,
						ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
						Items:        []types.LeaseItem{{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))}},
						State:        types.LEASE_STATE_PENDING,
						CreatedAt:    now,
					},
				},
				CreditAccounts: []types.CreditAccount{
					{
						Tenant:            tenant,
						CreditAddress:     creditAddr.String(),
						PendingLeaseCount: 3, // Wrong: says 3 but only 1 pending
						ReservedAmounts:   validLeaseReservation,
					},
				},
			},
			expectErr: true,
			errMsg:    "pending_lease_count 3 but has 1 pending leases",
		},
		// ---- service_name genesis validation ----
		{
			name: "valid lease with service_names on all items",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:         "01912345-6789-7abc-8def-0123456789ab",
						Tenant:       tenant,
						ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
						Items: []types.LeaseItem{
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100)), ServiceName: "web"},
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100)), ServiceName: "db"},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
				CreditAccounts: []types.CreditAccount{
					{
						Tenant:           tenant,
						CreditAddress:    creditAddr.String(),
						ActiveLeaseCount: 1,
						// 2 items: 100 * 1 * 3600 * 2 = 720000
						ReservedAmounts: sdk.NewCoins(sdk.NewCoin(testDenom, math.NewInt(720000))),
					},
				},
				LeaseSequence: 1,
			},
			expectErr: false,
		},
		{
			name: "lease with mixed service_names (some set, some not)",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:         "01912345-6789-7abc-8def-0123456789ab",
						Tenant:       tenant,
						ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
						Items: []types.LeaseItem{
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100)), ServiceName: "web"},
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ae", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
				CreditAccounts: []types.CreditAccount{validCreditAccount},
			},
			expectErr: true,
			errMsg:    "all items must have service_name or none",
		},
		{
			name: "lease with duplicate service_name",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:         "01912345-6789-7abc-8def-0123456789ab",
						Tenant:       tenant,
						ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
						Items: []types.LeaseItem{
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100)), ServiceName: "web"},
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ae", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100)), ServiceName: "web"},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
				CreditAccounts: []types.CreditAccount{validCreditAccount},
			},
			expectErr: true,
			errMsg:    "duplicate service_name",
		},
		{
			name: "lease with invalid DNS label in service_name",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:         "01912345-6789-7abc-8def-0123456789ab",
						Tenant:       tenant,
						ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
						Items: []types.LeaseItem{
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100)), ServiceName: "INVALID"},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
				CreditAccounts: []types.CreditAccount{validCreditAccount},
			},
			expectErr: true,
			errMsg:    "invalid service_name",
		},
		{
			name: "lease with duplicate sku_uuid in legacy mode (no service_names)",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:         "01912345-6789-7abc-8def-0123456789ab",
						Tenant:       tenant,
						ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
						Items: []types.LeaseItem{
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))},
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 2, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
				CreditAccounts: []types.CreditAccount{validCreditAccount},
			},
			expectErr: true,
			errMsg:    "duplicate sku_uuid",
		},
	}

	validators := []struct {
		name     string
		validate func(*types.GenesisState) error
	}{
		{name: "default", validate: (*types.GenesisState).Validate},
		{name: "strict", validate: (*types.GenesisState).ValidateStrict},
	}

	for _, validator := range validators {
		t.Run(validator.name, func(t *testing.T) {
			for _, tc := range tests {
				t.Run(tc.name, func(t *testing.T) {
					err := validator.validate(tc.genesis)
					if tc.expectErr {
						require.Error(t, err)
						require.Contains(t, err.Error(), tc.errMsg)
					} else {
						require.NoError(t, err)
					}
				})
			}
		})
	}
}

// TestGenesisState_Validate_MultipleMissingCreditAccounts pins the sorted
// iteration over expectedReservations. With more than one offending tenant,
// ranging over the map named an arbitrary one per run; Validate must always
// report the lexicographically lowest address so the failure is reproducible.
func TestGenesisState_Validate_MultipleMissingCreditAccounts(t *testing.T) {
	_, _, tenantAddr := testdata.KeyTestPubAddr()
	_, _, tenant2Addr := testdata.KeyTestPubAddr()
	tenant := tenantAddr.String()
	tenant2 := tenant2Addr.String()

	now := time.Now().UTC()
	lease := func(uuid, owner string) types.Lease {
		return types.Lease{
			Uuid:         uuid,
			Tenant:       owner,
			ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
			Items: []types.LeaseItem{
				{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))},
			},
			State:         types.LEASE_STATE_ACTIVE,
			CreatedAt:     now,
			LastSettledAt: now,
		}
	}

	// Both tenants hold reservations and neither has a credit account.
	gs := &types.GenesisState{
		Params: types.DefaultParams(),
		Leases: []types.Lease{
			lease("01912345-6789-7abc-8def-0123456789ab", tenant),
			lease("01912345-6789-7abc-8def-0123456789ae", tenant2),
		},
		LeaseSequence: 2,
	}

	lowest := min(tenant, tenant2)
	highest := max(tenant, tenant2)

	// Repeat: map iteration order is randomised per range statement, so a single
	// run would have caught the unsorted version only half the time.
	for range 50 {
		err := gs.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "no credit account")
		require.Contains(t, err.Error(), lowest, "must report the lexicographically lowest offending tenant")
		require.NotContains(t, err.Error(), highest)
	}
}

func TestGenesisState_ValidateCreditAccountLeaseCounts_MissingAccountUsesLeaseOrder(t *testing.T) {
	_, _, firstTenantAddr := testdata.KeyTestPubAddr()
	_, _, secondTenantAddr := testdata.KeyTestPubAddr()
	firstTenant := firstTenantAddr.String()
	secondTenant := secondTenantAddr.String()
	gs := &types.GenesisState{
		Leases: []types.Lease{
			{Tenant: firstTenant, State: types.LEASE_STATE_ACTIVE},
			{Tenant: secondTenant, State: types.LEASE_STATE_PENDING},
		},
	}

	for range 50 {
		err := gs.ValidateCreditAccountLeaseCounts()
		require.ErrorIs(t, err, types.ErrInvalidCreditOperation)
		require.Contains(t, err.Error(), firstTenant+" has 1 active and 0 pending leases but no credit account")
		require.NotContains(t, err.Error(), secondTenant)
	}
}

func TestGenesisState_Validate_UsesCanonicalTenantIdentity(t *testing.T) {
	_, _, tenantAddr := testdata.KeyTestPubAddr()
	tenant := tenantAddr.String()
	tenantAlias := strings.ToUpper(tenant)
	creditAddress := types.DeriveCreditAddress(tenantAddr).String()
	params := types.DefaultParams()
	now := time.Unix(1, 0).UTC()

	lease := func(uuid, owner string) types.Lease {
		return types.Lease{
			Uuid:         uuid,
			Tenant:       owner,
			ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
			Items: []types.LeaseItem{{
				SkuUuid:     "01912345-6789-7abc-8def-0123456789ad",
				Quantity:    1,
				LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100)),
			}},
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  now,
			LastSettledAt:              now,
			MinLeaseDurationAtCreation: params.MinLeaseDuration,
		}
	}

	firstLease := lease("01912345-6789-7abc-8def-0123456789ab", tenant)
	secondLease := lease("01912345-6789-7abc-8def-0123456789ae", tenantAlias)
	reservation := types.GetLeaseReservationAmount(&firstLease, params.MinLeaseDuration)
	gs := &types.GenesisState{
		Params: params,
		Leases: []types.Lease{firstLease, secondLease},
		CreditAccounts: []types.CreditAccount{{
			Tenant:           tenant,
			CreditAddress:    strings.ToUpper(creditAddress),
			ActiveLeaseCount: 2,
			ReservedAmounts:  reservation.Add(reservation...),
		}},
		LeaseSequence: 2,
	}

	require.NoError(t, gs.Validate())
	require.NoError(t, gs.ValidateStrict())

	gs.Leases = nil
	gs.LeaseSequence = 0
	gs.CreditAccounts = append(gs.CreditAccounts, types.CreditAccount{
		Tenant:        tenantAlias,
		CreditAddress: creditAddress,
	})
	gs.CreditAccounts[0].ActiveLeaseCount = 0
	gs.CreditAccounts[0].ReservedAmounts = nil
	err := gs.Validate()
	require.ErrorIs(t, err, types.ErrInvalidCreditOperation)
	require.Contains(t, err.Error(), "duplicate credit account for tenant")
}

func TestGenesisState_Validate_RequiresKnownReservationPortion(t *testing.T) {
	_, _, tenantAddr := testdata.KeyTestPubAddr()
	tenant := tenantAddr.String()
	creditAddr := types.DeriveCreditAddress(tenantAddr)
	params := types.DefaultParams()
	now := time.Now().UTC()

	lease := func(uuid string, duration uint64) types.Lease {
		return types.Lease{
			Uuid:         uuid,
			Tenant:       tenant,
			ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
			Items: []types.LeaseItem{{
				SkuUuid:     "01912345-6789-7abc-8def-0123456789ad",
				Quantity:    1,
				LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100)),
			}},
			State:                      types.LEASE_STATE_ACTIVE,
			CreatedAt:                  now,
			LastSettledAt:              now,
			MinLeaseDurationAtCreation: duration,
		}
	}

	legacyLease := lease("01912345-6789-7abc-8def-0123456789ab", 0)
	modernLease := lease("01912345-6789-7abc-8def-0123456789ae", params.MinLeaseDuration)
	knownReservation := types.CalculateLeaseReservation(
		modernLease.Items,
		modernLease.MinLeaseDurationAtCreation,
	)
	gs := &types.GenesisState{
		Params: params,
		Leases: []types.Lease{legacyLease, modernLease},
		CreditAccounts: []types.CreditAccount{{
			Tenant:           tenant,
			CreditAddress:    creditAddr.String(),
			ActiveLeaseCount: 2,
			ReservedAmounts: sdk.NewCoins(sdk.NewCoin(
				testDenom,
				knownReservation.AmountOf(testDenom).SubRaw(1),
			)),
		}},
		LeaseSequence: 2,
	}

	err := gs.Validate()
	require.ErrorIs(t, err, types.ErrInvalidCreditOperation)
	require.Contains(t, err.Error(), "below known non-legacy lease reservations")
}

func TestGenesisState_Validate_AcceptsMixedHistoricalReservations(t *testing.T) {
	_, _, tenantAddr := testdata.KeyTestPubAddr()
	tenant := tenantAddr.String()
	now := time.Unix(1, 0).UTC()
	closedAt := now.Add(time.Minute)
	params := types.DefaultParams()

	lease := func(uuid string, state types.LeaseState, duration uint64) types.Lease {
		return types.Lease{
			Uuid:         uuid,
			Tenant:       tenant,
			ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
			Items: []types.LeaseItem{{
				SkuUuid:     "01912345-6789-7abc-8def-0123456789ad",
				Quantity:    1,
				LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100)),
			}},
			State:                      state,
			CreatedAt:                  now,
			LastSettledAt:              now,
			MinLeaseDurationAtCreation: duration,
		}
	}

	legacyLease := lease("01912345-6789-7abc-8def-0123456789ab", types.LEASE_STATE_ACTIVE, 0)
	modernLease1 := lease("01912345-6789-7abc-8def-0123456789ae", types.LEASE_STATE_ACTIVE, params.MinLeaseDuration)
	modernLease2 := lease("01912345-6789-7abc-8def-0123456789af", types.LEASE_STATE_ACTIVE, params.MinLeaseDuration)
	closedLease := lease("01912345-6789-7abc-8def-0123456789b0", types.LEASE_STATE_CLOSED, 0)
	closedLease.ClosedAt = &closedAt

	knownModernReservations := types.CalculateLeaseReservation(
		modernLease1.Items,
		modernLease1.MinLeaseDurationAtCreation,
	).Add(types.CalculateLeaseReservation(
		modernLease2.Items,
		modernLease2.MinLeaseDurationAtCreation,
	)...)
	historicalLegacyReservation := types.CalculateLeaseReservation(
		legacyLease.Items,
		params.MinLeaseDuration/2,
	)

	gs := &types.GenesisState{
		Params: params,
		Leases: []types.Lease{legacyLease, modernLease1, modernLease2, closedLease},
		CreditAccounts: []types.CreditAccount{{
			Tenant:           tenant,
			CreditAddress:    types.DeriveCreditAddress(tenantAddr).String(),
			ActiveLeaseCount: 3,
			ReservedAmounts:  knownModernReservations.Add(historicalLegacyReservation...),
		}},
		LeaseSequence: 4,
	}

	require.ErrorIs(t, gs.ValidateStrict(), types.ErrInvalidCreditOperation)
	require.NoError(t, gs.Validate())
}

func TestGenesisState_Validate_AcceptsTerminalLegacyReservationResidual(t *testing.T) {
	_, _, tenantAddr := testdata.KeyTestPubAddr()
	tenant := tenantAddr.String()
	now := time.Unix(1, 0).UTC()
	closedAt := now.Add(time.Minute)
	params := types.DefaultParams()

	legacyLease := types.Lease{
		Uuid:         "01912345-6789-7abc-8def-0123456789ab",
		Tenant:       tenant,
		ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
		Items: []types.LeaseItem{{
			SkuUuid:     "01912345-6789-7abc-8def-0123456789ad",
			Quantity:    1,
			LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100)),
		}},
		State:         types.LEASE_STATE_CLOSED,
		CreatedAt:     now,
		LastSettledAt: now,
		ClosedAt:      &closedAt,
		// Zero identifies a lease created before the duration was stored.
		MinLeaseDurationAtCreation: 0,
	}
	historicalReservation := types.CalculateLeaseReservation(
		legacyLease.Items,
		params.MinLeaseDuration*2,
	)
	gs := &types.GenesisState{
		Params: params,
		Leases: []types.Lease{legacyLease},
		CreditAccounts: []types.CreditAccount{{
			Tenant:          tenant,
			CreditAddress:   types.DeriveCreditAddress(tenantAddr).String(),
			ReservedAmounts: historicalReservation,
		}},
		LeaseSequence: 1,
	}

	// Historical release used the later, lower parameter and legitimately left
	// a residual that cannot be reconstructed from the exported lease record.
	require.NoError(t, gs.Validate())
	require.ErrorIs(t, gs.ValidateStrict(), types.ErrInvalidCreditOperation)
}

// ============================================================================
// Genesis ValidateWithBlockTime Tests
// ============================================================================

func TestGenesisState_ValidateWithBlockTime(t *testing.T) {
	_, _, tenantAddr := testdata.KeyTestPubAddr()
	tenant := tenantAddr.String()

	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	closedAt := past.Add(30 * time.Minute)

	creditAddr := types.DeriveCreditAddress(tenantAddr)

	tests := []struct {
		name      string
		genesis   *types.GenesisState
		blockTime time.Time
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid - all timestamps in the past",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:         "01912345-6789-7abc-8def-0123456789ab",
						Tenant:       tenant,
						ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
						Items: []types.LeaseItem{
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     past,
						LastSettledAt: past,
					},
				},
				CreditAccounts: []types.CreditAccount{
					{Tenant: tenant, CreditAddress: creditAddr.String()},
				},
			},
			blockTime: now,
			expectErr: false,
		},
		{
			name: "invalid - last_settled_at in the future",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:         "01912345-6789-7abc-8def-0123456789ab",
						Tenant:       tenant,
						ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
						Items: []types.LeaseItem{
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     past,
						LastSettledAt: future,
					},
				},
			},
			blockTime: now,
			expectErr: true,
			errMsg:    "last_settled_at",
		},
		{
			name: "invalid - created_at in the future",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:         "01912345-6789-7abc-8def-0123456789ab",
						Tenant:       tenant,
						ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
						Items: []types.LeaseItem{
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     future,
						LastSettledAt: past,
					},
				},
			},
			blockTime: now,
			expectErr: true,
			errMsg:    "created_at",
		},
		{
			name: "invalid - closed_at in the future for inactive lease",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:         "01912345-6789-7abc-8def-0123456789ab",
						Tenant:       tenant,
						ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
						Items: []types.LeaseItem{
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))},
						},
						State:         types.LEASE_STATE_CLOSED,
						CreatedAt:     past,
						LastSettledAt: past,
						ClosedAt:      &future,
					},
				},
			},
			blockTime: now,
			expectErr: true,
			errMsg:    "closed_at",
		},
		{
			name: "valid - inactive lease with all timestamps in past",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:         "01912345-6789-7abc-8def-0123456789ab",
						Tenant:       tenant,
						ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
						Items: []types.LeaseItem{
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))},
						},
						State:         types.LEASE_STATE_CLOSED,
						CreatedAt:     past,
						LastSettledAt: closedAt,
						ClosedAt:      &closedAt,
					},
				},
			},
			blockTime: now,
			expectErr: false,
		},
		{
			name: "valid - timestamps exactly at block time",
			genesis: &types.GenesisState{
				Params: types.DefaultParams(),
				Leases: []types.Lease{
					{
						Uuid:         "01912345-6789-7abc-8def-0123456789ab",
						Tenant:       tenant,
						ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
						Items: []types.LeaseItem{
							{SkuUuid: "01912345-6789-7abc-8def-0123456789ad", Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100))},
						},
						State:         types.LEASE_STATE_ACTIVE,
						CreatedAt:     now,
						LastSettledAt: now,
					},
				},
			},
			blockTime: now,
			expectErr: false,
		},
		{
			name:      "valid - empty genesis",
			genesis:   types.DefaultGenesis(),
			blockTime: now,
			expectErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.genesis.ValidateWithBlockTime(tc.blockTime)
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
