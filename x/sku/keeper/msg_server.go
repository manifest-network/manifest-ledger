package keeper

import (
	"context"
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"

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

// isAuthorizedSender checks if the sender is the authority or in the allowed list.
func (ms msgServer) isAuthorizedSender(ctx context.Context, sender string) (bool, error) {
	if ms.k.authority == sender {
		return true, nil
	}
	params, err := ms.k.GetParams(ctx)
	if err != nil {
		return false, err
	}
	return params.IsAllowed(sender), nil
}

// CreateSKU creates a new SKU.
func (ms msgServer) CreateSKU(ctx context.Context, req *types.MsgCreateSKU) (*types.MsgCreateSKUResponse, error) {
	authorized, err := ms.isAuthorizedSender(ctx, req.Authority)
	if err != nil {
		return nil, types.ErrUnauthorized.Wrapf("failed to check authorization: %s", err)
	}
	if !authorized {
		return nil, types.ErrUnauthorized.Wrapf("%s is not the authority or in the allowed list", req.Authority)
	}

	if err := req.Validate(); err != nil {
		return nil, types.ErrInvalidSKU.Wrapf("invalid create sku message: %s", err)
	}

	id, err := ms.k.GetNextID(ctx)
	if err != nil {
		return nil, err
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
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeSKUCreated,
			sdk.NewAttribute(types.AttributeKeySKUID, strconv.FormatUint(id, 10)),
			sdk.NewAttribute(types.AttributeKeyProvider, req.Provider),
			sdk.NewAttribute(types.AttributeKeyName, req.Name),
		),
	})

	ms.k.Logger().Info("SKU created", "id", id, "provider", req.Provider, "name", req.Name)

	return &types.MsgCreateSKUResponse{Id: id}, nil
}

// UpdateSKU updates an existing SKU.
func (ms msgServer) UpdateSKU(ctx context.Context, req *types.MsgUpdateSKU) (*types.MsgUpdateSKUResponse, error) {
	authorized, err := ms.isAuthorizedSender(ctx, req.Authority)
	if err != nil {
		return nil, types.ErrUnauthorized.Wrapf("failed to check authorization: %s", err)
	}
	if !authorized {
		return nil, types.ErrUnauthorized.Wrapf("%s is not the authority or in the allowed list", req.Authority)
	}

	if err := req.Validate(); err != nil {
		return nil, types.ErrInvalidSKU.Wrapf("invalid update sku message: %s", err)
	}

	existingSKU, err := ms.k.GetSKU(ctx, req.Id)
	if err != nil {
		return nil, types.ErrSKUNotFound.Wrapf("sku %d not found", req.Id)
	}

	if existingSKU.Provider != req.Provider {
		return nil, types.ErrInvalidSKU.Wrapf("provider mismatch; expected %s, got %s", existingSKU.Provider, req.Provider)
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
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeSKUUpdated,
			sdk.NewAttribute(types.AttributeKeySKUID, strconv.FormatUint(req.Id, 10)),
			sdk.NewAttribute(types.AttributeKeyProvider, req.Provider),
		),
	})

	ms.k.Logger().Info("SKU updated", "id", req.Id, "provider", req.Provider)

	return &types.MsgUpdateSKUResponse{}, nil
}

// DeleteSKU deletes a SKU.
func (ms msgServer) DeleteSKU(ctx context.Context, req *types.MsgDeleteSKU) (*types.MsgDeleteSKUResponse, error) {
	authorized, err := ms.isAuthorizedSender(ctx, req.Authority)
	if err != nil {
		return nil, types.ErrUnauthorized.Wrapf("failed to check authorization: %s", err)
	}
	if !authorized {
		return nil, types.ErrUnauthorized.Wrapf("%s is not the authority or in the allowed list", req.Authority)
	}

	if err := req.Validate(); err != nil {
		return nil, types.ErrInvalidSKU.Wrapf("invalid delete sku message: %s", err)
	}

	existingSKU, err := ms.k.GetSKU(ctx, req.Id)
	if err != nil {
		return nil, types.ErrSKUNotFound.Wrapf("sku %d not found", req.Id)
	}

	if existingSKU.Provider != req.Provider {
		return nil, types.ErrInvalidSKU.Wrapf("provider mismatch; expected %s, got %s", existingSKU.Provider, req.Provider)
	}

	if err := ms.k.DeleteSKU(ctx, req.Id); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeSKUDeleted,
			sdk.NewAttribute(types.AttributeKeySKUID, strconv.FormatUint(req.Id, 10)),
			sdk.NewAttribute(types.AttributeKeyProvider, req.Provider),
		),
	})

	ms.k.Logger().Info("SKU deleted", "id", req.Id, "provider", req.Provider)

	return &types.MsgDeleteSKUResponse{}, nil
}

// UpdateParams updates the module parameters.
func (ms msgServer) UpdateParams(ctx context.Context, req *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if ms.k.authority != req.Authority {
		return nil, types.ErrUnauthorized.Wrapf("expected %s, got %s", ms.k.authority, req.Authority)
	}

	if err := req.Validate(); err != nil {
		return nil, types.ErrInvalidConfig.Wrapf("invalid params: %s", err)
	}

	if err := ms.k.SetParams(ctx, req.Params); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(types.EventTypeParamsUpdated),
	})

	ms.k.Logger().Info("Params updated")

	return &types.MsgUpdateParamsResponse{}, nil
}
