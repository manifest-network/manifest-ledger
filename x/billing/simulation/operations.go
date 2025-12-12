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
	OpWeightMsgCloseLease           = "op_weight_msg_billing_close_lease"             //nolint:gosec
	OpWeightMsgWithdraw             = "op_weight_msg_billing_withdraw"                //nolint:gosec
	OpWeightMsgWithdrawAll          = "op_weight_msg_billing_withdraw_all"            //nolint:gosec

	DefaultWeightMsgFundCredit           = 50
	DefaultWeightMsgCreateLease          = 40
	DefaultWeightMsgCreateLeaseForTenant = 10 // Lower weight since it's authority-only
	DefaultWeightMsgCloseLease           = 20
	DefaultWeightMsgWithdraw             = 30
	DefaultWeightMsgWithdrawAll          = 10
)

// SKUKeeper defines the expected SKU keeper interface for simulation.
type SKUKeeper interface {
	GetAllSKUs(ctx context.Context) ([]skutypes.SKU, error)
	GetProvider(ctx context.Context, id uint64) (skutypes.Provider, error)
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

	var weightMsgCloseLease int
	appParams.GetOrGenerate(OpWeightMsgCloseLease, &weightMsgCloseLease, nil, func(_ *rand.Rand) {
		weightMsgCloseLease = DefaultWeightMsgCloseLease
	})

	var weightMsgWithdraw int
	appParams.GetOrGenerate(OpWeightMsgWithdraw, &weightMsgWithdraw, nil, func(_ *rand.Rand) {
		weightMsgWithdraw = DefaultWeightMsgWithdraw
	})

	var weightMsgWithdrawAll int
	appParams.GetOrGenerate(OpWeightMsgWithdrawAll, &weightMsgWithdrawAll, nil, func(_ *rand.Rand) {
		weightMsgWithdrawAll = DefaultWeightMsgWithdrawAll
	})

	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgFundCredit,
		SimulateMsgFundCredit(txGen, k),
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
		weightMsgCloseLease,
		SimulateMsgCloseLease(txGen, k),
	))

	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgWithdraw,
		SimulateMsgWithdraw(txGen, k, sk),
	))

	operations = append(operations, simulation.NewWeightedOperation(
		weightMsgWithdrawAll,
		SimulateMsgWithdrawAll(txGen, k, sk),
	))

	return operations
}

// SimulateMsgFundCredit generates a MsgFundCredit with random values.
func SimulateMsgFundCredit(txGen client.TxConfig, k keeper.Keeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgFundCredit{})

		// Select random sender
		sender, _ := simtypes.RandomAcc(r, accs)

		// Select random tenant (can be same as sender or different)
		tenant, _ := simtypes.RandomAcc(r, accs)

		// Get billing params to use correct denom
		params, err := k.GetParams(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get params"), nil, nil
		}

		// Get total spendable balance in billing denom
		spendableCoins := k.GetBankKeeper().SpendableCoins(ctx, sender.Address)
		senderBalance := spendableCoins.AmountOf(params.Denom)
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

		amount := sdk.NewCoin(params.Denom, randAmount)
		fees := sdk.NewCoins(sdk.NewCoin(params.Denom, fixedFee))

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
		provider, err := sk.GetProvider(ctx, sku.ProviderId)
		if err != nil || !provider.Active {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "provider not active"), nil, nil
		}

		// Select random tenant
		tenant, _ := simtypes.RandomAcc(r, accs)

		// Check if tenant has enough credit
		params, err := k.GetParams(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get params"), nil, nil
		}

		creditBalance, err := k.GetCreditBalance(ctx, tenant.Address.String(), params.Denom)
		if err != nil || creditBalance.Amount.LT(params.MinCreditBalance) {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "tenant has insufficient credit"), nil, nil
		}

		// Check tenant hasn't exceeded max leases
		activeLeaseCount, err := k.CountActiveLeasesByTenant(ctx, tenant.Address.String())
		if err != nil || activeLeaseCount >= params.MaxLeasesPerTenant {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "tenant at max lease limit"), nil, nil
		}

		// Create lease items (1-3 items from same provider)
		numItems := r.Intn(3) + 1

		// Get all SKUs from the same provider
		var providerSKUs []skutypes.SKU
		for _, s := range activeSKUs {
			if s.ProviderId == sku.ProviderId {
				providerSKUs = append(providerSKUs, s)
			}
		}

		if len(providerSKUs) < numItems {
			numItems = len(providerSKUs)
		}

		// Shuffle and pick unique SKUs
		r.Shuffle(len(providerSKUs), func(i, j int) {
			providerSKUs[i], providerSKUs[j] = providerSKUs[j], providerSKUs[i]
		})

		items := make([]types.LeaseItemInput, numItems)
		for i := 0; i < numItems; i++ {
			items[i] = types.LeaseItemInput{
				SkuId:    providerSKUs[i].Id,
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
		provider, err := sk.GetProvider(ctx, sku.ProviderId)
		if err != nil || !provider.Active {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "provider not active"), nil, nil
		}

		// Select random tenant
		tenant, _ := simtypes.RandomAcc(r, accs)

		// Check if tenant has enough credit
		params, err := k.GetParams(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get params"), nil, nil
		}

		creditBalance, err := k.GetCreditBalance(ctx, tenant.Address.String(), params.Denom)
		if err != nil || creditBalance.Amount.LT(params.MinCreditBalance) {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "tenant has insufficient credit"), nil, nil
		}

		// Check tenant hasn't exceeded max leases
		activeLeaseCount, err := k.CountActiveLeasesByTenant(ctx, tenant.Address.String())
		if err != nil || activeLeaseCount >= params.MaxLeasesPerTenant {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "tenant at max lease limit"), nil, nil
		}

		// Create lease items (1-3 items from same provider)
		numItems := r.Intn(3) + 1

		// Get all SKUs from the same provider
		var providerSKUs []skutypes.SKU
		for _, s := range activeSKUs {
			if s.ProviderId == sku.ProviderId {
				providerSKUs = append(providerSKUs, s)
			}
		}

		if len(providerSKUs) < numItems {
			numItems = len(providerSKUs)
		}

		// Shuffle and pick unique SKUs
		r.Shuffle(len(providerSKUs), func(i, j int) {
			providerSKUs[i], providerSKUs[j] = providerSKUs[j], providerSKUs[i]
		})

		items := make([]types.LeaseItemInput, numItems)
		for i := 0; i < numItems; i++ {
			items[i] = types.LeaseItemInput{
				SkuId:    providerSKUs[i].Id,
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
			Sender:  sender.Address.String(),
			LeaseId: lease.Id,
		}

		return genAndDeliverTxWithRandFees(r, app, ctx, txGen, sender, msg, k)
	}
}

// SimulateMsgWithdraw generates a MsgWithdraw with random values.
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
			if withdrawable.IsPositive() {
				withdrawableLeases = append(withdrawableLeases, lease)
			}
		}

		if len(withdrawableLeases) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no leases with withdrawable amount"), nil, nil
		}

		// Pick a random lease with withdrawable amount
		lease := withdrawableLeases[r.Intn(len(withdrawableLeases))]

		// Get provider to find the provider address
		provider, err := sk.GetProvider(ctx, lease.ProviderId)
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

		msg := &types.MsgWithdraw{
			Sender:  sender.Address.String(),
			LeaseId: lease.Id,
		}

		return genAndDeliverTxWithRandFees(r, app, ctx, txGen, sender, msg, k)
	}
}

// SimulateMsgWithdrawAll generates a MsgWithdrawAll with random values.
func SimulateMsgWithdrawAll(txGen client.TxConfig, k keeper.Keeper, sk SKUKeeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgWithdrawAll{})

		// Get all providers
		allProviders, err := sk.GetAllProviders(ctx)
		if err != nil || len(allProviders) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no providers found"), nil, nil
		}

		// Filter to providers that have withdrawable amounts
		var withdrawableProviders []skutypes.Provider
		for _, provider := range allProviders {
			// Check if this provider has any withdrawable amount by checking their leases
			leases, err := k.GetLeasesByProviderID(ctx, provider.Id)
			if err != nil {
				continue
			}

			hasWithdrawable := false
			for _, lease := range leases {
				withdrawable := k.CalculateWithdrawableForLease(ctx, lease)
				if withdrawable.IsPositive() {
					hasWithdrawable = true
					break
				}
			}

			if hasWithdrawable {
				withdrawableProviders = append(withdrawableProviders, provider)
			}
		}

		if len(withdrawableProviders) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no providers with withdrawable amount"), nil, nil
		}

		// Pick a random provider with withdrawable amount
		provider := withdrawableProviders[r.Intn(len(withdrawableProviders))]

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

		msg := &types.MsgWithdrawAll{
			Sender:     sender.Address.String(),
			ProviderId: provider.Id,
		}

		return genAndDeliverTxWithRandFees(r, app, ctx, txGen, sender, msg, k)
	}
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
