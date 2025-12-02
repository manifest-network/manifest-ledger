package keeper

import (
	"context"
	"fmt"

	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

type msgServer struct {
	k Keeper
}

var _ types.MsgServer = msgServer{}

// NewMsgServerImpl returns an implementation of the module MsgServer interface.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{k: keeper}
}

// CreateSKU creates a new SKU.
func (ms msgServer) CreateSKU(ctx context.Context, req *types.MsgCreateSKU) (*types.MsgCreateSKUResponse, error) {
	if ms.k.authority != req.Authority {
		return nil, fmt.Errorf("invalid authority; expected %s, got %s", ms.k.authority, req.Authority)
	}

	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid create sku message: %w", err)
	}

	id, err := ms.k.GetNextID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get next id: %w", err)
	}

	sku := types.SKU{
		Id:        id,
		Provider:  req.Provider,
		Name:      req.Name,
		Unit:      req.Unit,
		BasePrice: req.BasePrice,
		MetaHash:  req.MetaHash,
		Active:    true,
	}

	if err := ms.k.SetSKU(ctx, sku); err != nil {
		return nil, fmt.Errorf("failed to set sku: %w", err)
	}

	ms.k.Logger().Info("SKU created", "id", id, "provider", req.Provider, "name", req.Name)

	return &types.MsgCreateSKUResponse{Id: id}, nil
}

// UpdateSKU updates an existing SKU.
func (ms msgServer) UpdateSKU(ctx context.Context, req *types.MsgUpdateSKU) (*types.MsgUpdateSKUResponse, error) {
	if ms.k.authority != req.Authority {
		return nil, fmt.Errorf("invalid authority; expected %s, got %s", ms.k.authority, req.Authority)
	}

	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid update sku message: %w", err)
	}

	existingSKU, err := ms.k.GetSKU(ctx, req.Id)
	if err != nil {
		return nil, types.ErrSKUNotFound
	}

	if existingSKU.Provider != req.Provider {
		return nil, fmt.Errorf("provider mismatch; expected %s, got %s", existingSKU.Provider, req.Provider)
	}

	sku := types.SKU{
		Id:        req.Id,
		Provider:  req.Provider,
		Name:      req.Name,
		Unit:      req.Unit,
		BasePrice: req.BasePrice,
		MetaHash:  req.MetaHash,
		Active:    req.Active,
	}

	if err := ms.k.SetSKU(ctx, sku); err != nil {
		return nil, fmt.Errorf("failed to update sku: %w", err)
	}

	ms.k.Logger().Info("SKU updated", "id", req.Id, "provider", req.Provider)

	return &types.MsgUpdateSKUResponse{}, nil
}

// DeleteSKU deletes a SKU.
func (ms msgServer) DeleteSKU(ctx context.Context, req *types.MsgDeleteSKU) (*types.MsgDeleteSKUResponse, error) {
	if ms.k.authority != req.Authority {
		return nil, fmt.Errorf("invalid authority; expected %s, got %s", ms.k.authority, req.Authority)
	}

	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid delete sku message: %w", err)
	}

	existingSKU, err := ms.k.GetSKU(ctx, req.Id)
	if err != nil {
		return nil, types.ErrSKUNotFound
	}

	if existingSKU.Provider != req.Provider {
		return nil, fmt.Errorf("provider mismatch; expected %s, got %s", existingSKU.Provider, req.Provider)
	}

	if err := ms.k.DeleteSKU(ctx, req.Id); err != nil {
		return nil, fmt.Errorf("failed to delete sku: %w", err)
	}

	ms.k.Logger().Info("SKU deleted", "id", req.Id, "provider", req.Provider)

	return &types.MsgDeleteSKUResponse{}, nil
}
