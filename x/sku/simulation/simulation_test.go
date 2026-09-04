package simulation

import (
	"bytes"
	"errors"
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"github.com/manifest-network/manifest-ledger/x/sku/keeper"
	"github.com/manifest-network/manifest-ledger/x/sku/types"
)

func TestWeightedOperationsRegistersEveryDirectSKUMessage(t *testing.T) {
	operations := WeightedOperations(simtypes.AppParams{}, nil, nil, keeper.Keeper{})
	require.Len(t, operations, 6)
	require.Equal(t, []int{
		DefaultWeightMsgCreateProvider,
		DefaultWeightMsgUpdateProvider,
		DefaultWeightMsgDeactivateProvider,
		DefaultWeightMsgCreateSKU,
		DefaultWeightMsgUpdateSKU,
		DefaultWeightMsgDeactivateSKU,
	}, weightedOperationWeights(operations))
}

func TestAuthorizedSimulationAccountsUsesDecodedIdentityAndSliceOrder(t *testing.T) {
	accounts := testSimulationAccounts(3)
	params := types.DefaultParams()
	params.AllowedList = []string{strings.ToUpper(accounts[1].Address.String())}

	selected, err := authorizedSimulationAccounts(
		accounts,
		strings.ToUpper(accounts[0].Address.String()),
		params,
	)
	require.NoError(t, err)
	require.Len(t, selected, 2)
	require.True(t, selected[0].Address.Equals(accounts[0].Address))
	require.True(t, selected[1].Address.Equals(accounts[1].Address))
}

func TestSimulationAllowedListIsDeterministicUniqueAndBounded(t *testing.T) {
	accounts := append(testSimulationAccounts(5), testSimulationAccounts(1)[0])

	first := simulationAllowedList(rand.New(rand.NewSource(7)), accounts)  //nolint:gosec
	second := simulationAllowedList(rand.New(rand.NewSource(7)), accounts) //nolint:gosec
	require.Equal(t, first, second)
	require.NotEmpty(t, first)
	require.LessOrEqual(t, len(first), maxSimulationManagers)

	params := types.DefaultParams()
	params.AllowedList = first
	require.NoError(t, params.Validate())
}

func TestProvidersRequiringDeactivationIncludesCascadeContinuation(t *testing.T) {
	providers := []types.Provider{
		{Uuid: "active", Active: true},
		{Uuid: "continuation"},
		{Uuid: "complete"},
	}
	inspected := make([]string, 0, 2)
	selected, err := providersRequiringDeactivation(providers, func(providerUUID string) (bool, error) {
		inspected = append(inspected, providerUUID)
		return providerUUID == "continuation", nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"continuation", "complete"}, inspected)
	require.Len(t, selected, 2)
	require.Equal(t, []string{"active", "continuation"}, []string{selected[0].Uuid, selected[1].Uuid})

	expectedErr := errors.New("inspect failed")
	_, err = providersRequiringDeactivation(providers[1:], func(string) (bool, error) {
		return false, expectedErr
	})
	require.ErrorIs(t, err, expectedErr)
}

func TestSimulationDeactivateLimitIsBoundedAndExercisesDefault(t *testing.T) {
	var sawDefault, sawExplicit bool
	for seed := int64(0); seed < 64; seed++ {
		limit := simulationDeactivateLimit(rand.New(rand.NewSource(seed))) //nolint:gosec
		require.LessOrEqual(t, limit, maxSimulationDeactivateSKULimit)
		if limit == 0 {
			sawDefault = true
		} else {
			sawExplicit = true
		}
	}
	require.True(t, sawDefault)
	require.True(t, sawExplicit)
}

func TestSimulationProviderAPIURLUpdateExercisesPresenceModes(t *testing.T) {
	var sawPreserve, sawSet, sawClear bool
	for seed := int64(0); seed < 64; seed++ {
		firstURL, firstClear := simulationProviderAPIURLUpdate(rand.New(rand.NewSource(seed)))   //nolint:gosec
		secondURL, secondClear := simulationProviderAPIURLUpdate(rand.New(rand.NewSource(seed))) //nolint:gosec
		require.Equal(t, firstURL, secondURL)
		require.Equal(t, firstClear, secondClear)
		require.False(t, firstClear && firstURL != "", "clear and set must never be combined")
		switch {
		case firstClear:
			sawClear = true
		case firstURL != "":
			sawSet = true
			require.NoError(t, types.ValidateAPIURL(firstURL))
		default:
			sawPreserve = true
		}
	}
	require.True(t, sawPreserve)
	require.True(t, sawSet)
	require.True(t, sawClear)
}

func TestUpdateParamsIsDeliberatelyExcludedFromSimulation(t *testing.T) {
	require.Empty(t, ProposalMsgs())
}

func testSimulationAccounts(count int) []simtypes.Account {
	accounts := make([]simtypes.Account, count)
	for i := range count {
		accounts[i] = simtypes.Account{
			Address: sdk.AccAddress(bytes.Repeat([]byte{byte(i + 1)}, 20)), //nolint:gosec // test-only bounded conversion
		}
	}
	return accounts
}

func weightedOperationWeights(operations []simtypes.WeightedOperation) []int {
	weights := make([]int, len(operations))
	for i, operation := range operations {
		weights[i] = operation.Weight()
	}
	return weights
}
