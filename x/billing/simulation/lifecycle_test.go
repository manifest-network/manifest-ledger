package simulation

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestAllowedBillingSimulationAccountsUsesDecodedIdentityAndAccountOrder(t *testing.T) {
	accounts := billingTestSimulationAccounts(3)
	params := types.DefaultParams()
	params.AllowedList = []string{strings.ToUpper(accounts[2].Address.String()), accounts[0].Address.String()}
	allowed := allowedBillingSimulationAccounts(accounts, params)
	require.Equal(t, []string{accounts[0].Address.String(), accounts[2].Address.String()}, []string{allowed[0].Address.String(), allowed[1].Address.String()})
	params.AllowedList = nil
	require.Empty(t, allowedBillingSimulationAccounts(accounts, params))
}

func TestSimulationLeaseBatchExercisesSharedTenantBatchesWithinCapacity(t *testing.T) {
	seed := types.Lease{Uuid: "a", Tenant: "tenant", ProviderUuid: "provider", State: types.LEASE_STATE_PENDING}
	leases := []types.Lease{
		seed,
		{Uuid: "b", Tenant: seed.Tenant, ProviderUuid: seed.ProviderUuid, State: seed.State},
		{Uuid: "c", Tenant: seed.Tenant, ProviderUuid: seed.ProviderUuid, State: seed.State},
		{Uuid: "d", Tenant: "another tenant", ProviderUuid: seed.ProviderUuid, State: seed.State},
		{Uuid: "e", Tenant: seed.Tenant, ProviderUuid: "another provider", State: seed.State},
		{Uuid: "f", Tenant: seed.Tenant, ProviderUuid: seed.ProviderUuid, State: types.LEASE_STATE_ACTIVE},
	}
	for _, maximum := range []uint64{1, 2, types.MaxBatchLeaseSize} {
		sizes := make(map[int]bool)
		for randomSeed := range int64(100) {
			batch := simulationLeaseBatch(rand.New(rand.NewSource(randomSeed)), leases, seed, maximum) //nolint:gosec // deterministic simulation PRNG
			require.Contains(t, batch, seed.Uuid)
			require.Subset(t, []string{"a", "b", "c"}, batch)
			require.LessOrEqual(t, len(batch), int(min(maximum, 3))) //nolint:gosec // bounded to [1, 3]
			require.Len(t, uniqueStrings(batch), len(batch))
			sizes[len(batch)] = true
		}
		for size := 1; size <= int(min(maximum, 3)); size++ { //nolint:gosec // bounded to [1, 3]
			require.True(t, sizes[size], "batch size %d must be exercised", size)
		}
	}
}

func uniqueStrings(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
