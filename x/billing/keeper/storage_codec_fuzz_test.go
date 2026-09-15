package keeper

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	collcodec "cosmossdk.io/collections/codec"

	sdkcodec "github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"

	appparams "github.com/manifest-network/manifest-ledger/app/params"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

// Exercise real versioned and legacy protobuf decoders, including truncated
// version tags and invalid address encodings. Accepted values must have a stable
// canonical storage encoding; malformed values may return errors but never panic.
func FuzzBillingStorageValueCodecs(f *testing.F) {
	appparams.SetAddressPrefixes()
	cfg := moduletestutil.MakeTestEncodingConfig()
	tenant := sdk.AccAddress(bytes.Repeat([]byte{0x44}, 20))
	params := types.DefaultParams()
	params.AllowedList = []string{tenant.String()}
	lease := types.Lease{
		Tenant: tenant.String(),
		Reservation: &types.LeaseReservation{
			RemainingAmounts: sdk.NewCoins(sdk.NewInt64Coin("umfx", 7)),
		},
	}
	account := types.CreditAccount{
		Tenant: tenant.String(), CreditAddress: types.DeriveCreditAddress(tenant).String(),
		ReservedAmounts: sdk.NewCoins(sdk.NewInt64Coin("umfx", 7)),
	}
	paramsCodec, leaseCodec, accountCodec := newParamsValueCodec(cfg.Codec), newLeaseValueCodec(cfg.Codec), newCreditAccountValueCodec(cfg.Codec)
	seedStorageCodec(f, 0, paramsCodec, sdkcodec.CollValue[types.Params](cfg.Codec), params)
	seedStorageCodec(f, 1, leaseCodec, sdkcodec.CollValue[types.Lease](cfg.Codec), lease)
	seedStorageCodec(f, 2, accountCodec, sdkcodec.CollValue[types.CreditAccount](cfg.Codec), account)
	for kind, prefix := range []string{paramsStoragePrefix, leaseStoragePrefix, creditAccountStoragePrefix} {
		f.Add(uint8(kind), []byte(prefix))
		f.Add(uint8(kind), append([]byte(prefix), 0xff))
		f.Add(uint8(kind), []byte("\x00billing/unknown/v9"))
	}
	f.Fuzz(func(t *testing.T, kind uint8, encoded []byte) {
		if len(encoded) > 32*1024 {
			t.Skip()
		}
		switch kind % 3 {
		case 0:
			checkStorageCodecFixedPoint(t, paramsCodec, encoded)
		case 1:
			checkStorageCodecFixedPoint(t, leaseCodec, encoded)
		case 2:
			checkStorageCodecFixedPoint(t, accountCodec, encoded)
		}
	})
}

func seedStorageCodec[T any](f *testing.F, kind uint8, current, legacy collcodec.ValueCodec[T], value T) {
	f.Helper()
	for _, codec := range []collcodec.ValueCodec[T]{current, legacy} {
		encoded, err := codec.Encode(value)
		require.NoError(f, err)
		f.Add(kind, encoded)
	}
}

func checkStorageCodecFixedPoint[T any](t *testing.T, codec collcodec.ValueCodec[T], encoded []byte) {
	t.Helper()
	snapshot := bytes.Clone(encoded)
	value, err := codec.Decode(encoded)
	require.Equal(t, snapshot, encoded, "decoding must not mutate its input")
	if err != nil {
		return
	}
	canonical, err := codec.Encode(value)
	require.NoError(t, err)
	roundTrip, err := codec.Decode(canonical)
	require.NoError(t, err)
	reencoded, err := codec.Encode(roundTrip)
	require.NoError(t, err)
	require.Equal(t, canonical, reencoded)
}
