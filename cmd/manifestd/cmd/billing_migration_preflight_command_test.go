package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	"github.com/manifest-network/manifest-ledger/app/params"
	billingtypes "github.com/manifest-network/manifest-ledger/x/billing/types"
)

func preflightCommandDocument(t *testing.T, cdc codec.JSONCodec) []byte {
	t.Helper()
	state, err := json.Marshal(map[string]json.RawMessage{
		"billing": cdc.MustMarshalJSON(billingtypes.DefaultGenesis()),
		"bank":    cdc.MustMarshalJSON(banktypes.DefaultGenesisState()),
		"auth":    json.RawMessage(`{}`),
		"sku":     json.RawMessage(`{}`),
	})
	require.NoError(t, err)
	document, err := json.Marshal(genutiltypes.AppGenesis{
		ChainID: "preflight-command-test", InitialHeight: 123,
		GenesisTime: time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC), AppState: state,
	})
	require.NoError(t, err)
	return document
}

func TestBillingMigrationPreflightCommand(t *testing.T) {
	encodingConfig := params.MakeEncodingConfig()
	document := preflightCommandDocument(t, encodingConfig.Codec)
	for _, test := range []struct {
		name         string
		at           string
		missingCodec bool
		missingFile  bool
		wantError    string
	}{
		{name: "UTC normalized success", at: "2030-01-02T05:04:05.123+02:00"},
		{name: "missing codec", at: "2030-01-02T03:04:05Z", missingCodec: true, wantError: "requires an application codec"},
		{name: "missing file", at: "2030-01-02T03:04:05Z", missingFile: true, wantError: "open exported genesis"},
		{name: "invalid time", at: "tomorrow", wantError: "--at requires a non-zero RFC3339"},
		{name: "zero time", at: "0001-01-01T00:00:00Z", wantError: "--at requires a non-zero RFC3339"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "export.json")
			require.NoError(t, os.WriteFile(path, document, 0o600))
			command := newBillingMigrationPreflightCmd()
			command.SetContext(context.Background())
			command.SilenceErrors = true
			command.SilenceUsage = true
			if !test.missingCodec {
				require.NoError(t, client.SetCmdClientContext(command, client.Context{}.WithCodec(encodingConfig.Codec)))
			}
			inputPath := path
			if test.missingFile {
				inputPath = filepath.Join(t.TempDir(), "missing.json")
			}
			command.SetArgs([]string{inputPath, "--at", test.at})
			var output bytes.Buffer
			command.SetOut(&output)
			command.SetErr(io.Discard)
			err := command.Execute()
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				require.Empty(t, output.String(), "failed validation must not print a partial audit report")
			} else {
				require.NoError(t, err)
				var report billingMigrationPreflightOutput
				require.NoError(t, json.Unmarshal(output.Bytes(), &report))
				require.Equal(t, uint32(billingMigrationPreflightSchemaVersion), report.SchemaVersion)
				require.Equal(t, "preflight-command-test", report.SourceChainID)
				require.Equal(t, int64(123), report.SourceInitialHeight)
				require.Equal(t, "2030-01-02T03:04:05.123Z", report.PlannerTime)
				require.Equal(t, "2030-01-02T03:04:05Z", report.InputGenesisTime)
				require.Equal(t, "lease_free", report.BillingState)
				require.Equal(t, "none", report.MigrationPath)
			}
			unchanged, err := os.ReadFile(path) //nolint:gosec // Fixture inside t.TempDir.
			require.NoError(t, err)
			require.Equal(t, document, unchanged, "offline preflight must never rewrite the export")
			info, err := os.Stat(path)
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		})
	}
}

func TestBillingMigrationPreflightRejectsMalformedDocumentWithoutReport(t *testing.T) {
	encodingConfig := params.MakeEncodingConfig()
	document := preflightCommandDocument(t, encodingConfig.Codec)
	var original map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(document, &original))
	withField := func(field, value string) []byte {
		modified := make(map[string]json.RawMessage, len(original))
		for key, content := range original {
			modified[key] = content
		}
		modified[field] = json.RawMessage(value)
		result, err := json.Marshal(modified)
		require.NoError(t, err)
		return result
	}
	withModule := func(module, value string) []byte {
		var state map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(original["app_state"], &state))
		state[module] = json.RawMessage(value)
		result, err := json.Marshal(state)
		require.NoError(t, err)
		return withField("app_state", string(result))
	}
	for _, test := range []struct {
		name      string
		document  []byte
		wantError string
	}{
		{"invalid document JSON", []byte(`{"`), "decode exported genesis document:"},
		{"second document", append(bytes.Clone(document), []byte(` {}`)...), "unexpected trailing JSON value"},
		{"invalid trailing JSON", append(bytes.Clone(document), []byte(` trailing`)...), "decode exported genesis document trailer:"},
		{"missing genesis time", withField("genesis_time", `"0001-01-01T00:00:00Z"`), "missing or zero genesis_time"},
		{"oversized chain ID", withField("chain_id", `"`+strings.Repeat("x", genutiltypes.MaxChainIDLen+1)+`"`), "chain_id exceeds maximum length"},
		{"app state is array", withField("app_state", `[]`), "decode exported genesis app_state:"},
		{"malformed billing", withModule("billing", `{"leases":false}`), "decode billing genesis for migration preflight:"},
		{"malformed bank", withModule("bank", `{"balances":false}`), "decode bank genesis for billing migration preflight:"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			err := writeBillingMigrationPreflight(encodingConfig.Codec, bytes.NewReader(test.document), &output, time.Now())
			require.ErrorContains(t, err, test.wantError)
			require.Empty(t, output.String())
		})
	}
	t.Run("missing valuation time", func(t *testing.T) {
		var output bytes.Buffer
		err := writeBillingMigrationPreflight(encodingConfig.Codec, bytes.NewReader(document), &output, time.Time{})
		require.ErrorContains(t, err, "requires an explicit non-zero planner time")
		require.Empty(t, output.String())
	})
	t.Run("output failure is returned", func(t *testing.T) {
		err := writeBillingMigrationPreflight(encodingConfig.Codec, bytes.NewReader(document), failedPreflightWriter{}, time.Now())
		require.ErrorIs(t, err, io.ErrClosedPipe)
		require.ErrorContains(t, err, "encode billing migration preflight report")
	})
}

type failedPreflightWriter struct{}

func (failedPreflightWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
