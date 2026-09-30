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
//
// Call this after the SDK's configuration interceptor: it applies app.toml,
// config.toml and environment values to unset flags, which then take precedence
// over client.toml when the transaction runs.
func rejectTestnetClientChain(cmd *cobra.Command) error {
	if path := testnetCommandPath(cmd); path[0] != "tx" {
		return nil
	}
	// Resolve the home and chain ID as client.ReadPersistentCommandFlags does for
	// the transaction, without building its keyring or node clients.
	clientCtx := client.GetClientContextFromCmd(cmd)
	home, chainID := clientCtx.HomeDir, clientCtx.ChainID
	if home == "" || cmd.Flags().Changed(flags.FlagHome) {
		home, _ = cmd.Flags().GetString(flags.FlagHome)
	}
	if chainID == "" || cmd.Flags().Changed(flags.FlagChainID) {
		chainID, _ = cmd.Flags().GetString(flags.FlagChainID)
	}
	if _, err := os.Lstat(filepath.Join(home, inPlaceTestnetMarker)); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	journal, err := readTestnetJournal(home)
	if err != nil {
		return err
	}
	if chainID != "" && chainID != journal.ChainID {
		return fmt.Errorf("client chain ID %q is not this fork's %q; set chain-id and node in %s, or pass --chain-id and --node",
			chainID, journal.ChainID, filepath.Join(home, "config", "client.toml"))
	}
	return nil
}
