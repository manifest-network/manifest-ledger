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
// lease, and an allowed-list account. The default lease is 1-item legacy mode
// (item.service_name = ""), addressable via msg.service_name = "".
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

// createMultiItemLease creates a multi-item ACTIVE lease in service-name mode
// with two items keyed by `web` and `db`. Returns the lease UUID.
func (s *customDomainSetup) createMultiItemLease(t *testing.T) string {
	t.Helper()
	return s.f.createAndAcknowledgeLease(t, s.msgServer, s.tenant, s.providerAddr, []types.LeaseItemInput{
		{SkuUuid: s.sku.Uuid, Quantity: 1, ServiceName: "web"},
		{SkuUuid: s.sku.Uuid, Quantity: 1, ServiceName: "db"},
	})
}

// createMultiItemLegacyLease creates a multi-item ACTIVE lease in legacy mode
// (no service_names). Two items with distinct SKUs to satisfy the legacy
// uniqueness rule. Returns the lease UUID and a second SKU we created.
func (s *customDomainSetup) createMultiItemLegacyLease(t *testing.T) (string, skutypes.SKU) {
	t.Helper()
	sku2 := s.f.createTestSKU(t, s.provider.Uuid, 3600)
	uuid := s.f.createAndAcknowledgeLease(t, s.msgServer, s.tenant, s.providerAddr, []types.LeaseItemInput{
		{SkuUuid: s.sku.Uuid, Quantity: 1},
		{SkuUuid: sku2.Uuid, Quantity: 1},
	})
	return uuid, sku2
}

// --- 1-item legacy lease (the default fixture) ---

func TestSetLeaseItemCustomDomain_LegacyOneItem_HappyPath(t *testing.T) {
	s := setupCustomDomain(t)

	role, err := s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), s.leaseUUID, "", "app.example.com")
	require.NoError(t, err)
	require.Equal(t, types.AttributeValueRoleTenant, role)

	lease, err := s.f.App.BillingKeeper.GetLease(s.f.Ctx, s.leaseUUID)
	require.NoError(t, err)
	require.Equal(t, "app.example.com", lease.Items[0].CustomDomain)

	got, serviceName, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "app.example.com")
	require.NoError(t, err)
	require.True(t, has)
	require.Equal(t, s.leaseUUID, got.Uuid)
	require.Equal(t, "", serviceName, "1-item legacy returns empty service_name in reverse lookup")
}

func TestSetLeaseItemCustomDomain_LegacyOneItem_NormalisesInput(t *testing.T) {
	s := setupCustomDomain(t)
	// Keeper-level normalises whitespace + case (ValidateBasic at msg layer
	// would reject uppercase outright; the keeper's normalisation is defence
	// for direct callers like genesis).
	role, err := s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), s.leaseUUID, "", " App.Example.COM ")
	require.NoError(t, err)
	require.Equal(t, types.AttributeValueRoleTenant, role)

	got, _, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "app.example.com")
	require.NoError(t, err)
	require.True(t, has)
	require.Equal(t, s.leaseUUID, got.Uuid)
}

func TestSetLeaseItemCustomDomain_LegacyOneItem_NonEmptyServiceName_NotFound(t *testing.T) {
	s := setupCustomDomain(t)
	// 1-item legacy lease has item.service_name = ""; passing "web" doesn't match.
	_, err := s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), s.leaseUUID, "web", "x.example.com")
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrLeaseItemNotFound)
}

func TestSetLeaseItemCustomDomain_ClearAllowsReclaim(t *testing.T) {
	s := setupCustomDomain(t)

	_, err := s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), s.leaseUUID, "", "app.example.com")
	require.NoError(t, err)

	_, err = s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), s.leaseUUID, "", "")
	require.NoError(t, err)

	lease, err := s.f.App.BillingKeeper.GetLease(s.f.Ctx, s.leaseUUID)
	require.NoError(t, err)
	require.Empty(t, lease.Items[0].CustomDomain)

	_, _, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "app.example.com")
	require.NoError(t, err)
	require.False(t, has)

	_, err = s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), s.leaseUUID, "", "app.example.com")
	require.NoError(t, err)
}

func TestSetLeaseItemCustomDomain_AuthorisationMatrix(t *testing.T) {
	cases := []struct {
		name     string
		who      func(s *customDomainSetup) sdk.AccAddress
		wantRole string
		wantErr  bool
	}{
		{"tenant", func(s *customDomainSetup) sdk.AccAddress { return s.tenant }, types.AttributeValueRoleTenant, false},
		{"authority", func(s *customDomainSetup) sdk.AccAddress { return s.f.Authority }, types.AttributeValueRoleAuthority, false},
		{"allowed", func(s *customDomainSetup) sdk.AccAddress { return s.allowed }, types.AttributeValueRoleAllowed, false},
		{"stranger", func(s *customDomainSetup) sdk.AccAddress { return s.stranger }, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := setupCustomDomain(t)
			role, err := s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, tc.who(s).String(), s.leaseUUID, "", "x.example.com")
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

func TestSetLeaseItemCustomDomain_ReservedSuffix(t *testing.T) {
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
			s := setupCustomDomain(t)
			params, err := s.f.App.BillingKeeper.GetParams(s.f.Ctx)
			require.NoError(t, err)
			params.ReservedDomainSuffixes = []string{".barney0.manifest0.net"}
			require.NoError(t, s.f.App.BillingKeeper.SetParams(s.f.Ctx, params))

			_, err = s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), s.leaseUUID, "", tc.domain)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, types.ErrInvalidCustomDomain)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSetLeaseItemCustomDomain_RejectedOnTerminalState(t *testing.T) {
	s := setupCustomDomain(t)
	_, err := s.msgServer.CloseLease(s.f.Ctx, &types.MsgCloseLease{
		Sender:     s.tenant.String(),
		LeaseUuids: []string{s.leaseUUID},
	})
	require.NoError(t, err)

	_, err = s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), s.leaseUUID, "", "x.example.com")
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrLeaseNotEditable)

	// Even authority cannot bypass the state check.
	_, err = s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.f.Authority.String(), s.leaseUUID, "", "x.example.com")
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrLeaseNotEditable)
}

// --- multi-item service-mode lease ---

func TestSetLeaseItemCustomDomain_MultiItemServiceMode_PerItemDomains(t *testing.T) {
	s := setupCustomDomain(t)
	leaseUUID := s.createMultiItemLease(t)

	_, err := s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), leaseUUID, "web", "web.example.com")
	require.NoError(t, err)
	_, err = s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), leaseUUID, "db", "db.example.com")
	require.NoError(t, err)

	got, serviceName, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "web.example.com")
	require.NoError(t, err)
	require.True(t, has)
	require.Equal(t, leaseUUID, got.Uuid)
	require.Equal(t, "web", serviceName)

	got, serviceName, has, err = s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "db.example.com")
	require.NoError(t, err)
	require.True(t, has)
	require.Equal(t, leaseUUID, got.Uuid)
	require.Equal(t, "db", serviceName)

	// Lease record carries both per-item domains.
	lease, err := s.f.App.BillingKeeper.GetLease(s.f.Ctx, leaseUUID)
	require.NoError(t, err)
	domainsByService := map[string]string{}
	for _, item := range lease.Items {
		domainsByService[item.ServiceName] = item.CustomDomain
	}
	require.Equal(t, "web.example.com", domainsByService["web"])
	require.Equal(t, "db.example.com", domainsByService["db"])
}

func TestSetLeaseItemCustomDomain_MultiItemServiceMode_NonMatchingServiceName(t *testing.T) {
	s := setupCustomDomain(t)
	leaseUUID := s.createMultiItemLease(t)

	_, err := s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), leaseUUID, "missing", "x.example.com")
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrLeaseItemNotFound)

	// Empty service_name must NOT match in service-mode multi-item lease (no
	// items have empty service_name).
	_, err = s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), leaseUUID, "", "x.example.com")
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrLeaseItemNotFound)
}

func TestSetLeaseItemCustomDomain_MultiItemLegacy_AmbiguousLookup(t *testing.T) {
	s := setupCustomDomain(t)
	leaseUUID, _ := s.createMultiItemLegacyLease(t)

	// Empty service_name matches both items → ambiguous, rejected.
	_, err := s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), leaseUUID, "", "x.example.com")
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrAmbiguousLeaseItem)
}

func TestSetLeaseItemCustomDomain_PreFlightUniqueness(t *testing.T) {
	s := setupCustomDomain(t)
	leaseUUID := s.createMultiItemLease(t)

	// Claim web → some.example.com.
	_, err := s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), leaseUUID, "web", "some.example.com")
	require.NoError(t, err)

	t.Run("idempotent re-set on same item", func(t *testing.T) {
		_, err := s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), leaseUUID, "web", "some.example.com")
		require.NoError(t, err)
	})

	t.Run("same lease different item → claimed by another item", func(t *testing.T) {
		_, err := s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), leaseUUID, "db", "some.example.com")
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrCustomDomainAlreadyClaimed)
		require.Contains(t, err.Error(), `item "web" on this lease`, "error must identify the conflicting sibling item")
	})

	t.Run("different lease → claimed by other lease", func(t *testing.T) {
		// Need another tenant with credit + a lease — use the default 1-item lease.
		_, err := s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), s.leaseUUID, "", "some.example.com")
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrCustomDomainAlreadyClaimed)
	})
}

func TestSetLeaseItemCustomDomain_RenameWithinItem(t *testing.T) {
	s := setupCustomDomain(t)
	leaseUUID := s.createMultiItemLease(t)

	_, err := s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), leaseUUID, "web", "old.example.com")
	require.NoError(t, err)
	_, err = s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), leaseUUID, "web", "new.example.com")
	require.NoError(t, err)

	_, _, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "old.example.com")
	require.NoError(t, err)
	require.False(t, has, "old domain must be released on rename")

	got, serviceName, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "new.example.com")
	require.NoError(t, err)
	require.True(t, has)
	require.Equal(t, leaseUUID, got.Uuid)
	require.Equal(t, "web", serviceName)
}

func TestSetLeaseItemCustomDomain_ClearOneItemLeavesSiblingsIntact(t *testing.T) {
	s := setupCustomDomain(t)
	leaseUUID := s.createMultiItemLease(t)

	_, err := s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), leaseUUID, "web", "web.example.com")
	require.NoError(t, err)
	_, err = s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), leaseUUID, "db", "db.example.com")
	require.NoError(t, err)

	// Clear only web's domain.
	_, err = s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), leaseUUID, "web", "")
	require.NoError(t, err)

	_, _, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "web.example.com")
	require.NoError(t, err)
	require.False(t, has)

	got, serviceName, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "db.example.com")
	require.NoError(t, err)
	require.True(t, has)
	require.Equal(t, leaseUUID, got.Uuid)
	require.Equal(t, "db", serviceName)
}

func TestSetLeaseItemCustomDomain_AcknowledgePreservesMultiItemIndex(t *testing.T) {
	s := setupCustomDomain(t)

	// Create a fresh PENDING multi-item lease (the helper acknowledges, so
	// build it manually here to keep PENDING state).
	pendingResp, err := s.msgServer.CreateLease(s.f.Ctx, &types.MsgCreateLease{
		Tenant: s.tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: s.sku.Uuid, Quantity: 1, ServiceName: "web"},
			{SkuUuid: s.sku.Uuid, Quantity: 1, ServiceName: "db"},
		},
	})
	require.NoError(t, err)

	_, err = s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), pendingResp.LeaseUuid, "web", "web.example.com")
	require.NoError(t, err)
	_, err = s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), pendingResp.LeaseUuid, "db", "db.example.com")
	require.NoError(t, err)

	// Acknowledge → PENDING → ACTIVE flips through SetLease and reconcile.
	_, err = s.msgServer.AcknowledgeLease(s.f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     s.providerAddr.String(),
		LeaseUuids: []string{pendingResp.LeaseUuid},
	})
	require.NoError(t, err)

	for _, dom := range []string{"web.example.com", "db.example.com"} {
		got, _, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, dom)
		require.NoError(t, err)
		require.True(t, has, "index entry must persist for %s across acknowledge", dom)
		require.Equal(t, pendingResp.LeaseUuid, got.Uuid)
	}
}

func TestSetLeaseItemCustomDomain_LifecycleCleanupWalksAllItems(t *testing.T) {
	s := setupCustomDomain(t)
	leaseUUID := s.createMultiItemLease(t)

	_, err := s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), leaseUUID, "web", "web.example.com")
	require.NoError(t, err)
	_, err = s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), leaseUUID, "db", "db.example.com")
	require.NoError(t, err)

	_, err = s.msgServer.CloseLease(s.f.Ctx, &types.MsgCloseLease{
		Sender:     s.tenant.String(),
		LeaseUuids: []string{leaseUUID},
	})
	require.NoError(t, err)

	for _, dom := range []string{"web.example.com", "db.example.com"} {
		_, _, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, dom)
		require.NoError(t, err)
		require.False(t, has, "index entry for %s must be released on close", dom)
	}

	// Lease record preserves CustomDomain on each item for audit.
	closed, err := s.f.App.BillingKeeper.GetLease(s.f.Ctx, leaseUUID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, closed.State)
	domainsByService := map[string]string{}
	for _, item := range closed.Items {
		domainsByService[item.ServiceName] = item.CustomDomain
	}
	require.Equal(t, "web.example.com", domainsByService["web"])
	require.Equal(t, "db.example.com", domainsByService["db"])
}

func TestSetLeaseItemCustomDomain_LifecycleCleanup_Cancel(t *testing.T) {
	s := setupCustomDomain(t)
	pendingResp, err := s.msgServer.CreateLease(s.f.Ctx, &types.MsgCreateLease{
		Tenant: s.tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: s.sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	_, err = s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), pendingResp.LeaseUuid, "", "cancel.example.com")
	require.NoError(t, err)

	_, err = s.msgServer.CancelLease(s.f.Ctx, &types.MsgCancelLease{
		Tenant:     s.tenant.String(),
		LeaseUuids: []string{pendingResp.LeaseUuid},
	})
	require.NoError(t, err)

	_, _, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "cancel.example.com")
	require.NoError(t, err)
	require.False(t, has)
}

func TestSetLeaseItemCustomDomain_LifecycleCleanup_Reject(t *testing.T) {
	s := setupCustomDomain(t)
	pendingResp, err := s.msgServer.CreateLease(s.f.Ctx, &types.MsgCreateLease{
		Tenant: s.tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: s.sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	_, err = s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), pendingResp.LeaseUuid, "", "reject.example.com")
	require.NoError(t, err)

	_, err = s.msgServer.RejectLease(s.f.Ctx, &types.MsgRejectLease{
		Sender:     s.providerAddr.String(),
		LeaseUuids: []string{pendingResp.LeaseUuid},
	})
	require.NoError(t, err)

	_, _, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "reject.example.com")
	require.NoError(t, err)
	require.False(t, has)
}

func TestSetLeaseItemCustomDomain_LifecycleCleanup_AutoCloseLease(t *testing.T) {
	s := setupCustomDomain(t)

	_, err := s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), s.leaseUUID, "", "auto.example.com")
	require.NoError(t, err)

	s.f.Ctx = s.f.Ctx.WithBlockTime(s.f.Ctx.BlockTime().Add(200_000_000 * time.Second))

	lease, err := s.f.App.BillingKeeper.GetLease(s.f.Ctx, s.leaseUUID)
	require.NoError(t, err)
	shouldClose, closeTime, err := s.f.App.BillingKeeper.ShouldAutoCloseLease(s.f.Ctx, &lease)
	require.NoError(t, err)
	require.True(t, shouldClose)

	params, err := s.f.App.BillingKeeper.GetParams(s.f.Ctx)
	require.NoError(t, err)
	_, err = s.f.App.BillingKeeper.AutoCloseLease(s.f.Ctx, &lease, closeTime, params.MinLeaseDuration)
	require.NoError(t, err)

	_, _, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "auto.example.com")
	require.NoError(t, err)
	require.False(t, has, "index entry must be removed on auto-close")
}

func TestSetLeaseItemCustomDomain_LifecycleCleanup_EndBlockerExpiry(t *testing.T) {
	s := setupCustomDomain(t)

	params, err := s.f.App.BillingKeeper.GetParams(s.f.Ctx)
	require.NoError(t, err)
	params.PendingTimeout = 60
	require.NoError(t, s.f.App.BillingKeeper.SetParams(s.f.Ctx, params))

	pendingResp, err := s.msgServer.CreateLease(s.f.Ctx, &types.MsgCreateLease{
		Tenant: s.tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: s.sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)
	_, err = s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), pendingResp.LeaseUuid, "", "expire.example.com")
	require.NoError(t, err)

	s.f.Ctx = s.f.Ctx.WithBlockTime(s.f.Ctx.BlockTime().Add(61 * time.Second))
	require.NoError(t, s.f.App.BillingKeeper.EndBlocker(s.f.Ctx))

	expired, err := s.f.App.BillingKeeper.GetLease(s.f.Ctx, pendingResp.LeaseUuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_EXPIRED, expired.State)

	_, _, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "expire.example.com")
	require.NoError(t, err)
	require.False(t, has)
}

// --- genesis ---

func TestInitGenesis_RebuildsCustomDomainIndex(t *testing.T) {
	f := initFixture(t)
	provider := f.createTestProvider(t, f.TestAccs[1].String(), f.TestAccs[2].String())
	sku := f.createTestSKU(t, provider.Uuid, 100)
	tenant := f.TestAccs[0]

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))

	now := f.Ctx.BlockTime()
	gs := &types.GenesisState{
		Params: types.DefaultParams(),
		Leases: []types.Lease{{
			Uuid:         "01912345-6789-7abc-8def-aaaaaaaaaaa1",
			Tenant:       tenant.String(),
			ProviderUuid: provider.Uuid,
			Items: []types.LeaseItem{
				{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1)), ServiceName: "web", CustomDomain: "web.example.com"},
				{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1)), ServiceName: "db"},
			},
			State:                      types.LEASE_STATE_PENDING,
			CreatedAt:                  now,
			LastSettledAt:              now,
			MinLeaseDurationAtCreation: 3600,
		}},
		CreditAccounts: []types.CreditAccount{
			{Tenant: tenant.String(), CreditAddress: creditAddr.String(), PendingLeaseCount: 1},
		},
	}

	require.NoError(t, f.App.BillingKeeper.InitGenesis(f.Ctx, gs))

	got, serviceName, has, err := f.App.BillingKeeper.GetLeaseByCustomDomain(f.Ctx, "web.example.com")
	require.NoError(t, err)
	require.True(t, has)
	require.Equal(t, "01912345-6789-7abc-8def-aaaaaaaaaaa1", got.Uuid)
	require.Equal(t, "web", serviceName)
}

func TestInitGenesis_DuplicateCustomDomainFails(t *testing.T) {
	f := initFixture(t)
	provider := f.createTestProvider(t, f.TestAccs[1].String(), f.TestAccs[2].String())
	sku := f.createTestSKU(t, provider.Uuid, 100)
	tenant := f.TestAccs[0]

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))

	now := f.Ctx.BlockTime()
	mkLease := func(uuid string) types.Lease {
		return types.Lease{
			Uuid:         uuid,
			Tenant:       tenant.String(),
			ProviderUuid: provider.Uuid,
			Items: []types.LeaseItem{
				{SkuUuid: sku.Uuid, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, sdkmath.NewInt(1)), CustomDomain: "dup.example.com"},
			},
			State:                      types.LEASE_STATE_PENDING,
			CreatedAt:                  now,
			LastSettledAt:              now,
			MinLeaseDurationAtCreation: 3600,
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

// TestMigrate1to2 verifies the v1→v2 migration is a true no-op: it must not
// touch Params.ReservedDomainSuffixes regardless of the pre-migration value.
// Operators seed the list at upgrade time or via post-upgrade MsgUpdateParams.
func TestMigrate1to2(t *testing.T) {
	f := initFixture(t)

	// Empty starting state stays empty.
	require.NoError(t, f.App.BillingKeeper.SetParams(f.Ctx, types.DefaultParams()))
	require.NoError(t, keeper.NewMigrator(f.App.BillingKeeper).Migrate1to2(f.Ctx))
	params, err := f.App.BillingKeeper.GetParams(f.Ctx)
	require.NoError(t, err)
	require.Empty(t, params.ReservedDomainSuffixes, "migration must not seed defaults")

	// Pre-existing operator values are preserved unchanged.
	custom := []string{".operator.zone"}
	params.ReservedDomainSuffixes = custom
	require.NoError(t, f.App.BillingKeeper.SetParams(f.Ctx, params))
	require.NoError(t, keeper.NewMigrator(f.App.BillingKeeper).Migrate1to2(f.Ctx))
	params, err = f.App.BillingKeeper.GetParams(f.Ctx)
	require.NoError(t, err)
	require.Equal(t, custom, params.ReservedDomainSuffixes)
}

// --- direct SetLease coverage (defence-in-depth and reconcile edges) ---

// TestSetLease_StorageLevelUniquenessRejection exercises the storage-level
// ErrCustomDomainAlreadyClaimed branch inside reconcileCustomDomainIndex via a
// direct SetLease call (bypassing SetLeaseItemCustomDomain's pre-flight). This
// branch is otherwise reachable only through genesis import.
func TestSetLease_StorageLevelUniquenessRejection(t *testing.T) {
	s := setupCustomDomain(t)

	// First lease (the fixture's 1-item legacy lease) claims a domain via the
	// supported path.
	_, err := s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), s.leaseUUID, "", "shared.example.com")
	require.NoError(t, err)

	// Build a second multi-item lease and try to set the same domain on its
	// "web" item via direct SetLease. The pre-flight check in
	// SetLeaseItemCustomDomain would catch this; we deliberately bypass it.
	secondUUID := s.createMultiItemLease(t)
	lease, err := s.f.App.BillingKeeper.GetLease(s.f.Ctx, secondUUID)
	require.NoError(t, err)
	for i := range lease.Items {
		if lease.Items[i].ServiceName == "web" {
			lease.Items[i].CustomDomain = "shared.example.com"
		}
	}

	err = s.f.App.BillingKeeper.SetLease(s.f.Ctx, lease)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrCustomDomainAlreadyClaimed)
	require.Contains(t, err.Error(), s.leaseUUID, "error must identify the conflicting lease")
}

// TestSetLease_StorageLevelUniqueness_SameLeaseCrossItem exercises the new
// "same lease different item" branch in reconcileCustomDomainIndex, which
// must produce a more helpful error than the generic cross-lease form.
func TestSetLease_StorageLevelUniqueness_SameLeaseCrossItem(t *testing.T) {
	s := setupCustomDomain(t)
	leaseUUID := s.createMultiItemLease(t)

	// Set web → some.example.com via the supported path.
	_, err := s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), leaseUUID, "web", "some.example.com")
	require.NoError(t, err)

	// Mutate the lease so db ALSO carries some.example.com (web still has it).
	// Direct SetLease bypasses the pre-flight; reconcile must reject with the
	// "this lease, item X" message rather than "lease Y item X".
	lease, err := s.f.App.BillingKeeper.GetLease(s.f.Ctx, leaseUUID)
	require.NoError(t, err)
	for i := range lease.Items {
		if lease.Items[i].ServiceName == "db" {
			lease.Items[i].CustomDomain = "some.example.com"
		}
	}
	err = s.f.App.BillingKeeper.SetLease(s.f.Ctx, lease)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrCustomDomainAlreadyClaimed)
	require.Contains(t, err.Error(), "on this lease", "must use the same-lease error variant")
}

// TestSetLease_CrossSwap covers a single SetLease call that swaps domains
// between two items: web's previous value moves to db, and web takes a new
// value. The reconcile must release web's old entry before installing it on
// db, otherwise the install would observe its own previous value and fail
// uniqueness.
func TestSetLease_CrossSwap(t *testing.T) {
	s := setupCustomDomain(t)
	leaseUUID := s.createMultiItemLease(t)

	_, err := s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), leaseUUID, "web", "a.example.com")
	require.NoError(t, err)
	_, err = s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), leaseUUID, "db", "b.example.com")
	require.NoError(t, err)

	// Swap: web now carries b.example.com (db's old value), db carries
	// c.example.com (new). Direct SetLease so both moves land in one reconcile.
	lease, err := s.f.App.BillingKeeper.GetLease(s.f.Ctx, leaseUUID)
	require.NoError(t, err)
	for i := range lease.Items {
		switch lease.Items[i].ServiceName {
		case "web":
			lease.Items[i].CustomDomain = "b.example.com"
		case "db":
			lease.Items[i].CustomDomain = "c.example.com"
		}
	}
	require.NoError(t, s.f.App.BillingKeeper.SetLease(s.f.Ctx, lease))

	// Reverse-lookup confirms the new shape.
	got, serviceName, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "b.example.com")
	require.NoError(t, err)
	require.True(t, has)
	require.Equal(t, leaseUUID, got.Uuid)
	require.Equal(t, "web", serviceName, "b.example.com must now belong to web")

	got, serviceName, has, err = s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "c.example.com")
	require.NoError(t, err)
	require.True(t, has)
	require.Equal(t, leaseUUID, got.Uuid)
	require.Equal(t, "db", serviceName)

	// a.example.com (web's pre-swap domain) is gone.
	_, _, has, err = s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, "a.example.com")
	require.NoError(t, err)
	require.False(t, has, "a.example.com must be released after web's domain changed")
}

// --- event attribute coverage ---

// findEvent returns the most recent event with the given type from the
// fixture's event manager, or fails the test if not found.
func findEvent(t *testing.T, ctx sdk.Context, eventType string) sdk.Event {
	t.Helper()
	events := ctx.EventManager().Events()
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == eventType {
			return events[i]
		}
	}
	t.Fatalf("event %q not found among %d emitted events", eventType, len(events))
	return sdk.Event{}
}

// attrValue returns the value of the named attribute from an event, or fails.
func attrValue(t *testing.T, ev sdk.Event, key string) string {
	t.Helper()
	for _, a := range ev.Attributes {
		if a.Key == key {
			return a.Value
		}
	}
	t.Fatalf("attribute %q not found on event %q", key, ev.Type)
	return ""
}

func TestSetLeaseItemCustomDomain_SetEvent_Attributes_Legacy(t *testing.T) {
	s := setupCustomDomain(t)

	_, err := s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), s.leaseUUID, "", "evt.example.com")
	require.NoError(t, err)

	ev := findEvent(t, s.f.Ctx, types.EventTypeLeaseCustomDomainSet)
	require.Equal(t, s.leaseUUID, attrValue(t, ev, types.AttributeKeyLeaseUUID))
	require.Equal(t, s.tenant.String(), attrValue(t, ev, types.AttributeKeyTenant))
	require.Equal(t, "", attrValue(t, ev, types.AttributeKeyServiceName), "1-item legacy emits empty service_name")
	require.Equal(t, "evt.example.com", attrValue(t, ev, types.AttributeKeyCustomDomain))
	require.Equal(t, types.AttributeValueRoleTenant, attrValue(t, ev, types.AttributeKeySetBy))
}

func TestSetLeaseItemCustomDomain_SetEvent_Attributes_MultiItem(t *testing.T) {
	s := setupCustomDomain(t)
	leaseUUID := s.createMultiItemLease(t)

	_, err := s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.f.Authority.String(), leaseUUID, "web", "evt.example.com")
	require.NoError(t, err)

	ev := findEvent(t, s.f.Ctx, types.EventTypeLeaseCustomDomainSet)
	require.Equal(t, leaseUUID, attrValue(t, ev, types.AttributeKeyLeaseUUID))
	require.Equal(t, "web", attrValue(t, ev, types.AttributeKeyServiceName))
	require.Equal(t, "evt.example.com", attrValue(t, ev, types.AttributeKeyCustomDomain))
	require.Equal(t, types.AttributeValueRoleAuthority, attrValue(t, ev, types.AttributeKeySetBy), "authority sender attribution")
}

func TestSetLeaseItemCustomDomain_ClearEvent_Attributes(t *testing.T) {
	s := setupCustomDomain(t)
	leaseUUID := s.createMultiItemLease(t)

	_, err := s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), leaseUUID, "web", "to-clear.example.com")
	require.NoError(t, err)
	_, err = s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), leaseUUID, "web", "")
	require.NoError(t, err)

	ev := findEvent(t, s.f.Ctx, types.EventTypeLeaseCustomDomainCleared)
	require.Equal(t, leaseUUID, attrValue(t, ev, types.AttributeKeyLeaseUUID))
	require.Equal(t, "web", attrValue(t, ev, types.AttributeKeyServiceName))
	require.Equal(t, "to-clear.example.com", attrValue(t, ev, types.AttributeKeyCustomDomain),
		"cleared event reports the previous domain for audit consumers")
	require.Equal(t, types.AttributeValueRoleTenant, attrValue(t, ev, types.AttributeKeySetBy))
}

// --- multi-item lifecycle cleanup beyond Close ---

func TestSetLeaseItemCustomDomain_LifecycleCleanup_MultiItem_Cancel(t *testing.T) {
	s := setupCustomDomain(t)

	pendingResp, err := s.msgServer.CreateLease(s.f.Ctx, &types.MsgCreateLease{
		Tenant: s.tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: s.sku.Uuid, Quantity: 1, ServiceName: "web"},
			{SkuUuid: s.sku.Uuid, Quantity: 1, ServiceName: "db"},
		},
	})
	require.NoError(t, err)

	_, err = s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), pendingResp.LeaseUuid, "web", "cancel-web.example.com")
	require.NoError(t, err)
	_, err = s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), pendingResp.LeaseUuid, "db", "cancel-db.example.com")
	require.NoError(t, err)

	_, err = s.msgServer.CancelLease(s.f.Ctx, &types.MsgCancelLease{
		Tenant:     s.tenant.String(),
		LeaseUuids: []string{pendingResp.LeaseUuid},
	})
	require.NoError(t, err)

	for _, dom := range []string{"cancel-web.example.com", "cancel-db.example.com"} {
		_, _, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, dom)
		require.NoError(t, err)
		require.False(t, has, "all per-item index entries must release on cancel; %q still resolved", dom)
	}
}

func TestSetLeaseItemCustomDomain_LifecycleCleanup_MultiItem_Reject(t *testing.T) {
	s := setupCustomDomain(t)

	pendingResp, err := s.msgServer.CreateLease(s.f.Ctx, &types.MsgCreateLease{
		Tenant: s.tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: s.sku.Uuid, Quantity: 1, ServiceName: "web"},
			{SkuUuid: s.sku.Uuid, Quantity: 1, ServiceName: "db"},
		},
	})
	require.NoError(t, err)

	_, err = s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), pendingResp.LeaseUuid, "web", "reject-web.example.com")
	require.NoError(t, err)
	_, err = s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), pendingResp.LeaseUuid, "db", "reject-db.example.com")
	require.NoError(t, err)

	_, err = s.msgServer.RejectLease(s.f.Ctx, &types.MsgRejectLease{
		Sender:     s.providerAddr.String(),
		LeaseUuids: []string{pendingResp.LeaseUuid},
	})
	require.NoError(t, err)

	for _, dom := range []string{"reject-web.example.com", "reject-db.example.com"} {
		_, _, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, dom)
		require.NoError(t, err)
		require.False(t, has, "all per-item index entries must release on reject; %q still resolved", dom)
	}
}

func TestSetLeaseItemCustomDomain_LifecycleCleanup_MultiItem_EndBlockerExpiry(t *testing.T) {
	s := setupCustomDomain(t)

	params, err := s.f.App.BillingKeeper.GetParams(s.f.Ctx)
	require.NoError(t, err)
	params.PendingTimeout = 60
	require.NoError(t, s.f.App.BillingKeeper.SetParams(s.f.Ctx, params))

	pendingResp, err := s.msgServer.CreateLease(s.f.Ctx, &types.MsgCreateLease{
		Tenant: s.tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: s.sku.Uuid, Quantity: 1, ServiceName: "web"},
			{SkuUuid: s.sku.Uuid, Quantity: 1, ServiceName: "db"},
		},
	})
	require.NoError(t, err)

	_, err = s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), pendingResp.LeaseUuid, "web", "expire-web.example.com")
	require.NoError(t, err)
	_, err = s.f.App.BillingKeeper.SetLeaseItemCustomDomain(s.f.Ctx, s.tenant.String(), pendingResp.LeaseUuid, "db", "expire-db.example.com")
	require.NoError(t, err)

	s.f.Ctx = s.f.Ctx.WithBlockTime(s.f.Ctx.BlockTime().Add(61 * time.Second))
	require.NoError(t, s.f.App.BillingKeeper.EndBlocker(s.f.Ctx))

	expired, err := s.f.App.BillingKeeper.GetLease(s.f.Ctx, pendingResp.LeaseUuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_EXPIRED, expired.State)

	for _, dom := range []string{"expire-web.example.com", "expire-db.example.com"} {
		_, _, has, err := s.f.App.BillingKeeper.GetLeaseByCustomDomain(s.f.Ctx, dom)
		require.NoError(t, err)
		require.False(t, has, "all per-item index entries must release on EndBlocker expiry; %q still resolved", dom)
	}
}
