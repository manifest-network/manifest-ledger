#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
: "${GOVULNCHECK:?Set GOVULNCHECK to the pinned scanner executable}"
mkdir -p build/vulnerability
# Match .goreleaser.yaml, including the hardware-wallet dependency graph.
# Keep the raw report for module/package findings and exception review.
"$GOVULNCHECK" -json -tags=netgo,ledger ./cmd/manifestd > build/vulnerability/govulncheck.json
python3 scripts/check-vulnerability-report.py \
  build/vulnerability/govulncheck.json scripts/govulncheck-exceptions.json
