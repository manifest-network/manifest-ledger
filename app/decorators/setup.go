package decorators

// Taken from https://github.com/rollchains/spawn/blob/release/v0.50/simapp/app/decorators/setup.go @ e332edf

import (
	protov2 "google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

var EmptyAnte = func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
	return ctx, nil
}

type MockTx struct {
	msgs []sdk.Msg
}

func NewMockTx(msgs ...sdk.Msg) MockTx {
	return MockTx{
		msgs: msgs,
	}
}

func (tx MockTx) GetMsgs() []sdk.Msg {
	return tx.msgs
}

func (tx MockTx) GetMsgsV2() ([]protov2.Message, error) {
	return nil, nil
}

func (tx MockTx) ValidateBasic() error {
	return nil
}
