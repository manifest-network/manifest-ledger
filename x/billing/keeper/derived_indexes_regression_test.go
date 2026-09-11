package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestDerivedIndexesInvariantSharedSKUAndReclaimedDomain(t *testing.T) {
	s := setupCustomDomain(t)
	leaseUUID := s.f.createAndAcknowledgeLease(t, s.msgServer, s.tenant, s.providerAddr, []types.LeaseItemInput{
		{SkuUuid: s.sku.Uuid, Quantity: 1, ServiceName: "web"},
		{SkuUuid: s.sku.Uuid, Quantity: 1, ServiceName: "worker"},
	})
	lease, err := s.f.App.BillingKeeper.GetLease(s.f.Ctx, leaseUUID)
	require.NoError(t, err)
	lease.Items[0].CustomDomain = "web.example.com"
	lease.Items[1].CustomDomain = "worker.example.com"
	require.NoError(t, s.f.App.BillingKeeper.SetLease(s.f.Ctx, lease))
	message, broken := keeper.DerivedIndexesInvariant(s.f.App.BillingKeeper)(s.f.Ctx)
	require.False(t, broken, message)

	// A terminal lease retains its historical domains, while a later active
	// lease may claim the same names. Only current claims count as index rows.
	lease.State = types.LEASE_STATE_CLOSED
	closedAt := s.f.Ctx.BlockTime()
	lease.ClosedAt = &closedAt
	require.NoError(t, s.f.App.BillingKeeper.SetLease(s.f.Ctx, lease))
	message, broken = keeper.DerivedIndexesInvariant(s.f.App.BillingKeeper)(s.f.Ctx)
	require.False(t, broken, message)
	lease.Uuid = "00000000-0000-7000-8000-000000000099"
	lease.State = types.LEASE_STATE_ACTIVE
	lease.ClosedAt = nil
	require.NoError(t, s.f.App.BillingKeeper.SetLease(s.f.Ctx, lease))
	message, broken = keeper.DerivedIndexesInvariant(s.f.App.BillingKeeper)(s.f.Ctx)
	require.False(t, broken, message)
}

func TestDerivedIndexesInvariantRejectsWrongManualTargets(t *testing.T) {
	const domain = "valid.example.com"
	const wrongUUID = "00000000-0000-7000-8000-000000000099"
	tests := []struct {
		name    string
		corrupt func(*testing.T, *customDomainSetup)
		want    string
	}{
		{
			name: "SKU surplus wrong target",
			corrupt: func(t *testing.T, s *customDomainSetup) {
				require.NoError(t, s.f.App.BillingKeeper.LeaseBySKUIndex.Set(s.f.Ctx, collections.Join(wrongUUID, s.leaseUUID), true))
			},
			want: "without that SKU",
		},
		{
			name: "SKU same count wrong target",
			corrupt: func(t *testing.T, s *customDomainSetup) {
				require.NoError(t, s.f.App.BillingKeeper.LeaseBySKUIndex.Remove(s.f.Ctx, collections.Join(s.sku.Uuid, s.leaseUUID)))
				require.NoError(t, s.f.App.BillingKeeper.LeaseBySKUIndex.Set(s.f.Ctx, collections.Join(wrongUUID, s.leaseUUID), true))
			},
			want: "missing from the SKU index",
		},
		{
			name: "SKU surplus missing lease",
			corrupt: func(t *testing.T, s *customDomainSetup) {
				require.NoError(t, s.f.App.BillingKeeper.LeaseBySKUIndex.Set(s.f.Ctx, collections.Join(s.sku.Uuid, wrongUUID), true))
			},
			want: "references missing lease",
		},
		{
			name: "SKU false marker",
			corrupt: func(t *testing.T, s *customDomainSetup) {
				require.NoError(t, s.f.App.BillingKeeper.LeaseBySKUIndex.Set(s.f.Ctx, collections.Join(s.sku.Uuid, s.leaseUUID), false))
			},
			want: "false SKU-index marker",
		},
		{
			name: "domain same count wrong lease",
			corrupt: func(t *testing.T, s *customDomainSetup) {
				require.NoError(t, s.f.App.BillingKeeper.CustomDomainIndex.Set(s.f.Ctx, domain, types.CustomDomainTarget{LeaseUuid: wrongUUID}))
			},
			want: "incompatible with lease",
		},
		{
			name: "domain same count wrong item",
			corrupt: func(t *testing.T, s *customDomainSetup) {
				require.NoError(t, s.f.App.BillingKeeper.CustomDomainIndex.Set(s.f.Ctx, domain, types.CustomDomainTarget{
					LeaseUuid: s.leaseUUID, ServiceName: "absent",
				}))
			},
			want: "incompatible with lease",
		},
		{
			name: "domain same count wrong key",
			corrupt: func(t *testing.T, s *customDomainSetup) {
				require.NoError(t, s.f.App.BillingKeeper.CustomDomainIndex.Remove(s.f.Ctx, domain))
				require.NoError(t, s.f.App.BillingKeeper.CustomDomainIndex.Set(s.f.Ctx, "wrong.example.com", types.CustomDomainTarget{LeaseUuid: s.leaseUUID}))
			},
			want: "missing from the reverse index",
		},
		{
			name: "domain surplus wrong item",
			corrupt: func(t *testing.T, s *customDomainSetup) {
				require.NoError(t, s.f.App.BillingKeeper.CustomDomainIndex.Set(s.f.Ctx, "wrong.example.com", types.CustomDomainTarget{LeaseUuid: s.leaseUUID}))
			},
			want: "references missing item",
		},
		{
			name: "domain surplus terminal lease",
			corrupt: func(t *testing.T, s *customDomainSetup) {
				lease, err := s.f.App.BillingKeeper.GetLease(s.f.Ctx, s.leaseUUID)
				require.NoError(t, err)
				lease.Uuid = wrongUUID
				lease.State = types.LEASE_STATE_CLOSED
				lease.Items[0].CustomDomain = ""
				require.NoError(t, s.f.App.BillingKeeper.SetLease(s.f.Ctx, lease))
				require.NoError(t, s.f.App.BillingKeeper.CustomDomainIndex.Set(s.f.Ctx, "wrong.example.com", types.CustomDomainTarget{LeaseUuid: wrongUUID}))
			},
			want: "references terminal lease",
		},
		{
			name: "credit address wrong tenant",
			corrupt: func(t *testing.T, s *customDomainSetup) {
				require.NoError(t, s.f.App.BillingKeeper.CreditAddressIndex.Set(s.f.Ctx, types.DeriveCreditAddress(s.tenant), s.allowed))
			},
			want: "indexes tenant",
		},
		{
			name: "credit address surplus wrong key",
			corrupt: func(t *testing.T, s *customDomainSetup) {
				require.NoError(t, s.f.App.BillingKeeper.CreditAddressIndex.Set(s.f.Ctx, types.DeriveCreditAddress(s.allowed), s.tenant))
			},
			want: "does not match derived credit address",
		},
		{
			name: "credit address surplus missing tenant",
			corrupt: func(t *testing.T, s *customDomainSetup) {
				require.NoError(t, s.f.App.BillingKeeper.CreditAddressIndex.Set(s.f.Ctx, types.DeriveCreditAddress(s.allowed), s.allowed))
			},
			want: "references missing tenant",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := setupCustomDomain(t)
			_, err := s.f.App.BillingKeeper.SetItemCustomDomain(s.f.Ctx, s.tenant.String(), s.leaseUUID, "", domain)
			require.NoError(t, err)
			message, broken := keeper.DerivedIndexesInvariant(s.f.App.BillingKeeper)(s.f.Ctx)
			require.False(t, broken, message)
			test.corrupt(t, s)
			message, broken = keeper.DerivedIndexesInvariant(s.f.App.BillingKeeper)(s.f.Ctx)
			require.True(t, broken, message)
			require.Contains(t, message, test.want)
		})
	}
}
