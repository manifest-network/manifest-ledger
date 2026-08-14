package types_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

const luValidUUID = "01902a9b-1234-7000-8000-000000000001"

var (
	luHash    = []byte("11111111111111111111111111111111")
	luTooLong = make([]byte, types.MaxMetaHashLength+1)

	// luValidAddr is generated rather than hardcoded so the bech32 checksum is
	// always correct; a bad literal would fail genesis validation before the
	// lease-update rules under test are ever reached.
	luValidAddr = mustTestAddr()
)

func mustTestAddr() string {
	_, _, addr := testdata.KeyTestPubAddr()
	return addr.String()
}

func TestMsgUpdateLease_ValidateBasic(t *testing.T) {
	tests := []struct {
		name    string
		msg     types.MsgUpdateLease
		wantErr error
	}{
		{
			name: "valid",
			msg:  types.MsgUpdateLease{Sender: luValidAddr, LeaseUuid: luValidUUID, MetaHash: luHash},
		},
		{
			name:    "invalid sender",
			msg:     types.MsgUpdateLease{Sender: "not-bech32", LeaseUuid: luValidUUID, MetaHash: luHash},
			wantErr: types.ErrUnauthorized,
		},
		{
			name:    "empty lease_uuid",
			msg:     types.MsgUpdateLease{Sender: luValidAddr, LeaseUuid: "", MetaHash: luHash},
			wantErr: types.ErrInvalidLease,
		},
		{
			name:    "malformed lease_uuid",
			msg:     types.MsgUpdateLease{Sender: luValidAddr, LeaseUuid: "not-a-uuid", MetaHash: luHash},
			wantErr: types.ErrInvalidLease,
		},
		{
			// Unlike lease creation, where meta_hash is optional, every
			// lease-update message names a specific manifest version.
			name:    "empty meta_hash",
			msg:     types.MsgUpdateLease{Sender: luValidAddr, LeaseUuid: luValidUUID, MetaHash: nil},
			wantErr: types.ErrInvalidMetaHash,
		},
		{
			name:    "meta_hash too long",
			msg:     types.MsgUpdateLease{Sender: luValidAddr, LeaseUuid: luValidUUID, MetaHash: luTooLong},
			wantErr: types.ErrInvalidMetaHash,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestMsgAcknowledgeLeaseUpdate_ValidateBasic(t *testing.T) {
	tests := []struct {
		name    string
		msg     types.MsgAcknowledgeLeaseUpdate
		wantErr error
	}{
		{
			name: "valid",
			msg:  types.MsgAcknowledgeLeaseUpdate{Sender: luValidAddr, LeaseUuid: luValidUUID, MetaHash: luHash},
		},
		{
			name:    "invalid sender",
			msg:     types.MsgAcknowledgeLeaseUpdate{Sender: "nope", LeaseUuid: luValidUUID, MetaHash: luHash},
			wantErr: types.ErrUnauthorized,
		},
		{
			name:    "malformed lease_uuid",
			msg:     types.MsgAcknowledgeLeaseUpdate{Sender: luValidAddr, LeaseUuid: "x", MetaHash: luHash},
			wantErr: types.ErrInvalidLease,
		},
		{
			// The hash is what makes acknowledgement safe against a request the
			// provider never evaluated, so it cannot be omitted.
			name:    "empty meta_hash",
			msg:     types.MsgAcknowledgeLeaseUpdate{Sender: luValidAddr, LeaseUuid: luValidUUID},
			wantErr: types.ErrInvalidMetaHash,
		},
		{
			name:    "meta_hash too long",
			msg:     types.MsgAcknowledgeLeaseUpdate{Sender: luValidAddr, LeaseUuid: luValidUUID, MetaHash: luTooLong},
			wantErr: types.ErrInvalidMetaHash,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestMsgRejectLeaseUpdate_ValidateBasic(t *testing.T) {
	tests := []struct {
		name    string
		msg     types.MsgRejectLeaseUpdate
		wantErr error
	}{
		{
			name: "valid with reason",
			msg:  types.MsgRejectLeaseUpdate{Sender: luValidAddr, LeaseUuid: luValidUUID, MetaHash: luHash, Reason: "unsupported image"},
		},
		{
			name: "valid without reason",
			msg:  types.MsgRejectLeaseUpdate{Sender: luValidAddr, LeaseUuid: luValidUUID, MetaHash: luHash},
		},
		{
			name:    "empty meta_hash",
			msg:     types.MsgRejectLeaseUpdate{Sender: luValidAddr, LeaseUuid: luValidUUID},
			wantErr: types.ErrInvalidMetaHash,
		},
		{
			name: "reason too long",
			msg: types.MsgRejectLeaseUpdate{
				Sender: luValidAddr, LeaseUuid: luValidUUID, MetaHash: luHash,
				Reason: strings.Repeat("a", types.MaxRejectionReasonLength+1),
			},
			wantErr: types.ErrInvalidRejectionReason,
		},
		{
			name: "reason at limit",
			msg: types.MsgRejectLeaseUpdate{
				Sender: luValidAddr, LeaseUuid: luValidUUID, MetaHash: luHash,
				Reason: strings.Repeat("a", types.MaxRejectionReasonLength),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestMsgCancelLeaseUpdate_ValidateBasic(t *testing.T) {
	tests := []struct {
		name    string
		msg     types.MsgCancelLeaseUpdate
		wantErr error
	}{
		{
			// No meta_hash: the tenant authored the request and always means
			// the one currently pending.
			name: "valid",
			msg:  types.MsgCancelLeaseUpdate{Sender: luValidAddr, LeaseUuid: luValidUUID},
		},
		{
			name:    "invalid sender",
			msg:     types.MsgCancelLeaseUpdate{Sender: "nope", LeaseUuid: luValidUUID},
			wantErr: types.ErrUnauthorized,
		},
		{
			name:    "empty lease_uuid",
			msg:     types.MsgCancelLeaseUpdate{Sender: luValidAddr},
			wantErr: types.ErrInvalidLease,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

// leaseUpdateGenesis returns a minimal valid genesis with a single ACTIVE lease
// that tests mutate to exercise the lease-update validation rules. The credit
// account must carry the lease's reservation (locked_price × quantity ×
// default min_lease_duration = 100 × 1 × 3600) or genesis fails on the
// reservation cross-check before reaching the rules under test.
func leaseUpdateGenesis(at time.Time) *types.GenesisState {
	creditAddr, err := types.DeriveCreditAddressFromBech32(luValidAddr)
	if err != nil {
		panic(err)
	}

	return &types.GenesisState{
		Params: types.DefaultParams(),
		Leases: []types.Lease{
			{
				Uuid:         luValidUUID,
				Tenant:       luValidAddr,
				ProviderUuid: "01902a9b-1234-7000-8000-0000000000aa",
				Items: []types.LeaseItem{
					{
						SkuUuid:     "01902a9b-1234-7000-8000-0000000000bb",
						Quantity:    1,
						LockedPrice: sdk.NewCoin(testDenom, math.NewInt(100)),
					},
				},
				State:         types.LEASE_STATE_ACTIVE,
				CreatedAt:     at,
				LastSettledAt: at,
			},
		},
		CreditAccounts: []types.CreditAccount{
			{
				Tenant:           luValidAddr,
				CreditAddress:    creditAddr.String(),
				ActiveLeaseCount: 1,
				ReservedAmounts:  sdk.NewCoins(sdk.NewCoin(testDenom, math.NewInt(360000))),
			},
		},
		LeaseSequence: 1,
	}
}

// TestGenesisValidate_LeaseUpdateFields covers the invariants the msg layer
// upholds at runtime, so a hand-written genesis cannot start the chain in a
// state the messages could never have produced.
func TestGenesisValidate_LeaseUpdateFields(t *testing.T) {
	at := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		mutate  func(*types.Lease)
		wantErr error
	}{
		{
			name: "pending update on an ACTIVE lease",
			mutate: func(l *types.Lease) {
				l.PendingMetaHash = luHash
				l.PendingMetaHashAt = &at
			},
		},
		{
			name: "no pending update",
			mutate: func(_ *types.Lease) {
			},
		},
		{
			name: "revision with a committed hash",
			mutate: func(l *types.Lease) {
				l.MetaHash = luHash
				l.MetaHashRevision = 3
			},
		},
		{
			name: "pending_meta_hash too long",
			mutate: func(l *types.Lease) {
				l.PendingMetaHash = luTooLong
				l.PendingMetaHashAt = &at
			},
			wantErr: types.ErrInvalidMetaHash,
		},
		{
			name: "pending update on a CLOSED lease",
			mutate: func(l *types.Lease) {
				l.State = types.LEASE_STATE_CLOSED
				l.ClosedAt = &at
				l.PendingMetaHash = luHash
				l.PendingMetaHashAt = &at
			},
			wantErr: types.ErrInvalidLease,
		},
		{
			name: "pending update without a timestamp",
			mutate: func(l *types.Lease) {
				l.PendingMetaHash = luHash
			},
			wantErr: types.ErrInvalidLease,
		},
		{
			name: "timestamp without a pending update",
			mutate: func(l *types.Lease) {
				l.PendingMetaHashAt = &at
			},
			wantErr: types.ErrInvalidLease,
		},
		{
			name: "revision without a committed hash",
			mutate: func(l *types.Lease) {
				l.MetaHash = nil
				l.MetaHashRevision = 1
			},
			wantErr: types.ErrInvalidMetaHash,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gs := leaseUpdateGenesis(at)
			tc.mutate(&gs.Leases[0])

			err := gs.Validate()
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}
