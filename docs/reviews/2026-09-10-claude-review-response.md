# PR179 Claude follow-up — 2026-09-10

This addresses Claude's [first follow-up](https://github.com/manifest-network/manifest-ledger/pull/179#issuecomment-5608255107)
and [mutation audit and correction](https://github.com/manifest-network/manifest-ledger/pull/179#issuecomment-5609198400)
on `ad85c7b`/`7730e70`. Earlier findings remain in the
[2026-09-09 response](2026-09-09-claude-review-response.md); the F13 disposition
there is corrected rather than left as an unqualified dismissal. Confidence
scores describe confidence in each disposition, not exploit probability or a
claim that no vulnerabilities remain.

## Findings and decisions

| Finding | Disposition | Confidence |
| --- | --- | --- |
| F13 crisis fee refunded by a failing tail | **Accepted with scope; medium operational exposure.** The message cache rolls back the ConstantFee after work, while ante fee/sequence and gas remain. This is pre-existing SDK behavior shared by all crisis routes. See the correction and operational controls below. | 100% for local reproduction; live policy unverified |
| Expiry cursor termination lacks the failure-at-page-end regression | **Test gap fixed; low.** A 101-row fixture fails candidate 100 and requires the healthy final row to expire. The correct exclusive cursor terminates with exactly 100 successes; a read-only `StartInclusive` mutation exhausts the finite test gas budget in under one second. No production cursor change was needed. | 100% |
| CLOSED timestamps may precede creation or settlement | **Fixed; low, defense in depth.** All static/import/current-state paths require `closed_at >= created_at` and `closed_at >= last_settled_at`; equality and historical partial settlement remain valid. This is separate from the completed original F07 fix; no fund loss was demonstrated. | 99% |
| F52 failed expiration mutates the caller's lease/reservation | **Fixed; low.** Expiration works on copied lease/reservation values and assigns the caller only after cached writes commit. Missing credit, release failure, and late domain-index failure leave both caller memory and stored state unchanged. Success still updates the caller. | 100% |
| F59 overdue PENDING lease can claim or renew a domain | **Fixed; low.** Nonempty claims and idempotent re-sets use the current hard pending deadline and existing `ErrLeaseAcknowledgementDeadlineExceeded` (code 34). Equality is allowed; empty clears remain available until terminal state. | 100% |
| F55 runtime invariant omits block-time validation | **Fixed; low.** Reservation accounting now reuses `ValidateWithBlockTime` after strict current-state validation. Future creation, settlement, and close timestamps break the invariant; equality passes. This does not add new checks for acknowledged/rejected/expired timestamps. **Subsequent correction:** this exposed a zero-height export context with no time; see the [export follow-up](2026-09-10-claude-export-review-response.md). | 99% |
| F56 lease-free preflight claims source format is v4 | **Fixed; low.** Schema 6 introduces `lease_free`/`none` for strictly valid zero-claim state, including funded accounts. Nonzero orphaned reservations, counts, or cohorts fail with an explicit ambiguity error. The tool cannot infer consensus version without lease format markers and does not guess a repair. | 99% |
| F48 genesis duplicates lease-item shape validation | **Simplified.** Genesis projects bounded item inputs through the shared shape validator; persisted pricing and domain validation remain separate. Error wording now comes from the shared validator. | 99% |
| F63 allowed-list authorization repeatedly decodes stored members | **Simplified.** Decode the candidate once, then use standard-library `slices.Contains` on canonical or all-uppercase encodings. Preserve SDK identity behavior for public raw Params values; no global cache or extra dependency. | 99% |
| F61 acknowledgment after provider/SKU deactivation | **Intentional policy retained and tested.** Existing PENDING offers remain acknowledgeable, as API documentation already specifies. Current payout, deadline, and active-cap checks still apply. Deactivation prevents new offers and does not retroactively revoke accepted creation terms. | 99% |
| F57 reuse provider batch helper for cancellation | **Not adopted; contracts differ.** Cancellation permits one tenant across multiple providers and checks ownership before state. The other helper permits multiple tenants for one provider and checks state first. New regressions pin both cancellation behaviors. | 100% |
| F09/D1 payout-rotation instructions | **Documentation fixed.** An ordinary rotation recipe withdraws first, processes every page/failure, preserves provider fields, and explains accrual between withdrawal and update. Blocked/self-colliding payouts use a separate repair-first flow because withdrawal cannot succeed there. | 100% |
| F62 governance receiving exception unnamed in operator docs | **Documentation fixed.** Migration, troubleshooting, and module guides name `gov` as the sole module-account bank receiving exemption. Existing app regression pins it; no new authorization or policy change. | 100% |
| Preflight exported Go API gained a required auth argument | **Compatibility note added.** Migration guide names the fourth `authtypes.GenesisAccounts` argument, full-auth-export requirement, explicit valuation time, and schema 5→6 consumer change. No protobuf field or storage-format change. | 100% |
| CodeQL 49/50/51 | **Already fixed/stale.** Earlier cleanup removed the useless assignments; old inline comments predate that commit. | 99% |
| CodeQL 52 sensitive `reflect` import | **False positive, dismissed on GitHub.** Deterministic `Kind`/`IsNil` checks only reject typed-nil codecs during keeper construction. Encode/Decode do not use reflection. A construction-boundary comment now documents this. | 100% |

## F13 correction and operational exposure

The earlier reasoning inspected the crisis fee debit and retained transaction
gas meter but missed the transaction cache that encloses the debit. A valid
`MsgVerifyInvariant` followed by a deliberately failing bank send runs the
invariant and then refunds the crisis ConstantFee. Ordinary ante fees remain
paid and the sender sequence advances. With zero ante fees, the caller can
repeat without spending tokens, provided it initially has enough spendable
funds to cover the temporarily debited ConstantFee. Gas is still consumed.

The committed manifest-1 genesis sets `block.max_gas = -1`, ships an empty
circuit disabled list, and its operator setup guides use `0umfx` minimum gas
prices. These historical files do not establish live network settings. The
billing invariant still exports and validates the whole module, so work and
peak memory grow with state. The previous linear index-cost fix remains valid.

The [operations guide](../../x/billing/docs/OPERATIONS.md) supplies read-only
audit commands and the exact authorized circuit disable/reset commands. The
app's router-level circuit gate covers nested authz, group, and governance
message execution as well as direct transactions. It does not stop direct
EndBlock or export assertions. Finite positive consensus block gas bounds
individual transaction gas and stops later transactions after block gas is
exhausted, including gas charged for failures. The last transaction can cross
the remaining block budget before that charge. Nonzero validator
minimum gas prices affect local CheckTx only and are not a consensus fee floor;
Manifest's ProcessProposal handler currently accepts proposals without that
price check. Raising the crisis ConstantFee alone is not sufficient.

A [subsequent review](2026-09-10-claude-export-review-response.md) also identified the unsigned Simulate path and qualified circuit-only protection for composed simulations.

No live policy was changed. Selecting and deploying an appropriate control,
measuring production-size invariant work, and any streaming redesign remain
explicit operational follow-ups. A streaming implementation would reduce
allocation but would not fix the generic refundable crisis fee.

## Validation and limits

Final validation is recorded in the [follow-up evidence](2026-09-10-claude-review-validation/README.md).
The review includes concrete same-block lifecycle, cross-module bank/crisis,
nested-message control, corruption/rollback, migration-format, and SDK address
identity probes. It does not claim that all eight broad cross-cutting hunts
requested in the original external audit have been completed. No live-chain
policy audit, production-cardinality rehearsal, real provider/wallet rollout,
or proof of vulnerability absence is claimed. Earlier deployment gates and
coverage limitations remain applicable.
