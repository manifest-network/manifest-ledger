# PR179 export/runbook review — 2026-09-10

This addresses Claude's [audit of `b4d8403`](https://github.com/manifest-network/manifest-ledger/pull/179#issuecomment-5623555111).
The previous production fixes remain intact. No new critical, high, or medium
finding was established; the remaining items concern an embedded export API
boundary, operator commands, and reproducible validation evidence.
Confidence describes the disposition, not a guarantee that no defects remain.

| Finding | Disposition | Confidence |
| --- | --- | --- |
| Height-match guard also rejects pre-first-Commit zero-height export | **Made explicit and consistent; low embedded-API limitation.** Require a committed source block before zero-height preparation. `InitialHeight=0` previously bypassed the mismatch because both heights were zero; `InitialHeight=1` was rejected. Both now return the same descriptive error after InitChain and after the first FinalizeBlock, and export succeeds after Commit. This deliberately chooses the committed-state contract rather than preserving initialization-dependent behavior. No shipped export path initializes an uncommitted app; ordinary export is unchanged. | 99% |
| Restart recipe omits the export command and stdout logging corrupts redirected JSON | **Fixed; low operator issue.** Both guides now name `manifestd export … --output-document …`, distinguish normal and zero-height export, retain the untouched export, and validate a separate restart copy using source block time. A disposable single-validator CLI fixture reproduces contaminated stdout and successful file-output validation. The SDK output flag changes where JSON is written; it does not update `genesis_time`. | 100% |
| Three mutant logs lack exact command/filter and mutation provenance | **Evidence corrected.** Rerun in an archive of exact baseline `b4d8403`, including successful controls. Archive the reconstruction driver, full patches, overlay maps, source hashes, exact verbose commands, filters, and exit statuses. Withdrawal filters now include both late-failure and success cases. The previous narrower/non-verbose logs did not establish those exact commands and should not have been presented as if they did. | 100% |
| Single-account circuit query fails when no grant is stored | **Documentation clarified.** Full paginated inventory remains required; a single-account lookup is optional. Classify a missing permission record as no explicit grant only when complete same-height inventory corroborates it. `InvalidArgument` alone does not distinguish absence from other failures, and an absent explicit grant does not remove module-authority powers. The isolated CLI fixture confirms the error and empty inventory at one height. | 100% |
| Two remaining actionable `validate-genesis` references are ambiguous | **Fixed; documentation only.** Use the full canonical command with an explicit file path in both places, and normalize descriptive mentions to static genesis validation. The bare prose was ambiguous, rather than proof that every alias reference was invalid: `manifestd genesis validate-genesis [file]` remains a valid SDK alias beneath `genesis`. | 99% |

Zero-height export now consistently requires at least one committed block,
matching its existing source-height/timestamp recovery. It still rejects an
unknown source time after restore/rollback and retains strict billing time and
spendable-backing validation. Tests cover both initial-height spellings before
commit and successful export afterward, alongside all existing vesting,
historical-height, and rollback regressions.

The [new validation notes](2026-09-10-claude-export-runbook-validation/README.md)
record the app suite, import simulation, CLI fixture, lint, and documentation
checks. The [corrected mutation evidence](2026-09-10-claude-follow-up-review-validation/README.md)
clearly separates its exact-baseline reruns from historical suite results.

The historical store timestamp conclusion and composed-simulation caveat are
unchanged. No live policy, permission, deployment, dependency, protobuf, or
generated binding changed. The eight broad review passes, production-cardinality
rehearsal, and live deployment audits remain open as documented in the
[preceding response](2026-09-10-claude-follow-up-review-response.md#validation-and-remaining-scope).
These targeted fixes do not close that broader assurance gap.
