**SKU and billing review — 2026-09-09**

Reviewed commit: `aeec29e1b0e5dc452415e699838c0676efe38fba`. Scope: `x/sku`, `x/billing`, their protobuf/CLI/query surfaces, shared UUID/pagination/storage/accounting helpers, relevant bank/tokenfactory/app wiring, migrations, simulations, integration-test sources, and documentation. The findings and baseline evidence below describe that commit. The user subsequently authorized tracking and remediation; current status is recorded separately below.

The architecture is broadly sound for the pinned Cosmos SDK 0.50 fork. The strongest remaining concern is an interaction with tokenfactory administrator powers: a normal denomination-admin operation can invalidate billing reservations and prevent an affected lease from terminating. Three other medium-priority items concern operational scripts and the already-known domain-ownership gap. The user subsequently confirmed billing v3 is not deployed, lowering the direct-v3 preflight issue to low priority. No unprivileged SKU administration bypass or unauthorized spending of ordinary native-token credit was demonstrated. A review and passing scanners cannot establish that no vulnerabilities exist.

Confidence measures confidence in the stated finding, not the probability of exploitation. P2 means medium priority; P3 means low priority. Preconditions and untested deployment consequences are stated explicitly. “New” below means newly reported against this checkout, without asserting that no external tracker already contains it.

| ID | Priority | Finding | Confidence |
| --- | --- | --- | --- |
| R1 | P2 | Tokenfactory admin debits can break reservation backing and strand mixed-denomination leases | 100% reproduced behavior |
| R2 | P3 after deployment clarification | Preflight can disagree with a direct v3→v4 upgrade; v3 is not deployed | 100% behavior; no identified current deployment exposure |
| R3 | P2 | Batch migration script reports success after failed transactions | 100% |
| R4 | P2 | SKU deactivation instructions depend on an undecoded transaction result | 99% |
| R5 | P2, known/deferred | Domain claims still do not establish domain ownership | 99% squatting; routing impact conditional |
| R6 | P3 | Live accounting invariant accepts missing reservation wrappers | 100% |
| R7 | P3 | SKU UUID extraction recipe reads obsolete transaction logs | 100% |
| R8 | P3 | Custom-domain notes reference a nonexistent command | 100% |
| R9 | P3, conditional | Authentication specification omits chain/provider audience binding | 98% omission |
| R10 | P3 | CLI, fuzz/property, and realistic scale assurance remain incomplete | 100% inspected gaps |

**Remediation tracking (authorized 2026-09-09)**

Umbrella: [ENG-917](https://linear.app/liftedinit/issue/ENG-917). Work is isolated on `codex/sku-billing-review-20260909`, based on the reviewed commit. The review's reproductions remain historical evidence; new regression tests assert the corrected behavior.

| Finding | Linear | Remediation status |
| --- | --- | --- |
| R1 | [ENG-918](https://linear.app/liftedinit/issue/ENG-918/protect-billing-credit-accounts-from-tokenfactory-issuer-debits) | Fixed locally; validated; In Review |
| R2 | [ENG-919](https://linear.app/liftedinit/issue/ENG-919/make-billing-reservation-preflights-supported-migration-path-explicit) | Supported path explicit and parity-tested; direct v3 intentionally unsupported |
| R3 | [ENG-920](https://linear.app/liftedinit/issue/ENG-920/make-documented-billing-migration-fail-on-transaction-failures) | Fixed locally; validated; In Review |
| R4 | [ENG-921](https://linear.app/liftedinit/issue/ENG-921/give-sku-deactivation-cli-users-a-usable-cascade-completion-workflow) | Fixed locally; validated; In Review |
| R6 | [ENG-922](https://linear.app/liftedinit/issue/ENG-922/keep-billing-runtime-invariants-strict-about-reservation) | Fixed locally; validated; In Review |
| R7 | [ENG-923](https://linear.app/liftedinit/issue/ENG-923/use-current-sdk-transaction-events-in-sku-creation-examples) | Fixed locally; validated; In Review |
| R8 | [ENG-924](https://linear.app/liftedinit/issue/ENG-924/correct-billing-custom-domain-query-command-examples) | Fixed locally; validated; In Review |
| R9 | [ENG-925](https://linear.app/liftedinit/issue/ENG-925/specify-audience-bound-provider-authentication-and-coordinate-rollout) | Proposal and vectors added; provider/client rollout open |
| R10 | [ENG-926](https://linear.app/liftedinit/issue/ENG-926/add-targeted-sku-cli-and-billing-propertyfuzz-regression-coverage) | Targeted gaps fixed locally; broader coverage/capacity remain open |
| R5 | [ENG-62](https://linear.app/liftedinit/issue/ENG-62) | Deferred by prior product decision; Icebox unchanged |

Broader test infrastructure remains [ENG-869](https://linear.app/liftedinit/issue/ENG-869); measured production capacity remains [ENG-890](https://linear.app/liftedinit/issue/ENG-890). Authentication v2 examples and context-validation vectors are local protocol work; off-chain client/verifier rollout is still required and must not be represented as a deployed fix.

**Remediation validation.** The complete SKU, billing, shared, app and command suites passed under Go 1.26.8. Scoped golangci-lint reported zero issues. All 36 executable documentation tests passed. The committed application simulation completed 100 blocks, exported/imported state, and continued for another 100 blocks (seed `2507940531156952020`, block size 50, invariant period 5). Native fuzzing passed 21,444 reservation-planner cases and 20,060 storage-codec cases, with 15 seconds per campaign. The checked-in seed corpus also runs with ordinary Go tests. [Commands and raw remediation logs](2026-09-09-sku-billing-remediation/README.md) distinguish these results from the baseline review evidence and coverage numbers below.

The app-local guard and runtime-validator separation received independent code review. Authentication serialization review found and corrected an HTML-escaping interoperability ambiguity, with a fixed `&` audience vector. These changes are local and unmerged/undeployed. No state schema, tokenfactory dependency, or chain migration format changed; the offline preflight report intentionally moves to schema 4. The bank guard is consensus behavior and belongs in the coordinated application upgrade. Full race/Docker suites, production-scale load tests and deployed provider/wallet interoperability were not rerun as part of this remediation.

**R1 — Tokenfactory administrator powers violate a billing assumption**

[App capabilities](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/app/app.go#L165) enable both `BurnFrom` and `ForceTransfer`. The pinned tokenfactory's [message handlers](https://github.com/strangelove-ventures/tokenfactory/blob/v0.50.7-wasmvm2/x/tokenfactory/keeper/msg_server.go#L96) authorize these by denomination administrator. Its [bank actions](https://github.com/strangelove-ventures/tokenfactory/blob/v0.50.7-wasmvm2/x/tokenfactory/keeper/bankactions.go#L42) protect static module accounts/blocked addresses; derived tenant credit addresses are ordinary accounts. No billing reservation check participates in these debits.

Once an authorized SKU administrator admits such a denomination, its denomination administrator can debit the tenant's reserved balance through the real tokenfactory handlers. Billing then rejects settlement and close because [PlanReservationSpend](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/x/billing/types/reservation_spend.go#L65) requires full backing for the lease's denominations. The adversarial test's [description of external drains as impossible](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/x/billing/keeper/adversarial_fund_safety_test.go#L258) is therefore incorrect for this app.

The local fixture creates a factory denomination, mints 10,000 units into credit, funds an ordinary denomination, and creates/acknowledges a lease containing both. A denomination-admin burn or force transfer succeeds. Closing the mixed lease fails with `ErrReservationInvariant`, leaves it ACTIVE, and retains its 3,600-unit ordinary-denomination reservation. A separate lease containing only the unaffected denomination remains operable: [reservation inputs are projected onto each lease's denominations](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/x/billing/keeper/reservation.go#L160). This is not a claim that every lease for that tenant freezes.

The broken backing also trips the registered accounting invariant. With nonzero `inv-check-period`, the SDK crisis EndBlock assertion can panic at a check height; the default period is zero, so an unconditional default-chain halt is not claimed. No deployed-node halt was tested.

Recommendation: protect known billing credit sources in the **tokenfactory-specific** bank dependency, covering both force-transfer and burn-from calls. The existing `CreditAddressIndex` permits context-aware identification. Merely adding recipients to `BlockedAddr` is insufficient and can break deposits. Alternatively, enforce and document a precise denomination/admin policy, account for later administrator changes and existing leases, and define deficit recovery. A one-time administrator allowlist does not remove retained clawback powers. Recovery must preserve other tenants' and leases' claims; relaxing subtraction checks alone is unsafe. Add a real tokenfactory/billing regression including mixed and independent denominations, admin changes, and both debit mechanisms.

Evidence: [local handler regression and original results](2026-09-09-sku-billing-evidence/README.md). Setup uses trusted SKU fixture helpers; tokenfactory operations and billing lifecycle operations use real application keepers/message handlers. It is not a signed-network attack test.

**Policy accepted after user follow-up (2026-09-09).** Protect the entire balance of every registered billing credit account from tokenfactory burn-from and force-transfer, while preserving those powers for ordinary wallets. This matches the existing [credit withdrawal policy](../../x/billing/README.md#credit-withdrawal-policy) and module-controlled custody design: unused credit is locked for present or future leases. Deposits, permitted minting into credit, and billing settlement/payout remain allowed. The user approved the proposed app-local adapter and authorized implementation; no tokenfactory fork is required. Confidence: 97% in alignment with the preexisting documented product intent, now confirmed by the user.

A cap based only on `balance - reserved` would preserve the arithmetic invariant but introduce an administrator-assisted withdrawal path. Lazy settlement also means that unreserved balance can fund service already accrued but not collected. The material tradeoff of full protection is an explicit exception to issuer clawback: anyone can place tokens into billing credit, where the issuer cannot seize them until a billing payout returns them to an ordinary account. Deposits are irreversible under the current policy. If unrestricted issuer clawback is required, it needs a separately designed billing policy rather than an implicit accounting bypass.

**R2 — Offline preflight does not distinguish source consensus versions**

[BuildReservationMigrationPreflight](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/x/billing/keeper/reservation_preflight.go#L127) always runs import preparation, including the repair associated with v2→v3. A chain already at v3 runs only [Migrate3to4](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/x/billing/keeper/reservation_migration.go#L253), which consumes its stored aggregate. The [CLI](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/cmd/manifestd/cmd/billing_migration_preflight.go#L73) has no source-version/mode input, and wrapper absence cannot distinguish v2 from v3.

Reproduction: a modern lease has nominal reservation 10, stored account aggregate 5, and bank balance 10. Preflight predicts 10. Direct v3→v4 assigns 5 to an ACTIVE lease; for a PENDING lease it aborts because the old aggregate cannot back the exact reservation. Both cases were reproduced under Go 1.26.8. The discrepancy requires aggregate drift in an existing v3 snapshot. The ordinary sequential v2→v3→v4 and genesis-import repair policies are not implicated.

Deployment clarification: the user confirmed that billing v3 is not deployed. R2 is consequently downgraded from P2 to P3; it is not an identified blocker for the sequential v2→v3→v4 path.

Recommendation: explicitly document the supported source path and its preparation steps. If direct-v3 upgrades are supported later, make source version/import-versus-upgrade mode explicit, report that provenance in JSON, and add direct-v3 parity fixtures alongside the existing [sequential parity test](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/x/billing/keeper/reservation_preflight_parity_test.go#L20). A generalized migration-mode interface is not urgent solely for an undeployed source version.

**R3 — Batch migration automation can falsely report completion**

The [batch migration example](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/x/billing/docs/MIGRATION.md#L479) does not verify funding, lease creation, or acknowledgement execution. It sleeps six seconds, extracts events without checking committed transaction code, and unconditionally prints completion.

Executing the exact block with mocked committed creation failures and failed acknowledgements returned exit status zero and printed `Migration complete!`. Slow inclusion or unavailable indexing can also defeat its assumptions. Operators can be left with funded accounts but missing or expiring leases. Shell `set -e` alone does not detect a successful CLI invocation carrying a nonzero chain execution code.

Recommendation: verify admission, poll inclusion with a deadline, verify execution code/height for every transaction, validate exactly one expected lease UUID, and checkpoint progress for restart. Reuse a single tested receipt-handling helper based on the more careful [withdrawal workflow](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/x/billing/docs/API.md#L426). Add delayed-inclusion, funding failure, DeliverTx failure, and restart examples to executable documentation tests.

**R4 — SKU deactivation continuation has no usable CLI stop condition**

The [command help](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/x/sku/client/cli/tx.go#L182), [API guide](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/x/sku/docs/API.md#L105), and [provider guide](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/x/sku/docs/PROVIDER_GUIDE.md#L248) instruct users to repeat until the response has `has_more=false`. The [command implementation](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/x/sku/client/cli/tx.go#L213) only calls normal SDK broadcasting. It prints a transaction envelope, not decoded `MsgDeactivateProviderResponse`; a standard committed `query tx` also leaves message responses encoded.

For a provider with more than the default 50 active SKUs, the first transaction correctly disables the provider but leaves more cascade work. A script treating the absent field as false stops early; subsequent reactivation is blocked until the cascade finishes. Admission protection itself remains correct.

Recommendation: provide a committed-result decoder analogous to billing's `withdraw-result`, or document waiting for committed success and querying `skus-by-provider UUID --active-only --limit 1` after each batch. Add a CLI/doc test with more than 50 SKUs.

**R5 — Known domain-ownership gap remains**

[SetItemCustomDomain](../../x/billing/keeper/custom_domain.go) checks lease authority, syntax, policy, and uniqueness, but does not prove control of the domain. A tenant can claim an otherwise-unclaimed FQDN belonging to someone else and block its legitimate claim. The [architecture guide](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/x/billing/docs/ARCHITECTURE.md#L646) now states that provider verification is post-deployment and ownership/recovery is deferred.

This is the previously recorded R2/ENG-62, not a newly discovered issue. Wrong-tenant routing remains conditional on provider implementation and DNS/ingress behavior; no provider implementation or deployed takeover was tested. Registry membership alone is insufficient ownership evidence.

Recommendation: retain explicit documentation of the limitation and define provider verification and legitimate-owner recovery. A future claim activation protocol should bind domain, tenant, provider/deployment, and expiry; network verification must stay outside consensus execution. See the [previous review's scope and deferral](2026-09-08-sku-billing-review.md).

**R6 — Runtime accounting validation inherits legacy import leniency**

[ReservationAccountingInvariant](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/x/billing/keeper/invariants.go#L81) invokes import-safe genesis validation. [All-nil reservation wrappers](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/x/billing/types/genesis.go#L478) select the legacy format, so that path repairs/validates a copy. It does not require current stored leases to possess a wrapper.

Removing only the sole ACTIVE lease's `Reservation` pointer made both registered billing invariants report valid state, while withdrawal failed at [leaseReservationAllocation](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/x/billing/keeper/reservation.go#L24) with `no initialized reservation`. This was reproduced under Go 1.26.8. No public message path that removes the wrapper was identified; this is a corruption/migration diagnostic gap.

Recommendation: use a non-repairing current-state validator in invariants, explicitly reject every missing wrapper, and retain legacy acceptance only in import/migration. Test the all-nil/one-lease case as well as mixed-format state.

**R7 — SKU UUID extraction uses an obsolete response field**

[API.md:1066](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/x/sku/docs/API.md#L1066) extracts `provider_uuid` from `.logs[0].events[]`. The pinned SDK exposes current successful events under top-level `.events`. The exact jq recipe failed with exit 5 against the guide's own success event fixture plus the current empty `logs` envelope; using `.events[]` returned the expected UUID.

Recommendation: correct the path, scope by message index when supporting multiple creates, and execute the snippet in documentation tests.

**R8 — Two domain-query references name an unavailable command**

[API.md:524](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/x/billing/docs/API.md#L524) and [API.md:1848](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/x/billing/docs/API.md#L1848) recommend `lease-by-custom-domain`. The [registered command](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/x/billing/client/cli/query.go#L629) is `lease-by-domain`, with no matching alias. The dedicated documentation section already uses the correct spelling.

Recommendation: fix both notes and link to one canonical command definition; add a command-name smoke check.

**R9 — Off-chain authentication lacks audience separation**

The documented [ADR-036 payload](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/x/billing/docs/INTEGRATION.md#L118) signs lease UUID and timestamp, plus a hash for upload, without chain ID or provider/origin audience. ADR-036 itself uses an empty sign-document chain ID and leaves application replay protection to signed data. The wallet's chain-selection argument does not bind that audience. [Official ADR-036](https://github.com/cosmos/cosmos-sdk/blob/main/docs/architecture/adr-036-arbitrary-signature.md).

If lease state is cloned into a less-trusted environment, or corresponding endpoints change, a captured still-valid signature can be accepted by another audience sharing the tenant/UUID/state. Actual impact depends on provider verification behavior; no deployed provider was tested. Same-audience reuse during a deliberately short bearer-token lifetime is not independently called a vulnerability.

Recommendation: version the signed payload and bind chain, provider/expected origin, operation, and expiry. Explicitly choose reusable bearer or one-use challenge semantics. Add cross-chain/audience and expiry vectors. Keep the existing recommendation to use established ADR-036 verification rather than hand-writing Amino encoding; [Keplr's documentation](https://docs.keplr.app/api/guide/sign-arbitrary) identifies its verifier. Repair the stale ADR link in the integration guide.

**R10 — Assurance gaps persist despite substantial keeper coverage**

The [SKU transaction test](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/x/sku/client/cli/tx_test.go#L9) checks that a Cobra flag exists and can be read. It never executes the request construction that [forwards ClearApiUrl](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/x/sku/client/cli/tx.go#L156). Omitting that assignment would silently break URL clearing while the test still passed. No `clear-api-url` interchaintest was found. Add generate-only request tests that decode the resulting transaction and exercise omitted, explicit-clear, and conflicting modes.

There are no native `Fuzz*` entry points in either module or the reviewed shared packages. Add bounded malformed-storage decoder tests and independent reservation-planner properties: conservation, nonnegative allocations, allocation caps, deterministic tie breaks, and permutation invariance. Preserve economic example regressions; assertions that merely recalculate the production implementation add little assurance.

The only module benchmarks found were three accrual microbenchmarks. Large correctness fixtures do not establish node capacity. [“Millions of leases without performance degradation”](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/x/billing/docs/CAPABILITIES.md#L84) is unsupported by the available performance evidence. Lazy settlement avoids a full active-set scan each block, but database/index cost, bounded pending expiry, query work, historical retention, export, migrations, and invariant scans still matter. Benchmark representative committed stores and hardware before making capacity claims. Parameter caps, provider cascade latency, upgrade time/memory, and state retention need an explicit deployment envelope.

**Architecture, Go, Cosmos SDK, and simplification assessment**

The keeper/message/query boundaries, narrow expected-keeper interfaces, typed collections, versioned migrations, deterministic iteration, and bank-backed reservation model are appropriate. The code uses current Go practices including `errors.Is`, `errors.AsType`, `slices`, `cmp`, defensive copies, integer-range loops, and checked SDK arithmetic. Registered invariants, raw address identity, bounded pagination, and cache-isolated state transitions provide meaningful defenses. Query simulation and provider withdrawal share the per-lease execution path, reducing quote/transaction drift. These choices align with [Cosmos module design guidance](https://docs.cosmos.network/sdk/latest/guides/module-design/module-design-considerations) and [v0.50 message/query services](https://docs.cosmos.network/sdk/v0.50/build/building-modules/messages-and-queries).

Go 1.26.8 is the current patch in a supported Go branch; 1.27.1 is the latest stable toolchain as of this review. Both were verified against the [official release history](https://go.dev/doc/devel/release). Keeping coordinated 1.26.8 production pins is reasonable while testing dependency support for 1.27. Merely adopting new syntax is not a security improvement. The installed 1.27 toolchain triggered a Sonic compatibility fallback during an initial test attempt, reinforcing the need for an explicit upgrade validation matrix.

| Opportunity | Recommendation and boundary | Confidence |
| --- | --- | --- |
| Routine query CLI | Evaluate existing Cosmos AutoCLI for pass-through queries; preserve command grammar, JSON defaults, pagination flags, and cursor encoding with contract tests. Specialized quote/result commands remain custom. | 95% |
| Validation policy | Separate strict current-state validation from compatibility import repair; reuse pure checks under explicit policies. R6 demonstrates why the distinction matters. | 100% |
| Keeper organization | Split large keeper/msg-server files into lifecycle, genesis, indexing, and parameter responsibilities without adding new public abstraction layers. | 90% |
| Operational examples | Consolidate receipt polling/decoding/checkpoint behavior into a tested helper and reference it from both migration and provider guides. | 99% |
| Small Go cleanups | Use `bytes.Clone`/`slices.Clone` for remaining append-to-nil copies where clearer; avoid broad stylistic churn. | 100% equivalence at applicable copy sites |
| Third-party replacements | Keep deterministic UUID generation, bounded pagination, checked coin merges, and deterministic allocation. Generic UUID/finance/pagination packages do not preserve all their consensus and resource contracts. Standard `hash/fnv`, SDK math, Collections, and the shared storage envelope already remove substantial duplication. | 98% |

The authority injected by manual app wiring comes from [POA_ADMIN_ADDRESS](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/app/helpers/utils.go#L14), whereas depinject uses governance by default. Different validator-local authority settings can produce different consensus decisions. Documentation acknowledges the wiring distinction; this is an existing deployment constraint, not a remotely controlled setting. Compare consensus-critical configuration at startup/deployment, and consider committing authority in genesis/state. Confidence: 99% in the configuration dependency.

**Validation and limitations**

The final module/shared-package coverage run passed under Go 1.26.8. These are **statement coverage percentages for handwritten code**, with generated protobuf/gateway/Pulsar files excluded and cross-package execution included:

| Area | SKU | Billing |
| --- | ---: | ---: |
| Module total | 66.8% | 79.2% |
| Keeper | 87.1% | 83.8% |
| Types | 89.1% | 94.0% |
| CLI | 15.1% | 41.6% |
| Simulation package | 51.9% | 66.7% |
| Module wiring | 56.8% | 66.7% |

Shared coverage: pagination 90.4%, UUID 100%, sanitization 100%, collection validation 87.9%, storage envelope 81.0%. These figures exclude the temporary review reproductions, app simulations, and Docker execution; they are not branch coverage. The [coverage summary and successful logs](2026-09-09-sku-billing-evidence/README.md) preserve evidence and reproduction commands.

Executed successfully:

- Module/shared unit and in-process integration tests with coverage: `go test -p 2 ./x/sku/... ./x/billing/... ./pkg/... ./internal/... -count=1 -coverpkg=./x/sku/...,./x/billing/...,./pkg/...,./internal/... -coverprofile=...`.
- Application and command tests: `go test -p 2 ./app/... ./cmd/manifestd/cmd -count=1`. Randomized app simulations are opt-in and were also run explicitly as described below.
- Configured golangci-lint 2.12.2 across both modules and shared packages: **0 issues**.
- Executable documentation suite: `node --test scripts/docs_examples.test.mjs`, **7 tests passed**.
- Go 1.26.8 overlay reproductions for R1, R2, R6; unchanged-script mock reproduction for R3.
- Committed 100-block full-app simulation: `TestFullAppSimulation`, seed `2507940531156952020`, `Period=5`, `simulation/sim_params.json`: **passed**.
- Export/import continuation simulation: `TestAppSimulationAfterImport` using the same configured seed/profile, 100 blocks before export and 100 after import: **passed** (139.132s test execution).

The Go tests above use the repository workspace graph. The separate daemon dependency scan disables workspace overrides. This review did not rerun race instrumentation, every simulation seed/determinism permutation, all static release build variants, or Docker end-to-end suites.

Initial attempts encountered a full `/tmp`, sandbox-denied localhost/child-process operations, and scanner/toolchain mismatch. Successful reruns used workspace scratch space, permitted SDK test execution, and the official pinned toolchain; those initial infrastructure failures are not counted as product defects.

The source review checked the tests for permissions, canonical address aliases, pricing/overflow, fractional settlement, reservations, batched atomicity/partial success, query immutability, cursor continuation, expiry, migration/idempotency, genesis round trips, and index corruption. Tests are generally substantive. The concrete gaps above show why coverage percentages alone cannot establish correctness.

Scanners were run against the pinned Go 1.26.8 toolchain. The module scan reported no reachable known vulnerability. A separate `GOWORK=off` daemon scan flagged msgpack GO-2026-4740 and listed GO-2026-4513 at package level. These are already exact-version exceptions in [the repository policy](https://github.com/manifest-network/manifest-ledger/blob/aeec29e1b0e5dc452415e699838c0676efe38fba/tools/govulncheck-policy/main.go#L69). The upstream maintainer identifies them as duplicate/stale fixed-version records, and the [v2.4.1 release](https://github.com/shamaton/msgpack/releases/tag/v2.4.1) includes the fix; the checkout uses v2.4.2. The [database correction remains tracked](https://github.com/golang/vulndb/issues/5034). These raw scanner hits are not newly confirmed application vulnerabilities.

Both scans also list uncalled module advisories for the unused x/crypto OpenPGP package and CometBFT GO-2025-3442. Reachability is scoped to the selected packages/build, and module replacements/forks require source-aware advisory review. The scan does not cover unknown defects or prove an entire dependency fork safe. Production static/musl/ledger release variants and Docker end-to-end suites were not all rerun in this review.

The [previous review](2026-09-08-sku-billing-review.md) also records remaining SDK simulator ordering/time-queue limitations, disabled PoA mutation operations, a tokenfactory simulation collision case, and downstream JavaScript binding work. Current source retains the simulation workarounds. This review does not certify external tracker status or a newly published JavaScript package.

**Questions affecting prioritization**

1. Answered: billing consensus v3 is not deployed. R2 is now low priority.
2. Answered: the user accepted protection of all registered billing credit from issuer debits and authorized implementation using an app-local tokenfactory bank adapter.
3. Is the batch migration recipe used operationally, or do deployed wrappers already verify committed receipts and track resumable progress?
4. What are expected provider/tenant/lease counts, items/denominations per tenant, historical retention, and the upgrade time/memory budget?
5. Do deployed provider APIs already enforce chain/audience binding and independent domain ownership/recovery beyond this repository's guide?
