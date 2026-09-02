package keeper

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"

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
	pageReq, err := pagination.BoundedPageRequest(req.Pagination)
	if err != nil {
		return nil, paginationRequestError(err)
	}

	// Use Active index if active_only is set (O(k) instead of O(n))
	if req.ActiveOnly {
		iter, err := pagination.MatchExactWithOrder(ctx, q.k.Providers.Indexes.Active, true, pageReq)
		if err != nil {
			return nil, paginationResultError(err)
		}

		providers, pageRes, err := pagination.PaginateStringIndex(
			ctx,
			iter,
			q.k.Providers.Get,
			pageReq,
			nil, // No additional filter needed - index already filters active
			pagination.WithStringIndexHas(q.k.Providers.Has),
		)
		if err != nil {
			return nil, paginationResultError(err)
		}

		return &types.QueryProvidersResponse{
			Providers:  providers,
			Pagination: pageRes,
		}, nil
	}

	providers, pageRes, err := pagination.CollectionPaginate(
		ctx,
		q.k.Providers,
		pageReq,
		func(_ string, provider types.Provider) (types.Provider, error) {
			return provider, nil
		},
	)
	if err != nil {
		return nil, paginationResultError(err)
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
	pageReq, err := pagination.BoundedPageRequest(req.Pagination)
	if err != nil {
		return nil, paginationRequestError(err)
	}

	// Use Active index if active_only is set (O(k) instead of O(n))
	if req.ActiveOnly {
		iter, err := pagination.MatchExactWithOrder(ctx, q.k.SKUs.Indexes.Active, true, pageReq)
		if err != nil {
			return nil, paginationResultError(err)
		}

		skus, pageRes, err := pagination.PaginateStringIndex(
			ctx,
			iter,
			q.k.SKUs.Get,
			pageReq,
			nil, // No additional filter needed - index already filters active
			pagination.WithStringIndexHas(q.k.SKUs.Has),
		)
		if err != nil {
			return nil, paginationResultError(err)
		}

		return &types.QuerySKUsResponse{
			Skus:       skus,
			Pagination: pageRes,
		}, nil
	}

	skus, pageRes, err := pagination.CollectionPaginate(
		ctx,
		q.k.SKUs,
		pageReq,
		func(_ string, sku types.SKU) (types.SKU, error) {
			return sku, nil
		},
	)
	if err != nil {
		return nil, paginationResultError(err)
	}

	return &types.QuerySKUsResponse{
		Skus:       skus,
		Pagination: pageRes,
	}, nil
}

// SKUsByProvider queries SKUs by provider UUID with pagination.
// Uses the Provider index for efficient lookup - only iterates over SKUs belonging to this provider.
// When active_only is set, uses the compound ProviderActive index for O(k) direct lookup.
func (q Querier) SKUsByProvider(ctx context.Context, req *types.QuerySKUsByProviderRequest) (*types.QuerySKUsByProviderResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.ProviderUuid == "" {
		return nil, status.Error(codes.InvalidArgument, "provider_uuid cannot be empty")
	}
	pageReq, err := pagination.BoundedPageRequest(req.Pagination)
	if err != nil {
		return nil, paginationRequestError(err)
	}

	// Use ProviderActive compound index if active_only is set (O(k) direct lookup)
	if req.ActiveOnly {
		iter, err := pagination.MatchExactWithOrder(ctx, q.k.SKUs.Indexes.ProviderActive, collections.Join(req.ProviderUuid, true), pageReq)
		if err != nil {
			return nil, paginationResultError(err)
		}

		skus, pageRes, err := pagination.PaginateStringIndex(
			ctx,
			iter,
			q.k.SKUs.Get,
			pageReq,
			nil, // No additional filter needed - index already filters by provider and active
			pagination.WithStringIndexHas(q.k.SKUs.Has),
		)
		if err != nil {
			return nil, paginationResultError(err)
		}

		return &types.QuerySKUsByProviderResponse{
			Skus:       skus,
			Pagination: pageRes,
		}, nil
	}

	// Use the provider index to iterate only over this provider's SKUs
	iter, err := pagination.MatchExactWithOrder(ctx, q.k.SKUs.Indexes.Provider, req.ProviderUuid, pageReq)
	if err != nil {
		return nil, paginationResultError(err)
	}

	skus, pageRes, err := pagination.PaginateStringIndex(
		ctx,
		iter,
		q.k.SKUs.Get,
		pageReq,
		nil,
		pagination.WithStringIndexHas(q.k.SKUs.Has),
	)
	if err != nil {
		return nil, paginationResultError(err)
	}

	return &types.QuerySKUsByProviderResponse{
		Skus:       skus,
		Pagination: pageRes,
	}, nil
}

// ProviderByAddress queries all Providers with the given management address.
// Uses the address index for efficient lookup.
func (q Querier) ProviderByAddress(ctx context.Context, req *types.QueryProviderByAddressRequest) (*types.QueryProviderByAddressResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address cannot be empty")
	}

	// Parse the address
	addr, err := sdk.AccAddressFromBech32(req.Address)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid address format")
	}
	pageReq, err := pagination.BoundedPageRequest(req.Pagination)
	if err != nil {
		return nil, paginationRequestError(err)
	}

	// Use the address index to iterate only over providers with this address
	iter, err := pagination.MatchExactWithOrder(ctx, q.k.Providers.Indexes.Address, addr, pageReq)
	if err != nil {
		return nil, paginationResultError(err)
	}

	// Build filter based on active_only
	var filter func(types.Provider) bool
	if req.ActiveOnly {
		filter = func(p types.Provider) bool { return p.Active }
	}

	providers, pageRes, err := pagination.PaginateStringIndex(
		ctx,
		iter,
		q.k.Providers.Get,
		pageReq,
		filter,
		pagination.WithStringIndexHas(q.k.Providers.Has),
	)
	if err != nil {
		return nil, paginationResultError(err)
	}

	return &types.QueryProviderByAddressResponse{
		Providers:  providers,
		Pagination: pageRes,
	}, nil
}

func paginationResultError(err error) error {
	if errors.Is(err, pagination.ErrPaginationScanLimitExceeded) {
		return status.Error(codes.ResourceExhausted, err.Error())
	}
	return status.Error(codes.Internal, err.Error())
}

func paginationRequestError(err error) error {
	if errors.Is(err, pagination.ErrPaginationScanLimitExceeded) {
		return status.Error(codes.ResourceExhausted, err.Error())
	}
	return status.Error(codes.InvalidArgument, err.Error())
}
