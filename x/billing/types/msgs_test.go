package types_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestIsValidDNSLabel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		valid bool
	}{
		{name: "simple lowercase", input: "web", valid: true},
		{name: "alphanumeric", input: "web1", valid: true},
		{name: "with hyphen", input: "my-service", valid: true},
		{name: "single char", input: "a", valid: true},
		{name: "single digit", input: "0", valid: true},
		{name: "max length 63", input: strings.Repeat("a", 63), valid: true},
		{name: "starts with digit", input: "1web", valid: true},
		{name: "empty", input: "", valid: false},
		{name: "uppercase", input: "Web", valid: false},
		{name: "leading hyphen", input: "-web", valid: false},
		{name: "trailing hyphen", input: "web-", valid: false},
		{name: "underscore", input: "my_service", valid: false},
		{name: "dot", input: "my.service", valid: false},
		{name: "space", input: "my service", valid: false},
		{name: "too long 64", input: strings.Repeat("a", 64), valid: false},
		{name: "unicode", input: "servi\u00e7e", valid: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := types.IsValidDNSLabel(tc.input)
			require.Equal(t, tc.valid, result)
		})
	}
}

func TestValidateLeaseItems_ServiceNameMode(t *testing.T) {
	skuA := "01912345-6789-7abc-8def-0123456789ab"
	skuB := "01912345-6789-7abc-8def-0123456789ac"

	tests := []struct {
		name      string
		items     []types.LeaseItemInput
		expectErr bool
		errMsg    string
	}{
		{
			name: "service_name mode: same SKU different names",
			items: []types.LeaseItemInput{
				{SkuUuid: skuA, Quantity: 1, ServiceName: "web"},
				{SkuUuid: skuA, Quantity: 1, ServiceName: "db"},
			},
			expectErr: false,
		},
		{
			name: "service_name mode: different SKUs with names",
			items: []types.LeaseItemInput{
				{SkuUuid: skuA, Quantity: 2, ServiceName: "frontend"},
				{SkuUuid: skuB, Quantity: 1, ServiceName: "backend"},
			},
			expectErr: false,
		},
		{
			name: "service_name mode: single item",
			items: []types.LeaseItemInput{
				{SkuUuid: skuA, Quantity: 1, ServiceName: "web"},
			},
			expectErr: false,
		},
		{
			name: "mixed mode fails: first has name, second doesn't",
			items: []types.LeaseItemInput{
				{SkuUuid: skuA, Quantity: 1, ServiceName: "web"},
				{SkuUuid: skuB, Quantity: 1},
			},
			expectErr: true,
			errMsg:    "all items must have service_name or none",
		},
		{
			name: "mixed mode fails: first has no name, second does",
			items: []types.LeaseItemInput{
				{SkuUuid: skuA, Quantity: 1},
				{SkuUuid: skuB, Quantity: 1, ServiceName: "db"},
			},
			expectErr: true,
			errMsg:    "all items must have service_name or none",
		},
		{
			name: "duplicate service_name fails",
			items: []types.LeaseItemInput{
				{SkuUuid: skuA, Quantity: 1, ServiceName: "web"},
				{SkuUuid: skuB, Quantity: 1, ServiceName: "web"},
			},
			expectErr: true,
			errMsg:    "duplicate service_name",
		},
		{
			name: "invalid DNS label: uppercase",
			items: []types.LeaseItemInput{
				{SkuUuid: skuA, Quantity: 1, ServiceName: "Web"},
			},
			expectErr: true,
			errMsg:    "invalid service_name",
		},
		{
			name: "invalid DNS label: leading hyphen",
			items: []types.LeaseItemInput{
				{SkuUuid: skuA, Quantity: 1, ServiceName: "-web"},
			},
			expectErr: true,
			errMsg:    "invalid service_name",
		},
		{
			name: "invalid DNS label: underscore",
			items: []types.LeaseItemInput{
				{SkuUuid: skuA, Quantity: 1, ServiceName: "my_service"},
			},
			expectErr: true,
			errMsg:    "invalid service_name",
		},
		{
			name: "legacy mode: duplicate SKU still fails",
			items: []types.LeaseItemInput{
				{SkuUuid: skuA, Quantity: 1},
				{SkuUuid: skuA, Quantity: 2},
			},
			expectErr: true,
			errMsg:    "appears multiple times",
		},
		{
			name: "legacy mode: different SKUs passes",
			items: []types.LeaseItemInput{
				{SkuUuid: skuA, Quantity: 1},
				{SkuUuid: skuB, Quantity: 2},
			},
			expectErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := types.ValidateLeaseItems(tc.items)
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
