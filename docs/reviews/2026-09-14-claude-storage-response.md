# Claude storage-contract follow-up — September 14, 2026

Reviewed [Claude's audit of `fe6ccd3`](https://github.com/manifest-network/manifest-ledger/pull/179#issuecomment-5665140869).
The internal storage proto still repeated the CLOSED-cursor assumption removed
from the public lease field. The finding is valid: the storage codec copies
`LastSettledAt` verbatim, and accepted CLOSED imports can retain a final interval.

## Findings and confidence

| ID | Severity | Correction | Confidence |
| --- | --- | --- | --- |
| F1 | Informational; internal storage contract | The internal `last_settled_at` comment and both generated bindings now distinguish normal close finalization from a CLOSED import's remaining interval for specific-UUID withdrawal. | 99% |
| A1 | Informational; public state contract | The `LEASE_STATE_CLOSED` enum also claimed final settlement had always occurred. It now documents the same imported-state exception. | 99% |
| A2 | Informational; adjacent state descriptions | ACTIVE describes acknowledgement starting billing without asserting off-chain provisioning. REJECTED includes authority rejection and tenant cancellation. EXPIRED covers deadline handling and migration expiration when pending reservations cannot all be backed. Released credit remains in the tenant's billing account. | 99% |

Confidence applies to each finding and correction, not the absence of
undiscovered defects. No transition, codec, accounting rule, or public API
behavior changed. The enum descriptions follow the actual acknowledgement,
rejection, cancellation, expiration and migration paths. In particular, they
do not promise that a historical shared reservation is fully released whenever
one cohort member terminates.

A bounded sweep included public/internal protos, generated Go, source comments,
non-review documentation and other repository text. Other final-settlement
statements describe actual close operations or fixtures that just closed a
lease. Those remain correct. Historical review evidence remains unchanged.

## Validation

[Exact commands and statuses](2026-09-14-claude-storage-validation/checks.json)
record Go 1.26.8, the working directory, final source hashes and raw/display log
hashes. Both final checks exited 0:

- Full `make proto-all`: [output](2026-09-14-claude-storage-validation/proto-all.log).
- Existing storage codec, imported-CLOSED settlement, reservation lifecycle and
  migration-expiration regressions: [output](2026-09-14-claude-storage-validation/focused-go.log).

Both [source protos](2026-09-14-claude-storage-validation/proto-verification.json)
have identical non-comment lines before and after. The four regenerated Go files
have **104,008 identical non-comment tokens**, including descriptor literals.
`go.mod` and `go.sum` are unchanged.

The [token comparison record](2026-09-14-claude-storage-validation/generated-tokens.json)
includes directory setup, all four baseline-seeding commands and hashes,
verifier copying, actual comparison argv and stdout. It reuses the existing
[scanner](2026-09-14-claude-docs-validation/generated-token-check.go.txt).
Repeating this before/after comparison requires the recorded `fe6ccd3` Git
object. No archive or new verification driver was added; the existing bundled
mutation workflows were untouched.

This comment-only follow-up does not rerun the broader suites locally. The
earlier frontend tests and mutation results remain attributed to their reviewed
commit in the [previous report](2026-09-14-claude-docs-response.md).
The pushed commit runs its own CI. Production snapshot rehearsal, capacity and
gas/circuit/RPC checks, and deployed Fred/Barney integration remain release
gates. Nothing was merged or deployed.
