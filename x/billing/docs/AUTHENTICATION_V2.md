# Provider authentication v2 proposal

Status: proposed protocol with executable serialization/context-validation examples. Provider and client adoption remains open in [ENG-925](https://linear.app/liftedinit/issue/ENG-925). This repository implements on-chain billing, not the off-chain API or wallet client. These examples do not establish that any deployed provider accepts v2.

The [legacy v1 profile](INTEGRATION.md#authentication) signs a lease identifier and timestamp, plus a hash for upload. It has no signed audience. V2 places the expected chain, provider, API base URL and operation inside the signed data. ADR-036's sign document continues to use an empty `chain_id`; do not change that document to add network binding. Use an established ADR-036 verifier such as `verifyADR36Amino` from `@keplr-wallet/cosmos`, as recommended by the [Keplr signing documentation](https://docs.keplr.app/api/guide/sign-arbitrary).

## Signed bytes and token

The message is the UTF-8 encoding of this JSON array, in exactly this order, with no whitespace or trailing newline:

```json
["manifest-provider-auth",2,"manifest-1","01902a9b-1234-7000-8000-000000000002","https://provider.example/api","lease.connection.read","01902a9b-1234-7000-8000-000000000001","manifest1tenant",1700000000,1700000300,""]
```

The fields are protocol tag, version, chain ID, provider UUID, API audience, operation, lease UUID, tenant address, issued-at seconds, expiry seconds, and lowercase hexadecimal SHA-256 payload hash. The sample tenant is a placeholder, not a valid address. The operation is exactly `lease.connection.read` (GET connection, empty hash) or `lease.data.upload` (POST data, 64 lowercase hexadecimal hash characters). Timestamps are nonnegative safe integers encoded as decimal JSON numbers. UUIDs and tenant addresses use canonical lowercase spellings. The tenant must pass the chain's actual Bech32 address validation and derive from the signing public key; a regular expression alone is not address validation.

Array order removes object-key serialization ambiguity. For this profile, chain IDs use ASCII letters, digits, `.`, `_` and `-`, with at most 128 characters; other fields use the restricted ASCII formats checked below. Serialization must match ECMAScript `JSON.stringify`: do not escape printable ASCII except for JSON-required quote/backslash escaping, do not escape `/` or HTML characters such as `&`, and do not use exponent notation for timestamps. For example, an audience path `/api/a&b` appears literally as `"https://provider.example/api/a&b"`, never with `\u0026`. In Go, use `json.Encoder.SetEscapeHTML(false)` and remove the encoder's trailing newline. Implementations in other languages must match the fixed bytes above. The base64 JSON bearer envelope carries these fields by name plus `pub_key` and `signature`; its object-key order is immaterial because the verifier reconstructs the array.

The audience is the canonical HTTPS **API base URL, including its base path**, derived from the provider record and pinned in verifier configuration. Normalize it using the WHATWG URL serialization, remove trailing path slashes, and reject credentials, query strings and fragments. V2 token audiences must already equal that canonical spelling. Different paths at the same origin are different audiences. The client must obtain the provider UUID from the lease and the URL from that provider's on-chain record. The verifier must use its configured chain connection, provider UUID and canonical audience; never take those expected values from the token or an untrusted forwarded-host header. Configure a new audience deliberately when the public API URL changes.

Independent deployments must use distinct chain IDs and/or provider audiences. Cloning the chain ID, provider UUID and public audience together clones the identity this profile binds; v2 does not distinguish deployments that deliberately reuse all those identifiers.

## Shared serialization and context checks

This reference code deliberately delegates cryptography and chain authorization to the provider. It only constructs signed bytes and rejects invalid or mismatched token context. Applications should package one shared implementation for clients and verifiers rather than copy it into each call site.

```js
function canonicalAudience(value) {
  if (typeof value !== "string") throw new Error("invalid audience");
  const url = new URL(value);
  if (url.protocol !== "https:" || url.username || url.password || url.search || url.hash) {
    throw new Error("audience must be an HTTPS API base URL");
  }
  return (url.origin + url.pathname).replace(/\/+$/, "");
}

function providerAuthMessage(token) {
  const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
  const matches = (pattern, value) => typeof value === "string" && pattern.test(value);
  if (!token || token.version !== 2 ||
      !matches(/^[A-Za-z0-9._-]{1,128}$/, token.chain_id) ||
      !matches(uuid, token.provider_uuid) || !matches(uuid, token.lease_uuid) ||
      !matches(/^[a-z0-9]{1,128}$/, token.tenant) ||
      token.audience !== canonicalAudience(token.audience) ||
      !Number.isSafeInteger(token.issued_at) || token.issued_at < 0 ||
      !Number.isSafeInteger(token.expires_at) || token.expires_at <= token.issued_at ||
      token.expires_at - token.issued_at > 300) {
    throw new Error("invalid v2 authentication fields");
  }
  if (token.operation === "lease.connection.read") {
    if (token.meta_hash !== "") throw new Error("connection tokens have no payload hash");
  } else if (token.operation === "lease.data.upload") {
    if (!matches(/^[0-9a-f]{64}$/, token.meta_hash)) throw new Error("invalid upload hash");
  } else {
    throw new Error("unsupported authentication operation");
  }
  return JSON.stringify([
    "manifest-provider-auth", 2, token.chain_id, token.provider_uuid,
    token.audience, token.operation, token.lease_uuid, token.tenant,
    token.issued_at, token.expires_at, token.meta_hash,
  ]);
}

function validateProviderAuthContext(token, expected, now) {
  const message = providerAuthMessage(token);
  for (const field of ["chain_id", "provider_uuid", "audience", "operation", "lease_uuid", "tenant", "meta_hash"]) {
    if (token[field] !== expected[field]) throw new Error(`authentication ${field} mismatch`);
  }
  if (!Number.isSafeInteger(now) || now < 0 || token.issued_at > now + 30 || now >= token.expires_at) {
    throw new Error("authentication token is not current");
  }
  return message; // Still requires signature verification and chain authorization.
}
```

## Client flow

1. Query the selected chain for the lease and provider. Verify the lease belongs to the connected tenant. Require explicitly configured v2 support for that provider.
2. Set `version: 2`, the actual chain ID, provider/lease UUIDs, canonical audience, tenant, operation and hash. Capture `issued_at` once and set `expires_at` no more than 300 seconds later.
3. Call `providerAuthMessage(token)` and pass the result to `keplr.signArbitrary(chainId, tenant, message)`. The wallet chain argument selects the signing account; the array provides signed network binding.
4. Keep the same fields after wallet confirmation, add the returned `pub_key` and `signature`, and base64-encode the JSON envelope. If signing outlasts expiry, request a fresh signature. Never edit only the timestamps after signing.
5. Send it as a bearer token to the exact audience's `/v1/leases/{lease_uuid}/connection` or `/data` path. Do not forward bearer tokens across redirects. Upload the exact bytes whose hash is signed and recorded on-chain.

## Provider flow

1. Bound the authorization header/body size, strictly decode the base64 JSON envelope, and reject missing, duplicate or wrong-type fields and unsupported versions. Select the v2 verifier explicitly; a failed v2 verification must never be retried as v1.
2. Select the chain, provider and canonical API audience from trusted configuration. Select the operation and lease UUID from the matched HTTP method/route, not from token claims. Use that chain connection to fetch the lease; require its provider UUID to match this provider and its tenant to match the valid signing address.
3. Require ACTIVE for connection retrieval and PENDING for data upload, enforcing the current pending timeout for uploads. For upload, take the expected hash from the lease's SHA-256 `meta_hash`; reject a different hash or unsupported hash length. Connection retrieval expects an empty hash.
4. Construct `expected` from these trusted values, with the chain lease's tenant. Call `validateProviderAuthContext(token, expected, currentUnixSeconds)`. Its maximum lifetime is 300 seconds, expiry is exclusive, and issued-at may be at most 30 seconds in the future for clock skew.
5. Verify the returned message with an ADR-036 library, the token public key/signature and the expected tenant. Require the expected secp256k1 key type, address prefix and public-key-derived address; reject all decode and verification errors. Do not implement ad hoc ECDSA or Amino JSON canonicalization.
6. For uploads, hash the bounded received body and compare it to the signed/on-chain hash before accepting it. Apply provider authorization and idempotency checks before returning credentials or provisioning.

Tokens remain reusable bearer credentials within their lifetime **inside the same audience and operation**. V2 does not promise one-time use. Upload handling must be idempotent; require a separately designed nonce/challenge protocol for operations that need single-use authorization. Do not log tokens, signatures, uploaded secrets or connection credentials.

## Coordinated rollout and acceptance

Deploy provider verification and client signing together behind explicit per-provider version configuration. During a limited transition, any v1 acceptance is a documented exception with an end date and remains exposed to cross-audience replay. Once a provider requires v2, reject absent versions and v1 tokens. Authentication failure, network failure and an unknown version must never trigger an automatic downgrade. A ledger release does not advertise v2 support on behalf of an off-chain provider.

The executable examples in `scripts/provider_auth_examples.test.mjs` pin canonical message bytes and reject altered chain, provider, API path, operation, tenant, lease, hash, expiry and version contexts. They are protocol vectors, not real-wallet signature tests. Before enabling v2, each provider/client implementation must also test actual Keplr-compatible signatures with its selected ADR-036 library, reject tampering with every signed field, validate addresses, and exercise both operations over HTTP against the intended chain. Record deployed versions and removal of v1 acceptance in ENG-925.
