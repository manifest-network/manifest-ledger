// Package interchaintest contains end-to-end tests for the billing module.
// This file covers the on-chain lease-update handshake: a tenant requests a new
// deployment manifest hash, and the provider acknowledges, rejects, or the
// tenant cancels it. The property under test throughout is that the committed
// meta_hash only advances on acknowledgement, so the payload a provider is
// already serving stays valid for the whole window.
//
// Run with: go test -v ./interchaintest -run TestBillingLeaseUpdate -timeout 45m
package interchaintest

import (
	"context"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/strangelove-ventures/interchaintest/v8/ibc"

	"github.com/manifest-network/manifest-ledger/interchaintest/helpers"
	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
)

// SHA-256-sized manifest hashes standing in for three manifest versions.
const (
	hashV0Hex = "0000000000000000000000000000000000000000000000000000000000000000"
	hashV1Hex = "1111111111111111111111111111111111111111111111111111111111111111"
	hashV2Hex = "2222222222222222222222222222222222222222222222222222222222222222"
)

func mustDecodeHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
}

// createAckedLeaseWithMetaHash creates a 1-item lease carrying metaHash as its
// revision-0 commitment and has the provider acknowledge it.
func createAckedLeaseWithMetaHash(t *testing.T, ctx context.Context, tc *billingTestContext, tenant ibc.Wallet, metaHash string) string {
	t.Helper()

	res, err := helpers.BillingCreateLeaseWithMetaHash(
		ctx, tc.chain, tenant, []string{fmt.Sprintf("%s:1", tc.skuUUID)}, metaHash,
	)
	require.NoError(t, err)
	txRes, err := tc.chain.GetTransaction(res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), txRes.Code, "lease creation should succeed: %s", txRes.RawLog)

	leaseUUID, err := helpers.GetLeaseIDFromTxHash(ctx, tc.chain, res.TxHash)
	require.NoError(t, err)

	ackRes, err := helpers.BillingAcknowledgeLease(ctx, tc.chain, tc.providerWallet, leaseUUID)
	require.NoError(t, err)
	ackTx, err := tc.chain.GetTransaction(ackRes.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), ackTx.Code, "acknowledge should succeed: %s", ackTx.RawLog)

	return leaseUUID
}

// TestBillingLeaseUpdate exercises the four-verb lease-update handshake
// end-to-end.
func TestBillingLeaseUpdate(t *testing.T) {
	ctx, tc, cleanup := setupBillingTest(t, "billing-lease-update-test")
	t.Cleanup(cleanup)

	testPWRDenom = tc.pwrDenom
	testProviderUUID = tc.providerUUID
	testSKUUUID = tc.skuUUID

	fundTenantCredit(t, ctx, tc, tc.tenant1, 100_000_000)

	lease := createAckedLeaseWithMetaHash(t, ctx, tc, tc.tenant1, hashV0Hex)

	t.Run("request does not advance the committed hash", func(t *testing.T) {
		res, err := helpers.BillingUpdateLease(ctx, tc.chain, tc.tenant1, lease, hashV1Hex)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "update request should succeed: %s", txRes.RawLog)

		got, err := helpers.BillingQueryLease(ctx, tc.chain, lease)
		require.NoError(t, err)
		require.Equal(t, mustDecodeHex(t, hashV0Hex), got.Lease.MetaHash,
			"committed hash must not move until the provider acknowledges")
		require.Equal(t, mustDecodeHex(t, hashV1Hex), got.Lease.PendingMetaHash)
		require.NotNil(t, got.Lease.PendingMetaHashAt)
		require.Empty(t, got.Lease.MetaHashRevision, "still revision 0")

		// The provider can discover the request without scanning its leases.
		pending, err := helpers.BillingQueryPendingLeaseUpdates(ctx, tc.chain, tc.providerUUID)
		require.NoError(t, err)
		require.Len(t, pending.Leases, 1)
		require.Equal(t, lease, pending.Leases[0].Uuid)
	})

	t.Run("tenant cannot acknowledge its own update", func(t *testing.T) {
		res, err := helpers.BillingAcknowledgeLeaseUpdate(ctx, tc.chain, tc.tenant1, lease, hashV1Hex)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "tenant-signed acknowledgement must be rejected")
		require.Contains(t, txRes.RawLog, billingtypes.ErrUnauthorized.Error())
	})

	t.Run("acknowledging a stale hash is rejected", func(t *testing.T) {
		res, err := helpers.BillingAcknowledgeLeaseUpdate(ctx, tc.chain, tc.providerWallet, lease, hashV2Hex)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "hash mismatch must be rejected")
		require.Contains(t, txRes.RawLog, billingtypes.ErrLeaseUpdateMismatch.Error())
	})

	t.Run("provider acknowledgement promotes the hash and bumps the revision", func(t *testing.T) {
		res, err := helpers.BillingAcknowledgeLeaseUpdate(ctx, tc.chain, tc.providerWallet, lease, hashV1Hex)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "acknowledgement should succeed: %s", txRes.RawLog)

		got, err := helpers.BillingQueryLease(ctx, tc.chain, lease)
		require.NoError(t, err)
		require.Equal(t, mustDecodeHex(t, hashV1Hex), got.Lease.MetaHash)
		require.Empty(t, got.Lease.PendingMetaHash)
		require.Nil(t, got.Lease.PendingMetaHashAt)
		require.Equal(t, "1", got.Lease.MetaHashRevision)

		// Off the provider's work list.
		pending, err := helpers.BillingQueryPendingLeaseUpdates(ctx, tc.chain, tc.providerUUID)
		require.NoError(t, err)
		require.Empty(t, pending.Leases)
	})

	t.Run("acknowledging with nothing pending is rejected", func(t *testing.T) {
		res, err := helpers.BillingAcknowledgeLeaseUpdate(ctx, tc.chain, tc.providerWallet, lease, hashV2Hex)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code)
		require.Contains(t, txRes.RawLog, billingtypes.ErrNoPendingLeaseUpdate.Error())
	})

	t.Run("rejection leaves the committed hash and revision untouched", func(t *testing.T) {
		res, err := helpers.BillingUpdateLease(ctx, tc.chain, tc.tenant1, lease, hashV2Hex)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code, "update request should succeed: %s", txRes.RawLog)

		rejRes, err := helpers.BillingRejectLeaseUpdate(ctx, tc.chain, tc.providerWallet, lease, hashV2Hex, "unsupported image")
		require.NoError(t, err)
		rejTx, err := tc.chain.GetTransaction(rejRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), rejTx.Code, "rejection should succeed: %s", rejTx.RawLog)

		got, err := helpers.BillingQueryLease(ctx, tc.chain, lease)
		require.NoError(t, err)
		require.Equal(t, mustDecodeHex(t, hashV1Hex), got.Lease.MetaHash)
		require.Empty(t, got.Lease.PendingMetaHash)
		require.Equal(t, "1", got.Lease.MetaHashRevision, "a rejection is not a new revision")
	})

	t.Run("tenant can cancel its own request", func(t *testing.T) {
		res, err := helpers.BillingUpdateLease(ctx, tc.chain, tc.tenant1, lease, hashV2Hex)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		cancelRes, err := helpers.BillingCancelLeaseUpdate(ctx, tc.chain, tc.tenant1, lease)
		require.NoError(t, err)
		cancelTx, err := tc.chain.GetTransaction(cancelRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), cancelTx.Code, "cancel should succeed: %s", cancelTx.RawLog)

		got, err := helpers.BillingQueryLease(ctx, tc.chain, lease)
		require.NoError(t, err)
		require.Empty(t, got.Lease.PendingMetaHash)
		require.Equal(t, mustDecodeHex(t, hashV1Hex), got.Lease.MetaHash)
	})

	t.Run("unauthorized sender cannot request an update", func(t *testing.T) {
		res, err := helpers.BillingUpdateLease(ctx, tc.chain, tc.unauthorizedUser, lease, hashV2Hex)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code, "stranger must not be able to request an update")
		require.Contains(t, txRes.RawLog, billingtypes.ErrUnauthorized.Error())
	})

	t.Run("closing the lease clears a pending request", func(t *testing.T) {
		res, err := helpers.BillingUpdateLease(ctx, tc.chain, tc.tenant1, lease, hashV2Hex)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), txRes.Code)

		closeRes, err := helpers.BillingCloseLease(ctx, tc.chain, tc.tenant1, lease)
		require.NoError(t, err)
		closeTx, err := tc.chain.GetTransaction(closeRes.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint32(0), closeTx.Code, "close should succeed: %s", closeTx.RawLog)

		got, err := helpers.BillingQueryLease(ctx, tc.chain, lease)
		require.NoError(t, err)
		require.Equal(t, billingtypes.LEASE_STATE_CLOSED, got.Lease.GetState())
		require.Empty(t, got.Lease.PendingMetaHash)
		require.Equal(t, mustDecodeHex(t, hashV1Hex), got.Lease.MetaHash,
			"the last acknowledged manifest stays on the record for audit")

		pending, err := helpers.BillingQueryPendingLeaseUpdates(ctx, tc.chain, tc.providerUUID)
		require.NoError(t, err)
		require.Empty(t, pending.Leases)
	})

	t.Run("updates are rejected on a closed lease", func(t *testing.T) {
		res, err := helpers.BillingUpdateLease(ctx, tc.chain, tc.tenant1, lease, hashV2Hex)
		require.NoError(t, err)
		txRes, err := tc.chain.GetTransaction(res.TxHash)
		require.NoError(t, err)
		require.NotEqual(t, uint32(0), txRes.Code)
		require.Contains(t, txRes.RawLog, billingtypes.ErrLeaseNotActive.Error())
	})
}
