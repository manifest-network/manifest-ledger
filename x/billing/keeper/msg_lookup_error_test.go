package keeper_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"

	sdkcodec "github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

type billingLookupFixture struct {
	f            *testFixture
	msgServer    types.MsgServer
	tenant       sdk.AccAddress
	providerAddr sdk.AccAddress
	provider     skutypes.Provider
	sku          skutypes.SKU
}

func newBillingLookupFixture(t *testing.T) billingLookupFixture {
	t.Helper()
	f := initFixture(t)
	params := types.DefaultParams()
	params.MinLeaseDuration = 10
	require.NoError(t, f.App.BillingKeeper.SetParams(f.Ctx, params))

	tenant := f.TestAccs[0]
	providerAddr := f.TestAccs[1]
	provider := f.createTestProvider(t, providerAddr.String(), f.TestAccs[2].String())
	sku := f.createTestSKU(t, provider.Uuid, 3600)
	creditAddr := types.DeriveCreditAddress(tenant)
	f.fundAccount(t, creditAddr, sdk.NewCoins(sdk.NewInt64Coin(testDenom, 1_000_000)))
	require.NoError(t, f.App.BillingKeeper.SetCreditAccount(f.Ctx, types.CreditAccount{
		Tenant:        tenant.String(),
		CreditAddress: creditAddr.String(),
	}))

	return billingLookupFixture{
		f:            f,
		msgServer:    keeper.NewMsgServerImpl(f.App.BillingKeeper),
		tenant:       tenant,
		providerAddr: providerAddr,
		provider:     provider,
		sku:          sku,
	}
}

func (s billingLookupFixture) createPendingLease(t *testing.T) string {
	t.Helper()
	response, err := s.msgServer.CreateLease(s.f.Ctx, &types.MsgCreateLease{
		Tenant: s.tenant.String(),
		Items: []types.LeaseItemInput{{
			SkuUuid:  s.sku.Uuid,
			Quantity: 1,
		}},
	})
	require.NoError(t, err)
	return response.LeaseUuid
}

func (s billingLookupFixture) createActiveLease(t *testing.T) string {
	t.Helper()
	return s.f.createAndAcknowledgeLease(
		t,
		s.msgServer,
		s.tenant,
		s.providerAddr,
		[]types.LeaseItemInput{{SkuUuid: s.sku.Uuid, Quantity: 1}},
	)
}

func TestCreateLeaseDoesNotMaskCorruptLookupsAsNotFound(t *testing.T) {
	tests := []struct {
		name     string
		corrupt  func(testing.TB, billingLookupFixture)
		notFound error
	}{
		{
			name: "credit account",
			corrupt: func(t testing.TB, s billingLookupFixture) {
				corruptBillingCreditAccount(t, s, s.tenant)
			},
			notFound: types.ErrCreditAccountNotFound,
		},
		{
			name: "SKU",
			corrupt: func(t testing.TB, s billingLookupFixture) {
				corruptStringPrimaryRecordInStore(t, s.f, skutypes.StoreKey, skutypes.SKUKey.Bytes(), s.sku.Uuid)
			},
			notFound: types.ErrSKUNotFound,
		},
		{
			name: "provider",
			corrupt: func(t testing.TB, s billingLookupFixture) {
				corruptStringPrimaryRecordInStore(t, s.f, skutypes.StoreKey, skutypes.ProviderKey.Bytes(), s.provider.Uuid)
			},
			notFound: types.ErrProviderNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := newBillingLookupFixture(t)
			test.corrupt(t, s)

			_, err := s.msgServer.CreateLease(s.f.Ctx, &types.MsgCreateLease{
				Tenant: s.tenant.String(),
				Items:  []types.LeaseItemInput{{SkuUuid: s.sku.Uuid, Quantity: 1}},
			})
			require.Error(t, err)
			require.NotErrorIs(t, err, test.notFound)
		})
	}
}

func TestCreateLeaseDoesNotMaskInvalidStoredPricingAsMissingSKU(t *testing.T) {
	s := newBillingLookupFixture(t)
	invalidSKU := s.sku
	invalidSKU.Unit = skutypes.Unit_UNIT_UNSPECIFIED
	require.NoError(t, s.f.App.SKUKeeper.SetSKU(s.f.Ctx, invalidSKU))

	_, err := s.msgServer.CreateLease(s.f.Ctx, &types.MsgCreateLease{
		Tenant: s.tenant.String(),
		Items:  []types.LeaseItemInput{{SkuUuid: invalidSKU.Uuid, Quantity: 1}},
	})
	require.Error(t, err)
	require.NotErrorIs(t, err, types.ErrSKUNotFound)
}

func TestLeaseMessagesDoNotMaskCorruptLeaseAsNotFound(t *testing.T) {
	tests := []struct {
		name string
		call func(billingLookupFixture, string) error
	}{
		{
			name: "close",
			call: func(s billingLookupFixture, leaseUUID string) error {
				_, err := s.msgServer.CloseLease(s.f.Ctx, &types.MsgCloseLease{
					Sender:     s.tenant.String(),
					LeaseUuids: []string{leaseUUID},
					Reason:     "test close",
				})
				return err
			},
		},
		{
			name: "acknowledge",
			call: func(s billingLookupFixture, leaseUUID string) error {
				_, err := s.msgServer.AcknowledgeLease(s.f.Ctx, &types.MsgAcknowledgeLease{
					Sender:     s.providerAddr.String(),
					LeaseUuids: []string{leaseUUID},
				})
				return err
			},
		},
		{
			name: "reject",
			call: func(s billingLookupFixture, leaseUUID string) error {
				_, err := s.msgServer.RejectLease(s.f.Ctx, &types.MsgRejectLease{
					Sender:     s.providerAddr.String(),
					LeaseUuids: []string{leaseUUID},
					Reason:     "test rejection",
				})
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := newBillingLookupFixture(t)
			leaseUUID := s.createPendingLease(t)
			corruptStringPrimaryRecordInStore(t, s.f, types.StoreKey, types.LeaseKey.Bytes(), leaseUUID)

			err := test.call(s, leaseUUID)
			require.Error(t, err)
			require.NotErrorIs(t, err, types.ErrLeaseNotFound)
		})
	}
}

func TestLeaseMessagesDoNotMaskCorruptCreditAccountAsNotFound(t *testing.T) {
	t.Run("acknowledge batch validation", func(t *testing.T) {
		s := newBillingLookupFixture(t)
		leaseUUID := s.createPendingLease(t)
		corruptBillingCreditAccount(t, s, s.tenant)

		_, err := s.msgServer.AcknowledgeLease(s.f.Ctx, &types.MsgAcknowledgeLease{
			Sender:     s.providerAddr.String(),
			LeaseUuids: []string{leaseUUID},
		})
		require.Error(t, err)
		require.NotErrorIs(t, err, types.ErrCreditAccountNotFound)
	})

	t.Run("close", func(t *testing.T) {
		s := newBillingLookupFixture(t)
		leaseUUID := s.createActiveLease(t)
		corruptBillingCreditAccount(t, s, s.tenant)

		_, err := s.msgServer.CloseLease(s.f.Ctx, &types.MsgCloseLease{
			Sender:     s.tenant.String(),
			LeaseUuids: []string{leaseUUID},
			Reason:     "test close",
		})
		require.Error(t, err)
		require.NotErrorIs(t, err, types.ErrCreditAccountNotFound)
	})

	t.Run("cancel", func(t *testing.T) {
		s := newBillingLookupFixture(t)
		leaseUUID := s.createPendingLease(t)
		corruptBillingCreditAccount(t, s, s.tenant)

		_, err := s.msgServer.CancelLease(s.f.Ctx, &types.MsgCancelLease{
			Tenant:     s.tenant.String(),
			LeaseUuids: []string{leaseUUID},
		})
		require.Error(t, err)
		require.NotErrorIs(t, err, types.ErrCreditAccountNotFound)
	})
}

func TestLeaseMessagesDoNotMaskCorruptProviderAsNotFound(t *testing.T) {
	t.Run("provider authorization", func(t *testing.T) {
		s := newBillingLookupFixture(t)
		leaseUUID := s.createPendingLease(t)
		corruptStringPrimaryRecordInStore(t, s.f, skutypes.StoreKey, skutypes.ProviderKey.Bytes(), s.provider.Uuid)

		_, err := s.msgServer.AcknowledgeLease(s.f.Ctx, &types.MsgAcknowledgeLease{
			Sender:     s.providerAddr.String(),
			LeaseUuids: []string{leaseUUID},
		})
		require.Error(t, err)
		require.NotErrorIs(t, err, types.ErrProviderNotFound)
	})

	t.Run("settlement", func(t *testing.T) {
		s := newBillingLookupFixture(t)
		leaseUUID := s.createActiveLease(t)
		corruptStringPrimaryRecordInStore(t, s.f, skutypes.StoreKey, skutypes.ProviderKey.Bytes(), s.provider.Uuid)
		settleCtx := s.f.Ctx.WithBlockTime(s.f.Ctx.BlockTime().Add(time.Second)).
			WithEventManager(sdk.NewEventManager())

		_, err := s.msgServer.CloseLease(settleCtx, &types.MsgCloseLease{
			Sender:     s.tenant.String(),
			LeaseUuids: []string{leaseUUID},
			Reason:     "test close",
		})
		require.Error(t, err)
		require.NotErrorIs(t, err, types.ErrProviderNotFound)
	})

	t.Run("legacy provider with invalid payout address", func(t *testing.T) {
		s := newBillingLookupFixture(t)
		leaseUUID := s.createActiveLease(t)
		provider := s.provider
		provider.PayoutAddress = "not-a-bech32-address"
		legacy, err := sdkcodec.CollValue[skutypes.Provider](s.f.EncodingCfg.Codec).Encode(provider)
		require.NoError(t, err)
		key, err := collections.EncodeKeyWithPrefix(
			skutypes.ProviderKey.Bytes(),
			collections.StringKey,
			provider.Uuid,
		)
		require.NoError(t, err)
		s.f.Ctx.KVStore(s.f.App.GetKey(skutypes.StoreKey)).Set(key, legacy)

		settleCtx := s.f.Ctx.WithBlockTime(s.f.Ctx.BlockTime().Add(time.Second)).
			WithEventManager(sdk.NewEventManager())
		_, err = s.msgServer.CloseLease(settleCtx, &types.MsgCloseLease{
			Sender:     s.tenant.String(),
			LeaseUuids: []string{leaseUUID},
			Reason:     "test close",
		})
		require.Error(t, err)
		require.NotErrorIs(t, err, types.ErrProviderNotFound)
	})
}

func corruptStringPrimaryRecordInStore(
	t testing.TB,
	f *testFixture,
	storeKey string,
	prefix []byte,
	uuid string,
) {
	t.Helper()
	key, err := collections.EncodeKeyWithPrefix(prefix, collections.StringKey, uuid)
	require.NoError(t, err)
	f.Ctx.KVStore(f.App.GetKey(storeKey)).Set(key, []byte{0xff})
}

func corruptBillingCreditAccount(t testing.TB, s billingLookupFixture, tenant sdk.AccAddress) {
	t.Helper()
	key, err := collections.EncodeKeyWithPrefix(
		types.CreditAccountKey.Bytes(),
		sdk.AccAddressKey,
		tenant,
	)
	require.NoError(t, err)
	s.f.Ctx.KVStore(s.f.App.GetKey(types.StoreKey)).Set(key, []byte{0xff})
}
