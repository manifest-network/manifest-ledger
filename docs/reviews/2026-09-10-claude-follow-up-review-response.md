# PR179 follow-up review — 2026-09-10

This addresses Claude's [audit of `4b89fda`](https://github.com/manifest-network/manifest-ledger/pull/179#issuecomment-5622184165).
No new critical, high, or medium issue was established. The concrete remaining
findings are low-severity correctness, documentation, and test-assurance issues.
Confidence describes confidence in each disposition, not a guarantee that no
other vulnerabilities exist.

The [subsequent export/runbook response](2026-09-10-claude-export-runbook-response.md)
clarifies the pre-first-Commit API boundary and export file-output command, and
replaces the three incomplete mutant logs with reproducible exact-baseline reruns.

## Findings and dispositions

| Finding | Disposition | Confidence |
| --- | --- | --- |
| Circuit grantee command uses unsupported `--limit` | **Fixed.** The generated command uses `--page-limit`. All four audit queries now explicitly include the same `--height`, including consensus parameters and the disabled list. Real CLI help and closed-loopback query attempts confirm that the corrected pagination/height arguments parse; no live grantee inventory was queried. | 100% |
| Restart recipe invokes a nonexistent root `validate-genesis` command | **Fixed.** Both restart guides now use `manifestd genesis validate /path/to/restart-genesis.json` after preparing the restart copy. The explicit path avoids validating the default-home genesis. A real binary accepts the supplied valid fixture even when the default-home genesis is invalid; omitting the path fails on that invalid default file. | 100% |
| In-process rollback can reuse a newer CheckTx clock | **Fixed; low, latent API issue.** Cached time is retained only if the original CheckTx header height equals the selected store height. Matching commit metadata remains authoritative. Without either source, zero-height preparation errors. The new regression rolls back from height 3 to 2 without reopening, and supplies a timestamp valid only at height 3. The former implementation accepts it; the fix rejects before export mutation. The shipped CLI uses separate processes, so no CLI path to this stale-header acceptance was established. | 99% |
| Provider withdrawal mutates caller lease/reservation before fallible writes | **Fixed; low, defensive issue.** Live settlement stages a lease with the existing reservation clone helper and assigns it only after both persistence operations succeed. Tests force real reserved-fund consumption followed by lease-index or credit-account persistence failure. Successful live settlement retains its fractional cursor; auto-close and caller-owned CacheContext behavior remain intact. Current callers discard the local value on error; no reachable persistent fund-loss path was established. | 99% |
| Simulation state assertions observe CMS instead of CheckTx | **Test gap fixed.** Snapshot balances, sequence, and billing state through `GetContextForCheckTx(nil)` before and after simulation. The real signed fixture requests and finite budgets are unchanged. A real cache-leak mutation fails the corrected test while the previous CMS observer passes all six cases against the identical mutation. | 100% |
| Release-wrapper fixture no longer detects lost physical-path normalization | **Test gap fixed.** Retain the real standalone Git fixture and Docker mount assertion; add a nested-directory case supplying a valid symlinked repository root through a narrow Git stub. Installed Git already canonicalizes this path, so this tests the wrapper's input contract, not a demonstrated Git vulnerability. Production wrapper unchanged. | 99% |
| AutoClose's early settlement case does not test copy staging | **Classification corrected.** Rename/comment it as early-error propagation. The two late persistence cases remain the meaningful copy-staging regressions; no claim that the early case distinguishes staging remains. | 100% |
| Historical store v1.0.2 might predate commit timestamps | **Refuted for the normal historical commit flow.** The field and timestamp persistence already exist in v1.0.2, and Manifest's exact pre-upgrade SDK fork passes the header before committing. This does not certify retained mainnet metadata or remove the known snapshot/rollback limitation. | 99% source verification |

The earlier [export review](2026-09-10-claude-export-review-response.md) remains
historical evidence. Its caveat about pre-existing in-process CheckTx *state*
does not excuse the stale *clock* fallback introduced there; this follow-up
corrects that fallback explicitly.

## Historical timestamp evidence

The [pre-bump Manifest dependency graph](https://github.com/manifest-network/manifest-ledger/blob/2c74394e7e9c7ed4b44e7b11b916a95cfc541044/go.mod)
pins store v1.0.2 and SDK fork `24d6e6cf46be`. In that exact fork,
[BaseApp.Commit supplies the commit header before committing](https://github.com/manifest-network/cosmos-sdk/blob/24d6e6cf46beb55d9789c041db68a5ab3093c064/baseapp/abci.go#L917).
Store v1.0.2 already declares
[CommitInfo.Timestamp as protobuf field 3](https://github.com/cosmos/cosmos-sdk/blob/store/v1.0.2/store/types/commit_info.pb.go#L35)
and [persists the header's time](https://github.com/cosmos/cosmos-sdk/blob/store/v1.0.2/store/rootmulti/store.go#L483).
Therefore the dependency bump alone is not evidence that older normal commits
lack source time. Actual target-height metadata must still be checked against
the retained source copy before relying on historical zero-height export.

## Validation and remaining scope

Commands, results, and mutation evidence are recorded in the
[validation notes](2026-09-10-claude-follow-up-review-validation/README.md).
This round changes no protobuf, generated binding, dependency, production
simulation policy, or release-wrapper code. It does not execute unsigned
simulation or composed permission-changing requests.

The eight broad review passes named in the earlier audit remain incomplete:
cross-module contracts, same-block ordering, economic attacker goals, operator
outage paths, quiet failures, contract drift, lease-termination comparisons,
and least-read files. These targeted fixes and regressions do not close that
broader assurance gap. Full local race/fuzz and Docker/interchain campaigns,
production-cardinality rehearsal, and live policy/metadata audits are not
claimed. Existing deployment and broader coverage gates remain in force.
