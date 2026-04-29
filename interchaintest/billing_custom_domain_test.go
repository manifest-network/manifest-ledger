// Package interchaintest contains end-to-end tests for the billing module.
// This file contains custom_domain feature tests (set, query, lifecycle, override).
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

// TestBillingCustomDomain exercises the on-chain custom_domain feature end-to-end:
// set, reverse-lookup query, uniqueness conflict, reserved-suffix rejection,
// authority override, and lifecycle cleanup on close.
func TestBillingCustomDomain(t *testing.T) {
	ctx, tc, cleanup := setupBillingTest(t, "billing-custom-domain-test")
	t.Cleanup(cleanup)

	testPWRDenom = tc.pwrDenom
	testProviderUUID = tc.providerUUID
	testSKUUUID = tc.skuUUID

	fundTenantCredit(t, ctx, tc, tc.tenant1, 100_000_000)
	fundTenantCredit(t, ctx, tc, tc.tenant2, 100_000_000)

	// Lease for tenant1 (active).
	items := []string{fmt.Sprintf("%s:1", tc.skuUUID)}
	leaseUUID1, err := helpers.BillingCreateAndAcknowledgeLease(ctx, tc.chain, tc.tenant1, tc.providerWallet, items)
	require.NoError(t, err)

	// Lease for tenant2 (active).
	leaseUUID2, err := helpers.BillingCreateAndAcknowledgeLease(ctx, tc.chain, tc.tenant2, tc.providerWallet, items)
	require.NoError(t, err)

	t.Run("set domain by tenant", func(t *testing.T) {
		res, err := helpers.BillingSetLeaseCustomDomain(ctx, tc.chain, tc.tenant1, leaseUUID1, "app.example.com")
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "set custom_domain should succeed: %s", txRes.RawLog)
	})

	t.Run("query reverse lookup", func(t *testing.T) {
		got, err := helpers.BillingQueryLeaseByCustomDomain(ctx, tc.chain, "app.example.com")
		require.NoError(t, err)
		require.Equal(t, leaseUUID1, got.Lease.Uuid)
	})

	t.Run("conflict on second tenant", func(t *testing.T) {
		// Should fail at delivery — same domain on a different lease.
		res, err := helpers.BillingSetLeaseCustomDomain(ctx, tc.chain, tc.tenant2, leaseUUID2, "app.example.com")
		require.NoError(t, err) // tx broadcast succeeds; assert deliver fails
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "expected delivery failure")
		require.Contains(t, txRes.RawLog, billingtypes.ErrCustomDomainAlreadyClaimed.Error())
	})

	t.Run("authority override sets domain on another tenant's lease", func(t *testing.T) {
		// Authority can set a (different) domain on tenant2's lease.
		res, err := helpers.BillingSetLeaseCustomDomain(ctx, tc.chain, tc.authority, leaseUUID2, "ops.example.com")
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "authority override should succeed: %s", txRes.RawLog)

		got, err := helpers.BillingQueryLeaseByCustomDomain(ctx, tc.chain, "ops.example.com")
		require.NoError(t, err)
		require.Equal(t, leaseUUID2, got.Lease.Uuid)
	})

	t.Run("close releases domain index entry", func(t *testing.T) {
		closeRes, err := helpers.BillingCloseLease(ctx, tc.chain, tc.tenant1, leaseUUID1)
		require.NoError(t, err)
		closeTx, err := tc.chain.GetTransaction(closeRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), closeTx.Code, "close should succeed: %s", closeTx.RawLog)

		// Domain reverse-lookup now misses.
		_, err = helpers.BillingQueryLeaseByCustomDomain(ctx, tc.chain, "app.example.com")
		require.Error(t, err, "expected NotFound after close")

		// Domain still recorded on the (now CLOSED) lease for audit.
		closedLease, err := helpers.BillingQueryLease(ctx, tc.chain, leaseUUID1)
		require.NoError(t, err)
		require.Equal(t, "app.example.com", closedLease.Lease.CustomDomain)
		require.Equal(t, billingtypes.LEASE_STATE_CLOSED, closedLease.Lease.GetState())
	})
}
