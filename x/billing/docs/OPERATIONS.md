# Billing invariant execution controls

## Why the crisis ConstantFee is not a work limit

The pinned Cosmos SDK executes `MsgVerifyInvariant` inside the transaction's
message cache. A later failing message rolls back that cache, including the
crisis `ConstantFee`, after the invariant has already run. Ante fees and account
sequence updates persist, and gas remains consumed. For this **on-chain**
transaction path, the signer must initially have enough spendable funds to pass
the crisis fee debit, but can reuse those funds after each failed transaction.

The public `/cosmos.tx.v1beta1.Service/Simulate` endpoint exposes a second path.
Simulation does not verify signatures and does not commit its message or ante
state. An unsigned, zero-fee request can use an existing funded account's
public address and current sequence to invoke the invariant; the caller needs
neither that account's private key nor funds of its own. The simulated sender
still needs enough spendable funds for the temporary crisis debit, which is
then discarded. Nonzero fees declared in the simulated transaction may also
be debited in the discarded context; simulation does not grant access to those
funds. Local `minimum-gas-prices` is a CheckTx filter and does not protect this
endpoint.

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
manifestd query consensus params --height [audit-height] --output json
manifestd query circuit disabled-list --height [audit-height] --output json
manifestd query circuit accounts --height [audit-height] --page-limit 100 --output json
```

Pin the state queries to the same committed audit height with `--height`.
The `accounts` response is paginated: while `pagination.next_key` is nonempty,
repeat the command with `--page-key [pagination.next_key]` at that same height.
Circuit's generated CLI uses `--page-limit`, unlike billing's custom `--limit`.
Inspect every grantee, not only the intended operator. `LEVEL_SUPER_ADMIN` and
`LEVEL_ALL_MSGS` can reset this circuit; `LEVEL_SOME_MSGS` can also reset it when
its `limit_type_urls` includes the exact crisis type URL. Super admins and the
module authority can change grants. The authority itself is not necessarily
listed among grantees: audit the deployed authority/governance policy and any
authz delegations that can execute circuit messages too. Recheck the disabled
list and grants before relying on the control throughout the exposure window.

To inspect a particular operator, optionally query its explicit permission record:

```bash
manifestd query circuit account [operator-address] --height [audit-height] --output json
```

An address with no explicit record produces a nonzero exit status, reported as
`InvalidArgument`. Record "no explicit grant" only when the error identifies a
missing permission record and the complete same-height `accounts` inventory
confirms its absence. `InvalidArgument` alone is not proof of absence; other
query, transport, decoding, or unavailable-height errors leave the audit
incomplete. An absent explicit record does not rule out module-authority powers.

Also inspect the effective `minimum-gas-prices`, `inv-check-period`, and
`wasm.simulation_gas_limit`, including launch-time overrides. Inspect every
node serving transaction simulation, not only validators. These settings are
local and cannot be established by querying one public RPC. Retain the results
with the migration rehearsal evidence.

| Control | Effect and limit |
| --- | --- |
| Disable `/cosmos.crisis.v1beta1.MsgVerifyInvariant` through circuit state | Blocks disabled crisis messages at ante and shared-router checks, including direct Simulate requests and nested message routing while the route remains disabled in that context. It is not a complete simulation-work bound: simulation can model permission changes in its discarded cache. Does not stop direct EndBlock assertions or offline export checks. |
| Set a finite positive consensus `block.max_gas` | Rejects transaction gas limits above the block maximum and stops later transactions after block gas is exhausted. Failed transactions still consume block gas. The final transaction can cross the remaining block budget before it is charged, so this is not an exact no-overshoot bound. Also provides the simulation meter limit when `wasm.simulation_gas_limit` is unset. This is not a precise CPU or memory bound; rehearse at production state size before choosing the cap. |
| Set a positive local `wasm.simulation_gas_limit` on every simulation-serving node | Caps metered work for all transaction simulations, including non-Wasm crisis messages. An explicit value replaces the fallback to consensus block gas; it is not automatically the smaller of the two. Zero is invalid. This does not charge callers or limit aggregate concurrent requests. |
| Set nonzero validator `minimum-gas-prices` | Requires sufficient offered ante fees for local CheckTx admission. Once a transaction is included, those ante fees remain paid if its messages fail; mempool acceptance alone does not charge an on-chain fee. This is not a consensus fee floor: Manifest's current ProcessProposal handler accepts proposals without enforcing that price. A proposer can include a transaction that bypasses another validator's mempool policy. It does not constrain Simulate. |

Do not rely on circuit state alone to bound all simulation requests. Simulation
executes hypothetical state changes, and the router consults circuit state in the same discarded
cache. This includes permission updates within the
simulated execution. Direct verification against an unchanged disabled circuit
is rejected, as the local regression verifies; that result is not a guarantee
for every composed simulation. Signature checks on real transactions still
protect committed permission changes. Independently enforce a positive
simulation limit and RPC request-rate/concurrency controls.

With `block.max_gas <= 0` and no explicit simulation limit, the pinned Wasmd
ante decorator leaves simulation gas unbounded. Configure a measured positive
`simulation_gas_limit` in the node's `[wasm]` section of `app.toml`; when block
gas is finite, normally keep the simulation limit at or below it. Apply changes
through the node's normal restart procedure, verify the effective configuration,
and confirm a request exceeding the limit fails. Use gateway request-rate and
concurrency limits as well; a per-request gas cap does not bound total public
RPC load. The limit affects gas estimation clients, so rehearse normal client
requests before rollout.

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
