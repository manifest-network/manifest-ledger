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

### "invalid sku" (when used in billing)

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

---

## API URL Issues

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

**Cause**: The API URL exceeds the maximum length of 2048 characters.

**Solution**: Use a shorter URL. Consider using a URL shortener service or a shorter domain/path.

---

## Parameter Issues

### "invalid params" (duplicate addresses)

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

**Behavior**: Attempting to deactivate an already inactive provider or SKU returns an error.

**Error Messages:**
- Provider: `invalid provider: provider {uuid} is already inactive`
- SKU: `invalid sku: sku {uuid} is already inactive`

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

## Query Issues

### "invalid uuid format"

**Cause**: The UUID is not in valid UUIDv7 format.

**Solution**: Ensure you're using a valid UUID (format: `xxxxxxxx-xxxx-7xxx-xxxx-xxxxxxxxxxxx`):
```bash
manifestd query sku provider 01912345-6789-7abc-8def-0123456789ab
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
