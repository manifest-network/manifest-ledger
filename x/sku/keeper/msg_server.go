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

// CreateProvider creates a new Provider.
func (ms msgServer) CreateProvider(ctx context.Context, req *types.MsgCreateProvider) (*types.MsgCreateProviderResponse, error) {
	authorized, err := ms.isAuthorizedSender(ctx, req.Authority)
	if err != nil {
		return nil, types.ErrUnauthorized.Wrapf("failed to check authorization: %s", err)
	}
	if !authorized {
		return nil, types.ErrUnauthorized.Wrapf("%s is not the authority or in the allowed list", req.Authority)
	}

	if err := req.Validate(); err != nil {
		return nil, types.ErrInvalidProvider.Wrapf("invalid create provider message: %s", err)
	}

	id, err := ms.k.GetNextProviderID(ctx)
	if err != nil {
		return nil, err
	}

	provider := types.Provider{
		Id:            id,
		Address:       req.Address,
		PayoutAddress: req.PayoutAddress,
		MetaHash:      req.MetaHash,
		Active:        true,
	}

	if err := ms.k.SetProvider(ctx, provider); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeProviderCreated,
			sdk.NewAttribute(types.AttributeKeyProviderID, strconv.FormatUint(id, 10)),
		),
	})

	ms.k.Logger().Info("Provider created", "id", id, "address", req.Address)

	return &types.MsgCreateProviderResponse{Id: id}, nil
}

// UpdateProvider updates an existing Provider.
func (ms msgServer) UpdateProvider(ctx context.Context, req *types.MsgUpdateProvider) (*types.MsgUpdateProviderResponse, error) {
	authorized, err := ms.isAuthorizedSender(ctx, req.Authority)
	if err != nil {
		return nil, types.ErrUnauthorized.Wrapf("failed to check authorization: %s", err)
	}
	if !authorized {
		return nil, types.ErrUnauthorized.Wrapf("%s is not the authority or in the allowed list", req.Authority)
	}

	if err := req.Validate(); err != nil {
		return nil, types.ErrInvalidProvider.Wrapf("invalid update provider message: %s", err)
	}

	existingProvider, err := ms.k.GetProvider(ctx, req.Id)
	if err != nil {
		return nil, types.ErrProviderNotFound.Wrapf("provider %d not found", req.Id)
	}

	wasInactive := !existingProvider.Active

	provider := types.Provider{
		Id:            req.Id,
		Address:       req.Address,
		PayoutAddress: req.PayoutAddress,
		MetaHash:      req.MetaHash,
		Active:        req.Active,
	}

	if err := ms.k.SetProvider(ctx, provider); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Emit activated event if transitioning from inactive to active
	if wasInactive && req.Active {
		sdkCtx.EventManager().EmitEvents(sdk.Events{
			sdk.NewEvent(
				types.EventTypeProviderActivated,
				sdk.NewAttribute(types.AttributeKeyProviderID, strconv.FormatUint(req.Id, 10)),
			),
		})
	}

	sdkCtx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeProviderUpdated,
			sdk.NewAttribute(types.AttributeKeyProviderID, strconv.FormatUint(req.Id, 10)),
		),
	})

	ms.k.Logger().Info("Provider updated", "id", req.Id)

	return &types.MsgUpdateProviderResponse{}, nil
}

// DeactivateProvider deactivates a Provider (soft delete).
func (ms msgServer) DeactivateProvider(ctx context.Context, req *types.MsgDeactivateProvider) (*types.MsgDeactivateProviderResponse, error) {
	authorized, err := ms.isAuthorizedSender(ctx, req.Authority)
	if err != nil {
		return nil, types.ErrUnauthorized.Wrapf("failed to check authorization: %s", err)
	}
	if !authorized {
		return nil, types.ErrUnauthorized.Wrapf("%s is not the authority or in the allowed list", req.Authority)
	}

	if err := req.Validate(); err != nil {
		return nil, types.ErrInvalidProvider.Wrapf("invalid deactivate provider message: %s", err)
	}

	existingProvider, err := ms.k.GetProvider(ctx, req.Id)
	if err != nil {
		return nil, types.ErrProviderNotFound.Wrapf("provider %d not found", req.Id)
	}

	if !existingProvider.Active {
		return nil, types.ErrInvalidProvider.Wrapf("provider %d is already inactive", req.Id)
	}

	existingProvider.Active = false
	if err := ms.k.SetProvider(ctx, existingProvider); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeProviderDeactivated,
			sdk.NewAttribute(types.AttributeKeyProviderID, strconv.FormatUint(req.Id, 10)),
		),
	})

	ms.k.Logger().Info("Provider deactivated", "id", req.Id)

	return &types.MsgDeactivateProviderResponse{}, nil
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

	// Verify provider exists and is active
	provider, err := ms.k.GetProvider(ctx, req.ProviderId)
	if err != nil {
		return nil, types.ErrProviderNotFound.Wrapf("provider %d not found", req.ProviderId)
	}
	if !provider.Active {
		return nil, types.ErrInvalidProvider.Wrapf("provider %d is not active", req.ProviderId)
	}

	id, err := ms.k.GetNextSKUID(ctx)
	if err != nil {
		return nil, err
	}

	sku := types.SKU{
		Id:         id,
		ProviderId: req.ProviderId,
		Name:       req.Name,
		Unit:       req.Unit,
		BasePrice:  req.BasePrice,
		MetaHash:   req.MetaHash,
		Active:     true,
	}

	if err := ms.k.SetSKU(ctx, sku); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeSKUCreated,
			sdk.NewAttribute(types.AttributeKeySKUID, strconv.FormatUint(id, 10)),
			sdk.NewAttribute(types.AttributeKeyProviderID, strconv.FormatUint(req.ProviderId, 10)),
			sdk.NewAttribute(types.AttributeKeyName, req.Name),
		),
	})

	ms.k.Logger().Info("SKU created", "id", id, "provider_id", req.ProviderId, "name", req.Name)

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

	if existingSKU.ProviderId != req.ProviderId {
		return nil, types.ErrInvalidSKU.Wrapf("provider_id mismatch; expected %d, got %d", existingSKU.ProviderId, req.ProviderId)
	}

	wasInactive := !existingSKU.Active

	sku := types.SKU{
		Id:         req.Id,
		ProviderId: req.ProviderId,
		Name:       req.Name,
		Unit:       req.Unit,
		BasePrice:  req.BasePrice,
		MetaHash:   req.MetaHash,
		Active:     req.Active,
	}

	if err := ms.k.SetSKU(ctx, sku); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Emit activated event if transitioning from inactive to active
	if wasInactive && req.Active {
		sdkCtx.EventManager().EmitEvents(sdk.Events{
			sdk.NewEvent(
				types.EventTypeSKUActivated,
				sdk.NewAttribute(types.AttributeKeySKUID, strconv.FormatUint(req.Id, 10)),
				sdk.NewAttribute(types.AttributeKeyProviderID, strconv.FormatUint(req.ProviderId, 10)),
			),
		})
	}

	sdkCtx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeSKUUpdated,
			sdk.NewAttribute(types.AttributeKeySKUID, strconv.FormatUint(req.Id, 10)),
			sdk.NewAttribute(types.AttributeKeyProviderID, strconv.FormatUint(req.ProviderId, 10)),
		),
	})

	ms.k.Logger().Info("SKU updated", "id", req.Id, "provider_id", req.ProviderId)

	return &types.MsgUpdateSKUResponse{}, nil
}

// DeactivateSKU deactivates a SKU (soft delete).
func (ms msgServer) DeactivateSKU(ctx context.Context, req *types.MsgDeactivateSKU) (*types.MsgDeactivateSKUResponse, error) {
	authorized, err := ms.isAuthorizedSender(ctx, req.Authority)
	if err != nil {
		return nil, types.ErrUnauthorized.Wrapf("failed to check authorization: %s", err)
	}
	if !authorized {
		return nil, types.ErrUnauthorized.Wrapf("%s is not the authority or in the allowed list", req.Authority)
	}

	if err := req.Validate(); err != nil {
		return nil, types.ErrInvalidSKU.Wrapf("invalid deactivate sku message: %s", err)
	}

	existingSKU, err := ms.k.GetSKU(ctx, req.Id)
	if err != nil {
		return nil, types.ErrSKUNotFound.Wrapf("sku %d not found", req.Id)
	}

	if !existingSKU.Active {
		return nil, types.ErrInvalidSKU.Wrapf("sku %d is already inactive", req.Id)
	}

	existingSKU.Active = false
	if err := ms.k.SetSKU(ctx, existingSKU); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeSKUDeactivated,
			sdk.NewAttribute(types.AttributeKeySKUID, strconv.FormatUint(req.Id, 10)),
			sdk.NewAttribute(types.AttributeKeyProviderID, strconv.FormatUint(existingSKU.ProviderId, 10)),
		),
	})

	ms.k.Logger().Info("SKU deactivated", "id", req.Id, "provider_id", existingSKU.ProviderId)

	return &types.MsgDeactivateSKUResponse{}, nil
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
