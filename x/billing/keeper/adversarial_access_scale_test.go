/*
Package keeper_test contains adversarial tests for access control bypass attempts
and scale/concurrency stress tests.
*/
package keeper_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// =============================================================================
// Access Control Bypass Attempts
// =============================================================================

// TestAdversarial_TenantCannotCancelOtherTenantsPendingLease tests that a tenant
// cannot cancel another tenant's pending lease.
func TestAdversarial_TenantCannotCancelOtherTenantsPendingLease(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant1 := f.TestAccs[0]
	tenant2 := f.TestAccs[1]
	providerAddr := f.TestAccs[2]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Setup both tenants
	for _, tenant := range []sdk.AccAddress{tenant1, tenant2} {
		creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
		require.NoError(t, err)
		f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))
		err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
			Tenant:        tenant.String(),
			CreditAddress: creditAddr.String(),
		})
		require.NoError(t, err)
	}

	// Tenant1 creates a pending lease
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant1.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)

	// Tenant2 tries to cancel tenant1's lease
	_, err = msgServer.CancelLease(f.Ctx, &types.MsgCancelLease{
		Tenant:     tenant2.String(),
		LeaseUuids: []string{createResp.LeaseUuid},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not the tenant")
}

// TestAdversarial_NonAuthorityCannotCreateLeaseForTenant tests that a non-authority
// address cannot use CreateLeaseForTenant.
func TestAdversarial_NonAuthorityCannotCreateLeaseForTenant(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	attacker := f.TestAccs[2]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Attacker tries to create lease for tenant
	_, err = msgServer.CreateLeaseForTenant(f.Ctx, &types.MsgCreateLeaseForTenant{
		Authority: attacker.String(),
		Tenant:    tenant.String(),
		Items:     []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

// TestAdversarial_NonAuthorityCannotUpdateParams tests that only the governance
// authority can update module parameters.
func TestAdversarial_NonAuthorityCannotUpdateParams(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	attacker := f.TestAccs[0]

	_, err := msgServer.UpdateParams(f.Ctx, &types.MsgUpdateParams{
		Authority: attacker.String(),
		Params:    types.DefaultParams(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

// TestAdversarial_ProviderCannotCloseOtherProvidersLease tests that a provider
// cannot close a lease belonging to a different provider.
func TestAdversarial_ProviderCannotCloseOtherProvidersLease(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr1 := f.TestAccs[1]
	providerAddr2 := f.TestAccs[2]

	provider1 := f.createTestProvider(t, providerAddr1.String(), providerAddr1.String())
	f.createTestProvider(t, providerAddr2.String(), providerAddr2.String())
	sku1 := f.createTestSKU(t, provider1.Uuid, 3600)

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create lease with provider1
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr1, []types.LeaseItemInput{
		{SkuUuid: sku1.Uuid, Quantity: 1},
	})

	// Provider2 tries to close provider1's lease
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     providerAddr2.String(),
		LeaseUuids: []string{leaseID},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

// withdrawAccessFixture is two providers that each serve the same two tenants,
// 100 seconds after acknowledgement: four streams, each owing 100.
type withdrawAccessFixture struct {
	*testFixture
	msgServer types.MsgServer
	tenants   []sdk.AccAddress
	// provider 1 is the victim, provider 2 the other provider.
	provider1, provider2         string
	providerAddr1, providerAddr2 sdk.AccAddress
	payout1, payout2             sdk.AccAddress
}

func newWithdrawAccessFixture(t *testing.T) *withdrawAccessFixture {
	t.Helper()
	f := initFixture(t)
	w := &withdrawAccessFixture{
		testFixture:   f,
		msgServer:     keeper.NewMsgServerImpl(f.App.BillingKeeper),
		tenants:       []sdk.AccAddress{f.TestAccs[0], f.TestAccs[1]},
		providerAddr1: f.TestAccs[2],
		providerAddr2: f.TestAccs[3],
		payout1:       sdk.AccAddress("adv-access-payout-1_"),
		payout2:       sdk.AccAddress("adv-access-payout-2_"),
	}

	w.provider1 = f.createTestProvider(t, w.providerAddr1.String(), w.payout1.String()).Uuid
	w.provider2 = f.createTestProvider(t, w.providerAddr2.String(), w.payout2.String()).Uuid
	sku1 := f.createTestSKU(t, w.provider1, 3600)
	sku2 := f.createTestSKU(t, w.provider2, 3600)

	for _, tenant := range w.tenants {
		creditAddr := types.DeriveCreditAddress(tenant)
		f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))
		require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
			Tenant:        tenant.String(),
			CreditAddress: creditAddr.String(),
		}))
		f.createAndAcknowledgeLease(t, w.msgServer, tenant, w.providerAddr1, []types.LeaseItemInput{{SkuUuid: sku1.Uuid, Quantity: 1}})
		f.createAndAcknowledgeLease(t, w.msgServer, tenant, w.providerAddr2, []types.LeaseItemInput{{SkuUuid: sku2.Uuid, Quantity: 1}})
	}

	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
	return w
}

func (w *withdrawAccessFixture) balance(addr sdk.AccAddress) sdkmath.Int {
	return w.App.BankKeeper.GetBalance(w.Ctx, addr, testDenom).Amount
}

// streamsOf returns the stored streams of one provider.
func (w *withdrawAccessFixture) streamsOf(t *testing.T, providerUUID string) []types.Stream {
	t.Helper()
	all, err := w.App.BillingKeeper.GetAllStreams(w.Ctx)
	require.NoError(t, err)
	out := make([]types.Stream, 0, len(all))
	for _, stream := range all {
		if stream.ProviderUuid == providerUUID {
			out = append(out, stream)
		}
	}
	return out
}

// TestAdversarial_ThirdPartyCannotWithdrawAnotherProvidersStreams tests that
// nobody but a provider (or the authority) can settle that provider's streams:
// not a stranger, not a tenant, not another provider, and not with a pagination
// key crafted to point into the victim's streams.
func TestAdversarial_ThirdPartyCannotWithdrawAnotherProvidersStreams(t *testing.T) {
	cursorCodec := collections.PairKeyCodec(sdk.AccAddressKey, collections.StringKey)
	cursor := func(t *testing.T, tenant sdk.AccAddress, denom string) []byte {
		t.Helper()
		key := collections.Join(tenant, denom)
		buf := make([]byte, cursorCodec.Size(key))
		_, err := cursorCodec.Encode(buf, key)
		require.NoError(t, err)
		return buf
	}

	t.Run("a sender that is not the provider is rejected and settles nothing", func(t *testing.T) {
		w := newWithdrawAccessFixture(t)
		stranger := w.TestAccs[4]

		tests := []struct {
			name   string
			sender sdk.AccAddress
			key    []byte
		}{
			{name: "stranger", sender: stranger},
			{name: "tenant of the provider", sender: w.tenants[0]},
			{name: "the provider's own payout address", sender: w.payout1},
			{name: "another provider", sender: w.providerAddr2},
			{name: "another provider's payout address", sender: w.payout2},
			{name: "another provider with a cursor into the victim's streams", sender: w.providerAddr2, key: cursor(t, w.tenants[0], "")},
			{name: "stranger with a cursor naming a real stream", sender: stranger, key: cursor(t, w.tenants[1], testDenom)},
		}

		before := w.streamsOf(t, w.provider1)
		require.Len(t, before, 2)

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				for _, limit := range []uint64{0, 1, types.MaxBatchLeaseSize} {
					_, err := w.msgServer.Withdraw(w.Ctx, &types.MsgWithdraw{
						Sender:       tc.sender.String(),
						ProviderUuid: w.provider1,
						Limit:        limit,
						Key:          tc.key,
					})
					require.ErrorIs(t, err, types.ErrUnauthorized)
				}

				// Nothing moved: no payout, no debit, and the streams still owe
				// what they owed.
				require.True(t, w.balance(w.payout1).IsZero())
				require.True(t, w.balance(tc.sender).IsZero())
				for _, tenant := range w.tenants {
					require.Equal(t, sdkmath.NewInt(1_000_000), w.balance(types.DeriveCreditAddress(tenant)))
				}
				require.Equal(t, before, w.streamsOf(t, w.provider1))
			})
		}

		// The provider itself is still owed, and paid, everything.
		resp, err := w.msgServer.Withdraw(w.Ctx, &types.MsgWithdraw{
			Sender:       w.providerAddr1.String(),
			ProviderUuid: w.provider1,
		})
		require.NoError(t, err)
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(200))), resp.TotalAmounts)
		require.Equal(t, w.payout1.String(), resp.PayoutAddress)
		require.Equal(t, sdkmath.NewInt(200), w.balance(w.payout1))
		requireInvariants(t, w.testFixture)
	})

	t.Run("a provider withdrawing its own streams cannot reach another provider's with a crafted key", func(t *testing.T) {
		longAddr := make([]byte, 255)
		for i := range longAddr {
			longAddr[i] = 0xff
		}

		tests := []struct {
			name string
			key  func(t *testing.T, w *withdrawAccessFixture) []byte
			// rejected is true when the key is refused outright.
			rejected bool
		}{
			{name: "cursor before every stream", key: func(t *testing.T, _ *withdrawAccessFixture) []byte {
				return cursor(t, sdk.AccAddress(make([]byte, 20)), "")
			}},
			{name: "cursor on a tenant both providers serve", key: func(t *testing.T, w *withdrawAccessFixture) []byte {
				return cursor(t, w.tenants[0], testDenom)
			}},
			{name: "cursor after every stream", key: func(t *testing.T, _ *withdrawAccessFixture) []byte {
				return cursor(t, sdk.AccAddress(longAddr), "\xff\xff\xff\xff")
			}},
			{name: "cursor with an empty tenant", key: func(t *testing.T, _ *withdrawAccessFixture) []byte {
				return cursor(t, sdk.AccAddress{}, testDenom)
			}},
			{name: "cursor whose denom embeds the victim's provider uuid", key: func(t *testing.T, w *withdrawAccessFixture) []byte {
				return cursor(t, w.tenants[0], "\x00"+w.provider1+"\x00"+string(w.tenants[0])+testDenom)
			}},
			{name: "cursor whose tenant is the victim's provider uuid", key: func(t *testing.T, w *withdrawAccessFixture) []byte {
				return cursor(t, sdk.AccAddress(w.provider1+"\x00"), testDenom)
			}},
			{name: "truncated cursor", rejected: true, key: func(*testing.T, *withdrawAccessFixture) []byte {
				return []byte{0xff}
			}},
			{name: "length prefix longer than the key", rejected: true, key: func(*testing.T, *withdrawAccessFixture) []byte {
				return []byte{20, 1, 2, 3}
			}},
			{name: "oversized key", rejected: true, key: func(*testing.T, *withdrawAccessFixture) []byte {
				return make([]byte, types.MaxWithdrawCursorLen+1)
			}},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				w := newWithdrawAccessFixture(t)
				before := w.streamsOf(t, w.provider1)
				require.Len(t, before, 2)

				var (
					resp *types.MsgWithdrawResponse
					err  error
				)
				require.NotPanics(t, func() {
					resp, err = w.msgServer.Withdraw(w.Ctx, &types.MsgWithdraw{
						Sender:       w.providerAddr2.String(),
						ProviderUuid: w.provider2,
						Limit:        types.MaxBatchLeaseSize,
						Key:          tc.key(t, w),
					})
				})

				paid := w.balance(w.payout2)
				if tc.rejected {
					require.ErrorIs(t, err, types.ErrInvalidRequest)
					require.True(t, paid.IsZero())
				} else {
					require.NoError(t, err)
					require.Equal(t, w.payout2.String(), resp.PayoutAddress)
					// A cursor can only skip the caller's own streams: provider 2
					// is paid for at most its own two streams, never more.
					require.True(t, paid.LTE(sdkmath.NewInt(200)), "provider 2 was paid %s for streams owing 200", paid)
					require.Equal(t, paid.String(), resp.TotalAmounts.AmountOf(testDenom).String())
				}

				// The victim's streams were not settled and its payout address
				// received nothing.
				require.True(t, w.balance(w.payout1).IsZero())
				require.Equal(t, before, w.streamsOf(t, w.provider1))

				// The tenants paid exactly what provider 2 received.
				debited := sdkmath.ZeroInt()
				for _, tenant := range w.tenants {
					debited = debited.Add(sdkmath.NewInt(1_000_000).Sub(w.balance(types.DeriveCreditAddress(tenant))))
				}
				require.Equal(t, paid.String(), debited.String())

				// Provider 1 still collects everything it is owed.
				own, err := w.msgServer.Withdraw(w.Ctx, &types.MsgWithdraw{
					Sender:       w.providerAddr1.String(),
					ProviderUuid: w.provider1,
				})
				require.NoError(t, err)
				require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(200))), own.TotalAmounts)
				requireInvariants(t, w.testFixture)
			})
		}
	})

	t.Run("the authority may settle a provider's streams, but only to that provider's payout address", func(t *testing.T) {
		w := newWithdrawAccessFixture(t)
		resp, err := w.msgServer.Withdraw(w.Ctx, &types.MsgWithdraw{
			Sender:       w.Authority.String(),
			ProviderUuid: w.provider1,
		})
		require.NoError(t, err)
		require.Equal(t, w.payout1.String(), resp.PayoutAddress)
		require.Equal(t, sdkmath.NewInt(200), w.balance(w.payout1))
		require.True(t, w.balance(w.Authority).IsZero())
		require.True(t, w.balance(w.payout2).IsZero())
		requireInvariants(t, w.testFixture)
	})

	t.Run("an unknown provider uuid is rejected", func(t *testing.T) {
		w := newWithdrawAccessFixture(t)
		_, err := w.msgServer.Withdraw(w.Ctx, &types.MsgWithdraw{
			Sender:       w.providerAddr2.String(),
			ProviderUuid: testProviderUUID,
		})
		require.ErrorIs(t, err, types.ErrProviderNotFound)
	})
}

// TestAdversarial_BatchCloseWithMixedAuthRoles tests that batch close rejects
// when the sender has different roles for different leases.
func TestAdversarial_BatchCloseWithMixedAuthRoles(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	// tenant is also a provider's address for their own leases
	tenant := f.TestAccs[0]
	otherTenant := f.TestAccs[1]
	providerAddr := f.TestAccs[0] // Same as tenant!

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Setup both tenants
	for _, tn := range []sdk.AccAddress{tenant, otherTenant} {
		creditAddr, err := types.DeriveCreditAddressFromBech32(tn.String())
		require.NoError(t, err)
		f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))
		err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
			Tenant:        tn.String(),
			CreditAddress: creditAddr.String(),
		})
		require.NoError(t, err)
	}

	// Lease 1: tenant is the tenant (role: tenant)
	leaseID1 := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})
	// Lease 2: otherTenant's lease where sender is the provider (role: provider)
	leaseID2 := f.createAndAcknowledgeLease(t, msgServer, otherTenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	// Try batch close: sender has tenant role for lease1 but provider role for lease2
	_, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseID1, leaseID2},
	})
	require.Error(t, err, "batch close with mixed roles should be rejected")
	require.Contains(t, err.Error(), "inconsistent authorization")
}

// =============================================================================
// Lease Limit Enforcement
// =============================================================================

// TestAdversarial_MaxLeasesPerTenantEnforcement tests that creating more than the
// max allowed active leases is properly blocked.
func TestAdversarial_MaxLeasesPerTenantEnforcement(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Set very low max leases
	err := f.App.BillingKeeper.SetParams(f.Ctx, types.Params{
		MaxLeasesPerTenant:        2,
		MaxItemsPerLease:          20,
		MinLeaseDuration:          60,
		MaxPendingLeasesPerTenant: 10,
		PendingTimeout:            1800,
	})
	require.NoError(t, err)

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100_000_000))))
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create and acknowledge 2 leases (at the limit)
	for i := 0; i < 2; i++ {
		_ = f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		})
	}

	// Third lease creation should fail
	_, err = msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "maximum leases")
}

// TestAdversarial_MaxPendingLeasesEnforcement tests that the pending lease limit
// is properly enforced.
func TestAdversarial_MaxPendingLeasesEnforcement(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Set very low pending limit
	err := f.App.BillingKeeper.SetParams(f.Ctx, types.Params{
		MaxLeasesPerTenant:        100,
		MaxItemsPerLease:          20,
		MinLeaseDuration:          60,
		MaxPendingLeasesPerTenant: 2,
		PendingTimeout:            1800,
	})
	require.NoError(t, err)

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100_000_000))))
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create 2 pending leases (at the limit)
	for i := 0; i < 2; i++ {
		_, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
			Tenant: tenant.String(),
			Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
		})
		require.NoError(t, err)
	}

	// Third should fail
	_, err = msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "pending leases")
}

// =============================================================================
// Batch Operation Atomicity
// =============================================================================

// TestAdversarial_BatchAcknowledgeAtomicity tests that batch acknowledge is atomic:
// if one lease fails validation, none should be acknowledged.
func TestAdversarial_BatchAcknowledgeAtomicity(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100_000_000))))
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create 2 pending leases
	resp1, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)

	resp2, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)

	// Acknowledge lease1 individually first
	_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{resp1.LeaseUuid},
	})
	require.NoError(t, err)

	// Try batch acknowledge with lease1 (already ACTIVE) and lease2 (PENDING)
	// This should fail atomically because lease1 is not PENDING
	_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{resp1.LeaseUuid, resp2.LeaseUuid},
	})
	require.Error(t, err, "batch acknowledge with non-pending lease should fail atomically")

	// Lease2 should still be PENDING (not partially acknowledged)
	lease2, err := f.App.BillingKeeper.GetLease(f.Ctx, resp2.LeaseUuid)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_PENDING, lease2.State,
		"batch failure should not have partially acknowledged lease2")
}

// =============================================================================
// Scale Tests
// =============================================================================

// TestAdversarial_ManyActiveLeasesFundConservation creates many active leases
// and verifies fund conservation across all of them.
func TestAdversarial_ManyActiveLeasesFundConservation(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Increase limits
	err := f.App.BillingKeeper.SetParams(f.Ctx, types.Params{
		MaxLeasesPerTenant:        200,
		MaxItemsPerLease:          20,
		MinLeaseDuration:          60,
		MaxPendingLeasesPerTenant: 200,
		PendingTimeout:            1800,
	})
	require.NoError(t, err)

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	initialFunding := sdkmath.NewInt(1_000_000_000)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, initialFunding)))
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create 50 active leases
	numLeases := 50
	var leaseIDs []string
	for i := 0; i < numLeases; i++ {
		leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		})
		leaseIDs = append(leaseIDs, leaseID)
	}

	providerBalBefore := f.App.BankKeeper.GetBalance(f.Ctx, providerAddr, testDenom)

	// All 50 leases share one stream
	state, err := f.App.BillingKeeper.Streams.Get(f.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(int64(numLeases)), state.Rate)
	require.Equal(t, uint64(numLeases), state.LeaseCount) //nolint:gosec // test code, numLeases is 50
	requireInvariants(t, f)

	// Advance time
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))

	// Close all leases in a batch: the stream is settled once, with one transfer
	f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())
	resp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: leaseIDs,
	})
	require.NoError(t, err)
	// 50 leases at 1/s for 100 seconds
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(5000))), resp.TotalSettledAmounts)
	require.Equal(t, 1, countEvents(f.Ctx, "transfer"))

	// Fund conservation check
	providerBalAfter := f.App.BankKeeper.GetBalance(f.Ctx, providerAddr, testDenom)
	creditBalAfter := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom)

	providerGain := providerBalAfter.Amount.Sub(providerBalBefore.Amount)
	totalAccounted := providerGain.Add(creditBalAfter.Amount)

	require.Equal(t, sdkmath.NewInt(5000), providerGain)
	require.Equal(t, initialFunding, totalAccounted,
		"fund conservation violated: provider gained %s + credit remaining %s = %s, but started with %s",
		providerGain, creditBalAfter.Amount, totalAccounted, initialFunding)

	// The emptied stream is gone
	streams, err := f.App.BillingKeeper.GetAllStreams(f.Ctx)
	require.NoError(t, err)
	require.Empty(t, streams)
	requireInvariants(t, f)
}

// =============================================================================
// Inactive SKU / Provider Edge Cases
// =============================================================================

// TestAdversarial_CreateLeaseWithInactiveSKU tests that creating a lease with an
// inactive SKU is properly rejected.
func TestAdversarial_CreateLeaseWithInactiveSKU(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Deactivate the SKU
	sku.Active = false
	err := f.App.SKUKeeper.SetSKU(f.Ctx, sku)
	require.NoError(t, err)

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	_, err = msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not active")
}

// TestAdversarial_CreateLeaseWithInactiveProvider tests that creating a lease with
// an inactive provider is properly rejected.
func TestAdversarial_CreateLeaseWithInactiveProvider(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Deactivate the provider
	provider.Active = false
	err := f.App.SKUKeeper.SetProvider(f.Ctx, provider)
	require.NoError(t, err)

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	_, err = msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not active")
}

// TestAdversarial_CreateLeaseWithoutCreditAccount tests that creating a lease
// without a credit account is properly rejected.
func TestAdversarial_CreateLeaseWithoutCreditAccount(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Don't create credit account

	_, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "credit account")
}

// TestAdversarial_FundCreditZeroAmount tests that funding with zero amount is rejected.
func TestAdversarial_FundCreditZeroAmount(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	sender := f.TestAccs[0]
	tenant := f.TestAccs[1]

	_, err := msgServer.FundCredit(f.Ctx, &types.MsgFundCredit{
		Sender: sender.String(),
		Tenant: tenant.String(),
		Amount: sdk.NewCoin(testDenom, sdkmath.ZeroInt()),
	})
	require.Error(t, err)
}

// TestAdversarial_MaxItemsPerLeaseEnforcement tests that the hard limit on items
// per lease is enforced at the message level.
func TestAdversarial_MaxItemsPerLeaseEnforcement(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())

	// Create 101 SKUs
	var items []types.LeaseItemInput
	for i := 0; i < types.MaxItemsPerLeaseHardLimit+1; i++ {
		sku := f.createTestSKU(t, provider.Uuid, 3600)
		items = append(items, types.LeaseItemInput{
			SkuUuid:     sku.Uuid,
			Quantity:    1,
			ServiceName: fmt.Sprintf("svc%d", i),
		})
	}

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000_000))))
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	_, err = msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  items,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "too many items")
}
