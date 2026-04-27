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
//   - x/tokenfactory: strangelove-ventures/tokenfactory v0.50.7-wasmvm2
//     builds a TxConfig from codectypes.NewInterfaceRegistry(), which in
//     v0.53 returns a failingAddressCodec. Long-term fix lives in the
//     manifest-network/tokenfactory fork (Linear ENG-39).
//   - x/poa: PoA sim ops bypass the admin check and add validators
//     directly, eventually exceeding staking's MaxValidators and
//     triggering "more validators than maxValidators found" in
//     staking's BeginBlock historical-info tracking. Long-term fix lives
//     in the manifest-network/poa fork (Linear ENG-40).
//   - x/staking: defensive — sim ops include MsgCreateValidator which
//     would compound the PoA issue above and have other v0.53 friction.
//     Upstream-driven; expected to be carried along by future SDK bumps.
//   - x/billing: SimulateMsgCreateLease picks a random SKU + random
//     quantities/durations without pre-flighting that the chosen lease
//     cost fits within the tenant's available credit, so the tx fails
//     with "insufficient credit balance" once accumulated leases drain
//     the credit account. Pre-existing bug; the proper fix is to
//     pre-compute lease cost and compare against GetAvailableCredit
//     before submitting. Tracked separately.
//
// Tracking: ENG-43. Drop the tokenfactory and poa entries once ENG-39
// and ENG-40 land respectively. Drop the billing entry once the sim op
// learns to pre-flight credit availability.
type noopSimWeightedOpsModule struct {
	module.AppModuleSimulation
}

// WeightedOperations returns nil, skipping the wrapped module's sim ops.
func (noopSimWeightedOpsModule) WeightedOperations(_ module.SimulationState) []simulation.WeightedOperation {
	return nil
}
