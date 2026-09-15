# Imported CLOSED settlement mutation evidence

These September 11 checks exercise specific-lease withdrawal from accepted
imported CLOSED leases. The fixed implementation finalizes their remaining
interval once and writes off terminal shortfalls. A zero payment does not count
as a payout or emit `provider_withdraw`; a later top-up cannot revive the interval.
Existing ACTIVE auto-close counting remains a separate lifecycle contract.

| Variant | Mutation | Expected result |
| --- | --- | --- |
| `fixed` | Unmodified implementation | Pass |
| `original` | Archived pre-fix `msg_server.go` | Fails zero-payment and batch payout counts, and subsecond finalization |
| `retained-cursor` | Advance a CLOSED cursor only when all accrued amounts were paid | Fails retry checks: a top-up permits another full charge, including denominations paid earlier |
| `skipped-zero` | Skip a zero transfer before finalizing its CLOSED interval | Fails zero-payment finalization and batch cursor checks |

Every run selects `^TestMsgWithdrawImportedClosedLease` in
`./x/billing/keeper` using Go 1.26.8, `GOMAXPROCS=4`, `-p 2`, `-count=1`,
`-timeout=180s`, and `-v`. The fixtures import an aggregate-only pre-v4
snapshot through the actual keeper's `InitGenesis`, retain an ACTIVE lease's
reserved backing, and check zero, partial, full, and multi-denomination payments,
batch accounting, subsecond finalization, and retries after funding.

[manifest.json](manifest.json) records the complete test argument arrays,
environment overrides, working directories, timestamps, exit statuses, selected
and failed tests, source/test/dependency hashes, Go module graph hash, and
driver hash. Each variant's `.go.txt`, overlay map, and zero-context patch
preserve the tested bytes. Overlay paths record the original run locations;
the driver generates new paths when reproduced. The fixed control has no patch.
The driver's own raw `sys.argv`, interpreter path, and invocation directory are
recorded separately from the resolved source and work paths.
[Independent verification](verification.json) checks the recorded hashes and
commands and reconstructs all three mutant files with `git apply --unidiff-zero`
in separate scratch repositories.

Raw output is preserved exactly as UTF-8 in each `*-raw.json`. Display logs
expand tabs to eight-column stops and strip trailing whitespace on each line
before hashing; verbose test output is otherwise retained. Both the raw output
bytes and JSON archive have recorded hashes. Source and patch bytes are never
whitespace-normalized.

To reproduce, use a source tree containing the fixed implementation and tests
matching [inputs.json](inputs.json), and choose an unused work directory:

```bash
python3 docs/reviews/2026-09-11-claude-review-validation/terminal-mutations/reproduce.py \
  . .review-tmp/reproduce-terminal-mutations .review-tmp/reproduced-terminal-evidence
```

Later comment or help changes can make the current checkout differ from those
historical input hashes. The existing [Docker source snapshot](../interchain-expiration/docker-context.tar.gz)
contains the exact required implementation, test, `go.mod` and `go.sum` bytes;
all four member hashes were [verified independently](../../2026-09-11-claude-hygiene-validation/terminal-archive-input-checks.json).
Use that snapshot as the source tree without requiring old Git objects:

```bash
mkdir -p .review-tmp
mkdir .review-tmp/terminal-historical-source
tar -xzf docs/reviews/2026-09-11-claude-review-validation/interchain-expiration/docker-context.tar.gz \
  -C .review-tmp/terminal-historical-source
python3 docs/reviews/2026-09-11-claude-review-validation/terminal-mutations/reproduce.py \
  .review-tmp/terminal-historical-source .review-tmp/reproduce-terminal-history \
  .review-tmp/reproduced-terminal-history
```

This fallback was checked against the four required archive member hashes;
the Go regression suite was not rerun for this transcript correction.

The driver reads the archived [original implementation](original.go.txt),
so neither its historical commit nor the PR branch must survive a squash merge.
It generates the other two mutants from the matching fixed source and uses
Go overlays without editing the source tree or shared SDK files. It records
the main source tree's Go-file hashes and checks that they and the pinned
source/test/dependency inputs remain unchanged throughout the run.

Python 3 and Go are required; the driver does not invoke Git. Go needs normal
module/toolchain cache access, and the SDK fixtures bind localhost sockets.
The recorded module graph is [dependency-modules.json](dependency-modules.json).
For manual patch reconstruction, use `git apply --unidiff-zero` against the
matching fixed file. Timing, fixture addresses, and resulting output hashes
can vary across executions; source/mutation hashes, selected tests, and expected
outcomes define the reproduction contract. Mutants returning exit 1 are expected
regression failures, not passing tests.
