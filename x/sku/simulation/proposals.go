package simulation

import (
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
)

// ProposalMsgs is empty because MsgUpdateParams requires the configured POA
// authority, while Cosmos SDK proposal messages must be signed by the gov
// module account. Neither account has a simulation private key, so registering
// the message as a direct operation or governance proposal would be unsound.
func ProposalMsgs() []simtypes.WeightedProposalMsg {
	return nil
}
