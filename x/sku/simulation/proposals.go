package simulation

import (
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
)

// ProposalMsgs is empty because governance proposal messages must use the gov
// module signer, which can differ from the configured POA authority. The full-app
// harness gives that authority a simulation key; SimulateMsgUpdateParams covers
// direct authorized updates and skips imports whose authority is not signable.
func ProposalMsgs() []simtypes.WeightedProposalMsg {
	return nil
}
