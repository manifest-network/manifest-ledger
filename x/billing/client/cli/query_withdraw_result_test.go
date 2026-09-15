package cli

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"

	"github.com/cosmos/cosmos-sdk/client"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	clitestutil "github.com/cosmos/cosmos-sdk/testutil/cli"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

type withdrawResultRPC struct {
	client.CometRPC
	result *coretypes.ResultTx
	err    error
	hash   []byte
}

func (rpc *withdrawResultRPC) Tx(_ context.Context, hash []byte, _ bool) (*coretypes.ResultTx, error) {
	rpc.hash = hash
	return rpc.result, rpc.err
}

func TestWithdrawResultCmd(t *testing.T) {
	const pageJSON = `{
		"total_amounts": [{"denom": "umfx", "amount": "123"}],
		"payout_address": "manifest1payout",
		"withdrawal_count": "2",
		"has_more": true,
		"next_key": "AP8B",
		"failed_lease_uuids": ["01902a9b-1234-7000-8000-000000000001"]
	}`
	const finalPageJSON = `{
		"total_amounts": [],
		"payout_address": "",
		"withdrawal_count": "0",
		"has_more": false,
		"next_key": null,
		"failed_lease_uuids": []
	}`
	want := &types.MsgWithdrawResponse{
		TotalAmounts:     sdk.NewCoins(sdk.NewInt64Coin("umfx", 123)),
		PayoutAddress:    "manifest1payout",
		WithdrawalCount:  2,
		HasMore:          true,
		NextKey:          []byte{0, 255, 1},
		FailedLeaseUuids: []string{"01902a9b-1234-7000-8000-000000000001"},
	}
	withdrawal, err := codectypes.NewAnyWithValue(want)
	require.NoError(t, err)
	other, err := codectypes.NewAnyWithValue(&types.MsgCloseLeaseResponse{})
	require.NoError(t, err)
	data := func(responses ...*codectypes.Any) []byte {
		encoded, err := (&sdk.TxMsgData{MsgResponses: responses}).Marshal()
		require.NoError(t, err)
		return encoded
	}
	committed := func(encoded []byte) *coretypes.ResultTx {
		return &coretypes.ResultTx{Height: 42, TxResult: abcitypes.ExecTxResult{Data: encoded}}
	}
	encoding := moduletestutil.MakeTestEncodingConfig()
	hash := strings.Repeat("AB", 32)

	for _, tc := range []struct {
		name     string
		result   *coretypes.ResultTx
		err      error
		args     []string
		wantJSON string
		errMsg   string
	}{
		{name: "decoded module response preserves cursor and failures", result: committed(data(withdrawal)), wantJSON: pageJSON},
		{name: "explicit second message", result: committed(data(other, withdrawal)), args: []string{"--msg-index=1"}, wantJSON: pageJSON},
		{name: "empty final page", result: committed(data(&codectypes.Any{TypeUrl: withdrawal.TypeUrl})), wantJSON: finalPageJSON},
		{name: "not yet indexed", err: errors.New("tx not found"), errMsg: "query committed transaction: tx not found"},
		{name: "sync admission is not execution", result: &coretypes.ResultTx{}, errMsg: "not been included"},
		{name: "nil result", errMsg: "not been included"},
		{name: "failed execution", result: &coretypes.ResultTx{Height: 42, TxResult: abcitypes.ExecTxResult{Code: 5, Codespace: "billing", Log: "settlement failed"}}, errMsg: "transaction failed with code 5 (billing): settlement failed"},
		{name: "malformed envelope", result: committed([]byte{255}), errMsg: "decode transaction message responses"},
		{name: "missing responses", result: committed(nil), errMsg: "out of range"},
		{name: "empty response entry", result: committed(data(&codectypes.Any{})), errMsg: "is not /liftedinit.billing.v1.MsgWithdrawResponse"},
		{name: "wrong message", result: committed(data(other)), errMsg: "is not /liftedinit.billing.v1.MsgWithdrawResponse"},
		{name: "malformed response", result: committed(data(&codectypes.Any{TypeUrl: withdrawal.TypeUrl, Value: []byte{255}})), errMsg: "decode withdrawal response"},
		{name: "index too large", result: committed(data(withdrawal)), args: []string{"--msg-index=4294967295"}, errMsg: "out of range"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rpc := &withdrawResultRPC{result: tc.result, err: tc.err}
			clientCtx := client.Context{}.WithCodec(encoding.Codec).WithClient(rpc)
			args := append([]string{hash, "--output=json"}, tc.args...)
			out, err := clitestutil.ExecTestCLICmd(clientCtx, GetWithdrawResultCmd(), args)
			if tc.errMsg != "" {
				require.ErrorContains(t, err, tc.errMsg)
				require.NotContains(t, out.String(), `"has_more"`)
				return
			}
			require.NoError(t, err)
			require.Equal(t, strings.ToLower(hash), hex.EncodeToString(rpc.hash))
			// The operator recipe consumes these exact snake_case keys, JSON
			// types, and final-page defaults. A protobuf round trip also accepts
			// camelCase aliases and omitted defaults, hiding breaking CLI changes.
			require.JSONEq(t, tc.wantJSON, out.String())
		})
	}
}

func TestWithdrawResultCmdRejectsMalformedHash(t *testing.T) {
	for _, hash := range []string{"", "AB", strings.Repeat("xx", 32), "0x" + strings.Repeat("ab", 32)} {
		rpc := &withdrawResultRPC{}
		_, err := clitestutil.ExecTestCLICmd(client.Context{}.WithClient(rpc), GetWithdrawResultCmd(), []string{hash})
		require.ErrorContains(t, err, "64-character hexadecimal")
		require.Nil(t, rpc.hash, "invalid input must fail before making an RPC request")
	}
}
