package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/manifest-network/manifest-ledger/pkg/pagination"
	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

var _ types.QueryServer = Querier{}

// Querier implements the module gRPC query service.
// It wraps the Keeper to provide query functionality without exposing
// internal keeper methods, following the same pattern as billing module.
type Querier struct {
	k Keeper
}

// NewQuerier returns a new Querier instance.
func NewQuerier(keeper Keeper) Querier {
	return Querier{k: keeper}
}

// Params queries the module parameters.
func (q Querier) Params(ctx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	params, err := q.k.GetParams(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryParamsResponse{Params: params}, nil
}

// Provider queries a Provider by its UUID.
func (q Querier) Provider(ctx context.Context, req *types.QueryProviderRequest) (*types.QueryProviderResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Uuid == "" {
		return nil, status.Error(codes.InvalidArgument, "uuid cannot be empty")
	}

	provider, err := q.k.GetProvider(ctx, req.Uuid)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	return &types.QueryProviderResponse{Provider: provider}, nil
}

// Providers queries all Providers with pagination.
func (q Querier) Providers(ctx context.Context, req *types.QueryProvidersRequest) (*types.QueryProvidersResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Use filtered pagination if active_only is set
	if req.ActiveOnly {
		providers, pageRes, err := query.CollectionFilteredPaginate(
			ctx,
			q.k.Providers,
			req.Pagination,
			func(_ string, provider types.Provider) (bool, error) {
				return provider.Active, nil
			},
			func(_ string, provider types.Provider) (types.Provider, error) {
				return provider, nil
			},
		)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		return &types.QueryProvidersResponse{
			Providers:  providers,
			Pagination: pageRes,
		}, nil
	}

	providers, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Providers,
		req.Pagination,
		func(_ string, provider types.Provider) (types.Provider, error) {
			return provider, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryProvidersResponse{
		Providers:  providers,
		Pagination: pageRes,
	}, nil
}

// SKU queries a SKU by its UUID.
func (q Querier) SKU(ctx context.Context, req *types.QuerySKURequest) (*types.QuerySKUResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Uuid == "" {
		return nil, status.Error(codes.InvalidArgument, "uuid cannot be empty")
	}

	sku, err := q.k.GetSKU(ctx, req.Uuid)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	return &types.QuerySKUResponse{Sku: sku}, nil
}

// SKUs queries all SKUs with pagination.
func (q Querier) SKUs(ctx context.Context, req *types.QuerySKUsRequest) (*types.QuerySKUsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Use filtered pagination if active_only is set
	if req.ActiveOnly {
		skus, pageRes, err := query.CollectionFilteredPaginate(
			ctx,
			q.k.SKUs,
			req.Pagination,
			func(_ string, sku types.SKU) (bool, error) {
				return sku.Active, nil
			},
			func(_ string, sku types.SKU) (types.SKU, error) {
				return sku, nil
			},
		)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		return &types.QuerySKUsResponse{
			Skus:       skus,
			Pagination: pageRes,
		}, nil
	}

	skus, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.SKUs,
		req.Pagination,
		func(_ string, sku types.SKU) (types.SKU, error) {
			return sku, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QuerySKUsResponse{
		Skus:       skus,
		Pagination: pageRes,
	}, nil
}

// SKUsByProvider queries SKUs by provider UUID with pagination.
// Uses the Provider index for efficient lookup - only iterates over SKUs belonging to this provider.
func (q Querier) SKUsByProvider(ctx context.Context, req *types.QuerySKUsByProviderRequest) (*types.QuerySKUsByProviderResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.ProviderUuid == "" {
		return nil, status.Error(codes.InvalidArgument, "provider_uuid cannot be empty")
	}

	// Use the provider index to iterate only over this provider's SKUs
	iter, err := q.k.SKUs.Indexes.Provider.MatchExact(ctx, req.ProviderUuid)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Build filter based on active_only
	var filter func(types.SKU) bool
	if req.ActiveOnly {
		filter = func(s types.SKU) bool { return s.Active }
	}

	skus, pageRes, err := pagination.PaginateStringIndex(
		ctx,
		iter,
		q.k.SKUs.Get,
		req.Pagination,
		filter,
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QuerySKUsByProviderResponse{
		Skus:       skus,
		Pagination: pageRes,
	}, nil
}

// ProviderByAddress queries a Provider by its management address.
// Note: This query requires scanning all providers since there is no address index.
// This is acceptable since the provider count is typically small.
func (q Querier) ProviderByAddress(ctx context.Context, req *types.QueryProviderByAddressRequest) (*types.QueryProviderByAddressResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address cannot be empty")
	}

	// Validate address format
	if _, err := sdk.AccAddressFromBech32(req.Address); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid address format")
	}

	// Iterate through all providers to find matching address
	var foundProvider *types.Provider
	err := q.k.Providers.Walk(ctx, nil, func(_ string, provider types.Provider) (stop bool, err error) {
		if provider.Address == req.Address {
			foundProvider = &provider
			return true, nil // Stop iteration
		}
		return false, nil // Continue iteration
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if foundProvider == nil {
		return nil, status.Error(codes.NotFound, "provider not found for address")
	}

	return &types.QueryProviderByAddressResponse{Provider: *foundProvider}, nil
}
