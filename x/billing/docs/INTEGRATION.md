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
manifest lease access 550e8400-e29b-41d4-a716-446655440000 1702834946
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
  "lease_uuid": "550e8400-e29b-41d4-a716-446655440000",
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

## Related Documentation

- [Billing README](../README.md) - Module overview
- [API Reference](API.md) - CLI and gRPC/REST documentation
- [Provider Setup Guide](../../sku/docs/PROVIDER_GUIDE.md) - Creating providers
