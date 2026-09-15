package keeper

import (
	"bytes"
	"strings"
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
	for name, mutate := range map[string]func(*types.CreditAccount){
		"orphan aggregate": func(account *types.CreditAccount) {
			account.ReservedAmounts = sdk.NewCoins(sdk.NewInt64Coin("umfx", 1))
		},
		"orphan cohort": func(account *types.CreditAccount) {
			account.UnattributedReservedAmounts = sdk.NewCoins(sdk.NewInt64Coin("umfx", 1))
		},
		"active count":       func(account *types.CreditAccount) { account.ActiveLeaseCount = 1 },
		"pending count":      func(account *types.CreditAccount) { account.PendingLeaseCount = 1 },
		"unattributed count": func(account *types.CreditAccount) { account.UnattributedLeaseCount = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			for _, aliases := range []bool{false, true} {
				genesis := types.DefaultGenesis()
				tenant := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
				creditAddress := types.DeriveCreditAddress(tenant)
				genesis.CreditAccounts = []types.CreditAccount{{Tenant: tenant.String(), CreditAddress: creditAddress.String()}}
				mutate(&genesis.CreditAccounts[0])
				if aliases {
					genesis.Params.AllowedList = []string{tenant.String(), strings.ToUpper(tenant.String())}
				}
				before := genesis.String()
				bankGenesis := banktypes.DefaultGenesisState()
				bankGenesis.Balances = []banktypes.Balance{{Address: creditAddress.String(), Coins: sdk.NewCoins(sdk.NewInt64Coin("umfx", 100))}}
				report, err := BuildReservationMigrationPreflight(time.Unix(1, 0), genesis, bankGenesis, nil)
				require.ErrorContains(t, err, "no reservation format marker")
				require.NotErrorIs(t, err, types.ErrInvalidParams, "supported aliases must not mask ambiguous accounting")
				require.Empty(t, report.BillingState)
				require.Equal(t, before, genesis.String(), "failed audits must not repair caller accounting or params")
			}
		})
	}
}

func TestLeaseFreePreflightAcceptsImportSafeAllowedListAliases(t *testing.T) {
	genesis := types.DefaultGenesis()
	tenant := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	creditAddress := types.DeriveCreditAddress(tenant)
	genesis.CreditAccounts = []types.CreditAccount{{Tenant: tenant.String(), CreditAddress: creditAddress.String()}}
	genesis.Params.AllowedList = []string{strings.ToUpper(tenant.String()), tenant.String(), tenant.String()}
	before := genesis.String()
	bankGenesis := banktypes.DefaultGenesisState()
	bankGenesis.Balances = []banktypes.Balance{{Address: creditAddress.String(), Coins: sdk.NewCoins(sdk.NewInt64Coin("umfx", 100))}}
	accounts := authtypes.GenesisAccounts{authtypes.NewBaseAccountWithAddress(creditAddress)}

	prepared, err := genesis.PrepareForImport()
	require.NoError(t, err)
	require.Equal(t, []string{tenant.String()}, prepared.Params.AllowedList)
	expected, err := BuildReservationMigrationPreflight(time.Unix(1, 0), prepared, bankGenesis, accounts)
	require.NoError(t, err)
	report, err := BuildReservationMigrationPreflight(time.Unix(1, 0), genesis, bankGenesis, accounts)
	require.NoError(t, err)
	require.Equal(t, expected, report, "preflight must accept the same harmless aliases as import")
	require.Equal(t, ReservationPreflightStateLeaseFree, report.BillingState)
	require.Equal(t, ReservationPreflightPathNone, report.MigrationPath)
	require.Zero(t, report.ReservationChangeTenantCount)
	require.Equal(t, before, genesis.String(), "preflight must not normalize its caller's allowed list")
}
