package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
)

// Application commands on a fork home skip the SDK's client configuration,
// which would create a keyring before conversion. Resolve the output format
// as it would: an explicit --output, then the copied client.toml, then text.
func testnetOutputFormat(cmd *cobra.Command, home string) string {
	if flag := cmd.Flags().Lookup(flags.FlagOutput); flag != nil && flag.Changed {
		return flag.Value.String()
	}
	v := viper.New()
	v.SetConfigFile(filepath.Join(home, "config", "client.toml"))
	if err := v.ReadInConfig(); err == nil && v.IsSet(flags.FlagOutput) {
		return v.GetString(flags.FlagOutput)
	}
	return flags.OutputFormatText
}

// A copied client.toml keeps the source chain's ID and node. Refuse to sign from
// a fork home with another chain ID, so a transaction meant for the fork cannot
// be signed for, and broadcast to, the source chain. Queries are not checked:
// their risk is the node they reach, which a chain ID cannot reveal.
func rejectTestnetClientChain(cmd *cobra.Command) error {
	if path := testnetCommandPath(cmd); path[0] != "tx" {
		return nil
	}
	clientCtx := client.GetClientContextFromCmd(cmd)
	if _, err := os.Lstat(filepath.Join(clientCtx.HomeDir, inPlaceTestnetMarker)); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	journal, err := readTestnetJournal(clientCtx.HomeDir)
	if err != nil {
		return err
	}
	if clientCtx.ChainID != "" && clientCtx.ChainID != journal.ChainID {
		return fmt.Errorf("client chain ID %q is not this fork's %q; set chain-id and node in %s, or pass --chain-id and --node",
			clientCtx.ChainID, journal.ChainID, filepath.Join(clientCtx.HomeDir, "config", "client.toml"))
	}
	return nil
}
