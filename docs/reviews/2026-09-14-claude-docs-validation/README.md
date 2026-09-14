# Documentation validation — September 14, 2026

Reviewed baseline: `9659d1641ad6f6e971c9cd5ceebafe2cdba3d4b5`.
See [findings and confidence](../2026-09-14-claude-docs-response.md).
[checks.json](checks.json) records the focused Go command, its exit status,
final source hashes and raw/display log hashes. Display logs expand tabs to
eight-column stops, trim trailing whitespace and end with one LF.

## Executable frontend example

```bash
timeout 180s node --test --test-reporter=tap \
  scripts/docs_examples.test.mjs scripts/provider_auth_examples.test.mjs
```

The [final run](documentation-tests.log) passed all **43 tests**, with no skips.
The new regression executes the actual fenced clear-provider-URL example with
nonempty binary metadata, an inactive provider, and empty metadata. It checks
the queried UUID and every field passed to the message composer, including
unchanged metadata bytes and active state. It captures composer inputs using
stubs; it does not encode or broadcast a JavaScript SDK message.

Two isolated mutations confirmed the regression distinguishes the faulty
examples. Copy `scripts/docs_examples.test.mjs` and `docs/FRONTEND.md` into
matching `scripts/` and `docs/` paths under a fresh fixture directory. In the
document, replace exactly one `metaHash: currentProvider.metaHash,` with
`metaHash: new Uint8Array(),`, or, in a separate fixture, replace exactly one
`active: currentProvider.active,` with `active: true,`. Run that fixture's test
file with `--test-name-pattern='frontend provider API URL clearing'`.
Both runs exited 1 as expected: the [metadata mutant](metadata-mutant.log)
failed both nonempty fixtures, while the [active-state mutant](active-mutant.log)
failed the inactive-provider fixture.

[Exact commands and statuses](documentation-commands.log) also record the initial
harness failure, corrected by placing the wrapper's return after the fenced
snippet's trailing comment on a new line; its [initial output](documentation-initial-attempt.log)
is retained. The spendable-credit query comments were edited after the final
test run; they are outside the exercised section. The provider snippet and
test source remained unchanged afterward.

## Protobuf and Go behavior

Full `make proto-all` passed with Go 1.26.8 and workspace `TMPDIR`:
[command](proto-command.json), [output](proto-all.log).
Only the intended source proto and two generated Go comments changed.
The [verification](proto-verification.json) checks unchanged non-comment proto
lines, Go dependency files, and generated tokens, including descriptor literals.

The [scanner source](generated-token-check.go.txt), [original command](generated-token-command.json)
and [output](generated-tokens.log) are retained. An independent
[repeat comparison](generated-token-replay.json) records directory setup,
verifier copying, both baseline-seeding `git show` commands, baseline hashes
and actual comparison argv/output. It found **18,992 gogo and 43,253 pulsar
tokens identical**. Repeating that before/after comparison requires the named
baseline Git object; this record makes no additional archive-based reproduction
claim. No historical mutation archive or transcript was changed.

The focused existing provider-update keeper/types/CLI tests and imported-CLOSED
settlement regressions all passed: [output](focused-go.log).
Their SDK fixtures ran with host process permissions for local listeners.
The full root suite, lint, simulations, coverage and Docker were not rerun for
these documentation and comment changes. All **31 baseline CI checks passed**
([record](baseline-ci.json)); the pushed follow-up runs its own CI.
