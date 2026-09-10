package types_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestGenesisAndMessagesShareLeaseItemShape(t *testing.T) {
	const skuUUID = "01912345-6789-7abc-8def-0123456789a0"
	for _, tc := range []struct {
		name  string
		items []types.LeaseItemInput
	}{
		{"legacy", []types.LeaseItemInput{{SkuUuid: skuUUID, Quantity: 1}}},
		{"duplicate legacy SKU", []types.LeaseItemInput{{SkuUuid: skuUUID, Quantity: 1}, {SkuUuid: skuUUID, Quantity: 1}}},
		{"distinct services same SKU", []types.LeaseItemInput{{SkuUuid: skuUUID, Quantity: 1, ServiceName: "web"}, {SkuUuid: skuUUID, Quantity: 1, ServiceName: "db"}}},
		{"duplicate services", []types.LeaseItemInput{{SkuUuid: skuUUID, Quantity: 1, ServiceName: "web"}, {SkuUuid: skuUUID, Quantity: 1, ServiceName: "web"}}},
		{"mixed modes", []types.LeaseItemInput{{SkuUuid: skuUUID, Quantity: 1, ServiceName: "web"}, {SkuUuid: skuUUID, Quantity: 1}}},
		{"invalid service", []types.LeaseItemInput{{SkuUuid: skuUUID, Quantity: 1, ServiceName: "Bad.Label"}}},
		{"invalid UUID", []types.LeaseItemInput{{SkuUuid: "invalid", Quantity: 1}}},
		{"zero quantity", []types.LeaseItemInput{{SkuUuid: skuUUID, Quantity: 0}}},
		{"excess quantity", []types.LeaseItemInput{{SkuUuid: skuUUID, Quantity: types.MaxQuantityPerItem + 1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Unix(1, 0)
			tenant := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
			genesis := types.DefaultGenesis()
			genesis.LeaseSequence = 1
			genesis.CreditAccounts = []types.CreditAccount{{Tenant: tenant.String(), CreditAddress: types.DeriveCreditAddress(tenant).String()}}
			items := make([]types.LeaseItem, len(tc.items))
			for index, item := range tc.items {
				items[index] = types.LeaseItem{SkuUuid: item.SkuUuid, Quantity: item.Quantity, ServiceName: item.ServiceName, LockedPrice: sdk.NewInt64Coin("umfx", 1)}
			}
			genesis.Leases = []types.Lease{{
				Uuid: "01912345-6789-7abc-8def-0123456789b0", ProviderUuid: "01912345-6789-7abc-8def-0123456789c0", Tenant: tenant.String(),
				Items: items, State: types.LEASE_STATE_REJECTED, CreatedAt: now, LastSettledAt: now, RejectedAt: &now,
				Reservation: &types.LeaseReservation{},
			}}
			messageErr := types.ValidateLeaseItems(tc.items)
			genesisErr := genesis.Validate()
			if messageErr == nil {
				require.NoError(t, genesisErr)
			} else {
				require.Error(t, genesisErr)
			}
			if tc.name == "duplicate legacy SKU" {
				require.ErrorIs(t, messageErr, types.ErrDuplicateSKU)
				require.ErrorIs(t, genesisErr, types.ErrDuplicateSKU)
			}
		})
	}
}
