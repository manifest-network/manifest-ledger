package keeper

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestLeaseFreePreflightDoesNotInventSourceVersion(t *testing.T) {
	for _, funded := range []bool{false, true} {
		genesis := types.DefaultGenesis()
		bankGenesis := banktypes.DefaultGenesisState()
		var accounts authtypes.GenesisAccounts
		if funded {
			tenant := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
			creditAddress := types.DeriveCreditAddress(tenant)
			genesis.CreditAccounts = []types.CreditAccount{{Tenant: tenant.String(), CreditAddress: creditAddress.String()}}
			bankGenesis.Balances = []banktypes.Balance{{Address: creditAddress.String(), Coins: sdk.NewCoins(sdk.NewInt64Coin("umfx", 100))}}
			accounts = authtypes.GenesisAccounts{authtypes.NewBaseAccountWithAddress(creditAddress)}
		}
		report, err := BuildReservationMigrationPreflight(time.Unix(1, 0), genesis, bankGenesis, accounts)
		require.NoError(t, err)
		require.Equal(t, ReservationPreflightStateLeaseFree, report.BillingState)
		require.Equal(t, ReservationPreflightPathNone, report.MigrationPath)
		require.Zero(t, report.ReservationChangeTenantCount)
	}
}

func TestLeaseFreePreflightRejectsAmbiguousAccounting(t *testing.T) {
	for _, mutate := range []func(*types.CreditAccount){
		func(account *types.CreditAccount) {
			account.ReservedAmounts = sdk.NewCoins(sdk.NewInt64Coin("umfx", 1))
		},
		func(account *types.CreditAccount) {
			account.UnattributedReservedAmounts = sdk.NewCoins(sdk.NewInt64Coin("umfx", 1))
		},
		func(account *types.CreditAccount) { account.ActiveLeaseCount = 1 },
		func(account *types.CreditAccount) { account.PendingLeaseCount = 1 },
		func(account *types.CreditAccount) { account.UnattributedLeaseCount = 1 },
	} {
		genesis := types.DefaultGenesis()
		tenant := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
		creditAddress := types.DeriveCreditAddress(tenant)
		genesis.CreditAccounts = []types.CreditAccount{{Tenant: tenant.String(), CreditAddress: creditAddress.String()}}
		mutate(&genesis.CreditAccounts[0])
		bankGenesis := banktypes.DefaultGenesisState()
		bankGenesis.Balances = []banktypes.Balance{{Address: creditAddress.String(), Coins: sdk.NewCoins(sdk.NewInt64Coin("umfx", 100))}}
		report, err := BuildReservationMigrationPreflight(time.Unix(1, 0), genesis, bankGenesis, nil)
		require.ErrorContains(t, err, "no reservation format marker")
		require.Empty(t, report.BillingState)
	}
}
