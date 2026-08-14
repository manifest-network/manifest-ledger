package simulation

import (
	"context"
	"fmt"
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

const (
	OpWeightMsgFundCredit           = "op_weight_msg_billing_fund_credit"             //nolint:gosec
	OpWeightMsgCreateLease          = "op_weight_msg_billing_create_lease"            //nolint:gosec
	OpWeightMsgCreateLeaseForTenant = "op_weight_msg_billing_create_lease_for_tenant" //nolint:gosec
	OpWeightMsgAcknowledgeLease     = "op_weight_msg_billing_acknowledge_lease"       //nolint:gosec
	OpWeightMsgRejectLease          = "op_weight_msg_billing_reject_lease"            //nolint:gosec
	OpWeightMsgCancelLease          = "op_weight_msg_billing_cancel_lease"            //nolint:gosec
	OpWeightMsgCloseLease           = "op_weight_msg_billing_close_lease"             //nolint:gosec
	OpWeightMsgWithdraw             = "op_weight_msg_billing_withdraw"                //nolint:gosec

	OpWeightMsgUpdateLease            = "op_weight_msg_billing_update_lease"             //nolint:gosec
	OpWeightMsgAcknowledgeLeaseUpdate = "op_weight_msg_billing_acknowledge_lease_update" //nolint:gosec
	OpWeightMsgRejectLeaseUpdate      = "op_weight_msg_billing_reject_lease_update"      //nolint:gosec
	OpWeightMsgCancelLeaseUpdate      = "op_weight_msg_billing_cancel_lease_update"      //nolint:gosec

	DefaultWeightMsgFundCredit           = 50
	DefaultWeightMsgCreateLease          = 40
	DefaultWeightMsgCreateLeaseForTenant = 10 // Lower weight since it's authority-only
	DefaultWeightMsgAcknowledgeLease     = 35 // High weight to process pending leases
	DefaultWeightMsgRejectLease          = 10 // Lower weight for rejections
	DefaultWeightMsgCancelLease          = 10 // Lower weight for cancellations
	DefaultWeightMsgCloseLease           = 20
	DefaultWeightMsgWithdraw             = 30

	// Lease-update handshake. These weights are tuned, not arbitrary — please
	// measure before lowering them.
	//
	// UpdateLease is the only operation that gives a simulated lease a
	// pending_meta_hash, and the other three can only act on a lease that
	// already has one. Genesis starts with no leases, so if UpdateLease rarely
	// succeeds, the pending state barely exists and the acknowledge/reject/
	// cancel handlers — plus the PendingUpdateIndex and the SetLease
	// normalisation — are never reached, while the simulation still passes.
	//
	// Measured over 100 blocks (successful deliveries per operation):
	//
	//	weight 25/20/10/10 → update 4,  ack 0, reject 0, cancel 0  ← useless
	//	weight 60/40/15/15 → update 36, ack 10, reject 5, cancel 1 (seed 42)
	//	                     update 19, ack 3,  reject 3, cancel 1 (seed 7)
	//	                     update 8,  ack 1,  reject 1, cancel 0 (seed 1234)
	//
	// Update and acknowledge — the paths that actually move meta_hash — fire on
	// every seed tried. Cancel is thin on some seeds; raising the weights
	// further would crowd out the rest of the module for a handler that is
	// reject-minus-the-hash-guard, so it is left as is.
	DefaultWeightMsgUpdateLease            = 60
	DefaultWeightMsgAcknowledgeLeaseUpdate = 40
	DefaultWeightMsgRejectLeaseUpdate      = 15
	DefaultWeightMsgCancelLeaseUpdate      = 15
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
	operations := make([]simtypes.WeightedOperation, 0)

	var weightMsgFundCredit int
	appParams.GetOrGenerate(OpWeightMsgFundCredit, &weightMsgFundCredit, nil, func(_ *rand.Rand) {
		weightMsgFundCredit = DefaultWeightMsgFundCredit
	})

	var weightMsgCreateLease int
	appParams.GetOrGenerate(OpWeightMsgCreateLease, &weightMsgCreateLease, nil, func(_ *rand.Rand) {
		weightMsgCreateLease = DefaultWeightMsgCreateLease
	})

	var weightMsgCreateLeaseForTenant int
	appParams.GetOrGenerate(OpWeightMsgCreateLeaseForTenant, &weightMsgCreateLeaseForTenant, nil, func(_ *rand.Rand) {
		weightMsgCreateLeaseForTenant = DefaultWeightMsgCreateLeaseForTenant
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

	var weightMsgUpdateLease int
	appParams.GetOrGenerate(OpWeightMsgUpdateLease, &weightMsgUpdateLease, nil, func(_ *rand.Rand) {
		weightMsgUpdateLease = DefaultWeightMsgUpdateLease
	})

	var weightMsgAcknowledgeLeaseUpdate int
	appParams.GetOrGenerate(OpWeightMsgAcknowledgeLeaseUpdate, &weightMsgAcknowledgeLeaseUpdate, nil, func(_ *rand.Rand) {
		weightMsgAcknowledgeLeaseUpdate = DefaultWeightMsgAcknowledgeLeaseUpdate
	})

	var weightMsgRejectLeaseUpdate int
	appParams.GetOrGenerate(OpWeightMsgRejectLeaseUpdate, &weightMsgRejectLeaseUpdate, nil, func(_ *rand.Rand) {
		weightMsgRejectLeaseUpdate = DefaultWeightMsgRejectLeaseUpdate
	})

	var weightMsgCancelLeaseUpdate int
	appParams.GetOrGenerate(OpWeightMsgCancelLeaseUpdate, &weightMsgCancelLeaseUpdate, nil, func(_ *rand.Rand) {
		weightMsgCancelLeaseUpdate = DefaultWeightMsgCancelLeaseUpdate
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
		weightMsgCreateLeaseForTenant,
		SimulateMsgCreateLeaseForTenant(txGen, k, sk),
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
		weightMsgUpdateLease,
		SimulateMsgUpdateLease(txGen, k),
	))

	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgAcknowledgeLeaseUpdate,
		SimulateMsgAcknowledgeLeaseUpdate(txGen, k, sk),
	))

	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgRejectLeaseUpdate,
		SimulateMsgRejectLeaseUpdate(txGen, k, sk),
	))

	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgCancelLeaseUpdate,
		SimulateMsgCancelLeaseUpdate(txGen, k),
	))

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
		minRequired := fixedFee.Add(minFundingAmount)

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
		var randAmount sdkmath.Int
		if maxFundingAmount.GT(minFundingAmount) {
			randRange := maxFundingAmount.Sub(minFundingAmount).Int64()
			if randRange > 0 {
				randAmount = minFundingAmount.Add(sdkmath.NewInt(int64(r.Intn(int(randRange)))))
			} else {
				randAmount = minFundingAmount
			}
		} else {
			randAmount = minFundingAmount
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

		// Check tenant hasn't exceeded max leases
		params, err := k.GetParams(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get params"), nil, nil
		}

		activeLeaseCount, err := k.CountActiveLeasesByTenant(ctx, tenant.Address.String())
		if err != nil || activeLeaseCount >= params.MaxLeasesPerTenant {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "tenant at max lease limit"), nil, nil
		}

		// Create lease items (1-3 items from same provider)
		numItems := r.Intn(3) + 1

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

// SimulateMsgCreateLeaseForTenant generates a MsgCreateLeaseForTenant with random values.
// This simulates authority creating leases on behalf of tenants (e.g., for migration).
func SimulateMsgCreateLeaseForTenant(txGen client.TxConfig, k keeper.Keeper, sk SKUKeeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgCreateLeaseForTenant{})

		// Get all active SKUs
		allSKUs, err := sk.GetAllSKUs(ctx)
		if err != nil || len(allSKUs) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no SKUs found"), nil, nil
		}

		// Filter to active SKUs
		var activeSKUs []skutypes.SKU
		for _, sku := range allSKUs {
			if sku.Active {
				activeSKUs = append(activeSKUs, sku)
			}
		}

		if len(activeSKUs) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no active SKUs found"), nil, nil
		}

		// Pick a random SKU
		sku := activeSKUs[r.Intn(len(activeSKUs))]

		// Verify provider is active
		provider, err := sk.GetProvider(ctx, sku.ProviderUuid)
		if err != nil || !provider.Active {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "provider not active"), nil, nil
		}

		// Select random tenant
		tenant, _ := simtypes.RandomAcc(r, accs)

		// Check if tenant has credit in the SKU's denom
		skuDenom := sku.BasePrice.Denom

		creditBalance, err := k.GetCreditBalance(ctx, tenant.Address.String(), skuDenom)
		if err != nil || creditBalance.Amount.IsZero() {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "tenant has no credit"), nil, nil
		}

		// Check tenant hasn't exceeded max leases
		params, err := k.GetParams(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get params"), nil, nil
		}

		activeLeaseCount, err := k.CountActiveLeasesByTenant(ctx, tenant.Address.String())
		if err != nil || activeLeaseCount >= params.MaxLeasesPerTenant {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "tenant at max lease limit"), nil, nil
		}

		// Create lease items (1-3 items from same provider)
		numItems := r.Intn(3) + 1

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

		// Skip if the tenant cannot afford the lease (see SimulateMsgCreateLease).
		if !tenantCanAffordLease(ctx, k, tenant.Address.String(), items, providerSKUs[:numItems], params.MinLeaseDuration) {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "tenant cannot afford lease"), nil, nil
		}

		// Use the module authority as sender
		// In simulation, we use the authority address from params
		authority := k.GetAuthority()

		msg := &types.MsgCreateLeaseForTenant{
			Authority: authority,
			Tenant:    tenant.Address.String(),
			Items:     items,
		}

		// For authority messages, we need to find a simulation account that matches
		// Since authority is typically a group policy address, this operation
		// will often result in NoOp in simulation. This is acceptable as it tests
		// the message validation and routing.
		var authorityAcc simtypes.Account
		var found bool
		for _, acc := range accs {
			if acc.Address.String() == authority {
				authorityAcc = acc
				found = true
				break
			}
		}

		if !found {
			// Authority not in simulation accounts - this is expected
			// We can still test message validation by returning NoOp
			return simtypes.NoOpMsg(types.ModuleName, msgType, "authority not in simulation accounts"), nil, nil
		}

		return genAndDeliverTxWithRandFees(r, app, ctx, txGen, authorityAcc, msg, k)
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

		// Find the provider address account
		var sender simtypes.Account
		var found bool
		for _, acc := range accs {
			if acc.Address.String() == provider.Address {
				sender = acc
				found = true
				break
			}
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

		// Find the provider address account
		var sender simtypes.Account
		var found bool
		for _, acc := range accs {
			if acc.Address.String() == provider.Address {
				sender = acc
				found = true
				break
			}
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

		// Find the tenant account
		var sender simtypes.Account
		var found bool
		for _, acc := range accs {
			if acc.Address.String() == lease.Tenant {
				sender = acc
				found = true
				break
			}
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

		// Find the tenant account
		var sender simtypes.Account
		var found bool
		for _, acc := range accs {
			if acc.Address.String() == lease.Tenant {
				sender = acc
				found = true
				break
			}
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
		if err != nil || len(allLeases) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no leases found"), nil, nil
		}

		// Filter to leases that have withdrawable amounts
		var withdrawableLeases []types.Lease
		for _, lease := range allLeases {
			// Calculate withdrawable amount for this lease
			withdrawable := k.CalculateWithdrawableForLease(ctx, lease)
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

	// Find the provider address account
	var sender simtypes.Account
	var found bool
	for _, acc := range accs {
		if acc.Address.String() == provider.Address {
			sender = acc
			found = true
			break
		}
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

	// Find the provider address account
	var sender simtypes.Account
	var found bool
	for _, acc := range accs {
		if acc.Address.String() == provider.Address {
			sender = acc
			found = true
			break
		}
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
		totalRatesPerSecond = totalRatesPerSecond.Add(
			sdk.NewCoin(ratePerSecond.Denom, ratePerSecond.Amount.Mul(sdkmath.NewIntFromUint64(item.Quantity))),
		)
	}

	reservation := types.CalculateLeaseReservationFromRates(totalRatesPerSecond, minLeaseDuration)
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
		balances = balances.Add(bal)
	}

	available := types.GetAvailableCredit(balances, creditAccount.ReservedAmounts)
	for _, res := range reservation {
		if available.AmountOf(res.Denom).LT(res.Amount) {
			return false
		}
	}
	return true
}

// selectLeaseWithPendingUpdate picks a random ACTIVE lease carrying a pending
// manifest update, plus the account that may act on it. accountFor resolves the
// authorised signer from the lease (the tenant for tenant-side verbs, the
// provider address for provider-side ones); returning false means the signer is
// not a simulation account and the lease is skipped.
//
// Returns ok=false with a reason when nothing suitable exists, which every
// caller turns into a NoOpMsg.
func selectLeaseWithPendingUpdate(
	r *rand.Rand,
	ctx sdk.Context,
	k keeper.Keeper,
	accountFor func(types.Lease) (simtypes.Account, bool),
) (types.Lease, simtypes.Account, string, bool) {
	allLeases, err := k.GetAllLeases(ctx)
	if err != nil || len(allLeases) == 0 {
		return types.Lease{}, simtypes.Account{}, "no leases found", false
	}

	var candidates []types.Lease
	for _, lease := range allLeases {
		if lease.State == types.LEASE_STATE_ACTIVE && len(lease.PendingMetaHash) > 0 {
			candidates = append(candidates, lease)
		}
	}
	if len(candidates) == 0 {
		return types.Lease{}, simtypes.Account{}, "no leases with a pending update", false
	}

	// Try candidates in random order: a lease whose signer is not a simulation
	// account would otherwise starve the operation even when other leases are
	// actionable.
	for _, i := range r.Perm(len(candidates)) {
		lease := candidates[i]
		if acc, ok := accountFor(lease); ok {
			return lease, acc, "", true
		}
	}

	return types.Lease{}, simtypes.Account{}, "no pending update with a known signer", false
}

// findSimAccount returns the simulation account matching a bech32 address.
func findSimAccount(accs []simtypes.Account, address string) (simtypes.Account, bool) {
	for _, acc := range accs {
		if acc.Address.String() == address {
			return acc, true
		}
	}
	return simtypes.Account{}, false
}

// SimulateMsgUpdateLease requests a new deployment manifest hash on an ACTIVE
// lease. This is the only operation that puts a lease into the pending-update
// state, so without it the PendingUpdateIndex, the SetLease normalisation and
// the other three lease-update handlers are all unreachable under simulation.
func SimulateMsgUpdateLease(txGen client.TxConfig, k keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgUpdateLease{})

		allLeases, err := k.GetAllLeases(ctx)
		if err != nil || len(allLeases) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no leases found"), nil, nil
		}

		var activeLeases []types.Lease
		for _, lease := range allLeases {
			if lease.State == types.LEASE_STATE_ACTIVE {
				activeLeases = append(activeLeases, lease)
			}
		}
		if len(activeLeases) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no active leases found"), nil, nil
		}

		var (
			lease  types.Lease
			sender simtypes.Account
			found  bool
		)
		for _, i := range r.Perm(len(activeLeases)) {
			if acc, ok := findSimAccount(accs, activeLeases[i].Tenant); ok {
				lease, sender, found = activeLeases[i], acc, true
				break
			}
		}
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "tenant account not found in simulation"), nil, nil
		}

		// A random 32-byte hash, standing in for SHA-256 of a manifest. Drawn
		// fresh each time so re-requests genuinely supersede rather than
		// hitting the identical-hash no-op path.
		metaHash := make([]byte, 32)
		if _, err := r.Read(metaHash); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to generate meta_hash"), nil, nil
		}

		msg := &types.MsgUpdateLease{
			Sender:    sender.Address.String(),
			LeaseUuid: lease.Uuid,
			MetaHash:  metaHash,
		}

		return genAndDeliverTxWithRandFees(r, app, ctx, txGen, sender, msg, k)
	}
}

// SimulateMsgAcknowledgeLeaseUpdate promotes a lease's pending update to its
// committed meta_hash, signed by the lease's provider.
func SimulateMsgAcknowledgeLeaseUpdate(txGen client.TxConfig, k keeper.Keeper, sk SKUKeeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgAcknowledgeLeaseUpdate{})

		lease, sender, reason, ok := selectLeaseWithPendingUpdate(r, ctx, k, func(l types.Lease) (simtypes.Account, bool) {
			provider, err := sk.GetProvider(ctx, l.ProviderUuid)
			if err != nil {
				return simtypes.Account{}, false
			}
			return findSimAccount(accs, provider.Address)
		})
		if !ok {
			return simtypes.NoOpMsg(types.ModuleName, msgType, reason), nil, nil
		}

		// The hash must match the lease's current pending value; anything else
		// is rejected by the supersession guard.
		msg := &types.MsgAcknowledgeLeaseUpdate{
			Sender:    sender.Address.String(),
			LeaseUuid: lease.Uuid,
			MetaHash:  lease.PendingMetaHash,
		}

		return genAndDeliverTxWithRandFees(r, app, ctx, txGen, sender, msg, k)
	}
}

// SimulateMsgRejectLeaseUpdate discards a pending update, signed by the lease's
// provider. The committed meta_hash is left untouched.
func SimulateMsgRejectLeaseUpdate(txGen client.TxConfig, k keeper.Keeper, sk SKUKeeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgRejectLeaseUpdate{})

		lease, sender, reason, ok := selectLeaseWithPendingUpdate(r, ctx, k, func(l types.Lease) (simtypes.Account, bool) {
			provider, err := sk.GetProvider(ctx, l.ProviderUuid)
			if err != nil {
				return simtypes.Account{}, false
			}
			return findSimAccount(accs, provider.Address)
		})
		if !ok {
			return simtypes.NoOpMsg(types.ModuleName, msgType, reason), nil, nil
		}

		msg := &types.MsgRejectLeaseUpdate{
			Sender:    sender.Address.String(),
			LeaseUuid: lease.Uuid,
			MetaHash:  lease.PendingMetaHash,
			Reason:    simtypes.RandStringOfLength(r, r.Intn(64)),
		}

		return genAndDeliverTxWithRandFees(r, app, ctx, txGen, sender, msg, k)
	}
}

// SimulateMsgCancelLeaseUpdate withdraws a pending update, signed by the tenant.
func SimulateMsgCancelLeaseUpdate(txGen client.TxConfig, k keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgCancelLeaseUpdate{})

		lease, sender, reason, ok := selectLeaseWithPendingUpdate(r, ctx, k, func(l types.Lease) (simtypes.Account, bool) {
			return findSimAccount(accs, l.Tenant)
		})
		if !ok {
			return simtypes.NoOpMsg(types.ModuleName, msgType, reason), nil, nil
		}

		msg := &types.MsgCancelLeaseUpdate{
			Sender:    sender.Address.String(),
			LeaseUuid: lease.Uuid,
		}

		return genAndDeliverTxWithRandFees(r, app, ctx, txGen, sender, msg, k)
	}
}
