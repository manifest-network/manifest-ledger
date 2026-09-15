# PR179 export and simulation review — 2026-09-10

This addresses Claude's [audit of `3ed262e`](https://github.com/manifest-network/manifest-ledger/pull/179#issuecomment-5620450055).
The earlier [response](2026-09-10-claude-review-response.md) and its validation
remain historical evidence. The prior round missed the zero-height export
entrypoint introduced by F55; both CI failures independently confirm that
regression. This round includes the previously failing normal and
coverage-instrumented import simulations.

The [subsequent review response](2026-09-10-claude-follow-up-review-response.md)
corrects stale in-process rollback time, runbook command spelling, and the
simulation state observer, and verifies the historical store timestamp support.

Confidence describes confidence in the disposition, not exploit probability
or a guarantee that the code contains no other vulnerabilities.

## Findings and decisions

| Finding | Disposition | Confidence |
| --- | --- | --- |
| F55 timestamp invariant breaks `export --for-zero-height` | **Fixed; high operational severity.** Export previously constructed a zero-time header. It now uses the selected committed height's timestamp, recovering it from root multistore metadata after reopening; normal in-process header time is retained where applicable. Runtime timestamp and spendable-backing checks remain strict. Populated PENDING/ACTIVE/CLOSED and matured vesting state pass live, reopened latest, and selected historical export/import tests. | 99% |
| Snapshot/rollback metadata can omit source time | **Handled explicitly; additional operator limitation.** Independent review found the SDK rebuilds commit metadata without timestamps on restore/rollback. A real rollback/reopen test now requires a descriptive zero-height export error rather than a misleading future-lease panic. Ordinary export remains available. No source time is guessed; use retained timestamp metadata or an appropriate later committed height for zero-height preparation. | 99% |
| Public Simulate can invoke crisis without committing fees | **Accepted with scope; operational exposure.** Pinned source skips signature verification in simulation, checks public account sequence, and discards simulated state. An unsigned caller can use a funded account's public identity for temporary crisis-fee backing. We did not reproduce unsigned/impersonated requests; six bounded, fully signed requests from test-owned accounts verify actual gRPC simulation gas controls, circuit checks, and discarded balances/sequence/module state. | 99% source behavior; no unsigned reproduction |
| Circuit state is not a complete bound on all composed simulations | **Additional source-derived qualification.** Direct requests against an unchanged disabled circuit fail before invariant work. Simulation can also model permission changes, while the router consults the same discarded state. Therefore the runbook requires an independent positive simulation meter and RPC rate/concurrency controls; it does not promise that disabling one message alone bounds all composed simulation work. No unsigned or policy-changing bypass was executed. | 95%; source analysis only |
| F52 caller mutation survives in `AutoCloseLease` | **Fixed; low defensive issue.** Reuse the existing reservation clone helper, stage lease/account values, and assign callers only after all operations succeed. Tests cover settlement followed by late lease-index and credit-account failures, aliases, and success. The existing caller-owned CacheContext contract remains: this helper does not independently roll back store/bank writes. No currently reachable persistent state-loss path was established. | 99% |
| F56 lease-free preflight rejects import-safe allowed-list aliases | **Fixed; low, offline only.** Normalize only allowed-list identities in an audit copy before strict accounting validation. Never rebuild counts or erase orphan claims at that gate. Alias fixtures match prepared import; all five orphan reservation/count variants still fail, including with aliases. Schema remains 6. | 99% |
| Error code 34 docs mention only acknowledgement | **Documentation fixed.** API error table and troubleshooting now cover nonempty pending-domain set/re-set calls, equality, unchanged-timeout retry behavior, and clearing after deadline while still PENDING. Error number and message contracts are unchanged. | 100% |
| Crisis transaction `GasUsed > 0` can be satisfied by ante alone | **Test gap fixed.** Direct invariant tests isolate the caller's gas meter from ante overhead. Adding 16 valid accounts increases charged read work, and the smaller empty-state budget stops the populated fixture. A read-only infinite-meter mutation fails the test. | 100% |
| Circuit audit queries only one grantee | **Documentation fixed.** Enumerate all permission pages at one height, inspect exact reset rights and grant administrators, and account for module authority and authz delegation outside the grantee list. The guide distinguishes those rights from the circuit disabled-message list. | 99% |
| Restart export retains original `genesis_time` | **Documentation completed; pre-existing behavior.** The restart step now explicitly requires a separate genesis copy using source block time or an agreed later time, with preflight and isolated import verification. Zero-height export does not rewind lease or vesting time; old time can fail timestamp or backing checks. | 100% |
| Redundant `closed_at >= created_at` check | **Retained.** It follows from the other ordering constraints, but retaining it preserves the precise existing diagnostic. Removing it does not simplify the public contract or improve accepted-state behavior. | 100% |
| Historical acknowledged/rejected/expired timestamps lack new import checks | **Previously scoped information retained.** This change does not add requirements for those historical fields; operator docs continue to state the limits. No fund-impacting consumer was established by this review. | 99% |

The full root suite also exposed an existing release-wrapper test that assumed
the developer checkout had a `.git` directory. The wrapper intentionally
requires a self-contained checkout for its container mount. Its test now creates
a small ordinary Git repository and invokes the unchanged production wrapper
there, so validation works from a linked worktree too.

## Why the export fix preserves validation

A blanket zero-time skip would remove the visible timestamp panic but leave
vesting-backed reservation validation evaluating locks at Go's zero time. The
fix supplies the source clock to all export consumers. It uses metadata for
the selected height, not the database's newest height, and returns an error
when zero-height preparation has no reliable clock. Corruption that lies after
the selected historical height still fails, even when a newer block exists.
This follows the pinned SDK's own root multistore timestamp lookup pattern.

CLI export reopens the application and reads the selected committed state.
This does not newly promise a pure snapshot from an arbitrary running app whose
CheckTx cache already contains uncommitted ante changes; that pre-existing
in-process export behavior is outside this patch.

## Simulation controls and limits

The [operations guide](../../x/billing/docs/OPERATIONS.md) separates on-chain
fee rollback from simulation. An explicit positive `wasm.simulation_gas_limit`
applies to all transaction simulations, including non-Wasm messages. It
overrides, rather than takes the minimum of, positive consensus block gas.
When unset, positive block gas is the fallback; neither configured means the
simulation meter remains unbounded. Zero is invalid. Local minimum gas prices
only affect CheckTx and do not protect Simulate.

The bounded test matrix verifies positive fallback, explicit positive limit,
both exhaustion paths, explicit limit precedence over a smaller block limit,
and direct circuit rejection before the invariant callback. All use real signed
transactions from keys created and owned by the test. Successful and failed
simulations leave balances, sequence, billing state, and fee collector state
unchanged. They do not test every composed permission-changing simulation.

The unsigned path was verified by reading the pinned SDK's public-key/signature,
sequence, fee, simulation-cache, and routing behavior. An automated cybersecurity
review stopped the delegated unsigned reproduction; it was not retried. The
bounded signed tests are a narrower validation of the available protections,
not evidence that an unsigned attack was executed.

No live circuit, gas, validator, gateway, or permission policy was changed.
Production-sized workload measurements and selection/deployment of controls
remain an explicit gate (ENG-890). A finite gas meter is not a precise CPU or
peak-memory limit, and parallel RPC requests still need aggregate controls.

## Validation

Commands, results, and mutation evidence are recorded in the
[validation notes](2026-09-10-claude-export-review-validation/README.md).
The scope includes the two CI entrypoints that failed at `3ed262e`, whole-root
unit tests, focused export/atomicity/preflight/meter tests, configured lint,
and executable documentation checks. It does not claim that every broad
cross-cutting hunt from the original review has run. Full Docker/interchain
coverage, unsigned reproduction, live policy audit, and production-cardinality
rehearsal are not claimed as local validation.
