package keeper_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
	skukeeper "github.com/manifest-network/manifest-ledger/x/sku/keeper"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

func TestPendingCustomDomainClaimsRespectCurrentHardDeadline(t *testing.T) {
	for _, timeout := range []uint64{types.MinPendingTimeout, types.DefaultPendingTimeout} {
		t.Run(strconv.FormatUint(timeout, 10)+"s", func(t *testing.T) {
			s := setupCustomDomain(t)
			created, err := s.msgServer.CreateLease(s.f.Ctx, &types.MsgCreateLease{Tenant: s.tenant.String(), Items: []types.LeaseItemInput{{SkuUuid: s.sku.Uuid, Quantity: 1}}})
			require.NoError(t, err)
			k := s.f.App.BillingKeeper
			// A parameter change applies immediately, just as acknowledgement's
			// gate uses the current timeout rather than a creation-time snapshot.
			params, err := k.GetParams(s.f.Ctx)
			require.NoError(t, err)
			params.PendingTimeout = timeout
			require.NoError(t, k.SetParams(s.f.Ctx, params))
			deadline := params.PendingLeaseDeadline(s.f.Ctx.BlockTime())
			atDeadline := s.f.Ctx.WithBlockTime(deadline)
			_, err = k.SetItemCustomDomain(atDeadline, s.tenant.String(), created.LeaseUuid, "", "deadline.example.com")
			require.NoError(t, err, "the hard deadline has an inclusive activation boundary")
			afterDeadline := atDeadline.WithBlockTime(deadline.Add(time.Nanosecond)).WithEventManager(sdk.NewEventManager())
			for _, domain := range []string{"new.example.com", "deadline.example.com"} {
				_, err = k.SetItemCustomDomain(afterDeadline, s.tenant.String(), created.LeaseUuid, "", domain)
				require.ErrorIs(t, err, types.ErrLeaseAcknowledgementDeadlineExceeded)
			}
			require.Empty(t, afterDeadline.EventManager().Events())
			lease, err := k.GetLease(afterDeadline, created.LeaseUuid)
			require.NoError(t, err)
			require.Equal(t, "deadline.example.com", lease.Items[0].CustomDomain)
			_, _, exists, err := k.GetLeaseByCustomDomain(afterDeadline, "new.example.com")
			require.NoError(t, err)
			require.False(t, exists)
			// Cleanup can lag; explicitly releasing the existing claim remains
			// available without waiting for EndBlocker to expire this lease.
			_, err = k.SetItemCustomDomain(afterDeadline, s.tenant.String(), created.LeaseUuid, "", "")
			require.NoError(t, err)
			_, _, exists, err = k.GetLeaseByCustomDomain(afterDeadline, "deadline.example.com")
			require.NoError(t, err)
			require.False(t, exists)
			// ACTIVE leases do not use the acknowledgement deadline for edits.
			_, err = k.SetItemCustomDomain(afterDeadline, s.tenant.String(), s.leaseUUID, "", "active.example.com")
			require.NoError(t, err)
		})
	}
}

func TestPendingOfferSurvivesCatalogDeactivation(t *testing.T) {
	for _, deactivateProvider := range []bool{false, true} {
		name := "sku"
		if deactivateProvider {
			name = "provider and sku"
		}
		t.Run(name, func(t *testing.T) {
			s := setupCustomDomain(t)
			created, err := s.msgServer.CreateLease(s.f.Ctx, &types.MsgCreateLease{Tenant: s.tenant.String(), Items: []types.LeaseItemInput{{SkuUuid: s.sku.Uuid, Quantity: 1}}})
			require.NoError(t, err)
			skuServer := skukeeper.NewMsgServerImpl(s.f.App.SKUKeeper)
			if deactivateProvider {
				_, err = skuServer.DeactivateProvider(s.f.Ctx, &skutypes.MsgDeactivateProvider{Authority: s.f.App.SKUKeeper.GetAuthority(), Uuid: s.provider.Uuid})
			} else {
				_, err = skuServer.DeactivateSKU(s.f.Ctx, &skutypes.MsgDeactivateSKU{Authority: s.f.App.SKUKeeper.GetAuthority(), Uuid: s.sku.Uuid})
			}
			require.NoError(t, err)
			_, err = s.msgServer.CreateLease(s.f.Ctx, &types.MsgCreateLease{Tenant: s.tenant.String(), Items: []types.LeaseItemInput{{SkuUuid: s.sku.Uuid, Quantity: 1}}})
			require.Error(t, err, "deactivation blocks new admission")
			acknowledged, err := s.msgServer.AcknowledgeLease(s.f.Ctx, &types.MsgAcknowledgeLease{Sender: s.providerAddr.String(), LeaseUuids: []string{created.LeaseUuid}})
			require.NoError(t, err, "an already-admitted pending offer keeps its locked catalog terms")
			require.Equal(t, uint64(1), acknowledged.AcknowledgedCount)
			lease, err := s.f.App.BillingKeeper.GetLease(s.f.Ctx, created.LeaseUuid)
			require.NoError(t, err)
			require.Equal(t, types.LEASE_STATE_ACTIVE, lease.State)
		})
	}
}
