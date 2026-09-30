# PR #180 / #179 merge order

Verified on 2026-09-16 against [PR #180](https://github.com/manifest-network/manifest-ledger/pull/180)
head `0ea5815554276a33c332bac5103efc828211aab1` and
[PR #179](https://github.com/manifest-network/manifest-ledger/pull/179)
head `32d6ae7a6d9230b572cb5651f0f3db83e7fb2e4f`.
The [review](https://github.com/manifest-network/manifest-ledger/pull/180#issuecomment-5700571008)
identifies overlapping dependency, upgrade, tooling and coverage changes.
Both branches start from `9b14229c445298df026b04eac9f76e5689667a9d` and modify
26 common paths. A conflict-free textual merge is not sufficient validation.

## Order

1. Merge #180 after its final checks and review are complete and the repository
   owner resolves the separate `codecov/project` provider/required-context issue.
   This plan does not change branch protections or infer the owner's Codecov plan.
2. After #180 lands, add the exact required contexts
   `e2e-tests (ictest-in-place-testnet)` and `e2e-tests (ictest-billing-upgrade)`.
   Confirm the workflows emit both names before requiring them on other PRs.
3. Rebase or merge the new main into #179, review the resolved result as a new
   change, and rerun its full validation before merging #179.

## Carry forward during #179 resolution

- **SDK:** Both #179 modules currently replace the SDK with
  `v0.50.14-liftedinit.1.0.20260914180641-5359749658aa`.
  Its source has no `server/testnet.go`, and its `server/start.go`, armor and
  vesting code differ from `.3`. Select `.3` or a reviewed descendant containing
  the in-place-testnet repair from SDK PR #4. Keep both module replacements in
  agreement and regenerate their sums. Do not restore the earlier vesting
  implementation while resolving module conflicts; rerun #179's vesting,
  migration and compatibility regressions against the selected release.
- **Compiler and dependencies:** Carry #179's coordinated Go 1.26.8 migration
  through module/workspace minimums, all workflows, release and Docker pins.
  The coverage host and both images must use the same exact compiler. Preserve
  #180's gRPC 1.83.2 / x/text 0.41.0 fixes and maintained SDK armor path, alongside
  #179's x/crypto maintenance update, unless a reviewed newer dependency is chosen.
  Revalidate the graph with that compiler; existing 1.25.14 results do not certify
  the combined 1.26.8 build.
- **Upgrade and CLI wiring:** Preserve the split normal and
  `testnet_upgrade_fixture` handlers from #180, including the fixture's measurable
  post-migration change, while retaining #179's billing/SKU migrations. Avoid
  duplicate `CreateUpgradeHandler` definitions. Combine #179's query-gas and
  migration-preflight commands with #180's early isolation/journal guards,
  authority checks and protected restart wiring in `commands.go` / `root.go`.
- **Live uploads and simulation:** Retain the single-file Docker archive helpers
  used for live-validator uploads. Keep the pending-lease-cap simulation guard
  once and preserve regression cases from both branches.
- **Generated code and release checks:** Preserve the digest-pinned protocol
  build/lint/drift check and its deletion/output-path regressions while carrying
  #179's additional schema compatibility checks. Retain the real native Darwin,
  ARM64 image and GoReleaser snapshot checks; resolve #179's deletion of the old
  Docker release workflow against the intended release pipeline explicitly.
- **Coverage and security policy:** Resolve #179's `codecov.yaml` and #180's
  `codecov.yml` into one authoritative configuration. Reconcile #179's explicit
  80% changed-line gate with Codecov's automatic patch target; do not silently
  drop either requirement while resolving files. Preserve complete base/head
  comparison and uploaded evidence, compiler identity checks, deterministic
  simulation seeds and exact E2E selectors. Retain the pinned vulnerability
  scanner and narrowly scoped, expiring advisory policy.

## Before merging #179

Review the complete SDK/dependency, handler and migration diff, rather than only
conflict markers. Run module tidy/verify, unit and simulation/determinism suites,
vesting and migration regressions, command/preflight tests, protocol checks,
release/snapshot/native architecture builds, vulnerability analysis and the full
E2E matrix, including in-place-testnet and billing upgrades. Validate coverage
from the combined source graph with matching host/image compilers and inspect
both the full comparison and changed-line result. Attach the new evidence to
#179; green results from either original branch are not a substitute.
