package cmd

import (
	"bytes"
	"context"
	"math"
	"testing"
	"text/template"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/testutil/sims"
)

func TestQueryGasLimitConfiguration(t *testing.T) {
	for _, test := range []struct {
		name    string
		value   any
		want    uint64
		invalid bool
	}{
		{"missing uses bounded default", nil, defaultQueryGasLimit, false},
		{"positive config string", "7000000", 7_000_000, false},
		{"positive flag", uint64(123), 123, false},
		{"positive integer config", int64(456), 456, false},
		{"largest representable budget", "18446744073709551615", math.MaxUint64, false},
		{"boolean", true, 0, true},
		{"fraction", 1.5, 0, true},
		{"NaN", math.NaN(), 0, true},
		{"positive infinity", math.Inf(1), 0, true},
		{"negative infinity", math.Inf(-1), 0, true},
		{"out of range", "18446744073709551616", 0, true},
		{"hexadecimal", "0xffff", 0, true},
		{"old unbounded config", "0", 0, true},
		{"zero flag", uint64(0), 0, true},
		{"negative", "-1", 0, true},
		{"invalid", "unlimited", 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			limit, err := queryGasLimit(sims.AppOptionsMap{server.FlagQueryGasLimit: test.value})
			if test.invalid {
				require.ErrorContains(t, err, "positive finite gas budget")
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, limit)
		})
	}
	// Verify the actual generated app.toml contains the BaseApp default;
	// merely changing a Wasm setting would leave native module queries unlimited.
	text, config := initWasmConfig()
	tmpl, err := template.New("app.toml").Parse(text)
	require.NoError(t, err)
	var data bytes.Buffer
	require.NoError(t, tmpl.Execute(&data, config))
	require.Contains(t, data.String(), "Manifest rejects zero at startup")
	require.NotContains(t, data.String(), "the query can consume an unbounded amount of gas")
	settings := viper.New()
	settings.SetConfigType("toml")
	require.NoError(t, settings.ReadConfig(&data))
	require.Equal(t, defaultQueryGasLimit, settings.GetUint64(server.FlagQueryGasLimit))
}

func TestStartRejectsUnboundedQueryGas(t *testing.T) {
	for _, test := range []struct {
		name       string
		configured any
		flag       string
		want       uint64
		invalid    bool
	}{
		{"default", nil, "", defaultQueryGasLimit, false},
		{"old config rejected", "0", "", 0, true},
		{"flag overrides old config", "0", "7000000", 7_000_000, false},
		{"zero flag overrides safe config", "5000000", "0", 0, true},
		{"finite config override", "8000000", "", 8_000_000, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := server.StartCmd(newApp, t.TempDir())
			configureQueryGasLimit(command)
			serverContext := server.NewDefaultContext()
			if test.configured != nil {
				serverContext.Viper.SetDefault(server.FlagQueryGasLimit, test.configured)
			}
			command.SetContext(context.WithValue(context.Background(), server.ServerContextKey, serverContext))
			if test.flag != "" {
				require.NoError(t, command.Flags().Set(server.FlagQueryGasLimit, test.flag))
			}
			err := command.PreRunE(command, nil)
			if test.invalid {
				require.ErrorContains(t, err, "zero previously meant unbounded")
				return
			}
			require.NoError(t, err)
			limit, err := queryGasLimit(serverContext.Viper)
			require.NoError(t, err)
			require.Equal(t, test.want, limit)
		})
	}
}
