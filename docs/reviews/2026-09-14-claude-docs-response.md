# Claude documentation follow-up — September 14, 2026

Reviewed [Claude's audit of `9659d16`](https://github.com/manifest-network/manifest-ledger/pull/179#issuecomment-5664410129).
All four findings describe documentation that still disagreed with existing
behavior. The frontend example could cause an integrator to clear metadata
unintentionally when clearing a provider API URL. The corrections preserve the
existing API, settlement and import contracts.

## Findings and confidence

Confidence applies to each finding and its disposition, not the absence of
undiscovered defects.

| ID | Severity | Correction | Confidence |
| --- | --- | --- | --- |
| F1 | Medium; integration example | The frontend clear-URL example resends the current provider metadata and preserves its other fields. It distinguishes typed byte arrays from REST/base64 values and explains intentional clearing. An executable documentation regression checks the actual example's constructed update. | 99% |
| F2 | Low; reference documentation | Both provider and SKU update flag lists in `MODULE.md` explicitly say that omitted/empty metadata clears it and preserving it requires resending the current hash. | 99% |
| F3 | Informational; state documentation | The billing README's lease field table now distinguishes normal close finalization from accepted CLOSED imports with a remaining interval. | 99% |
| F4 | Informational; protobuf documentation | The source proto and both generated Go comments describe that same imported-state exception. Regeneration changes comments only; field numbers, descriptors and executable Go tokens remain unchanged. | 99% |
| A1 | Low; related operator examples | The provider-reactivation recipes in SKU troubleshooting now resend the current metadata hash, with guidance on converting queried base64 bytes to CLI hex and handling an empty current hash. | 99% |
| A2 | Low; related frontend query guidance | The credit-query example now describes spendable balances and explains that a bank page containing fully locked denominations can return empty balances with a continuation key. Clients must follow that key. | 99% |

The update handler replaces `meta_hash` unconditionally. An omitted or empty
value is a valid explicit clearing operation, so requiring the flag would change
the API contract. The examples now convey the existing behavior. A fetched
provider is a snapshot: these replacement updates do not provide a compare-and-set
operation against concurrent administrator edits.

Normal close paths finalize the cursor at `closed_at`. A supported imported
CLOSED lease can retain an earlier cursor, which specific-UUID withdrawal
finalizes once, including with zero payment. Provider-wide withdrawal visits
ACTIVE leases only. These rules are unchanged.

## Validation and remaining scope

[Commands and results](2026-09-14-claude-docs-validation/README.md) record the
documentation regressions, protobuf regeneration and focused existing Go tests.
The [baseline CI record](2026-09-14-claude-docs-validation/baseline-ci.json) confirms
all 31 checks passed at `9659d16`; the pushed follow-up needs its own CI.
No archived mutation transcripts, historical source snapshots or integrity
drivers were changed.

The earlier [merge and rollout assessment](2026-09-11-claude-hygiene-response.md)
still applies: production snapshot rehearsal, capacity and gas/circuit/RPC
checks, and deployed Fred/Barney integration remain release gates. This
documentation follow-up adds no consensus or migration change. Nothing was
merged or deployed.
