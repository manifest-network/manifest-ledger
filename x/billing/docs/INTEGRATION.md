# Billing Module Integration Guide

This guide covers how tenants authenticate to provider off-chain APIs after lease creation.

## Provider Off-Chain API Integration

Providers expose a REST API for tenants to retrieve connection details after lease acknowledgement. The API endpoint URL is stored on-chain in the `Provider.api_url` field. This field is optional: it is a proto3 `string` with `omitempty`, so a provider that never set one leaves it as the empty string, omitted from JSON output (a `jq -r '.provider.api_url'` prints `null` for the missing key — it is never a literal JSON `null` value). Tenants and clients must handle this absent case, in which the provider has no off-chain API registered.

### Tenant Flow

1. Tenant queries lease to get `provider_uuid`
2. Tenant queries provider to get `api_url` (may be empty if the provider registered no off-chain API)
3. Tenant calls provider's API with signature-based authentication

### Authentication

Authentication uses [ADR-036](https://docs.cosmos.network/main/build/architecture/adr-036-arbitrary-signature) signature verification without on-chain challenge storage. The tenant proves lease ownership by signing a message containing the lease UUID and timestamp:

**Message format:**
```
manifest lease access {lease_uuid} {unix_timestamp}
```

**Example:**
```
manifest lease access 019abcde-f012-7abc-8def-abcdef012345 1702834946
```

### API Endpoint

```
GET {provider.api_url}/v1/leases/{lease_uuid}/connection
Authorization: Bearer <base64_encoded_auth_token>
```

**Bearer Token Format (base64-encoded JSON):**
```json
{
  "tenant": "manifest1...",
  "lease_uuid": "019abcde-f012-7abc-8def-abcdef012345",
  "timestamp": 1702834946,
  "pub_key": {
    "type": "tendermint/PubKeySecp256k1",
    "value": "<base64_encoded_pubkey>"
  },
  "signature": "<base64_signature>"
}
```

### Provider Validation Steps

1. Decode the Bearer token
2. Query the chain for the lease by UUID
3. Verify lease is ACTIVE
4. Verify tenant address matches lease tenant (derived from pubkey)
5. Verify timestamp is within acceptable window (±5 minutes)
6. Reconstruct the message: `manifest lease access {lease_uuid} {timestamp}`
7. Verify signature using ADR-036 verification

### Wallet Compatibility

ADR-036 ensures compatibility with all major Cosmos wallets:

| Wallet | API |
|--------|-----|
| Keplr | `signArbitrary()` - works with Ledger |
| Leap | Same API as Keplr |
| Ledger | Via Keplr/Leap (Amino signing) |
| Web3Auth | Via CosmJS `OfflineAminoSigner` |

**Keplr Example (JavaScript):**
```js
const message = `manifest lease access ${leaseUuid} ${Math.floor(Date.now() / 1000)}`;

const signature = await window.keplr.signArbitrary(
  "manifest-1",           // chainId
  tenantAddress,          // signer address
  message                 // the message to sign
);

const authToken = btoa(JSON.stringify({
  tenant: tenantAddress,
  lease_uuid: leaseUuid,
  timestamp: Math.floor(Date.now() / 1000),
  pub_key: signature.pub_key,
  signature: signature.signature
}));

fetch(`${providerApiUrl}/v1/leases/${leaseUuid}/connection`, {
  headers: { "Authorization": `Bearer ${authToken}` }
});
```

**Note:** The Cosmos SDK does not include a built-in CLI command for ADR-036 signing. For CLI-based signing, use CosmJS or a custom signing tool. Wallet-based signing (Keplr, Leap) is the recommended approach for end users.

### Provider Signature Verification

For Go-based providers, use the `@keplr-wallet/cosmos` package's verification logic as reference, or use a library that implements ADR-036 verification.

**Verification Steps (pseudocode):**
```
1. Decode base64 Bearer token to get: tenant, lease_uuid, timestamp, pub_key, signature
2. Validate timestamp is within ±5 minutes of current time
3. Query chain: verify lease exists, is ACTIVE, and tenant matches
4. Reconstruct message: "manifest lease access {lease_uuid} {timestamp}"
5. Verify ADR-036 signature using the reconstructed message
6. Verify pub_key derives to the claimed tenant address
```

**ADR-036 Sign Doc Format:**
```json
{
  "chain_id": "",
  "account_number": "0",
  "sequence": "0",
  "fee": {"amount": [], "gas": "0"},
  "msgs": [{
    "type": "sign/MsgSignData",
    "value": {
      "signer": "<tenant_address>",
      "data": "<base64_encoded_message>"
    }
  }],
  "memo": ""
}
```

**Important:** The exact byte representation of the sign doc must match what Keplr produces. Consider using the [`@keplr-wallet/cosmos`](https://www.npmjs.com/package/@keplr-wallet/cosmos) package's `verifyADR36Amino` function as a reference implementation, or test thoroughly against actual Keplr signatures.

### Example Response

```json
{
  "lease_uuid": "...",
  "endpoints": [
    {
      "sku_uuid": "01912345-6789-7abc-8def-0123456789ab",
      "service_name": "web",
      "type": "ssh",
      "host": "192.168.1.100",
      "port": 22,
      "credentials": {
        "username": "tenant",
        "key": "..."
      }
    }
  ],
  "status": "running",
  "provisioned_at": "2024-12-16T19:30:00Z"
}
```

When a lease is created in service-name mode (`sku-uuid:quantity:service_name`), the same `sku_uuid` may appear across multiple items, so `service_name` — not `sku_uuid` — is the unique key for a lease's endpoints. Legacy single-item leases (`sku-uuid:quantity`) omit `service_name` and are keyed by `sku_uuid`.

### Security Considerations

| Risk | Mitigation |
|------|------------|
| Replay attacks | Timestamp validation (±5 min window), HTTPS required |
| Provider API spoofing | Tenants verify `api_url` from on-chain provider record |
| Clock skew | 5-minute tolerance, NTP recommended |
| Signature reuse | Message includes lease-specific UUID |

## Deployment Data Upload (POST) - Optional

Tenants can optionally upload deployment data to providers using the same ADR-036 authentication pattern used for connection info retrieval. The on-chain lease stores only a hash of the deployment data (`meta_hash`), while the actual payload is transmitted off-chain.

### When to Use

This feature is optional and depends on the provider's requirements:

| Provider Type | Data Upload Needed | Example |
|--------------|-------------------|---------|
| Fixed SKUs | No | Pre-configured VMs, standard database instances |
| Configurable SKUs | Yes | Custom Kubernetes deployments, tenant-specific settings |

For providers with fixed SKUs (pre-configured resources), tenants create leases without `meta_hash` and providers acknowledge based solely on the SKU selection.

### Workflow

```
1. Tenant prepares deployment manifest (e.g., YAML, JSON)
2. Tenant computes hash: meta_hash = SHA-256(manifest)
3. Tenant creates lease with meta_hash on-chain (lease is PENDING)
4. Tenant POSTs manifest to provider while PENDING
5. Provider validates: SHA-256(received) == lease.meta_hash
6. Provider provisions resources
7. Provider acknowledges lease (lease becomes ACTIVE)
8. Tenant retrieves connection info
```

**Important**: Upload deployment data BEFORE the provider acknowledges. This allows the provider to validate the manifest and provision resources before committing to the lease.

**Pending timeout**: The entire upload → validate → provision → acknowledge sequence must complete within `params.pending_timeout` (default 1800s = 30 minutes; configurable 60..86400 seconds via `MsgUpdateParams`). If the provider does not acknowledge before the lease's `created_at + pending_timeout`, the EndBlocker transitions the lease to `LEASE_STATE_EXPIRED` and releases the tenant's credit reservation. The tenant must then create a new lease (and re-upload the deployment data); any payload already uploaded to the provider is orphaned.

### On-Chain Storage

Only the hash is stored on-chain:
- **Field**: `lease.meta_hash`
- **Max size**: 64 bytes (accommodates SHA-256 and SHA-512)
- **Format**: Raw bytes
- **Mutability**: set at lease creation (revision 0) and thereafter advanced
  only by the lease-update handshake described in
  [Deployment Data Update](#deployment-data-update---optional). It always names
  the manifest version the provider has committed to, so it stays the correct
  reference for a reprovision integrity check.

### Message Format for Signing

```
manifest lease data {lease_uuid} {meta_hash_hex} {unix_timestamp}
```

**Example:**
```
manifest lease data 019abcde-f012-7abc-8def-abcdef012345 a1b2c3d4e5f6... 1702834946
```

### API Endpoint

```
POST {provider.api_url}/v1/leases/{lease_uuid}/data
Authorization: Bearer <base64_encoded_auth_token>
Content-Type: application/octet-stream

<raw payload bytes>
```

### Bearer Token Format

Same structure as connection info, but message includes `meta_hash`:

```json
{
  "tenant": "manifest1...",
  "lease_uuid": "019abcde-f012-7abc-8def-abcdef012345",
  "meta_hash": "a1b2c3d4e5f6...",
  "timestamp": 1702834946,
  "pub_key": {
    "type": "tendermint/PubKeySecp256k1",
    "value": "<base64_encoded_pubkey>"
  },
  "signature": "<base64_signature>"
}
```

### Provider Validation Steps

1. Decode Bearer token
2. Query chain: verify lease exists, tenant matches, and `meta_hash` matches
3. Verify lease is in PENDING state (not yet acknowledged)
4. Verify timestamp within ±5 minutes
5. Verify ADR-036 signature of message: `manifest lease data {uuid} {meta_hash} {ts}`
6. Compute SHA-256 of received payload body
7. Verify `SHA-256(payload) == lease.meta_hash`
8. Accept/reject based on payload content (provider's discretion)

### Payload Size Recommendations

| Location | Limit | Notes |
|----------|-------|-------|
| On-chain | 64 bytes | Hash only, not the payload |
| Off-chain | 1-10 MB | Provider-defined, recommended max |

### CLI Example

```bash
# 1. Prepare and hash deployment manifest
MANIFEST_HASH=$(sha256sum deployment.yaml | cut -d' ' -f1)

# 2. Create lease with meta_hash on-chain
manifestd tx billing create-lease \
  01912345-6789-7abc-8def-0123456789ab:2 \
  --meta-hash "$MANIFEST_HASH" \
  --from tenant

# 3. Query provider's api_url
PROVIDER_API=$(manifestd query sku provider <provider-uuid> -o json | jq -r '.provider.api_url')

# 4. POST deployment data to provider (see auth section for signature generation)
curl -X POST "${PROVIDER_API}/v1/leases/${LEASE_UUID}/data" \
  -H "Content-Type: application/octet-stream" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  --data-binary @deployment.yaml

# 5. Provider validates and acknowledges lease
# 6. Tenant can now retrieve connection info
```

### JavaScript Example (Browser)

```js
const manifest = new TextEncoder().encode(deploymentYaml);
const hashBuffer = await crypto.subtle.digest('SHA-256', manifest);
const metaHash = Array.from(new Uint8Array(hashBuffer))
  .map(b => b.toString(16).padStart(2, '0'))
  .join('');

// After creating lease with metaHash on-chain...
const message = `manifest lease data ${leaseUuid} ${metaHash} ${Math.floor(Date.now() / 1000)}`;

const signature = await window.keplr.signArbitrary("manifest-1", tenantAddress, message);

const authToken = btoa(JSON.stringify({
  tenant: tenantAddress,
  lease_uuid: leaseUuid,
  meta_hash: metaHash,
  timestamp: Math.floor(Date.now() / 1000),
  pub_key: signature.pub_key,
  signature: signature.signature
}));

await fetch(`${providerApiUrl}/v1/leases/${leaseUuid}/data`, {
  method: 'POST',
  headers: {
    'Authorization': `Bearer ${authToken}`,
    'Content-Type': 'application/octet-stream'
  },
  body: manifest
});
```

## Deployment Data Update - Optional

The create-time handshake above commits the tenant's *first* manifest on-chain.
Updating a running deployment goes through a second handshake so that every
manifest version — not just the first — carries its own verifiable on-chain
commitment.

The problem it solves: on an already-ACTIVE lease the on-chain hash and the
off-chain payload cannot be changed atomically. If the chain moved to the new
hash immediately and the upload then failed, the provider would still hold the
*old* payload while the chain named the *new* hash, and a reprovision would
reject the only payload the provider has. So the committed `meta_hash` advances
only at the very end, after the provider has the new payload and has applied it.

### Chain fields

| Field | Meaning |
|-------|---------|
| `lease.meta_hash` | the committed manifest — what a reprovision compares against |
| `lease.pending_meta_hash` | the tenant's requested next manifest, not yet applied |
| `lease.pending_meta_hash_at` | when it was requested (never expired by the chain) |
| `lease.meta_hash_revision` | monotonic counter; `0` is the creation-time hash |

### Messages

| Message | Signer | Effect |
|---------|--------|--------|
| `MsgUpdateLease{sender, lease_uuid, meta_hash}` | tenant / authority / `allowed_list` | sets `pending_meta_hash`; supersedes an unacknowledged request |
| `MsgAcknowledgeLeaseUpdate{sender, lease_uuid, meta_hash}` | provider / authority | `meta_hash = pending_meta_hash`, clears pending, `meta_hash_revision++` |
| `MsgRejectLeaseUpdate{sender, lease_uuid, meta_hash, reason}` | provider / authority | clears pending, leaves `meta_hash` untouched |
| `MsgCancelLeaseUpdate{sender, lease_uuid}` | tenant / authority / `allowed_list` | clears pending |

All four require the lease to be **ACTIVE**. A PENDING lease still has its
create-time handshake, so changing the hash there would race the provider's
`MsgAcknowledgeLease`; a tenant who committed a wrong hash before
acknowledgement uses `MsgCancelLease` and creates a new lease.

`meta_hash` is required on acknowledge and reject and must equal the lease's
current `pending_meta_hash`. That guard is what stops a provider from committing
a request it never evaluated — one the tenant submitted after the provider read
the lease. It is deliberately absent on cancel: the tenant authored the request
and always means the one currently pending.

Note that `allowed_list` grants the tenant-side verbs only. Standing in for a
provider is a separate power and this feature does not widen it.

### Workflow

```
1. Tenant prepares the new manifest and computes new_hash = SHA-256(manifest)
2. Tenant submits MsgUpdateLease{lease_uuid, new_hash}
     → lease.pending_meta_hash = new_hash; lease.meta_hash UNCHANGED
3. Tenant POSTs the manifest to the provider (auth below)
4. Provider validates SHA-256(received) == lease.pending_meta_hash
5. Provider persists the payload and applies the new deployment
6. Provider submits MsgAcknowledgeLeaseUpdate{lease_uuid, new_hash}
     → lease.meta_hash = new_hash; revision++
7. Reprovision now verifies SHA-256(stored) == lease.meta_hash
```

Steps 2-5 are the window. Throughout it, `lease.meta_hash` still names the
payload the provider is already serving, so a reboot mid-update reprovisions the
previous deployment successfully rather than failing closed.

### Which field to validate against

| Situation | Validate the payload against |
|-----------|------------------------------|
| create-time upload (`POST .../data`, lease PENDING) | `lease.meta_hash` |
| update upload (`POST .../update`, lease ACTIVE) | `lease.pending_meta_hash` |
| reprovision / reboot | `lease.meta_hash` |

| Chain state | Update upload should |
|-------------|----------------------|
| `pending_meta_hash` empty | reject — no update was requested on-chain (`409`) |
| `pending_meta_hash` set, matches the body's hash | accept |
| `pending_meta_hash` set, does not match | reject (`409`) — the tenant superseded the request, or the body is wrong |

### Message Format for Signing

The update upload uses a distinct signing string. Reusing
`manifest lease data ...` would be ambiguous now that a lease can have two
hashes, and binding the token to the revision stops a token signed for revision
N being replayed into a later window:

```
manifest lease update {lease_uuid} {pending_meta_hash_hex} {meta_hash_revision} {unix_timestamp}
```

The bearer token is otherwise identical to the create-time one, with
`meta_hash` carrying the *pending* hash and an added `meta_hash_revision`.

### API Endpoint

```
POST {provider.api_url}/v1/leases/{lease_uuid}/update
Authorization: Bearer <base64_encoded_auth_token>
Content-Type: application/octet-stream

<raw payload bytes>
```

### Provider Obligations

- **Discovery**: poll `Query/PendingLeaseUpdates{provider_uuid}` to pick up
  requests missed during downtime, rather than scanning every lease you own.
- **Ordering**: apply, *then* acknowledge — never the reverse. Acknowledging
  first would move the committed hash to a manifest that is not actually
  running, and a reprovision in that window would fail.
- **Payload storage**: content-address the store so the previously committed
  payload stays servable through the window. Prune it only once the
  acknowledgement has landed on-chain.
- **Retries**: if the acknowledgement tx fails with `ErrLeaseUpdateMismatch`,
  the tenant superseded the request — abandon this attempt and process the new
  one. If it fails with `ErrLeaseNotActive`, the lease closed; stop.
- **Refusal**: use `MsgRejectLeaseUpdate` with a reason for anything you will
  not apply. Silently never acknowledging leaves the tenant unable to tell
  refusal from delay.
- **Staleness**: the chain never expires a pending request, because an
  unacknowledged request is inert and sweeping for them would add per-block work.
  Providers own that judgement: define a maximum age over
  `pending_meta_hash_at` beyond which you reject rather than apply a request the
  tenant has likely abandoned.
- **Rate limiting**: on-chain state cannot grow from repeated requests — each one
  overwrites the last, and re-requesting an identical hash is a no-op — so the
  chain imposes no cooldown beyond the gas each transaction costs. The churn
  lands on you instead: a tenant can supersede a request while you are mid-apply,
  and each supersession invalidates the work in flight. Rate-limit re-applies per
  lease, and treat `ErrLeaseUpdateMismatch` as the signal to abandon the current
  attempt rather than to retry it.

### Reprovision

```
payload = store.get(lease_uuid)
if SHA-256(payload) == lease.meta_hash:
    provision(payload)                 # normal path
else:
    # Payload predates the on-chain update handshake, or the store is corrupt.
    # Fall back to the local integrity reference if one was recorded, and
    # surface the mismatch either way — it is not a normal condition.
    fail_or_legacy_fallback()
```

### Trust Properties

Be precise about what this does and does not prove:

- It **does** give every manifest version a tamper-evident on-chain
  commitment, and it **does** make an acknowledgement a non-repudiable record
  that the provider accepted version N.
- It does **not** prove the provider actually runs what it acknowledged — the
  chain records the provider's claim, exactly as `MsgAcknowledgeLease` does at
  lease creation.
- A provider can also stall indefinitely. The tenant sees this as an ageing
  `pending_meta_hash_at` and can cancel, or close the lease.

## Related Documentation

- [Billing README](../README.md) - Module overview
- [API Reference](API.md) - CLI and gRPC/REST documentation
- [Provider Setup Guide](../../sku/docs/PROVIDER_GUIDE.md) - Creating providers
