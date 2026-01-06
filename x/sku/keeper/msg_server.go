package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/pkg/sanitize"
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

	uuid, err := ms.k.GenerateProviderUUID(ctx)
	if err != nil {
		return nil, err
	}

	provider := types.Provider{
		Uuid:          uuid,
		Address:       req.Address,
		PayoutAddress: req.PayoutAddress,
		MetaHash:      req.MetaHash,
		Active:        true,
		ApiUrl:        req.ApiUrl,
	}

	if err := ms.k.SetProvider(ctx, provider); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeProviderCreated,
			sdk.NewAttribute(types.AttributeKeyProviderUUID, uuid),
			sdk.NewAttribute(types.AttributeKeyAddress, req.Address),
			sdk.NewAttribute(types.AttributeKeyPayoutAddress, req.PayoutAddress),
			sdk.NewAttribute(types.AttributeKeyCreatedBy, req.Authority),
		),
	})

	ms.k.Logger().Info("Provider created", "uuid", uuid, "address", req.Address)

	return &types.MsgCreateProviderResponse{Uuid: uuid}, nil
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

	existingProvider, err := ms.k.GetProvider(ctx, req.Uuid)
	if err != nil {
		return nil, types.ErrProviderNotFound.Wrapf("provider %s not found", req.Uuid)
	}

	wasInactive := !existingProvider.Active

	// Preserve existing api_url if not provided in the update
	apiURL := req.ApiUrl
	if apiURL == "" {
		apiURL = existingProvider.ApiUrl
	}

	provider := types.Provider{
		Uuid:          req.Uuid,
		Address:       req.Address,
		PayoutAddress: req.PayoutAddress,
		MetaHash:      req.MetaHash,
		Active:        req.Active,
		ApiUrl:        apiURL,
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
				sdk.NewAttribute(types.AttributeKeyProviderUUID, req.Uuid),
			),
		})
	}

	sdkCtx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeProviderUpdated,
			sdk.NewAttribute(types.AttributeKeyProviderUUID, req.Uuid),
		),
	})

	ms.k.Logger().Info("Provider updated", "uuid", req.Uuid)

	return &types.MsgUpdateProviderResponse{}, nil
}

// DeactivateProvider deactivates a Provider (soft delete).
//
// IMPORTANT: Deactivating a provider does NOT affect existing active leases.
// Existing leases will continue to accrue charges and the provider can still
// withdraw earned funds. This is by design because:
//   - Lease prices are locked at creation time, providing price stability for tenants
//   - Abruptly closing leases could disrupt tenant services
//   - Tenants can close their own leases if the provider is no longer serving them
//
// Deactivation prevents:
//   - Creation of new SKUs under this provider
//   - Creation of new leases using this provider's SKUs
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

	existingProvider, err := ms.k.GetProvider(ctx, req.Uuid)
	if err != nil {
		return nil, types.ErrProviderNotFound.Wrapf("provider %s not found", req.Uuid)
	}

	if !existingProvider.Active {
		return nil, types.ErrInvalidProvider.Wrapf("provider %s is already inactive", req.Uuid)
	}

	existingProvider.Active = false
	if err := ms.k.SetProvider(ctx, existingProvider); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeProviderDeactivated,
			sdk.NewAttribute(types.AttributeKeyProviderUUID, req.Uuid),
			sdk.NewAttribute(types.AttributeKeyDeactivatedBy, req.Authority),
		),
	})

	ms.k.Logger().Info("Provider deactivated", "uuid", req.Uuid)

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
	provider, err := ms.k.GetProvider(ctx, req.ProviderUuid)
	if err != nil {
		return nil, types.ErrProviderNotFound.Wrapf("provider %s not found", req.ProviderUuid)
	}
	if !provider.Active {
		return nil, types.ErrInvalidProvider.Wrapf("provider %s is not active", req.ProviderUuid)
	}

	uuid, err := ms.k.GenerateSKUUUID(ctx)
	if err != nil {
		return nil, err
	}

	sku := types.SKU{
		Uuid:         uuid,
		ProviderUuid: req.ProviderUuid,
		Name:         req.Name,
		Unit:         req.Unit,
		BasePrice:    req.BasePrice,
		MetaHash:     req.MetaHash,
		Active:       true,
	}

	if err := ms.k.SetSKU(ctx, sku); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	// NOTE: We sanitize the SKU name to prevent log injection attacks.
	// The original name is stored in state but event/logs use sanitized version.
	sanitizedName := sanitize.EventAttribute(req.Name)
	sdkCtx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeSKUCreated,
			sdk.NewAttribute(types.AttributeKeySKUUUID, uuid),
			sdk.NewAttribute(types.AttributeKeyProviderUUID, req.ProviderUuid),
			sdk.NewAttribute(types.AttributeKeyName, sanitizedName),
			sdk.NewAttribute(types.AttributeKeyBasePrice, req.BasePrice.String()),
			sdk.NewAttribute(types.AttributeKeyCreatedBy, req.Authority),
		),
	})

	ms.k.Logger().Info("SKU created", "uuid", uuid, "provider_uuid", req.ProviderUuid, "name", sanitizedName)

	return &types.MsgCreateSKUResponse{Uuid: uuid}, nil
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

	existingSKU, err := ms.k.GetSKU(ctx, req.Uuid)
	if err != nil {
		return nil, types.ErrSKUNotFound.Wrapf("sku %s not found", req.Uuid)
	}

	if existingSKU.ProviderUuid != req.ProviderUuid {
		return nil, types.ErrInvalidSKU.Wrapf("provider_uuid mismatch; expected %s, got %s", existingSKU.ProviderUuid, req.ProviderUuid)
	}

	// Verify provider still exists
	if _, err := ms.k.GetProvider(ctx, existingSKU.ProviderUuid); err != nil {
		return nil, types.ErrProviderNotFound.Wrapf("provider %s not found", existingSKU.ProviderUuid)
	}

	wasInactive := !existingSKU.Active

	sku := types.SKU{
		Uuid:         req.Uuid,
		ProviderUuid: req.ProviderUuid,
		Name:         req.Name,
		Unit:         req.Unit,
		BasePrice:    req.BasePrice,
		MetaHash:     req.MetaHash,
		Active:       req.Active,
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
				sdk.NewAttribute(types.AttributeKeySKUUUID, req.Uuid),
				sdk.NewAttribute(types.AttributeKeyProviderUUID, req.ProviderUuid),
			),
		})
	}

	sdkCtx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeSKUUpdated,
			sdk.NewAttribute(types.AttributeKeySKUUUID, req.Uuid),
			sdk.NewAttribute(types.AttributeKeyProviderUUID, req.ProviderUuid),
		),
	})

	ms.k.Logger().Info("SKU updated", "uuid", req.Uuid, "provider_uuid", req.ProviderUuid)

	return &types.MsgUpdateSKUResponse{}, nil
}

// DeactivateSKU deactivates a SKU (soft delete).
//
// IMPORTANT: Deactivating a SKU does NOT affect existing active leases.
// Existing leases using this SKU will continue to accrue charges at the
// locked-in price. This is by design because:
//   - Lease prices are locked at creation time, providing price stability for tenants
//   - Abruptly closing leases could disrupt tenant services
//   - Tenants can close their own leases if the SKU is no longer being provided
//
// Deactivation prevents:
//   - Creation of new leases using this SKU
//   - This SKU from appearing in active SKU listings
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

	existingSKU, err := ms.k.GetSKU(ctx, req.Uuid)
	if err != nil {
		return nil, types.ErrSKUNotFound.Wrapf("sku %s not found", req.Uuid)
	}

	if !existingSKU.Active {
		return nil, types.ErrInvalidSKU.Wrapf("sku %s is already inactive", req.Uuid)
	}

	existingSKU.Active = false
	if err := ms.k.SetSKU(ctx, existingSKU); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeSKUDeactivated,
			sdk.NewAttribute(types.AttributeKeySKUUUID, req.Uuid),
			sdk.NewAttribute(types.AttributeKeyProviderUUID, existingSKU.ProviderUuid),
			sdk.NewAttribute(types.AttributeKeyDeactivatedBy, req.Authority),
		),
	})

	ms.k.Logger().Info("SKU deactivated", "uuid", req.Uuid, "provider_uuid", existingSKU.ProviderUuid)

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
