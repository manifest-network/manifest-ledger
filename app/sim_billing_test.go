package app_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simulationtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
)

func TestSimulationBillingTransfers(t *testing.T) {
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	for _, tc := range []struct {
		name               string
		defaultSendEnabled bool
		sendEnabled        []banktypes.SendEnabled
	}{
		{
			name:               "explicit deny overrides enabled default at pinned seed",
			defaultSendEnabled: true,
			sendEnabled: []banktypes.SendEnabled{
				{Denom: sdk.DefaultBondDenom, Enabled: false},
				{Denom: "other", Enabled: false},
			},
		},
		{
			name:               "disabled default without denomination override",
			defaultSendEnabled: false,
			sendEnabled:        []banktypes.SendEnabled{{Denom: "other", Enabled: true}},
		},
		{
			name:               "existing enabled override",
			defaultSendEnabled: false,
			sendEnabled:        []banktypes.SendEnabled{{Denom: sdk.DefaultBondDenom, Enabled: true}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bankGenesis := banktypes.DefaultGenesisState()
			bankGenesis.Params.DefaultSendEnabled = tc.defaultSendEnabled
			bankGenesis.SendEnabled = tc.sendEnabled
			bankGenesis.Balances = []banktypes.Balance{{
				Address: sdk.AccAddress([]byte("simulation-account!!")).String(),
				Coins:   sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 123)),
			}}
			bankGenesis.Supply = sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 123))
			state := map[string]json.RawMessage{
				banktypes.ModuleName:    cdc.MustMarshalJSON(bankGenesis),
				billingtypes.ModuleName: json.RawMessage(`{"params":{"min_lease_duration":"3600"}}`),
			}
			billingStateBefore := string(state[billingtypes.ModuleName])
			enableSimulationBillingTransfers(cdc, state)

			var updated banktypes.GenesisState
			cdc.MustUnmarshalJSON(state[banktypes.ModuleName], &updated)
			require.JSONEq(t, string(cdc.MustMarshalJSON(&bankGenesis.Params)), string(cdc.MustMarshalJSON(&updated.Params)))
			require.Equal(t, bankGenesis.Balances, updated.Balances)
			require.Equal(t, bankGenesis.Supply, updated.Supply)
			require.Equal(t, billingStateBefore, string(state[billingtypes.ModuleName]))
			billingOverrides := 0
			for _, entry := range updated.SendEnabled {
				if entry.Denom == sdk.DefaultBondDenom {
					billingOverrides++
					require.True(t, entry.Enabled, "billing funding must be send-enabled regardless of the default")
				}
			}
			for _, entry := range bankGenesis.SendEnabled {
				if entry.Denom != sdk.DefaultBondDenom {
					require.Contains(t, updated.SendEnabled, entry, "unrelated denomination policies must be preserved")
				}
			}
			require.Equal(t, 1, billingOverrides)
			require.NoError(t, updated.Validate())

			firstResult := string(state[banktypes.ModuleName])
			enableSimulationBillingTransfers(cdc, state)
			require.Equal(t, firstResult, string(state[banktypes.ModuleName]), "applying the simulation policy twice must not duplicate entries")
		})
	}
}

func TestBillingSimulationCoverage(t *testing.T) {
	for _, tc := range []struct {
		name          string
		fundingOK     int
		fundingNoOp   int
		creationOK    int
		creationNoOp  int
		expectedError string
	}{
		{name: "empty or billing-disabled run"},
		{name: "tiny run can select only no-ops", fundingNoOp: 19, creationNoOp: 19},
		{name: "pinned-seed all-no-op regression", fundingNoOp: 50, creationNoOp: 21, expectedError: "no credit deposits after 50 attempts"},
		{name: "funding starvation threshold", fundingNoOp: 20, expectedError: "no credit deposits after 20 attempts"},
		{name: "funding does not hide lease starvation", fundingOK: 50, creationNoOp: 20, expectedError: "no lease creations after 20 attempts"},
		{name: "short run with successful funding", fundingOK: 1, creationNoOp: 19},
		{name: "funding operation intentionally disabled", creationNoOp: 100},
		{name: "lease creation intentionally disabled", fundingOK: 50},
		{name: "billing exercised", fundingOK: 50, fundingNoOp: 50, creationOK: 21, creationNoOp: 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stats := simulation.EventStats{
				billingtypes.ModuleName: {
					sdk.MsgTypeURL(&billingtypes.MsgFundCredit{}): {
						"ok": tc.fundingOK, "failure": tc.fundingNoOp,
					},
					sdk.MsgTypeURL(&billingtypes.MsgCreateLease{}): {
						"ok": tc.creationOK, "failure": tc.creationNoOp,
					},
				},
			}
			err := billingSimulationCoverageError(stats)
			if tc.expectedError != "" {
				require.ErrorContains(t, err, tc.expectedError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestBillingSimulationResultDiagnostics(t *testing.T) {
	simulationErr := errors.New("finalize block failed")
	for _, tc := range []struct {
		name          string
		stopEarly     bool
		exportOnly    bool
		simulationErr error
		fundingOK     int
		expectedError string
	}{
		{name: "stopped run prints statistics without asserting coverage", stopEarly: true},
		{name: "stopped run honors explicit statistics export", stopEarly: true, exportOnly: true},
		{name: "completed run prints statistics before coverage failure", expectedError: "no credit deposits after 50 attempts"},
		{name: "completed run honors explicit statistics export", exportOnly: true, expectedError: "no credit deposits after 50 attempts"},
		{name: "completed run with coverage prints statistics once", fundingOK: 1},
		{name: "simulator error without statistics is preserved", stopEarly: true, simulationErr: simulationErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := simulationtypes.Config{
				Seed:            1729,
				ExportStatsPath: filepath.Join(t.TempDir(), "simulation-stats.json"),
			}
			stats := simulation.EventStats{
				billingtypes.ModuleName: {
					sdk.MsgTypeURL(&billingtypes.MsgFundCredit{}): {"ok": tc.fundingOK, "failure": 50},
				},
			}
			// Match the SDK: stopped runs export their partial statistics, while
			// a genuine error may return before creating the statistics file.
			if tc.simulationErr == nil {
				stats.ExportJSON(config.ExportStatsPath)
			}
			var output bytes.Buffer
			var writer io.Writer = &output
			if tc.exportOnly {
				writer = nil
			}
			err := checkBillingSimulationResult(writer, config, tc.stopEarly, tc.simulationErr)
			switch {
			case tc.simulationErr != nil:
				require.Same(t, tc.simulationErr, err)
			case tc.expectedError != "":
				require.ErrorContains(t, err, tc.expectedError)
			default:
				require.NoError(t, err)
			}
			if tc.exportOnly || tc.simulationErr != nil {
				require.Empty(t, output.String())
				return
			}

			decoder := json.NewDecoder(&output)
			var printed simulation.EventStats
			require.NoError(t, decoder.Decode(&printed))
			require.Equal(t, stats, printed)
			require.ErrorIs(t, decoder.Decode(&printed), io.EOF, "statistics must be printed exactly once")
		})
	}
}
