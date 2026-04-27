package app

import (
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/types/simulation"
)

// noopSimWeightedOpsModule wraps an AppModuleSimulation and replaces its
// WeightedOperations with nil. Genesis generation and store decoding are
// delegated to the inner module so the sim still produces realistic state
// for other modules; only the per-module sim ops are skipped.
//
// Used to skip module sim ops that panic or fail under SDK v0.53:
//   - x/staking: sim ops include MsgCreateValidator, which combined with
//     PoA's POA_BYPASS_ADMIN_CHECK_FOR_SIMULATION_TESTING_ONLY env-flag
//     bypass eventually exceeds staking's MaxValidators and triggers
//     "more validators than maxValidators found" in staking's BeginBlock
//     historical-info tracking.
//   - x/billing: SimulateMsgCreateLease picks a random SKU + random
//     quantities/durations without pre-flighting that the chosen lease
//     cost fits within the tenant's available credit, so the tx fails
//     with "insufficient credit balance" once accumulated leases drain
//     the credit account. Pre-existing bug (Linear ENG-44); the proper
//     fix is to pre-compute lease cost and compare against
//     GetAvailableCredit before submitting.
//
// Tracking: ENG-43. Drop the billing entry once ENG-44 lands.
type noopSimWeightedOpsModule struct {
	module.AppModuleSimulation
}

// WeightedOperations returns nil, skipping the wrapped module's sim ops.
func (noopSimWeightedOpsModule) WeightedOperations(_ module.SimulationState) []simulation.WeightedOperation {
	return nil
}
