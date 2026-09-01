// Package simulation defines billing module simulation operations.
package simulation

import (
	"context"
	"fmt"
	"math"
	"math/rand"

	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
	skutypes "github.com/manifest-network/manifest-ledger/x/sku/types"
)

func randomFundingAmount(r *rand.Rand, minimum, maximum sdkmath.Int) (sdkmath.Int, error) {
	if !maximum.GT(minimum) {
		return minimum, nil
	}

	randomRange, err := maximum.SafeSub(minimum)
	if err != nil {
		return sdkmath.Int{}, err
	}
	rangeInt64 := int64(math.MaxInt64)
	if randomRange.IsInt64() {
		rangeInt64 = randomRange.Int64()
	}
	if rangeInt64 <= 0 {
		return minimum, nil
	}

	return minimum.SafeAdd(sdkmath.NewInt(r.Int63n(rangeInt64)))
}

// Billing simulation operation keys and weights configure message frequencies.
const (
	OpWeightMsgFundCredit          = "op_weight_msg_billing_fund_credit"            //nolint:gosec
	OpWeightMsgCreateLease         = "op_weight_msg_billing_create_lease"           //nolint:gosec
	OpWeightMsgAcknowledgeLease    = "op_weight_msg_billing_acknowledge_lease"      //nolint:gosec
	OpWeightMsgRejectLease         = "op_weight_msg_billing_reject_lease"           //nolint:gosec
	OpWeightMsgCancelLease         = "op_weight_msg_billing_cancel_lease"           //nolint:gosec
	OpWeightMsgCloseLease          = "op_weight_msg_billing_close_lease"            //nolint:gosec
	OpWeightMsgWithdraw            = "op_weight_msg_billing_withdraw"               //nolint:gosec
	OpWeightMsgSetItemCustomDomain = "op_weight_msg_billing_set_item_custom_domain" //nolint:gosec

	DefaultWeightMsgFundCredit          = 50
	DefaultWeightMsgCreateLease         = 40
	DefaultWeightMsgAcknowledgeLease    = 35 // High weight to process pending leases
	DefaultWeightMsgRejectLease         = 10 // Lower weight for rejections
	DefaultWeightMsgCancelLease         = 10 // Lower weight for cancellations
	DefaultWeightMsgCloseLease          = 20
	DefaultWeightMsgWithdraw            = 30
	DefaultWeightMsgSetItemCustomDomain = 20

	maxSimulationDomainAttempts = 16
)

// SKUKeeper defines the expected SKU keeper interface for simulation.
type SKUKeeper interface {
	GetAllSKUs(ctx context.Context) ([]skutypes.SKU, error)
	GetProvider(ctx context.Context, uuid string) (skutypes.Provider, error)
	GetAllProviders(ctx context.Context) ([]skutypes.Provider, error)
}

// WeightedOperations returns the all the billing module operations with their respective weights.
func WeightedOperations(
	appParams simtypes.AppParams,
	_ codec.JSONCodec,
	txGen client.TxConfig,
	k keeper.Keeper,
	sk SKUKeeper,
) []simtypes.WeightedOperation {
	operations := make([]simtypes.WeightedOperation, 0, 8)

	var weightMsgFundCredit int
	appParams.GetOrGenerate(OpWeightMsgFundCredit, &weightMsgFundCredit, nil, func(_ *rand.Rand) {
		weightMsgFundCredit = DefaultWeightMsgFundCredit
	})

	var weightMsgCreateLease int
	appParams.GetOrGenerate(OpWeightMsgCreateLease, &weightMsgCreateLease, nil, func(_ *rand.Rand) {
		weightMsgCreateLease = DefaultWeightMsgCreateLease
	})

	var weightMsgAcknowledgeLease int
	appParams.GetOrGenerate(OpWeightMsgAcknowledgeLease, &weightMsgAcknowledgeLease, nil, func(_ *rand.Rand) {
		weightMsgAcknowledgeLease = DefaultWeightMsgAcknowledgeLease
	})

	var weightMsgRejectLease int
	appParams.GetOrGenerate(OpWeightMsgRejectLease, &weightMsgRejectLease, nil, func(_ *rand.Rand) {
		weightMsgRejectLease = DefaultWeightMsgRejectLease
	})

	var weightMsgCancelLease int
	appParams.GetOrGenerate(OpWeightMsgCancelLease, &weightMsgCancelLease, nil, func(_ *rand.Rand) {
		weightMsgCancelLease = DefaultWeightMsgCancelLease
	})

	var weightMsgCloseLease int
	appParams.GetOrGenerate(OpWeightMsgCloseLease, &weightMsgCloseLease, nil, func(_ *rand.Rand) {
		weightMsgCloseLease = DefaultWeightMsgCloseLease
	})

	var weightMsgWithdraw int
	appParams.GetOrGenerate(OpWeightMsgWithdraw, &weightMsgWithdraw, nil, func(_ *rand.Rand) {
		weightMsgWithdraw = DefaultWeightMsgWithdraw
	})

	var weightMsgSetItemCustomDomain int
	appParams.GetOrGenerate(OpWeightMsgSetItemCustomDomain, &weightMsgSetItemCustomDomain, nil, func(_ *rand.Rand) {
		weightMsgSetItemCustomDomain = DefaultWeightMsgSetItemCustomDomain
	})

	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgFundCredit,
		SimulateMsgFundCredit(txGen, k, sk),
	))

	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCreateLease,
		SimulateMsgCreateLease(txGen, k, sk),
	))

	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAcknowledgeLease,
		SimulateMsgAcknowledgeLease(txGen, k, sk),
	))

	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRejectLease,
		SimulateMsgRejectLease(txGen, k, sk),
	))

	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCancelLease,
		SimulateMsgCancelLease(txGen, k),
	))

	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCloseLease,
		SimulateMsgCloseLease(txGen, k),
	))

	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgWithdraw,
		SimulateMsgWithdraw(txGen, k, sk),
	))

	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgSetItemCustomDomain,
		SimulateMsgSetItemCustomDomain(txGen, k),
	))

	// MsgCreateLeaseForTenant is an administrative migration message. The
	// module authority has no simulation private key, and its stateful request
	// is not safe to delay inside a governance proposal. Dedicated keeper and
	// migration tests cover it instead.
	return operations
}

// SimulateMsgFundCredit generates a MsgFundCredit with random values.
func SimulateMsgFundCredit(txGen client.TxConfig, k keeper.Keeper, sk SKUKeeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgFundCredit{})

		// Select random sender
		sender, _ := simtypes.RandomAcc(r, accs)

		// Select random tenant (can be same as sender or different)
		tenant, _ := simtypes.RandomAcc(r, accs)

		// Get denom from an active SKU to ensure we fund credit in the correct denom
		// Default to DefaultBondDenom ("stake") which matches SKU simulation
		denom := sdk.DefaultBondDenom
		allSKUs, err := sk.GetAllSKUs(ctx)
		if err == nil && len(allSKUs) > 0 {
			// Use the denom from an existing active SKU
			for _, sku := range allSKUs {
				if sku.Active {
					denom = sku.BasePrice.Denom
					break
				}
			}
		}

		// Get total spendable balance in billing denom
		spendableCoins := k.GetBankKeeper().SpendableCoins(ctx, sender.Address)
		senderBalance := spendableCoins.AmountOf(denom)
		if senderBalance.IsZero() {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "sender has no billing denom balance"), nil, nil
		}

		// Reserve a fixed fee amount (conservative estimate)
		fixedFee := sdkmath.NewInt(100_000)

		// Minimum amount required: fee + minimum meaningful funding
		minFundingAmount := sdkmath.NewInt(1_000_000)
		minRequired, err := fixedFee.SafeAdd(minFundingAmount)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "funding amount overflow"), nil, nil
		}

		if senderBalance.LT(minRequired) {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "sender balance too low"), nil, nil
		}

		// Available for funding = total balance - reserved for fees
		availableForFunding := senderBalance.Sub(fixedFee)

		// Use at most 50% of available amount for this funding operation
		// to leave room for future operations
		maxFundingAmount := availableForFunding.QuoRaw(2)
		if maxFundingAmount.LT(minFundingAmount) {
			maxFundingAmount = minFundingAmount
		}

		// Ensure we don't exceed available
		if maxFundingAmount.GT(availableForFunding) {
			maxFundingAmount = availableForFunding
		}

		// Random amount between min and max
		randAmount, err := randomFundingAmount(r, minFundingAmount, maxFundingAmount)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "random funding amount overflow"), nil, nil
		}

		amount := sdk.NewCoin(denom, randAmount)
		fees := sdk.NewCoins(sdk.NewCoin(denom, fixedFee))

		msg := &types.MsgFundCredit{
			Sender: sender.Address.String(),
			Tenant: tenant.Address.String(),
			Amount: amount,
		}

		// Use GenAndDeliverTx with pre-calculated fees (not random fees)
		// This ensures we never overdraw by using a fixed fee we already accounted for
		return simulation.GenAndDeliverTx(newOperationInput(r, app, ctx, txGen, sender, msg, k), fees)
	}
}

// SimulateMsgCreateLease generates a MsgCreateLease with random values.
func SimulateMsgCreateLease(txGen client.TxConfig, k keeper.Keeper, sk SKUKeeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgCreateLease{})

		// Get all active SKUs
		allSKUs, err := sk.GetAllSKUs(ctx)
		if err != nil || len(allSKUs) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no SKUs found"), nil, nil
		}

		// Filter to active SKUs with active providers
		var activeSKUs []skutypes.SKU
		for _, sku := range allSKUs {
			if sku.Active {
				provider, err := sk.GetProvider(ctx, sku.ProviderUuid)
				if err == nil && provider.Active {
					activeSKUs = append(activeSKUs, sku)
				}
			}
		}

		if len(activeSKUs) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no active SKUs with active providers"), nil, nil
		}

		// Pick a random active SKU
		sku := activeSKUs[r.Intn(len(activeSKUs))]
		skuDenom := sku.BasePrice.Denom

		// Find a simulation account that has credit in the SKU's denom
		// Shuffle accounts to add randomness
		shuffledAccs := make([]simtypes.Account, len(accs))
		copy(shuffledAccs, accs)
		r.Shuffle(len(shuffledAccs), func(i, j int) {
			shuffledAccs[i], shuffledAccs[j] = shuffledAccs[j], shuffledAccs[i]
		})

		var tenant simtypes.Account
		var tenantFound bool
		for _, acc := range shuffledAccs {
			creditBalance, err := k.GetCreditBalance(ctx, acc.Address.String(), skuDenom)
			if err == nil && !creditBalance.Amount.IsZero() {
				tenant = acc
				tenantFound = true
				break
			}
		}

		if !tenantFound {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no accounts with credit found"), nil, nil
		}

		// Check both creation-time lease-count gates before delivery. The pending
		// cap is independent of the active cap and commonly fills first.
		params, err := k.GetParams(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get params"), nil, nil
		}

		creditAccount, err := k.GetCreditAccount(ctx, tenant.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "tenant credit account not found"), nil, nil
		}
		if creditAccount.ActiveLeaseCount >= params.MaxLeasesPerTenant {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "tenant at max lease limit"), nil, nil
		}
		if creditAccount.PendingLeaseCount >= params.MaxPendingLeasesPerTenant {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "tenant at max pending lease limit"), nil, nil
		}

		// Create 1-3 items without exceeding a governance-lowered module cap.
		numItems, validLimit := simulationLeaseItemCount(r, params.MaxItemsPerLease)
		if !validLimit {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "max items per lease is zero"), nil, nil
		}

		// Get all SKUs from the same provider
		var providerSKUs []skutypes.SKU
		for _, s := range activeSKUs {
			if s.ProviderUuid == sku.ProviderUuid {
				providerSKUs = append(providerSKUs, s)
			}
		}

		numItems = min(numItems, len(providerSKUs))

		// Shuffle and pick unique SKUs
		r.Shuffle(len(providerSKUs), func(i, j int) {
			providerSKUs[i], providerSKUs[j] = providerSKUs[j], providerSKUs[i]
		})

		items := buildSimLeaseItems(r, providerSKUs[:numItems])

		// Skip if the tenant cannot afford the lease; delivering it would fail with
		// ErrInsufficientCredit and abort the simulation instead of being a valid NoOp.
		if !tenantCanAffordLease(ctx, k, tenant.Address.String(), items, providerSKUs[:numItems], params.MinLeaseDuration) {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "tenant cannot afford lease"), nil, nil
		}

		msg := &types.MsgCreateLease{
			Tenant: tenant.Address.String(),
			Items:  items,
		}

		return genAndDeliverTxWithRandFees(r, app, ctx, txGen, tenant, msg, k)
	}
}

// SimulateMsgAcknowledgeLease generates a MsgAcknowledgeLease with random values.
// This simulates a provider acknowledging a PENDING lease to make it ACTIVE.
func SimulateMsgAcknowledgeLease(txGen client.TxConfig, k keeper.Keeper, sk SKUKeeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgAcknowledgeLease{})

		// Get all leases
		allLeases, err := k.GetAllLeases(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get leases"), nil, err
		}
		if len(allLeases) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no leases found"), nil, nil
		}

		params, err := k.GetParams(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get params"), nil, nil
		}

		// Filter to pending leases that still satisfy the activation gates. Delivering
		// an acknowledgement for an overdue lease or a tenant already at the active
		// cap would be an expected module rejection, not a useful simulation action.
		var pendingLeases []types.Lease
		activeCountsByTenant := make(map[string]uint64)
		invalidTenants := make(map[string]struct{})
		for _, lease := range allLeases {
			if lease.State != types.LEASE_STATE_PENDING || params.PendingLeaseDeadlineExceeded(ctx.BlockTime(), lease.CreatedAt) {
				continue
			}

			if _, invalid := invalidTenants[lease.Tenant]; invalid {
				continue
			}
			activeCount, found := activeCountsByTenant[lease.Tenant]
			if !found {
				creditAccount, err := k.GetCreditAccount(ctx, lease.Tenant)
				if err != nil {
					invalidTenants[lease.Tenant] = struct{}{}
					continue
				}
				activeCount = creditAccount.ActiveLeaseCount
				activeCountsByTenant[lease.Tenant] = activeCount
			}
			if activeCount >= params.MaxLeasesPerTenant {
				continue
			}

			pendingLeases = append(pendingLeases, lease)
		}

		if len(pendingLeases) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no acknowledgeable pending leases found"), nil, nil
		}

		// Pick a random pending lease
		lease := pendingLeases[r.Intn(len(pendingLeases))]

		// Get the provider to find the provider address
		provider, err := sk.GetProvider(ctx, lease.ProviderUuid)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "provider not found"), nil, nil
		}

		// Find the provider address account by decoded identity.
		sender, found, err := simulationAccountForAddress(accs, provider.Address)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "invalid provider address"), nil, err
		}

		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "provider account not found in simulation"), nil, nil
		}

		msg := &types.MsgAcknowledgeLease{
			Sender:     sender.Address.String(),
			LeaseUuids: []string{lease.Uuid},
		}

		return genAndDeliverTxWithRandFees(r, app, ctx, txGen, sender, msg, k)
	}
}

// SimulateMsgRejectLease generates a MsgRejectLease with random values.
// This simulates a provider rejecting a PENDING lease.
func SimulateMsgRejectLease(txGen client.TxConfig, k keeper.Keeper, sk SKUKeeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgRejectLease{})

		// Get all leases
		allLeases, err := k.GetAllLeases(ctx)
		if err != nil || len(allLeases) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no leases found"), nil, nil
		}

		// Filter to pending leases
		var pendingLeases []types.Lease
		for _, lease := range allLeases {
			if lease.State == types.LEASE_STATE_PENDING {
				pendingLeases = append(pendingLeases, lease)
			}
		}

		if len(pendingLeases) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no pending leases found"), nil, nil
		}

		// Pick a random pending lease
		lease := pendingLeases[r.Intn(len(pendingLeases))]

		// Get the provider to find the provider address
		provider, err := sk.GetProvider(ctx, lease.ProviderUuid)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "provider not found"), nil, nil
		}

		// Find the provider address account by decoded identity.
		sender, found, err := simulationAccountForAddress(accs, provider.Address)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "invalid provider address"), nil, err
		}

		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "provider account not found in simulation"), nil, nil
		}

		// Generate a random rejection reason
		reasons := []string{
			"Insufficient capacity",
			"Region unavailable",
			"Maintenance scheduled",
			"Resource constraints",
			"",
		}
		reason := reasons[r.Intn(len(reasons))]

		msg := &types.MsgRejectLease{
			Sender:     sender.Address.String(),
			LeaseUuids: []string{lease.Uuid},
			Reason:     reason,
		}

		return genAndDeliverTxWithRandFees(r, app, ctx, txGen, sender, msg, k)
	}
}

// SimulateMsgCancelLease generates a MsgCancelLease with random values.
// This simulates a tenant cancelling their own PENDING lease.
func SimulateMsgCancelLease(txGen client.TxConfig, k keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgCancelLease{})

		// Get all leases
		allLeases, err := k.GetAllLeases(ctx)
		if err != nil || len(allLeases) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no leases found"), nil, nil
		}

		// Filter to pending leases
		var pendingLeases []types.Lease
		for _, lease := range allLeases {
			if lease.State == types.LEASE_STATE_PENDING {
				pendingLeases = append(pendingLeases, lease)
			}
		}

		if len(pendingLeases) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no pending leases found"), nil, nil
		}

		// Pick a random pending lease
		lease := pendingLeases[r.Intn(len(pendingLeases))]

		// Find the tenant account by decoded identity.
		sender, found, err := simulationAccountForAddress(accs, lease.Tenant)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "invalid tenant address"), nil, err
		}

		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "tenant account not found in simulation"), nil, nil
		}

		msg := &types.MsgCancelLease{
			Tenant:     sender.Address.String(),
			LeaseUuids: []string{lease.Uuid},
		}

		return genAndDeliverTxWithRandFees(r, app, ctx, txGen, sender, msg, k)
	}
}

// SimulateMsgCloseLease generates a MsgCloseLease with random values.
func SimulateMsgCloseLease(txGen client.TxConfig, k keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgCloseLease{})

		// Get all leases
		allLeases, err := k.GetAllLeases(ctx)
		if err != nil || len(allLeases) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no leases found"), nil, nil
		}

		// Filter to active leases
		var activeLeases []types.Lease
		for _, lease := range allLeases {
			if lease.State == types.LEASE_STATE_ACTIVE {
				activeLeases = append(activeLeases, lease)
			}
		}

		if len(activeLeases) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no active leases found"), nil, nil
		}

		// Pick a random active lease
		lease := activeLeases[r.Intn(len(activeLeases))]

		// Find the tenant account by decoded identity.
		sender, found, err := simulationAccountForAddress(accs, lease.Tenant)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "invalid tenant address"), nil, err
		}

		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "tenant account not found in simulation"), nil, nil
		}

		msg := &types.MsgCloseLease{
			Sender:     sender.Address.String(),
			LeaseUuids: []string{lease.Uuid},
		}

		return genAndDeliverTxWithRandFees(r, app, ctx, txGen, sender, msg, k)
	}
}

// SimulateMsgWithdraw generates a MsgWithdraw with random values.
// Randomly chooses between specific lease mode and provider-wide mode.
func SimulateMsgWithdraw(txGen client.TxConfig, k keeper.Keeper, sk SKUKeeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgWithdraw{})

		// Get all leases
		allLeases, err := k.GetAllLeases(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get leases"), nil, err
		}
		if len(allLeases) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no leases found"), nil, nil
		}

		// Filter to leases that have withdrawable amounts
		var withdrawableLeases []types.Lease
		for _, lease := range allLeases {
			// Calculate withdrawable amount for this lease
			withdrawable, err := k.CalculateWithdrawableForLease(ctx, lease)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to calculate a lease withdrawal"), nil,
					fmt.Errorf("calculate withdrawable amount for lease %s: %w", lease.Uuid, err)
			}
			if !withdrawable.IsZero() {
				withdrawableLeases = append(withdrawableLeases, lease)
			}
		}

		if len(withdrawableLeases) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no leases with withdrawable amount"), nil, nil
		}

		// Randomly choose between specific lease mode (50%) and provider-wide mode (50%)
		useProviderWideMode := r.Intn(2) == 0

		if useProviderWideMode {
			return simulateProviderWideWithdraw(r, app, ctx, txGen, accs, sk, k, withdrawableLeases)
		}

		return simulateSpecificLeaseWithdraw(r, app, ctx, txGen, accs, sk, k, withdrawableLeases)
	}
}

type customDomainSimulationTarget struct {
	signer        simtypes.Account
	leaseUUID     string
	serviceName   string
	currentDomain string
}

// SimulateMsgSetItemCustomDomain generates both claim/replace and clear
// transitions for addressable items on live leases.
func SimulateMsgSetItemCustomDomain(txGen client.TxConfig, k keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgSetItemCustomDomain{})

		leases, err := k.GetAllLeases(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get leases"), nil, err
		}
		targets, err := eligibleCustomDomainTargets(leases, accs)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to select a lease item"), nil, err
		}
		target, clearDomain, found := selectCustomDomainMutation(r, targets)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no addressable live lease item owned by a simulation account"), nil, nil
		}
		params, err := k.GetParams(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get params"), nil, err
		}
		signer, found := selectCustomDomainSigner(r, target.signer, accs, params)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no authorized custom-domain signer"), nil, nil
		}

		customDomain := ""
		if !clearDomain {
			customDomain, found, err = unusedSimulationDomain(r, ctx, k, params)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to inspect custom-domain index"), nil, err
			}
			if !found {
				return simtypes.NoOpMsg(types.ModuleName, msgType, "could not generate an unreserved custom domain"), nil, nil
			}
		}

		msg := &types.MsgSetItemCustomDomain{
			Sender:       signer.Address.String(),
			LeaseUuid:    target.leaseUUID,
			ServiceName:  target.serviceName,
			CustomDomain: customDomain,
		}
		return genAndDeliverTxWithRandFees(r, app, ctx, txGen, signer, msg, k)
	}
}

// eligibleCustomDomainTargets walks lease and account slices only. Collection
// iteration supplies leases in key order, and decoded-byte identity matching
// makes the result deterministic even for equivalent Bech32 spellings.
func eligibleCustomDomainTargets(
	leases []types.Lease,
	accs []simtypes.Account,
) ([]customDomainSimulationTarget, error) {
	targets := make([]customDomainSimulationTarget, 0)
	for _, lease := range leases {
		if lease.State != types.LEASE_STATE_PENDING && lease.State != types.LEASE_STATE_ACTIVE {
			continue
		}
		signer, found, err := simulationAccountForAddress(accs, lease.Tenant)
		if err != nil {
			return nil, fmt.Errorf("decode tenant for lease %s: %w", lease.Uuid, err)
		}
		if !found || len(lease.Items) == 0 {
			continue
		}

		// A one-item legacy lease is addressable by the empty service name.
		// Multi-item legacy leases are ambiguous and deliberately excluded.
		if len(lease.Items) > 1 && lease.Items[0].ServiceName == "" {
			continue
		}
		for _, item := range lease.Items {
			targets = append(targets, customDomainSimulationTarget{
				signer:        signer,
				leaseUUID:     lease.Uuid,
				serviceName:   item.ServiceName,
				currentDomain: item.CustomDomain,
			})
		}
	}
	return targets, nil
}

func simulationAccountForAddress(
	accs []simtypes.Account,
	address string,
) (simtypes.Account, bool, error) {
	decoded, err := sdk.AccAddressFromBech32(address)
	if err != nil {
		return simtypes.Account{}, false, err
	}
	for _, acc := range accs {
		if acc.Address.Equals(decoded) {
			return acc, true, nil
		}
	}
	return simtypes.Account{}, false, nil
}

func selectCustomDomainMutation(
	r *rand.Rand,
	targets []customDomainSimulationTarget,
) (customDomainSimulationTarget, bool, bool) {
	if len(targets) == 0 {
		return customDomainSimulationTarget{}, false, false
	}

	claimed := make([]customDomainSimulationTarget, 0, len(targets))
	for _, target := range targets {
		if target.currentDomain != "" {
			claimed = append(claimed, target)
		}
	}
	if len(claimed) > 0 && r.Intn(2) == 0 {
		return claimed[r.Intn(len(claimed))], true, true
	}
	return targets[r.Intn(len(targets))], false, true
}

func selectCustomDomainSigner(
	r *rand.Rand,
	tenant simtypes.Account,
	accs []simtypes.Account,
	params types.Params,
) (simtypes.Account, bool) {
	authorized := make([]simtypes.Account, 0, len(accs))
	for _, acc := range accs {
		if !acc.Address.Equals(tenant.Address) && !params.IsAllowed(acc.Address.String()) {
			continue
		}
		duplicate := false
		for _, existing := range authorized {
			if acc.Address.Equals(existing.Address) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			authorized = append(authorized, acc)
		}
	}
	if len(authorized) == 0 {
		return simtypes.Account{}, false
	}
	return authorized[r.Intn(len(authorized))], true
}

func unusedSimulationDomain(
	r *rand.Rand,
	ctx sdk.Context,
	k keeper.Keeper,
	params types.Params,
) (string, bool, error) {
	for range maxSimulationDomainAttempts {
		candidate := simulationDomainCandidate(r)
		if types.MatchesReservedSuffix(candidate, params.ReservedDomainSuffixes) {
			continue
		}
		if err := types.IsValidFQDN(candidate); err != nil {
			return "", false, fmt.Errorf("generated invalid domain %q: %w", candidate, err)
		}
		_, _, claimed, err := k.GetLeaseByCustomDomain(ctx, candidate)
		if err != nil {
			return "", false, err
		}
		if !claimed {
			return candidate, true, nil
		}
	}
	return "", false, nil
}

func simulationDomainCandidate(r *rand.Rand) string {
	return fmt.Sprintf("sim-%016x.sim%08x", r.Uint64(), r.Uint32())
}

func simulationLeaseItemCount(r *rand.Rand, configuredMaximum uint64) (int, bool) {
	maximum := min(configuredMaximum, uint64(3))
	if maximum == 0 {
		return 0, false
	}
	return r.Intn(int(maximum)) + 1, true //nolint:gosec // maximum is bounded to [1, 3]
}

// simulateSpecificLeaseWithdraw simulates withdrawal from specific leases.
func simulateSpecificLeaseWithdraw(
	r *rand.Rand,
	app *baseapp.BaseApp,
	ctx sdk.Context,
	txGen client.TxConfig,
	accs []simtypes.Account,
	sk SKUKeeper,
	k keeper.Keeper,
	withdrawableLeases []types.Lease,
) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	msgType := sdk.MsgTypeURL(&types.MsgWithdraw{})

	// Pick a random lease with withdrawable amount
	lease := withdrawableLeases[r.Intn(len(withdrawableLeases))]

	// Get provider to find the provider address
	provider, err := sk.GetProvider(ctx, lease.ProviderUuid)
	if err != nil {
		return simtypes.NoOpMsg(types.ModuleName, msgType, "provider not found"), nil, nil
	}

	// Find the provider address account by decoded identity.
	sender, found, err := simulationAccountForAddress(accs, provider.Address)
	if err != nil {
		return simtypes.NoOpMsg(types.ModuleName, msgType, "invalid provider address"), nil, err
	}

	if !found {
		return simtypes.NoOpMsg(types.ModuleName, msgType, "provider account not found in simulation"), nil, nil
	}

	// Randomly pick 1-3 leases from the same provider
	var providerLeases []types.Lease
	for _, l := range withdrawableLeases {
		if l.ProviderUuid == lease.ProviderUuid {
			providerLeases = append(providerLeases, l)
		}
	}

	numLeases := r.Intn(3) + 1
	numLeases = min(numLeases, len(providerLeases))

	// Shuffle and pick
	r.Shuffle(len(providerLeases), func(i, j int) {
		providerLeases[i], providerLeases[j] = providerLeases[j], providerLeases[i]
	})

	leaseUUIDs := make([]string, numLeases)
	for i := range numLeases {
		leaseUUIDs[i] = providerLeases[i].Uuid
	}

	msg := &types.MsgWithdraw{
		Sender:     sender.Address.String(),
		LeaseUuids: leaseUUIDs,
	}

	return genAndDeliverTxWithRandFees(r, app, ctx, txGen, sender, msg, k)
}

// simulateProviderWideWithdraw simulates provider-wide withdrawal mode.
func simulateProviderWideWithdraw(
	r *rand.Rand,
	app *baseapp.BaseApp,
	ctx sdk.Context,
	txGen client.TxConfig,
	accs []simtypes.Account,
	sk SKUKeeper,
	k keeper.Keeper,
	withdrawableLeases []types.Lease,
) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	msgType := sdk.MsgTypeURL(&types.MsgWithdraw{})

	// Collect unique provider UUIDs in deterministic insertion order.
	// A map range would randomise the slice and break simulation determinism.
	seen := make(map[string]struct{})
	uuids := make([]string, 0, 4)
	for _, lease := range withdrawableLeases {
		if _, ok := seen[lease.ProviderUuid]; !ok {
			seen[lease.ProviderUuid] = struct{}{}
			uuids = append(uuids, lease.ProviderUuid)
		}
	}

	if len(uuids) == 0 {
		return simtypes.NoOpMsg(types.ModuleName, msgType, "no providers with withdrawable leases"), nil, nil
	}

	providerUUID := uuids[r.Intn(len(uuids))]

	// Get provider to find the provider address
	provider, err := sk.GetProvider(ctx, providerUUID)
	if err != nil {
		return simtypes.NoOpMsg(types.ModuleName, msgType, "provider not found"), nil, nil
	}

	// Find the provider address account by decoded identity.
	sender, found, err := simulationAccountForAddress(accs, provider.Address)
	if err != nil {
		return simtypes.NoOpMsg(types.ModuleName, msgType, "invalid provider address"), nil, err
	}

	if !found {
		return simtypes.NoOpMsg(types.ModuleName, msgType, "provider account not found in simulation"), nil, nil
	}

	// Random limit: 0 (use default), or 10-100
	var limit uint64
	if r.Intn(2) == 0 {
		limit = 0 // Use default limit
	} else {
		limit = uint64(r.Intn(91)) + 10 //nolint:gosec // r.Intn returns non-negative, result is 10-100
	}

	msg := &types.MsgWithdraw{
		Sender:       sender.Address.String(),
		ProviderUuid: providerUUID,
		Limit:        limit,
	}

	return genAndDeliverTxWithRandFees(r, app, ctx, txGen, sender, msg, k)
}

func newOperationInput(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, txGen client.TxConfig, simAccount simtypes.Account, msg sdk.Msg, k keeper.Keeper) simulation.OperationInput {
	return simulation.OperationInput{
		R:             r,
		App:           app,
		TxGen:         txGen,
		Cdc:           nil,
		Msg:           msg,
		Context:       ctx,
		SimAccount:    simAccount,
		AccountKeeper: k.GetAccountKeeper(),
		Bankkeeper:    k.GetBankKeeper(),
		ModuleName:    types.ModuleName,
	}
}

func genAndDeliverTxWithRandFees(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, txGen client.TxConfig, simAccount simtypes.Account, msg sdk.Msg, k keeper.Keeper) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	return simulation.GenAndDeliverTxWithRandFees(newOperationInput(r, app, ctx, txGen, simAccount, msg, k))
}

// buildSimLeaseItems constructs lease items from the given SKUs.
// 50% of the time it uses service_name mode (allowing the same SKU to appear
// multiple times with distinct DNS-label names), otherwise it uses legacy mode.
func buildSimLeaseItems(r *rand.Rand, skus []skutypes.SKU) []types.LeaseItemInput {
	useServiceNames := r.Intn(2) == 0

	if useServiceNames {
		// Service-name mode: reuse the first SKU for every item with unique names.
		items := make([]types.LeaseItemInput, len(skus))
		for i := range skus {
			items[i] = types.LeaseItemInput{
				SkuUuid:     skus[0].Uuid,
				Quantity:    uint64(r.Intn(10) + 1), //nolint:gosec
				ServiceName: fmt.Sprintf("svc-%d", i),
			}
		}
		return items
	}

	// Legacy mode: unique SKUs, no service_name.
	items := make([]types.LeaseItemInput, len(skus))
	for i, s := range skus {
		items[i] = types.LeaseItemInput{
			SkuUuid:  s.Uuid,
			Quantity: uint64(r.Intn(10) + 1), //nolint:gosec
		}
	}
	return items
}

// tenantCanAffordLease reports whether the tenant has enough AVAILABLE credit (balance minus
// already-reserved amounts) to cover the reservation for the given lease items over
// minLeaseDuration. It mirrors the credit check in the CreateLease message handler
// (msg_server.go), so simulated leases that would be rejected with ErrInsufficientCredit are
// skipped as a NoOp instead of aborting the whole simulation with a delivery error.
func tenantCanAffordLease(ctx sdk.Context, k keeper.Keeper, tenant string, items []types.LeaseItemInput, skus []skutypes.SKU, minLeaseDuration uint64) bool {
	skuByUUID := make(map[string]skutypes.SKU, len(skus))
	for _, s := range skus {
		skuByUUID[s.Uuid] = s
	}

	totalRatesPerSecond := sdk.NewCoins()
	for _, item := range items {
		s, ok := skuByUUID[item.SkuUuid]
		if !ok {
			return false
		}
		ratePerSecond, err := keeper.ConvertBasePriceToPerSecond(s.BasePrice, s.Unit)
		if err != nil {
			return false
		}
		itemRate, err := types.SafeMultiplyCoin(ratePerSecond, sdkmath.NewIntFromUint64(item.Quantity))
		if err != nil {
			return false
		}
		totalRatesPerSecond, err = types.SafeAddCoins(totalRatesPerSecond, sdk.Coins{itemRate})
		if err != nil {
			return false
		}
	}

	reservation, err := types.CalculateLeaseReservationFromRates(totalRatesPerSecond, minLeaseDuration)
	if err != nil {
		return false
	}
	if reservation.IsZero() {
		return true
	}

	creditAccount, err := k.GetCreditAccount(ctx, tenant)
	if err != nil {
		return false
	}
	balances := sdk.NewCoins()
	for _, res := range reservation {
		bal, err := k.GetCreditBalance(ctx, tenant, res.Denom)
		if err != nil {
			return false
		}
		if bal.IsPositive() {
			balances, err = types.SafeAddCoins(balances, sdk.Coins{bal})
			if err != nil {
				return false
			}
		}
	}

	available := types.GetAvailableCredit(balances, creditAccount.ReservedAmounts)
	for _, res := range reservation {
		if available.AmountOf(res.Denom).LT(res.Amount) {
			return false
		}
	}
	return true
}
