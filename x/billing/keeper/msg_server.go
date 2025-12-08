package keeper

import (
	"context"
	"strconv"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
	k Keeper
}

// NewMsgServerImpl returns an implementation of the MsgServer interface.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{k: keeper}
}

// FundCredit funds a tenant's credit account.
func (ms msgServer) FundCredit(ctx context.Context, msg *types.MsgFundCredit) (*types.MsgFundCreditResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	// Validate denom matches billing params
	params, err := ms.k.GetParams(ctx)
	if err != nil {
		return nil, err
	}

	if msg.Amount.Denom != params.Denom {
		return nil, types.ErrInvalidDenom.Wrapf("expected %s, got %s", params.Denom, msg.Amount.Denom)
	}

	// TODO: Implement credit funding logic
	// 1. Transfer tokens from sender to credit address
	// 2. Create/update credit account
	// 3. Emit event

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeCreditFunded,
			sdk.NewAttribute(types.AttributeKeyTenant, msg.Tenant),
			sdk.NewAttribute(types.AttributeKeyAmount, msg.Amount.String()),
		),
	)

	creditAddr, err := types.DeriveCreditAddressFromBech32(msg.Tenant)
	if err != nil {
		return nil, err
	}

	return &types.MsgFundCreditResponse{
		CreditAddress: creditAddr.String(),
		NewBalance:    msg.Amount, // placeholder
	}, nil
}

// CreateLease creates a new lease for the tenant.
func (ms msgServer) CreateLease(ctx context.Context, msg *types.MsgCreateLease) (*types.MsgCreateLeaseResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	// TODO: Implement lease creation logic
	// 1. Verify tenant has sufficient credit balance
	// 2. Verify all SKUs exist, are active, and belong to the same provider
	// 3. Verify tenant hasn't exceeded max leases
	// 4. Lock prices from SKUs
	// 5. Create lease
	// 6. Emit event

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	leaseID, err := ms.k.GetNextLeaseID(ctx)
	if err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeLeaseCreated,
			sdk.NewAttribute(types.AttributeKeyLeaseID, strconv.FormatUint(leaseID, 10)),
			sdk.NewAttribute(types.AttributeKeyTenant, msg.Tenant),
		),
	)

	return &types.MsgCreateLeaseResponse{
		LeaseId: leaseID,
	}, nil
}

// CloseLease closes an active lease.
func (ms msgServer) CloseLease(ctx context.Context, msg *types.MsgCloseLease) (*types.MsgCloseLeaseResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	// TODO: Implement lease closure logic
	// 1. Verify lease exists and is active
	// 2. Verify sender is authorized (tenant, provider, or authority)
	// 3. Settle accrued charges
	// 4. Update lease state to inactive
	// 5. Emit event

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	params, err := ms.k.GetParams(ctx)
	if err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeLeaseClosed,
			sdk.NewAttribute(types.AttributeKeyLeaseID, strconv.FormatUint(msg.LeaseId, 10)),
		),
	)

	return &types.MsgCloseLeaseResponse{
		SettledAmount: sdk.NewCoin(params.Denom, math.ZeroInt()), // placeholder
	}, nil
}

// Withdraw allows a provider to withdraw accrued funds from a specific lease.
func (ms msgServer) Withdraw(ctx context.Context, msg *types.MsgWithdraw) (*types.MsgWithdrawResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	// TODO: Implement withdrawal logic
	// 1. Verify lease exists
	// 2. Calculate accrued amount since last settlement
	// 3. Verify sender is authorized (provider address or authority)
	// 4. Transfer accrued amount from credit account to provider payout address
	// 5. Update last_settled_at
	// 6. Emit event

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	params, err := ms.k.GetParams(ctx)
	if err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeProviderWithdraw,
			sdk.NewAttribute(types.AttributeKeyLeaseID, strconv.FormatUint(msg.LeaseId, 10)),
		),
	)

	return &types.MsgWithdrawResponse{
		Amount:        sdk.NewCoin(params.Denom, math.ZeroInt()), // placeholder
		PayoutAddress: "",                                        // placeholder
	}, nil
}

// WithdrawAll allows a provider to withdraw all accrued funds from all their leases.
func (ms msgServer) WithdrawAll(ctx context.Context, msg *types.MsgWithdrawAll) (*types.MsgWithdrawAllResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	// TODO: Implement withdraw all logic
	// 1. Determine provider_id (from msg or lookup by sender address)
	// 2. Get all leases for provider
	// 3. For each active lease, calculate and withdraw accrued amount
	// 4. Emit event

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	params, err := ms.k.GetParams(ctx)
	if err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeProviderWithdrawAll,
			sdk.NewAttribute(types.AttributeKeyProviderID, strconv.FormatUint(msg.ProviderId, 10)),
		),
	)

	return &types.MsgWithdrawAllResponse{
		TotalAmount:   sdk.NewCoin(params.Denom, math.ZeroInt()), // placeholder
		LeaseCount:    0,                                         // placeholder
		PayoutAddress: "",                                        // placeholder
	}, nil
}

// UpdateParams updates the module parameters.
func (ms msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}

	if ms.k.GetAuthority() != msg.Authority {
		return nil, types.ErrUnauthorized.Wrapf("expected %s, got %s", ms.k.GetAuthority(), msg.Authority)
	}

	if err := ms.k.SetParams(ctx, msg.Params); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeParamsUpdated),
	)

	return &types.MsgUpdateParamsResponse{}, nil
}
