package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
)

// defaultQueryGasLimit bounds SDK state reads for each ABCI or native gRPC query.
// This is distinct from wasm.query_gas_limit, which only bounds smart queries.
const defaultQueryGasLimit uint64 = 5_000_000

func queryGasLimit(appOpts servertypes.AppOptions) (uint64, error) {
	value := appOpts.Get(server.FlagQueryGasLimit)
	if value == nil {
		return defaultQueryGasLimit, nil
	}
	limit, err := strconv.ParseUint(fmt.Sprint(value), 10, 64)
	if err != nil || limit == 0 {
		return 0, fmt.Errorf("%s must be a positive finite gas budget expressed as a decimal integer; set %s = \"%d\" in app.toml (zero previously meant unbounded)", server.FlagQueryGasLimit, server.FlagQueryGasLimit, defaultQueryGasLimit)
	}
	return limit, nil
}

func configureQueryGasLimit(startCmd *cobra.Command) {
	flag := startCmd.Flags().Lookup(server.FlagQueryGasLimit)
	// The SDK installs this flag before invoking addModuleInitFlags.
	defaultValue := strconv.FormatUint(defaultQueryGasLimit, 10)
	if err := flag.Value.Set(defaultValue); err != nil {
		panic(err)
	}
	flag.DefValue = defaultValue
	flag.Usage = "Maximum SDK gas per ABCI or native gRPC query; must be positive (independent of wasm.query_gas_limit)"
	previousPreRun := startCmd.PreRunE
	startCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if previousPreRun != nil {
			if err := previousPreRun(cmd, args); err != nil {
				return err
			}
		}
		_, err := queryGasLimit(server.GetServerContextFromCmd(cmd).Viper)
		return err
	}
}

// Keep generated operator guidance consistent with the daemon's stricter
// policy while retaining the rest of the SDK's maintained config template.
func queryGasConfigTemplate() string {
	return strings.Replace(serverconfig.DefaultConfigTemplate,
		"# If this is set to zero, the query can consume an unbounded amount of gas.",
		"# Must be positive. Manifest rejects zero at startup.", 1)
}
