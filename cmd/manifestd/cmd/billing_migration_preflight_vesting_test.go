package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/manifest-network/manifest-ledger/app/params"
	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestBillingMigrationPreflightRequiresExplicitTimeAndAuth(t *testing.T) {
	cmd := newBillingMigrationPreflightCmd()
	cmd.SetArgs([]string{"unused.json"})
	require.ErrorContains(t, cmd.Execute(), `required flag(s) "at" not set`)
	encoding := params.MakeEncodingConfig()
	var output bytes.Buffer
	require.ErrorContains(t, writeBillingMigrationPreflight(encoding.Codec, bytes.NewReader(nil), &output, time.Time{}), "explicit non-zero planner time")
	require.Empty(t, output.String())
	for _, auth := range []string{"", `,"auth":null`} {
		document := `{"chain_id":"test","genesis_time":"2020-01-01T00:00:00Z","app_state":{"billing":{},"bank":{},"sku":{}` + auth + `}}`
		require.ErrorContains(t, writeBillingMigrationPreflight(encoding.Codec, bytes.NewBufferString(document), &output, time.Unix(1, 0)), `missing "auth" module`)
	}
}

func TestBillingMigrationPreflightEvaluatesAuthLocksAtExplicitTime(t *testing.T) {
	encoding := params.MakeEncodingConfig()
	authtypes.RegisterInterfaces(encoding.InterfaceRegistry)
	vestingtypes.RegisterInterfaces(encoding.InterfaceRegistry)
	plannerTime := time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC)
	tenant := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	creditAddress := billingtypes.DeriveCreditAddress(tenant)
	lockedAccount, err := vestingtypes.NewDelayedVestingAccount(
		authtypes.NewBaseAccountWithAddress(creditAddress),
		sdk.NewCoins(sdk.NewInt64Coin("umfx", 5)),
		plannerTime.Add(time.Second).Unix(),
	)
	require.NoError(t, err)
	billingGenesis := billingtypes.DefaultGenesis()
	billingGenesis.LeaseSequence = 1
	billingGenesis.Leases = []billingtypes.Lease{{
		Uuid: "01912345-6789-7abc-8def-0123456789a0", Tenant: tenant.String(),
		ProviderUuid: "01912345-6789-7abc-8def-0123456789b0",
		Items:        []billingtypes.LeaseItem{{SkuUuid: "01912345-6789-7abc-8def-0123456789c0", Quantity: 1, LockedPrice: sdk.NewInt64Coin("umfx", 10)}},
		State:        billingtypes.LEASE_STATE_ACTIVE, CreatedAt: plannerTime, LastSettledAt: plannerTime,
		MinLeaseDurationAtCreation: 1,
	}}
	billingGenesis.CreditAccounts = []billingtypes.CreditAccount{{
		Tenant: tenant.String(), CreditAddress: creditAddress.String(), ActiveLeaseCount: 1,
		ReservedAmounts: sdk.NewCoins(sdk.NewInt64Coin("umfx", 10)),
	}}
	bankGenesis := banktypes.DefaultGenesisState()
	bankGenesis.Balances = []banktypes.Balance{{Address: creditAddress.String(), Coins: sdk.NewCoins(sdk.NewInt64Coin("umfx", 10))}}
	authGenesis := authtypes.NewGenesisState(authtypes.DefaultParams(), authtypes.GenesisAccounts{lockedAccount})
	document, err := json.Marshal(map[string]any{
		"chain_id": "test", "genesis_time": "2020-01-01T00:00:00Z",
		"app_state": map[string]json.RawMessage{
			"billing": encoding.Codec.MustMarshalJSON(billingGenesis),
			"bank":    encoding.Codec.MustMarshalJSON(bankGenesis),
			"auth":    encoding.Codec.MustMarshalJSON(authGenesis),
			"sku":     json.RawMessage(`{}`),
		},
	})
	require.NoError(t, err)
	for _, at := range []time.Time{plannerTime, plannerTime.Add(time.Second)} {
		var output bytes.Buffer
		require.NoError(t, writeBillingMigrationPreflight(encoding.Codec, bytes.NewReader(document), &output, at))
		var report billingMigrationPreflightOutput
		require.NoError(t, json.Unmarshal(output.Bytes(), &report))
		require.Equal(t, "2020-01-01T00:00:00Z", report.InputGenesisTime)
		require.Equal(t, at.Format(time.RFC3339), report.PlannerTime)
		require.Equal(t, "10", report.Tenants[0].Denominations[0].BankBalance)
		want := "5"
		if at.After(plannerTime) {
			want = "10"
		}
		require.Equal(t, want, report.Tenants[0].Denominations[0].SpendableBalance)
		require.Equal(t, want, report.Tenants[0].ModernActiveLeases[0].PlannedRemainingAmounts[0].Amount)
	}
}

func TestBillingMigrationPreflightRejectsMalformedAuthWithoutPanic(t *testing.T) {
	encoding := params.MakeEncodingConfig()
	authtypes.RegisterInterfaces(encoding.InterfaceRegistry)
	vestingtypes.RegisterInterfaces(encoding.InterfaceRegistry)
	address := sdk.AccAddress(bytes.Repeat([]byte{3}, 20)).String()
	const maxInt = "115792089237316195423570985008687907853269984665640564039457584007913129639935"
	for _, account := range []string{
		`null`,
		`{"@type":"/cosmos.vesting.v1beta1.PermanentLockedAccount"}`,
		`{"@type":"/cosmos.vesting.v1beta1.PermanentLockedAccount","base_vesting_account":{}}`,
		fmt.Sprintf(`{"@type":"/cosmos.vesting.v1beta1.PeriodicVestingAccount","base_vesting_account":{"base_account":{"address":%q},"original_vesting":[{"denom":"umfx","amount":%q}],"end_time":"2"},"start_time":"0","vesting_periods":[{"length":"1","amount":[{"denom":"umfx","amount":%q}]},{"length":"1","amount":[{"denom":"umfx","amount":%q}]}]}`, address, maxInt, maxInt, maxInt),
	} {
		document := `{"chain_id":"test","genesis_time":"2020-01-01T00:00:00Z","app_state":{"billing":{},"bank":{},"sku":{},"auth":{"accounts":[` + account + `]}}}`
		var output bytes.Buffer
		require.NotPanics(t, func() {
			err := writeBillingMigrationPreflight(encoding.Codec, bytes.NewBufferString(document), &output, time.Date(2030, time.January, 2, 0, 0, 0, 0, time.UTC))
			require.ErrorContains(t, err, "auth")
		})
		require.Empty(t, output.String(), "invalid auth input must not produce an apparently usable report")
	}
}
