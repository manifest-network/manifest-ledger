# September 11 review validation

Baseline: `9d253689dfbac54009dcbbed7c0345ab0cd05b44`, plus this round's
terminal-withdrawal fix, two regression files, expiration assertions, generated
comments and documentation. See the [finding dispositions and confidence](../2026-09-11-claude-review-response.md).
Only the terminal-withdrawal change alters production settlement behavior.

All display logs in this directory expand tabs to eight-column stops and trim
trailing whitespace before archiving, unless their own manifest states otherwise.
Raw mutation output is retained in the corresponding JSON archives.

## Local checks

All commands below run from the review checkout. The Go toolchain is pinned to
1.26.8; build scratch space stays in the ignored workspace directory because
host `/tmp` has little free space. Real SDK/CLI/Docker fixtures use host
permissions to bind local sockets and access normal Go caches.

```bash
export GOTOOLCHAIN=go1.26.8 GOMAXPROCS=4
export TMPDIR="$PWD/.review-tmp/build"
go test -p 2 ./... -count=1
GOFLAGS=-p=2 make sim-after-import
node --test --test-reporter=tap scripts/docs_examples.test.mjs scripts/provider_auth_examples.test.mjs
make proto-all
```

| Check | Result and evidence |
| --- | --- |
| Complete root package test suite | Passed; [full output](full-tests.log). Nested interchaintest is covered separately below. |
| Complete billing keeper suite | Passed; [output](terminal-keeper-suite.log), including existing ACTIVE, auto-close and atomicity contracts. |
| Enabled import-continuation simulation | Passed; [complete output](import-simulation.log), 92.79 seconds, 100 blocks, commit enabled, invariant period 5, seed `2507940531156952020` and next-seed import continuation. |
| SKU module gate and both CLI packages | Passed; [output](sku-gate-cli-tests.log). The new gate checks both JSON decoding and semantic import validation. |
| SKU validation bypass mutation | Expected test failure; [output](sku-gate-mutation.log). Removing `data.Validate()` fails six invalid-genesis cases. This is not a passing production test run. |
| Documentation examples/authentication | 39 passed, zero failed/skipped; [output](documentation-tests.log). An initial sandbox attempt failed to spawn child bash (`EPERM`); the complete host-permission run passed. |
| Configured full-root lint | Zero issues; [output](lint.log). Command: `golangci-lint run --concurrency 2 ./...`, v2.12.2, explicit Go 1.26.8 `GOROOT`/`PATH`, ignored review lint cache. |
| Full protobuf workflow | Passed; [output](proto-all.log). Only the intended `MsgUpdateSKU.meta_hash` comments changed in the two generated Go files; no module dependency changes. |
| Nested interchaintest compilation | Passed; [output](interchaintest-compile.log). The fresh-image execution was blocked before assertions by host Docker port-forwarding setup; see below. |

The [SKU gate provenance](sku-gate/README.md) separates the coverage run from
the uninstrumented mutation control pair and preserves their exact inputs and
commands.

The new focused SKU run measures `AppModuleBasic.ValidateGenesis` and
`DefaultGenesis` at 100% statement coverage. It does not establish 100% SKU
coverage. We did not rerun the complete instrumented `make coverage` pipeline
locally; differently scoped coverage profiles are not comparable. The baseline
PR's coverage CI passed; the pushed commit must run its own checks.

## Fresh-image interchaintest limit

The repository Dockerfile built the current production-source snapshot under
unique tag `manifest:review-pr179-sep11-9d25368`; the user's `manifest:local`
image was left intact. The focused pending-expiration test uses an overlay
changing only the fixture image tag. Docker then failed to start the faucet
helper before chain initialization and before the new assertions ran:
`iptables v1.8.13 (nf_tables): RULE_APPEND failed (No such file or directory): rule in chain DOCKER`.
The host could not install the required published-port DNAT rule. No host
firewall/network-policy change was attempted.

This is a blocked execution check, not passing e2e evidence. The stronger
factory-denomination/counter/reservation assertions compile, but their actual
chain execution remains for a working Docker host or PR CI. Build, exact test
command, source/image identity and failure evidence are archived with the
interchaintest provenance. The node's fresh image build itself passed its
static-link and version checks. See the [execution provenance](interchain-expiration/README.md)
for the exact build/test commands, source bundles, image identity, error and cleanup evidence.

## Exact CLI evidence

The checked-in [driver](../2026-09-10-claude-export-runbook-validation/verify_export_cli.py)
builds a disposable, loopback-only single-validator fixture, with generated
fixture keys, no peers and a fresh home. It queries one committed height, stops
the node and performs offline exports. It does not use an existing node home
or submit external transactions. The fixture and keys are deleted on exit.

```bash
go build -p 2 -o .review-tmp/sep11-manifestd ./cmd/manifestd
python3 docs/reviews/2026-09-10-claude-export-runbook-validation/verify_export_cli.py \
  .review-tmp/sep11-manifestd .review-tmp/sep11-export-cli.log
```

The run passed. [export-cli.log](export-cli.log) contains all 19 executed argv
arrays rendered with `shlex.join`; [export-cli.json](export-cli.json) records
the exact arrays, working directory, environment override, inherited temporary
directory, driver/binary hashes, completion timestamps, exit codes and
stdout/stderr hashes. Display commands were independently compared to all 19
manifest arrays. The node command is recorded after termination, with its
combined stdout/stderr hash, so this is completion order rather than start
order. Display logs expand tabs and trim trailing whitespace; hashes of
subprocess output refer to the original captured bytes. No full key material
is published.

The run covers command help, circuit `--page-limit`/`--page-key` and same-height
query flags, a complete empty circuit inventory versus an absent explicit
grant, stdout contamination, clean `--output-document`, source-time restart
copy validation, and explicit-path validation despite an invalid default-home
genesis. The stdout file is written by Python; the driver does not pretend to
have executed shell redirection. This is a lease-free CLI fixture; populated
billing/vesting export contracts are exercised by app regressions.

The two September 10 CLI display logs are preserved as explicitly labelled
historical illustrative summaries. Their old provenance refers to the driver
at `9d25368`; it is not the hash of the enhanced driver in this tree.

## Mutation and historical evidence

[Terminal mutation evidence](terminal-mutations/README.md) records a passing
fixed control and failing original, retained-cursor and skipped-zero mutants.
Inputs, patches, overlays, exact execution provenance and a portable driver
are archived; no shared source or SDK file is overwritten. The retained-cursor
mutant demonstrates an actual second payment for the already partially paid
interval after funding. The new regression suite fails it.

[Historical source evidence](terminal-history.log) records exact Git commands,
source hashes and relevant line excerpts at billing-v2 introduction and the
pre-PR merge base. It supports compatibility and terminal write-off semantics,
not a claim about any live database.

The earlier [export/withdrawal mutation suite](../2026-09-10-claude-follow-up-review-validation/mutation-provenance/README.md)
was rerun using `--bundled`: both controls exit 0, all three mutants exit 1,
and all 483 archived baseline files remain unchanged. Its exact 1.4 MB source
archive and historical replacement files are checked in with hashes, so a
squash merge does not require retaining the old branch. All three zero-context
patches were independently applied and matched their expected mutant hashes.
The [hash verification](bundled-hash-verification.json) also checked driver,
source/dependency, overlay, patch, raw/display log and input hashes.

## Scope and outstanding checks

Claude's eight sweeps and subsequent execution campaign are now complete as
reported in the linked review comments. They are attributed external evidence;
this round did not independently rerun every one of those probes. Full race,
fuzz, canonical coverage and production-capacity campaigns were not repeated.
The [downstream source audit](downstream-source-audit.md) identifies Fred's
eventual reconciliation backstop and a targeted integration gate; no Fred or
Barney tests, deployments or live timing claims are included. Production
snapshot replay, consumer integration, gas/circuit/RPC policy and rollout
approval remain separate gates. Nothing was merged or deployed.
