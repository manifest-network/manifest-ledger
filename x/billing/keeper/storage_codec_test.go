package keeper

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdkcodec "github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"

	appparams "github.com/manifest-network/manifest-ledger/app/params"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestStorageValueCodecsPersistRawAddressBytes(t *testing.T) {
	appparams.SetAddressPrefixes()
	cfg := moduletestutil.MakeTestEncodingConfig()
	tenant := sdk.AccAddress(bytes.Repeat([]byte{0x11}, 20))
	allowed := sdk.AccAddress(bytes.Repeat([]byte{0x22}, 20))
	credit := types.DeriveCreditAddress(tenant)
	now := time.Unix(1_700_000_000, 0).UTC()

	params := types.DefaultParams()
	params.AllowedList = []string{allowed.String()}
	lease := types.Lease{
		Uuid:         "01912345-6789-7abc-8def-0123456789ab",
		Tenant:       tenant.String(),
		ProviderUuid: "01912345-6789-7abc-8def-0123456789ac",
		Items: []types.LeaseItem{{
			SkuUuid:     "01912345-6789-7abc-8def-0123456789ad",
			Quantity:    2,
			LockedPrice: sdk.NewCoin("umfx", sdkmath.NewInt(7)),
			ServiceName: "web",
		}},
		State:                      types.LEASE_STATE_ACTIVE,
		CreatedAt:                  now,
		LastSettledAt:              now,
		AcknowledgedAt:             &now,
		MetaHash:                   []byte{0xaa, 0xbb},
		MinLeaseDurationAtCreation: 3600,
	}
	account := types.CreditAccount{
		Tenant:            tenant.String(),
		CreditAddress:     credit.String(),
		ActiveLeaseCount:  1,
		PendingLeaseCount: 2,
		ReservedAmounts:   sdk.NewCoins(sdk.NewCoin("umfx", sdkmath.NewInt(50))),
	}

	t.Run("params", func(t *testing.T) {
		codec := newParamsValueCodec(cfg.Codec)
		encoded, err := codec.Encode(params)
		require.NoError(t, err)
		require.True(t, bytes.HasPrefix(encoded, []byte(paramsStoragePrefix)))
		require.True(t, bytes.Contains(encoded, allowed.Bytes()))
		require.False(t, bytes.Contains(encoded, []byte(allowed.String())))

		decoded, err := codec.Decode(encoded)
		require.NoError(t, err)
		require.Equal(t, params, decoded)
	})

	t.Run("lease", func(t *testing.T) {
		codec := newLeaseValueCodec(cfg.Codec)
		encoded, err := codec.Encode(lease)
		require.NoError(t, err)
		require.True(t, bytes.HasPrefix(encoded, []byte(leaseStoragePrefix)))
		require.True(t, bytes.Contains(encoded, tenant.Bytes()))
		require.False(t, bytes.Contains(encoded, []byte(tenant.String())))

		decoded, err := codec.Decode(encoded)
		require.NoError(t, err)
		require.Equal(t, lease, decoded)
	})

	t.Run("credit account", func(t *testing.T) {
		codec := newCreditAccountValueCodec(cfg.Codec)
		encoded, err := codec.Encode(account)
		require.NoError(t, err)
		require.True(t, bytes.HasPrefix(encoded, []byte(creditAccountStoragePrefix)))
		require.True(t, bytes.Contains(encoded, tenant.Bytes()))
		require.True(t, bytes.Contains(encoded, credit.Bytes()))
		require.False(t, bytes.Contains(encoded, []byte(tenant.String())))
		require.False(t, bytes.Contains(encoded, []byte(credit.String())))

		decoded, err := codec.Decode(encoded)
		require.NoError(t, err)
		require.Equal(t, account, decoded)
	})
}

func TestStorageValueCodecsCanonicalizeEquivalentBech32(t *testing.T) {
	appparams.SetAddressPrefixes()
	cfg := moduletestutil.MakeTestEncodingConfig()
	tenant := sdk.AccAddress(bytes.Repeat([]byte{0x33}, 20))
	credit := types.DeriveCreditAddress(tenant)

	assertSameEncoding := func(t *testing.T, lower, upper []byte) {
		t.Helper()
		require.Equal(t, lower, upper)
	}

	paramsCodec := newParamsValueCodec(cfg.Codec)
	lowerParams := types.DefaultParams()
	lowerParams.AllowedList = []string{tenant.String()}
	upperParams := lowerParams
	upperParams.AllowedList = []string{strings.ToUpper(tenant.String())}
	lowerParamsBytes, err := paramsCodec.Encode(lowerParams)
	require.NoError(t, err)
	upperParamsBytes, err := paramsCodec.Encode(upperParams)
	require.NoError(t, err)
	assertSameEncoding(t, lowerParamsBytes, upperParamsBytes)

	leaseCodec := newLeaseValueCodec(cfg.Codec)
	lowerLease := types.Lease{Tenant: tenant.String()}
	upperLease := lowerLease
	upperLease.Tenant = strings.ToUpper(tenant.String())
	lowerLeaseBytes, err := leaseCodec.Encode(lowerLease)
	require.NoError(t, err)
	upperLeaseBytes, err := leaseCodec.Encode(upperLease)
	require.NoError(t, err)
	assertSameEncoding(t, lowerLeaseBytes, upperLeaseBytes)

	accountCodec := newCreditAccountValueCodec(cfg.Codec)
	lowerAccount := types.CreditAccount{Tenant: tenant.String(), CreditAddress: credit.String()}
	upperAccount := types.CreditAccount{
		Tenant:        strings.ToUpper(tenant.String()),
		CreditAddress: strings.ToUpper(credit.String()),
	}
	lowerAccountBytes, err := accountCodec.Encode(lowerAccount)
	require.NoError(t, err)
	upperAccountBytes, err := accountCodec.Encode(upperAccount)
	require.NoError(t, err)
	assertSameEncoding(t, lowerAccountBytes, upperAccountBytes)
}

func TestStorageValueCodecsDecodeLegacyAndRejectCorruptVersionedValues(t *testing.T) {
	appparams.SetAddressPrefixes()
	cfg := moduletestutil.MakeTestEncodingConfig()
	tenant := sdk.AccAddress(bytes.Repeat([]byte{0x44}, 20))
	credit := types.DeriveCreditAddress(tenant)

	t.Run("params", func(t *testing.T) {
		value := types.DefaultParams()
		value.AllowedList = []string{strings.ToUpper(tenant.String())}
		legacy, err := sdkcodec.CollValue[types.Params](cfg.Codec).Encode(value)
		require.NoError(t, err)

		codec := newParamsValueCodec(cfg.Codec)
		decoded, err := codec.Decode(legacy)
		require.NoError(t, err)
		require.Equal(t, []string{tenant.String()}, decoded.AllowedList)
		_, err = codec.Decode(append([]byte(paramsStoragePrefix), 0xff))
		require.Error(t, err)
	})

	t.Run("lease", func(t *testing.T) {
		value := types.Lease{Tenant: strings.ToUpper(tenant.String())}
		legacy, err := sdkcodec.CollValue[types.Lease](cfg.Codec).Encode(value)
		require.NoError(t, err)

		codec := newLeaseValueCodec(cfg.Codec)
		decoded, err := codec.Decode(legacy)
		require.NoError(t, err)
		require.Equal(t, tenant.String(), decoded.Tenant)
		_, err = codec.Decode(append([]byte(leaseStoragePrefix), 0xff))
		require.Error(t, err)
	})

	t.Run("credit account", func(t *testing.T) {
		value := types.CreditAccount{
			Tenant:        strings.ToUpper(tenant.String()),
			CreditAddress: strings.ToUpper(credit.String()),
		}
		legacy, err := sdkcodec.CollValue[types.CreditAccount](cfg.Codec).Encode(value)
		require.NoError(t, err)

		codec := newCreditAccountValueCodec(cfg.Codec)
		decoded, err := codec.Decode(legacy)
		require.NoError(t, err)
		require.Equal(t, tenant.String(), decoded.Tenant)
		require.Equal(t, credit.String(), decoded.CreditAddress)
		_, err = codec.Decode(append([]byte(creditAccountStoragePrefix), 0xff))
		require.Error(t, err)
	})
}
