package simulation

import (
	"bytes"
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"github.com/manifest-network/manifest-ledger/x/billing/keeper"
	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestWeightedOperationsRegistersDirectMessagesAndExcludesAdminMigration(t *testing.T) {
	operations := WeightedOperations(simtypes.AppParams{}, nil, nil, keeper.Keeper{}, nil)
	require.Len(t, operations, 8)
	require.Equal(t, []int{
		DefaultWeightMsgFundCredit,
		DefaultWeightMsgCreateLease,
		DefaultWeightMsgAcknowledgeLease,
		DefaultWeightMsgRejectLease,
		DefaultWeightMsgCancelLease,
		DefaultWeightMsgCloseLease,
		DefaultWeightMsgWithdraw,
		DefaultWeightMsgSetItemCustomDomain,
	}, billingWeightedOperationWeights(operations))
}

func TestEligibleCustomDomainTargetsUsesLiveAddressableItemsAndDecodedIdentity(t *testing.T) {
	accounts := billingTestSimulationAccounts(2)
	tenant := accounts[0].Address.String()
	lease := func(uuid string, state types.LeaseState, owner string, items ...types.LeaseItem) types.Lease {
		return types.Lease{Uuid: uuid, State: state, Tenant: owner, Items: items}
	}
	leases := []types.Lease{
		lease("01912345-6789-7abc-8def-0123456789a1", types.LEASE_STATE_PENDING, strings.ToUpper(tenant),
			types.LeaseItem{ServiceName: "web"},
			types.LeaseItem{ServiceName: "db", CustomDomain: "db.example.test"},
		),
		lease("01912345-6789-7abc-8def-0123456789a2", types.LEASE_STATE_ACTIVE, tenant,
			types.LeaseItem{},
		),
		lease("01912345-6789-7abc-8def-0123456789a3", types.LEASE_STATE_CLOSED, tenant,
			types.LeaseItem{ServiceName: "closed"},
		),
		lease("01912345-6789-7abc-8def-0123456789a4", types.LEASE_STATE_PENDING, accounts[1].Address.String(),
			types.LeaseItem{},
			types.LeaseItem{},
		),
	}

	targets, err := eligibleCustomDomainTargets(leases, accounts[:1])
	require.NoError(t, err)
	require.Len(t, targets, 3)
	require.Equal(t, "web", targets[0].serviceName)
	require.Equal(t, "db", targets[1].serviceName)
	require.Equal(t, "db.example.test", targets[1].currentDomain)
	require.Empty(t, targets[2].serviceName, "one-item legacy lease uses the empty addressing key")
	for _, target := range targets {
		require.True(t, target.signer.Address.Equals(accounts[0].Address))
	}
}

func TestSelectCustomDomainMutationExercisesSetReplaceAndClear(t *testing.T) {
	empty := customDomainSimulationTarget{leaseUUID: "empty"}
	target, clearDomain, found := selectCustomDomainMutation(rand.New(rand.NewSource(1)), []customDomainSimulationTarget{empty}) //nolint:gosec
	require.True(t, found)
	require.False(t, clearDomain)
	require.Equal(t, "empty", target.leaseUUID)

	claimed := customDomainSimulationTarget{leaseUUID: "claimed", currentDomain: "claimed.example.test"}
	var sawReplace, sawClear bool
	for seed := int64(0); seed < 32; seed++ {
		target, clearDomain, found = selectCustomDomainMutation(
			rand.New(rand.NewSource(seed)), //nolint:gosec
			[]customDomainSimulationTarget{claimed},
		)
		require.True(t, found)
		require.Equal(t, "claimed", target.leaseUUID)
		if clearDomain {
			sawClear = true
		} else {
			sawReplace = true
		}
	}
	require.True(t, sawReplace)
	require.True(t, sawClear)
}

func TestSelectCustomDomainSignerExercisesTenantAndAllowedAdmin(t *testing.T) {
	accounts := billingTestSimulationAccounts(3)
	params := types.DefaultParams()
	params.AllowedList = []string{strings.ToUpper(accounts[1].Address.String())}

	var sawTenant, sawAdmin bool
	for seed := int64(0); seed < 32; seed++ {
		signer, found := selectCustomDomainSigner(
			rand.New(rand.NewSource(seed)), //nolint:gosec
			accounts[0],
			append(accounts, accounts[1]),
			params,
		)
		require.True(t, found)
		switch {
		case signer.Address.Equals(accounts[0].Address):
			sawTenant = true
		case signer.Address.Equals(accounts[1].Address):
			sawAdmin = true
		default:
			t.Fatalf("selected unauthorized signer %s", signer.Address)
		}
	}
	require.True(t, sawTenant)
	require.True(t, sawAdmin)
}

func TestSimulationDomainCandidateIsDeterministicAndValid(t *testing.T) {
	firstRand := rand.New(rand.NewSource(23))  //nolint:gosec
	secondRand := rand.New(rand.NewSource(23)) //nolint:gosec
	for range 100 {
		first := simulationDomainCandidate(firstRand)
		second := simulationDomainCandidate(secondRand)
		require.Equal(t, first, second)
		require.NoError(t, types.IsValidFQDN(first))
	}
}

func TestSimulationLeaseItemCountHonorsCurrentParameter(t *testing.T) {
	_, ok := simulationLeaseItemCount(rand.New(rand.NewSource(1)), 0) //nolint:gosec
	require.False(t, ok)

	for _, maximum := range []uint64{1, 2, 3, types.MaxItemsPerLeaseHardLimit} {
		r := rand.New(rand.NewSource(19)) //nolint:gosec
		expectedMaximum := 3
		if maximum < 3 {
			expectedMaximum = int(maximum) //nolint:gosec // maximum is 1 or 2 in this branch
		}
		for range 100 {
			count, ok := simulationLeaseItemCount(r, maximum)
			require.True(t, ok)
			require.GreaterOrEqual(t, count, 1)
			require.LessOrEqual(t, count, expectedMaximum)
		}
	}
}

func TestRandomParamsAreValidAndBounded(t *testing.T) {
	for seed := int64(0); seed < 128; seed++ {
		params := randomParams(rand.New(rand.NewSource(seed))) //nolint:gosec
		require.NoError(t, params.Validate())
		require.GreaterOrEqual(t, params.MaxLeasesPerTenant, uint64(10))
		require.LessOrEqual(t, params.MaxLeasesPerTenant, uint64(200))
		require.GreaterOrEqual(t, params.MaxItemsPerLease, uint64(5))
		require.LessOrEqual(t, params.MaxItemsPerLease, uint64(50))
		require.GreaterOrEqual(t, params.MinLeaseDuration, uint64(3600))
		require.LessOrEqual(t, params.MinLeaseDuration, uint64(24*3600))
		require.GreaterOrEqual(t, params.PendingTimeout, types.MinPendingTimeout)
		require.LessOrEqual(t, params.PendingTimeout, uint64(3600))
		require.LessOrEqual(t, len(params.ReservedDomainSuffixes), 2)
	}
}

func TestSimulationBillingAllowedListIsDeterministicUniqueAndBounded(t *testing.T) {
	accounts := append(billingTestSimulationAccounts(5), billingTestSimulationAccounts(1)[0])
	first := simulationBillingAllowedList(rand.New(rand.NewSource(29)), accounts)  //nolint:gosec
	second := simulationBillingAllowedList(rand.New(rand.NewSource(29)), accounts) //nolint:gosec
	require.Equal(t, first, second)
	require.NotEmpty(t, first)
	require.LessOrEqual(t, len(first), maxSimulationBillingAdmins)

	params := types.DefaultParams()
	params.AllowedList = first
	require.NoError(t, params.Validate())
}

func TestUpdateParamsIsDeliberatelyExcludedFromSimulation(t *testing.T) {
	require.Empty(t, ProposalMsgs())
}

func billingTestSimulationAccounts(count int) []simtypes.Account {
	accounts := make([]simtypes.Account, count)
	for i := range count {
		accounts[i] = simtypes.Account{
			Address: sdk.AccAddress(bytes.Repeat([]byte{byte(i + 101)}, 20)), //nolint:gosec // test-only bounded conversion
		}
	}
	return accounts
}

func billingWeightedOperationWeights(operations []simtypes.WeightedOperation) []int {
	weights := make([]int, len(operations))
	for i, operation := range operations {
		weights[i] = operation.Weight()
	}
	return weights
}
