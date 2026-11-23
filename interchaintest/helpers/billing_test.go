package helpers

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"

	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
)

// TestProviderWithdrawableResponseJSON_Unmarshal reproduces the proto3-JSON CLI
// output for `query billing provider-withdrawable`: uint64 fields (lease_count and
// pagination.total) are emitted as JSON strings. Unmarshaling that into the raw
// proto QueryProviderWithdrawableResponse fails on pagination.total (the SDK's
// query.PageResponse.Total has no `,string` tag), which broke ictest-billing when
// the query moved to PageRequest/PageResponse. This pins the custom-struct fix.
func TestProviderWithdrawableResponseJSON_Unmarshal(t *testing.T) {
	blob := `{"amounts":[{"denom":"upwr","amount":"2000"}],"lease_count":"3","pagination":{"next_key":null,"total":"0"}}`

	var res ProviderWithdrawableResponseJSON
	require.NoError(t, json.Unmarshal([]byte(blob), &res))

	require.Equal(t, uint64(3), res.LeaseCount)
	require.Len(t, res.Amounts, 1)
	require.Equal(t, "upwr", res.Amounts[0].Denom)
	require.Equal(t, "2000", res.Amounts[0].Amount.String())
	require.NotNil(t, res.Pagination)
	require.Equal(t, "0", res.Pagination.Total)
}

// TestStreamsResponseJSON_Unmarshal pins StreamsResponseJSON to what the CLI
// prints for the stream queries. The blob is produced by the same encoder the
// CLI uses (clientCtx.PrintProto), so a change to the Stream proto that the
// e2e helpers cannot decode fails here instead of in a Docker run.
func TestStreamsResponseJSON_Unmarshal(t *testing.T) {
	const (
		providerUUID = "01902a9b-1234-7000-8000-000000000001"
		tenantA      = "manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct"
		tenantB      = "manifest1efd63aw40lxf3n4mhf7dzhjkr453axurm6rp3z"
	)
	settledAt := time.Date(2026, time.November, 20, 10, 11, 12, 130_000_000, time.UTC)
	exhaustedAt := settledAt.Add(90 * time.Second)

	blob, err := codec.ProtoMarshalJSON(&billingtypes.QueryStreamsByProviderResponse{
		Streams: []billingtypes.Stream{
			{
				ProviderUuid: providerUUID, Tenant: tenantA, Denom: "upwr",
				State: billingtypes.StreamState{
					Rate: sdkmath.NewInt(3000), LastSettledAt: settledAt, LeaseCount: 2, Remainder: sdkmath.NewInt(999_999_999),
				},
			},
			{
				ProviderUuid: providerUUID, Tenant: tenantB, Denom: "factory/" + tenantB + "/utest",
				State: billingtypes.StreamState{
					Rate: sdkmath.NewInt(1), LastSettledAt: exhaustedAt, LeaseCount: 1, ExhaustedAt: &exhaustedAt, Remainder: sdkmath.ZeroInt(),
				},
			},
		},
		Pagination: &query.PageResponse{NextKey: []byte("next"), Total: 2},
	}, nil)
	require.NoError(t, err)

	tests := []struct {
		name string
		blob string
	}{
		{name: "provider query", blob: string(blob)},
		// QueryStreamsByTenantResponse has the same shape.
		{name: "without pagination", blob: strings.Replace(string(blob), `"pagination":`, `"ignored":`, 1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var res StreamsResponseJSON
			require.NoError(t, json.Unmarshal([]byte(tc.blob), &res), tc.blob)
			require.Len(t, res.Streams, 2)

			healthy := res.Find(providerUUID, tenantA, "upwr")
			require.NotNil(t, healthy)
			require.Equal(t, "3000", healthy.State.Rate.String())
			require.True(t, settledAt.Equal(healthy.State.LastSettledAt), "last_settled_at %s", healthy.State.LastSettledAt)
			require.Equal(t, uint64(2), healthy.State.LeaseCount)
			require.Equal(t, "999999999", healthy.State.Remainder.String())
			require.False(t, healthy.State.IsExhausted())

			frozen := res.Find(providerUUID, tenantB, "factory/"+tenantB+"/utest")
			require.NotNil(t, frozen)
			require.Equal(t, "1", frozen.State.Rate.String())
			require.Equal(t, uint64(1), frozen.State.LeaseCount)
			require.Equal(t, "0", frozen.State.Remainder.String())
			require.True(t, frozen.State.IsExhausted())
			require.True(t, exhaustedAt.Equal(*frozen.State.ExhaustedAt))

			require.Nil(t, res.Find(providerUUID, tenantA, "umfx"), "a stream is identified by its denom too")
		})
	}

	var paged StreamsResponseJSON
	require.NoError(t, json.Unmarshal(blob, &paged))
	require.Equal(t, "bmV4dA==", paged.GetNextKeyString(), "next_key is the base64 the CLI takes as --page-key")
}

// TestStreamStateJSON_AccrualAt pins the exact accrual the e2e tests expect of
// a settlement: nothing is rounded to whole seconds, what does not amount to a
// whole unit is carried in the remainder.
func TestStreamStateJSON_AccrualAt(t *testing.T) {
	settledAt := time.Date(2026, time.November, 20, 10, 11, 12, 130_000_000, time.UTC)
	exhaustedAt := settledAt

	tests := []struct {
		name          string
		state         StreamStateJSON
		elapsed       time.Duration
		wantPaid      int64
		wantRemainder int64
	}{
		{
			name:    "whole seconds",
			state:   StreamStateJSON{Rate: sdkmath.NewInt(1000), LastSettledAt: settledAt, Remainder: sdkmath.ZeroInt()},
			elapsed: 5 * time.Second, wantPaid: 5000, wantRemainder: 0,
		},
		{
			name:    "fraction of a second is paid, not dropped",
			state:   StreamStateJSON{Rate: sdkmath.NewInt(1000), LastSettledAt: settledAt, Remainder: sdkmath.ZeroInt()},
			elapsed: 2*time.Second + 345_678_912, wantPaid: 2345, wantRemainder: 678_912_000,
		},
		{
			name:    "less than one unit accrues into the remainder",
			state:   StreamStateJSON{Rate: sdkmath.NewInt(3), LastSettledAt: settledAt, Remainder: sdkmath.ZeroInt()},
			elapsed: 300 * time.Millisecond, wantPaid: 0, wantRemainder: 900_000_000,
		},
		{
			name:    "carried remainder completes a unit",
			state:   StreamStateJSON{Rate: sdkmath.NewInt(3), LastSettledAt: settledAt, Remainder: sdkmath.NewInt(900_000_000)},
			elapsed: 100 * time.Millisecond, wantPaid: 1, wantRemainder: 200_000_000,
		},
		{
			name:    "missing remainder counts as zero",
			state:   StreamStateJSON{Rate: sdkmath.NewInt(2), LastSettledAt: settledAt},
			elapsed: 1500 * time.Millisecond, wantPaid: 3, wantRemainder: 0,
		},
		{
			name:    "not after the last settlement",
			state:   StreamStateJSON{Rate: sdkmath.NewInt(1000), LastSettledAt: settledAt, Remainder: sdkmath.NewInt(7)},
			elapsed: -time.Second, wantPaid: 0, wantRemainder: 7,
		},
		{
			name:    "frozen stream accrues nothing",
			state:   StreamStateJSON{Rate: sdkmath.NewInt(1000), LastSettledAt: settledAt, Remainder: sdkmath.NewInt(7), ExhaustedAt: &exhaustedAt},
			elapsed: time.Hour, wantPaid: 0, wantRemainder: 7,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			paid, remainder := tc.state.AccrualAt(settledAt.Add(tc.elapsed))
			require.Equal(t, tc.wantPaid, paid.Int64())
			require.Equal(t, tc.wantRemainder, remainder.Int64())
		})
	}
}

// TestLeaseJSON_IgnoresDeprecatedLastSettledAt pins that a lease printed by a
// node still running consensus version 2 (which carries last_settled_at)
// decodes into LeaseJSON, which no longer has the field.
func TestLeaseJSON_IgnoresDeprecatedLastSettledAt(t *testing.T) {
	blob := `{"lease":{"uuid":"01902a9b-1234-7000-8000-000000000002","state":"LEASE_STATE_ACTIVE",` +
		`"items":[],"created_at":"2026-11-20T10:11:12Z","last_settled_at":"2026-11-20T10:11:13Z"}}`

	var res LeaseResponseJSON
	require.NoError(t, json.Unmarshal([]byte(blob), &res))
	require.Equal(t, billingtypes.LEASE_STATE_ACTIVE, res.Lease.GetState())
}

// TestGetWithdrawResponse pins the decoding of a withdraw transaction's result
// data, the only place has_more and next_key are reported.
func TestGetWithdrawResponse(t *testing.T) {
	want := billingtypes.MsgWithdrawResponse{
		TotalAmounts:    sdk.NewCoins(sdk.NewInt64Coin("upwr", 5000)),
		PayoutAddress:   "manifest1hj5fveer5cjtn4wd6wstzugjfdxzl0xp8ws9ct",
		WithdrawalCount: 2,
		HasMore:         true,
		NextKey:         []byte{0x14, 0x01, 0x02},
	}
	msgResponse, err := codectypes.NewAnyWithValue(&want)
	require.NoError(t, err)
	// Another message's response ahead of it must be skipped, not mistaken for it.
	other, err := codectypes.NewAnyWithValue(&billingtypes.MsgFundCreditResponse{})
	require.NoError(t, err)
	data, err := (&sdk.TxMsgData{MsgResponses: []*codectypes.Any{other, msgResponse}}).Marshal()
	require.NoError(t, err)

	// sdk.NewResponseResultTx upper-cases the hex encoding.
	got, err := GetWithdrawResponse(&sdk.TxResponse{TxHash: "AB", Data: strings.ToUpper(hex.EncodeToString(data))})
	require.NoError(t, err)
	require.True(t, want.TotalAmounts.Equal(got.TotalAmounts))
	require.Equal(t, want.PayoutAddress, got.PayoutAddress)
	require.Equal(t, want.WithdrawalCount, got.WithdrawalCount)
	require.True(t, got.HasMore)
	require.Equal(t, want.NextKey, got.NextKey)

	// A failed transaction has no message responses.
	_, err = GetWithdrawResponse(&sdk.TxResponse{TxHash: "CD", Code: 6, RawLog: "unauthorized"})
	require.ErrorContains(t, err, "unauthorized")

	_, err = GetWithdrawResponse(&sdk.TxResponse{TxHash: "EF", Data: "not hex"})
	require.Error(t, err)
}

// TestFilterAndFindEvents covers the event helpers used to tell a transaction's
// events apart from the EndBlocker's.
func TestFilterAndFindEvents(t *testing.T) {
	events := []abci.Event{
		{Type: "stream_settled", Attributes: []abci.EventAttribute{{Key: "tenant", Value: "a"}, {Key: "amount", Value: "1upwr"}}},
		{Type: "batch_withdraw", Attributes: []abci.EventAttribute{{Key: "stream_count", Value: "2"}}},
		{Type: "stream_settled", Attributes: []abci.EventAttribute{{Key: "tenant", Value: "b"}, {Key: "amount", Value: "2upwr"}}},
	}

	settled := FilterEvents(events, "stream_settled")
	require.Len(t, settled, 2)
	require.Equal(t, "1upwr", settled[0]["amount"])
	require.Equal(t, "2upwr", settled[1]["amount"])
	require.Empty(t, FilterEvents(events, "lease_auto_closed"))

	found, ok := FindEvent(events, "stream_settled", "tenant", "b")
	require.True(t, ok)
	require.Equal(t, "2upwr", found["amount"])

	_, ok = FindEvent(events, "stream_settled", "tenant", "c")
	require.False(t, ok)
}
