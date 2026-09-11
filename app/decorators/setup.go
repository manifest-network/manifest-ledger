package decorators

// Taken from https://github.com/rollchains/spawn/blob/release/v0.50/simapp/app/decorators/setup.go @ e332edf

import (
	protov2 "google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// EmptyAnte is an ante handler that leaves the context unchanged.
var EmptyAnte = func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
	return ctx, nil
}

// MockTx is a minimal transaction implementation for decorator tests.
type MockTx struct {
	msgs []sdk.Msg
}

// NewMockTx constructs a mock transaction containing msgs.
func NewMockTx(msgs ...sdk.Msg) MockTx {
	return MockTx{
		msgs: msgs,
	}
}

// GetMsgs returns the transaction's messages.
func (tx MockTx) GetMsgs() []sdk.Msg {
	return tx.msgs
}

// GetMsgsV2 implements the SDK transaction interface.
func (tx MockTx) GetMsgsV2() ([]protov2.Message, error) {
	return nil, nil
}

// ValidateBasic implements the SDK transaction interface.
func (tx MockTx) ValidateBasic() error {
	return nil
}
