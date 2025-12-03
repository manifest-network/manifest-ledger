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

// SKUsByProvider queries SKUs by provider with pagination.
func (q Querier) SKUsByProvider(ctx context.Context, req *types.QuerySKUsByProviderRequest) (*types.QuerySKUsByProviderResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Provider == "" {
		return nil, status.Error(codes.InvalidArgument, "provider cannot be empty")
	}

	skus, pageRes, err := query.CollectionFilteredPaginate(
		ctx,
		q.Keeper.SKUs,
		req.Pagination,
		func(_ uint64, sku types.SKU) (bool, error) {
			return sku.Provider == req.Provider, nil
		},
		func(_ uint64, sku types.SKU) (types.SKU, error) {
			return sku, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QuerySKUsByProviderResponse{
		Skus:       skus,
		Pagination: pageRes,
	}, nil
}
