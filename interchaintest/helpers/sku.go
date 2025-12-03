package helpers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"

	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

// PageResponseJSON is a JSON-friendly version of query.PageResponse that handles
// string-encoded uint64 values from REST API responses.
// NextKey is kept as a string (base64-encoded) to preserve the format needed for CLI pagination.
type PageResponseJSON struct {
	NextKey string `json:"next_key,omitempty"`
	Total   string `json:"total,omitempty"`
}

// SKUsResponseJSON is a JSON-friendly version of QuerySKUsResponse.
type SKUsResponseJSON struct {
	Skus       []skutypes.SKU    `json:"skus"`
	Pagination *PageResponseJSON `json:"pagination,omitempty"`
}

// ToProto converts the JSON response to the protobuf type.
func (r *SKUsResponseJSON) ToProto() *skutypes.QuerySKUsResponse {
	res := &skutypes.QuerySKUsResponse{
		Skus: r.Skus,
	}
	if r.Pagination != nil {
		total, _ := strconv.ParseUint(r.Pagination.Total, 10, 64)
		nextKey, _ := base64.StdEncoding.DecodeString(r.Pagination.NextKey)
		res.Pagination = &query.PageResponse{
			NextKey: nextKey,
			Total:   total,
		}
	}
	return res
}

// GetNextKeyString returns the base64-encoded next key for CLI pagination.
func (r *SKUsResponseJSON) GetNextKeyString() string {
	if r.Pagination != nil {
		return r.Pagination.NextKey
	}
	return ""
}

// SKUsByProviderResponseJSON is a JSON-friendly version of QuerySKUsByProviderResponse.
type SKUsByProviderResponseJSON struct {
	Skus       []skutypes.SKU    `json:"skus"`
	Pagination *PageResponseJSON `json:"pagination,omitempty"`
}

// ToProto converts the JSON response to the protobuf type.
func (r *SKUsByProviderResponseJSON) ToProto() *skutypes.QuerySKUsByProviderResponse {
	res := &skutypes.QuerySKUsByProviderResponse{
		Skus: r.Skus,
	}
	if r.Pagination != nil {
		total, _ := strconv.ParseUint(r.Pagination.Total, 10, 64)
		nextKey, _ := base64.StdEncoding.DecodeString(r.Pagination.NextKey)
		res.Pagination = &query.PageResponse{
			NextKey: nextKey,
			Total:   total,
		}
	}
	return res
}

// GetNextKeyString returns the base64-encoded next key for CLI pagination.
func (r *SKUsByProviderResponseJSON) GetNextKeyString() string {
	if r.Pagination != nil {
		return r.Pagination.NextKey
	}
	return ""
}

// SKUCreateSKU creates a new SKU.
func SKUCreateSKU(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, provider, name string, unit int, basePrice string, metaHash string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "sku", "create-sku", provider, name, strconv.Itoa(unit), basePrice}
	if metaHash != "" {
		flags = append(flags, "--meta-hash", metaHash)
	}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// SKUUpdateSKU updates an existing SKU.
func SKUUpdateSKU(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, provider string, id uint64, name string, unit int, basePrice string, active bool, metaHash string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "sku", "update-sku", provider, strconv.FormatUint(id, 10), name, strconv.Itoa(unit), basePrice, strconv.FormatBool(active)}
	if metaHash != "" {
		flags = append(flags, "--meta-hash", metaHash)
	}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// SKUDeleteSKU deletes a SKU.
func SKUDeleteSKU(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, provider string, id uint64, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "sku", "delete-sku", provider, strconv.FormatUint(id, 10)}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// SKUUpdateParams updates the SKU module parameters.
func SKUUpdateParams(ctx context.Context, chain *cosmos.CosmosChain, user ibc.Wallet, allowedList string, flags ...string) (sdk.TxResponse, error) {
	cmd := []string{"tx", "sku", "update-params", "--allowed-list", allowedList}
	return ExecuteTransaction(ctx, chain, TxCommandBuilder(ctx, chain, cmd, user.KeyName(), flags...))
}

// SKUQueryParams queries the SKU module parameters.
func SKUQueryParams(ctx context.Context, chain *cosmos.CosmosChain) (*skutypes.QueryParamsResponse, error) {
	var res skutypes.QueryParamsResponse
	cmd := []string{"query", "sku", "params"}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// SKUQuerySKU queries a SKU by ID.
func SKUQuerySKU(ctx context.Context, chain *cosmos.CosmosChain, id uint64) (*skutypes.QuerySKUResponse, error) {
	var res skutypes.QuerySKUResponse
	cmd := []string{"query", "sku", "sku", strconv.FormatUint(id, 10)}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// SKUQuerySKUs queries all SKUs.
func SKUQuerySKUs(ctx context.Context, chain *cosmos.CosmosChain) (*skutypes.QuerySKUsResponse, error) {
	var res SKUsResponseJSON
	cmd := []string{"query", "sku", "skus"}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return res.ToProto(), nil
}

// SKUQuerySKUsPaginated queries SKUs with pagination.
// Returns the response and the base64-encoded next key for subsequent pagination calls.
func SKUQuerySKUsPaginated(ctx context.Context, chain *cosmos.CosmosChain, limit uint64, key string) (*skutypes.QuerySKUsResponse, string, error) {
	var res SKUsResponseJSON
	cmd := []string{"query", "sku", "skus", "--limit", strconv.FormatUint(limit, 10)}
	if key != "" {
		cmd = append(cmd, "--page-key", key)
	}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, "", err
	}
	return res.ToProto(), res.GetNextKeyString(), nil
}

// SKUQuerySKUsByProvider queries SKUs by provider.
func SKUQuerySKUsByProvider(ctx context.Context, chain *cosmos.CosmosChain, provider string) (*skutypes.QuerySKUsByProviderResponse, error) {
	var res SKUsByProviderResponseJSON
	cmd := []string{"query", "sku", "skus-by-provider", provider}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return res.ToProto(), nil
}

// SKUQuerySKUsByProviderPaginated queries SKUs by provider with pagination.
// Returns the response and the base64-encoded next key for subsequent pagination calls.
func SKUQuerySKUsByProviderPaginated(ctx context.Context, chain *cosmos.CosmosChain, provider string, limit uint64, key string) (*skutypes.QuerySKUsByProviderResponse, string, error) {
	var res SKUsByProviderResponseJSON
	cmd := []string{"query", "sku", "skus-by-provider", provider, "--limit", strconv.FormatUint(limit, 10)}
	if key != "" {
		cmd = append(cmd, "--page-key", key)
	}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, "", err
	}
	return res.ToProto(), res.GetNextKeyString(), nil
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

// SKUQuerySKURaw queries a SKU by ID and returns raw JSON (for error checking).
func SKUQuerySKURaw(ctx context.Context, chain *cosmos.CosmosChain, id uint64) ([]byte, error) {
	cmd := []string{chain.Config().Bin, "query", "sku", "sku", strconv.FormatUint(id, 10), "--node", chain.GetRPCAddress(), "--output=json"}
	stdout, _, err := chain.Exec(ctx, cmd, chain.Config().Env)
	return stdout, err
}

// GetSKUIDFromTxResponse extracts the SKU ID from a CreateSKU transaction response.
func GetSKUIDFromTxResponse(res sdk.TxResponse) (uint64, error) {
	for _, event := range res.Events {
		if event.Type == "sku_created" {
			for _, attr := range event.Attributes {
				if attr.Key == "sku_id" {
					return strconv.ParseUint(attr.Value, 10, 64)
				}
			}
		}
	}
	return 0, fmt.Errorf("sku_id not found in events")
}

// GetSKUIDFromTxHash queries a transaction and extracts the SKU ID from it.
func GetSKUIDFromTxHash(_ context.Context, chain *cosmos.CosmosChain, txHash string) (uint64, error) {
	txRes, err := chain.GetTransaction(txHash)
	if err != nil {
		return 0, err
	}

	for _, event := range txRes.Events {
		if event.Type == "sku_created" {
			for _, attr := range event.Attributes {
				if attr.Key == "sku_id" {
					return strconv.ParseUint(attr.Value, 10, 64)
				}
			}
		}
	}
	return 0, fmt.Errorf("sku_id not found in tx %s events", txHash)
}

// SKURawResponse is used to parse raw query responses.
type SKURawResponse struct {
	Code    int             `json:"code,omitempty"`
	Message string          `json:"message,omitempty"`
	SKU     json.RawMessage `json:"sku,omitempty"`
}
