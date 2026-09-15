**Review evidence — 2026-09-09**

These fixtures reproduce findings in [the review](../2026-09-09-sku-billing-review.md) against commit `aeec29e1b0e5dc452415e699838c0676efe38fba`. Passing means the recorded problematic behavior was observed; these are not assertions that the defects are fixed. The source files use `.go.txt` to keep them outside ordinary package builds.

Trailing whitespace on blank lines was normalized when archiving the script and logs; executable content and recorded results are preserved.

- `tokenfactory_credit_test.go.txt`: real tokenfactory admin burn/force transfer makes a mixed-denomination lease unclosable; an independent unaffected-denomination lease still closes.
- `state_findings_test.go.txt`: preflight/direct-v3 migration mismatch and missing-reservation invariant false negative.
- `tokenfactory-result.log`, `state-result.log`: original Go 1.26.8 results. Logged line numbers refer to the original temporary files before formatting this archive.
- `migration-example.sh.txt`, `mock-migration.sh.txt`, `migration-result.log`: the unchanged documentation example, a local mock, and its false-success result. The mock's source path records the original scratch location; use the command below to replay from this archive.
- `govuln-module.log`, `govuln-daemon.log`: raw scanner results; the report explains the daemon scan's known stale msgpack advisories and the existing exact-version policy exceptions.
- `module-tests.log`, `app-tests.log`, `doc-tests.log`, `lint.log`, `coverage-summary.txt`, and simulation logs: successful review validation. The raw module test per-package percentages use the entire `-coverpkg` scope as denominator; use the filtered summary for per-package handwritten-code coverage.

Run Go reproductions from a checkout of the reviewed commit with this evidence directory copied into it, using Bash from that checkout's root. They assert the original defects and are expected to fail after remediation. They require normal SDK test permissions, including a localhost socket. Nothing opens a live chain or sends network transactions:

```bash
review_tmp=$(mktemp -d "$PWD/.sku-billing-review.XXXXXX")
trap 'rm -rf "$review_tmp"' EXIT
python3 - "$review_tmp" <<'PY'
import json, sys
from pathlib import Path
root = Path.cwd()
evidence = root / "docs/reviews/2026-09-09-sku-billing-evidence"
replacements = {
    str(root / "x/billing/keeper/review_tokenfactory_credit_test.go"):
        str(evidence / "tokenfactory_credit_test.go.txt"),
    str(root / "x/billing/keeper/review_state_findings_test.go"):
        str(evidence / "state_findings_test.go.txt"),
}
Path(sys.argv[1], "overlay.json").write_text(json.dumps({"Replace": replacements}))
PY
GOTOOLCHAIN=go1.26.8 TMPDIR="$review_tmp" go test -p 2 \
  -overlay "$review_tmp/overlay.json" ./x/billing/keeper \
  -run '^(TestReviewTokenfactoryReservationBacking|TestReviewV3PreflightMismatch|TestReviewInvariantAcceptsMissingReservation)$' \
  -count=1 -v
```

To repeat only the mocked migration failure, use a temporary Bash script with the functions from `mock-migration.sh.txt`, replacing its final `source` line with the absolute path to `migration-example.sh.txt`. No real `manifestd` process is invoked by that fixture. All three acknowledgement calls fail but the archived example exits zero and prints completion.

Coverage run:

```bash
GOTOOLCHAIN=go1.26.8 go test -p 2 \
  ./x/sku/... ./x/billing/... ./pkg/... ./internal/... -count=1 \
  -coverpkg=./x/sku/...,./x/billing/...,./pkg/...,./internal/... \
  -coverprofile=/tmp/sku-billing-review.cover
```

Coverage summary excludes `*.pb.go`, `*.pb.gw.go`, and `*.pulsar.go`, counts statements, and merges repeated source blocks as covered if any included suite executed them. Application simulations, Docker tests, and the temporary reproductions do not contribute to these percentages.
