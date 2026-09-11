# Review hygiene validation — September 11, 2026

Baseline: `8492a4ad18996d0877a7425b8039ed355cc94726`, plus this follow-up's
help text, comments, documentation and evidence-tool changes. See the
[findings and confidence scores](../2026-09-11-claude-hygiene-response.md).
No settlement logic, dependency version, protobuf field or generated binding changed.

## Product help and documentation

The [exact shell commands and exit statuses](product-commands.log) record:

- `go test -p 2 -count=1 ./x/sku/client/cli`: passed; [output](sku-cli-tests.log).
- Provider-update keeper/types tests selected with
  `^Test(UpdateProvider|MsgUpdateProvider)`: passed; [output](provider-tests.log).
- Both executable documentation suites: 39 passed, zero failures/skips;
  [output](documentation-tests.log).
- A fresh CLI's `update-provider --help`: passed and rendered the preserve/clear
  rules; [output](update-provider-help.log). SDK initialization requires a local
  listener even for this command; a sandboxed attempt failed before help
  rendered, and host-permission execution passed.

These Go commands use `GOTOOLCHAIN=go1.26.8`, `GOMAXPROCS=4`, and workspace
`TMPDIR`. The CLI test wrapper remained alive after printing its package result,
then exited 0 without cancellation or rerun. No telemetry configuration changed.
The configured full-root linter also passed with zero issues; [output](lint.log).
Its exact command, binary/source hashes and display-log hashes are in
[local-checks.json](local-checks.json). Display logs expand tabs to eight-column
stops, trim trailing whitespace and end with one LF. Tests appropriate to these
help/comment changes passed; the complete root, simulation, coverage and Docker
suites were not rerun locally for this follow-up.

## CLI verification guard

The [driver](../2026-09-10-claude-export-runbook-validation/verify_export_cli.py)
uses test assertions, so it now explicitly refuses optimized execution.
The [guard probes](cli-optimization-guard.json) record rejection under `-O`,
`-OO` and `PYTHONOPTIMIZE=1`, before arguments or a binary can be accessed.
Ordinary execution passed against the newly built CLI and a disposable
loopback-only, lease-free node fixture. It generated its own keys, had no peers,
queried one committed height, stopped the node and validated offline exports.
The generated home and keys were removed on exit.

[export-cli.json](export-cli.json) records all 19 actual subprocess argv arrays,
working directory, environment override, driver/binary hashes, output hashes and
exit codes. Every rendered command in [export-cli.log](export-cli.log) was checked
against its recorded argv. The fixture covers the same explicit-home/path,
circuit inventory, clean output-document and source-time restart contracts as
the prior run. It does not claim populated-chain or live-operator validation.
The older September 11 CLI manifest refers to the driver at `8492a4a`; this is
the authoritative manifest for the new optimization guard and this binary.

## Optimized integrity checks

The bundled [reproduction driver](../2026-09-10-claude-follow-up-review-validation/mutation-provenance/reproduce.py)
uses explicit checks that run under normal and optimized Python. Its
[integrity regression driver](../2026-09-10-claude-follow-up-review-validation/mutation-provenance/check_integrity.py)
passed 24 bounded cases: 14 altered-input cases rejected before any Go invocation,
and 10 cases using a local Go stub to exercise wrong outcomes, missing execution
or failure evidence, and changed source/driver checks. The stub checks are tests
of the validation tooling, not executions of Go regressions. See the
[exact records](integrity/checks.json) and [display summary](integrity/checks.log).

A separate real `python3 -O reproduce.py --bundled` run succeeded: both controls
passed, all three mutants failed as intended, and all 483 baseline source files
remained unchanged. Existing output artifacts were refreshed with their actual
commands, maps, raw/display output and hashes. The original patches and all
three committed source archives remain byte-identical. The
[artifact verification](integrity/artifact-checks.json) checked 22 recorded
hashes, exact command construction and source/test/dependency inputs; a second
archive-only verification also passed without access to old execution paths.
See the [reproduction instructions](../2026-09-10-claude-follow-up-review-validation/mutation-provenance/README.md).

## Complete patch transcripts

[Patch reconstruction instructions](PATCH_RECONSTRUCTION.md) explain both
corrected records and their shared verifier. Each record includes all five
steps: initialize a fresh repository, seed and verify its baseline, verify the
patch, apply it, and verify the reconstructed result. The baseline is an existing
archive member or archived fixed source, not an undocumented file copied from
the current checkout.

All six reconstructions and their literal replays at new paths succeeded under
`python3 -O` (60 recorded successful steps). Wrong-baseline controls rejected
before writing. The parent reviewer then independently replayed all six
records again at new paths and ran archive-only artifact verification; all
commands exited 0. The terminal record's original Go/source verification remains
explicitly historical. Its unchanged source can be recovered from the already
committed Docker context archive; the four required source/test/dependency
member hashes were checked. No historical Git object or new archive is needed.

## CI closes the prior expiration gap

[ci-status.json](ci-status.json) captures all 31 successful checks for the
reviewed head and identifies the billing-state job. Its [selected log lines](billing-state-ci-excerpt.log)
show checkout of merge `6bd1d06` (PR `8492a4a` into `9b14229`) and actual RUN/PASS
output for the new factory-denomination/reservation expiration assertions.
This establishes chain execution in CI, while preserving the previously recorded
local Docker DNAT failure. No host firewall change or new local Docker run occurred.
The pushed follow-up commit must run its own checks. Live rollout and Fred/Barney
integration remain separate work.
