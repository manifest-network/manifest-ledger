# SKU module genesis gate evidence

The [portable driver](reproduce.py) runs an uninstrumented fixed control and a
mutation that replaces only `return data.Validate()` with `return nil` in
`AppModuleBasic.ValidateGenesis`. JSON decoding remains intact. It then measures
coverage independently against the unmodified source.

[inputs.json](inputs.json) records the module, test and dependency input hashes.
The exact [fixed source](module-fixed.go.txt), [mutant](module-skip-validation.go.txt),
[test](module-test.go.txt) and [patch](skip-validation.patch) are included. No
historical Git object is needed for these replacements. The rest of the source
tree must match the reviewed checkout and its dependency files.

The final [manifest](runs/manifest.json) records actual argv arrays, working
directory, explicit environment overrides, start/completion times, exit statuses,
overlay/source hashes and hashes of raw combined stdout/stderr. Raw JSON files
retain the exact captured bytes as base64; display logs expand tabs at 8-column
stops, normalize line endings, remove trailing whitespace and EOF blank lines,
and end with one LF. Display files have separate hashes. Other host
environment entries are inherited; credentials are not recorded. The driver
sets Go 1.26.8, `GOMAXPROCS=4`, `GOWORK=off` and `GOFLAGS=-mod=readonly`.

- [Fixed control](runs/fixed.log): exit 0.
- [Skipped-validation mutant](runs/skip-validation.log): exit 1; all six semantic
  invalid-genesis cases fail because they receive nil instead of the expected
  domain error.
- [Separate coverage run](runs/fixed-coverage.log): exit 0. The
  [raw profile](runs/fixed-coverage.out) and [function report](runs/fixed-coverage-functions.log)
  show 100% statement coverage of `ValidateGenesis` and `DefaultGenesis`, and
  24.3% of the module package for this focused filter. This does not measure
  all SKU packages or imply complete behavioral coverage.

Reproduce from a matching source checkout, choosing new scratch and output
directories:

```bash
python3 docs/reviews/2026-09-11-claude-review-validation/sku-gate/reproduce.py \
  "$PWD" "$PWD/.review-tmp/sku-gate-reproduction" "$PWD/.review-tmp/sku-gate-results"
```

An earlier [diagnostic attempt](instrumented-attempt/runs/manifest.json) combined
`-coverprofile` and the overlay for both control and mutant. It unexpectedly
returned 0 for the planned mutant, so that run is not used as evidence of
mutation efficacy. Its original [driver bytes](instrumented-attempt/attempt-driver.py.txt),
logs, overlays and [hashes](instrumented-attempt/archive-hashes.json) are retained.
Its display normalization and raw JSON archives were added afterward; its
recorded commands, driver hash, raw-byte hashes and exit statuses are unchanged.
The final uninstrumented comparison and independent coverage measurement above
avoid that tooling interaction. This observation does not identify an
application defect; the Go-toolchain interaction was not investigated further.
