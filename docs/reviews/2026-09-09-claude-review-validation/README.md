# Claude feedback validation — 2026-09-09

This directory records validation for the [PR179 review response](../2026-09-09-claude-review-response.md). Tests ran in the isolated `codex/sku-billing-review-20260909` worktree with Go 1.26.8, golangci-lint 2.12.2 and Node 24.15.0. SDK app fixtures required localhost sockets; Bash/jq recipes required subprocess access. A workspace TMPDIR avoided the nearly full system temporary partition. Archived logs normalize carriage-return progress lines and trailing whitespace; result text is preserved.

## CI formatter correction

The first pushed head `ad85c7b` passed local generation-only checks but failed
CI's preceding clang-format step on new protobuf comments. The follow-up uses
the complete `make proto-all` workflow (format, lint, generation, module tidy).
A second complete run is idempotent across every tracked file; see
`feedback-proto-all-repeat.log`. The correction changes comments only, verified
by comparing source and generated files with comment lines removed. Go.mod,
Go.sum, descriptors, fields and executable code are unchanged. The Go, race,
fuzz, documentation and simulation results below apply to the same executable
code; they were not redundantly rerun for comment wrapping.

## Final checks

```bash
export GOTOOLCHAIN=go1.26.8
export TMPDIR="$PWD/.review-tmp/build"
mkdir -p "$TMPDIR"

go test -p 2 ./x/sku/... ./x/billing/... ./pkg/... ./internal/... ./app/... ./cmd/manifestd/cmd \
  -count=1 -timeout=10m -coverprofile=.review-tmp/feedback-final.cover
make proto-all
golangci-lint run --concurrency 2 ./x/sku/... ./x/billing/... ./pkg/... ./internal/... ./app/... ./cmd/manifestd/cmd
node --test --test-reporter=tap scripts/docs_examples.test.mjs scripts/provider_auth_examples.test.mjs

go test -p 2 ./x/billing/keeper \
  -run 'Test(PermanentLockedCredit|CreditBalanceTracksVesting|VestingReservation|ReservationMigrationPreflightRejectsMalformedAuth)' \
  -count=1 -race -timeout=5m
go test -p 2 ./x/billing/keeper -run '^$' -fuzz '^FuzzBillingStorageValueCodecs$' \
  -fuzztime=15s -parallel=2

# Run once per seed, 1436702331596064385 and 622462849479825385:
go test -p 2 ./app -run '^TestFullAppSimulation$' -count=1 -timeout=15m \
  -Enabled=true -Seed=1436702331596064385 -NumBlocks=100 -BlockSize=50 \
  -Period=5 -Commit=true -Params=../simulation/sim_params.json
```

Lint additionally set GOROOT and PATH to the cached Go 1.26.8 toolchain because the host defaults to Go 1.27.1. Pinned protobuf generation used `ghcr.io/cosmos/proto-builder:0.14.0@sha256:93e2035b90e5780b4d56210a88ecb0afed881c7bb828285d4a61a897cebb54fb` and `scripts/protocgen.sh`. A second generation in scratch storage produced 37 Go files byte-identical to the worktree. All 13 changed public generated files differ only in comments.

| Evidence | Scope |
| --- | --- |
| `feedback-go-tests-final.log` | Complete affected Go suites; raw percentages include generated code |
| `coverage-summary.txt` | Statement counts excluding generated protobuf, gateway and Pulsar code; simulation campaigns are separate and not merged into this profile |
| `feedback-lint-final.log` | Zero scoped lint issues |
| `feedback-docs-final.log` | All 39 executable documentation tests passed, no skips |
| `feedback-vesting-race.log` | Focused vesting/credit/import-migration regressions under the race detector |
| `feedback-storage-fuzz.log` | Native byte-codec fuzz engine results; current/legacy representations and fixed points |
| `feedback-proto-recheck.log` | Initial generation-only comparison, preceding the formatter correction |
| `feedback-proto-all-repeat.log` | Complete CI protobuf workflow, repeated with no tracked-file drift |
| `feedback-json.log` | Actual SDK default proto-JSON versus standard Go JSON for an unset Provider |
| `simulation-claude-seed-*.log` | Two fresh committed 100-block runs, invariant period 5; each delivered all 10 billing and 7 SKU message types successfully |

Malformed auth-input, index-work bounds/corruption, expiry progress, lifecycle state, zero-transfer settlement, event attribution, CLI RPC/gRPC snapshots and stable ABCI code assertions are part of the committed Go tests. Independent agents checked the spendable-credit and expiry changes, then reproduced the malformed-auth error handling and the transport-specific CLI fixes.

## Original-balance reproduction

`original-balance-reproduction.log` is a **bug reproduction**, not desired behavior. The accompanying `.go.txt` files are excluded from normal package builds. A read-only Go overlay replaces the corrected balance helper with total-balance semantics and injects one test. It does not claim to rebuild every file at historical commit `0307b4b`.

The test creates a real SDK permanently locked account at a prospective credit address with one unit, funds 7,200 unlocked units, opens/acknowledges a 1-unit/second lease, and attempts closure after three hours. Old accounting attempts 7,201 units against 7,200 spendable units and fails. Another 3,600 unlocked units lets it pay the 10,800 units accrued and close, disproving permanent irrecoverability. The current normal tests verify closure without that extra funding and protection of a sibling reservation.

Reproduce only the old-accounting assertion from the repository root:

```bash
python3 - <<'PY'
import json
from pathlib import Path
root = Path.cwd()
assert (root / 'go.mod').is_file()
evidence = root / 'docs/reviews/2026-09-09-claude-review-validation'
scratch = root / '.review-tmp'
scratch.mkdir(exist_ok=True)
overlay = {'Replace': {
    str(root / 'x/billing/keeper/spendable_credit.go'): str(evidence / 'old_spendable_credit.go.txt'),
    str(root / 'x/billing/keeper/zz_review_locked_close_probe_test.go'): str(evidence / 'locked_close_probe_test.go.txt'),
}}
(scratch / 'claude-balance-overlay.json').write_text(json.dumps(overlay))
PY
GOTOOLCHAIN=go1.26.8 TMPDIR="$PWD/.review-tmp/build" go test \
  -overlay .review-tmp/claude-balance-overlay.json ./x/billing/keeper \
  -run '^TestReviewProbeOriginalLockedCreditAndRecovery$' -count=1 -v
```

## Limits

The coverage summary describes this command and does not merge race, fuzz, simulation campaign, Docker or external integration execution. Keeper coverage is stronger than CLI/simulator registration coverage; percentages do not establish adversarial completeness. Default `go test -cover`
instruments each tested package, not all of its dependencies; helper/type code
exercised through keeper fixtures is not credited to those other packages.
In particular, the collections marker validator is exercised by keeper
corruption tests despite its low standalone-package percentage. Further CLI generation/error matrices, broader stateful fuzzing and production-cardinality measurements remain useful.

This follow-up did not rerun full-module race suites, export/import continuation simulations, release builds, Docker/interchain upgrade rehearsals, live-chain fee/gas configuration, or real provider/wallet authentication interoperability. Those remain separately scoped prior evidence or rollout gates. The final head must pass CI; failed previous-head builds were independently traced to HTTP/2 download errors at sum.golang.org and proxy.golang.org. No deployment or security-completeness guarantee is implied.
