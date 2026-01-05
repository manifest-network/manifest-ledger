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

// ProvidersResponseJSON is a JSON-friendly version of QueryProvidersResponse.
type ProvidersResponseJSON struct {
	Providers  []skutypes.Provider `json:"providers"`
	Pagination *PageResponseJSON   `json:"pagination,omitempty"`
}

// ToProto converts the JSON response to the protobuf type.
func (r *ProvidersResponseJSON) ToProto() *skutypes.QueryProvidersResponse {
	res := &skutypes.QueryProvidersResponse{
		Providers: r.Providers,
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
func (r *ProvidersResponseJSON) GetNextKeyString() string {
	if r.Pagination != nil {
		return r.Pagination.NextKey
	}
	return ""
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

// SKUQueryProviders queries all providers.
func SKUQueryProviders(ctx context.Context, chain *cosmos.CosmosChain) (*skutypes.QueryProvidersResponse, error) {
	var res ProvidersResponseJSON
	cmd := []string{"query", "sku", "providers"}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return res.ToProto(), nil
}

// SKUQueryProvidersPaginated queries providers with pagination.
func SKUQueryProvidersPaginated(ctx context.Context, chain *cosmos.CosmosChain, limit uint64, key string) (*skutypes.QueryProvidersResponse, string, error) {
	var res ProvidersResponseJSON
	cmd := []string{"query", "sku", "providers", "--limit", strconv.FormatUint(limit, 10)}
	if key != "" {
		cmd = append(cmd, "--page-key", key)
	}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, "", err
	}
	return res.ToProto(), res.GetNextKeyString(), nil
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

// SKUQuerySKUsByProvider queries SKUs by provider UUID.
func SKUQuerySKUsByProvider(ctx context.Context, chain *cosmos.CosmosChain, providerUUID string) (*skutypes.QuerySKUsByProviderResponse, error) {
	var res SKUsByProviderResponseJSON
	cmd := []string{"query", "sku", "skus-by-provider", providerUUID}
	if err := executeQueryWithError(ctx, chain, cmd, &res); err != nil {
		return nil, err
	}
	return res.ToProto(), nil
}

// SKUQuerySKUsByProviderPaginated queries SKUs by provider UUID with pagination.
// Returns the response and the base64-encoded next key for subsequent pagination calls.
func SKUQuerySKUsByProviderPaginated(ctx context.Context, chain *cosmos.CosmosChain, providerUUID string, limit uint64, key string) (*skutypes.QuerySKUsByProviderResponse, string, error) {
	var res SKUsByProviderResponseJSON
	cmd := []string{"query", "sku", "skus-by-provider", providerUUID, "--limit", strconv.FormatUint(limit, 10)}
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

// SKUQuerySKURaw queries a SKU by UUID and returns raw JSON (for error checking).
func SKUQuerySKURaw(ctx context.Context, chain *cosmos.CosmosChain, uuid string) ([]byte, error) {
	cmd := []string{chain.Config().Bin, "query", "sku", "sku", uuid, "--node", chain.GetRPCAddress(), "--output=json"}
	stdout, _, err := chain.Exec(ctx, cmd, chain.Config().Env)
	return stdout, err
}

// GetProviderUUIDFromTxResponse extracts the provider UUID from a CreateProvider transaction response.
func GetProviderUUIDFromTxResponse(res sdk.TxResponse) (string, error) {
	for _, event := range res.Events {
		if event.Type == "provider_created" {
			for _, attr := range event.Attributes {
				if attr.Key == "provider_uuid" {
					return attr.Value, nil
				}
			}
		}
	}
	return "", fmt.Errorf("provider_uuid not found in events")
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

// GetSKUUUIDFromTxResponse extracts the SKU UUID from a CreateSKU transaction response.
func GetSKUUUIDFromTxResponse(res sdk.TxResponse) (string, error) {
	for _, event := range res.Events {
		if event.Type == "sku_created" {
			for _, attr := range event.Attributes {
				if attr.Key == "sku_uuid" {
					return attr.Value, nil
				}
			}
		}
	}
	return "", fmt.Errorf("sku_uuid not found in events")
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

// SKURawResponse is used to parse raw query responses.
type SKURawResponse struct {
	Code    int             `json:"code,omitempty"`
	Message string          `json:"message,omitempty"`
	SKU     json.RawMessage `json:"sku,omitempty"`
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

// CreateProviderMsgFull creates a MsgCreateProvider with all fields for use in governance proposals.
func CreateProviderMsgFull(authority, address, payoutAddress, metaHash, apiURL string) skutypes.MsgCreateProvider {
	msg := skutypes.MsgCreateProvider{
		Authority:     authority,
		Address:       address,
		PayoutAddress: payoutAddress,
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

// CreateSKUMsgFull creates a MsgCreateSKU with all fields for use in governance proposals.
func CreateSKUMsgFull(authority, providerUUID, name string, unit skutypes.Unit, basePrice sdk.Coin, metaHash string) skutypes.MsgCreateSKU {
	msg := skutypes.MsgCreateSKU{
		Authority:    authority,
		ProviderUuid: providerUUID,
		Name:         name,
		Unit:         unit,
		BasePrice:    basePrice,
	}
	if metaHash != "" {
		msg.MetaHash = []byte(metaHash)
	}
	return msg
}
