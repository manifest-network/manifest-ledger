package main

import (
	"fmt"
	"os"

	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"

	"github.com/manifest-network/manifest-ledger/app"
	"github.com/manifest-network/manifest-ledger/cmd/manifestd/cmd"
)

func main() {
	// Reject the bypass before the command tree, or any configuration loader it
	// runs, can report a different error first.
	if err := cmd.RejectSimulationAdminBypass(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	rootCmd := cmd.NewRootCmd()

	if err := svrcmd.Execute(rootCmd, "MANIFESTD", app.DefaultNodeHome); err != nil {
		fmt.Fprintln(rootCmd.OutOrStderr(), err)
		os.Exit(1)
	}
}
