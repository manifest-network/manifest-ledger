package simulation

import (
	"context"
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

	DefaultWeightMsgFundCredit           = 50
	DefaultWeightMsgCreateLease          = 40
	DefaultWeightMsgCreateLeaseForTenant = 10 // Lower weight since it's authority-only
	DefaultWeightMsgAcknowledgeLease     = 35 // High weight to process pending leases
	DefaultWeightMsgRejectLease          = 10 // Lower weight for rejections
	DefaultWeightMsgCancelLease          = 10 // Lower weight for cancellations
	DefaultWeightMsgCloseLease           = 20
	DefaultWeightMsgWithdraw             = 30
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

		items := make([]types.LeaseItemInput, numItems)
		for i := range numItems {
			items[i] = types.LeaseItemInput{
				SkuUuid:  providerSKUs[i].Uuid,
				Quantity: uint64(r.Intn(10) + 1), //nolint:gosec
			}
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

		items := make([]types.LeaseItemInput, numItems)
		for i := range numItems {
			items[i] = types.LeaseItemInput{
				SkuUuid:  providerSKUs[i].Uuid,
				Quantity: uint64(r.Intn(10) + 1), //nolint:gosec
			}
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

	// Build map of provider UUIDs with withdrawable leases
	providerUUIDs := make(map[string]bool)
	for _, lease := range withdrawableLeases {
		providerUUIDs[lease.ProviderUuid] = true
	}

	if len(providerUUIDs) == 0 {
		return simtypes.NoOpMsg(types.ModuleName, msgType, "no providers with withdrawable leases"), nil, nil
	}

	// Convert to slice and pick random provider
	uuids := make([]string, 0, len(providerUUIDs))
	for uuid := range providerUUIDs {
		uuids = append(uuids, uuid)
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
