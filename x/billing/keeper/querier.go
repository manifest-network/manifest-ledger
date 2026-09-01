package keeper

import (
	"context"
	"errors"
	"math"
	"slices"
	"strings"

	"github.com/spf13/cast"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/manifest-network/manifest-ledger/pkg/pagination"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

var _ types.QueryServer = Querier{}

// Querier implements the Query gRPC service.
type Querier struct {
	k Keeper
}

// NewQuerier returns a new Querier instance.
func NewQuerier(keeper Keeper) Querier {
	return Querier{k: keeper}
}

// Params queries the module parameters.
func (q Querier) Params(ctx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	params, err := q.k.GetParams(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryParamsResponse{Params: params}, nil
}

// Lease queries a lease by ID.
func (q Querier) Lease(ctx context.Context, req *types.QueryLeaseRequest) (*types.QueryLeaseResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.LeaseUuid == "" {
		return nil, status.Error(codes.InvalidArgument, "lease_uuid cannot be empty")
	}

	// Use simple GetLease for queries - auto-close only happens during transactions
	lease, err := q.k.GetLease(ctx, req.LeaseUuid)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	return &types.QueryLeaseResponse{Lease: lease}, nil
}

// Leases queries all leases with pagination.
func (q Querier) Leases(ctx context.Context, req *types.QueryLeasesRequest) (*types.QueryLeasesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	pageReq, err := pagination.CursorPageRequest(req.Pagination)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// Use state index for efficient lookup when filtering by state
	if req.StateFilter != types.LEASE_STATE_UNSPECIFIED {
		iter, err := pagination.MatchExactWithOrder(ctx, q.k.Leases.Indexes.State, int32(req.StateFilter), pageReq)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		leases, pageRes, err := pagination.PaginateStringIndex(
			ctx,
			iter,
			q.k.Leases.Get,
			pageReq,
			nil, // No additional filter needed since we already used the state index
		)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		return &types.QueryLeasesResponse{
			Leases:     leases,
			Pagination: pageRes,
		}, nil
	}

	leases, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Leases,
		pageReq,
		func(_ string, lease types.Lease) (types.Lease, error) {
			return lease, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryLeasesResponse{
		Leases:     leases,
		Pagination: pageRes,
	}, nil
}

// LeasesByTenant queries leases by tenant address.
// Uses the compound (tenant, state) index when state filter is provided for O(1) lookup.
// Falls back to Tenant index when no state filter is provided.
func (q Querier) LeasesByTenant(ctx context.Context, req *types.QueryLeasesByTenantRequest) (*types.QueryLeasesByTenantResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.Tenant == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant cannot be empty")
	}

	tenantAddr, err := sdk.AccAddressFromBech32(req.Tenant)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid tenant address")
	}
	pageReq, err := pagination.CursorPageRequest(req.Pagination)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// Use compound index when state filter is provided - O(1) direct lookup
	if req.StateFilter != types.LEASE_STATE_UNSPECIFIED {
		key := collections.Join(tenantAddr, int32(req.StateFilter))
		iter, err := pagination.MatchExactWithOrder(ctx, q.k.Leases.Indexes.TenantState, key, pageReq)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		leases, pageRes, err := pagination.PaginateStringIndex(
			ctx,
			iter,
			q.k.Leases.Get,
			pageReq,
			nil, // No filter needed - compound index already filtered by state
		)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		return &types.QueryLeasesByTenantResponse{
			Leases:     leases,
			Pagination: pageRes,
		}, nil
	}

	// Use tenant index when no state filter - iterate all tenant's leases
	iter, err := pagination.MatchExactWithOrder(ctx, q.k.Leases.Indexes.Tenant, tenantAddr, pageReq)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	leases, pageRes, err := pagination.PaginateStringIndex(
		ctx,
		iter,
		q.k.Leases.Get,
		pageReq,
		nil,
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryLeasesByTenantResponse{
		Leases:     leases,
		Pagination: pageRes,
	}, nil
}

// LeasesByProvider queries leases by provider ID.
// Uses the compound (provider, state) index when state filter is provided for O(1) lookup.
// Falls back to Provider index when no state filter is provided.
func (q Querier) LeasesByProvider(ctx context.Context, req *types.QueryLeasesByProviderRequest) (*types.QueryLeasesByProviderResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.ProviderUuid == "" {
		return nil, status.Error(codes.InvalidArgument, "provider_uuid cannot be empty")
	}
	pageReq, err := pagination.CursorPageRequest(req.Pagination)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// Use compound index when state filter is provided - O(1) direct lookup
	if req.StateFilter != types.LEASE_STATE_UNSPECIFIED {
		key := collections.Join(req.ProviderUuid, int32(req.StateFilter))
		iter, err := pagination.MatchExactWithOrder(ctx, q.k.Leases.Indexes.ProviderState, key, pageReq)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		leases, pageRes, err := pagination.PaginateStringIndex(
			ctx,
			iter,
			q.k.Leases.Get,
			pageReq,
			nil, // No filter needed - compound index already filtered by state
		)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		return &types.QueryLeasesByProviderResponse{
			Leases:     leases,
			Pagination: pageRes,
		}, nil
	}

	// Use provider index when no state filter - iterate all provider's leases
	iter, err := pagination.MatchExactWithOrder(ctx, q.k.Leases.Indexes.Provider, req.ProviderUuid, pageReq)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	leases, pageRes, err := pagination.PaginateStringIndex(
		ctx,
		iter,
		q.k.Leases.Get,
		pageReq,
		nil,
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryLeasesByProviderResponse{
		Leases:     leases,
		Pagination: pageRes,
	}, nil
}

// CreditAccount queries a tenant's credit account.
func (q Querier) CreditAccount(ctx context.Context, req *types.QueryCreditAccountRequest) (*types.QueryCreditAccountResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.Tenant == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant cannot be empty")
	}

	if _, err := sdk.AccAddressFromBech32(req.Tenant); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid tenant address")
	}

	ca, err := q.k.GetCreditAccount(ctx, req.Tenant)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	pageReq := query.PageRequest{}
	if req.Pagination != nil {
		pageReq = *req.Pagination
	}
	if pageReq.Offset != 0 {
		return nil, status.Error(codes.InvalidArgument, "credit-account supports cursor pagination only; offset must be zero")
	}
	if pageReq.CountTotal {
		return nil, status.Error(codes.InvalidArgument, "credit-account does not support count_total")
	}
	if pageReq.Limit == 0 {
		pageReq.Limit = types.DefaultCreditAccountBalanceQueryLimit
	}
	if pageReq.Limit > types.MaxCreditAccountBalanceQueryLimit {
		pageReq.Limit = types.MaxCreditAccountBalanceQueryLimit
	}

	creditAddr, err := types.DeriveCreditAddressFromBech32(req.Tenant)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	bankResponse, err := q.k.bankKeeper.AllBalances(ctx, &banktypes.QueryAllBalancesRequest{
		Address:    creditAddr.String(),
		Pagination: &pageReq,
	})
	if err != nil {
		return nil, err
	}

	// bank's reverse pagination preserves store order and therefore returns a
	// descending coin slice. GetAvailableCredit operates on canonical ascending
	// sdk.Coins, so normalize only the page view and restore the requested order
	// in the response. The bank response itself remains untouched.
	availablePage := bankResponse.Balances
	if pageReq.Reverse {
		availablePage = slices.Clone(availablePage)
		slices.Reverse(availablePage)
	}
	availableBalances := types.GetAvailableCredit(availablePage, ca.ReservedAmounts)
	if pageReq.Reverse {
		slices.Reverse(availableBalances)
	}

	return &types.QueryCreditAccountResponse{
		CreditAccount:     ca,
		Balances:          bankResponse.Balances,
		AvailableBalances: availableBalances,
		Pagination:        bankResponse.Pagination,
	}, nil
}

// CreditAddress derives the credit address for a tenant.
func (q Querier) CreditAddress(_ context.Context, req *types.QueryCreditAddressRequest) (*types.QueryCreditAddressResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.Tenant == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant cannot be empty")
	}

	creditAddr, err := types.DeriveCreditAddressFromBech32(req.Tenant)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid tenant address")
	}

	return &types.QueryCreditAddressResponse{CreditAddress: creditAddr.String()}, nil
}

// WithdrawableAmount queries the amounts available for provider withdrawal from a lease.
func (q Querier) WithdrawableAmount(ctx context.Context, req *types.QueryWithdrawableAmountRequest) (*types.QueryWithdrawableAmountResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.LeaseUuid == "" {
		return nil, status.Error(codes.InvalidArgument, "lease_uuid cannot be empty")
	}

	// Use simple GetLease for queries - auto-close only happens during transactions
	lease, err := q.k.GetLease(ctx, req.LeaseUuid)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	// Calculate withdrawable amounts based on accrual since last settlement
	withdrawableAmounts, err := q.k.CalculateWithdrawableForLease(ctx, lease)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryWithdrawableAmountResponse{
		Amounts: withdrawableAmounts,
	}, nil
}

// ProviderWithdrawable estimates a provider withdrawal for one page of ACTIVE
// leases. Leases are dry-run in the returned index order against page-local,
// canonical-tenant snapshots, so leases in the same page cannot each count the
// same unreserved credit. A page is an execution estimate, not an additive part
// of a provider-wide snapshot: clients should submit the corresponding withdraw
// and re-query before processing another page because pages can share balances.
// Per-lease failures are discarded and skipped, matching provider-wide
// withdrawal's best-effort execution contract.
// The page size defaults to DefaultProviderWithdrawableQueryLimit and is capped
// at MaxProviderWithdrawableQueryLimit to bound query cost.
func (q Querier) ProviderWithdrawable(ctx context.Context, req *types.QueryProviderWithdrawableRequest) (*types.QueryProviderWithdrawableResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.ProviderUuid == "" {
		return nil, status.Error(codes.InvalidArgument, "provider_uuid cannot be empty")
	}
	provider, err := q.k.skuKeeper.GetProvider(ctx, req.ProviderUuid)
	if err != nil {
		return nil, status.Error(codes.NotFound, types.ErrProviderNotFound.Wrapf(
			"provider_uuid %s not found", req.ProviderUuid,
		).Error())
	}
	if _, err := sdk.AccAddressFromBech32(provider.GetPayoutAddress()); err != nil {
		return nil, status.Error(codes.Internal, types.ErrProviderNotFound.Wrapf(
			"provider %s has invalid payout address: %s", req.ProviderUuid, err,
		).Error())
	}

	// Normalize pagination: default and cap the page limit to bound query cost,
	// preserving any cursor/order the caller supplied.
	pageReq := query.PageRequest{}
	if req.Pagination != nil {
		pageReq = *req.Pagination
	}
	if pageReq.Offset != 0 {
		return nil, status.Error(codes.InvalidArgument, "provider-withdrawable supports cursor pagination only; offset must be zero")
	}
	if pageReq.CountTotal {
		return nil, status.Error(codes.InvalidArgument, "provider-withdrawable does not support count_total")
	}
	if pageReq.Limit == 0 {
		pageReq.Limit = types.DefaultProviderWithdrawableQueryLimit
	}
	if pageReq.Limit > types.MaxProviderWithdrawableQueryLimit {
		pageReq.Limit = types.MaxProviderWithdrawableQueryLimit
	}

	// Iterate ACTIVE leases only via the compound (provider, state) index,
	// reusing the same index-pagination helpers as LeasesByProvider.
	key := collections.Join(req.ProviderUuid, int32(types.LEASE_STATE_ACTIVE))
	iter, err := pagination.MatchExactWithOrder(ctx, q.k.Leases.Indexes.ProviderState, key, &pageReq)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	leases, pageRes, err := pagination.PaginateStringIndex(ctx, iter, q.k.Leases.Get, &pageReq, nil)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Run the page through the real lifecycle against a discarded cache. This
	// is read-only at the query boundary and keeps estimates aligned with future
	// settlement/release changes without duplicating consensus accounting.
	simulationCtx, _ := sdk.UnwrapSDKContext(ctx).CacheContext()
	transferCoins := make([]sdk.Coin, 0, len(leases))
	failedLeaseUUIDs := make([]string, 0)
	var leaseCount uint64
	for i := range leases {
		// Match provider-wide withdrawal's best-effort contract: each lease gets
		// its own nested cache, failures are discarded, and successful effects
		// are committed into the shared page simulation for subsequent leases.
		leaseCtx, commitLease := simulationCtx.CacheContext()
		lease := leases[i]
		result, err := q.k.executeProviderLeaseWithdrawal(leaseCtx, &lease)
		if err != nil {
			failedLeaseUUIDs = append(failedLeaseUUIDs, lease.Uuid)
			continue
		}
		if !result.counted {
			continue
		}

		commitLease()
		transferCoins = append(transferCoins, result.transferAmounts...)
		leaseCount++
	}
	totalWithdrawable, err := types.SafeAggregateCoins(transferCoins)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryProviderWithdrawableResponse{
		Amounts:          totalWithdrawable,
		LeaseCount:       leaseCount,
		Pagination:       pageRes,
		FailedLeaseUuids: failedLeaseUUIDs,
	}, nil
}

// CreditAccounts queries all credit accounts with pagination.
func (q Querier) CreditAccounts(ctx context.Context, req *types.QueryCreditAccountsRequest) (*types.QueryCreditAccountsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}
	pageReq, err := pagination.CursorPageRequest(req.Pagination)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	creditAccounts, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.CreditAccounts,
		pageReq,
		func(_ sdk.AccAddress, ca types.CreditAccount) (types.CreditAccount, error) {
			return ca, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryCreditAccountsResponse{
		CreditAccounts: creditAccounts,
		Pagination:     pageRes,
	}, nil
}

// LeasesBySKU queries leases by SKU UUID.
// Uses the LeaseBySKUIndex for efficient O(k) lookup where k = leases containing the SKU.
func (q Querier) LeasesBySKU(ctx context.Context, req *types.QueryLeasesBySKURequest) (*types.QueryLeasesBySKUResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.SkuUuid == "" {
		return nil, status.Error(codes.InvalidArgument, "sku_uuid cannot be empty")
	}

	pageReq, err := pagination.CursorPageRequest(req.Pagination)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// Use the SKU index to iterate only over leases containing this SKU.
	rng := collections.NewPrefixedPairRange[string, string](req.SkuUuid)
	if pageReq != nil {
		// Resume by a store-level seek on the cursor (deletion-tolerant, O(log n)),
		// mirroring pkg/pagination.MatchExactWithOrder. collections.PairRange's Start*
		// binds the byte-order lower bound regardless of Descending(), so reverse
		// resume must bound the upper end (EndInclusive) to land inclusively on the
		// cursor and iterate downward. Inclusive bounds keep next_key wire-compatible.
		if len(pageReq.Key) > 0 {
			cursor := string(pageReq.Key)
			if pageReq.Reverse {
				rng = rng.EndInclusive(cursor)
			} else {
				rng = rng.StartInclusive(cursor)
			}
		}
		if pageReq.Reverse {
			rng = rng.Descending()
		}
	}
	iter, err := q.k.LeaseBySKUIndex.Iterate(ctx, rng)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Build filter based on state_filter
	var filter func(types.Lease) bool
	if req.StateFilter != types.LEASE_STATE_UNSPECIFIED {
		filter = func(l types.Lease) bool { return l.State == req.StateFilter }
	}

	// Custom pagination over the SKU index
	leases, pageRes, err := paginateSKUIndex(
		ctx,
		iter,
		q.k.Leases.Get,
		pageReq,
		filter,
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryLeasesBySKUResponse{
		Leases:     leases,
		Pagination: pageRes,
	}, nil
}

// CreditEstimate reports gross bank-balance runway at the tenant's current
// aggregate ACTIVE rate. It is not a reservation-aware lifecycle forecast.
func (q Querier) CreditEstimate(ctx context.Context, req *types.QueryCreditEstimateRequest) (response *types.QueryCreditEstimateResponse, err error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.Tenant == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant cannot be empty")
	}

	tenantAddr, err := sdk.AccAddressFromBech32(req.Tenant)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid tenant address")
	}

	// Verify the credit account exists and retain its aggregate count. The stored
	// count remains authoritative when governance lowers the current lease limit.
	creditAccount, err := q.k.GetCreditAccount(ctx, req.Tenant)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	// Historical state can exceed the current governance parameter, but not the
	// fixed v2 state bound. Refuse malformed state rather than returning a partial
	// estimate from a silently truncated scan.
	if creditAccount.ActiveLeaseCount > types.MaxActiveLeasesPerTenantStateUpperBound {
		return nil, status.Error(codes.ResourceExhausted,
			types.ErrLeaseQueryLimitExceeded.Wrapf(
				"tenant %s has %d active leases; unpaginated query ceiling is %d",
				creditAccount.Tenant,
				creditAccount.ActiveLeaseCount,
				types.MaxActiveLeasesPerTenantStateUpperBound,
			).Error(),
		)
	}

	// Calculate total rate per second across all active leases.
	// Also collect relevant denoms for per-denom balance queries (DoS mitigation).
	rateCoins := make([]sdk.Coin, 0, 4)
	var activeLeaseCount uint64
	var leaseItemCount uint64
	denomSet := make(map[string]struct{})
	denoms := make([]string, 0, 4)

	// Use TenantState compound index to iterate only over active leases - O(k) instead of O(n)
	key := collections.Join(tenantAddr, int32(types.LEASE_STATE_ACTIVE))
	iter, err := q.k.Leases.Indexes.TenantState.MatchExact(ctx, key)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer func() {
		if closeErr := iter.Close(); closeErr != nil && err == nil {
			response = nil
			err = status.Error(codes.Internal, closeErr.Error())
		}
	}()

	for ; activeLeaseCount < creditAccount.ActiveLeaseCount && iter.Valid(); iter.Next() {
		activeLeaseCount++

		leaseUUID, err := iter.PrimaryKey()
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		lease, err := q.k.Leases.Get(ctx, leaseUUID)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		leaseItemCount, err = checkedCreditEstimateItemCount(
			creditAccount.Tenant,
			leaseItemCount,
			len(lease.Items),
		)
		if err != nil {
			return nil, err
		}

		// Sum up rates for all items in this lease
		for _, item := range lease.Items {
			if err := types.ValidateLeaseItemPricing(item.LockedPrice, item.Quantity); err != nil {
				return nil, status.Error(codes.Internal, errorsmod.Wrapf(
					err,
					"validate accrual input for lease %s sku %s",
					lease.Uuid,
					item.SkuUuid,
				).Error())
			}
			// Rate per second = locked_price * quantity
			// locked_price is already in per-second terms
			itemRate, err := types.SafeMultiplyCoin(item.LockedPrice, sdkmath.NewIntFromUint64(item.Quantity))
			if err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
			rateCoins = append(rateCoins, itemRate)
			if _, ok := denomSet[item.LockedPrice.Denom]; !ok {
				denomSet[item.LockedPrice.Denom] = struct{}{}
				denoms = append(denoms, item.LockedPrice.Denom)
			}
		}
	}
	if activeLeaseCount != creditAccount.ActiveLeaseCount || iter.Valid() {
		return nil, status.Error(codes.Internal, types.ErrReservationInvariant.Wrapf(
			"tenant %s account reports %d active leases but its index does not contain exactly that count",
			creditAccount.Tenant, creditAccount.ActiveLeaseCount,
		).Error())
	}
	totalRatePerSecond, err := types.SafeAggregateCoins(rateCoins)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Fetch balances for only the denoms used by active leases (DoS mitigation).
	currentBalance, err := q.k.getCreditBalancesForDenoms(ctx, req.Tenant, denoms)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Calculate estimated duration
	// Find minimum duration across all denoms: min(balance[denom] / rate[denom])
	var estimatedDurationSeconds uint64
	if activeLeaseCount > 0 && !totalRatePerSecond.IsZero() {
		// Start with max uint64, then find minimum
		estimatedDurationSeconds = math.MaxUint64
		foundRate := false
		balanceIndex := 0

		for _, rateCoin := range totalRatePerSecond {
			if rateCoin.Amount.IsZero() {
				continue
			}
			foundRate = true
			for balanceIndex < len(currentBalance) && currentBalance[balanceIndex].Denom < rateCoin.Denom {
				balanceIndex++
			}
			balanceAmount := sdkmath.ZeroInt()
			if balanceIndex < len(currentBalance) && currentBalance[balanceIndex].Denom == rateCoin.Denom {
				balanceAmount = currentBalance[balanceIndex].Amount
			}
			if balanceAmount.IsZero() {
				// No balance for this denom means immediate exhaustion
				estimatedDurationSeconds = 0
				break
			}
			// Duration = balance / rate (integer division, rounds down)
			quotient := balanceAmount.Quo(rateCoin.Amount)
			var duration uint64
			if quotient.IsUint64() {
				duration = quotient.Uint64()
			} else {
				duration = math.MaxUint64
			}
			estimatedDurationSeconds = min(estimatedDurationSeconds, duration)
		}

		// If we never found a matching denom, set to 0
		if !foundRate {
			estimatedDurationSeconds = 0
		}
	}

	return &types.QueryCreditEstimateResponse{
		CurrentBalance:           currentBalance,
		TotalRatePerSecond:       totalRatePerSecond,
		EstimatedDurationSeconds: estimatedDurationSeconds,
		ActiveLeaseCount:         activeLeaseCount,
	}, nil
}

func checkedCreditEstimateItemCount(tenant string, current uint64, additional int) (uint64, error) {
	additionalCount, err := cast.ToUint64E(additional)
	if err != nil {
		return current, status.Error(codes.Internal, types.ErrReservationInvariant.Wrapf(
			"tenant %s has invalid negative credit-estimate item count increment %d",
			tenant,
			additional,
		).Error())
	}
	if current > types.MaxCreditEstimateLeaseItems ||
		additionalCount > types.MaxCreditEstimateLeaseItems-current {
		return current, status.Error(codes.ResourceExhausted,
			types.ErrLeaseQueryLimitExceeded.Wrapf(
				"tenant %s active leases contain more than %d items; credit-estimate work ceiling exceeded",
				tenant,
				types.MaxCreditEstimateLeaseItems,
			).Error(),
		)
	}
	return current + additionalCount, nil
}

// paginateSKUIndex paginates over the LeaseBySKUIndex iterator with bounded
// cursor-only work. A sparse state filter may yield a short page and a non-empty
// cursor once the per-request scan budget is exhausted.
func paginateSKUIndex(
	ctx context.Context,
	iter collections.Iterator[collections.Pair[string, string], bool],
	getLease func(ctx context.Context, leaseUUID string) (types.Lease, error),
	pageReq *query.PageRequest,
	filter func(types.Lease) bool,
) (leases []types.Lease, pageResponse *query.PageResponse, err error) {
	defer func() {
		if closeErr := iter.Close(); err == nil {
			err = closeErr
		}
	}()

	pageReq, err = pagination.CursorPageRequest(pageReq)
	if err != nil {
		return nil, nil, err
	}

	// Default pagination values
	limit := uint64(query.DefaultLimit)

	if pageReq != nil {
		if pageReq.Limit > 0 {
			limit = pageReq.Limit
		}
	}

	var nextKey []byte
	var scanned uint64

	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			return nil, nil, err
		}
		leaseUUID := key.K2()
		if scanned >= pagination.MaxPageScanLimit {
			nextKey = []byte(leaseUUID)
			break
		}
		scanned++

		lease, err := getLease(ctx, leaseUUID)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				continue
			}
			return nil, nil, err
		}

		// Apply filter if provided
		if filter != nil && !filter(lease) {
			continue
		}

		// Check if we've reached the limit
		if uint64(len(leases)) >= limit {
			nextKey = []byte(leaseUUID)
			break
		}

		leases = append(leases, lease)
	}

	return leases, &query.PageResponse{NextKey: nextKey}, nil
}

// LeaseByCustomDomain returns the lease and the service_name of the item that
// has claimed the given custom_domain.
func (q Querier) LeaseByCustomDomain(ctx context.Context, req *types.QueryLeaseByCustomDomainRequest) (*types.QueryLeaseByCustomDomainResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request cannot be nil")
	}
	// Normalise BEFORE the empty check so whitespace-only input is rejected
	// with InvalidArgument instead of falling through to a misleading NotFound
	// for the empty-string canonical form.
	domain := strings.ToLower(strings.TrimSpace(req.CustomDomain))
	if domain == "" {
		return nil, status.Error(codes.InvalidArgument, "custom_domain cannot be empty")
	}
	lease, serviceName, has, err := q.k.GetLeaseByCustomDomain(ctx, domain)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !has {
		return nil, status.Errorf(codes.NotFound, "no lease with custom_domain %s", domain)
	}
	return &types.QueryLeaseByCustomDomainResponse{Lease: lease, ServiceName: serviceName}, nil
}
