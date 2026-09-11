# SKU Module API Reference

This document provides a comprehensive API reference for the SKU module, covering both CLI commands and gRPC/REST endpoints.

## Table of Contents

- [CLI Commands](#cli-commands)
  - [Transaction Commands](#transaction-commands)
  - [Query Commands](#query-commands)
- [gRPC API](#grpc-api)
  - [Msg Service](#msg-service)
  - [Query Service](#query-service)
- [REST API](#rest-api)
- [Data Types](#data-types)
- [Events](#events)
- [Error Codes](#error-codes)
- [Authorization](#authorization)

---

## CLI Commands

### Transaction Commands

#### create-provider

Create a new provider with management and payout addresses.

```bash
manifestd tx sku create-provider [address] [payout-address] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| address | string | Bech32 address of the provider (management address) |
| payout-address | string | Bech32 payout address permitted by bank policy; protected module accounts are rejected |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --api-url | string | HTTPS endpoint for provider's off-chain API (optional) |
| --meta-hash | string | Hex-encoded hash of off-chain metadata (optional) |

New API URLs require HTTPS, a nonempty hostname, no credentials, and an explicit
port between 1 and 65535 when supplied. IPv6 literals must be bracketed. An empty
explicit port is rejected.

**Example:**
```bash
manifestd tx sku create-provider manifest1provider... manifest1payout... \
  --api-url https://api.provider.com \
  --meta-hash deadbeef \
  --from authority
```

---

#### update-provider

Update an existing provider. Resend the current `--meta-hash` as hex to preserve
it; omitting the flag or passing an empty value clears it. Query responses
encode nonempty hashes as base64, so decode and convert before resubmitting.
A payout change redirects existing unsettled accrual and future charges;
withdraw first if the old recipient should receive existing earnings. A blocked
payout must be repaired first, then withdrawn to the new recipient.

```bash
manifestd tx sku update-provider [uuid] [address] [payout-address] [active] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| uuid | string | Canonical lowercase UUIDv7 of the provider |
| address | string | New management address |
| payout-address | string | New payout address permitted by bank policy; may repair a previously blocked payout |
| active | bool | Whether the provider is active (true/false) |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --api-url | string | HTTPS endpoint for provider's off-chain API (optional) |
| --clear-api-url | bool | Clear the stored API URL; cannot be combined with a non-empty `--api-url` |
| --meta-hash | string | Hex-encoded metadata hash; omitted or empty clears it. Resend the current hex value to preserve it. |

**Example:**
```bash
manifestd tx sku update-provider 01912345-6789-7abc-8def-0123456789ab manifest1provider... manifest1payout... true \
  --api-url https://api.provider.com \
  --meta-hash cafebabe \
  --from authority

# Clear the stored API URL while updating the other required fields:
manifestd tx sku update-provider 01912345-6789-7abc-8def-0123456789ab manifest1provider... manifest1payout... true \
  --clear-api-url \
  --meta-hash [current-meta-hash-hex] \
  --from authority
```

**Note:** If `--api-url` is omitted (empty string), the existing API URL is
preserved. Use `--clear-api-url` to remove it. Supplying both
`--clear-api-url` and a non-empty `--api-url` is rejected.

---

#### deactivate-provider

Deactivate a provider (soft delete). The provider remains in state but is marked inactive. Inactive providers cannot have new SKUs created for them.

SKU deactivation is paginated to prevent gas exhaustion with many SKUs. Each
committed call deactivates up to the requested limit. The CLI prints an SDK
transaction response, so its output does not expose the module
`MsgDeactivateProviderResponse.has_more` field. After successful execution,
query `skus-by-provider UUID --active-only --limit 1` at that transaction height.
Repeat deactivation while the query returns a SKU; an empty result means the
cascade is complete. The provider itself becomes inactive on the first call.

```bash
manifestd tx sku deactivate-provider [uuid] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| uuid | string | Canonical lowercase UUIDv7 of the provider to deactivate |

**Flags:**
| Flag | Type | Default | Description |
|------|------|---------|-------------|
| --limit | uint64 | 50 | Maximum SKUs to deactivate per call (max 100) |

**Example:**
```bash
manifestd tx sku deactivate-provider 01912345-6789-7abc-8def-0123456789ab --from authority
manifestd tx sku deactivate-provider 01912345-6789-7abc-8def-0123456789ab --limit 100 --from authority
```


##### Complete a provider deactivation cascade

This Bash + jq example handles more than one page and waits for committed
success before querying the remaining active SKUs. Configure the chain, RPC,
provider, and authority first. Use one worker per state directory; keep the
same configuration when resuming. A timeout retains the transaction for lookup
on restart. An ambiguous broadcast must be investigated before replacing
`pending.json`; for a confirmed nonzero execution code, correct the cause and
archive the pending and included receipts before submitting a replacement.
The state query is pinned to the committed transaction height. If that height
has been pruned before a restart, use an archive RPC for the same chain to
resolve the retained receipt and query; do not submit another transaction just
because historical state is unavailable. Update the saved RPC entry in the
plan only after verifying the replacement endpoint serves that chain.

```bash
#!/usr/bin/env bash
set -euo pipefail
PROVIDER_UUID="01912345-6789-7abc-8def-0123456789ab"
CHAIN_ID="replace-with-chain-id"
NODE="https://replace-with-rpc"
AUTHORITY_KEY="authority"
STATE_DIR="./deactivate-state-$PROVIDER_UUID"
mkdir -p "$STATE_DIR"
jq -n --args '$ARGS.positional' "$PROVIDER_UUID" "$CHAIN_ID" "$NODE" "$AUTHORITY_KEY" \
  > "$STATE_DIR/plan.expected.json"
if [ -f "$STATE_DIR/plan.json" ]; then
  cmp "$STATE_DIR/plan.expected.json" "$STATE_DIR/plan.json"
else
  mv "$STATE_DIR/plan.expected.json" "$STATE_DIR/plan.json"
fi
while true; do
  if [ ! -f "$STATE_DIR/pending.json" ]; then
    manifestd tx sku deactivate-provider "$PROVIDER_UUID" --limit 50 \
      --from "$AUTHORITY_KEY" --chain-id "$CHAIN_ID" --node "$NODE" \
      --broadcast-mode sync --output json -y --gas auto --gas-adjustment 1.5 \
      > "$STATE_DIR/pending.json"
  fi
  jq -e '(.code | tonumber) == 0 and (.txhash | test("^[[:xdigit:]]{64}$"))' \
    "$STATE_DIR/pending.json" > /dev/null
  TXHASH=$(jq -r '.txhash' "$STATE_DIR/pending.json")
  INCLUDED=false
  for ((attempt = 0; attempt < 60; attempt++)); do
    if manifestd query tx "$TXHASH" --node "$NODE" --output json \
      > "$STATE_DIR/included.json" 2> "$STATE_DIR/query-error.txt"; then
      INCLUDED=true
      break
    fi
    sleep 2
  done
  if [ "$INCLUDED" != true ]; then
    echo "Transaction $TXHASH not yet queryable; retain pending.json and resume." >&2
    exit 1
  fi
  jq -e --arg hash "$TXHASH" \
    '(.code | tonumber) == 0 and (.height | tonumber) > 0 and .txhash == $hash' \
    "$STATE_DIR/included.json" > /dev/null
  cp "$STATE_DIR/included.json" "$STATE_DIR/$TXHASH.json"
  HEIGHT=$(jq -r '.height' "$STATE_DIR/included.json")
  manifestd query sku skus-by-provider "$PROVIDER_UUID" --active-only --limit 1 \
    --height "$HEIGHT" --node "$NODE" --output json > "$STATE_DIR/active.json"
  jq -e '.skus | type == "array"' "$STATE_DIR/active.json" > /dev/null
  if [ "$(jq '.skus | length' "$STATE_DIR/active.json")" -eq 0 ]; then
    # A retained receipt proves the earlier cascade, not the provider's state now.
    manifestd status --node "$NODE" --output json > "$STATE_DIR/status.json"
    CURRENT_HEIGHT=$(jq -er --arg chain "$CHAIN_ID" \
      'select(.node_info.network == $chain and .sync_info.catching_up == false) |
       .sync_info.latest_block_height | select(test("^[1-9][0-9]*$"))' "$STATE_DIR/status.json")
    manifestd query sku provider "$PROVIDER_UUID" --height "$CURRENT_HEIGHT" \
      --node "$NODE" --output json > "$STATE_DIR/current-provider.json"
    manifestd query sku skus-by-provider "$PROVIDER_UUID" --active-only --limit 1 \
      --height "$CURRENT_HEIGHT" --node "$NODE" --output json > "$STATE_DIR/current-active.json"
    if ! jq -e '.provider.active == false' "$STATE_DIR/current-provider.json" > /dev/null ||
       ! jq -e '(.skus | type == "array") and (.skus | length == 0)' "$STATE_DIR/current-active.json" > /dev/null; then
      echo "The provider changed after this checkpoint. Use a new state directory for a new cascade." >&2
      exit 1
    fi
    break
  fi
  rm "$STATE_DIR/pending.json"
done
echo "Provider deactivation complete; no active SKUs remain."
```

The final pending receipt remains as a completion checkpoint: restarting the
same run verifies its historical result and checks the provider and active SKUs
at one current committed height without sending another transaction. A changed
provider fails the restart; use a new state directory for a later deactivation
after reactivation. Completion describes the checked height; later authorized
updates can reactivate the provider.

---

#### create-sku

Create a new SKU for an active provider.

```bash
manifestd tx sku create-sku [provider-uuid] [name] [unit] [base-price] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| provider-uuid | string | Canonical lowercase UUIDv7 of the provider this SKU belongs to |
| name | string | Human-readable name for the SKU |
| unit | int | Billing unit: 1 = per hour, 2 = per day |
| base-price | coin | Base price (e.g., `3600upwr` for 1/second rate per hour) |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --meta-hash | string | Hex-encoded hash of off-chain metadata (optional) |

**Example:**
```bash
manifestd tx sku create-sku 01912345-6789-7abc-8def-0123456789ab "Compute Instance Small" 1 3600000upwr \
  --meta-hash deadbeef \
  --from authority
```

**Price Validation:**

The base price must be exactly divisible by the billing unit's seconds. See [Pricing and Exact Divisibility](../README.md#pricing-and-exact-divisibility) for requirements and examples.

**Error Messages** (produced under `ErrInvalidSKU`, wrapped as `invalid create sku message: invalid price/unit combination: ...`; the unit renders as its enum name, e.g. `UNIT_PER_HOUR`):
- If price is not evenly divisible: `base price <price> is not evenly divisible by <UNIT_NAME> (remainder: <r>); price must be exactly divisible to avoid rounding errors`
- If price results in a zero per-second rate: `base price <price> with unit <UNIT_NAME> results in zero per-second rate; increase price or change unit`

---

#### update-sku

Update an existing SKU.

```bash
manifestd tx sku update-sku [uuid] [provider-uuid] [name] [unit] [base-price] [active] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| uuid | string | Canonical lowercase UUIDv7 of the SKU |
| provider-uuid | string | Canonical lowercase UUIDv7 of the provider |
| name | string | SKU name |
| unit | int | Billing unit: 1 = per hour, 2 = per day |
| base-price | coin | Base price |
| active | bool | Whether the SKU is active (true/false) |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --meta-hash | string | Hex-encoded metadata hash; omitted or empty clears it. Resend the current hex value to preserve it. |

**Example:**
```bash
manifestd tx sku update-sku 01912345-6789-7abc-8def-0123456789ab 01912345-6789-7abc-8def-0123456789ab "Compute Instance Medium" 1 7200000upwr true \
  --meta-hash cafebabe \
  --from authority
```

---

#### deactivate-sku

Deactivate a SKU (soft delete). The SKU remains in state but is marked inactive. Inactive SKUs cannot be used for new leases but existing leases continue with their locked prices.

```bash
manifestd tx sku deactivate-sku [uuid] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| uuid | string | Canonical lowercase UUIDv7 of the SKU to deactivate |

**Example:**
```bash
manifestd tx sku deactivate-sku 01912345-6789-7abc-8def-0123456789ab --from authority
```

---

#### update-params

Update the module parameters (authority only).

```bash
manifestd tx sku update-params [flags]
```

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --allowed-list | string | Required: replacement list of addresses allowed to manage SKUs and providers. Pass an explicit empty value to clear; omission is rejected. |

**Example:**
```bash
# Replace the complete allowed list (include every address to retain)
manifestd tx sku update-params \
  --allowed-list "manifest1abc...,manifest1def..." \
  --from authority

# Clear the allowed list
manifestd tx sku update-params \
  --allowed-list "" \
  --from authority
```

---

### Query Commands

**Query cursor contract:** `pagination.next_key` is opaque `bytes`. JSON and CLI
output encode it as base64; pass that string verbatim to `--page-key`. Do not
decode it in the shell or treat it as a UUID. Programmatic gRPC clients pass the
decoded bytes in `PageRequest.key`. The cursor identifies the first unread row,
and the next scan resumes inclusively at that key. `--reverse` may be combined
with `--page-key` and resumes in the same direction.

SKU list queries support the standard SDK `--offset`, `--page`, and
`--count-total` compatibility modes. Unfiltered compatibility requests may
inspect at most 20,000 physical rows. Value-filtered requests retain a 1000-row
ceiling in every mode; currently this applies to `ProviderByAddress
--active-only`. A request that cannot return an exact page or total within its
ceiling fails with gRPC `ResourceExhausted` rather than returning a partial
result. Cursor pagination remains the efficient, unbounded-history path. A
request that combines a page key with a nonzero offset fails with gRPC
`InvalidArgument`; as in the SDK, `count_total` is ignored when a page key is
present. An omitted or zero limit defaults to 100 without implicitly enabling
`count_total`; request the total explicitly when needed. Cursor resume on
index-backed paths is deletion-tolerant: if the cursor row is removed between
calls, the query starts at the nearest surviving row in the requested
direction.

The SDK default page size is 100, oversized `limit` values are clamped to 1000,
and value-filtered cursor pages inspect at most 1000 physical rows. A sparse
filter can therefore return a short or empty page with a non-empty `next_key`;
bulk indexers must continue until that cursor is empty.

`skus-by-provider` forwards `provider-uuid` to the service without local UUID
validation. It therefore surfaces the service's field-specific gRPC
`InvalidArgument`: an empty value reports `provider_uuid cannot be empty`, and
a non-empty malformed, uppercase, or non-v7 value reports
`provider_uuid must be a valid UUIDv7`. An unknown canonical lowercase UUIDv7
returns an empty page.

The direct `provider` and `sku` commands also forward their lookup keys without
local UUID validation. Their services reject an empty `uuid` with
`InvalidArgument`; every non-empty key is looked up as supplied, so malformed,
uppercase, non-v7, and unknown canonical values return `NotFound`.

#### params

Query module parameters.

```bash
manifestd query sku params
```

**Response:**
```json
{
  "params": {
    "allowed_list": ["manifest1abc..."]
  }
}
```

---

#### provider

Query a provider by UUID.

```bash
manifestd query sku provider [uuid]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| uuid | string | Canonical lowercase UUIDv7 of the provider |

**Response:**
```json
{
  "provider": {
    "uuid": "01912345-6789-7abc-8def-0123456789ab",
    "address": "manifest1provider...",
    "payout_address": "manifest1payout...",
    "api_url": "https://api.provider.com",
    "meta_hash": null,
    "active": true
  }
}
```

---

#### provider-by-address

Query all providers with a given management address.

```bash
manifestd query sku provider-by-address [address] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| address | string | Provider's management address (Bech32) |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --active-only | bool | Filter to return only active providers |
| --limit | uint64 | Pagination limit |
| --page-key | string | Base64 `pagination.next_key` from the previous response |
| --count-total | bool | Return the exact total when it can be computed within the applicable scan ceiling |

**Example:**
```bash
manifestd query sku provider-by-address manifest1abc... --active-only --limit 10 --count-total
```

**Response:**
```json
{
  "providers": [
    {
      "uuid": "01912345-6789-7abc-8def-0123456789ab",
      "address": "manifest1provider...",
      "payout_address": "manifest1payout...",
      "api_url": "https://api.provider.com",
      "meta_hash": null,
      "active": true
    }
  ],
  "pagination": {
    "next_key": null,
    "total": "1"
  }
}
```

**Note:** A single address can manage multiple providers. This query returns all providers associated with the given address, using an efficient address index.

---

#### providers

Query all providers with pagination.

```bash
manifestd query sku providers [flags]
```

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --active-only | bool | Filter to return only active providers |
| --limit | uint64 | Pagination limit |
| --page-key | string | Base64 `pagination.next_key` from the previous response |
| --count-total | bool | Return the exact total when it can be computed within the applicable scan ceiling |

**Example:**
```bash
manifestd query sku providers --active-only --limit 10 --count-total
```

**Response:**
```json
{
  "providers": [
    {
      "uuid": "01912345-6789-7abc-8def-0123456789ab",
      "address": "manifest1provider...",
      "payout_address": "manifest1payout...",
      "api_url": "https://api.provider.com",
      "meta_hash": null,
      "active": true
    }
  ],
  "pagination": {
    "next_key": null,
    "total": "1"
  }
}
```

---

#### sku

Query a SKU by UUID.

```bash
manifestd query sku sku [uuid]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| uuid | string | Canonical lowercase UUIDv7 of the SKU |

**Response:**
```json
{
  "sku": {
    "uuid": "01912345-6789-7abc-8def-0123456789cd",
    "provider_uuid": "01912345-6789-7abc-8def-0123456789ab",
    "name": "Compute Instance Small",
    "unit": "UNIT_PER_HOUR",
    "base_price": {
      "denom": "upwr",
      "amount": "3600000"
    },
    "meta_hash": null,
    "active": true
  }
}
```

---

#### skus

Query all SKUs with pagination.

```bash
manifestd query sku skus [flags]
```

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --active-only | bool | Filter to return only active SKUs |
| --limit | uint64 | Pagination limit |
| --page-key | string | Base64 `pagination.next_key` from the previous response |

**Example:**
```bash
manifestd query sku skus --active-only --limit 10
```

---

#### skus-by-provider

Query all SKUs for a specific provider.

```bash
manifestd query sku skus-by-provider [provider-uuid] [flags]
```

**Arguments:**
| Argument | Type | Description |
|----------|------|-------------|
| provider-uuid | string | Canonical lowercase UUIDv7 of the provider |

**Flags:**
| Flag | Type | Description |
|------|------|-------------|
| --active-only | bool | Filter to return only active SKUs |
| --limit | uint64 | Pagination limit |
| --page-key | string | Base64 `pagination.next_key` from the previous response |

**Example:**
```bash
manifestd query sku skus-by-provider 01912345-6789-7abc-8def-0123456789ab --active-only
```

---

## gRPC API

The generated embedded descriptors omit protobuf `SourceCodeInfo`, so runtime
reflection exposes the schema but not source comments. Use this API reference
for the documented validation and error semantics.

### Msg Service

The Msg service handles all state-changing operations.

**Service Definition:**
```protobuf
service Msg {
  rpc CreateProvider(MsgCreateProvider) returns (MsgCreateProviderResponse);
  rpc UpdateProvider(MsgUpdateProvider) returns (MsgUpdateProviderResponse);
  rpc DeactivateProvider(MsgDeactivateProvider) returns (MsgDeactivateProviderResponse);
  rpc CreateSKU(MsgCreateSKU) returns (MsgCreateSKUResponse);
  rpc UpdateSKU(MsgUpdateSKU) returns (MsgUpdateSKUResponse);
  rpc DeactivateSKU(MsgDeactivateSKU) returns (MsgDeactivateSKUResponse);
  rpc UpdateParams(MsgUpdateParams) returns (MsgUpdateParamsResponse);
}
```

#### MsgCreateProvider

Create a new provider.

**Request:**
```protobuf
message MsgCreateProvider {
  string authority = 1;       // Authority or allowed address
  string address = 2;         // Provider's management address
  string payout_address = 3;  // Provider's payout address
  bytes meta_hash = 4;        // Off-chain metadata hash
  string api_url = 5;         // HTTPS endpoint for off-chain API
}
```

**Response:**
```protobuf
message MsgCreateProviderResponse {
  string uuid = 1;  // Created provider UUID
}
```

---

#### MsgUpdateProvider

Update an existing provider.

**Request:**
```protobuf
message MsgUpdateProvider {
  string authority = 1;       // Authority or allowed address
  string uuid = 2;            // Canonical lowercase provider UUIDv7
  string address = 3;         // New management address
  string payout_address = 4;  // New payout address
  bytes meta_hash = 5;        // Replacement metadata hash; empty clears
  bool active = 6;            // Active status
  string api_url = 7;         // HTTPS endpoint for off-chain API
  bool clear_api_url = 8;     // Explicitly clear the stored API URL
}
```

**Response:**
```protobuf
message MsgUpdateProviderResponse {}
```

**Notes:**
- Each update replaces `meta_hash`. Resend the current bytes to preserve it;
  an empty or omitted value clears it. JSON encodes nonempty bytes as base64,
  while the CLI's `--meta-hash` flag accepts hex.
- If `api_url` is empty and `clear_api_url` is false, the existing API URL is
  preserved. This retains the behavior of clients built before tag 8 existed.
- If `clear_api_url` is true, the stored API URL is cleared. The request is
  rejected if `api_url` is also non-empty.
- This is a transaction-only wire addition. Provider storage and genesis are
  unchanged, so no module or store migration is required.
- **Reactivation requires a completed cascade:** Setting `active=true` on an inactive provider is rejected while any of its SKUs remain active. Repeat `MsgDeactivateProvider` until `has_more=false`, then reactivate the provider and desired SKUs individually.
- **Deactivation is forbidden:** Setting `active=false` on an active provider will return an error. Use `MsgDeactivateProvider` instead, which properly cascades deactivation to all associated SKUs.
- An already-inactive provider accepts `active=false` for metadata updates.
- Payout addresses must be permitted by bank policy. An existing blocked payout can be repaired by supplying an allowed replacement.
- Historical API URLs remain importable and are preserved when omitted from an update. Newly supplied URLs must pass the current hostname and port checks.

---

#### MsgDeactivateProvider

Deactivate a provider (soft delete). This also deactivates all associated SKUs.

SKU deactivation is paginated to prevent gas exhaustion when a provider has many SKUs.
If `has_more` is true in the response, call again to continue deactivating SKUs.

**Request:**
```protobuf
message MsgDeactivateProvider {
  string authority = 1;  // Authority or allowed address
  string uuid = 2;       // Canonical lowercase provider UUIDv7
  uint64 limit = 3;      // Max SKUs to deactivate (0 = default 50, max 100)
}
```

**Response:**
```protobuf
message MsgDeactivateProviderResponse {
  uint64 deactivated_sku_count = 1;  // Number of SKUs deactivated in this call
  bool has_more = 2;                  // True if more SKUs need deactivation
}
```

---

#### MsgCreateSKU

Create a new SKU.

**Request:**
```protobuf
message MsgCreateSKU {
  string authority = 1;                    // Authority or allowed address
  string provider_uuid = 2;                // Canonical lowercase provider UUIDv7
  string name = 3;                         // SKU name
  Unit unit = 4;                           // Billing unit
  cosmos.base.v1beta1.Coin base_price = 5; // Base price
  bytes meta_hash = 6;                     // Off-chain metadata hash
}
```

**Response:**
```protobuf
message MsgCreateSKUResponse {
  string uuid = 1;  // Created SKU UUID
}
```

---

#### MsgUpdateSKU

Update an existing SKU.

**Request:**
```protobuf
message MsgUpdateSKU {
  string authority = 1;                    // Authority or allowed address
  string uuid = 2;                         // Canonical lowercase SKU UUIDv7
  string provider_uuid = 3;                // Canonical lowercase provider UUIDv7
  string name = 4;                         // SKU name
  Unit unit = 5;                           // Billing unit
  cosmos.base.v1beta1.Coin base_price = 6; // Base price
  bytes meta_hash = 7;                     // Replacement metadata hash; empty clears
  bool active = 8;                         // Active status
}
```

**Response:**
```protobuf
message MsgUpdateSKUResponse {}
```

**Notes:**
- Each update replaces `meta_hash`. Resend the current bytes to preserve it; an empty or omitted value clears it.
- **Reactivation is allowed:** Setting `active=true` on an inactive SKU will reactivate it (requires the provider to be active).
- **Deactivation is forbidden:** Setting `active=false` on an active SKU will return an error. Use `MsgDeactivateSKU` instead.

---

#### MsgDeactivateSKU

Deactivate a SKU (soft delete).

**Request:**
```protobuf
message MsgDeactivateSKU {
  string authority = 1;  // Authority or allowed address
  string uuid = 2;       // Canonical lowercase SKU UUIDv7
}
```

**Response:**
```protobuf
message MsgDeactivateSKUResponse {}
```

---

#### MsgUpdateParams

Update module parameters (authority only).

**Request:**
```protobuf
message MsgUpdateParams {
  string authority = 1;  // Module authority
  Params params = 2;     // New parameters
}
```

**Response:**
```protobuf
message MsgUpdateParamsResponse {}
```

---

### Query Service

The Query service provides read-only access to state.

**Service Definition:**
```protobuf
service Query {
  rpc Params(QueryParamsRequest) returns (QueryParamsResponse);
  rpc Provider(QueryProviderRequest) returns (QueryProviderResponse);
  rpc ProviderByAddress(QueryProviderByAddressRequest) returns (QueryProviderByAddressResponse);
  rpc Providers(QueryProvidersRequest) returns (QueryProvidersResponse);
  rpc SKU(QuerySKURequest) returns (QuerySKUResponse);
  rpc SKUs(QuerySKUsRequest) returns (QuerySKUsResponse);
  rpc SKUsByProvider(QuerySKUsByProviderRequest) returns (QuerySKUsByProviderResponse);
}
```

`Provider.uuid` and `SKU.uuid` preserve direct-lookup behavior. They reject an
empty field with gRPC `InvalidArgument`, then look up every non-empty key as
supplied. A malformed, uppercase, non-v7, or unknown canonical value therefore
returns `NotFound` rather than a UUID-format error. `NotFound` is reserved for
an absent key; an unexpected primary-store or value-decoding failure returns
`Internal` so state corruption is not disguised as a missing resource.

#### QueryParams

Get module parameters.

**Endpoint:** `liftedinit.sku.v1.Query/Params`

**Request:** Empty

**Response:**
```protobuf
message QueryParamsResponse {
  Params params = 1;
}
```

---

#### QueryProvider

Get a provider by UUID.

**Endpoint:** `liftedinit.sku.v1.Query/Provider`

**Request:**
```protobuf
message QueryProviderRequest {
  string uuid = 1;
}
```

**Response:**
```protobuf
message QueryProviderResponse {
  Provider provider = 1;
}
```

---

#### QueryProviderByAddress

Get all providers with a given management address.

**Endpoint:** `liftedinit.sku.v1.Query/ProviderByAddress`

**Request:**
```protobuf
message QueryProviderByAddressRequest {
  string address = 1;  // Provider's management address
  cosmos.base.query.v1beta1.PageRequest pagination = 2;
  bool active_only = 3;
}
```

**Response:**
```protobuf
message QueryProviderByAddressResponse {
  repeated Provider providers = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}
```

**Note:** A single address can manage multiple providers. This query returns all providers associated with the given address, using an efficient address index.

---

#### QueryProviders

List all providers with pagination.

**Endpoint:** `liftedinit.sku.v1.Query/Providers`

**Request:**
```protobuf
message QueryProvidersRequest {
  cosmos.base.query.v1beta1.PageRequest pagination = 1;
  bool active_only = 2;
}
```

**Response:**
```protobuf
message QueryProvidersResponse {
  repeated Provider providers = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}
```

---

#### QuerySKU

Get a SKU by UUID.

**Endpoint:** `liftedinit.sku.v1.Query/SKU`

**Request:**
```protobuf
message QuerySKURequest {
  string uuid = 1;
}
```

**Response:**
```protobuf
message QuerySKUResponse {
  SKU sku = 1;
}
```

---

#### QuerySKUs

List all SKUs with pagination.

**Endpoint:** `liftedinit.sku.v1.Query/SKUs`

**Request:**
```protobuf
message QuerySKUsRequest {
  cosmos.base.query.v1beta1.PageRequest pagination = 1;
  bool active_only = 2;
}
```

**Response:**
```protobuf
message QuerySKUsResponse {
  repeated SKU skus = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}
```

---

#### QuerySKUsByProvider

List SKUs for a specific provider.

**Endpoint:** `liftedinit.sku.v1.Query/SKUsByProvider`

**Request:**
```protobuf
message QuerySKUsByProviderRequest {
  string provider_uuid = 1;
  cosmos.base.query.v1beta1.PageRequest pagination = 2;
  bool active_only = 3;
}
```

**Response:**
```protobuf
message QuerySKUsByProviderResponse {
  repeated SKU skus = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}
```

`provider_uuid` must be a non-empty canonical lowercase UUIDv7. An empty field
fails with gRPC `InvalidArgument` and `provider_uuid cannot be empty`; a
non-empty malformed, uppercase, or non-v7 value fails with `InvalidArgument`
and `provider_uuid must be a valid UUIDv7`. An unknown canonical value is valid
input and returns an empty page.

---

## REST API

REST endpoints are available via gRPC-gateway.

### Base URL

```
http://localhost:1317/liftedinit/sku/v1
```

### Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/params` | Get module parameters |
| GET | `/provider/{uuid}` | Get provider by UUID |
| GET | `/provider/address/{address}` | Get providers by management address |
| GET | `/providers` | List all providers |
| GET | `/sku/{uuid}` | Get SKU by UUID |
| GET | `/skus` | List all SKUs |
| GET | `/skus/provider/{provider_uuid}` | List SKUs by provider |

`/skus/provider/{provider_uuid}` applies the same canonical lowercase UUIDv7
validation as `QuerySKUsByProvider`. A non-empty malformed, uppercase, or
non-v7 path value maps to HTTP 400 / gRPC `InvalidArgument`; an unknown
canonical value returns an empty page. A missing path component does not match
the route; a trailing empty component does match and maps the handler's
`provider_uuid cannot be empty` response to HTTP 400.

The direct `/provider/{uuid}` and `/sku/{uuid}` routes map any non-empty key
that does not exist—including malformed, uppercase, or non-v7 text—to HTTP 404
/ gRPC `NotFound`. Their trailing-empty forms reach the handlers and map
`uuid cannot be empty` to HTTP 400. An unexpected primary-store or decoding
failure maps to HTTP 500 / gRPC `Internal`.

### Examples

**Get Parameters:**
```bash
curl http://localhost:1317/liftedinit/sku/v1/params
```

**Get Provider:**
```bash
curl http://localhost:1317/liftedinit/sku/v1/provider/01912345-6789-7abc-8def-0123456789ab
```

**Get Providers by Address:**
```bash
curl http://localhost:1317/liftedinit/sku/v1/provider/address/manifest1provider...
```

**List Active Providers:**
```bash
curl "http://localhost:1317/liftedinit/sku/v1/providers?active_only=true&pagination.limit=10"
```

**Get SKU:**
```bash
curl http://localhost:1317/liftedinit/sku/v1/sku/01912345-6789-7abc-8def-0123456789cd
```

**List Active SKUs:**
```bash
curl "http://localhost:1317/liftedinit/sku/v1/skus?active_only=true&pagination.limit=10"
```

**List SKUs by Provider:**
```bash
curl "http://localhost:1317/liftedinit/sku/v1/skus/provider/01912345-6789-7abc-8def-0123456789ab?active_only=true"
```

---

## Data Types

### Provider

```protobuf
message Provider {
  string uuid = 1;            // Unique UUIDv7 identifier
  string address = 2;         // Management address
  string payout_address = 3;  // Payout address
  bytes meta_hash = 4;        // Off-chain metadata hash (max 64 bytes)
  bool active = 5;            // Active status
  string api_url = 6;         // HTTPS endpoint for off-chain API
}
```

**Field Notes:**
- `meta_hash`: Optional hash or reference linking to off-chain metadata (e.g., provider description, terms of service, contact info). Maximum 64 bytes to accommodate SHA-256 or SHA-512 hashes. Each `MsgUpdateProvider` replaces this value: resend the current bytes to preserve it; an empty or omitted value clears it.

### SKU

```protobuf
message SKU {
  string uuid = 1;                         // Unique UUIDv7 identifier
  string provider_uuid = 2;                // Provider UUID
  string name = 3;                         // Human-readable name
  Unit unit = 4;                           // Billing unit
  cosmos.base.v1beta1.Coin base_price = 5; // Base price
  bytes meta_hash = 6;                     // Off-chain metadata hash (max 64 bytes)
  bool active = 7;                         // Active status
}
```

**Field Notes:**
- `meta_hash`: Optional hash or reference linking to off-chain metadata (e.g., detailed specifications, SLA terms, resource configurations). Maximum 64 bytes to accommodate SHA-256 or SHA-512 hashes. Each `MsgUpdateSKU` replaces this value: resend the current bytes to preserve it; an empty or omitted value clears it.

### Unit

```protobuf
enum Unit {
  UNIT_UNSPECIFIED = 0;  // Invalid
  UNIT_PER_HOUR = 1;     // Per-hour billing (3600 seconds)
  UNIT_PER_DAY = 2;      // Per-day billing (86400 seconds)
}
```

### Params

```protobuf
message Params {
  repeated string allowed_list = 1;  // Addresses allowed to manage SKUs
}
```

`allowed_list` is limited to 100 valid, distinct decoded account identities.
The hard cap bounds authorization scans and cannot be changed by governance.

---

## Events

The SKU module emits the following events for state changes:

| Event | Attributes | Description |
|-------|------------|-------------|
| `provider_created` | provider_uuid, address, payout_address, created_by | Provider created |
| `provider_updated` | provider_uuid | Provider updated |
| `provider_activated` | provider_uuid | Provider transitioned from inactive → active |
| `provider_deactivated` | provider_uuid, deactivated_by | Provider deactivated |
| `sku_created` | sku_uuid, provider_uuid, name, base_price, created_by | SKU created |
| `sku_updated` | sku_uuid, provider_uuid | SKU updated |
| `sku_activated` | sku_uuid, provider_uuid | SKU transitioned from inactive → active |
| `sku_deactivated` | sku_uuid, provider_uuid, deactivated_by | SKU deactivated |
| `params_updated` | | Module parameters updated |

### Event Attribute Sanitization

SKU names are sanitized before being emitted in events to prevent log injection attacks. The original name is stored in state unchanged, but the sanitized version appears in event attributes.

### Querying Events

Query the committed transaction and verify `code == 0` before extracting events.
Sync broadcast responses have no execution events. For transactions with multiple
messages, additionally filter events by their `msg_index` attribute to select the
intended message; the examples below assume a single creation message:

```bash
# Query events for a specific transaction
manifestd query tx [txhash] --output json | jq '.events'

# Example: Extract provider_uuid from a provider creation
manifestd query tx [txhash] --output json | jq -er 'select((.code | tonumber) == 0 and (.height | tonumber) > 0) | .events[] | select(.type=="provider_created") | .attributes[] | select(.key=="provider_uuid") | .value'
```

---

## Error Codes

| Error | Code | Description |
|-------|------|-------------|
| `ErrInvalidSKU` | 1 | Invalid SKU parameters (includes inactive check) |
| `ErrSKUNotFound` | 2 | SKU doesn't exist |
| `ErrUnauthorized` | 3 | Sender not authorized |
| `ErrInvalidConfig` | 4 | Invalid module configuration |
| `ErrInvalidProvider` | 5 | Invalid provider parameters (includes inactive check) |
| `ErrProviderNotFound` | 6 | Provider doesn't exist |
| `ErrInvalidAPIURL` | 7 | Invalid API URL (not HTTPS, too long, contains credentials, etc.) |
| `ErrSequenceExhausted` | 8 | A deterministic provider or SKU UUID sequence has exhausted its `uint64` range |
| `ErrInternalCorruption` | 9 | Provider/SKU primary state or the active-SKU index cannot be read consistently; distinct from a missing requested resource |

**Note:** Active status checks (e.g., "provider is not active", "SKU is not active") are reported via `ErrInvalidProvider` or `ErrInvalidSKU` respectively.

---

## Authorization

| Operation | Authority | Allowed List |
|-----------|-----------|--------------|
| CreateProvider | ✓ | ✓ |
| UpdateProvider | ✓ | ✓ |
| DeactivateProvider | ✓ | ✓ |
| CreateSKU | ✓ | ✓ |
| UpdateSKU | ✓ | ✓ |
| DeactivateSKU | ✓ | ✓ |
| UpdateParams | ✓ | ✗ |

---

## Related Documentation

- [Provider Setup Guide](PROVIDER_GUIDE.md) - Step-by-step guide to creating providers
- [SKU Setup Guide](SKU_GUIDE.md) - Step-by-step guide to creating SKUs
- [Architecture](ARCHITECTURE.md) - Internal architecture and data models
- [Design Decisions](DESIGN_DECISIONS.md) - Key design decisions and rationale
- [Capabilities](CAPABILITIES.md) - Module capability overview
- [Troubleshooting](TROUBLESHOOTING.md) - Common errors and resolutions
- [Billing Module](../../billing/README.md) - Understanding the billing system
