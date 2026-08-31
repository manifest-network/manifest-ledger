package keeper

import (
	"context"
	"errors"
	"math"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

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

	// Use state index for efficient lookup when filtering by state
	if req.StateFilter != types.LEASE_STATE_UNSPECIFIED {
		iter, err := pagination.MatchExactWithOrder(ctx, q.k.Leases.Indexes.State, int32(req.StateFilter), req.Pagination)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		leases, pageRes, err := pagination.PaginateStringIndex(
			ctx,
			iter,
			q.k.Leases.Get,
			req.Pagination,
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
		req.Pagination,
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

	// Use compound index when state filter is provided - O(1) direct lookup
	if req.StateFilter != types.LEASE_STATE_UNSPECIFIED {
		key := collections.Join(tenantAddr, int32(req.StateFilter))
		iter, err := pagination.MatchExactWithOrder(ctx, q.k.Leases.Indexes.TenantState, key, req.Pagination)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		leases, pageRes, err := pagination.PaginateStringIndex(
			ctx,
			iter,
			q.k.Leases.Get,
			req.Pagination,
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
	iter, err := pagination.MatchExactWithOrder(ctx, q.k.Leases.Indexes.Tenant, tenantAddr, req.Pagination)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	leases, pageRes, err := pagination.PaginateStringIndex(
		ctx,
		iter,
		q.k.Leases.Get,
		req.Pagination,
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

	// Use compound index when state filter is provided - O(1) direct lookup
	if req.StateFilter != types.LEASE_STATE_UNSPECIFIED {
		key := collections.Join(req.ProviderUuid, int32(req.StateFilter))
		iter, err := pagination.MatchExactWithOrder(ctx, q.k.Leases.Indexes.ProviderState, key, req.Pagination)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		leases, pageRes, err := pagination.PaginateStringIndex(
			ctx,
			iter,
			q.k.Leases.Get,
			req.Pagination,
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
	iter, err := pagination.MatchExactWithOrder(ctx, q.k.Leases.Indexes.Provider, req.ProviderUuid, req.Pagination)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	leases, pageRes, err := pagination.PaginateStringIndex(
		ctx,
		iter,
		q.k.Leases.Get,
		req.Pagination,
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

	// Collect relevant denoms from the tenant's leases and reserved amounts.
	// Uses per-denom GetBalance to avoid loading dust from unrelated token sends (DoS mitigation).
	relevantDenoms, err := q.k.getRelevantDenomsForTenant(ctx, ca)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	var balances sdk.Coins
	if len(relevantDenoms) > 0 {
		// Primary path: per-denom queries using only lease/reservation denoms (DoS-safe).
		balances, err = q.k.getCreditBalancesForDenoms(ctx, req.Tenant, relevantDenoms)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	} else {
		// Fallback for tenants with no denom hints: reached whenever the credit account
		// currently has no active/pending leases and no reservations. getRelevantDenomsForTenant
		// derives denoms only from ReservedAmounts plus ACTIVE/PENDING leases, and reservations
		// are released when a lease closes, so this covers any inter-lease steady state (a funded
		// account between leases), not just the window between FundCredit and the first lease.
		// Uses GetAllBalances since we have no denom hints. The node-side cost is O(n) where n is
		// the number of denoms on the credit address, but exploiting this requires spending gas to
		// send dust; revisit this scan if the fallback ever becomes hot.
		creditAddr, addrErr := types.DeriveCreditAddressFromBech32(req.Tenant)
		if addrErr != nil {
			return nil, status.Error(codes.Internal, addrErr.Error())
		}
		balances = q.k.bankKeeper.GetAllBalances(ctx, creditAddr)
	}

	// Calculate available balances (balance - reserved amounts)
	// This is what can be used for new lease reservations
	availableBalances := types.GetAvailableCredit(balances, ca.ReservedAmounts)

	return &types.QueryCreditAccountResponse{
		CreditAccount:     ca,
		Balances:          balances,
		AvailableBalances: availableBalances,
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

// ProviderWithdrawable queries the withdrawable amounts for a provider, paginated
// over the provider's ACTIVE leases (the only state CalculateWithdrawableForLease
// can return a non-zero amount for). It mirrors how LeasesByProvider paginates and
// is symmetric with provider-wide MsgWithdraw: loop until pagination.next_key is
// empty and sum the per-page amounts for the provider's full withdrawable total.
// The page size defaults to DefaultProviderWithdrawableQueryLimit and is capped at
// MaxProviderWithdrawableQueryLimit to bound query cost.
func (q Querier) ProviderWithdrawable(ctx context.Context, req *types.QueryProviderWithdrawableRequest) (*types.QueryProviderWithdrawableResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.ProviderUuid == "" {
		return nil, status.Error(codes.InvalidArgument, "provider_uuid cannot be empty")
	}

	// Normalize pagination: default and cap the page limit to bound query cost,
	// preserving any cursor/order the caller supplied.
	pageReq := query.PageRequest{}
	if req.Pagination != nil {
		pageReq = *req.Pagination
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

	totalWithdrawable := sdk.NewCoins()
	var leaseCount uint64
	for i := range leases {
		withdrawable, err := q.k.CalculateWithdrawableForLease(ctx, leases[i])
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		if !withdrawable.IsZero() {
			totalWithdrawable, err = types.SafeAddCoins(totalWithdrawable, withdrawable)
			if err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
			leaseCount++
		}
	}

	return &types.QueryProviderWithdrawableResponse{
		Amounts:    totalWithdrawable,
		LeaseCount: leaseCount,
		Pagination: pageRes,
	}, nil
}

// CreditAccounts queries all credit accounts with pagination.
func (q Querier) CreditAccounts(ctx context.Context, req *types.QueryCreditAccountsRequest) (*types.QueryCreditAccountsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	creditAccounts, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.CreditAccounts,
		req.Pagination,
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

	// Use the SKU index to iterate only over leases containing this SKU.
	rng := collections.NewPrefixedPairRange[string, string](req.SkuUuid)
	if req.Pagination != nil {
		// Resume by a store-level seek on the cursor (deletion-tolerant, O(log n)),
		// mirroring pkg/pagination.MatchExactWithOrder. collections.PairRange's Start*
		// binds the byte-order lower bound regardless of Descending(), so reverse
		// resume must bound the upper end (EndInclusive) to land inclusively on the
		// cursor and iterate downward. Inclusive bounds keep next_key wire-compatible.
		if len(req.Pagination.Key) > 0 {
			cursor := string(req.Pagination.Key)
			if req.Pagination.Reverse {
				rng = rng.EndInclusive(cursor)
			} else {
				rng = rng.StartInclusive(cursor)
			}
		}
		if req.Pagination.Reverse {
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
		req.Pagination,
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

// CreditEstimate estimates remaining lease duration for a tenant.
func (q Querier) CreditEstimate(ctx context.Context, req *types.QueryCreditEstimateRequest) (*types.QueryCreditEstimateResponse, error) {
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

	// Bound iteration by the stored count rather than current params so pre-existing
	// active leases remain visible after a limit reduction. Retain a fixed hard ceiling
	// for corrupt or adversarial imported state.
	maxActiveLeases := min(creditAccount.ActiveLeaseCount, maxActiveLeaseQueryIterations)

	// Calculate total rate per second across all active leases.
	// Also collect relevant denoms for per-denom balance queries (DoS mitigation).
	totalRatePerSecond := sdk.NewCoins()
	var activeLeaseCount uint64
	denomSet := make(map[string]struct{})
	denoms := make([]string, 0, 4)

	// Use TenantState compound index to iterate only over active leases - O(k) instead of O(n)
	key := collections.Join(tenantAddr, int32(types.LEASE_STATE_ACTIVE))
	iter, err := q.k.Leases.Indexes.TenantState.MatchExact(ctx, key)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		// Bounded by the aggregate-derived cap (see maxActiveLeases above). Check before the
		// increment (mirroring getRelevantDenomsForTenant) so ActiveLeaseCount and the
		// summed set never diverge if the cap is ever reached.
		if activeLeaseCount >= maxActiveLeases {
			break
		}
		activeLeaseCount++

		leaseUUID, err := iter.PrimaryKey()
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		lease, err := q.k.Leases.Get(ctx, leaseUUID)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
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
			totalRatePerSecond, err = types.SafeAddCoins(totalRatePerSecond, sdk.Coins{itemRate})
			if err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
			if _, ok := denomSet[item.LockedPrice.Denom]; !ok {
				denomSet[item.LockedPrice.Denom] = struct{}{}
				denoms = append(denoms, item.LockedPrice.Denom)
			}
		}
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

		for _, rateCoin := range totalRatePerSecond {
			if rateCoin.Amount.IsZero() {
				continue
			}
			foundRate = true
			balanceAmount := currentBalance.AmountOf(rateCoin.Denom)
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

// paginateSKUIndex paginates over the LeaseBySKUIndex iterator.
// This is a custom pagination function for the many-to-many SKU → Lease index.
// It supports key-based cursor pagination, offset-based pagination, and countTotal.
func paginateSKUIndex(
	ctx context.Context,
	iter collections.Iterator[collections.Pair[string, string], bool],
	getLease func(ctx context.Context, leaseUUID string) (types.Lease, error),
	pageReq *query.PageRequest,
	filter func(types.Lease) bool,
) ([]types.Lease, *query.PageResponse, error) {
	defer iter.Close()

	// Default pagination values
	limit := uint64(query.DefaultLimit)
	offset := uint64(0)
	countTotal := false
	var startKey []byte

	if pageReq != nil {
		if pageReq.Limit > 0 {
			limit = pageReq.Limit
		}
		offset = pageReq.Offset
		countTotal = pageReq.CountTotal
		startKey = pageReq.Key
	}

	var leases []types.Lease
	var total uint64
	var skipped uint64
	var nextKey []byte

	// The iterator is already positioned at the resume point by LeasesBySKU's
	// store-level seek on pageReq.Key, so no front-scan is needed here.
	hasKey := len(startKey) > 0

	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			return nil, nil, err
		}
		leaseUUID := key.K2()

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

		// Count for total (if requested)
		if countTotal {
			total++
		}

		// Handle offset-based pagination (only when no key provided)
		if !hasKey && skipped < offset {
			skipped++
			continue
		}

		// Check if we've reached the limit
		if uint64(len(leases)) >= limit {
			if len(nextKey) == 0 {
				nextKey = []byte(leaseUUID)
			}
			if !countTotal {
				break
			}
			continue
		}

		leases = append(leases, lease)
	}

	// Build response
	pageRes := &query.PageResponse{NextKey: nextKey}
	if countTotal {
		pageRes.Total = total
	}

	return leases, pageRes, nil
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
