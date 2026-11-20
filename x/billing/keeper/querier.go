package keeper

import (
	"context"
	"errors"
	"math"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/collections"

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
	relevantDenoms, err := q.k.getRelevantDenomsForTenant(ctx, req.Tenant, ca.ReservedAmounts)
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

// WithdrawableAmount queried the amounts available for provider withdrawal from a lease.
//
// Deprecated: settlement is per stream, so a lease has no withdrawable amount
// of its own. The method is kept so existing clients get an explicit error
// naming the replacement (ADR-044) instead of a missing-method failure.
func (q Querier) WithdrawableAmount(_ context.Context, _ *types.QueryWithdrawableAmountRequest) (*types.QueryWithdrawableAmountResponse, error) {
	return nil, status.Error(codes.Unimplemented,
		"per-lease withdrawable amounts are no longer available: settlement is per stream, use ProviderWithdrawable or StreamsByProvider")
}

// normalizeStreamPageRequest defaults and caps the page limit of a stream
// query to bound its cost, preserving any cursor/order the caller supplied.
func normalizeStreamPageRequest(req *query.PageRequest) *query.PageRequest {
	pageReq := query.PageRequest{}
	if req != nil {
		pageReq = *req
	}
	if pageReq.Limit == 0 {
		pageReq.Limit = types.DefaultProviderWithdrawableQueryLimit
	}
	if pageReq.Limit > types.MaxProviderWithdrawableQueryLimit {
		pageReq.Limit = types.MaxProviderWithdrawableQueryLimit
	}
	return &pageReq
}

// ProviderWithdrawable queries the withdrawable amounts for a provider, paginated
// over the provider's streams. It is symmetric with MsgWithdraw: loop until
// pagination.next_key is empty and sum the per-page amounts for the provider's
// full withdrawable total. The page size defaults to
// DefaultProviderWithdrawableQueryLimit and is capped at
// MaxProviderWithdrawableQueryLimit to bound query cost.
func (q Querier) ProviderWithdrawable(ctx context.Context, req *types.QueryProviderWithdrawableRequest) (*types.QueryProviderWithdrawableResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.ProviderUuid == "" {
		return nil, status.Error(codes.InvalidArgument, "provider_uuid cannot be empty")
	}

	blockTime := sdk.UnwrapSDKContext(ctx).BlockTime()

	type streamAmount struct {
		amount     sdk.Coin
		leaseCount uint64
	}

	// Within one provider every stream has a distinct (tenant, denom), so each
	// stream is capped by a credit balance no other stream of the page shares.
	amounts, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Streams,
		normalizeStreamPageRequest(req.Pagination),
		func(key StreamKey, state types.StreamState) (streamAmount, error) {
			return streamAmount{
				amount:     q.k.streamWithdrawable(ctx, key, state, blockTime),
				leaseCount: state.LeaseCount,
			}, nil
		},
		query.WithCollectionPaginationPairPrefix[string, collections.Pair[sdk.AccAddress, string]](req.ProviderUuid),
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	totalWithdrawable := sdk.NewCoins()
	var leaseCount uint64
	for _, a := range amounts {
		if a.amount.IsPositive() {
			totalWithdrawable = totalWithdrawable.Add(a.amount)
			leaseCount += a.leaseCount
		}
	}

	return &types.QueryProviderWithdrawableResponse{
		Amounts:    totalWithdrawable,
		LeaseCount: leaseCount,
		Pagination: pageRes,
	}, nil
}

// StreamsByProvider queries the settlement streams of a provider.
func (q Querier) StreamsByProvider(ctx context.Context, req *types.QueryStreamsByProviderRequest) (*types.QueryStreamsByProviderResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	if req.ProviderUuid == "" {
		return nil, status.Error(codes.InvalidArgument, "provider_uuid cannot be empty")
	}

	streams, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Streams,
		normalizeStreamPageRequest(req.Pagination),
		func(key StreamKey, state types.StreamState) (types.Stream, error) {
			return streamFromState(key, state), nil
		},
		query.WithCollectionPaginationPairPrefix[string, collections.Pair[sdk.AccAddress, string]](req.ProviderUuid),
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryStreamsByProviderResponse{
		Streams:    streams,
		Pagination: pageRes,
	}, nil
}

// StreamsByTenant queries the settlement streams paid by a tenant.
func (q Querier) StreamsByTenant(ctx context.Context, req *types.QueryStreamsByTenantRequest) (*types.QueryStreamsByTenantResponse, error) {
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

	streams, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.StreamsByTenant,
		normalizeStreamPageRequest(req.Pagination),
		func(tenantKey StreamTenantKey, _ collections.NoValue) (types.Stream, error) {
			key := NewStreamKey(tenantKey.K2().K1(), tenantKey.K1(), tenantKey.K2().K2())
			state, err := q.k.Streams.Get(ctx, key)
			if err != nil {
				return types.Stream{}, err
			}
			return streamFromState(key, state), nil
		},
		query.WithCollectionPaginationPairPrefix[sdk.AccAddress, collections.Pair[string, string]](tenantAddr),
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryStreamsByTenantResponse{
		Streams:    streams,
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
//
// The burn rate is read from the tenant's streams, so the cost is O(streams)
// rather than O(leases). A stream frozen for exhausted credit no longer
// accrues and is left out of the rate. What the streams have accrued but not
// yet settled is already owed to providers, so it is deducted from the balance
// before estimating.
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

	// Verify credit account exists
	creditAccount, err := q.k.GetCreditAccount(ctx, req.Tenant)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	// The number of streams of a tenant is bounded by its active leases times
	// the items per lease; bound the walk by the same param-derived cap used
	// for per-tenant lease iteration (clamped to the params' upper bounds).
	params, err := q.k.GetParams(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	maxStreams := activeLeaseIterationCap(params) * max(params.MaxItemsPerLease, 1)

	blockTime := sdk.UnwrapSDKContext(ctx).BlockTime()

	totalRatePerSecond := sdk.NewCoins()
	unsettled := sdk.NewCoins()
	denomSet := make(map[string]struct{})
	denoms := make([]string, 0, 4)

	rng := collections.NewPrefixedPairRange[sdk.AccAddress, collections.Pair[string, string]](tenantAddr)
	iter, err := q.k.StreamsByTenant.Iterate(ctx, rng)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer iter.Close()

	var streamCount uint64
	accrualOverflow := false
	for ; iter.Valid() && streamCount < maxStreams; iter.Next() {
		streamCount++

		tenantKey, err := iter.Key()
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		denom := tenantKey.K2().K2()
		key := NewStreamKey(tenantKey.K2().K1(), tenantAddr, denom)

		state, err := q.k.Streams.Get(ctx, key)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		// Report the balance of every denom the tenant has a stream in, frozen
		// or not; only non-frozen streams contribute to the burn rate.
		if _, ok := denomSet[denom]; !ok {
			denomSet[denom] = struct{}{}
			denoms = append(denoms, denom)
		}
		if state.IsExhausted() {
			continue
		}

		totalRatePerSecond = totalRatePerSecond.Add(sdk.NewCoin(denom, state.Rate))
		accrued, _, representable := streamAccrual(state, blockTime)
		if !representable {
			// An unrepresentable accrual certainly exceeds the balance.
			accrualOverflow = true
		} else {
			unsettled = unsettled.Add(sdk.NewCoin(denom, accrued))
		}
	}

	// Fetch balances for only the denoms used by the streams (DoS mitigation).
	currentBalance, err := q.k.getCreditBalancesForDenoms(ctx, req.Tenant, denoms)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Calculate estimated duration
	// Find minimum duration across all denoms: min(remaining[denom] / rate[denom])
	var estimatedDurationSeconds uint64
	if !totalRatePerSecond.IsZero() && !accrualOverflow {
		// Start with max uint64, then find minimum
		estimatedDurationSeconds = math.MaxUint64

		for _, rateCoin := range totalRatePerSecond {
			if rateCoin.Amount.IsZero() {
				continue
			}
			// What is already accrued is not available to pay for future time.
			remaining := currentBalance.AmountOf(rateCoin.Denom).Sub(unsettled.AmountOf(rateCoin.Denom))
			if !remaining.IsPositive() {
				// Nothing left for this denom means immediate exhaustion
				estimatedDurationSeconds = 0
				break
			}
			// Duration = remaining / rate (integer division, rounds down)
			quotient := remaining.Quo(rateCoin.Amount)
			var duration uint64
			if quotient.IsUint64() {
				duration = quotient.Uint64()
			} else {
				duration = math.MaxUint64
			}
			estimatedDurationSeconds = min(estimatedDurationSeconds, duration)
		}

		// If we never found a matching denom, set to 0
		if estimatedDurationSeconds == math.MaxUint64 {
			estimatedDurationSeconds = 0
		}
	}

	return &types.QueryCreditEstimateResponse{
		CurrentBalance:           currentBalance,
		TotalRatePerSecond:       totalRatePerSecond,
		EstimatedDurationSeconds: estimatedDurationSeconds,
		ActiveLeaseCount:         creditAccount.ActiveLeaseCount,
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
