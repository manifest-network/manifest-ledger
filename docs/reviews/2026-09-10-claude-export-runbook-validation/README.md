# Export/runbook follow-up validation — 2026-09-10

Baseline: `b4d8403`, plus this round's first-Commit guard, tests, and docs.
The only production change is the explicit guard in `app/export.go`.

## App and documentation checks

```bash
mkdir -p .review-tmp/build
export GOTOOLCHAIN=go1.26.8 GOMAXPROCS=4
export TMPDIR="$PWD/.review-tmp/build"
go test -p 2 ./app -count=1
GOFLAGS=-p=2 make sim-after-import
node --test --test-reporter=tap scripts/docs_examples.test.mjs scripts/provider_auth_examples.test.mjs
```

- [Full app suite](app-tests.log): **passed**, 5.050 seconds.
- [Focused zero-height tests](export-tests.log): **passed**, including both
  `InitialHeight=0` and `InitialHeight=1`, rejection after InitChain and after
  the first FinalizeBlock, and successful export after Commit. All earlier
  vesting, timestamp, historical-height, and rollback regressions remain green.
  The [source/command notes](export-source.log) record the pinned SDK mechanics.
- [Enabled import simulation](import-simulation-summary.log): **passed**, test
  143.28 seconds, seed `2507940531156952020`, 100 blocks, commit enabled,
  invariant period 5, unchanged `simulation/sim_params.json`. The harness uses
  the next seed for import continuation. Parameter/progress output is omitted
  from the explicitly labelled summary.
- [Documentation tests](documentation-tests.log): **39 passed**, none failed or skipped.
- [Configured full-repository lint](lint.log): **0 issues** with golangci-lint
  2.12.2 and `run --concurrency 2 ./...`. `GOROOT` and `PATH` point at Go 1.26.8;
  `GOLANGCI_LINT_CACHE` uses the existing isolated review cache. Go 1.27 was not
  used for lint.

## Real CLI export and absent-grant check

[verify_export_cli.py](verify_export_cli.py) is the complete fixture driver.
It creates a disposable single-validator testnet, starts it only on loopback
with no peers, observes at least three committed blocks, queries the circuit at
one recorded height, stops the node, then exports through fresh CLI processes.
It starts no validator from an existing home, broadcasts no transaction, and
removes its generated test keys and database on completion.

```bash
go build -p 2 -o .review-tmp/round70-manifestd ./cmd/manifestd
python3 docs/reviews/2026-09-10-claude-export-runbook-validation/verify_export_cli.py \
  .review-tmp/round70-manifestd .review-tmp/round70-export-cli.log
```

The [CLI log](export-cli.log) records the selected block/time, complete empty
permission inventory, missing-account error, and both output paths. Default
stdout export exits successfully but fails JSON parsing and genesis validation
because invariant INFO logs precede the document. `--output-document` produces
valid JSON while logs remain on stdout. The raw export retains its original
`genesis_time`; a separate restart copy uses source block time and passes
`manifestd genesis validate`. The raw export's hash stays unchanged.

An absent explicit circuit permission returns `InvalidArgument` and a nonzero
exit, while the same-height complete inventory is empty. This is a local test
of the documented interpretation, not a live permission audit. The fixture has
no billing leases; populated billing/vesting export behavior is covered by the
app regressions, not claimed from this CLI check.

[CLI provenance](cli-provenance.log) records the exact build/run commands,
source/script/binary hashes, and exit status. Archived display logs expand tabs
and strip trailing whitespace; no test lines are filtered except in the labelled
simulation summary.

## Corrected earlier mutation evidence

The [earlier validation directory](../2026-09-10-claude-follow-up-review-validation/README.md)
now identifies its three replaced logs as exact-`b4d8403` reruns, with successful
controls, verbose filters, patches, overlay maps, hashes, exit statuses, and a
reconstruction driver. The earlier unrelated suite/control artifacts remain
historical evidence with their original scope.

This round does not claim another whole-root test run, full instrumented
coverage pipeline, race/fuzz campaign, Docker/interchain rehearsal, live audit,
or completion of the eight broader review passes. No dependencies, protobuf,
generated bindings, live policy, or deployment changed. The new commit must
run its own CI.
