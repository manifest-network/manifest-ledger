package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

var _ types.QueryServer = Querier{}

// Querier implements the module gRPC query service.
type Querier struct {
	Keeper
}

// NewQuerier returns a new Querier instance.
func NewQuerier(keeper Keeper) Querier {
	return Querier{Keeper: keeper}
}

// Params queries the module parameters.
func (q Querier) Params(ctx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	params, err := q.Keeper.GetParams(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryParamsResponse{Params: params}, nil
}

// Provider queries a Provider by its ID.
func (q Querier) Provider(ctx context.Context, req *types.QueryProviderRequest) (*types.QueryProviderResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	provider, err := q.Keeper.GetProvider(ctx, req.Id)
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
			q.Keeper.Providers,
			req.Pagination,
			func(_ uint64, provider types.Provider) (bool, error) {
				return provider.Active, nil
			},
			func(_ uint64, provider types.Provider) (types.Provider, error) {
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
		q.Keeper.Providers,
		req.Pagination,
		func(_ uint64, provider types.Provider) (types.Provider, error) {
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

// SKU queries a SKU by its ID.
func (q Querier) SKU(ctx context.Context, req *types.QuerySKURequest) (*types.QuerySKUResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	sku, err := q.Keeper.GetSKU(ctx, req.Id)
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
			q.Keeper.SKUs,
			req.Pagination,
			func(_ uint64, sku types.SKU) (bool, error) {
				return sku.Active, nil
			},
			func(_ uint64, sku types.SKU) (types.SKU, error) {
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
		q.Keeper.SKUs,
		req.Pagination,
		func(_ uint64, sku types.SKU) (types.SKU, error) {
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

// SKUsByProvider queries SKUs by provider ID with pagination.
// Uses the Provider index for efficient lookup - only iterates over SKUs belonging to this provider.
func (q Querier) SKUsByProvider(ctx context.Context, req *types.QuerySKUsByProviderRequest) (*types.QuerySKUsByProviderResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.ProviderId == 0 {
		return nil, status.Error(codes.InvalidArgument, "provider_id cannot be zero")
	}

	// Use the provider index to iterate only over this provider's SKUs
	iter, err := q.Keeper.SKUs.Indexes.Provider.MatchExact(ctx, req.ProviderId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Build filter based on active_only
	var filter func(types.SKU) bool
	if req.ActiveOnly {
		filter = func(s types.SKU) bool { return s.Active }
	}

	skus, pageRes, err := PaginateUint64Index(
		ctx,
		iter,
		q.Keeper.SKUs.Get,
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
