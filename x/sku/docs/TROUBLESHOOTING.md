# SKU Module Troubleshooting Guide

This guide covers common errors and issues users may encounter when using the SKU module.

## Provider Issues

### "provider not found"

**Cause**: The specified provider UUID doesn't exist.

**Solution**:
1. Query available providers:
   ```bash
   manifestd query sku providers
   ```
2. Use a valid provider UUID.

### "invalid provider" (when creating SKU)

**Cause**: The provider exists but is not active. SKUs can only be created for active providers.

**Solution**:
1. Check the provider's status:
   ```bash
   manifestd query sku provider [provider-uuid]
   ```
2. If the provider is inactive, contact an authorized user (authority or allowed list member) to reactivate it:
   ```bash
   manifestd tx sku update-provider [provider-uuid] [address] [payout-address] true --from [authorized-key]
   ```

### "unauthorized"

**Cause**: The sender is not the module authority and not in the `allowed_list`.

**Solution**:
1. Check if your address is in the allowed list:
   ```bash
   manifestd query sku params
   ```
2. If not, contact the authority to add your address:
   ```bash
   manifestd tx sku update-params --allowed-list "your-address,other-addresses" --from authority
   ```

---

## SKU Issues

### "sku not found"

**Cause**: The specified SKU UUID doesn't exist.

**Solution**:
1. Query available SKUs:
   ```bash
   manifestd query sku skus
   ```
2. Query SKUs for a specific provider:
   ```bash
   manifestd query sku skus-by-provider [provider-uuid]
   ```
3. Use a valid SKU UUID.

### "sku not active" (when used in billing)

**Error**: `sku not active: sku_uuid {uuid} is not active` (codespace `billing`, code 11)

**Cause**: The SKU exists but is not active. Inactive SKUs cannot be used for new leases.

**Solution**:
1. Check the SKU's status:
   ```bash
   manifestd query sku sku [sku-uuid]
   ```
2. If the SKU is inactive, contact an authorized user (authority or allowed list member) to reactivate it:
   ```bash
   manifestd tx sku update-sku [sku-uuid] [provider-uuid] [name] [unit] [base-price] true --from [authorized-key]
   ```

### "invalid sku" (price not divisible)

**Cause**: The SKU's base price is not exactly divisible by the billing unit's seconds.

**Solution**: Ensure your price is divisible:
- **UNIT_PER_HOUR (1)**: Price must be divisible by 3600
- **UNIT_PER_DAY (2)**: Price must be divisible by 86400

**Examples:**
```bash
# Valid: 3600 / 3600 = 1 per second (exact)
manifestd tx sku create-sku [provider-uuid] "Compute Small" 1 3600upwr --from authority

# Valid: 7200 / 3600 = 2 per second (exact)
manifestd tx sku create-sku [provider-uuid] "Compute Medium" 1 7200upwr --from authority

# Invalid: 3601 / 3600 = 1.000277... (not exact)
# This will fail with "invalid sku" error
manifestd tx sku create-sku [provider-uuid] "Bad SKU" 1 3601upwr --from authority
```

**Second failure mode (price too small)**: If the base price is smaller than the
unit's divisor (< 3600 for UNIT_PER_HOUR, < 86400 for UNIT_PER_DAY), the
per-second rate truncates to zero and the transaction fails with a different
message: `base price {price} with unit {unit} results in zero per-second rate; increase price or change unit`.
This is checked *before* the divisibility check.

```bash
# Invalid: 1000 / 3600 truncates to 0 per second
# This will fail with the "zero per-second rate" message
manifestd tx sku create-sku [provider-uuid] "Too Cheap" 1 1000upwr --from authority
```

**Solution**: Raise the base price to at least the unit's divisor (≥ 3600 for
`UNIT_PER_HOUR`, ≥ 86400 for `UNIT_PER_DAY`) and keep it evenly divisible. A
coarser unit has a *larger* divisor, so `UNIT_PER_DAY` (86400) makes the
zero-rate failure **more** likely, not less — prefer the finer `UNIT_PER_HOUR`
or simply raise the price.

---

## API URL Issues

### "clear_api_url cannot be true when api_url is non-empty"

**Cause**: An update requested two conflicting operations: set the URL and
clear it.

**Solution**: Use exactly one of `--api-url <https-url>` or
`--clear-api-url`. Omit both to preserve the existing URL.

### "invalid API URL" (not HTTPS)

**Cause**: The API URL doesn't use HTTPS scheme.

**Solution**: Use an HTTPS URL:
```bash
# Wrong:
manifestd tx sku create-provider manifest1... manifest1... --api-url http://api.example.com --from authority

# Correct:
manifestd tx sku create-provider manifest1... manifest1... --api-url https://api.example.com --from authority
```

### "invalid API URL" (contains credentials)

**Cause**: The API URL contains embedded user credentials.

**Solution**: Remove credentials from the URL:
```bash
# Wrong:
manifestd tx sku create-provider manifest1... manifest1... --api-url https://user:pass@api.example.com --from authority

# Correct:
manifestd tx sku create-provider manifest1... manifest1... --api-url https://api.example.com --from authority
```

**Note**: Authentication should be handled separately (e.g., via headers at the application level), not embedded in the URL.

### "invalid API URL" (empty host)

**Cause**: The API URL is malformed and doesn't have a valid host.

**Solution**: Provide a properly formatted URL:
```bash
# Wrong:
manifestd tx sku create-provider manifest1... manifest1... --api-url https:///path --from authority

# Correct:
manifestd tx sku create-provider manifest1... manifest1... --api-url https://api.example.com/path --from authority
```

### "invalid API URL" (too long)

**Cause**: The API URL exceeds the maximum encoded length of 2048 UTF-8 bytes.

**Solution**: Use a shorter URL. Consider using a URL shortener service or a shorter domain/path.

---

## Parameter Issues

### "invalid module configuration" (duplicate addresses)

**Error**: `invalid module configuration: invalid params: invalid module configuration: duplicate address in allowed list: {addr}`

**Cause**: The `allowed_list` contains duplicate addresses.

**Solution**: Remove duplicate addresses:
```bash
# Wrong (duplicate manifest1abc):
manifestd tx sku update-params --allowed-list "manifest1abc...,manifest1def...,manifest1abc..." --from authority

# Correct:
manifestd tx sku update-params --allowed-list "manifest1abc...,manifest1def..." --from authority
```

### "unauthorized" on UpdateParams

**Cause**: Only the module authority can update parameters.

**Solution**: Use the authority account (POA admin group):
```bash
manifestd tx sku update-params --allowed-list "manifest1abc..." --from authority
```

---

## Deactivation Issues

### Deactivating an already inactive provider/SKU

**Behavior**: The two cases differ:
- **SKU**: Deactivating an already inactive SKU always returns an error.
- **Provider**: Deactivating an already inactive provider returns an error **only if all of its SKUs are also inactive**. If the provider still has active SKUs, the call is treated as a continuation of the paginated SKU cascade and succeeds (see note below).

**Error Messages:**
- Provider (only when all SKUs are already inactive): `invalid provider: provider {uuid} and all its SKUs are already inactive`
- SKU: `invalid sku: sku {uuid} is already inactive`

**Note**: `DeactivateProvider` deactivates a provider's SKUs in pages (`DefaultDeactivateSKULimit` = 50, `MaxDeactivateSKULimit` = 100). When the response reports `has_more` = true, the provider is already inactive but SKUs remain; you must **re-invoke** `DeactivateProvider` while `has_more` is true. Re-invocation on an already-inactive provider is the normal, expected flow and is **not** an error condition.

**Solution**: Check the provider/SKU status before deactivation:
```bash
# Check provider status
manifestd query sku provider [provider-uuid]

# Check SKU status
manifestd query sku sku [sku-uuid]
```

**Note**: If idempotent behavior is desired in your application logic, check the `active` field before calling deactivate.

### Cannot create SKU for deactivated provider

**Cause**: Attempting to create a SKU for a provider that is not active.

**Solution**:
1. Reactivate the provider first:
   ```bash
   manifestd tx sku update-provider [provider-uuid] [address] [payout-address] true --from authority
   ```
2. Then create the SKU:
   ```bash
   manifestd tx sku create-sku [provider-uuid] "SKU Name" 1 3600upwr --from authority
   ```

---

## UUID Format Issues

### "invalid UUIDv7 format" / "provider_uuid must be a valid UUIDv7"

**Errors**:

- Transactions with an empty UUID field include `uuid cannot be empty`, wrapped
  with the field and module-error context.
- Transactions with a non-empty invalid UUID: `invalid UUIDv7 format: {uuid}`
  appears inside the field- and module-specific error.
- `skus-by-provider` with an empty value: `provider_uuid cannot be empty`.
- `skus-by-provider` with a non-empty invalid value:
  `provider_uuid must be a valid UUIDv7`.

**Cause**: The UUID is not in valid UUIDv7 format. Transactions validate UUIDs
during message validation. The `query sku skus-by-provider` collection query
also requires a canonical lowercase provider UUIDv7 and returns gRPC
`InvalidArgument` for malformed, uppercase, or non-v7 input. An unknown
canonical provider UUIDv7 returns an empty SKU page. Direct `query sku provider`
and `query sku sku` lookups remain unchanged: an empty `uuid` is rejected with
`InvalidArgument: uuid cannot be empty`; a non-empty malformed or unknown key
is looked up as-is and returns gRPC `NotFound` with `provider not found` or
`sku not found`.

**Format constraints** (see the UUIDv7 regex): lowercase hex digits only, the version nibble must be `7`, and the variant nibble must be one of `8`, `9`, `a`, or `b`. Uppercase UUIDs are rejected.

**Solution**: Ensure you're using a valid UUID (format: `xxxxxxxx-xxxx-7xxx-yxxx-xxxxxxxxxxxx`, where `y` is `8`/`9`/`a`/`b`):
```bash
manifestd tx sku deactivate-provider 01912345-6789-7abc-8def-0123456789ab --from authority
```

---

## Getting Help

If you encounter an issue not covered here:

1. **Check the full error message**: The error often contains specific details
2. **Query relevant state**:
   ```bash
   manifestd query sku params
   manifestd query sku provider [provider-uuid]
   manifestd query sku sku [sku-uuid]
   manifestd query sku providers
   manifestd query sku skus
   ```
3. **Check events**: Query the transaction to see emitted events
   ```bash
   manifestd query tx [txhash] --output json | jq '.events'
   ```

## Related Documentation

- [SKU README](../README.md) - Complete SKU module overview
- [API Reference](API.md) - Detailed API documentation
- [Architecture](ARCHITECTURE.md) - Technical architecture details
- [Billing Troubleshooting](../../billing/docs/TROUBLESHOOTING.md) - Billing-related issues
