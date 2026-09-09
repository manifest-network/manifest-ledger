# SKU and billing review — 2026-09-08

Reviewed commit: `85d16028224ea2e3a22a20c2dac7f5a363c943a2`. Scope: `x/sku`, `x/billing`, their protobuf/API/CLI surfaces, supporting UUID/pagination/collection helpers, relevant app wiring, migrations, simulations, interchaintest sources, and documentation. The initial review did not change production code; the authorized remediation is tracked below.

The architecture is broadly sound and idiomatic for the pinned Cosmos SDK 0.50 fork. Five medium-priority issues need attention: one billing accounting defect, a custom-domain ownership gap, a CLI estimate mismatch, and two broken operational/integration recipes. Additional low-priority findings concern test assurance, exported helper contracts, documentation, and dependency maintenance. No critical issue or authorization bypass was demonstrated. This review and its tooling cannot establish that no vulnerabilities exist.

Source links in the original findings point to the reviewed commit. Paths under `/tmp` identify local review evidence, not repository artifacts; the validation commands and results are recorded here for reproduction from the repository root. The permanent [sum-overflow regression](../../x/billing/keeper/sum_overflow_test.go) covers the original accounting reproduction.

Confidence scores express confidence in the stated finding, not the probability of exploitation. P2 means medium priority; P3 means low priority. Conditional impacts are called out explicitly.

## Remediation follow-up

On 2026-09-08 the user requested Linear tracking and fixes. [ENG-897](https://linear.app/liftedinit/issue/ENG-897/xsku-xbilling-review-2026-09-08-findings-and-remediation) tracks this work. The findings and coverage below describe the original reviewed commit; later source line numbers may move as fixes are applied.

The user explicitly deferred R2 and clarified that domain verification currently occurs **post-deployment**. The existing [ENG-62](https://linear.app/liftedinit/issue/ENG-62/research-domain-ownership-verification-squatting-protection) is in Icebox. No domain-claim protocol change is part of this batch. Provider verification implementation and legitimate-owner claim recovery remain outside this repository review; the conditional routing concern is not a demonstrated deployed-provider exploit.

The eleven other original findings and simplifications S1, S2, and S4 are implemented locally, with regression coverage. The changes preserve unrelated billing denominations on overflow, align quote pagination, decode executed withdrawal responses, repair executable integration examples, validate exported pricing helpers, strengthen simulations and UUID compatibility, correct lifecycle documentation, update Go/crypto maintenance pins, and apply the documented governance receiving exemption. Shared storage envelopes retain module-specific wire formats and legacy policies. The implementation is prepared for review in [PR #179](https://github.com/manifest-network/manifest-ledger/pull/179); deployment remains pending.

AutoCLI remains a compatibility investigation: the pinned dependency changes pagination flag names, page-key parsing, enum grammar, and JSON defaults. Replacing the custom commands immediately would change their public contract. The broader fuzz/property and CLI coverage backlog remains in [ENG-869](https://linear.app/liftedinit/issue/ENG-869/close-risk-based-test-gaps-in-xsku-and-xbilling).

| Review item | Linear | Status |
| --- | --- | --- |
| R1 | [ENG-898](https://linear.app/liftedinit/issue/ENG-898/billing-preserve-unrelated-accrued-denominations-when-an-item-sum) | Implemented; In Review |
| R3 | [ENG-899](https://linear.app/liftedinit/issue/ENG-899/billing-cli-align-provider-withdrawable-default-page-with-withdrawal) | Implemented; In Review |
| R4 | [ENG-900](https://linear.app/liftedinit/issue/ENG-900/billing-docs-decode-executed-withdrawal-responses-before-continuing) | Implemented; In Review |
| R5 | [ENG-901](https://linear.app/liftedinit/issue/ENG-901/frontend-docs-reuse-the-signed-authentication-timestamp-after-wallet) | Implemented; In Review |
| R6 | [ENG-902](https://linear.app/liftedinit/issue/ENG-902/sku-validate-coin-inputs-at-exported-pricing-helper-boundaries) | Implemented; In Review |
| R7 | [ENG-903](https://linear.app/liftedinit/issue/ENG-903/sku-simulation-propagate-unexpected-store-and-codec-failures) | Implemented; In Review |
| R8 | [ENG-904](https://linear.app/liftedinit/issue/ENG-904/billing-simulation-exercise-allowed-list-creation-cursor-continuation) | Implemented; In Review |
| R9/S1 | [ENG-905](https://linear.app/liftedinit/issue/ENG-905/uuid-pin-consensus-output-with-golden-vectors-and-use-standard-hashfnv) | Implemented; In Review |
| R10 | [ENG-906](https://linear.app/liftedinit/issue/ENG-906/skubilling-docs-correct-provider-deactivation-and-terminal-domain) | Implemented; In Review |
| R11 | [ENG-907](https://linear.app/liftedinit/issue/ENG-907/ledger-update-coordinated-go-126-patch-pins-and-xcrypto-maintenance) | Implemented; In Review |
| R12 | [ENG-908](https://linear.app/liftedinit/issue/ENG-908/app-apply-governance-receiving-exemption-to-the-module-address-key) | Implemented; In Review |
| S2 | [ENG-909](https://linear.app/liftedinit/issue/ENG-909/skubilling-share-the-versioned-storage-value-codec-envelope) | Implemented; In Review |
| S3 | [ENG-910](https://linear.app/liftedinit/issue/ENG-910/skubilling-evaluate-autocli-for-routine-read-only-query-commands) | Tracked follow-up |
| S4 | [ENG-911](https://linear.app/liftedinit/issue/ENG-911/skubilling-apply-small-go-standard-library-simplifications-where) | Implemented; In Review |
| R2 | [ENG-62](https://linear.app/liftedinit/issue/ENG-62/research-domain-ownership-verification-squatting-protection) | Deferred by user |

### Additional findings during implementation

**R13 — P2: published JavaScript withdrawal bindings omit failed lease IDs. Confidence: 100% for the inspected source and installed package.** Local `manifestjs/src/codegen/liftedinit/billing/v1/tx.ts` and installed `@manifest-network/manifestjs` 3.0.0 only encode/decode response fields 1–5; they omit `failedLeaseUuids`. Consumers can lose explicit settlement retry targets. [ENG-913](https://linear.app/liftedinit/issue/ENG-913/manifestjs-regenerate-billing-withdrawal-bindings-to-retain-failed) tracks regeneration and a downstream release. The ledger fix supplies a CLI decoder using this repository's protobufs, so its corrected automation works without waiting for that release.

**R14 — P2: committed SDK simulations can discard successful simulated transactions. Confidence: 100%.** The pinned SDK simulator runs `FinalizeBlock`, then `SimDeliver` operations, then `Commit`. `FinalizeBlock.workingHash` flushes the finalize cache before those simulated operations; `Commit` commits the root store without flushing their later writes. A cross-block fixture demonstrated that a successful operation's state can disappear on the next block. Passing delivery statistics and the initial simulation runs therefore did not establish persistent multi-block billing histories. Real chain transactions execute inside `FinalizeBlock` before its flush, so this finding concerns the test harness. [ENG-914](https://linear.app/liftedinit/issue/ENG-914/app-simulation-persist-post-finalize-simulated-transactions-across) tracks a test-only precommit flush, a persistence regression, and corrected simulation runs.

**R15 — P2: the SDK simulator misorders validator mutations and loses time-queued operations. Confidence: 100% for the pinned source behavior and reproduced panic; 97% for the validator reconciliation diagnosis.** Once R14 preserved transaction writes, the original 100-block profile panicked at block 3 with `more validators than maxValidators found`. The simulator delivers transactions after EndBlock; PoA validator mutations therefore miss the same-block staking reconciliation that limits the selected validator set. Production transactions execute before EndBlock. This reproduction does not demonstrate the same panic in live-chain execution.

The committed simulation profile now explicitly disables all four PoA validator mutation operations alongside its existing staking exclusions. SKU and billing operations remain enabled. The precommit adapter fixes persistence, but does not correct transaction ordering; passing this profile does not establish randomized PoA validator-mutation coverage. [ENG-915](https://linear.app/liftedinit/issue/ENG-915/sdk-simulation-restore-transaction-ordering-and-time-queued-operation) tracks the upstream lifecycle repair, validator regression, and restoration of those operation weights.

The SDK also loses additions to its time-based simulation queue because the slice header is passed by value. R8's new continuations use the persistent height queue and refresh the provider's signer at execution time. Direct signed-transaction tests cover three withdrawal pages, continuation after the cursor lease closes, and provider management changes. The upstream queue repair is included in ENG-915. These corrections concern simulation assurance; production validator behavior and the domain-claim protocol are unchanged.

Upstream verification on 2026-09-08: R14 is known and fixed by [PR #20936](https://github.com/cosmos/cosmos-sdk/pull/20936), merged July 12, 2024. The fix is present by v0.53.0 as `SimWriteState()` before simulation commits. Our pinned v0.50 fork lacks it. R15's ordering was reported in [issue #22393](https://github.com/cosmos/cosmos-sdk/issues/22393); proposed [PR #23382](https://github.com/cosmos/cosmos-sdk/pull/23382) was closed without merging. Time-queue [PR #25722](https://github.com/cosmos/cosmos-sdk/pull/25722) was also closed without merging. Direct inspection of tagged v0.53.8, v0.54.4, and v0.55.0 confirms persistence is fixed but ordering and time-queue propagation remain unchanged. See the [v0.55.0 simulation loop](https://github.com/cosmos/cosmos-sdk/blob/v0.55.0/x/simulation/simulate.go#L194-L248) and [queue implementation](https://github.com/cosmos/cosmos-sdk/blob/v0.55.0/x/simulation/operation.go#L75-L105). Confidence: 99% for this source-based version assessment; no newer-SDK runtime migration was tested.

**R16 — P3, adjacent dependency: tokenfactory's simulation generator does not handle existing denomination candidates. Confidence: 100% for the reproduced rejection and missing existence check; 98% for the random-sequence replay diagnosis.** The pinned tokenfactory generator selects an account and ten-character subdenomination, then delivers without checking existing metadata. Restarting the original random sequence against imported populated state caused an already-existing-denomination failure at block 17. This is a generator failure on a valid business rejection, not a demonstrated production defect. [ENG-916](https://linear.app/liftedinit/issue/ENG-916/tokenfactory-simulation-handle-existing-denomination-candidates-after) tracks bounded candidate validation and a duplicate regression. The import fixture now uses `Seed + 1` to exercise a fresh reproducible history while retaining source accounts, state, time, and chain identity. It does not suppress errors or disable tokenfactory. The dependency still needs arbitrary-collision handling. Evidence: original-seed failure (`/tmp/manifest-review-fixes-after-import-same-seed-failure.log`, local evidence).

## Findings

### R1 — P2: sum overflow discards unrelated denominations’ accrued charges
**Confidence: 100%.**

At [accrual.go:155](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/x/billing/keeper/accrual.go#L155), `totals, err = SafeAddCoins(...)` replaces the accumulated totals with nil when a denomination sum overflows. `markOverflow` then removes the offending denomination from that already-empty result. Earlier totals for other denominations are lost.

A concrete valid-price scenario uses one item at 1 `uok`/second, followed by two items in `uoverflow`, each at `floor(2^255 / 7200)` per second. Quantities are one. Hourly SKU prices and the 3,600-second creation reservation fit the SDK integer range. At 7,201 seconds, each large item accrual fits individually, but their sum overflows. The helper returns an overflow for `uoverflow` and **0 uok instead of 7,201 uok**.

[Silent settlement](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/x/billing/keeper/settlement.go#L183) restores only overflowed denominations by clamping them to spendable credit. It does not restore the omitted ordinary denomination. A [terminal close](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/x/billing/keeper/keeper.go#L1175) advances the settlement timestamp and releases reservations, making the omitted charge unrecoverable through later settlement.

Fix: assign the candidate sum to a temporary variable and update `totals` only on success; on overflow, remove only the affected denomination from the previous totals. Add a regression for sum overflow with ordinary denominations before and after the overflowing items, item permutations, and real settlement/close and quote paths.

The real keeper/bank reproduction also succeeded in creating, acknowledging, and closing the lease. The new regression assertion failed as expected: the provider received 0 uok, and the credit account retained its full 10,000 uok. The test source and output are retained at regression source (`/tmp/manifest-module-review-overflow-repro_test.go`, local evidence) and regression output (`/tmp/manifest-module-review-overflow-test.txt`, local evidence); the temporary repository test was removed.

This requires extreme but accepted prices/balances; it is not an ordinary-price exploit or an authorization bypass. Existing denomination-isolation coverage exercises multiplication overflow and does not cover this mixed-denomination sum-overflow case.

### R2 — P2: domain claims prove lease authority, not domain ownership
**Confidence: 99% for squatting; 90% for traffic takeover, conditional on provider behavior.**

[SetItemCustomDomain](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/x/billing/keeper/custom_domain.go#L163) checks authority over a live lease, syntax, reserved suffixes, and first-claim uniqueness. It does not verify control of the domain. An ordinary tenant with a PENDING lease can claim another party’s unclaimed FQDN and prevent that party from claiming it. Cancellation/recreation permits repeated claims subject to transaction fees and existing lease limits.

The [architecture guide](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/x/billing/docs/ARCHITECTURE.md#L644) tells providers to trust the on-chain lookup when routing incoming Host headers. For a domain already pointing to shared provider ingress, provider-managed HTTP-01 validation does not independently bind that domain to the claiming blockchain tenant. Routing to the wrong tenant is a conditional consequence of following that recipe without additional ownership checks. This is an inference from the module contract and the [ACME HTTP challenge model](https://www.rfc-editor.org/rfc/rfc8555#section-8.3); provider implementation was not supplied.

Fix: define a verifiable claim/activation protocol binding domain, tenant, and deployment, with expiry and legitimate-owner recovery. A provider-verified DNS challenge and on-chain attestation is one possible design. Keep DNS/network access outside consensus execution. Document that registry membership alone is insufficient, and require providers to check ACTIVE state and provider identity before serving. Existing administrative clear authority is useful, but it needs an ownership/recovery policy.

Open question: do deployed providers already verify tenant domain control independently, and how are squatted claims recovered?

### R3 — P2: default CLI quote covers 100 leases; default withdrawal processes 50
**Confidence: 100%.**

[query.go:464](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/x/billing/client/cli/query.go#L464) registers SDK pagination flags, inheriting `--limit=100`. The CLI forwards that nonzero limit. The [keeper default](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/x/billing/keeper/querier.go#L418) of 50 applies only to nil/zero limits. The [withdraw transaction](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/x/billing/client/cli/tx.go#L394) defaults to zero, which becomes 50.

With more than 50 active leases, an unqualified `query billing provider-withdrawable` can estimate a different page from the immediately following unqualified provider withdrawal. The query command’s help promises 50.

Executed command-construction reproduction: `CLI query no-flag limit=100; keeper query default=50; withdrawal default=50`.

Fix: set this command’s pagination default to `DefaultProviderWithdrawableQueryLimit`, or deliberately forward zero when omitted. Test actual outgoing requests for omitted and explicit limits, and compare query/transaction page membership.

### R4 — P2: published withdrawal loop reads the wrong transaction response
**Confidence: 100%.**

The [API automation recipe](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/x/billing/docs/API.md#L442) reads `.has_more`, `.next_key`, `.failed_lease_uuids`, and `.withdrawal_count` directly from `manifestd tx ... -o json`. The CLI uses SDK `GenerateOrBroadcastTxCLI`, which prints `sdk.TxResponse`, not a flattened `MsgWithdrawResponse`. Sync broadcast returns admission information; the module result is available only after execution and decoding.

With a standard sync response, the documented loop submits once, obtains `has_more=null`, and announces completion. Later pages and failed-lease retries are silently omitted. This was reproduced with the documented loop and a realistic mocked response envelope.

Fix: wait for inclusion, verify execution success, decode the appropriate `TxMsgData.msg_responses` value, persist the cursor and failures, then continue. Publish a tested helper or reuse the frontend’s existing response-decoding approach. [MIGRATION.md](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/x/billing/docs/MIGRATION.md#L489) already recognizes the sync response limitation.

### R5 — P2: frontend authentication example signs and sends different timestamps
**Confidence: 100%.**

[FRONTEND.md:356](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/docs/FRONTEND.md#L356) captures the timestamp inside the signed message, awaits wallet confirmation, then recomputes it for the token at line 360. If confirmation crosses a second boundary, the server reconstructs a different message and rejects the signature.

Fix: capture one timestamp before signing and reuse it in the message and token. The [integration guide](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/x/billing/docs/INTEGRATION.md#L95) already shows the correct implementation. Consolidate the duplicate recipe and test a delayed wallet response.

### R6 — P3: exported SKU pricing helpers have an undocumented input precondition
**Confidence: 99%.**

[CalculatePricePerSecond](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/x/sku/types/unit.go#L56) and [ValidatePriceAndUnit](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/x/sku/types/unit.go#L87) divide `basePrice.Amount` without validating the Coin. A zero-value amount can panic; a negative exact multiple can return a negative valid rate or no error.

Current SKU message/genesis validation and billing’s conversion wrapper validate first, so no current transaction-reachable financial vulnerability was found here.

Fix: reject invalid/nonpositive Coins at the exported boundary, or document the precondition and keep unchecked arithmetic private. Consolidate the duplicated quotient/remainder calculation. Add nil/negative input cases.

### R7 — P3: SKU simulations suppress storage failures as successful NoOps
**Confidence: 99%.**

For example, [operations.go:163](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/x/sku/simulation/operations.go#L163) combines `err != nil` with an empty catalog and returns a successful “no providers found” NoOp. Similar cases occur around lines 267, 322, 330, and 387.

A read/codec failure can reduce simulated activity instead of failing at its cause. Later invariants may catch persistent corruption, but diagnostics and coverage are weakened.

Fix: propagate unexpected read errors and reserve successful NoOps for genuinely empty/unavailable business state. Verify with a failing store or keeper fixture.

### R8 — P3: randomized operation coverage omits usable administrative and continuation paths
**Confidence: 99%.**

[Billing simulation](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/x/billing/simulation/operations.go#L164) excludes `CreateLeaseForTenant` because the authority has no private key. However, [randomized genesis](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/x/billing/simulation/genesis.go#L22) seeds signable simulation accounts into the allowed list, and the message accepts them. The stated reason is incomplete.

[Provider withdrawal simulation](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/x/billing/simulation/operations.go#L962) never supplies a continuation key. Lifecycle batches are predominantly singletons, leaving shared-tenant multi-lease batch transitions less exercised.

Fix: use allowed simulation signers for the administrative operation and generate continuation and multi-lease batch histories. Preserve focused keeper tests; this finding concerns randomized composition, not an absence of direct tests.

### R9 — P3: UUID tests do not pin the consensus encoding
**Confidence: 100%.**

The [UUID determinism tests](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/pkg/uuid/uuid_test.go#L43) compare current implementation calls with one another. There are no fixed expected generated UUID vectors. A hash constant, byte order, namespace framing, or formatting change can alter future identifiers while these tests pass. The timestamp-extraction test also does not actually decode timestamp bits.

Fix: add golden vectors using fixed time/header/chain inputs and the actual provider/SKU/lease namespaces; include sequences 0, 4095, 4096, and a large counter. Decode and assert timestamp, version, and variant. Require these checks before simplifying the implementation. This is a compatibility-test gap, not evidence of present nondeterminism.

### R10 — P3: lifecycle documentation contradicts stored state
**Confidence: 100%.**

The [SKU RPC comment](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/proto/liftedinit/sku/v1/tx.proto#L25) says existing SKUs continue after provider deactivation. The implementation deactivates them through a bounded cascade; existing leases continue. Generated service comments inherit the mistake.

[Billing API field notes](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/x/billing/docs/API.md#L1796) say `custom_domain` is cleared on terminal transitions. The implementation deliberately retains the historical field and releases the live reverse index.

Fix: correct the proto source and regenerate comments; document the distinction between historical field values and live claims. Integrators should use state/live lookup to decide whether a domain is currently claimed.

### R11 — P3: release toolchain and one dependency need maintenance updates
**Confidence: 100% for version facts; no reachable module vulnerability demonstrated.**

The repository declares Go 1.26.7 and pins it in [Dockerfile](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/Dockerfile#L1) and CI. As of this review, 1.26.8 is the latest patch on that supported branch and 1.27.1 is the latest major-series patch. [Official release history](https://go.dev/doc/devel/release).

Update the coordinated 1.26 pins to 1.26.8 at minimum. A 1.27 migration should also validate analysis-tool and dependency compatibility: local Go 1.27 runs warned that Sonic fell back to encoding/json, and installed analysis tools required the pinned 1.26 toolchain to load successfully. Neither observation establishes consensus divergence.

The pinned `golang.org/x/crypto v0.55.0` predates fixes in v0.56.0 for two SSH deadlocks. The module scan found no affected-package or function trace, so this is dependency maintenance rather than an exploitable SKU/billing finding. Evaluate the update against all release build graphs. [GO-2026-6354](https://pkg.go.dev/vuln/GO-2026-6354), [GO-2026-6355](https://pkg.go.dev/vuln/GO-2026-6355).

### R12 — P3, adjacent app integration: governance receiving exemption deletes the wrong key
**Confidence: 100% for the mismatch; intended policy needs confirmation.**

[BlockedAddresses](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/app/app.go#L1264) builds a map keyed by Bech32 account addresses, then deletes the string `"gov"` at line 1268. That deletion cannot exempt the governance address as its comment intends. SKU payout validation consequently continues to reject that address.

If governance should receive funds, delete `authtypes.NewModuleAddress(govtypes.ModuleName).String()` and test that exact policy. If blocking is intended, remove the misleading exemption code/comment. This is outside the two module implementations and should be handled as a separate app-policy correction.

## Architecture and idiomatic design

The main design choices are appropriate:

- Keeper, MsgServer, QueryServer, and module wiring are separated; dependencies use narrow interfaces. Typed Collections and registered errors follow the SDK’s supported patterns. [Cosmos Collections guidance](https://docs.cosmos.network/sdk/v0.50/build/packages/collections).
- Public Bech32 identities are canonicalized and persisted as bytes through explicitly versioned storage codecs. Authorization compares decoded identities.
- Billing snapshots prices and creation-time minimum duration. Consumable reservations distinguish each modern lease’s remaining claim from the explicit legacy cohort. Transfers use checked SDK arithmetic and protect other leases’ claims.
- Compound indexes support bounded queries, provider deactivation, and pending expiration. Iterators are closed before index mutations; consensus-visible ordering is explicit.
- Cache contexts protect partial transitions. Provider withdrawal shares its transition engine with the discarded-state query simulation, reducing quote/execution drift at the keeper layer.
- Genesis validation, byte-format migration, reservation cutover, preflight tooling, sequence bounds, index validation, and corruption errors are unusually thorough. The no-mint cutover is deterministic and bank-backed. No additional migration defect was demonstrated.
- Payout recipient policy is checked at admission and settlement; terminal reservation release and bank failure handling receive substantial tests.

The main architectural weakness is the undocumented boundary between domain registry claims and off-chain ownership/routing. Billing’s size also increases the cost of review: it mixes state access, import/export, index reconciliation, expiry, and accounting helpers in a large keeper file. A future responsibility-based file split can improve navigation without changing the keeper API or state format.

## Simplification and modern Go opportunities

| ID | Opportunity | Confidence |
|---|---|---|
| S1 | Replace the manual FNV-1a loop in [uuid.go:129](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/pkg/uuid/uuid.go#L129) with standard `hash/fnv.New64a`, writing exactly the existing byte sequence. Add R9’s vectors first. | 100% |
| S2 | Extract the duplicated generic storage envelope in [SKU](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/x/sku/keeper/storage_codec.go#L28) and [billing](https://github.com/manifest-network/manifest-ledger/blob/85d16028224ea2e3a22a20c2dac7f5a363c943a2/x/billing/keeper/storage_codec.go#L28) into a small internal adapter. Keep prefixes, model conversions, migration policies, and legacy normalizers module-local. | 98% |
| S3 | Use the existing AutoCLI dependency for routine queries, preserving custom transaction parsing and withdrawal behavior. Protect command names, flags, defaults, pagination, and output with contract tests. | 95% |
| S4 | Use `errors.AsType` for typed overflow extraction, `b.Loop()` in accrual benchmarks, and `bytes.Clone`/`slices.Clone` where they clarify defensive copies. These are optional clarity improvements, not correctness fixes. | 99% |

The standard FNV package implements the existing algorithm; no new third-party dependency is needed. [hash/fnv documentation](https://pkg.go.dev/hash/fnv). AutoCLI supports mixing generated and custom commands. [Cosmos AutoCLI guidance](https://docs.cosmos.network/sdk/v0.50/learn/advanced/autocli). The proposed Go APIs are available within the declared toolchain baseline. [Go 1.26](https://go.dev/doc/go1.26), [Go 1.24](https://go.dev/doc/go1.24).

No suitable replacement was identified for the monetary or bounded-pagination adapters. They encode protocol-specific safety semantics that generic money or pagination packages would not preserve. Likewise, a standard UUIDv7 generator that reads the clock or randomness is inappropriate for consensus; even google/uuid’s reader-based v7 constructor still uses current time. Keep deterministic generation and its existing byte contract. New Go 1.27 syntax does not by itself justify consensus-code churn.

## Validation and coverage

Existing module tests passed with:

```text
go test ./x/sku/... ./x/billing/... -coverprofile=/tmp/manifest-module-review.cover -count=1
go test ./pkg/pagination ./pkg/uuid ./app ./cmd/manifestd/cmd -count=1
```

These unit/reproduction runs used the available Linux/amd64 Go 1.27.1-X:nodwarf5 toolchain; static analysis and vulnerability scanning explicitly used Go 1.26.7. The simulation runs use the same local 1.27 toolchain. This is not a claim that production release binaries were tested.

Coverage below excludes generated protobuf/gateway/pulsar statements. It is package-local unit-test statement coverage from the first command, not combined application, simulation, and Docker interchaintest coverage.

| Package area | SKU | Billing |
|---|---:|---:|
| Keeper | 87.9% | 85.9% |
| Handwritten types | 84.9% | 87.8% |
| CLI | 13.8% | 38.4% |
| Simulation package | 29.6% | 45.9% |
| Module wiring | 17.0% | 30.8% |

Keeper/type coverage is substantial, with meaningful authorization, rounding, reservation, corruption, migration, pagination, and fund-conservation tests. Low local CLI/simulation percentages should not be presented as total repository coverage: CI separately runs instrumented app simulations and extensive interchaintests. Nevertheless, R1 and R3–R5 demonstrate real gaps in arithmetic composition and executable client/documentation contracts.

Add independent arithmetic/property checks: permutation-invariant per-denomination accrual, checked arithmetic against a big.Int reference, settlement split across whole/fractional seconds, and reservation conservation across mixed modern/legacy histories. Add malformed-codec fuzz seeds and R9’s fixed UUID vectors. No Go fuzz targets were found in either module. These additions should complement the existing focused tests rather than inflate coverage by mirroring implementation branches.

Security tooling: govulncheck v1.7.0 completed a source/symbol scan of both module graphs using Go 1.26.7; database last-modified timestamp was 2026-09-02T19:12:04Z. It reported **no reachable vulnerable symbols**. The five emitted records were module-only matches covering four advisory IDs. CometBFT’s advisory is fixed on the pinned 0.38.21 branch (0.38.17 was the patched release); deprecated OpenPGP had no affected import/call trace; the two SSH advisories are recorded in R11. [CometBFT advisory](https://github.com/cometbft/cometbft/security/advisories/GHSA-22qq-3xwm-r5x4), [OpenPGP advisory](https://pkg.go.dev/vuln/GO-2026-5932).

Static analysis passed: golangci-lint 2.12.2 reported **0 issues** for both modules with the repository configuration and Go 1.26.7. Initial analysis attempts encountered toolchain/cache incompatibilities and temporary-filesystem exhaustion; the successful retry used the pinned toolchain, an isolated cache, and workspace build scratch space. Initial app/CLI tests needed localhost sockets unavailable in the sandbox; their successful retry used the required access rather than changing source. Evidence: lint result (`/tmp/manifest-module-review-lint.txt`, local evidence), app/helper/CLI test result (`/tmp/manifest-module-review-integration-tests.txt`, local evidence), vulnerability summary (`/tmp/manifest-module-review-vuln-summary.json`, local evidence), coverage profile (`/tmp/manifest-module-review.cover`, local evidence).

R1’s new runtime regression failed on the expected payment assertion after a successful create/acknowledge/close sequence, independently demonstrating the defect despite the existing passing suite. The temporary regression source was removed from the repository after preserving its source/output in /tmp.

All four initial application simulations passed in separate processes, using 100 blocks, seed `2507940531156952020`, committed state, invariant period 5, and `simulation/sim_params.json`. **R14 subsequently showed that these initial runs discarded post-finalize transaction writes; these results are historical and do not establish persistent multi-block histories.** Corrected remediation validation is recorded separately below.

| Test | Result | Runtime |
|---|---|---:|
| Full application (`/tmp/manifest-module-review-TestFullAppSimulation.txt`, local evidence) | Pass | 55.642s |
| Import/export (`/tmp/manifest-module-review-TestAppImportExport.txt`, local evidence) | Pass | 46.381s |
| Simulation after import (`/tmp/manifest-module-review-TestAppSimulationAfterImport.txt`, local evidence) | Pass | 83.306s |
| State determinism (`/tmp/manifest-module-review-TestAppStateDeterminism.txt`, local evidence) | Pass; three replays produced equal app hashes | 137.464s |

For reproducibility, run each `TestFullAppSimulation`, `TestAppImportExport`, `TestAppSimulationAfterImport`, and `TestAppStateDeterminism` individually with `go test ./app -run '^TEST_NAME$' -count=1 -NumBlocks=100 -Enabled=true -Commit=true -Period=5 -Params="$(pwd)/simulation/sim_params.json" -Verbose=false -Seed=2507940531156952020 -timeout=20m`.

An initial combined run completed the full 100-block simulation, then failed when a second test tried to modify the SDK’s already-sealed global address configuration. The successful final verification follows CI’s separate-process structure.

### Remediation validation

The remediation uses the official Go **1.26.8** toolchain on Linux/amd64. Root and interchaintest module manifests now use `golang.org/x/crypto v0.56.0`; the pinned Docker image digest was checked against the official registry manifest. No container release build or interchain upgrade run was performed.

- The root `go test -p 3 ./... -count=1 -coverprofile=...` run passed all packages except an existing migration-preflight test that expected governance to remain blocked. Its expectation was updated to match the corrected app receiving policy, and the complete command package rerun passed. The root run and corrective rerun together cover all root packages; the first command itself exited unsuccessfully. Evidence: root test output (`/tmp/manifest-review-fixes-all-tests.log`, local evidence), command-package rerun (`/tmp/manifest-review-fixes-cmd-tests.log`, local evidence).
- Billing regressions cover item-order permutations and real quote/close/withdraw paths with mixed-denomination sum overflow, including provider payment and credit conservation. CLI tests inspect outgoing pagination requests and reject failed, missing, incorrectly indexed, or wrongly typed withdrawal results. Pricing helpers reject invalid coins; UUID vectors pin existing consensus bytes.
- Seven executable documentation tests pass, including delayed wallet signing, multi-page withdrawals, receipt/checkpoint recovery, failed execution, and ambiguous broadcast handling. They run the published snippets with controlled external responses and are now included in unit-test CI. Evidence: documentation tests (`/tmp/manifest-review-fixes-doc-examples.log`, local evidence).
- The final billing simulation package passes, including signed allowed-list lease creation and three-page withdrawal continuation after cursor-lease closure and provider address rotation. A separate BaseApp regression proves the persistence adapter retains writes across three commits and subsequent block reads.
- Race-instrumented tests pass for all selected `x/sku/...`, `x/billing/...`, and `pkg/uuid/...` packages. The initial combined run exhausted the default ten-minute timeout in billing keeper tests without reporting a race; its complete keeper rerun passed with a longer timeout in 1,029.192 seconds on the loaded host. Evidence: combined race output (`/tmp/manifest-review-fixes-race.log`, local evidence), billing keeper race rerun (`/tmp/manifest-review-fixes-billing-keeper-race.log`, local evidence).
- Scoped static analysis reports **0 issues**, including the final app harness changes. Source-level vulnerability analysis reports **no reachable vulnerable symbols** in the selected SKU/billing graph after the dependency update. Module-only OpenPGP and CometBFT records remain subject to the reachability/branch qualifications above. Evidence: lint (`/tmp/manifest-review-fixes-lint-final.log`, local evidence), app lint (`/tmp/manifest-review-fixes-app-lint.log`, local evidence), vulnerability scan (`/tmp/manifest-review-fixes-vuln.json`, local evidence).

The corrected import harness preserves the source block timestamp and chain identity. A zero-height export resets heights without rewinding time behind persisted lease settlements. This corrects test context construction without relaxing production genesis validation. Its store comparison also follows pinned wasmd's own test by excluding only Wasm's per-block transaction counter (`TXCounterPrefix`), which genesis intentionally omits and the next block resets. Other Wasm keys remain compared; SKU and billing have no comparison exclusions.

All four corrected committed application simulations pass with 100 blocks per run, invariant period 5, `simulation/sim_params.json`, and seed `2507940531156952020`. The post-import continuation uses seed `2507940531156952021`; deterministic replay runs the original seed three times and compares committed app hashes.

| Corrected test | Result | Runtime |
| --- | --- | ---: |
| Full application (`/tmp/manifest-review-fixes-validated-TestFullAppSimulation.log`, local evidence) | Pass | 47.548s |
| Import/export (`/tmp/manifest-review-fixes-validated-TestAppImportExport.log`, local evidence) | Pass; SKU/billing stores match without exclusions | 130.598s |
| Simulation after import (`/tmp/manifest-review-fixes-validated-TestAppSimulationAfterImport.log`, local evidence) | Pass; 100 source blocks and 100 continuation blocks | 370.279s |
| State determinism (`/tmp/manifest-review-fixes-validated-TestAppStateDeterminism.log`, local evidence) | Pass; three equal committed app hashes | 269.581s |

These results supersede the initial simulation evidence for persistent histories. They retain the explicit PoA validator-mutation exclusion described in R15 and do not close the tokenfactory generator's arbitrary-collision gap in R16. Reproduce with the earlier separate-process commands and `GOTOOLCHAIN=go1.26.8`; the import test derives its continuation seed internally.

## PR review follow-up — 2026-09-09

[Claude's review of `aebd057`](https://github.com/manifest-network/manifest-ledger/pull/179#issuecomment-5603383436) identified four remaining gaps. Each was confirmed against the source; none requires changing production UUID generation or pricing behavior.

| Gap | Confidence | Correction |
| --- | --- | --- |
| Billing golden vectors and examples used `billing-lease`, while the keeper uses `billing` | 100% | Use the production module constants in external golden-vector tests; capture expectations from the original manual implementation at `85d1602`; pin two real keeper-created lease UUIDs across sequences 4095–4096. |
| The combined `command -v bash jq` CI guard succeeds when only one tool is available | 100% | Check each tool separately and exit immediately if either is missing. |
| Re-decoding CLI JSON through protobuf accepts aliases and omitted defaults, leaving the operator recipe's field names untested | 100% | Assert literal CLI JSON for populated and final pages, including snake_case keys, string counts, base64 cursors, and false/null/empty-array defaults. |
| SKU architecture documentation copied the old pricing helper and omitted Coin validation failures | 100% | Replace the copied implementation with the exported contract and align the design-decision summary. |

Follow-up validation with Go 1.26.8 passed the complete UUID package, the real keeper UUID regression, and the withdrawal-result CLI tests. A temporary Go source overlay substituting the incorrect `billing-lease` namespace at the keeper call site caused the new regression to fail on the expected literal UUID, demonstrating that it detects namespace drift. The overlay did not modify the production source.

All seven executable documentation tests passed with zero skips. The workflow dependency guard succeeded with both Bash and jq available and failed when either was individually missing. Scoped lint for UUID, billing CLI, and billing keeper reported **0 issues**. Evidence: UUID tests (`/tmp/manifest-pr179-uuid-package.log`, local evidence), namespace mutation check (`/tmp/manifest-pr179-uuid-namespace-mutation.log`, local evidence), documentation tests (`/tmp/manifest-pr179-doc-tests.log`, local evidence), lint (`/tmp/manifest-pr179-review-lint.log`, local evidence).

GitHub reported all 31 checks successful for `aebd057`, including the interchaintest matrix, container/build checks, simulations, and vulnerability analysis. That evidence applies to the preceding head; this follow-up receives its own CI run.

## Limits and remaining questions

No deployed provider, live-chain state, or production price catalog was inspected. Docker interchaintests, a fuzz campaign, release-tagged binaries, and all platform/build-tag vulnerability graphs were not rerun locally. The subsequent successful CI checks for `aebd057` are recorded above and do not certify later commits. The remediated simulation profile excludes randomized validator mutations pending ENG-915.

The remaining decisions are:

1. Is the current release already live, and which module consensus versions and parameter sets are deployed?
2. How does post-deployment domain verification bind ownership to tenants, and what is the recovery procedure? The user explicitly postponed this policy work.
3. What provider/tenant/denomination volumes and block-time budget should define scale benchmarks? Existing bounds are valuable, but this review did not certify maximum-state throughput.

The local governance correction follows the existing documented receiving exemption. Domain verification and claim recovery remain deferred to ENG-62.
