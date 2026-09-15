# Export/simulation follow-up validation — 2026-09-10

Baseline: PR179 `3ed262e`. Go 1.26.8, Node 24.15.0, golangci-lint 2.12.2.
Root commands used `GOTOOLCHAIN=go1.26.8`, `GOMAXPROCS=4`, and a workspace-local
`TMPDIR` because `/tmp` was nearly full. Archived whitespace is normalized;
simulation summaries explicitly omit parameter dumps and operation progress.

## Previously failing CI entrypoints

Both baseline failures were independently read from GitHub:
[simulation](https://github.com/manifest-network/manifest-ledger/actions/runs/34479117166/job/102877160868)
and [Codecov](https://github.com/manifest-network/manifest-ledger/actions/runs/34479117250/job/102877161548).
Both panic in zero-height export with a valid lease timestamp in 2075 compared
against Go's zero block time. Ordinary unit tests do not enable this simulation.

```bash
make sim-after-import
```

**Passed**, [summary](import-simulation-summary.log), 109.48 seconds. This uses
the unchanged release seed `2507940531156952020`, 100 blocks, commit enabled,
invariant period 5, and `simulation/sim_params.json`. Import continuation uses
the harness's next seed and source timestamp.

The Codecov failure was reproduced at its instrumented simulation entrypoint,
using the same coverage flags and test invocation as `make coverage`:

```bash
go test -p 2 -c ./app -mod=readonly -covermode=atomic \
  -coverpkg=github.com/manifest-network/manifest-ledger/... -cover \
  -o .review-tmp/next-simulation-cover.test
.review-tmp/next-simulation-cover.test \
  -test.run '^TestAppSimulationAfterImport$' -Enabled=True -NumBlocks=100 \
  -Commit=true -Period=5 -Params="$PWD/simulation/sim_params.json" \
  -Verbose=false -Seed=2507940531156952020 -test.v \
  -test.gocoverdir="$PWD/.review-tmp/next-simulation-cover"
```

**Passed**, [summary](covered-import-summary.log), 111.00 seconds. Its 22.5%
statement coverage covers this one simulation over the whole instrumented root
module; it is not an aggregate billing/SKU coverage measurement. The full
`make coverage` target's later race/interchaintest phases were not run locally.

## Root packages and static checks

```bash
go test -p 2 ./... -count=1
go test ./scripts -count=1
golangci-lint run --concurrency 2 ./...
node --test --test-reporter=tap scripts/docs_examples.test.mjs scripts/provider_auth_examples.test.mjs
```

The [initial root run](root-tests-initial.log) passed all packages except an
existing release-wrapper fixture that required a `.git` directory in the
current checkout. This linked worktree uses a `.git` file. The test now creates
its own ordinary Git repo and invokes the unchanged wrapper there; the
[complete scripts rerun passes](scripts-final.log). Together these cover all
root packages on the final implementation; the first command itself exited
unsuccessfully. Billing keeper completed in 108.213 seconds and SKU keeper in
21.058 seconds under concurrent host load.

[Final lint](lint.log): **0 issues**. [Documentation tests](documentation-tests.log):
**39 passed**, none failed or skipped. Public protobuf and generated bindings
were not changed in this follow-up, so no new regeneration is required.

## Targeted behavior and mutation proofs

- [Zero-height export tests](export-tests.log) pass for populated
  PENDING/ACTIVE/CLOSED state with matured delayed-vesting credit, both in
  process and after reopening latest/selected historical state. Future
  corruption still fails at the selected height. Source-time import succeeds;
  original-genesis-time import fails timestamp validation or relocked backing.
  Real SDK rollback followed by reopen produces the explicit missing-time error,
  while ordinary export succeeds.
- [Simulation control tests](simulation-controls.log) use only valid signed
  transactions from fixture-owned keys and finite positive budgets. Six cases
  verify explicit Wasm limits, block-gas fallback, explicit-limit precedence,
  exhaustion, and direct circuit rejection before invariant work. Actual gRPC
  service routing leaves owned balances, fee collector, sequence, and billing
  state unchanged on success and failure.
- The direct caller-meter regression compares empty state with 16 valid credit
  accounts and checks budget exhaustion. A read-only overlay giving the invariant
  an infinite meter produces the [expected failure](meter-mutant.log); ante
  overhead cannot satisfy this isolated assertion.
- Restoring the original AutoClose/preflight helpers through a read-only overlay
  produces [expected regression failures](autoclose-preflight-mutants.log): caller
  lease/reservation mutation after a late failure and import-safe allowed-list
  aliases rejected as duplicate parameters. The fixed tests pass in the full
  keeper suite. No shared production source was reverted during these probes.

An automated cybersecurity review stopped the delegated unsigned-simulation
reproduction. It was not retried; that behavior is source analysis, while the
bounded signed tests validate specific controls. No unsigned/impersonated
requests or composed permission-change bypass were executed. No live network
policy, circuit grant, endpoint configuration, or application deployment changed.

This round does not add a full race/fuzz campaign, Docker/interchain rehearsal,
production-cardinality measurement, every original cross-cutting hunt, or a
claim that all vulnerabilities have been ruled out. The pushed commit must run
its own full CI.
