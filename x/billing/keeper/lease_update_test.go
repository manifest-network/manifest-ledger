package keeper_test

import (
	"encoding/hex"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

var (
	hashV0 = []byte("00000000000000000000000000000000")
	hashV1 = []byte("11111111111111111111111111111111")
	hashV2 = []byte("22222222222222222222222222222222")
)

// leaseUpdateSetup spins up a fixture with one tenant + provider and a 1-item
// ACTIVE lease whose meta_hash is hashV0 (revision 0).
type leaseUpdateSetup struct {
	f            *testFixture
	msgServer    types.MsgServer
	tenant       sdk.AccAddress
	provider     skutypes.Provider
	providerAddr sdk.AccAddress
	allowed      sdk.AccAddress
	stranger     sdk.AccAddress
	sku          skutypes.SKU
	leaseUUID    string
}

func setupLeaseUpdate(t *testing.T) *leaseUpdateSetup {
	t.Helper()
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	payoutAddr := f.TestAccs[2]
	allowed := f.TestAccs[3]
	stranger := f.TestAccs[4]

	provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100_000_000))))
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	}))

	params, err := f.App.BillingKeeper.GetParams(f.Ctx)
	require.NoError(t, err)
	params.AllowedList = []string{allowed.String()}
	require.NoError(t, f.App.BillingKeeper.SetParams(f.Ctx, params))

	s := &leaseUpdateSetup{
		f: f, msgServer: msgServer,
		tenant: tenant, provider: provider, providerAddr: providerAddr,
		allowed: allowed, stranger: stranger,
		sku: sku,
	}
	s.leaseUUID = s.createLeaseWithMetaHash(t, hashV0)
	return s
}

// createLeaseWithMetaHash creates and acknowledges a 1-item lease carrying
// metaHash as its creation-time (revision 0) commitment.
func (s *leaseUpdateSetup) createLeaseWithMetaHash(t *testing.T, metaHash []byte) string {
	t.Helper()
	createResp, err := s.msgServer.CreateLease(s.f.Ctx, &types.MsgCreateLease{
		Tenant:   s.tenant.String(),
		Items:    []types.LeaseItemInput{{SkuUuid: s.sku.Uuid, Quantity: 1}},
		MetaHash: metaHash,
	})
	require.NoError(t, err)

	_, err = s.msgServer.AcknowledgeLease(s.f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     s.providerAddr.String(),
		LeaseUuids: []string{createResp.LeaseUuid},
	})
	require.NoError(t, err)

	return createResp.LeaseUuid
}

func (s *leaseUpdateSetup) lease(t *testing.T) types.Lease {
	t.Helper()
	lease, err := s.f.App.BillingKeeper.GetLease(s.f.Ctx, s.leaseUUID)
	require.NoError(t, err)
	return lease
}

// hasIndexEntry reports whether the PendingUpdateIndex holds an entry for the
// fixture's lease under its provider.
func (s *leaseUpdateSetup) hasIndexEntry(t *testing.T) bool {
	t.Helper()
	has, err := s.f.App.BillingKeeper.PendingUpdateIndex.Has(
		s.f.Ctx, collections.Join(s.provider.Uuid, s.leaseUUID),
	)
	require.NoError(t, err)
	return has
}

// --- happy path ---

// TestLeaseUpdate_RequestDoesNotAdvanceMetaHash is the property the whole
// two-phase design exists for: while an update is pending, the committed
// meta_hash still names the payload the provider is serving, so a reprovision
// during the window succeeds against the OLD payload.
func TestLeaseUpdate_RequestDoesNotAdvanceMetaHash(t *testing.T) {
	s := setupLeaseUpdate(t)

	role, _, err := s.f.App.BillingKeeper.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
	require.NoError(t, err)
	require.Equal(t, types.AttributeValueRoleTenant, role)

	lease := s.lease(t)
	require.Equal(t, hashV0, lease.MetaHash, "committed hash must not move on request")
	require.Equal(t, hashV1, lease.PendingMetaHash)
	require.NotNil(t, lease.PendingMetaHashAt)
	require.Equal(t, uint64(0), lease.MetaHashRevision)
	require.True(t, s.hasIndexEntry(t))
}

func TestLeaseUpdate_AcknowledgePromotesAndBumpsRevision(t *testing.T) {
	s := setupLeaseUpdate(t)

	_, _, err := s.f.App.BillingKeeper.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
	require.NoError(t, err)

	revision, err := s.f.App.BillingKeeper.AcknowledgeLeaseUpdate(s.f.Ctx, s.providerAddr.String(), s.leaseUUID, hashV1)
	require.NoError(t, err)
	require.Equal(t, uint64(1), revision)

	lease := s.lease(t)
	require.Equal(t, hashV1, lease.MetaHash)
	require.Empty(t, lease.PendingMetaHash)
	require.Nil(t, lease.PendingMetaHashAt)
	require.Equal(t, uint64(1), lease.MetaHashRevision)
	require.False(t, s.hasIndexEntry(t))
}

func TestLeaseUpdate_RejectLeavesMetaHashUntouched(t *testing.T) {
	s := setupLeaseUpdate(t)

	_, _, err := s.f.App.BillingKeeper.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
	require.NoError(t, err)

	require.NoError(t, s.f.App.BillingKeeper.RejectLeaseUpdate(
		s.f.Ctx, s.providerAddr.String(), s.leaseUUID, hashV1, "unsupported image",
	))

	lease := s.lease(t)
	require.Equal(t, hashV0, lease.MetaHash)
	require.Empty(t, lease.PendingMetaHash)
	require.Nil(t, lease.PendingMetaHashAt)
	require.Equal(t, uint64(0), lease.MetaHashRevision, "a rejection is not a new revision")
	require.False(t, s.hasIndexEntry(t))
}

func TestLeaseUpdate_CancelWithdrawsRequest(t *testing.T) {
	s := setupLeaseUpdate(t)

	_, _, err := s.f.App.BillingKeeper.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
	require.NoError(t, err)

	role, err := s.f.App.BillingKeeper.CancelLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID)
	require.NoError(t, err)
	require.Equal(t, types.AttributeValueRoleTenant, role)

	lease := s.lease(t)
	require.Equal(t, hashV0, lease.MetaHash)
	require.Empty(t, lease.PendingMetaHash)
	require.Equal(t, uint64(0), lease.MetaHashRevision)
	require.False(t, s.hasIndexEntry(t))
}

// TestLeaseUpdate_RevisionMonotonicAcrossCycles also covers the TUF-style
// rollback case: re-committing an EARLIER manifest is still a new, higher
// revision, so a rollback is a visible event rather than a silent rewind.
func TestLeaseUpdate_RevisionMonotonicAcrossCycles(t *testing.T) {
	s := setupLeaseUpdate(t)
	k := s.f.App.BillingKeeper

	var want uint64
	for _, h := range [][]byte{hashV1, hashV2, hashV0} {
		want++

		_, _, err := k.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, h)
		require.NoError(t, err)

		revision, err := k.AcknowledgeLeaseUpdate(s.f.Ctx, s.providerAddr.String(), s.leaseUUID, h)
		require.NoError(t, err)
		require.Equal(t, want, revision)

		lease := s.lease(t)
		require.Equal(t, h, lease.MetaHash)
	}

	require.Equal(t, uint64(3), s.lease(t).MetaHashRevision)
}

// TestLeaseUpdate_RequestOnLeaseWithoutInitialHash covers a lease created by a
// fixed-SKU provider with no meta_hash: the update path can introduce a
// commitment where there was none.
func TestLeaseUpdate_RequestOnLeaseWithoutInitialHash(t *testing.T) {
	s := setupLeaseUpdate(t)
	s.leaseUUID = s.createLeaseWithMetaHash(t, nil)
	require.Empty(t, s.lease(t).MetaHash)

	_, _, err := s.f.App.BillingKeeper.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
	require.NoError(t, err)
	_, err = s.f.App.BillingKeeper.AcknowledgeLeaseUpdate(s.f.Ctx, s.providerAddr.String(), s.leaseUUID, hashV1)
	require.NoError(t, err)

	require.Equal(t, hashV1, s.lease(t).MetaHash)
}

// --- authorisation ---

func TestLeaseUpdate_RequestAuthorisationMatrix(t *testing.T) {
	tests := []struct {
		name     string
		who      func(*leaseUpdateSetup) sdk.AccAddress
		wantRole string
		wantErr  bool
	}{
		{"tenant", func(s *leaseUpdateSetup) sdk.AccAddress { return s.tenant }, types.AttributeValueRoleTenant, false},
		{"authority", func(s *leaseUpdateSetup) sdk.AccAddress { return s.f.Authority }, types.AttributeValueRoleAuthority, false},
		{"allowed_list", func(s *leaseUpdateSetup) sdk.AccAddress { return s.allowed }, types.AttributeValueRoleAllowed, false},
		{"stranger", func(s *leaseUpdateSetup) sdk.AccAddress { return s.stranger }, "", true},
		{"provider", func(s *leaseUpdateSetup) sdk.AccAddress { return s.providerAddr }, "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := setupLeaseUpdate(t)
			role, _, err := s.f.App.BillingKeeper.RequestLeaseUpdate(s.f.Ctx, tc.who(s).String(), s.leaseUUID, hashV1)
			if tc.wantErr {
				require.ErrorIs(t, err, types.ErrUnauthorized)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantRole, role)
		})
	}
}

// TestLeaseUpdate_AcknowledgeAuthorisationMatrix pins down that allowed_list
// grants tenant-side verbs only: standing in for a provider is a different
// power and this feature must not widen it.
func TestLeaseUpdate_AcknowledgeAuthorisationMatrix(t *testing.T) {
	tests := []struct {
		name    string
		who     func(*leaseUpdateSetup) sdk.AccAddress
		wantErr bool
	}{
		{"provider", func(s *leaseUpdateSetup) sdk.AccAddress { return s.providerAddr }, false},
		{"authority", func(s *leaseUpdateSetup) sdk.AccAddress { return s.f.Authority }, false},
		{"tenant", func(s *leaseUpdateSetup) sdk.AccAddress { return s.tenant }, true},
		{"allowed_list", func(s *leaseUpdateSetup) sdk.AccAddress { return s.allowed }, true},
		{"stranger", func(s *leaseUpdateSetup) sdk.AccAddress { return s.stranger }, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := setupLeaseUpdate(t)
			_, _, err := s.f.App.BillingKeeper.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
			require.NoError(t, err)

			_, err = s.f.App.BillingKeeper.AcknowledgeLeaseUpdate(s.f.Ctx, tc.who(s).String(), s.leaseUUID, hashV1)
			if tc.wantErr {
				require.ErrorIs(t, err, types.ErrUnauthorized)
				require.Equal(t, hashV0, s.lease(t).MetaHash, "unauthorised call must not promote")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestLeaseUpdate_RejectAuthorisationMatrix(t *testing.T) {
	tests := []struct {
		name    string
		who     func(*leaseUpdateSetup) sdk.AccAddress
		wantErr bool
	}{
		{"provider", func(s *leaseUpdateSetup) sdk.AccAddress { return s.providerAddr }, false},
		{"authority", func(s *leaseUpdateSetup) sdk.AccAddress { return s.f.Authority }, false},
		{"tenant", func(s *leaseUpdateSetup) sdk.AccAddress { return s.tenant }, true},
		{"allowed_list", func(s *leaseUpdateSetup) sdk.AccAddress { return s.allowed }, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := setupLeaseUpdate(t)
			_, _, err := s.f.App.BillingKeeper.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
			require.NoError(t, err)

			err = s.f.App.BillingKeeper.RejectLeaseUpdate(s.f.Ctx, tc.who(s).String(), s.leaseUUID, hashV1, "no")
			if tc.wantErr {
				require.ErrorIs(t, err, types.ErrUnauthorized)
				require.NotEmpty(t, s.lease(t).PendingMetaHash, "unauthorised call must not clear")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestLeaseUpdate_CancelAuthorisationMatrix(t *testing.T) {
	tests := []struct {
		name    string
		who     func(*leaseUpdateSetup) sdk.AccAddress
		wantErr bool
	}{
		{"tenant", func(s *leaseUpdateSetup) sdk.AccAddress { return s.tenant }, false},
		{"authority", func(s *leaseUpdateSetup) sdk.AccAddress { return s.f.Authority }, false},
		{"allowed_list", func(s *leaseUpdateSetup) sdk.AccAddress { return s.allowed }, false},
		{"stranger", func(s *leaseUpdateSetup) sdk.AccAddress { return s.stranger }, true},
		{"provider", func(s *leaseUpdateSetup) sdk.AccAddress { return s.providerAddr }, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := setupLeaseUpdate(t)
			_, _, err := s.f.App.BillingKeeper.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
			require.NoError(t, err)

			_, err = s.f.App.BillingKeeper.CancelLeaseUpdate(s.f.Ctx, tc.who(s).String(), s.leaseUUID)
			if tc.wantErr {
				require.ErrorIs(t, err, types.ErrUnauthorized)
				return
			}
			require.NoError(t, err)
		})
	}
}

// --- state gate ---

// TestLeaseUpdate_RejectedOnPendingLease documents the deliberate ACTIVE-only
// gate: changing the hash on a PENDING lease would race the provider's
// MsgAcknowledgeLease, so tenants cancel and recreate instead.
func TestLeaseUpdate_RejectedOnPendingLease(t *testing.T) {
	s := setupLeaseUpdate(t)

	createResp, err := s.msgServer.CreateLease(s.f.Ctx, &types.MsgCreateLease{
		Tenant:   s.tenant.String(),
		Items:    []types.LeaseItemInput{{SkuUuid: s.sku.Uuid, Quantity: 1}},
		MetaHash: hashV0,
	})
	require.NoError(t, err)

	_, _, err = s.f.App.BillingKeeper.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), createResp.LeaseUuid, hashV1)
	require.ErrorIs(t, err, types.ErrLeaseNotActive)

	// Not even the authority may bypass the gate.
	_, _, err = s.f.App.BillingKeeper.RequestLeaseUpdate(s.f.Ctx, s.f.Authority.String(), createResp.LeaseUuid, hashV1)
	require.ErrorIs(t, err, types.ErrLeaseNotActive)
}

func TestLeaseUpdate_RejectedOnClosedLease(t *testing.T) {
	s := setupLeaseUpdate(t)
	k := s.f.App.BillingKeeper

	_, err := s.msgServer.CloseLease(s.f.Ctx, &types.MsgCloseLease{
		Sender:     s.tenant.String(),
		LeaseUuids: []string{s.leaseUUID},
	})
	require.NoError(t, err)

	_, _, err = k.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
	require.ErrorIs(t, err, types.ErrLeaseNotActive)

	_, err = k.AcknowledgeLeaseUpdate(s.f.Ctx, s.providerAddr.String(), s.leaseUUID, hashV1)
	require.ErrorIs(t, err, types.ErrLeaseNotActive)

	err = k.RejectLeaseUpdate(s.f.Ctx, s.providerAddr.String(), s.leaseUUID, hashV1, "")
	require.ErrorIs(t, err, types.ErrLeaseNotActive)

	_, err = k.CancelLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID)
	require.ErrorIs(t, err, types.ErrLeaseNotActive)
}

func TestLeaseUpdate_UnknownLease(t *testing.T) {
	s := setupLeaseUpdate(t)
	const missing = "01902a9b-1234-7000-8000-0000000000ff"

	_, _, err := s.f.App.BillingKeeper.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), missing, hashV1)
	require.Error(t, err)
}

// --- supersession and hash guards ---

// TestLeaseUpdate_ReRequestSupersedes checks the second request replaces the
// first outright, so the provider can never end up with two live requests.
func TestLeaseUpdate_ReRequestSupersedes(t *testing.T) {
	s := setupLeaseUpdate(t)
	k := s.f.App.BillingKeeper

	_, _, err := k.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
	require.NoError(t, err)
	firstAt := s.lease(t).PendingMetaHashAt
	require.NotNil(t, firstAt)

	_, _, err = k.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV2)
	require.NoError(t, err)

	lease := s.lease(t)
	require.Equal(t, hashV2, lease.PendingMetaHash)
	require.Equal(t, hashV0, lease.MetaHash)

	// The supersession is visible to indexers.
	ev := findEvent(t, s.f.Ctx, types.EventTypeLeaseUpdateRequested)
	require.Equal(t, hex.EncodeToString(hashV1), attrValue(t, ev, types.AttributeKeySupersededMetaHash))
}

// TestLeaseUpdate_AcknowledgeStaleHashRejected is the race guard: a provider
// that evaluated hashV1 must not accidentally commit the tenant's newer hashV2,
// which it has never seen.
func TestLeaseUpdate_AcknowledgeStaleHashRejected(t *testing.T) {
	s := setupLeaseUpdate(t)
	k := s.f.App.BillingKeeper

	_, _, err := k.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
	require.NoError(t, err)
	_, _, err = k.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV2)
	require.NoError(t, err)

	_, err = k.AcknowledgeLeaseUpdate(s.f.Ctx, s.providerAddr.String(), s.leaseUUID, hashV1)
	require.ErrorIs(t, err, types.ErrLeaseUpdateMismatch)

	lease := s.lease(t)
	require.Equal(t, hashV0, lease.MetaHash)
	require.Equal(t, hashV2, lease.PendingMetaHash)
	require.Equal(t, uint64(0), lease.MetaHashRevision)
}

func TestLeaseUpdate_RejectStaleHashRejected(t *testing.T) {
	s := setupLeaseUpdate(t)
	k := s.f.App.BillingKeeper

	_, _, err := k.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV2)
	require.NoError(t, err)

	err = k.RejectLeaseUpdate(s.f.Ctx, s.providerAddr.String(), s.leaseUUID, hashV1, "stale")
	require.ErrorIs(t, err, types.ErrLeaseUpdateMismatch)
	require.Equal(t, hashV2, s.lease(t).PendingMetaHash)
}

func TestLeaseUpdate_NoPendingUpdate(t *testing.T) {
	s := setupLeaseUpdate(t)
	k := s.f.App.BillingKeeper

	_, err := k.AcknowledgeLeaseUpdate(s.f.Ctx, s.providerAddr.String(), s.leaseUUID, hashV1)
	require.ErrorIs(t, err, types.ErrNoPendingLeaseUpdate)

	err = k.RejectLeaseUpdate(s.f.Ctx, s.providerAddr.String(), s.leaseUUID, hashV1, "")
	require.ErrorIs(t, err, types.ErrNoPendingLeaseUpdate)

	_, err = k.CancelLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID)
	require.ErrorIs(t, err, types.ErrNoPendingLeaseUpdate)
}

func TestLeaseUpdate_RequestRejectsInvalidHash(t *testing.T) {
	s := setupLeaseUpdate(t)
	k := s.f.App.BillingKeeper

	_, _, err := k.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, nil)
	require.ErrorIs(t, err, types.ErrInvalidMetaHash)

	_, _, err = k.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, make([]byte, types.MaxMetaHashLength+1))
	require.ErrorIs(t, err, types.ErrInvalidMetaHash)
}

// TestLeaseUpdate_IdempotentReRequest verifies re-requesting the identical hash
// is a true no-op. It must not refresh pending_meta_hash_at, or a tenant could
// reset the provider's staleness clock for free.
func TestLeaseUpdate_IdempotentReRequest(t *testing.T) {
	s := setupLeaseUpdate(t)
	k := s.f.App.BillingKeeper

	_, _, err := k.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
	require.NoError(t, err)
	firstAt := *s.lease(t).PendingMetaHashAt
	before := countEvents(s.f.Ctx, types.EventTypeLeaseUpdateRequested)

	// Advance the block clock so a refresh would be observable.
	s.f.Ctx = s.f.Ctx.WithBlockTime(s.f.Ctx.BlockTime().Add(10 * time.Minute))

	role, requestedAt, err := k.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
	require.NoError(t, err)
	require.Equal(t, types.AttributeValueRoleTenant, role)

	require.Equal(t, before, countEvents(s.f.Ctx, types.EventTypeLeaseUpdateRequested), "no-op must not emit")
	require.Equal(t, firstAt, *s.lease(t).PendingMetaHashAt, "no-op must not reset the staleness clock")
	require.Equal(t, firstAt, requestedAt,
		"the reported requested_at must be the stored timestamp, not this block's time")
}

// TestLeaseUpdate_MsgServerReportsStoredRequestedAt is the msg-layer half of the
// above: MsgUpdateLeaseResponse.requested_at documents itself as the value
// recorded in pending_meta_hash_at, so a no-op must not report the current
// block time instead.
func TestLeaseUpdate_MsgServerReportsStoredRequestedAt(t *testing.T) {
	s := setupLeaseUpdate(t)

	first, err := s.msgServer.UpdateLease(s.f.Ctx, &types.MsgUpdateLease{
		Sender: s.tenant.String(), LeaseUuid: s.leaseUUID, MetaHash: hashV1,
	})
	require.NoError(t, err)

	s.f.Ctx = s.f.Ctx.WithBlockTime(s.f.Ctx.BlockTime().Add(10 * time.Minute))

	again, err := s.msgServer.UpdateLease(s.f.Ctx, &types.MsgUpdateLease{
		Sender: s.tenant.String(), LeaseUuid: s.leaseUUID, MetaHash: hashV1,
	})
	require.NoError(t, err)
	require.Equal(t, first.RequestedAt, again.RequestedAt)
	require.Equal(t, *s.lease(t).PendingMetaHashAt, again.RequestedAt)
}

// --- lifecycle interaction ---

func TestLeaseUpdate_CloseLeaseClearsPending(t *testing.T) {
	s := setupLeaseUpdate(t)

	_, _, err := s.f.App.BillingKeeper.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
	require.NoError(t, err)
	require.True(t, s.hasIndexEntry(t))

	_, err = s.msgServer.CloseLease(s.f.Ctx, &types.MsgCloseLease{
		Sender:     s.tenant.String(),
		LeaseUuids: []string{s.leaseUUID},
	})
	require.NoError(t, err)

	lease := s.lease(t)
	require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
	require.Empty(t, lease.PendingMetaHash)
	require.Nil(t, lease.PendingMetaHashAt)
	require.False(t, s.hasIndexEntry(t))
}

// TestLeaseUpdate_AutoCloseClearsPending covers the second ACTIVE → CLOSED
// transition: credit exhaustion. Both withdraw paths route their auto-close
// through Keeper.AutoCloseLease, so clearing here covers them too.
func TestLeaseUpdate_AutoCloseClearsPending(t *testing.T) {
	s := setupLeaseUpdate(t)
	k := s.f.App.BillingKeeper

	_, _, err := k.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
	require.NoError(t, err)
	require.True(t, s.hasIndexEntry(t))

	// Run the clock far enough forward to exhaust the tenant's credit.
	s.f.Ctx = s.f.Ctx.WithBlockTime(s.f.Ctx.BlockTime().Add(200_000_000 * time.Second))

	lease := s.lease(t)
	shouldClose, closeTime, err := k.ShouldAutoCloseLease(s.f.Ctx, &lease)
	require.NoError(t, err)
	require.True(t, shouldClose)

	params, err := k.GetParams(s.f.Ctx)
	require.NoError(t, err)
	_, err = k.AutoCloseLease(s.f.Ctx, &lease, closeTime, params.MinLeaseDuration)
	require.NoError(t, err)

	closed := s.lease(t)
	require.Equal(t, types.LEASE_STATE_CLOSED, closed.State)
	require.Equal(t, types.ClosureReasonCreditExhausted, closed.ClosureReason)
	require.Empty(t, closed.PendingMetaHash)
	require.Nil(t, closed.PendingMetaHashAt)
	require.Equal(t, hashV0, closed.MetaHash, "the last acknowledged manifest survives for audit")
	require.False(t, s.hasIndexEntry(t))
}

// TestLeaseUpdate_WithdrawPreservesPending guards the opposite direction: a
// withdrawal leaves the lease ACTIVE, so the pending request must survive it.
func TestLeaseUpdate_WithdrawPreservesPending(t *testing.T) {
	s := setupLeaseUpdate(t)

	_, _, err := s.f.App.BillingKeeper.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
	require.NoError(t, err)

	s.f.Ctx = s.f.Ctx.WithBlockTime(s.f.Ctx.BlockTime().Add(time.Hour))
	_, err = s.msgServer.Withdraw(s.f.Ctx, &types.MsgWithdraw{
		Sender:     s.providerAddr.String(),
		LeaseUuids: []string{s.leaseUUID},
	})
	require.NoError(t, err)

	lease := s.lease(t)
	require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)
	require.Equal(t, hashV1, lease.PendingMetaHash)
	require.True(t, s.hasIndexEntry(t))
}

// TestLeaseUpdate_CustomDomainWritePreservesPending covers the other in-place
// lease mutator: SetLease reconciles both indexes, and neither may clobber the
// other's state.
func TestLeaseUpdate_CustomDomainWritePreservesPending(t *testing.T) {
	s := setupLeaseUpdate(t)
	k := s.f.App.BillingKeeper

	_, _, err := k.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
	require.NoError(t, err)

	_, err = k.SetItemCustomDomain(s.f.Ctx, s.tenant.String(), s.leaseUUID, "", "app.example.com")
	require.NoError(t, err)

	lease := s.lease(t)
	require.Equal(t, hashV1, lease.PendingMetaHash)
	require.True(t, s.hasIndexEntry(t))
}

// TestLeaseUpdate_SetLeaseNormalisesPendingOnTerminalState covers the structural
// backstop in SetLease. A pending update on a non-ACTIVE lease is unreachable
// through the messages — but a hand-crafted genesis could carry one, and since
// all four verbs require ACTIVE it could never be cleared and would haunt every
// export. SetLease strips it instead, so the fields are derived from state.
func TestLeaseUpdate_SetLeaseNormalisesPendingOnTerminalState(t *testing.T) {
	s := setupLeaseUpdate(t)
	k := s.f.App.BillingKeeper

	at := s.f.Ctx.BlockTime()
	lease := s.lease(t)
	lease.State = types.LEASE_STATE_CLOSED
	lease.ClosedAt = &at
	lease.PendingMetaHash = hashV1
	lease.PendingMetaHashAt = &at

	require.NoError(t, k.SetLease(s.f.Ctx, lease))

	stored := s.lease(t)
	require.Empty(t, stored.PendingMetaHash, "SetLease must strip a pending update from a terminal lease")
	require.Nil(t, stored.PendingMetaHashAt)
	require.False(t, s.hasIndexEntry(t))
}

// TestLeaseUpdate_ReRequestRepairsMissingTimestamp guards the idempotent path
// against corrupt state: a pending hash with no timestamp cannot be produced by
// any handler, but must not panic on a nil deref if it somehow exists. The
// request repairs the record instead.
func TestLeaseUpdate_ReRequestRepairsMissingTimestamp(t *testing.T) {
	s := setupLeaseUpdate(t)
	k := s.f.App.BillingKeeper

	// Write the corrupt shape directly; the lease stays ACTIVE so SetLease's
	// normalisation does not strip it.
	lease := s.lease(t)
	lease.PendingMetaHash = hashV1
	lease.PendingMetaHashAt = nil
	require.NoError(t, k.SetLease(s.f.Ctx, lease))
	require.Nil(t, s.lease(t).PendingMetaHashAt)

	require.NotPanics(t, func() {
		_, requestedAt, err := k.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
		require.NoError(t, err)
		require.False(t, requestedAt.IsZero())
	})

	require.NotNil(t, s.lease(t).PendingMetaHashAt, "the re-request must repair the missing timestamp")
}

// --- index scoping ---

func TestLeaseUpdate_IndexIsScopedToProvider(t *testing.T) {
	s := setupLeaseUpdate(t)
	k := s.f.App.BillingKeeper

	_, _, err := k.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
	require.NoError(t, err)

	// A second provider must not see the first provider's pending update.
	// Only the UUID matters here, so the addresses are irrelevant.
	otherProvider := s.f.createTestProvider(t, s.stranger.String(), s.stranger.String())
	require.NotEqual(t, s.provider.Uuid, otherProvider.Uuid)
	has, err := k.PendingUpdateIndex.Has(s.f.Ctx, collections.Join(otherProvider.Uuid, s.leaseUUID))
	require.NoError(t, err)
	require.False(t, has)
}

// TestLeaseUpdate_IndexRebuiltFromGenesis proves the index needs no genesis
// representation: it is derived state that SetLease reconstructs on import.
func TestLeaseUpdate_IndexRebuiltFromGenesis(t *testing.T) {
	s := setupLeaseUpdate(t)

	_, _, err := s.f.App.BillingKeeper.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
	require.NoError(t, err)

	exported := s.f.App.BillingKeeper.ExportGenesis(s.f.Ctx)
	require.NoError(t, exported.Validate())

	// Drop the index, then re-import and confirm it comes back.
	require.NoError(t, s.f.App.BillingKeeper.PendingUpdateIndex.Remove(
		s.f.Ctx, collections.Join(s.provider.Uuid, s.leaseUUID),
	))
	require.False(t, s.hasIndexEntry(t))

	require.NoError(t, s.f.App.BillingKeeper.InitGenesis(s.f.Ctx, exported))
	require.True(t, s.hasIndexEntry(t))

	lease := s.lease(t)
	require.Equal(t, hashV1, lease.PendingMetaHash)
	require.NotNil(t, lease.PendingMetaHashAt)
}

// --- query ---

func TestLeaseUpdate_PendingLeaseUpdatesQuery(t *testing.T) {
	s := setupLeaseUpdate(t)
	k := s.f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	res, err := querier.PendingLeaseUpdates(s.f.Ctx, &types.QueryPendingLeaseUpdatesRequest{
		ProviderUuid: s.provider.Uuid,
	})
	require.NoError(t, err)
	require.Empty(t, res.Leases)

	_, _, err = k.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
	require.NoError(t, err)

	res, err = querier.PendingLeaseUpdates(s.f.Ctx, &types.QueryPendingLeaseUpdatesRequest{
		ProviderUuid: s.provider.Uuid,
	})
	require.NoError(t, err)
	require.Len(t, res.Leases, 1)
	require.Equal(t, s.leaseUUID, res.Leases[0].Uuid)
	require.Equal(t, hashV1, res.Leases[0].PendingMetaHash)

	// Acknowledging removes it from the provider's work list.
	_, err = k.AcknowledgeLeaseUpdate(s.f.Ctx, s.providerAddr.String(), s.leaseUUID, hashV1)
	require.NoError(t, err)

	res, err = querier.PendingLeaseUpdates(s.f.Ctx, &types.QueryPendingLeaseUpdatesRequest{
		ProviderUuid: s.provider.Uuid,
	})
	require.NoError(t, err)
	require.Empty(t, res.Leases)
}

// TestLeaseUpdate_PendingLeaseUpdatesPagination walks the query one page at a
// time. The KeySet iterator is a new adapter feeding the shared
// PaginateStringIndex cursor contract, so the resume path needs proving:
// following next_key must visit every lease exactly once.
func TestLeaseUpdate_PendingLeaseUpdatesPagination(t *testing.T) {
	s := setupLeaseUpdate(t)
	k := s.f.App.BillingKeeper
	querier := keeper.NewQuerier(k)

	// Three ACTIVE leases for the same provider, each with a pending update.
	want := map[string]bool{}
	for _, h := range [][]byte{hashV0, hashV1, hashV2} {
		uuid := s.createLeaseWithMetaHash(t, hashV0)
		_, _, err := k.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), uuid, h)
		require.NoError(t, err)
		want[uuid] = true
	}
	// The fixture's own lease has no pending update and must not appear.
	require.Len(t, want, 3)

	got := map[string]bool{}
	var nextKey []byte
	for page := 0; page < 10; page++ {
		res, err := querier.PendingLeaseUpdates(s.f.Ctx, &types.QueryPendingLeaseUpdatesRequest{
			ProviderUuid: s.provider.Uuid,
			Pagination:   &query.PageRequest{Limit: 1, Key: nextKey},
		})
		require.NoError(t, err)
		require.Len(t, res.Leases, 1, "page %d", page)

		uuid := res.Leases[0].Uuid
		require.False(t, got[uuid], "lease %s returned twice", uuid)
		got[uuid] = true

		nextKey = res.Pagination.NextKey
		if len(nextKey) == 0 {
			break
		}
	}

	require.Equal(t, want, got, "paging must visit every pending lease exactly once")

	// Reverse order exercises the other branch of the shared range builder
	// (EndInclusive + Descending), which the forward walk above never touches.
	rev, err := querier.PendingLeaseUpdates(s.f.Ctx, &types.QueryPendingLeaseUpdatesRequest{
		ProviderUuid: s.provider.Uuid,
		Pagination:   &query.PageRequest{Reverse: true},
	})
	require.NoError(t, err)
	require.Len(t, rev.Leases, 3)

	reversed := make([]string, len(rev.Leases))
	for i, l := range rev.Leases {
		reversed[i] = l.Uuid
	}
	forward := slices.Clone(reversed)
	slices.Reverse(forward)
	require.True(t, slices.IsSorted(forward), "reverse paging must return descending lease UUIDs")
}

func TestLeaseUpdate_PendingLeaseUpdatesQueryValidation(t *testing.T) {
	s := setupLeaseUpdate(t)
	querier := keeper.NewQuerier(s.f.App.BillingKeeper)

	_, err := querier.PendingLeaseUpdates(s.f.Ctx, nil)
	require.Error(t, err)

	_, err = querier.PendingLeaseUpdates(s.f.Ctx, &types.QueryPendingLeaseUpdatesRequest{ProviderUuid: ""})
	require.Error(t, err)
}

// --- events ---

func TestLeaseUpdate_RequestedEventAttributes(t *testing.T) {
	s := setupLeaseUpdate(t)

	_, _, err := s.f.App.BillingKeeper.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
	require.NoError(t, err)

	ev := findEvent(t, s.f.Ctx, types.EventTypeLeaseUpdateRequested)
	require.Equal(t, s.leaseUUID, attrValue(t, ev, types.AttributeKeyLeaseUUID))
	require.Equal(t, s.tenant.String(), attrValue(t, ev, types.AttributeKeyTenant))
	require.Equal(t, s.provider.Uuid, attrValue(t, ev, types.AttributeKeyProviderUUID))
	require.Equal(t, hex.EncodeToString(hashV1), attrValue(t, ev, types.AttributeKeyPendingMetaHash))
	require.Equal(t, "0", attrValue(t, ev, types.AttributeKeyMetaHashRevision))
	require.Equal(t, types.AttributeValueRoleTenant, attrValue(t, ev, types.AttributeKeyRequestedBy))
}

func TestLeaseUpdate_AcknowledgedEventAttributes(t *testing.T) {
	s := setupLeaseUpdate(t)
	k := s.f.App.BillingKeeper

	_, _, err := k.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
	require.NoError(t, err)
	_, err = k.AcknowledgeLeaseUpdate(s.f.Ctx, s.providerAddr.String(), s.leaseUUID, hashV1)
	require.NoError(t, err)

	ev := findEvent(t, s.f.Ctx, types.EventTypeLeaseUpdateAcknowledged)
	require.Equal(t, s.leaseUUID, attrValue(t, ev, types.AttributeKeyLeaseUUID))
	require.Equal(t, hex.EncodeToString(hashV1), attrValue(t, ev, types.AttributeKeyMetaHash))
	require.Equal(t, hex.EncodeToString(hashV0), attrValue(t, ev, types.AttributeKeyPreviousMetaHash))
	require.Equal(t, "1", attrValue(t, ev, types.AttributeKeyMetaHashRevision))
	require.Equal(t, s.providerAddr.String(), attrValue(t, ev, types.AttributeKeyAcknowledgedBy))
}

func TestLeaseUpdate_RejectedEventAttributes(t *testing.T) {
	s := setupLeaseUpdate(t)
	k := s.f.App.BillingKeeper

	_, _, err := k.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
	require.NoError(t, err)
	require.NoError(t, k.RejectLeaseUpdate(s.f.Ctx, s.providerAddr.String(), s.leaseUUID, hashV1, "unsupported image"))

	ev := findEvent(t, s.f.Ctx, types.EventTypeLeaseUpdateRejected)
	require.Equal(t, s.leaseUUID, attrValue(t, ev, types.AttributeKeyLeaseUUID))
	require.Equal(t, hex.EncodeToString(hashV1), attrValue(t, ev, types.AttributeKeyPendingMetaHash))
	require.Equal(t, "unsupported image", attrValue(t, ev, types.AttributeKeyReason))
	require.Equal(t, s.providerAddr.String(), attrValue(t, ev, types.AttributeKeyRejectedBy))
}

// TestLeaseUpdate_RejectReasonIsSanitised covers the log-injection guard. The
// reason is provider-supplied free text, so control characters must be stripped
// before they reach the event stream, matching MsgRejectLease and MsgCloseLease.
func TestLeaseUpdate_RejectReasonIsSanitised(t *testing.T) {
	s := setupLeaseUpdate(t)
	k := s.f.App.BillingKeeper

	_, _, err := k.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
	require.NoError(t, err)

	// A reason that tries to forge a second event line in a text log pipeline.
	require.NoError(t, k.RejectLeaseUpdate(s.f.Ctx, s.providerAddr.String(), s.leaseUUID, hashV1,
		"nope\n\nlease_update_acknowledged\nlease_uuid: forged"))

	ev := findEvent(t, s.f.Ctx, types.EventTypeLeaseUpdateRejected)
	got := attrValue(t, ev, types.AttributeKeyReason)
	require.NotContains(t, got, "\n")
	require.Equal(t, "nopelease_update_acknowledgedlease_uuid: forged", got)
}

func TestLeaseUpdate_CancelledEventAttributes(t *testing.T) {
	s := setupLeaseUpdate(t)
	k := s.f.App.BillingKeeper

	_, _, err := k.RequestLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID, hashV1)
	require.NoError(t, err)
	_, err = k.CancelLeaseUpdate(s.f.Ctx, s.tenant.String(), s.leaseUUID)
	require.NoError(t, err)

	ev := findEvent(t, s.f.Ctx, types.EventTypeLeaseUpdateCancelled)
	require.Equal(t, s.leaseUUID, attrValue(t, ev, types.AttributeKeyLeaseUUID))
	require.Equal(t, hex.EncodeToString(hashV1), attrValue(t, ev, types.AttributeKeyPendingMetaHash))
	require.Equal(t, types.AttributeValueRoleTenant, attrValue(t, ev, types.AttributeKeyCancelledBy))
}

// --- msg server plumbing ---

func TestLeaseUpdate_MsgServerRoundTrip(t *testing.T) {
	s := setupLeaseUpdate(t)

	_, err := s.msgServer.UpdateLease(s.f.Ctx, &types.MsgUpdateLease{
		Sender:    s.tenant.String(),
		LeaseUuid: s.leaseUUID,
		MetaHash:  hashV1,
	})
	require.NoError(t, err)
	require.Equal(t, hashV1, s.lease(t).PendingMetaHash)

	ackResp, err := s.msgServer.AcknowledgeLeaseUpdate(s.f.Ctx, &types.MsgAcknowledgeLeaseUpdate{
		Sender:    s.providerAddr.String(),
		LeaseUuid: s.leaseUUID,
		MetaHash:  hashV1,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(1), ackResp.MetaHashRevision)
	require.Equal(t, hashV1, s.lease(t).MetaHash)

	// Reject path.
	_, err = s.msgServer.UpdateLease(s.f.Ctx, &types.MsgUpdateLease{
		Sender: s.tenant.String(), LeaseUuid: s.leaseUUID, MetaHash: hashV2,
	})
	require.NoError(t, err)
	_, err = s.msgServer.RejectLeaseUpdate(s.f.Ctx, &types.MsgRejectLeaseUpdate{
		Sender: s.providerAddr.String(), LeaseUuid: s.leaseUUID, MetaHash: hashV2, Reason: "no",
	})
	require.NoError(t, err)
	require.Equal(t, hashV1, s.lease(t).MetaHash)

	// Cancel path.
	_, err = s.msgServer.UpdateLease(s.f.Ctx, &types.MsgUpdateLease{
		Sender: s.tenant.String(), LeaseUuid: s.leaseUUID, MetaHash: hashV2,
	})
	require.NoError(t, err)
	_, err = s.msgServer.CancelLeaseUpdate(s.f.Ctx, &types.MsgCancelLeaseUpdate{
		Sender: s.tenant.String(), LeaseUuid: s.leaseUUID,
	})
	require.NoError(t, err)
	require.Empty(t, s.lease(t).PendingMetaHash)
}

// TestLeaseUpdate_MsgServerRunsValidateBasic confirms the msg servers reject
// malformed input before touching state.
func TestLeaseUpdate_MsgServerRunsValidateBasic(t *testing.T) {
	s := setupLeaseUpdate(t)

	_, err := s.msgServer.UpdateLease(s.f.Ctx, &types.MsgUpdateLease{
		Sender: s.tenant.String(), LeaseUuid: s.leaseUUID, MetaHash: nil,
	})
	require.ErrorIs(t, err, types.ErrInvalidMetaHash)

	_, err = s.msgServer.CancelLeaseUpdate(s.f.Ctx, &types.MsgCancelLeaseUpdate{
		Sender: s.tenant.String(), LeaseUuid: "not-a-uuid",
	})
	require.ErrorIs(t, err, types.ErrInvalidLease)
}
