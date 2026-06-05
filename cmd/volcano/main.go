// Volcano is the CLI entry point.
package main

import (
	"fmt"
	"os"

	rootcmd "github.com/Kong/volcano-cli/internal/cmd/root"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func main() {
	if err := rootcmd.New(cliruntime.Deps{}).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
