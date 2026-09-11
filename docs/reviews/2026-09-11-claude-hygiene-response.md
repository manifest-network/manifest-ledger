# Claude review follow-up — September 11, 2026

Reviewed [Claude's audit of `8492a4a`](https://github.com/manifest-network/manifest-ledger/pull/179#issuecomment-5637667805).
The review confirms the terminal-settlement policy, ACTIVE-path parity, genesis
validation tests and consumer audit. The new changes address help text,
documentation and evidence tooling. Settlement, authorization and wire behavior
remain unchanged.

## Findings and confidence

Confidence assesses the stated finding and disposition, rather than the absence
of undiscovered defects.

| ID | Severity | Disposition | Confidence |
| --- | --- | --- | --- |
| P1 | Low; operator help | `update-provider --meta-hash` now states that omitted/empty clears the hash and that preserving it requires resending its current hex value. Long help, examples and the provider API reference distinguish this from `--api-url`'s preserve-on-omission behavior. Existing proto/generated/provider-guide semantics already agree. | 99% |
| H1 | Low; reproduction transcript | Both patch-verification records now include the missing baseline-seeding step and explicit baseline hashes. A reusable verifier records and replays initialization, seeding, input-hash checks, patch application and result-hash checks in fresh directories using existing archived inputs. Historical Go test results stay identified as historical. | 99% |
| H2 | Low; documentation tooling | Bundled reproduction integrity checks now fail explicitly rather than using removable Python assertions. Tampered inputs are checked before Go invocation, including both historical replacement files. Plain and optimized execution rejected malformed inputs; the valid five-run suite also passed under `python3 -O`. | 99% |
| H3 | Informational; documentation | Removed the assumption that every CLOSED lease is fully settled. README, troubleshooting, architecture diagram/cursor table and the provider-wide implementation comment now distinguish normal close finalization from accepted CLOSED imports requiring explicit-UUID finalization. Zero-payment finalization and true no-op errors match the API contract. | 99% |
| H4 | Informational; repository size | Retain the three existing source archives, totaling 2,729,763 bytes. Their duplication is the documented tradeoff for reproducing evidence after a squash merge. Removing working-tree copies would not remove existing Git history and would break that contract. This follow-up adds no archive. | 100% for measured size; 99% for disposition |
| A1 | Informational; related fixture hygiene | The CLI verification fixture also uses test assertions. It now rejects `-O`, `-OO` and `PYTHONOPTIMIZE` before accessing arguments, binaries or fixture state, instead of silently disabling its test checks. Its ordinary execution remains supported and is rerun against the current CLI binary. | 99% |

The bundled hashes enforce agreement with the checked-in manifest. They do not
authenticate a simultaneously replaced manifest and bundle; trusted Git history
remains the origin of those expectations. No stronger authenticity claim is
made. The original archive bytes remain unchanged.

## Validation and updated limits

[Commands and evidence](2026-09-11-claude-hygiene-validation/README.md) record this
round's focused Go checks, documentation suite, lint, CLI fixture, optimized
integrity probes and patch reconstruction/replay. The earlier full-root,
simulation and mutation runs remain separately attributed. This help/comment
follow-up does not need another production settlement matrix.

All 31 checks for `8492a4ad18996d0877a7425b8039ed355cc94726` passed, including
billing-state interchaintest. We inspected the [actual CI job log](https://github.com/manifest-network/manifest-ledger/actions/runs/34610707111/job/103301629221),
which checked out merge `6bd1d06` of that PR head into `9b14229`. It explicitly
ran and passed `PendingLeaseExpiration/success:_expiration_releases_reserved_credit_without_charging_the_tenant`.
That closes the prior expiration-assertion execution gap with CI evidence.
The local Docker DNAT failure remains a truthful historical result; no host
network policy was changed or local e2e success claimed. The new pushed commit
must run its own CI.

Claude's 27-cell terminal-policy matrix and differential ACTIVE-path checks
are reviewer execution, not additional local runs claimed here. The earlier
Fred/Barney source audit and its eventual-cleanup limitation stand. Deployed
consumer integration, production snapshot replay, capacity measurement and
live gas/circuit/RPC controls remain rollout work. Nothing was merged or deployed.
