package helpers

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"

	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

// PageResponseJSON is needed because cosmos-sdk's query.PageResponse.Total lacks the
// `,string` JSON tag. CLI output encodes uint64 as strings, causing unmarshal failures
// with the proto type. The proto types work for non-paginated queries since their
// uint64 fields have the `,string` tag.
type PageResponseJSON struct {
	NextKey string `json:"next_key,omitempty"`
	Total   string `json:"total,omitempty"`
}

// ProvidersResponseJSON wraps provider list queries with pagination.
type ProvidersResponseJSON struct {
	Providers  []skutypes.Provider `json:"providers"`
	Pagination *PageResponseJSON   `json:"pagination,omitempty"`
}

// GetNextKeyString returns the base64-encoded next key for CLI pagination.
func (r *ProvidersResponseJSON) GetNextKeyString() string {
	if r.Pagination != nil {
		return r.Pagination.NextKey
	}
	return ""
}

// SKUsResponseJSON wraps SKU list queries with pagination.
// Used for both QuerySKUsResponse and QuerySKUsByProviderResponse since they have identical structure.
type SKUsResponseJSON struct {
	Skus       []skutypes.SKU    `json:"skus"`
	Pagination *PageResponseJSON `json:"pagination,omitempty"`
}

// ProviderByAddressResponseJSON wraps provider-by-address queries with pagination.
type ProviderByAddressResponseJSON struct {
	Providers  []skutypes.Provider `json:"providers"`
	Pagination *PageResponseJSON   `json:"pagination,omitempty"`
}

// GetNextKeyString returns the base64-encoded next key for CLI pagination.
func (r *SKUsResponseJSON) GetNextKeyString() string {
	if r.Pagination != nil {
		return r.Pagination.NextKey
	}
	return ""
}

// Provider transaction helpers

// SKUCreateProvider creates a new provider.
func SKUCreateProvider(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, address, payoutAddress string, metaHash string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "sku", "create-provider", address, payoutAddress}
	if metaHash != "" {
		flags = append(flags, "--meta-hash", metaHash)
	}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// SKUCreateProviderFull creates a new provider with all fields including api_url.
func SKUCreateProviderFull(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, address, payoutAddress, metaHash, apiURL string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "sku", "create-provider", address, payoutAddress}
	if metaHash != "" {
		flags = append(flags, "--meta-hash", metaHash)
	}
	if apiURL != "" {
		flags = append(flags, "--api-url", apiURL)
	}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// SKUUpdateProvider updates an existing provider.
func SKUUpdateProvider(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, uuid, address, payoutAddress string, active bool, metaHash string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "sku", "update-provider", uuid, address, payoutAddress, strconv.FormatBool(active)}
	if metaHash != "" {
		flags = append(flags, "--meta-hash", metaHash)
	}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// SKUUpdateProviderFull updates an existing provider with all fields.
func SKUUpdateProviderFull(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, uuid, address, payoutAddress string, active bool, metaHash, apiURL string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "sku", "update-provider", uuid, address, payoutAddress, strconv.FormatBool(active)}
	if metaHash != "" {
		flags = append(flags, "--meta-hash", metaHash)
	}
	if apiURL != "" {
		flags = append(flags, "--api-url", apiURL)
	}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// SKUDeactivateProvider deactivates a provider (soft delete).
func SKUDeactivateProvider(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, uuid string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "sku", "deactivate-provider", uuid}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// SKU transaction helpers

// SKUCreateSKU creates a new SKU.
func SKUCreateSKU(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, providerUUID, name string, unit int, basePrice string, metaHash string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "sku", "create-sku", providerUUID, name, strconv.Itoa(unit), basePrice}
	if metaHash != "" {
		flags = append(flags, "--meta-hash", metaHash)
	}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// SKUUpdateSKU updates an existing SKU.
func SKUUpdateSKU(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, uuid, providerUUID, name string, unit int, basePrice string, active bool, metaHash string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "sku", "update-sku", uuid, providerUUID, name, strconv.Itoa(unit), basePrice, strconv.FormatBool(active)}
	if metaHash != "" {
		flags = append(flags, "--meta-hash", metaHash)
	}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// SKUDeactivateSKU deactivates a SKU (soft delete).
func SKUDeactivateSKU(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, uuid string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "sku", "deactivate-sku", uuid}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// SKUUpdateParams updates the SKU module parameters.
func SKUUpdateParams(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, allowedList string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "sku", "update-params", "--allowed-list", allowedList}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// Query helpers

// SKUQueryParams queries the SKU module parameters.
func SKUQueryParams(ctx context.Context, chain *cosmos.CosmosChain) (*skutypes.QueryParamsResponse, error) {
	var res skutypes.QueryParamsResponse
	cmd := []string{"query", "sku", "params"}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// SKUQueryProvider queries a provider by UUID.
func SKUQueryProvider(ctx context.Context, chain *cosmos.CosmosChain, uuid string) (*skutypes.QueryProviderResponse, error) {
	var res skutypes.QueryProviderResponse
	cmd := []string{"query", "sku", "provider", uuid}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// SKUQueryProviderByAddress queries a provider by their address.
func SKUQueryProviderByAddress(ctx context.Context, chain *cosmos.CosmosChain, address string) (*ProviderByAddressResponseJSON, error) {
	var res ProviderByAddressResponseJSON
	cmd := []string{"query", "sku", "provider-by-address", address}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// SKUQueryProviders queries all providers.
func SKUQueryProviders(ctx context.Context, chain *cosmos.CosmosChain) (*ProvidersResponseJSON, error) {
	var res ProvidersResponseJSON
	cmd := []string{"query", "sku", "providers"}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// SKUQueryProvidersPaginated queries providers with pagination.
// Returns the response and the base64-encoded next key for subsequent pagination calls.
func SKUQueryProvidersPaginated(ctx context.Context, chain *cosmos.CosmosChain, limit uint64, key string) (*ProvidersResponseJSON, string, error) {
	var res ProvidersResponseJSON
	cmd := []string{"query", "sku", "providers", "--limit", strconv.FormatUint(limit, 10)}
	if key != "" {
		cmd = append(cmd, "--page-key", key)
	}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, "", err
	}
	return &res, res.GetNextKeyString(), nil
}

// SKUQuerySKU queries a SKU by UUID.
func SKUQuerySKU(ctx context.Context, chain *cosmos.CosmosChain, uuid string) (*skutypes.QuerySKUResponse, error) {
	var res skutypes.QuerySKUResponse
	cmd := []string{"query", "sku", "sku", uuid}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// SKUQuerySKUs queries all SKUs.
func SKUQuerySKUs(ctx context.Context, chain *cosmos.CosmosChain) (*SKUsResponseJSON, error) {
	var res SKUsResponseJSON
	cmd := []string{"query", "sku", "skus"}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// SKUQuerySKUsPaginated queries SKUs with pagination.
// Returns the response and the base64-encoded next key for subsequent pagination calls.
func SKUQuerySKUsPaginated(ctx context.Context, chain *cosmos.CosmosChain, limit uint64, key string) (*SKUsResponseJSON, string, error) {
	var res SKUsResponseJSON
	cmd := []string{"query", "sku", "skus", "--limit", strconv.FormatUint(limit, 10)}
	if key != "" {
		cmd = append(cmd, "--page-key", key)
	}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, "", err
	}
	return &res, res.GetNextKeyString(), nil
}

// SKUQuerySKUsByProvider queries SKUs by provider UUID.
func SKUQuerySKUsByProvider(ctx context.Context, chain *cosmos.CosmosChain, providerUUID string) (*SKUsResponseJSON, error) {
	var res SKUsResponseJSON
	cmd := []string{"query", "sku", "skus-by-provider", providerUUID}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// SKUQuerySKUsByProviderPaginated queries SKUs by provider UUID with pagination.
// Returns the response and the base64-encoded next key for subsequent pagination calls.
func SKUQuerySKUsByProviderPaginated(ctx context.Context, chain *cosmos.CosmosChain, providerUUID string, limit uint64, key string) (*SKUsResponseJSON, string, error) {
	var res SKUsResponseJSON
	cmd := []string{"query", "sku", "skus-by-provider", providerUUID, "--limit", strconv.FormatUint(limit, 10)}
	if key != "" {
		cmd = append(cmd, "--page-key", key)
	}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, "", err
	}
	return &res, res.GetNextKeyString(), nil
}

// executeQueryWithError executes a query command and returns an error if it fails.
func executeQueryWithError(ctx context.Context, chain *cosmos.CosmosChain, cmd []string, result interface{}) error {
	flags := []string{
		"--node", chain.GetRPCAddress(),
		"--output=json",
	}

	command := []string{chain.Config().Bin}
	command = append(command, cmd...)
	command = append(command, flags...)

	stdout, _, err := chain.Exec(ctx, command, chain.Config().Env)
	if err != nil {
		return fmt.Errorf("query execution failed: %w", err)
	}

	if err := json.Unmarshal(stdout, result); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return nil
}

// GetProviderUUIDFromTxHash queries a transaction and extracts the provider UUID from it.
func GetProviderUUIDFromTxHash(_ context.Context, chain *cosmos.CosmosChain, txHash string) (string, error) {
	txRes, err := chain.GetTransaction(txHash)
	if err != nil {
		return "", err
	}

	for _, event := range txRes.Events {
		if event.Type == "provider_created" {
			for _, attr := range event.Attributes {
				if attr.Key == "provider_uuid" {
					return attr.Value, nil
				}
			}
		}
	}
	return "", fmt.Errorf("provider_uuid not found in tx %s events", txHash)
}

// GetSKUUUIDFromTxHash queries a transaction and extracts the SKU UUID from it.
func GetSKUUUIDFromTxHash(_ context.Context, chain *cosmos.CosmosChain, txHash string) (string, error) {
	txRes, err := chain.GetTransaction(txHash)
	if err != nil {
		return "", err
	}

	for _, event := range txRes.Events {
		if event.Type == "sku_created" {
			for _, attr := range event.Attributes {
				if attr.Key == "sku_uuid" {
					return attr.Value, nil
				}
			}
		}
	}
	return "", fmt.Errorf("sku_uuid not found in tx %s events", txHash)
}

// Message builders for governance proposals

// CreateProviderMsg creates a MsgCreateProvider for use in governance proposals.
func CreateProviderMsg(authority, address, metaHash, apiURL string) skutypes.MsgCreateProvider {
	msg := skutypes.MsgCreateProvider{
		Authority:     authority,
		Address:       address,
		PayoutAddress: address, // Default payout to same address
	}
	if metaHash != "" {
		msg.MetaHash = []byte(metaHash)
	}
	if apiURL != "" {
		msg.ApiUrl = apiURL
	}
	return msg
}

// CreateSKUMsg creates a MsgCreateSKU for use in governance proposals.
// priceModel can be "UNIT_PER_HOUR" or "UNIT_PER_DAY"
func CreateSKUMsg(authority, providerUUID, name string, basePrice sdk.Coin, priceModel string) skutypes.MsgCreateSKU {
	// Map price model string to Unit enum
	unit := skutypes.Unit_UNIT_UNSPECIFIED
	switch priceModel {
	case "UNIT_PER_HOUR":
		unit = skutypes.Unit_UNIT_PER_HOUR
	case "UNIT_PER_DAY":
		unit = skutypes.Unit_UNIT_PER_DAY
	}
	return skutypes.MsgCreateSKU{
		Authority:    authority,
		ProviderUuid: providerUUID,
		Name:         name,
		Unit:         unit,
		BasePrice:    basePrice,
	}
}
