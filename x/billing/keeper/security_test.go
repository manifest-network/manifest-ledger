/*
Package keeper contains explicit security tests for the billing module.

Security Test Coverage:
- Authorization checks: only authorized parties can perform actions
- Overflow protection: calculations handle extreme values safely
- Cross-tenant isolation: users cannot access other users' resources
- Input validation: malformed inputs are properly rejected
- Denom-spam resistance: dust tokens on credit addresses don't leak into billing paths
*/
package keeper_test

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// ============================================================================
// Authorization Tests
// ============================================================================

func TestSecurity_UnauthorizedLeaseClose(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	attacker := f.TestAccs[2]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create and acknowledge a lease
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	// Attacker tries to close someone else's lease
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     attacker.String(),
		LeaseUuids: []string{leaseID},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestSecurity_UnauthorizedProviderWithdrawal(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	attacker := f.TestAccs[2]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create and acknowledge a lease
	f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	// Advance time to accrue some amount
	newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
	f.Ctx = newCtx

	streamKey := keeper.NewStreamKey(provider.Uuid, tenant, testDenom)
	streamBefore, err := f.App.BillingKeeper.Streams.Get(f.Ctx, streamKey)
	require.NoError(t, err)

	// Attacker (not the provider) tries to withdraw the provider's streams
	_, err = msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:       attacker.String(),
		ProviderUuid: provider.Uuid,
	})
	require.ErrorIs(t, err, types.ErrUnauthorized)
	require.Contains(t, err.Error(), "unauthorized")

	// The rejected withdrawal settled nothing: no transfer, stream untouched.
	require.Equal(t, sdkmath.NewInt(10000), f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom).Amount)
	require.True(t, f.App.BankKeeper.GetBalance(f.Ctx, providerAddr, testDenom).IsZero())
	require.True(t, f.App.BankKeeper.GetBalance(f.Ctx, attacker, testDenom).IsZero())
	streamAfter, err := f.App.BillingKeeper.Streams.Get(f.Ctx, streamKey)
	require.NoError(t, err)
	require.Equal(t, streamBefore, streamAfter)
	requireInvariants(t, f)
}

func TestSecurity_UnauthorizedLeaseAcknowledgement(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	attacker := f.TestAccs[2]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create a pending lease
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		},
	})
	require.NoError(t, err)

	// Attacker (not the provider) tries to acknowledge the lease
	_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     attacker.String(),
		LeaseUuids: []string{createResp.LeaseUuid},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestSecurity_UnauthorizedLeaseRejection(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	attacker := f.TestAccs[2]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create a pending lease
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		},
	})
	require.NoError(t, err)

	// Attacker (not the provider) tries to reject the lease
	_, err = msgServer.RejectLease(f.Ctx, &types.MsgRejectLease{
		Sender:     attacker.String(),
		LeaseUuids: []string{createResp.LeaseUuid},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

// ============================================================================
// Overflow Protection Tests
// ============================================================================

// TestSecurity_AccrualOverflowProtection verifies that an accrual that cannot be
// represented never panics, never errors out of the message, and never pays the
// provider more than the tenant's remaining credit. Both overflow guards are
// covered: a gap longer than MaxDurationSeconds, and a rate × seconds product
// that does not fit in 256 bits.
func TestSecurity_AccrualOverflowProtection(t *testing.T) {
	const funded = int64(1_000_000_000_000)

	// hugeRate × 4 seconds overflows sdkmath.Int (256 bits) while the gap is far
	// below MaxDurationSeconds, so only the SafeMul guard can catch it.
	hugeRate := sdkmath.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 254))

	tests := []struct {
		name    string
		rate    sdkmath.Int
		elapsed time.Duration
		// viaClose settles through CloseLease instead of Withdraw.
		viaClose bool
	}{
		{name: "gap over 100 years, withdraw", rate: sdkmath.NewInt(1000), elapsed: 101 * 365 * 24 * time.Hour},
		{name: "gap over 100 years, close", rate: sdkmath.NewInt(1000), elapsed: 101 * 365 * 24 * time.Hour, viaClose: true},
		{name: "rate times seconds overflows 256 bits, withdraw", rate: hugeRate, elapsed: 4 * time.Second},
		{name: "rate times seconds overflows 256 bits, close", rate: hugeRate, elapsed: 4 * time.Second, viaClose: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			k := f.App.BillingKeeper
			msgServer := keeper.NewMsgServerImpl(k)

			tenant := f.TestAccs[0]
			providerAddr := f.TestAccs[1]
			payoutAddr := f.TestAccs[2]
			provider := f.createTestProvider(t, providerAddr.String(), payoutAddr.String())

			creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
			require.NoError(t, err)
			f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(funded))))
			// The handlers cannot create a lease with such a rate (its reservation
			// would exceed any balance), so write it and its stream directly. A
			// one-second reservation keeps the reservation arithmetic in range, so
			// the accrual is the only thing that can overflow.
			require.NoError(t, k.SetCreditAccount(f.Ctx, types.CreditAccount{
				Tenant:           tenant.String(),
				CreditAddress:    creditAddr.String(),
				ActiveLeaseCount: 1,
				ReservedAmounts:  sdk.NewCoins(sdk.NewCoin(testDenom, tc.rate)),
			}))
			now := f.Ctx.BlockTime()
			require.NoError(t, k.SetLease(f.Ctx, types.Lease{
				Uuid:         testLeaseUUID1,
				Tenant:       tenant.String(),
				ProviderUuid: provider.Uuid,
				Items: []types.LeaseItem{
					{SkuUuid: testSKUUUID, Quantity: 1, LockedPrice: sdk.NewCoin(testDenom, tc.rate)},
				},
				State:                      types.LEASE_STATE_ACTIVE,
				CreatedAt:                  now,
				MinLeaseDurationAtCreation: 1,
			}))
			f.syncStreams(t, now)

			f.Ctx = f.Ctx.WithBlockTime(now.Add(tc.elapsed))
			want := sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(funded)))

			if tc.viaClose {
				resp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
					Sender:     tenant.String(),
					LeaseUuids: []string{testLeaseUUID1},
					Reason:     "done",
				})
				require.NoError(t, err)
				require.Equal(t, want, resp.TotalSettledAmounts)

				lease, err := k.GetLease(f.Ctx, testLeaseUUID1)
				require.NoError(t, err)
				require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
				require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason,
					"an unrepresentable accrual is credit exhaustion, whatever the caller says")
			} else {
				resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
					Sender:       providerAddr.String(),
					ProviderUuid: provider.Uuid,
				})
				require.NoError(t, err)
				require.Equal(t, want, resp.TotalAmounts)
				require.Equal(t, uint64(1), resp.WithdrawalCount)

				// The stream is frozen, so a second withdrawal pays nothing more.
				state, err := k.Streams.Get(f.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
				require.NoError(t, err)
				require.True(t, state.IsExhausted())
				again, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
					Sender:       providerAddr.String(),
					ProviderUuid: provider.Uuid,
				})
				require.NoError(t, err)
				require.True(t, again.TotalAmounts.IsZero())
			}

			// All remaining credit went to the provider, and not a unit more.
			require.Equal(t, sdkmath.NewInt(funded), f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, testDenom).Amount)
			require.True(t, f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom).IsZero())

			// The frozen stream is drained like any other: the lease is closed for
			// exhausted credit and its reservation is released.
			require.NoError(t, k.EndBlocker(f.Ctx))
			lease, err := k.GetLease(f.Ctx, testLeaseUUID1)
			require.NoError(t, err)
			require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
			require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason)
			account, err := k.GetCreditAccount(f.Ctx, tenant.String())
			require.NoError(t, err)
			require.Zero(t, account.ActiveLeaseCount)
			require.True(t, account.ReservedAmounts.IsZero())
			require.NoError(t, k.CheckStreamInvariant(f.Ctx))
		})
	}
}

func TestSecurity_ExtremeQuantityValues(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant credit account with large amount
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000_000_000_000))))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Try to create lease with extremely high quantity
	_, err = msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1<<63 - 1}, // Max int64
		},
	})
	// Should be rejected or handled safely
	require.Error(t, err)
}

// ============================================================================
// Cross-Tenant Isolation Tests
// ============================================================================

func TestSecurity_TenantCreditAccountIsolation(t *testing.T) {
	f := initFixture(t)

	tenant1 := f.TestAccs[0]
	tenant2 := f.TestAccs[1]

	// Fund tenant1's credit account
	creditAddr1, err := types.DeriveCreditAddressFromBech32(tenant1.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr1, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Fund tenant2's credit account with different amount
	creditAddr2, err := types.DeriveCreditAddressFromBech32(tenant2.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr2, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(5000))))

	// Verify credit addresses are different (deterministic derivation)
	require.NotEqual(t, creditAddr1.String(), creditAddr2.String())

	// Verify each tenant's balance is correct
	balance1 := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr1, testDenom)
	require.Equal(t, int64(10000), balance1.Amount.Int64())

	balance2 := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr2, testDenom)
	require.Equal(t, int64(5000), balance2.Amount.Int64())

	// Credit account addresses are deterministically derived from tenant addresses
	// so there's no way for one tenant to "reach" another's credit account
}

func TestSecurity_LeaseIsolation(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant1 := f.TestAccs[0]
	tenant2 := f.TestAccs[1]
	providerAddr := f.TestAccs[2]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund both tenant credit accounts
	for _, tenant := range []sdk.AccAddress{tenant1, tenant2} {
		creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
		require.NoError(t, err)
		f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

		err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
			Tenant:        tenant.String(),
			CreditAddress: creditAddr.String(),
		})
		require.NoError(t, err)
	}

	// Create an ACTIVE lease for tenant1
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant1, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	// Tenant2 cannot close tenant1's ACTIVE lease
	_, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant2.String(),
		LeaseUuids: []string{leaseID},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")

	// Create a PENDING lease for tenant1 (to test CancelLease isolation)
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant1.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 1},
		},
	})
	require.NoError(t, err)

	// Tenant2 cannot cancel tenant1's PENDING lease (both have credit accounts, but tenant2 doesn't own the lease)
	_, err = msgServer.CancelLease(f.Ctx, &types.MsgCancelLease{
		Tenant:     tenant2.String(),
		LeaseUuids: []string{createResp.LeaseUuid},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "is not the tenant")
}

// ============================================================================
// Input Validation Tests
// ============================================================================

func TestSecurity_InvalidUUIDFormat(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]

	tests := []struct {
		name    string
		leaseID string
	}{
		{"empty UUID", ""},
		{"invalid format", "not-a-uuid"},
		{"SQL injection attempt", "'; DROP TABLE leases; --"},
		{"path traversal", "../../../etc/passwd"},
		{"too long", "01912345-6789-7abc-8def-0123456789ab-extra-stuff-that-should-not-be-here"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
				Sender:     tenant.String(),
				LeaseUuids: []string{tc.leaseID},
			})
			require.Error(t, err)
		})
	}
}

func TestSecurity_InvalidProviderUUID(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Try to create lease referencing SKU with invalid UUID
	_, err = msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: "invalid-sku-uuid", Quantity: 1},
		},
	})
	require.Error(t, err)
}

func TestSecurity_NonexistentSKU(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Try to create lease with nonexistent SKU (valid UUID format)
	_, err = msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: "01912345-6789-7abc-8def-999999999999", Quantity: 1},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSecurity_ZeroQuantityLease(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Try to create lease with zero quantity
	_, err = msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items: []types.LeaseItemInput{
			{SkuUuid: sku.Uuid, Quantity: 0},
		},
	})
	require.Error(t, err)
}

func TestSecurity_EmptyLeaseItems(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Try to create lease with no items
	_, err = msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{},
	})
	require.Error(t, err)
}

// ============================================================================
// Provider Authorization Tests
// ============================================================================

// Note: Provider and SKU authorization tests are covered in the SKU module tests.
// The billing module relies on the SKU module for provider/SKU ownership validation.

// ============================================================================
// Lease State Transition Tests
// ============================================================================

func TestSecurity_DoubleAcknowledgement(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create and acknowledge a lease
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	// Try to acknowledge again (should fail)
	_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{leaseID},
	})
	require.Error(t, err)
}

func TestSecurity_CloseAlreadyClosedLease(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create and acknowledge a lease
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	// Close the lease
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)

	// Try to close again (should fail)
	_, err = msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseID},
	})
	require.Error(t, err)
}

func TestSecurity_WithdrawFromClosedLease(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund tenant credit account
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(10000))))

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create and acknowledge a lease
	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	// Advance time
	newCtx := f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
	f.Ctx = newCtx

	// Close the lease: the close settles the 100 seconds it ran.
	closeResp, err := msgServer.CloseLease(f.Ctx, &types.MsgCloseLease{
		Sender:     tenant.String(),
		LeaseUuids: []string{leaseID},
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100))), closeResp.TotalSettledAmounts)

	// The closed lease left its stream, which is gone with its last lease.
	_, err = f.App.BillingKeeper.Streams.Get(f.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
	require.ErrorIs(t, err, collections.ErrNotFound)

	// However much later the provider withdraws, a closed lease pays nothing more.
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))
	resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:       providerAddr.String(),
		ProviderUuid: provider.Uuid,
	})
	require.NoError(t, err)
	require.True(t, resp.TotalAmounts.IsZero())
	require.Zero(t, resp.WithdrawalCount)
	require.Equal(t, sdkmath.NewInt(100), f.App.BankKeeper.GetBalance(f.Ctx, providerAddr, testDenom).Amount)
	require.Equal(t, sdkmath.NewInt(9900), f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, testDenom).Amount)

	// The removed per-lease withdrawal cannot be used to reach it either.
	_, err = msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{leaseID}, //nolint:staticcheck // exercising the deprecated field
	})
	require.ErrorIs(t, err, types.ErrInvalidRequest)
	requireInvariants(t, f)
}

// ============================================================================
// Denom-Spam Resistance Tests
// ============================================================================

// TestSecurity_DenomSpamDoesNotAffectSettlement verifies that dust tokens
// sent to a credit address by third parties do not leak into settlement
// transfers.
func TestSecurity_DenomSpamDoesNotAffectSettlement(t *testing.T) {
	f := initFixture(t)

	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	// Create provider and SKU (priced in umfx)
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600) // 3600 umfx/hour

	// Fund tenant credit account with the real denom
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))

	// Spam the credit address with many dust denoms (simulating attacker)
	dustDenoms := []string{"dust1", "dust2", "dust3", "dust4", "dust5"}
	for _, denom := range dustDenoms {
		f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(1))))
	}

	// Verify dust is actually on the credit address
	allBalances := f.App.BankKeeper.GetAllBalances(f.Ctx, creditAddr)
	require.Equal(t, len(dustDenoms)+1, len(allBalances), "credit address should have real denom + dust denoms")

	// Create credit account
	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create and acknowledge a lease
	f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	// Dust never creates a stream: the tenant has exactly one, in the lease's denom.
	streams, err := f.App.BillingKeeper.GetAllStreams(f.Ctx)
	require.NoError(t, err)
	require.Len(t, streams, 1)
	require.Equal(t, testDenom, streams[0].Denom)

	// Advance time so there's something to settle
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(100 * time.Second))

	// Withdraw (triggers settlement)
	resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:       providerAddr.String(),
		ProviderUuid: provider.Uuid,
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(100))), resp.TotalAmounts)
	require.Equal(t, uint64(1), resp.WithdrawalCount)

	// Verify: provider received ONLY the lease denom, not dust
	for _, denom := range dustDenoms {
		providerBal := f.App.BankKeeper.GetBalance(f.Ctx, sdk.MustAccAddressFromBech32(provider.PayoutAddress), denom)
		require.True(t, providerBal.IsZero(), "provider should not receive dust denom %s", denom)
	}

	// Verify: dust is still on the credit address (untouched)
	for _, denom := range dustDenoms {
		creditBal := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, denom)
		require.Equal(t, sdkmath.NewInt(1), creditBal.Amount, "dust denom %s should remain on credit address", denom)
	}

	// Verify: the lease's real denom was settled correctly
	providerBal := f.App.BankKeeper.GetBalance(f.Ctx, sdk.MustAccAddressFromBech32(provider.PayoutAddress), testDenom)
	require.Equal(t, sdkmath.NewInt(100), providerBal.Amount, "provider should have received settlement in %s", testDenom)
	requireInvariants(t, f)
}

// TestSecurity_DenomSpamDoesNotAffectAutoClose verifies that dust tokens
// on a credit address do not interfere with the credit-exhaustion decision:
// the stream freezes on its own denom's balance and the EndBlocker closes it.
func TestSecurity_DenomSpamDoesNotAffectAutoClose(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund credit account with exactly the lease's reservation (one hour at 1/s)
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(3600))))

	// Spam dust denoms
	spamDenoms := []string{"spam1", "spam2", "spam3"}
	for _, denom := range spamDenoms {
		f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(999_999_999))))
	}

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	leaseID := f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	// Advance time one second past the point where the real denom is exhausted
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(3601 * time.Second))

	// The stream must freeze based on the lease's denom (umfx), not be confused
	// by the large balances in spam denoms.
	resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:       providerAddr.String(),
		ProviderUuid: provider.Uuid,
	})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(3600))), resp.TotalAmounts)

	state, err := f.App.BillingKeeper.Streams.Get(f.Ctx, keeper.NewStreamKey(provider.Uuid, tenant, testDenom))
	require.NoError(t, err)
	require.True(t, state.IsExhausted(), "stream should freeze: real denom exhausted despite large dust balances")
	requireInvariants(t, f)

	// The EndBlocker closes the lease for exhausted credit.
	require.NoError(t, f.App.BillingKeeper.EndBlocker(f.Ctx))
	lease, err := f.App.BillingKeeper.GetLease(f.Ctx, leaseID)
	require.NoError(t, err)
	require.Equal(t, types.LEASE_STATE_CLOSED, lease.State)
	require.Equal(t, types.ClosureReasonCreditExhausted, lease.ClosureReason)

	// The spam balances were neither spent nor moved.
	for _, denom := range spamDenoms {
		require.Equal(t, sdkmath.NewInt(999_999_999), f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, denom).Amount)
		require.True(t, f.App.BankKeeper.GetBalance(f.Ctx, providerAddr, denom).IsZero())
	}
	requireInvariants(t, f)
}

// TestSecurity_DenomSpamDoesNotLeakOnOverflow verifies that when accrual
// overflows (duration > ~100 years) and the settlement transfers all
// remaining credit, only the stream's denom is transferred — not dust.
func TestSecurity_DenomSpamDoesNotLeakOnOverflow(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]

	// Create provider and SKU
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	// Fund credit account with the real denom
	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(5000))))

	// Spam dust denoms with large balances
	dustDenoms := []string{"overflow_dust1", "overflow_dust2", "overflow_dust3"}
	for _, denom := range dustDenoms {
		f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(999_999_999))))
	}

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	f.createAndAcknowledgeLease(t, msgServer, tenant, providerAddr, []types.LeaseItemInput{
		{SkuUuid: sku.Uuid, Quantity: 1},
	})

	// Settle more than 100 years later to trigger the accrual overflow guard.
	// The settlement handles it by transferring all remaining credit rather
	// than returning an error.
	f.Ctx = f.Ctx.WithBlockTime(f.Ctx.BlockTime().Add(101 * 365 * 24 * time.Hour))
	resp, err := msgServer.Withdraw(f.Ctx, &types.MsgWithdraw{
		Sender:       providerAddr.String(),
		ProviderUuid: provider.Uuid,
	})
	require.NoError(t, err)

	// Verify: only the lease's denom was transferred, not dust
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(5000))), resp.TotalAmounts,
		"overflow settlement should transfer all remaining credit in the lease's denom and nothing else")

	// Verify: provider received none of the dust denoms
	payoutAddr := sdk.MustAccAddressFromBech32(provider.PayoutAddress)
	for _, denom := range dustDenoms {
		bal := f.App.BankKeeper.GetBalance(f.Ctx, payoutAddr, denom)
		require.True(t, bal.IsZero(), "provider should not receive dust denom %s on overflow", denom)
	}

	// Verify: dust is still on the credit address
	for _, denom := range dustDenoms {
		bal := f.App.BankKeeper.GetBalance(f.Ctx, creditAddr, denom)
		require.Equal(t, sdkmath.NewInt(999_999_999), bal.Amount,
			"dust denom %s should remain untouched on credit address after overflow settlement", denom)
	}
}

// TestSecurity_DenomSpamDoesNotAffectCreditAccountQuery verifies that the CreditAccount
// query uses per-denom balance fetching rather than GetAllBalances, so denom-spam on the
// credit address does not cause the query to load thousands of irrelevant balances.
func TestSecurity_DenomSpamDoesNotAffectCreditAccountQuery(t *testing.T) {
	f := initFixture(t)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)

	// Fund credit account with the real denom
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create and acknowledge a lease so there's an active lease with testDenom
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)

	_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{createResp.LeaseUuid},
	})
	require.NoError(t, err)

	// Spam the credit address with 50 different dust denoms
	dustDenoms := make([]string, 50)
	for i := range dustDenoms {
		dustDenoms[i] = fmt.Sprintf("dust%03d", i)
		f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(dustDenoms[i], sdkmath.NewInt(1))))
	}

	// Query CreditAccount — should only return the relevant denom (testDenom), not dust
	querier := keeper.NewQuerier(f.App.BillingKeeper)
	resp, err := querier.CreditAccount(f.Ctx, &types.QueryCreditAccountRequest{
		Tenant: tenant.String(),
	})
	require.NoError(t, err)

	// The response should contain testDenom but NOT any dust denoms
	require.True(t, resp.Balances.AmountOf(testDenom).IsPositive(),
		"should contain the real denom balance")
	for _, denom := range dustDenoms {
		require.True(t, resp.Balances.AmountOf(denom).IsZero(),
			"dust denom %s should not appear in CreditAccount query response", denom)
	}
}

// TestSecurity_DenomSpamDoesNotAffectCreditEstimateQuery verifies that the CreditEstimate
// query uses per-denom balance fetching rather than GetAllBalances.
func TestSecurity_DenomSpamDoesNotAffectCreditEstimateQuery(t *testing.T) {
	f := initFixture(t)

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	provider := f.createTestProvider(t, providerAddr.String(), providerAddr.String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)

	// Fund credit account
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(testDenom, sdkmath.NewInt(1_000_000))))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Create and acknowledge a lease
	msgServer := keeper.NewMsgServerImpl(f.App.BillingKeeper)
	createResp, err := msgServer.CreateLease(f.Ctx, &types.MsgCreateLease{
		Tenant: tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}},
	})
	require.NoError(t, err)

	_, err = msgServer.AcknowledgeLease(f.Ctx, &types.MsgAcknowledgeLease{
		Sender:     providerAddr.String(),
		LeaseUuids: []string{createResp.LeaseUuid},
	})
	require.NoError(t, err)

	// Spam the credit address with 50 different dust denoms
	for i := range 50 {
		denom := fmt.Sprintf("dust%03d", i)
		f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(1))))
	}

	// Query CreditEstimate — should only consider testDenom, not dust
	querier := keeper.NewQuerier(f.App.BillingKeeper)
	resp, err := querier.CreditEstimate(f.Ctx, &types.QueryCreditEstimateRequest{
		Tenant: tenant.String(),
	})
	require.NoError(t, err)

	// The response should contain testDenom balance but NOT dust
	require.True(t, resp.CurrentBalance.AmountOf(testDenom).IsPositive(),
		"should contain the real denom balance")
	for i := range 50 {
		denom := fmt.Sprintf("dust%03d", i)
		require.True(t, resp.CurrentBalance.AmountOf(denom).IsZero(),
			"dust denom %s should not appear in CreditEstimate response", denom)
	}

	// Estimate should be valid (non-zero duration since we have credit)
	require.Greater(t, resp.EstimatedDurationSeconds, uint64(0),
		"should have a positive estimated duration")
	require.Equal(t, uint64(1), resp.ActiveLeaseCount)
}

// TestSecurity_CreditAccountPreLeaseFallbackWithSpam verifies the CreditAccount query
// fallback path (pre-lease accounts) returns the funded balance and does not panic or
// return incorrect data when the credit address has been spammed with dust denoms.
func TestSecurity_CreditAccountPreLeaseFallbackWithSpam(t *testing.T) {
	f := initFixture(t)

	tenant := f.TestAccs[0]

	creditAddr, err := types.DeriveCreditAddressFromBech32(tenant.String())
	require.NoError(t, err)

	// Fund credit account with the real denom
	realAmount := sdk.NewCoin(testDenom, sdkmath.NewInt(500_000))
	f.fundAccount(t, creditAddr, sdk.NewCoins(realAmount))

	err = f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	})
	require.NoError(t, err)

	// Spam the credit address with dust BEFORE any lease is created.
	// This exercises the GetAllBalances fallback since there are no relevant denoms.
	for i := range 50 {
		denom := fmt.Sprintf("spam%03d", i)
		f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewCoin(denom, sdkmath.NewInt(1))))
	}

	querier := keeper.NewQuerier(f.App.BillingKeeper)
	resp, err := querier.CreditAccount(f.Ctx, &types.QueryCreditAccountRequest{
		Tenant: tenant.String(),
	})
	require.NoError(t, err)

	// The fallback returns all balances (including dust), but the real denom must be present
	require.True(t, resp.Balances.AmountOf(testDenom).Equal(sdkmath.NewInt(500_000)),
		"pre-lease fallback must return the funded balance")
	// Available balances should equal balances when there are no reservations
	require.True(t, resp.AvailableBalances.AmountOf(testDenom).Equal(sdkmath.NewInt(500_000)),
		"available balance should match total balance with no reservations")
}
