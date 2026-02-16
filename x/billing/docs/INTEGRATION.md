# Billing Module Integration Guide

This guide covers how tenants authenticate to provider off-chain APIs after lease creation.

## Provider Off-Chain API Integration

Providers expose a REST API for tenants to retrieve connection details after lease acknowledgement. The API endpoint URL is stored on-chain in the `Provider.api_url` field.

### Tenant Flow

1. Tenant queries lease to get `provider_uuid`
2. Tenant queries provider to get `api_url`
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

### On-Chain Storage

Only the hash is stored on-chain:
- **Field**: `lease.meta_hash`
- **Max size**: 64 bytes (accommodates SHA-256 and SHA-512)
- **Format**: Raw bytes
- **Immutable**: Set once at creation, cannot be updated

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

## Related Documentation

- [Billing README](../README.md) - Module overview
- [API Reference](API.md) - CLI and gRPC/REST documentation
- [Provider Setup Guide](../../sku/docs/PROVIDER_GUIDE.md) - Creating providers
