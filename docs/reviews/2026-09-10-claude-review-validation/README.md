# Claude follow-up validation — 2026-09-10

These checks validate the follow-up to PR179 head `7730e70`. Commands use Go
1.26.8; Node is 24.15.0 and golangci-lint is 2.12.2. Build temporary files use
`.review-tmp/build`. Real-app Go fixtures and documentation subprocess tests
ran outside the restricted sandbox. Archived log whitespace is normalized. An initial sandboxed Node invocation
stalled without output and was interrupted; the completed run below passed.

## Package suites

```bash
GOTOOLCHAIN=go1.26.8 go test -p 3 ./x/billing/... ./x/sku/... ./app/... ./cmd/manifestd/cmd ./internal/... -count=1
```

The [initial affected-suite run](affected-suites-initial.log) passed every
package except `x/billing/keeper`: four payout-test subcases reused a block
context from before their final close, which the new timestamp invariant
correctly rejected. The fixture now retains the close time for its final
invariant assertion. All original state/event/repair assertions remain.

The full keeper package was then rerun on the final source:

```bash
GOTOOLCHAIN=go1.26.8 go test -p 2 ./x/billing/keeper -count=1
```

Result: see [final keeper run](keeper-final.log). The changed address-identity
parity test also passed after lint cleanup; schema/preflight command tests
passed in the initial complete command-package run. This is a combined passing
package result, not a claim that the first command exited successfully.

New checked-in regressions cover:

- 101 expiration candidates with a failing row at the end of the first page;
- caller-memory and store rollback on missing credit, reservation-release
  failure, and a late corrupt domain-index failure;
- CLOSED chronology, equality, and historical partially settled imports;
- current block-time invariant bounds without import repair;
- overdue PENDING domain claim/re-set rejection, exact deadline and late clear;
- existing pending offers after real provider/SKU deactivation;
- lease-free state classification and orphan accounting rejection;
- shared genesis/message item-shape validation and SDK address-decoder parity;
- cross-provider cancellation and ownership-before-state error precedence.

## Crisis fee and circuit control

```bash
GOTOOLCHAIN=go1.26.8 go test ./app -run 'TestCrisisFeeRollsBack|TestCrisisCircuitBreaker' -count=1 -v
```

[Result: pass](crisis-regression.log). Real signed transactions execute the
`billing/reservation-accounting` route and commit state through ManifestApp:

| Transaction | Gas used | Tokens paid (umfx) |
| --- | ---: | ---: |
| Verification alone, ante fee 17 and crisis fee 1,000 | 87,907 | 1,017 |
| Verification followed by failing send, ante fee 17 | 96,058 | 17 |
| Same failing-tail pattern, zero ante fee | 78,633 | 0 |

The failed sends fail at message index 1, after actual verification. Sender
sequence advances, the recipient receives zero, and committed balances show
only ante fees persist. A nonzero local minimum gas price rejects the zero-fee
transaction at CheckTx; FinalizeBlock still executes those exact signed bytes.

The circuit regression calls the authorized trip/reset handlers through the
real router. Disabled crisis messages fail before their fee debit, both direct
and nested in an allowed authz `MsgExec`; unrelated bank send succeeds and reset
restores verification. Group/governance coverage is source analysis of shared
router wiring, not a dedicated signed group/governance integration test.

## Cursor mutation proof

A read-only Go overlay changes only the pagination continuation from
`StartExclusive(cursor)` to `StartInclusive(cursor)`. The new boundary test
uses a finite 20,000,000 gas budget. Correct source uses 6,557,353 gas and
terminates with 100 successful expirations; the
[inclusive mutant fails](inclusive-cursor-mutant.log) in 0.35 seconds with SDK
out-of-gas. This expected failure proves the regression detects the nonprogress
loop without altering shared source or leaking a watchdog goroutine. Wall time
and gas are fixture measurements, not production capacity estimates.

## Lint, documentation, generation

- [Scoped configured lint](lint.log): **0 issues** across billing/SKU, app,
  daemon command, and shared internal packages. Lint cleanup uses a switch for
  preflight classification, integer-safe test labels, and a seeded standard
  library byte-stream generator for the address corpus.
- [Executable documentation tests](documentation-tests.log): **39 passed**,
  zero failed/skipped, running both `scripts/docs_examples.test.mjs` and
  `scripts/provider_auth_examples.test.mjs`. New operator command names were
  additionally checked against the pinned SDK AutoCLI descriptors; they were
  not submitted to a live network.
- `make proto-all` completed twice using CI's pinned proto-builder 0.14.0
  digest. [Repeat output](proto-repeat.log); tracked-file SHA-256 comparison
  before/after the second run found **zero changed files**. Formatter, proto
  lint, generation and `go mod tidy` are included. Generated Go diffs are
  comments only; no field/descriptor/storage-format changes.
- `git diff --check`: clean.
- CodeQL alert [52](https://github.com/manifest-network/manifest-ledger/security/code-scanning/52)
  was dismissed as a false positive with a construction-only reflection
  rationale; alerts 49/50/51 were already fixed at the prior head.

No new aggregate coverage, full race/fuzz campaign, Docker/interchaintest,
live-network policy audit, or production-cardinality rehearsal was performed
in this follow-up. The [earlier coverage and campaigns](../2026-09-09-claude-review-validation/README.md)
remain historical evidence. CI must validate the pushed head independently.
