# Billing invariant execution controls

## Why the crisis ConstantFee is not a work limit

The pinned Cosmos SDK executes `MsgVerifyInvariant` inside the transaction's
message cache. A later failing message rolls back that cache, including the
crisis `ConstantFee`, after the invariant has already run. Ante fees and account
sequence updates persist, and gas remains consumed. A caller must initially
have enough spendable funds to pass the crisis fee debit, but can reuse those
funds after each failed transaction.

This applies to all exposed crisis routes, including bank and billing. Billing's
`reservation-accounting` route still materializes the module's full exported
state for validation. Its work and memory grow with state size; the earlier
index-validation optimization does not make full invariants constant-cost.
Raising `constant_fee` alone does not prevent repeated work with fee rollback.

The committed `manifest-1` genesis has `block.max_gas = -1` and no disabled
circuit messages, while its setup guides use `minimum-gas-prices = "0umfx"`.
These files are historical deployment inputs, not evidence of current live
parameters. Audit current configuration before deploying billing v4.

## Audit and choose controls before deployment

Record the chain ID, height, candidate binary, and these read-only results from
the intended network (supply your normal `--node` and network settings):

```bash
manifestd query consensus params --output json
manifestd query circuit disabled-list --output json
manifestd query circuit account [operator-address] --output json
```

Also inspect each validator's effective `minimum-gas-prices` and
`inv-check-period`, including launch-time overrides. Those settings are local
and cannot be established by querying one public RPC. Retain the results with
the migration rehearsal evidence.

| Control | Effect and limit |
| --- | --- |
| Disable `/cosmos.crisis.v1beta1.MsgVerifyInvariant` through circuit state | Stops public message invocation of all crisis routes, including nested authz/group/governance message routing. Does not stop direct EndBlock assertions or offline export checks. |
| Set a finite positive consensus `block.max_gas` | Rejects transaction gas limits above the block maximum and stops later transactions after block gas is exhausted. Failed transactions still consume block gas. The final transaction can cross the remaining block budget before it is charged, so this is not an exact no-overshoot bound. This is not a precise CPU or memory bound; rehearse at production state size before choosing the cap. |
| Set nonzero validator `minimum-gas-prices` | Requires sufficient offered ante fees for local CheckTx admission. Once a transaction is included, those ante fees remain paid if its messages fail; mempool acceptance alone does not charge an on-chain fee. This is not a consensus fee floor: Manifest's current ProcessProposal handler accepts proposals without enforcing that price. A proposer can include a transaction that bypasses another validator's mempool policy. |

For a new chain, set the selected consensus/circuit policy in genesis. For an
existing chain, execute the corresponding authorized state update through the
network's governance or circuit authority procedure. Do not edit a running
validator's genesis to change consensus state. A block-gas proposal must retain
the other consensus parameter fields and use a cap validated by rehearsal;
there is no universal safe cap supplied by this guide.

## Disable public crisis messages

An account with the required circuit permissions, or the circuit module's
authority, can submit the following transaction. Check permissions first; the
billing allowed list does not grant circuit permissions. A module/group
policy authority uses its normal proposal execution process instead of a
nonexistent local signing key.

```bash
manifestd tx circuit disable /cosmos.crisis.v1beta1.MsgVerifyInvariant --from [authorized-circuit-key]
```

Wait for inclusion and verify transaction execution succeeded, then query
`manifestd query circuit disabled-list --output json` and confirm the exact
leading-slash type URL appears. Do not treat sync broadcast acceptance as
successful execution. Normal billing transactions remain available; this
control blocks only the selected message type.

The app wires the breaker both into ante handling and into the shared message
router. Disabling only outer `MsgExec` would be unnecessarily broad and would
not cover every route. Use the crisis message's own type URL.

Re-enable public verification only after the chosen pricing/work controls and
production-size invariant rehearsal are accepted:

```bash
manifestd tx circuit reset /cosmos.crisis.v1beta1.MsgVerifyInvariant --from [authorized-circuit-key]
```

Again verify execution and the resulting disabled list. If periodic crisis
checks are enabled, their EndBlock work still needs separate measurement;
transaction gas controls do not cap direct EndBlock invariant calls. Run
expensive offline validation against a copy/export on a separate node where
appropriate.

No live network policy change is made by this PR. Production cardinality and
peak-memory rehearsal remain an upgrade gate (ENG-890); streaming the complete
reservation invariant is a separate optimization and would not itself close
the generic crisis fee-rollback path.
