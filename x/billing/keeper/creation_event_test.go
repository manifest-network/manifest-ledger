package keeper_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestLeaseCreatedEventIdentifiesCanonicalSignerAndRole(t *testing.T) {
	for _, role := range []string{types.AttributeValueRoleTenant, types.AttributeValueRoleAuthority, types.AttributeValueRoleAllowed} {
		t.Run(role, func(t *testing.T) {
			f := initFixture(t)
			tenant, providerAddress, allowed := f.TestAccs[0], f.TestAccs[1], f.TestAccs[2]
			params := types.DefaultParams()
			params.AllowedList = []string{allowed.String()}
			require.NoError(t, f.App.BillingKeeper.SetParams(f.Ctx, params))
			provider := f.createTestProvider(t, providerAddress.String(), providerAddress.String())
			sku := f.createTestSKU(t, provider.Uuid, 3_600)
			f.fundAccount(t, tenant, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 3_600)))
			ms := keeper.NewMsgServerImpl(f.App.BillingKeeper)
			_, err := ms.FundCredit(f.Ctx, &types.MsgFundCredit{Sender: tenant.String(), Tenant: tenant.String(), Amount: sdk.NewInt64Coin(testDenom, 3_600)})
			require.NoError(t, err)
			f.Ctx = f.Ctx.WithEventManager(sdk.NewEventManager())
			items := []types.LeaseItemInput{{SkuUuid: sku.Uuid, Quantity: 1}}
			signer := tenant
			if role == types.AttributeValueRoleTenant {
				_, err = ms.CreateLease(f.Ctx, &types.MsgCreateLease{Tenant: strings.ToUpper(tenant.String()), Items: items})
			} else {
				signer = f.Authority
				if role == types.AttributeValueRoleAllowed {
					signer = allowed
				}
				_, err = ms.CreateLeaseForTenant(f.Ctx, &types.MsgCreateLeaseForTenant{Authority: strings.ToUpper(signer.String()), Tenant: strings.ToUpper(tenant.String()), Items: items})
			}
			require.NoError(t, err)
			event := findEvent(t, f.Ctx, types.EventTypeLeaseCreated)
			require.Equal(t, signer.String(), attrValue(t, event, types.AttributeKeySender))
			require.Equal(t, tenant.String(), attrValue(t, event, types.AttributeKeyTenant))
			require.Equal(t, role, attrValue(t, event, types.AttributeKeyCreatedBy))
		})
	}
}
