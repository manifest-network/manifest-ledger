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
//   - x/poa: PoA sim ops (SimulateMsgCreateValidator, SimulateMsgSetPower,
//     etc., registered via module/depinject.go) bypass the admin check
//     under the POA_BYPASS_ADMIN_CHECK_FOR_SIMULATION_TESTING_ONLY
//     env-flag and add validators directly. Combined with staking's own
//     sim ops, the validator set eventually exceeds MaxValidators and
//     triggers "more validators than maxValidators found" in staking's
//     BeginBlock historical-info tracking. Long-term fix would be to
//     update the PoA fork's sim ops to respect MaxValidators.
//   - x/staking: defensive — sim ops include MsgCreateValidator which
//     compounds the PoA issue above.
//
// Tracking: ENG-43.
type noopSimWeightedOpsModule struct {
	module.AppModuleSimulation
}

// WeightedOperations returns nil, skipping the wrapped module's sim ops.
func (noopSimWeightedOpsModule) WeightedOperations(_ module.SimulationState) []simulation.WeightedOperation {
	return nil
}
