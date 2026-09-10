# PR179 follow-up validation — 2026-09-10

Baseline: `4b89fda`. Commands used Go 1.26.8 with `GOMAXPROCS=4`, `-p 2`
where shown, and a workspace-local `TMPDIR`. Lint used golangci-lint 2.12.2,
with `GOROOT` and `PATH` pointing to Go 1.26.8 and an isolated lint cache.
Archived whitespace is normalized; the simulation summary omits parameter
dumps and per-operation progress.

## Whole-root checks

```bash
go test -p 2 ./... -count=1
make sim-after-import
golangci-lint run --concurrency 2 ./...
node --test --test-reporter=tap scripts/docs_examples.test.mjs scripts/provider_auth_examples.test.mjs
```

- [Whole-root package suite](root-tests.log): **passed**, exit 0. Billing keeper
  took 192.702 seconds under concurrent host load; SKU keeper took 15.599 seconds.
- [Enabled import simulation](import-simulation-summary.log): **passed** in
  130.39 seconds at seed `2507940531156952020`, 100 blocks, commit enabled,
  invariant period 5, and unchanged `simulation/sim_params.json`. The harness
  uses its next seed for import continuation.
- [Full lint](lint.log): **0 issues** after correcting the new test adapter to
  take `context.Context` first. The [focused withdrawal rerun](withdrawal-tests.log)
  passes after that test-only correction. A final release-fixture hardening
  made shell argument checks explicit and gave each subtest a fresh Docker log;
  its [focused tests](wrapper-tests.log) and [scripts lint](scripts-lint.log)
  pass. Those test-only edits followed the whole-root run; production code was
  already final for that run.
- [Executable documentation tests](documentation-tests.log): **39 passed**,
  none failed or skipped.

## Regression and mutation checks

```bash
go test -p 2 ./app -run 'TestZeroHeight' -count=1 -v
go test -p 2 ./x/billing/keeper -run 'TestProviderLeaseWithdrawal|TestAutoCloseLeaseFailureLeavesCallerUnchanged' -count=1 -v
go test -p 2 ./app -run '^TestCrisisSimulationHonorsNodeGasAndCircuitControls$' -count=1 -v
go test -p 2 ./scripts -run '^TestContainerizedGoReleaserUsesPinnedOfflineToolchain$' -count=1 -v
```

- [Export tests](export-tests.log) pass for in-process, reopened latest,
  selected historical, vesting, restart-time, and rollback cases. Restoring
  only baseline `app/export.go` via a read-only overlay makes the new
  rollback-without-reopen regression [fail as expected](export-stale-header-mutant.log):
  the old implementation returns no error with a stale height-3 clock for
  selected height 2.
- [Withdrawal tests](withdrawal-tests.log) preserve caller state and aliases
  after actual reserved-fund transfers followed by two different persistence
  failures. Both the [baseline helper](withdrawal-original-mutant.log) and a
  [shallow-copy-only helper](withdrawal-shallow-mutant.log) fail the new tests.
  Read-only overlays never reverted the working implementation. Success cases
  cover fractional settlement, auto-close, address aliases, and caller-owned
  cache commit. The early AutoClose case checks error propagation; late writes
  test staging.
- [Simulation baseline](simulation-baseline.log) passes with the corrected
  CheckTx observer. An isolated copy of the pinned SDK removes only the outer
  simulation cache branch, letting real signed-request ante writes reach
  CheckTx. The new observer [fails on the 17-unit fee leak](simulation-cache-leak-mutant.log),
  while the [previous CMS observer passes](simulation-old-observer-control.log)
  all six cases against the identical mutation. All requests use test-owned
  keys and finite positive budgets. No unsigned, impersonated, composed
  permission-changing, unbounded, or live request was executed.
- The SDK probe uses an alternate `-modfile` with only the SDK replacement
  pointed at its isolated copy and `GOWORK=off`; a matching unmodified-SDK
  baseline passes. Go 1.26 disallows overlays beneath `GOMODCACHE`, which is
  why this probe uses a copy. No shared module-cache file or dependency manifest
  was changed.
- [Release-wrapper tests](wrapper-tests.log) pass. A copied wrapper with
  `pwd -P` changed to `pwd` [fails only the symlink input-contract case](wrapper-path-mutant.log);
  the real standalone-repository case still passes. The test uses a narrow Git
  stub for the logical path, because installed Git already canonicalizes it.
  A test overlay selects the mutated shell copy; production wrapper unchanged.
- [Source hash checks](mutation-source-check.log) confirm the SDK source,
  production wrapper, and finalized control tests were unchanged by these
  mutation runs.

## CLI and historical source verification

A freshly built `manifestd` was used for the [runbook command checks](runbook-cli.log).
Help confirms the actual command paths and pagination flags. Corrected read-only
queries, including a continuation key, parse and reach a closed loopback port;
connection refusal is expected, and no live node was contacted. A temporary
`init` fixture supplies a valid restart genesis. Explicit-path validation passes
while that home's default genesis is deliberately invalid; omitting the path
fails. No node was started and no transaction was broadcast.

[Historical source URLs and SHA-256 hashes](historical-source-hashes.log) record
store v1.0.2's timestamp field/persistence and the exact old SDK's commit-header
call. The [review response](../2026-09-10-claude-follow-up-review-response.md#historical-timestamp-evidence)
links the dependency graph and relevant code. Actual retained mainnet commit
metadata was not inspected.

This round does not rerun the complete instrumented `make coverage` pipeline,
local race/fuzz or Docker/interchain campaigns, or the eight broad review passes.
No protobuf, generated binding, or dependency changed. Baseline CI is historical
evidence; the pushed commit must run its own checks. Live policy and production
cardinality remain deployment gates.
