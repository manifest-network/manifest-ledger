package keeper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

// customDomainSetup spins up a fixture with one tenant + provider, an active
// lease, and an allowed-list account. Returns everything needed to drive
// SetLeaseCustomDomain end-to-end.
type customDomainSetup struct {
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

func setupCustomDomain(t *testing.T) *customDomainSetup {
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

	// Add the "allowed" address to params.AllowedList so it can act on behalf of tenants.
	params, err := f.App.BillingKeeper.GetParams(f.Ctx)
	require.NoError(t, err)
	params.AllowedList = []string{allowed.String()}
	require.NoError(t, f.App.BillingKeeper.SetParams(f.Ctx, params))

	leaseUUID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	return &customDomainSetup{
		f: f, msgServer: msgServer,
		tenant: tenant, provider: provider, providerAddr: providerAddr,
		allowed: allowed, stranger: stranger,
		sku: sku, leaseUUID: leaseUUID,
	}
}

func TestSetLeaseCustomDomain_HappyPath(t *testing.T) {
	s := setupCustomDomain(t)

	// The keeper defensively normalises whitespace + case, so " App.Example.COM "
	// resolves to "app.example.com". This is intentional — the strict reject
	// for raw uppercase happens earlier at MsgSetLeaseCustomDomain.ValidateBasic.
	role, err := s.f.App.BillingKeeper.SetLeaseCustomDomain(s.f.Ctx, s.tenant.String(), s.leaseUUID, " App.Example.COM ")
	require.NoError(t, err)
	require.Equal(t, "tenant", role)

	lease, err := s.f.App.BillingKeeper.GetLease(s.f.Ctx, s.leaseUUID)
	require.NoError(t, err)
	require.Equal(t, "app.example.com", lease.CustomDomain)

	got, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "app.example.com")
	require.NoError(t, err)
	require.True(t, has)
	require.Equal(t, s.leaseUUID, got.Uuid)
}

func TestSetLeaseCustomDomain_ClearAllowsReclaim(t *testing.T) {
	s := setupCustomDomain(t)

	_, err := s.f.App.BillingKeeper.SetLeaseCustomDomain(s.f.Ctx, s.tenant.String(), s.leaseUUID, "app.example.com")
	require.NoError(t, err)

	// Clear via empty domain
	_, err = s.f.App.BillingKeeper.SetLeaseCustomDomain(s.f.Ctx, s.tenant.String(), s.leaseUUID, "")
	require.NoError(t, err)

	lease, err := s.f.App.BillingKeeper.GetLease(s.f.Ctx, s.leaseUUID)
	require.NoError(t, err)
	require.Empty(t, lease.CustomDomain)

	_, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "app.example.com")
	require.NoError(t, err)
	require.False(t, has)

	// Same lease re-claims same domain — works.
	_, err = s.f.App.BillingKeeper.SetLeaseCustomDomain(s.f.Ctx, s.tenant.String(), s.leaseUUID, "app.example.com")
	require.NoError(t, err)
}

func TestSetLeaseCustomDomain_AuthorisationMatrix(t *testing.T) {
	cases := []struct {
		name     string
		who      func(s *customDomainSetup) sdk.AccAddress
		wantRole string
		wantErr  bool
	}{
		{"tenant", func(s *customDomainSetup) sdk.AccAddress { return s.tenant }, "tenant", false},
		{"authority", func(s *customDomainSetup) sdk.AccAddress { return s.f.Authority }, "authority", false},
		{"allowed", func(s *customDomainSetup) sdk.AccAddress { return s.allowed }, "allowed", false},
		{"stranger", func(s *customDomainSetup) sdk.AccAddress { return s.stranger }, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := setupCustomDomain(t)
			role, err := s.f.App.BillingKeeper.SetLeaseCustomDomain(s.f.Ctx, tc.who(s).String(), s.leaseUUID, "x.example.com")
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, types.ErrUnauthorized)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantRole, role)
		})
	}
}

func TestSetLeaseCustomDomain_ReservedSuffix(t *testing.T) {
	cases := []struct {
		name    string
		domain  string
		wantErr bool
	}{
		{"subdomain_blocked", "app.barney0.manifest0.net", true},
		{"apex_blocked", "barney0.manifest0.net", true},
		{"no_boundary_allowed", "xbarney0.manifest0.net", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := setupCustomDomain(t) // fresh fixture per case to avoid cross-domain conflicts
			params, err := s.f.App.BillingKeeper.GetParams(s.f.Ctx)
			require.NoError(t, err)
			params.ReservedDomainSuffixes = []string{".barney0.manifest0.net"}
			require.NoError(t, s.f.App.BillingKeeper.SetParams(s.f.Ctx, params))

			_, err = s.f.App.BillingKeeper.SetLeaseCustomDomain(s.f.Ctx, s.tenant.String(), s.leaseUUID, tc.domain)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, types.ErrInvalidCustomDomain)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSetLeaseCustomDomain_AlreadyClaimed(t *testing.T) {
	s := setupCustomDomain(t)

	_, err := s.f.App.BillingKeeper.SetLeaseCustomDomain(s.f.Ctx, s.tenant.String(), s.leaseUUID, "app.example.com")
	require.NoError(t, err)

	// Create a second lease for the same tenant and try to claim the same domain.
	secondUUID := s.f.createAndAcknowledgeLease(t, s.msgServer, s.tenant, s.providerAddr, []types.LeaseItemInput{
		{SkuUuid: s.sku.Uuid, Quantity: 1},
	})
	_, err = s.f.App.BillingKeeper.SetLeaseCustomDomain(s.f.Ctx, s.tenant.String(), secondUUID, "app.example.com")
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrCustomDomainAlreadyClaimed)
}

func TestSetLeaseCustomDomain_ReplaceDomainOnSameLease(t *testing.T) {
	s := setupCustomDomain(t)

	_, err := s.f.App.BillingKeeper.SetLeaseCustomDomain(s.f.Ctx, s.tenant.String(), s.leaseUUID, "old.example.com")
	require.NoError(t, err)

	_, err = s.f.App.BillingKeeper.SetLeaseCustomDomain(s.f.Ctx, s.tenant.String(), s.leaseUUID, "new.example.com")
	require.NoError(t, err)

	_, hasOld, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "old.example.com")
	require.NoError(t, err)
	require.False(t, hasOld, "old domain should be released")

	got, hasNew, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "new.example.com")
	require.NoError(t, err)
	require.True(t, hasNew)
	require.Equal(t, s.leaseUUID, got.Uuid)
}

func TestSetLeaseCustomDomain_RejectedOnTerminalState(t *testing.T) {
	s := setupCustomDomain(t)
	// Close the lease first.
	_, err := s.msgServer.CloseLease(s.f.Ctx, &types.MsgCloseLease{
		Sender:     s.tenant.String(),
		LeaseUuids: []string{s.leaseUUID},
	})
	require.NoError(t, err)

	_, err = s.f.App.BillingKeeper.SetLeaseCustomDomain(s.f.Ctx, s.tenant.String(), s.leaseUUID, "x.example.com")
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrLeaseNotEditable)

	// Even authority cannot bypass the state check.
	_, err = s.f.App.BillingKeeper.SetLeaseCustomDomain(s.f.Ctx, s.f.Authority.String(), s.leaseUUID, "x.example.com")
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrLeaseNotEditable)
}

func TestSetLeaseCustomDomain_LifecycleCleanup_Close(t *testing.T) {
	s := setupCustomDomain(t)
	_, err := s.f.App.BillingKeeper.SetLeaseCustomDomain(s.f.Ctx, s.tenant.String(), s.leaseUUID, "claim.example.com")
	require.NoError(t, err)

	_, err = s.msgServer.CloseLease(s.f.Ctx, &types.MsgCloseLease{
		Sender:     s.tenant.String(),
		LeaseUuids: []string{s.leaseUUID},
	})
	require.NoError(t, err)

	// Index entry gone, but Lease.CustomDomain is preserved for audit.
	_, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "claim.example.com")
	require.NoError(t, err)
	require.False(t, has)
	closed, err := s.f.App.BillingKeeper.GetLease(s.f.Ctx, s.leaseUUID)
	require.NoError(t, err)
	require.Equal(t, "claim.example.com", closed.CustomDomain)
	require.Equal(t, types.LEASE_STATE_CLOSED, closed.State)

	// A new lease can claim the same domain.
	newUUID := s.f.createAndAcknowledgeLease(t, s.msgServer, s.tenant, s.providerAddr, []types.LeaseItemInput{
		{SkuUuid: s.sku.Uuid, Quantity: 1},
	})
	_, err = s.f.App.BillingKeeper.SetLeaseCustomDomain(s.f.Ctx, s.tenant.String(), newUUID, "claim.example.com")
	require.NoError(t, err)
	got, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "claim.example.com")
	require.NoError(t, err)
	require.True(t, has)
	require.Equal(t, newUUID, got.Uuid)
}

func TestSetLeaseCustomDomain_LifecycleCleanup_Cancel(t *testing.T) {
	s := setupCustomDomain(t)
	// Create a fresh PENDING lease that we'll cancel (cancel only works on PENDING).
	pendingResp, err := s.msgServer.CreateLease(s.f.Ctx, &types.MsgCreateLease{
		Tenant: s.tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: s.sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	_, err = s.f.App.BillingKeeper.SetLeaseCustomDomain(s.f.Ctx, s.tenant.String(), pendingResp.LeaseUuid, "cancel.example.com")
	require.NoError(t, err)

	_, err = s.msgServer.CancelLease(s.f.Ctx, &types.MsgCancelLease{
		Tenant:     s.tenant.String(),
		LeaseUuids: []string{pendingResp.LeaseUuid},
	})
	require.NoError(t, err)

	_, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "cancel.example.com")
	require.NoError(t, err)
	require.False(t, has)
}

func TestSetLeaseCustomDomain_LifecycleCleanup_Reject(t *testing.T) {
	s := setupCustomDomain(t)
	pendingResp, err := s.msgServer.CreateLease(s.f.Ctx, &types.MsgCreateLease{
		Tenant: s.tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: s.sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	_, err = s.f.App.BillingKeeper.SetLeaseCustomDomain(s.f.Ctx, s.tenant.String(), pendingResp.LeaseUuid, "reject.example.com")
	require.NoError(t, err)

	_, err = s.msgServer.RejectLease(s.f.Ctx, &types.MsgRejectLease{
		Sender:     s.providerAddr.String(),
		LeaseUuids: []string{pendingResp.LeaseUuid},
	})
	require.NoError(t, err)

	_, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "reject.example.com")
	require.NoError(t, err)
	require.False(t, has)
}

func TestInitGenesis_RebuildsCustomDomainIndex(t *testing.T) {
	f := initFixture(t)
	// Build a genesis with one PENDING lease holding a custom_domain.
	provider := f.createTestProvider(t, f.TestAccs[1].String(), f.TestAccs[2].String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)
	tenant := f.TestAccs[0]

	// Fund credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))

	now := f.Ctx.BlockTime()
	gs := &types.GenesisState{
		Params: types.DefaultParams(),
		Leases: []types.Lease{
			{
				Uuid:                       "01912345-6789-7abc-8def-aaaaaaaaaaa1",
				Tenant:                     tenant.String(),
				ProviderUuid:               provider.Uuid,
				Items:                      []types.LeaseItem{{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1))}},
				State:                      types.LEASE_STATE_PENDING,
				CreatedAt:                  now,
				LastSettledAt:              now,
				MinLeaseDurationAtCreation: 3600,
				CustomDomain:               "genesis.example.com",
			},
		},
		CreditAccounts: []types.CreditAccount{
			{Tenant: tenant.String(), CreditAddress: creditAddr.String(), PendingLeaseCount: 1},
		},
	}

	require.NoError(t, f.App.BillingKeeper.InitGenesis(f.Ctx, gs))

	got, has, err := f.App.BillingKeeper.GetLeaseByCustomDomain(f.Ctx, "genesis.example.com")
	require.NoError(t, err)
	require.True(t, has)
	require.Equal(t, "01912345-6789-7abc-8def-aaaaaaaaaaa1", got.Uuid)
}

func TestInitGenesis_DuplicateCustomDomainFails(t *testing.T) {
	f := initFixture(t)
	provider := f.createTestProvider(t, f.TestAccs[1].String(), f.TestAccs[2].String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)
	tenant := f.TestAccs[0]

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))

	now := f.Ctx.BlockTime()
	mkLease := func(uuid string) types.Lease {
		return types.Lease{
			Uuid:                       uuid,
			Tenant:                     tenant.String(),
			ProviderUuid:               provider.Uuid,
			Items:                      []types.LeaseItem{{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1))}},
			State:                      types.LEASE_STATE_PENDING,
			CreatedAt:                  now,
			LastSettledAt:              now,
			MinLeaseDurationAtCreation: 3600,
			CustomDomain:               "dup.example.com",
		}
	}
	gs := &types.GenesisState{
		Params: types.DefaultParams(),
		Leases: []types.Lease{
			mkLease("01912345-6789-7abc-8def-bbbbbbbbbbb1"),
			mkLease("01912345-6789-7abc-8def-bbbbbbbbbbb2"),
		},
		CreditAccounts: []types.CreditAccount{
			{Tenant: tenant.String(), CreditAddress: creditAddr.String(), PendingLeaseCount: 2},
		},
	}

	err = f.App.BillingKeeper.InitGenesis(f.Ctx, gs)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrCustomDomainAlreadyClaimed)
}

func TestMigrate1to2(t *testing.T) {
	f := initFixture(t)

	// Empty list → defaults seeded.
	require.NoError(t, f.App.BillingKeeper.SetParams(f.Ctx, types.DefaultParams()))
	require.NoError(t, keeper.NewMigrator(f.App.BillingKeeper).Migrate1to2(f.Ctx))
	params, err := f.App.BillingKeeper.GetParams(f.Ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, keeper.DefaultReservedDomainSuffixesV2, params.ReservedDomainSuffixes)

	// Non-empty list → preserved.
	custom := []string{".operator.zone"}
	params.ReservedDomainSuffixes = custom
	require.NoError(t, f.App.BillingKeeper.SetParams(f.Ctx, params))
	require.NoError(t, keeper.NewMigrator(f.App.BillingKeeper).Migrate1to2(f.Ctx))
	params, err = f.App.BillingKeeper.GetParams(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, custom, params.ReservedDomainSuffixes)
}

// TestSetLeaseCustomDomain_LifecycleCleanup_AutoCloseLease verifies that
// AutoCloseLease (the credit-exhaustion path) frees the custom_domain index
// entry, allowing the same domain to be reclaimed by a fresh lease.
func TestSetLeaseCustomDomain_LifecycleCleanup_AutoCloseLease(t *testing.T) {
	s := setupCustomDomain(t)

	// Claim a domain on the active lease, then drain credit via time advance.
	_, err := s.f.App.BillingKeeper.SetLeaseCustomDomain(s.f.Ctx, s.tenant.String(), s.leaseUUID, "auto.example.com")
	require.NoError(t, err)

	// The fixture funds 100M umfx; the SKU costs 1/sec. Advance well past credit.
	s.f.Ctx = s.f.Ctx.WithBlockTime(s.f.Ctx.BlockTime().Add(200_000_000 * time.Second))

	lease, err := s.f.App.BillingKeeper.GetLease(s.f.Ctx, s.leaseUUID)
	require.NoError(t, err)
	shouldClose, closeTime, err := s.f.App.BillingKeeper.ShouldAutoCloseLease(s.f.Ctx, &lease)
	require.NoError(t, err)
	require.True(t, shouldClose, "lease should auto-close after credit exhaustion")

	params, err := s.f.App.BillingKeeper.GetParams(s.f.Ctx)
	require.NoError(t, err)
	_, err = s.f.App.BillingKeeper.AutoCloseLease(s.f.Ctx, &lease, closeTime, params.MinLeaseDuration)
	require.NoError(t, err)

	// Index entry is gone; the lease itself still has CustomDomain for audit.
	_, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "auto.example.com")
	require.NoError(t, err)
	require.False(t, has, "index entry must be removed on auto-close")
	closed, err := s.f.App.BillingKeeper.GetLease(s.f.Ctx, s.leaseUUID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, closed.State)
	require.Equal(t, "auto.example.com", closed.CustomDomain, "field preserved for audit")

	// Top up credit (the auto-close drained the account) and reclaim the same domain.
	creditAddr, err := types.DeriveCreditAddressFromBech32(s.tenant.String())
	require.NoError(t, err)
	s.f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100_000_000))))

	newUUID := s.f.createAndAcknowledgeLease(t, s.msgServer, s.tenant, s.providerAddr, []types.LeaseItemInput{
		{SkuUuid: s.sku.Uuid, Quantity: 1},
	})
	_, err = s.f.App.BillingKeeper.SetLeaseCustomDomain(s.f.Ctx, s.tenant.String(), newUUID, "auto.example.com")
	require.NoError(t, err)
}

// TestSetLeaseCustomDomain_AcknowledgePreservesIndex verifies that the
// PENDING → ACTIVE state transition (driven by AcknowledgeLease) preserves an
// already-claimed custom_domain index entry. This locks down the no-rename,
// no-state-exit branch of reconcileCustomDomainIndex: prev.CustomDomain ==
// lease.CustomDomain and both states are editable, so the entry must remain
// untouched and continue to point at the same lease.
func TestSetLeaseCustomDomain_AcknowledgePreservesIndex(t *testing.T) {
	s := setupCustomDomain(t)

	// Create a fresh PENDING lease (the fixture's lease is already ACTIVE).
	pendingResp, err := s.msgServer.CreateLease(s.f.Ctx, &types.MsgCreateLease{
		Tenant: s.tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: s.sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)

	// Claim a domain while PENDING.
	_, err = s.f.App.BillingKeeper.SetLeaseCustomDomain(s.f.Ctx, s.tenant.String(), pendingResp.LeaseUuid, "ack.example.com")
	require.NoError(t, err)

	got, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "ack.example.com")
	require.NoError(t, err)
	require.True(t, has, "index entry must exist while PENDING")
	require.Equal(t, pendingResp.LeaseUuid, got.Uuid)

	// Provider acknowledges → state flips PENDING → ACTIVE through SetLease.
	_, err = s.msgServer.AcknowledgeLease(s.f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     s.providerAddr.String(),
		LeaseUuids: []string{pendingResp.LeaseUuid},
	})
	require.NoError(t, err)

	// Index entry must survive the transition and still point at the same lease.
	got, has, err = s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "ack.example.com")
	require.NoError(t, err)
	require.True(t, has, "index entry must persist across PENDING → ACTIVE acknowledge")
	require.Equal(t, pendingResp.LeaseUuid, got.Uuid)

	// Lease record reflects the new ACTIVE state with the domain intact.
	lease, err := s.f.App.BillingKeeper.GetLease(s.f.Ctx, pendingResp.LeaseUuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)
	require.Equal(t, "ack.example.com", lease.CustomDomain)
}

// TestSetLeaseCustomDomain_LifecycleCleanup_EndBlockerExpiry verifies that
// ExpirePendingLease (called from EndBlocker after pending_timeout) frees the
// custom_domain index entry.
func TestSetLeaseCustomDomain_LifecycleCleanup_EndBlockerExpiry(t *testing.T) {
	s := setupCustomDomain(t)

	// Shorten the pending timeout so we can fast-forward past it.
	params, err := s.f.App.BillingKeeper.GetParams(s.f.Ctx)
	require.NoError(t, err)
	params.PendingTimeout = 60
	require.NoError(t, s.f.App.BillingKeeper.SetParams(s.f.Ctx, params))

	pendingResp, err := s.msgServer.CreateLease(s.f.Ctx, &types.MsgCreateLease{
		Tenant: s.tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: s.sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	_, err = s.f.App.BillingKeeper.SetLeaseCustomDomain(s.f.Ctx, s.tenant.String(), pendingResp.LeaseUuid, "expire.example.com")
	require.NoError(t, err)

	// Advance past the pending timeout and run EndBlocker.
	s.f.Ctx = s.f.Ctx.WithBlockTime(s.f.Ctx.BlockTime().Add(61 * time.Second))
	require.NoError(t, s.f.App.BillingKeeper.EndBlocker(s.f.Ctx))

	// The lease should have transitioned to EXPIRED and the index entry removed.
	expired, err := s.f.App.BillingKeeper.GetLease(s.f.Ctx, pendingResp.LeaseUuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_EXPIRED, expired.State)

	_, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "expire.example.com")
	require.NoError(t, err)
	require.False(t, has, "index entry must be removed on EndBlocker expiry")
}
