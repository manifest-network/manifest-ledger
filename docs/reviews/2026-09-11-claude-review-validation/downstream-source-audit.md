# Bounded downstream source audit — September 11, 2026

This read-only review traced consumption of billing closure events and the
state-reconciliation fallback in the local Fred and Barney checkouts. It did
not run either application, execute sibling tests, inspect deployments, or
change either repository. The scope was the event-to-cleanup path and its
documented timing implications, not a comprehensive audit of either consumer.

## Source identity

| Repository | Inspected commit | Working-copy qualification |
| --- | --- | --- |
| Fred | `c04e6a2aac3e9dbf0c979c490e2ceae2fee3d611` | The production Go files referenced below were unchanged relative to this commit. The checkout contained unrelated modified/untracked documentation and scratch files. |
| Barney | `484567e603d076dd753b79f7eb391724a5c18b04` | The registry, polling, configuration and layout files referenced below were unchanged relative to this commit. The checkout contained other work outside these inspected paths. |

Commits were identified with `git rev-parse HEAD`; `git diff --name-only` on
the inspected source paths checked the working-copy qualification. Identifier
searches for `lease_closed`, `lease_auto_closed`, `provider_withdraw`,
`duration_seconds` and `durationSeconds` were followed by reading the event
parser, routing, reconciliation, configuration and polling implementations.
Source pointers below are relative to the named repository at its listed
commit. They identify the relevant function or declaration, not a claimed
test run or live observation.

## Event duration and Fred event coverage

**No consumer treating `lease_closed.duration_seconds` as lease lifetime was
identified in the inspected source. Confidence: 99% for this bounded source
finding.** Fred's `internal/chain/events.go:53` defines `LeaseEvent` with event
type, lease UUID, provider UUID and tenant only. Its parser at line 510 does
not read the duration attribute. Searches of Barney's production `src` code
found no raw closure-event/duration-attribute consumer. Other duration fields,
such as credit-runway estimates and Fred metrics, are separate concepts.
This finding does not establish what other applications or deployed versions
consume. The ledger event field and its final-settlement-interval semantics
remain unchanged.

Fred's WebSocket subscriptions in `internal/chain/events.go:354` include
cross-provider `lease_auto_closed` and provider-specific `lease_closed`, but
not `provider_withdraw`. The parser at line 510 also lacks a
`provider_withdraw` branch. A specific-UUID withdrawal that auto-closes a
lease therefore does not enter Fred's immediate closure handler through that
event. Confidence: 99%.

Subscription alone is not a teardown guarantee: `internal/provisioner/bridge.go:47`
forwards events unchanged, and `Manager.PublishLeaseEvent` in
`internal/provisioner/manager.go:617` routes only created, closed and expired
lease events. It ignores `LeaseAutoClosed`. The watcher in
`internal/watcher/watcher.go:113` uses an own-provider auto-close to update
tenant tracking; a cross-provider auto-close can trigger another withdrawal.
Neither branch directly deprovisions a workload. Thus provider-wide auto-close
also relies on reconciliation for teardown in this source version.
The ordinary `LeaseClosed` route reaches `HandlerSet.processLeaseClose` in
`internal/provisioner/handler_set.go:145`, which requests deprovisioning.
Confidence: 99%.

## Fred's reconciliation fallback and timing

The missing immediate event path does not establish a permanent resource leak.
`ReconcileAll` in `internal/provisioner/reconciler.go:250` collects the provider's
PENDING and ACTIVE leases. Remaining backend-reported provisions become orphan
candidates at line 585. `processOrphan` at line 2019 requests an executable
cleanup decision and, when authorized, calls `DeprovisionOrphan` at line 2079.

`ObserveTerminalOrphan` in
`internal/provisioner/placement/reconciliation_sweep.go:1105` requires an exact
backend observation, an operation claim and a positive chain read confirming
the same provider's lease is CLOSED, REJECTED or EXPIRED. Missing records,
unknown state and failed reads do not authorize teardown. The deprovision
target comes from that observation at line 1187. The existing
`TestReconciler_ReconcileAll_OrphanProvision` in
`internal/provisioner/reconciler_test.go:1225` models a lease closed on chain
but still provisioned and asserts a deprovision call. That test was read,
not rerun during this audit. Confidence: 99% for the source-defined fallback.

The default `reconciliation_interval` is five minutes in
`internal/config/config.go:264`. Production wiring supplies it in
`cmd/providerd/main.go:497`, runs `RunOnce` at startup at line 571, and starts
periodic reconciliation at line 599. `Reconciler.Start` in
`internal/provisioner/reconciler.go:2193` waits an initial random delay of
zero to less than 25% of the interval, then starts the interval ticker at
line 2209. With the default, the first periodic tick follows approximately
five to six-and-a-quarter minutes after periodic startup; the separate
startup reconciliation has already been attempted.

**Practical inference, confidence: 95%:** a workload may remain provisioned
after its lease closes through a specific withdrawal until a successful
reconciliation sweep requests cleanup and the backend completes it. Five
minutes is a configured cadence, not a teardown deadline. A closure during
a sweep, a long sweep, unavailable RPC/backend inventory, ambiguous ownership,
an in-flight operation, or a failed backend action can defer cleanup further.
The code retries periodic reconciliation; this review did not measure actual
delay, resource use, or deployment-specific behavior.

## Barney's independent state refresh

`src/components/layout/MainLayout.tsx:47` mounts
`useRegistryReconciliation`. The hook in
`src/hooks/useRegistryReconciliation.ts:67` reads complete ACTIVE/PENDING tenant
lease lists and passes their states to registry reconciliation at line 81.
`getLeasesByTenant` in `src/api/billing.ts:105` collects pages and rejects
inconsistent or incomplete results instead of accepting a partial inventory.

`src/registry/appRegistry.ts:650` records absence from the live set as
`chainState = 'absent'`. `deriveAppStatus` at line 104 then marks the app
stopped, while preserving an applicable existing failure verdict. This is a
chain-state observation; it does not prove backend resources are already gone
or cause Fred teardown.

`src/config/constants.ts:36` sets the base refresh interval to 15 seconds.
`src/hooks/useVisibilityPolling.ts` uses completion-based timers, pauses while
the tab is hidden, and runs immediately on visible mount/resume. Consecutive
failures back off to at most eight times the base delay, or 120 seconds, plus
request duration. A wallet address and the mounted application are required.
These are source-defined conditions, not a universal 15-second update bound.
Confidence: 99% for the source behavior.

## Limits and integration follow-up

No deployment SHA/configuration, live event stream, provider interoperability
run, measured reconciliation latency, or end-to-end teardown was verified.
No sibling source was changed and no upstream message or issue was sent.
Other consumers were not searched. The concrete integration follow-up is to
verify all three ledger closure paths against the intended Fred deployment,
including which events cause immediate teardown and when reconciliation is
expected to recover missed events. This is a consumer latency/coverage finding,
not evidence that ledger closure state is incorrect or that resources remain
allocated permanently.
