# Remediation validation — 2026-09-09

These results validate the local fixes described in [the review](../2026-09-09-sku-billing-review.md), tracked in [ENG-917](https://linear.app/liftedinit/issue/ENG-917), on branch `codex/sku-billing-review-20260909`. The sibling `evidence` directory preserves reproductions against the original `aeec29e` baseline. These logs do not represent a deployment or a production capacity measurement.

Commands were run from the isolated worktree root with the pinned Go 1.26.8 toolchain, golangci-lint 2.12.2 and Node 24. SDK app fixtures require local sockets; the executable Bash/jq documentation fixtures require subprocess access. Go work and test temporary files used a workspace directory because `/tmp` was nearly full. Initial sandbox lint could not load Go packages; the final successful lint ran outside that sandbox after correcting one new test import-order issue.

```bash
export GOTOOLCHAIN=go1.26.8
export TMPDIR="$PWD/.review-tmp/build"
mkdir -p "$TMPDIR"

go test -p 2 ./x/billing/... ./pkg/... ./internal/... ./app/... ./cmd/manifestd/cmd -count=1 -timeout=10m
go test -p 2 ./x/sku/... -count=1 -timeout=5m
golangci-lint run --concurrency 2 ./x/sku/... ./x/billing/... ./pkg/... ./internal/... ./app/... ./cmd/manifestd/cmd
node --test --test-reporter=tap scripts/docs_examples.test.mjs scripts/provider_auth_examples.test.mjs

go test -p 2 ./app -run '^TestAppSimulationAfterImport$' -count=1 -timeout=15m \
  -Enabled=true -Seed=2507940531156952020 -NumBlocks=100 -BlockSize=50 \
  -Period=5 -Commit=true -Params=../simulation/sim_params.json

go test -p 2 ./x/billing/types -run '^$' -fuzz '^FuzzPlanReservationSpend$' -fuzztime=15s -parallel=2
go test -p 2 ./x/billing/keeper -run '^$' -fuzz '^FuzzBillingStorageValueCodecs$' -fuzztime=15s -parallel=2
```

The lint invocation also explicitly set `GOROOT` and prepended the cached Go 1.26.8 binary directory to `PATH`, since the host defaults to Go 1.27.1. Adjust those machine-specific locations when reproducing. The fuzz logs contain engine output from the bounded campaigns; their first instrumented build is substantially slower than subsequent runs.

| Evidence | Result |
| --- | --- |
| `remediation-go-tests.log` | Billing/shared/app/command suites passed |
| `remediation-sku-tests.log` | Complete SKU suite passed, including real CLI message generation and 151-SKU cascade/query checks |
| `remediation-lint.log` | Zero issues |
| `remediation-doc-tests.log` | 36 tests passed; no skips |
| `remediation-simulation.log` | 100-block simulation followed by export/import and 100-block continuation passed |
| `accounting-spend-fuzz.log` | 21,444 generated executions; no failures |
| `accounting-storage-fuzz.log` | 20,060 generated executions; no failures |

The credit guard is exercised through real tokenfactory handlers and billing settlement, plus an index-failure spy test. Accounting tests check the original all-nil reservation bypass, exact stored counts/params and retained import compatibility. The new fuzz properties check conservation, protected sibling claims, payment limits, codec fixed points and input immutability. Provider authentication tests are serialization/context vectors; actual off-chain cryptographic verifier and wallet interoperability remains part of ENG-925.

Coverage percentages in the review remain the explicitly labeled baseline measurement. This remediation did not generate new coverage percentages, rerun a full race/Docker suite, measure production capacity, or establish that no other vulnerabilities exist. Wider test infrastructure and capacity work remain ENG-869 and ENG-890.
