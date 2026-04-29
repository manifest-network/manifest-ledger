// Package interchaintest contains end-to-end tests for the billing module.
// This file contains custom_domain feature tests at the LeaseItem level:
// per-item set, reverse-lookup query (with service_name in response),
// uniqueness conflicts, reserved-suffix rejection, authority override,
// allowed-list sender, lifecycle cleanup on close, and 1-item legacy support.
//
// Run with: go test -v ./interchaintest -run TestBillingCustomDomain -timeout 45m
package interchaintest

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/manifest-network/manifest-ledger/interchaintest/helpers"
	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
)

// TestBillingCustomDomain exercises the on-chain custom_domain feature
// end-to-end at the LeaseItem level, covering both 1-item legacy leases and
// multi-item service-mode leases.
func TestBillingCustomDomain(t *testing.T) {
	ctx, tc, cleanup := setupBillingTest(t, "billing-custom-domain-test")
	t.Cleanup(cleanup)

	testPWRDenom = tc.pwrDenom
	testProviderUUID = tc.providerUUID
	testSKUUUID = tc.skuUUID

	fundTenantCredit(t, ctx, tc, tc.tenant1, 100_000_000)
	fundTenantCredit(t, ctx, tc, tc.tenant2, 100_000_000)

	// Multi-item service-mode lease for tenant1: web + db.
	multiItems := []string{
		fmt.Sprintf("%s:1:web", tc.skuUUID),
		fmt.Sprintf("%s:1:db", tc.skuUUID),
	}
	multiLease, err := helpers.BillingCreateAndAcknowledgeLease(ctx, tc.chain, tc.tenant1, tc.providerWallet, multiItems)
	require.NoError(t, err)

	// 1-item legacy lease for tenant2 (no service_name on the item).
	legacyItems := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
	legacyLease, err := helpers.BillingCreateAndAcknowledgeLease(ctx, tc.chain, tc.tenant2, tc.providerWallet, legacyItems)
	require.NoError(t, err)

	t.Run("multi-item: per-item domains", func(t *testing.T) {
		// Set domain on web.
		res, err := helpers.BillingSetLeaseItemCustomDomain(ctx, tc.chain, tc.tenant1, multiLease, "web", "web.example.com")
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "set web custom_domain should succeed: %s", txRes.RawLog)

		// Set domain on db.
		res, err = helpers.BillingSetLeaseItemCustomDomain(ctx, tc.chain, tc.tenant1, multiLease, "db", "db.example.com")
		require.NoError(t, err)
		txRes, err = tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "set db custom_domain should succeed: %s", txRes.RawLog)

		// Reverse-query both, verify service_name attribution.
		got, err := helpers.BillingQueryLeaseByCustomDomain(ctx, tc.chain, "web.example.com")
		require.NoError(t, err)
		require.Equal(t, multiLease, got.Lease.Uuid)
		require.Equal(t, "web", got.ServiceName)

		got, err = helpers.BillingQueryLeaseByCustomDomain(ctx, tc.chain, "db.example.com")
		require.NoError(t, err)
		require.Equal(t, multiLease, got.Lease.Uuid)
		require.Equal(t, "db", got.ServiceName)
	})

	t.Run("1-item legacy: empty service_name addresses the only item", func(t *testing.T) {
		res, err := helpers.BillingSetLeaseItemCustomDomain(ctx, tc.chain, tc.tenant2, legacyLease, "", "legacy.example.com")
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "1-item legacy set should succeed: %s", txRes.RawLog)

		got, err := helpers.BillingQueryLeaseByCustomDomain(ctx, tc.chain, "legacy.example.com")
		require.NoError(t, err)
		require.Equal(t, legacyLease, got.Lease.Uuid)
		require.Equal(t, "", got.ServiceName, "1-item legacy lease returns empty service_name")
	})

	t.Run("cross-lease conflict", func(t *testing.T) {
		// tenant2 tries to claim "web.example.com" on the legacy lease — already claimed by multi-item lease's web item.
		res, err := helpers.BillingSetLeaseItemCustomDomain(ctx, tc.chain, tc.tenant2, legacyLease, "", "web.example.com")
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "expected delivery failure")
		require.Contains(t, txRes.RawLog, billingtypes.ErrCustomDomainAlreadyClaimed.Error())
	})

	t.Run("authority override on a different tenant's lease", func(t *testing.T) {
		// Authority sets a domain on tenant1's multi-item lease, addressing the db item.
		// Override via the new domain that nobody has claimed.
		res, err := helpers.BillingSetLeaseItemCustomDomain(ctx, tc.chain, tc.authority, multiLease, "db", "ops.example.com")
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "authority override should succeed: %s", txRes.RawLog)

		got, err := helpers.BillingQueryLeaseByCustomDomain(ctx, tc.chain, "ops.example.com")
		require.NoError(t, err)
		require.Equal(t, multiLease, got.Lease.Uuid)
		require.Equal(t, "db", got.ServiceName)
	})

	t.Run("clear domain leaves siblings intact", func(t *testing.T) {
		// Clear the multi-item lease's web domain; db's "ops.example.com" must still resolve.
		res, err := helpers.BillingSetLeaseItemCustomDomain(ctx, tc.chain, tc.tenant1, multiLease, "web", "")
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "clear should succeed: %s", txRes.RawLog)

		_, err = helpers.BillingQueryLeaseByCustomDomain(ctx, tc.chain, "web.example.com")
		require.Error(t, err, "expected NotFound after clear")

		got, err := helpers.BillingQueryLeaseByCustomDomain(ctx, tc.chain, "ops.example.com")
		require.NoError(t, err)
		require.Equal(t, multiLease, got.Lease.Uuid)
		require.Equal(t, "db", got.ServiceName)
	})

	t.Run("allowed-list sender can set domain", func(t *testing.T) {
		_, err := helpers.BillingUpdateParamsFull(ctx, tc.chain, tc.authority,
			100, 20, 3600, 10, 1800,
			[]string{tc.unauthorizedUser.FormattedAddress()},
			nil,
		)
		require.NoError(t, err)

		// Allowed-list sender re-claims web.example.com on tenant1's multi-item lease.
		res, err := helpers.BillingSetLeaseItemCustomDomain(ctx, tc.chain, tc.unauthorizedUser, multiLease, "web", "allowed.example.com")
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "allowed-list sender should succeed: %s", txRes.RawLog)

		got, err := helpers.BillingQueryLeaseByCustomDomain(ctx, tc.chain, "allowed.example.com")
		require.NoError(t, err)
		require.Equal(t, multiLease, got.Lease.Uuid)
		require.Equal(t, "web", got.ServiceName)
	})

	t.Run("reserved suffix rejected", func(t *testing.T) {
		_, err := helpers.BillingUpdateParamsFull(ctx, tc.chain, tc.authority,
			100, 20, 3600, 10, 1800,
			[]string{tc.unauthorizedUser.FormattedAddress()},
			[]string{".barney0.manifest0.net"},
		)
		require.NoError(t, err)

		res, err := helpers.BillingSetLeaseItemCustomDomain(ctx, tc.chain, tc.tenant1, multiLease, "db", "x.barney0.manifest0.net")
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "reserved-suffix claim must be rejected")
		require.Contains(t, txRes.RawLog, billingtypes.ErrInvalidCustomDomain.Error())
	})

	t.Run("close releases all per-item index entries; audit fields preserved", func(t *testing.T) {
		closeRes, err := helpers.BillingCloseLease(ctx, tc.chain, tc.tenant1, multiLease)
		require.NoError(t, err)
		closeTx, err := tc.chain.GetTransaction(closeRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), closeTx.Code, "close should succeed: %s", closeTx.RawLog)

		// Both per-item index entries are gone.
		for _, dom := range []string{"allowed.example.com", "ops.example.com"} {
			_, err := helpers.BillingQueryLeaseByCustomDomain(ctx, tc.chain, dom)
			require.Error(t, err, "expected NotFound for %s after close", dom)
		}

		// Lease record preserves per-item CustomDomain for audit.
		closed, err := helpers.BillingQueryLease(ctx, tc.chain, multiLease)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_CLOSED, closed.Lease.GetState())
		domainsByService := map[string]string{}
		for _, item := range closed.Lease.Items {
			domainsByService[item.ServiceName] = item.CustomDomain
		}
		require.Equal(t, "allowed.example.com", domainsByService["web"])
		require.Equal(t, "ops.example.com", domainsByService["db"])
	})

	t.Run("reclaim after close on a fresh lease", func(t *testing.T) {
		newLease, err := helpers.BillingCreateAndAcknowledgeLease(ctx, tc.chain, tc.tenant1, tc.providerWallet, multiItems)
		require.NoError(t, err)

		res, err := helpers.BillingSetLeaseItemCustomDomain(ctx, tc.chain, tc.tenant1, newLease, "web", "allowed.example.com")
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "reclaim should succeed: %s", txRes.RawLog)

		got, err := helpers.BillingQueryLeaseByCustomDomain(ctx, tc.chain, "allowed.example.com")
		require.NoError(t, err)
		require.Equal(t, newLease, got.Lease.Uuid)
		require.Equal(t, "web", got.ServiceName)
	})
}
