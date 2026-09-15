// Package cli defines manifest module command-line commands.
package cli

import (
	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"

	"github.com/manifest-network/manifest-ledger/x/manifest/types"
)

// GetQueryCmd returns the manifest module's query command.
func GetQueryCmd() *cobra.Command {
	queryCmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      "Querying commands for " + types.ModuleName,
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	return queryCmd
}
