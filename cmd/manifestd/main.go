package main

import (
	"fmt"
	"os"

	"github.com/liftedinit/manifest-ledger/app"
	"github.com/liftedinit/manifest-ledger/cmd/manifestd/cmd"

	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
)

func main() {
	rootCmd := cmd.NewRootCmd()

	if err := svrcmd.Execute(rootCmd, "MANIFESTD", app.DefaultNodeHome); err != nil {
		fmt.Fprintln(rootCmd.OutOrStderr(), err)
		os.Exit(1)
	}
}
