# Frontend / Integrator Cookbook

This guide is for anyone building a wallet, dashboard, explorer, or off-chain service on top of `manifest-1`. Every recipe uses [`@manifest-network/manifestjs`](https://github.com/manifest-network/manifestjs) — the canonical TypeScript client generated from this repo's protos.

> **Why manifestjs and not raw CosmJS?** Raw CosmJS works but you'd have to register the `liftedinit.{billing,sku,manifest}.v1` types yourself and translate every message. manifestjs ships pre-registered protobuf types, amino converters (for Keplr/Leap signing of the chain's custom messages), and a typed query client. Stay on it unless you have a reason not to.

## Audience

Pick the section that matches what you're building:

| You are… | Read |
|---|---|
| A wallet that needs to sign chain-native messages | [Install](#install) → [Signing client](#signing-client-write-path) → [Tenant recipes](#tenant-recipes) |
| A tenant dashboard | [Install](#install) → [Query client](#query-client-read-path) → [Tenant recipes](#tenant-recipes) → [Subscribing to events](#subscribing-to-chain-events) |
| A provider control plane | [Install](#install) → [Provider recipes](#provider-recipes) → [Tenant authentication (ADR-036)](#tenant-authentication-to-your-provider-api) |
| An explorer / indexer | [Install](#install) → [Query client](#query-client-read-path) → [Subscribing to events](#subscribing-to-chain-events) → [Type URL / amino reference](#type-url--amino-name-reference) |

## Install

```bash
npm install @manifest-network/manifestjs
# Peer deps (commonly already present in a CosmJS app):
npm install @cosmjs/proto-signing @cosmjs/stargate @cosmjs/tendermint-rpc
```

manifestjs is a pure-codegen package — its semver tracks the proto surface, not chain releases. As of writing, mainnet runs `v2.3.1` of the chain; pin the `@manifest-network/manifestjs` range whose proto surface matches (see the [releases page](https://github.com/manifest-network/manifest-ledger/releases)).

## Query client (read path)

For read-only state, use `createRPCQueryClient`. It returns a typed object covering every module on the chain (`cosmos.*`, `ibc.*`, `cosmwasm.*`, `osmosis.tokenfactory.*`, `strangelove_ventures.poa.*`, and `liftedinit.{billing,sku,manifest}.v1`).

```ts
import { createRPCQueryClient } from "@manifest-network/manifestjs";

const client = await createRPCQueryClient({
  rpcEndpoint: "https://rpc.example.org:443",   // see chain-registry / Discord
});

// Bank balance — same as any CosmJS app:
const balance = await client.cosmos.bank.v1beta1.balance({
  address: "manifest1...",
  denom: "umfx",
});

// Manifest-specific reads:
const lease = await client.liftedinit.billing.v1.lease({
  leaseUuid: "01902a9b-1234-7000-8000-000000000001",
});

const credit = await client.liftedinit.billing.v1.creditAccount({
  tenant: "manifest1...",
  pagination: { key: new Uint8Array(), limit: 100n },
});
// credit.balances           — one cursor page of all bank balances
// credit.availableBalances  — the same page minus reserved_amounts
// Follow credit.pagination.nextKey to read the next page. Offset/countTotal
// are intentionally rejected so each request stays bounded.
// credit.creditAccount.reservedAmounts — R = live modern remaining tranches + U
// credit.creditAccount.unattributedReservedAmounts — U, shared live historical cohort
// credit.creditAccount.unattributedLeaseCount — exact live historical cohort size
// lease.lease.reservation?.remainingAmounts — this modern lease's remaining tranche A
```

Prefer `createLCDClient` if you need to hit the REST gateway instead of CometBFT RPC (e.g. browser sandboxes that can't open a WS):

```ts
import { createLCDClient } from "@manifest-network/manifestjs";

const lcd = await createLCDClient({ restEndpoint: "https://api.example.org" });
const params = await lcd.liftedinit.billing.v1.params();
```

## Signing client (write path)

For transactions, build a `SigningStargateClient` pre-loaded with the chain's protobuf registry and amino converters. manifestjs provides a one-call helper:

```ts
import { getSigningLiftedinitClient } from "@manifest-network/manifestjs";

// signer: any OfflineSigner — Keplr, Leap, a Web3Auth signer, or a local DirectSecp256k1HdWallet
const signingClient = await getSigningLiftedinitClient({
  rpcEndpoint: "https://rpc.example.org:443",
  signer,
});
```

If you already manage a `SigningStargateClient` yourself (because you also need `cosmwasm` exec messages, or custom client options), grab the registry and amino types separately and merge them into your own client:

```ts
import {
  getSigningLiftedinitClientOptions,
  liftedinitProtoRegistry,
  liftedinitAminoConverters,
} from "@manifest-network/manifestjs";

const { registry, aminoTypes } = getSigningLiftedinitClientOptions();
// or compose with other modules:
const myRegistry = new Registry([
  ...defaultRegistryTypes,
  ...wasmTypes,
  ...liftedinitProtoRegistry,
]);
const myAminoTypes = new AminoTypes({
  ...createWasmAminoConverters(),
  ...liftedinitAminoConverters,
});
```

### Browser wallets (Keplr / Leap)

Both Keplr and Leap implement `getOfflineSignerAuto(chainId)` (or `getOfflineSigner(chainId)`). For Ledger-backed accounts, use `getOfflineSignerOnlyAmino(chainId)` — the amino converters above make the chain's custom messages signable on Ledger.

```ts
await window.keplr.enable("manifest-1");
const signer = window.keplr.getOfflineSignerAuto
  ? await window.keplr.getOfflineSignerAuto("manifest-1")
  : window.keplr.getOfflineSigner("manifest-1");

const [{ address: tenant }] = await signer.getAccounts();
```

Make sure your suggestion to `experimentalSuggestChain` lists `umfx` (and any factory-token your dApp uses) under `currencies` and `feeCurrencies` so Keplr renders amounts correctly.

### Programmatic / server-side

For backend services or tests, use a local mnemonic-backed signer:

```ts
import { DirectSecp256k1HdWallet } from "@cosmjs/proto-signing";

const signer = await DirectSecp256k1HdWallet.fromMnemonic(MNEMONIC, {
  prefix: "manifest",
});
```

## Tenant recipes

Two equivalent patterns sit on top of the registry:
- **`MessageComposer.encoded.<msgName>(value)`** — returns a `{ typeUrl, value }` ready for `signAndBroadcast`. Use this when you want to bundle multiple messages in one tx.
- **Module RPC clients** — `client.liftedinit.billing.v1.fundCredit(...)` etc. Use this when you're sending one message and want a typed response.

Either way you end up sending the same on-chain message.

### Set, preserve, or clear a provider API URL

`MsgUpdateProvider` keeps legacy patch semantics for `apiUrl`: an empty string
preserves the stored URL. New clients clear it explicitly with
`clearApiUrl: true`. A non-empty `apiUrl` and `clearApiUrl: true` are mutually
exclusive and the chain rejects that combination.

```ts
const clearProviderAPIURL = liftedinit.sku.v1.MessageComposer.encoded.updateProvider({
  authority,
  uuid: providerUuid,
  address: providerAddress,
  payoutAddress,
  metaHash: new Uint8Array(),
  active: true,
  apiUrl: "",
  clearApiUrl: true,
});

// To preserve the current URL while updating other fields, leave both values
// at their protobuf defaults: apiUrl: "", clearApiUrl: false.
```

### Fund a tenant's credit account (`MsgFundCredit`)

```ts
import { liftedinit } from "@manifest-network/manifestjs";

const fundCredit = liftedinit.billing.v1.MessageComposer.encoded.fundCredit({
  sender: tenant,
  tenant,                                      // can be anyone — funding someone else is fine
  amount: { denom: "upwr", amount: "1000000" },
});

const fee = "auto";                            // or { amount: [{ denom: "umfx", amount: "5000" }], gas: "200000" }
const res = await signingClient.signAndBroadcast(tenant, [fundCredit], fee, "fund my credit");
console.log("tx hash:", res.transactionHash);
```

`MsgFundCreditResponse` carries `creditAddress` and `newBalance` — decode the response from `res.msgResponses[0]` if you need them.

### Create a lease (`MsgCreateLease`)

A lease is one or more `LeaseItemInput`s. All items must belong to the same provider. `serviceName` is optional but **all-or-nothing per lease**: if any item sets it, every item must.

```ts
import { liftedinit } from "@manifest-network/manifestjs";

// Single-item legacy lease:
const create = liftedinit.billing.v1.MessageComposer.encoded.createLease({
  tenant,
  items: [
    { skuUuid: "01912345-...-0123456789ab", quantity: 1n, serviceName: "" },
  ],
  metaHash: new Uint8Array(),                  // optional, ≤ 64 bytes
});

// Stack-mode (same SKU twice with different services):
const stack = liftedinit.billing.v1.MessageComposer.encoded.createLease({
  tenant,
  items: [
    { skuUuid: "01912345-...-aaaa", quantity: 1n, serviceName: "web" },
    { skuUuid: "01912345-...-aaaa", quantity: 1n, serviceName: "db"  },
  ],
  metaHash: hexToBytes("a1b2c3..."),           // hash of off-chain deployment manifest
});

const res = await signingClient.signAndBroadcast(tenant, [create], "auto");
// MsgCreateLeaseResponse.lease_uuid identifies the new lease (also surfaced in the `lease_created` event).
```

> `quantity` is a `bigint` because protobuf `uint64` deserialises to `BigInt` in JS. If you accept user input, parse with `BigInt(input)` rather than `Number()`.

### Set or clear a custom domain (`MsgSetItemCustomDomain`, v2.1.0+)

Mutates `LeaseItem.custom_domain` after creation. Sender must be the tenant, the module authority, or an address in `params.allowed_list`. The lease must be PENDING or ACTIVE.

```ts
const setDomain = liftedinit.billing.v1.MessageComposer.encoded.setItemCustomDomain({
  sender: tenant,
  leaseUuid: lease,
  serviceName: "web",                          // pass "" for a 1-item legacy lease
  customDomain: "app.example.com",
});

await signingClient.signAndBroadcast(tenant, [setDomain], "auto");

// Clear it:
const clear = liftedinit.billing.v1.MessageComposer.encoded.setItemCustomDomain({
  sender: tenant,
  leaseUuid: lease,
  serviceName: "web",
  customDomain: "",                            // empty string clears
});
```

**Lower-case `customDomain` client-side before signing** — `MsgSetItemCustomDomain.ValidateBasic()` rejects mixed case (it calls `IsValidFQDN` directly), so the tx fails before reaching the keeper. The keeper does its own `strings.ToLower(strings.TrimSpace(...))` as defence-in-depth, but you can't rely on it as a normalisation point for input. See [`x/billing/docs/API.md`](../x/billing/docs/API.md#set-item-custom-domain) for the full FQDN rules and reserved-suffix behaviour.

### Cancel a pending lease (`MsgCancelLease`)

```ts
const cancel = liftedinit.billing.v1.MessageComposer.encoded.cancelLease({
  tenant,
  leaseUuids: [lease],                         // 1–100 leases, all must be your own PENDING leases
});
await signingClient.signAndBroadcast(tenant, [cancel], "auto");
```

### Close an active lease (`MsgCloseLease`)

Tenant, provider, or authority can close. Final settlement happens at close.

```ts
const close = liftedinit.billing.v1.MessageComposer.encoded.closeLease({
  sender: tenant,
  leaseUuids: [lease],
  reason: "service no longer needed",          // ≤ 256 chars, applied to all
});
await signingClient.signAndBroadcast(tenant, [close], "auto");
```

## Provider recipes

### Acknowledge or reject (`MsgAcknowledgeLease` / `MsgRejectLease`)

```ts
const ack = liftedinit.billing.v1.MessageComposer.encoded.acknowledgeLease({
  sender: providerKey,
  leaseUuids: [lease],                         // batch up to 100; same provider; atomic
});
await signingClient.signAndBroadcast(providerKey, [ack], "auto");

// Or reject with a reason:
const reject = liftedinit.billing.v1.MessageComposer.encoded.rejectLease({
  sender: providerKey,
  leaseUuids: [lease],
  reason: "out of capacity",
});
```

Acknowledgement revalidates every lease against the hard pending deadline and each tenant's
post-batch active cap. It succeeds exactly at `createdAt + current pendingTimeout`, fails strictly
afterward even if the lease still queries as PENDING, and applies no part of a failing batch.

### Withdraw (`MsgWithdraw`)

Two mutually-exclusive modes. Specific-leases mode is atomic across the batch; provider-wide mode is paginated.

```ts
// Mode 1 — specific leases (1-100):
const withdraw = liftedinit.billing.v1.MessageComposer.encoded.withdraw({
  sender: providerKey,
  leaseUuids: [lease1, lease2],
  providerUuid: "",
  limit: 0n,
});

// Mode 2 — provider-wide (paginated). You MUST echo the cursor each round,
// or the loop restarts from the first ACTIVE lease and never terminates.
async function withdrawAll(providerUuid: string) {
  let key = new Uint8Array();                   // empty = start from the beginning
  while (true) {
    const msg = liftedinit.billing.v1.MessageComposer.encoded.withdraw({
      sender: providerKey,
      leaseUuids: [],
      providerUuid,
      limit: 100n,                             // max 100; 0 = default 50
      key,                                     // opaque cursor from the previous response
    });
    const res = await signingClient.signAndBroadcast(providerKey, [msg], "auto");
    const decoded = liftedinit.billing.v1.MsgWithdrawResponse.decode(res.msgResponses[0].value);
    if (!decoded.hasMore) return;              // stop when chain says no more
    key = decoded.nextKey;                     // advance past the last processed lease
  }
}
```

Read-only "what would I withdraw" estimates:

```ts
const perLease = await client.liftedinit.billing.v1.withdrawableAmount({ leaseUuid });

// Provider-wide estimates are page-local ordered dry-runs. Every forward page
// is comparable to one provider-wide MsgWithdraw over the same current segment
// because both the query and transaction are capped at 100 leases.
const page = await client.liftedinit.billing.v1.providerWithdrawable({
  providerUuid,
  pagination: { key: new Uint8Array(), limit: 100n },
});
// page.amounts estimates executing this page in index order. Earlier leases
// consume virtual shared tenant balances before later leases are evaluated.
// Failed per-lease simulations are discarded (matching provider-wide tx
// best-effort semantics); successful virtual effects feed later leases, but no
// query state commits. page.leaseCount matches the comparable transaction's
// withdrawalCount, including a successful zero-transfer auto-close.

// Do not loop and sum query pages: separately queried pages can count the same
// shared balance. Submit the comparable transaction and wait for commit. Then
// query the next segment with this page's pagination.nextKey, while the next
// transaction uses the prior transaction response's nextKey.
//
// Never interchange those cursors: the query key is its first unread
// (inclusive) index entry; the transaction key is its last processed lease and
// resumes exclusively. Reverse query pages are read-only estimates with no
// one-transaction analogue. Offset and countTotal are rejected.
```

### Tenant authentication to your provider API

After a lease goes ACTIVE, tenants prove ownership using ADR-036 arbitrary-message signing. The full message format, validation steps, and sample verifier code live in [`x/billing/docs/INTEGRATION.md`](../x/billing/docs/INTEGRATION.md). Frontend side, the call is:

```ts
const message = `manifest lease access ${leaseUuid} ${Math.floor(Date.now() / 1000)}`;
const sig = await window.keplr.signArbitrary("manifest-1", tenant, message);
// Base64-encode the payload and send it as a Bearer token on a GET (not a POST body):
const authToken = btoa(JSON.stringify({
  tenant, lease_uuid: leaseUuid, timestamp: Math.floor(Date.now() / 1000),
  pub_key: sig.pub_key, signature: sig.signature,
}));
await fetch(`${provider.api_url}/v1/leases/${leaseUuid}/connection`, {
  headers: { Authorization: `Bearer ${authToken}` },   // GET
});
```

Use the same recipe with Leap (`window.leap.signArbitrary(...)`) — the API is identical. Web3Auth-derived signers reach this through `OfflineAminoSigner.signAmino`.

## Querying state — common patterns

### Filter leases by state

`stateFilter` is a `LeaseState` enum value, exposed under the chain's bundle namespace. `LEASE_STATE_UNSPECIFIED (0)` returns everything.

```ts
import { liftedinit } from "@manifest-network/manifestjs";

const pending = await client.liftedinit.billing.v1.leasesByTenant({
  tenant,
  pagination: undefined,
  stateFilter: liftedinit.billing.v1.LeaseState.LEASE_STATE_PENDING,
});

const active = await client.liftedinit.billing.v1.leasesByProvider({
  providerUuid,
  pagination: undefined,
  stateFilter: liftedinit.billing.v1.LeaseState.LEASE_STATE_ACTIVE,
});
```

### Look up a lease by domain (v2.1.0+)

```ts
const { lease, serviceName } = await client.liftedinit.billing.v1.leaseByCustomDomain({
  customDomain: "app.example.com",             // case-insensitive; chain lower-cases on lookup
});
// throws if no PENDING/ACTIVE lease has claimed it.
```

### Pagination

All `Query.<list>` queries take a Cosmos `PageRequest`. Use `key` for stable
cursoring across pages. The billing and SKU collection/index lists also accept
SDK-compatible `offset` and explicit `countTotal` requests. Unfiltered
compatibility requests may inspect at most 20,000 physical rows. Value-filtered
requests remain capped at 1,000 physical rows in every mode; this applies to
`LeasesBySKU` with a state filter and `ProviderByAddress` with `activeOnly`.
Requests fail rather than return an inexact page or total. An omitted or zero
limit defaults to 100 without implicitly enabling `countTotal`; clients that
need a total must request it. `countTotal` is ignored when `key` is set,
matching the SDK. The `CreditAccount` and `ProviderWithdrawable` queries are
deliberately cursor-only.

```ts
import { cosmos, liftedinit } from "@manifest-network/manifestjs";

let key: Uint8Array | undefined;
do {
  const { leases, pagination } = await client.liftedinit.billing.v1.leases({
    pagination: cosmos.base.query.v1beta1.PageRequest.fromPartial({ key, limit: 100n }),
    stateFilter: liftedinit.billing.v1.LeaseState.LEASE_STATE_ACTIVE,
  });
  for (const l of leases) handle(l);
  key = pagination?.nextKey;
} while (key && key.length > 0);
```

> Don't import `PageRequest` from the package root — telescope exposes an unrelated helper of the same name there. Always go through `cosmos.base.query.v1beta1`.

### Discover providers and SKUs

```ts
const providers = await client.liftedinit.sku.v1.providers({
  activeOnly: true,
  pagination: undefined,
});

const skus = await client.liftedinit.sku.v1.sKUsByProvider({
  providerUuid,
  activeOnly: true,
  pagination: undefined,
});
// SKU.unit is a Unit enum: UNIT_PER_HOUR (1) or UNIT_PER_DAY (2).
// SKU.basePrice is the price per unit; per-second rate = base_price / 3600 (hour) or / 86400 (day).
```

> Note the camelCase: telescope-generated rpc methods are camelCased, but the all-caps `SKU` becomes `sKUs...`. If your editor's autocomplete struggles, search for `sKU` or use the typed module bundle (`liftedinit.sku.v1`).

## Subscribing to chain events

For push-style reactivity (e.g. provider notification on `lease_created`), open a CometBFT websocket. Custom event types from `x/billing` are emitted as standard ABCI events and queryable via `tm.event='Tx'` plus an attribute filter.

```ts
import { connectComet } from "@cosmjs/tendermint-rpc";

const tm = await connectComet("wss://rpc.example.org/websocket");

const sub = tm.subscribeTx(
  // Tendermint query syntax — match any transaction emitting a `lease_created` event for our provider:
  `lease_created.provider_uuid='${providerUuid}'`,
);

(async () => {
  for await (const tx of sub) {
    const ev = tx.result.events.find(e => e.type === "lease_created");
    const leaseUuid = ev?.attributes.find(a => a.key === "lease_uuid")?.value;
    onPendingLease(leaseUuid!);
  }
})();
```

Common subscriptions for a provider control plane:

| Goal | Subscription query |
|---|---|
| New pending leases for me | `lease_created.provider_uuid='<UUID>'` |
| Tenants cancelling pending leases | `lease_cancelled.provider_uuid='<UUID>'` |
| Auto-close via provider-wide withdraw | `lease_auto_closed.provider_uuid='<UUID>'` |
| Auto-close via `CloseLease` | `lease_closed.provider_uuid='<UUID>'`, then keep rows where `closed_by='credit_exhaustion'` |
| Auto-close via specific-lease withdraw | `provider_withdraw.provider_uuid='<UUID>'`, then keep rows where `auto_closed='true'` |
| Domain set on my leases | `lease_custom_domain_set.provider_uuid='<UUID>'` (v2.2.0+) |
| Domain cleared on my leases | `lease_custom_domain_cleared.provider_uuid='<UUID>'` (v2.2.0+) |

> **Credit exhaustion emits different events by path.** `lease_auto_closed` fires **only** from provider-wide withdraw. A close triggered by `MsgCloseLease` emits `lease_closed` with `closed_by='credit_exhaustion'`, and one triggered by a specific-lease `MsgWithdraw` emits `provider_withdraw` with `auto_closed='true'`. To catch *every* credit-exhaustion closure, watch all three.

The full event catalogue and attributes live in [`x/billing/docs/API.md#events`](../x/billing/docs/API.md#events).

## Type URL / amino name reference

manifestjs registers all of the following automatically. You only need this table when you're talking to a wallet that wants the type URL by string (e.g. `signer.signDirect` payloads) or building a low-level transaction by hand.

### x/billing

| Type URL | Amino name |
|---|---|
| `/liftedinit.billing.v1.MsgFundCredit` | `lifted/billing/MsgFundCredit` |
| `/liftedinit.billing.v1.MsgCreateLease` | `lifted/billing/MsgCreateLease` |
| `/liftedinit.billing.v1.MsgCreateLeaseForTenant` | `lifted/billing/MsgCreateLeaseForTenant` |
| `/liftedinit.billing.v1.MsgAcknowledgeLease` | `lifted/billing/MsgAcknowledgeLease` |
| `/liftedinit.billing.v1.MsgRejectLease` | `lifted/billing/MsgRejectLease` |
| `/liftedinit.billing.v1.MsgCancelLease` | `lifted/billing/MsgCancelLease` |
| `/liftedinit.billing.v1.MsgCloseLease` | `lifted/billing/MsgCloseLease` |
| `/liftedinit.billing.v1.MsgWithdraw` | `lifted/billing/MsgWithdraw` |
| `/liftedinit.billing.v1.MsgSetItemCustomDomain` | `lifted/billing/MsgSetItemCustomDomain` |
| `/liftedinit.billing.v1.MsgUpdateParams` | `lifted/billing/MsgUpdateParams` |

### x/sku

| Type URL | Amino name |
|---|---|
| `/liftedinit.sku.v1.MsgCreateProvider` | `lifted/sku/MsgCreateProvider` |
| `/liftedinit.sku.v1.MsgUpdateProvider` | `lifted/sku/MsgUpdateProvider` |
| `/liftedinit.sku.v1.MsgDeactivateProvider` | `lifted/sku/MsgDeactivateProvider` |
| `/liftedinit.sku.v1.MsgCreateSKU` | `lifted/sku/MsgCreateSKU` |
| `/liftedinit.sku.v1.MsgUpdateSKU` | `lifted/sku/MsgUpdateSKU` |
| `/liftedinit.sku.v1.MsgDeactivateSKU` | `lifted/sku/MsgDeactivateSKU` |
| `/liftedinit.sku.v1.MsgUpdateParams` | `lifted/sku/MsgUpdateParams` |

### x/manifest

| Type URL | Amino name |
|---|---|
| `/liftedinit.manifest.v1.MsgPayout` | `lifted/manifest/MsgPayout` |
| `/liftedinit.manifest.v1.MsgBurnHeldBalance` | `lifted/manifest/MsgBurnHeldBalance` |

`PayoutPair` (carried inside `MsgPayout`) has the amino name `lifted/manifest/payout-pair`.

## Tips

- **`uint64` is a `bigint`.** Telescope generates `bigint` for `uint64` (e.g. `quantity`, `limit`, `pendingTimeout`). Always parse user input with `BigInt(...)` and serialise with `.toString()` — `JSON.stringify` does not handle bigints natively.
- **`bytes` is a `Uint8Array`.** `metaHash` and event-attribute byte values are not hex-encoded for you. Use `bytesFromBase64` / `base64FromBytes` from the package's helpers, or hex utilities from your bundler.
- **Coin denoms.** Mainnet's gas/fee denom is `umfx`; the staking denom is `upoa`. Tenant credit accounts can hold *any* denom that matches a SKU's `basePrice.denom` — typically a TokenFactory-issued credit token (e.g. `factory/manifest1.../upwr`).
- **`MsgUpdateParams` is replace-style.** When wiring a governance dApp, query the current params first and merge your delta in — sending an unset list field will clear it. (The CLI implements PRESERVE-on-omit; the bare protobuf message does not.)
- **Don't construct credit addresses yourself.** `Query.creditAddress({ tenant })` returns the deterministic derived address. The derivation rule may evolve; the query won't.
- **Decoding `msgResponses`.** A successful `signAndBroadcast` populates `res.msgResponses` with raw protobuf bytes. To read structured response fields, decode with the matching response type: `MsgCreateLeaseResponse.decode(res.msgResponses[0].value)`.

## Related documentation

- Module APIs: [`x/billing/docs/API.md`](../x/billing/docs/API.md), [`x/sku/docs/API.md`](../x/sku/docs/API.md)
- Provider tenant-auth: [`x/billing/docs/INTEGRATION.md`](../x/billing/docs/INTEGRATION.md)
- Module concepts: [`x/billing/README.md`](../x/billing/README.md), [`x/sku/README.md`](../x/sku/README.md)
- Top-level chain overview: [`README.md`](../README.md)
- manifestjs source / issues: <https://github.com/manifest-network/manifestjs>
