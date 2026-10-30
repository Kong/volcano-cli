// Package lavaflow wires the volcano cloud lavaflow subcommands.
package lavaflow

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"github.com/spf13/cobra"

	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const groupShort = "Diagnose and fix the project's runtime errors"

var groupLong = clilavaflow.OnDescription + ` Turning it on is the whole setup.

` + clilavaflow.CostNote

// New returns the lavaflow command.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lavaflow",
		Short: groupShort,
		Long:  groupLong,
	}
	cmd.AddCommand(newStatus(deps))
	cmd.AddCommand(newEnable(deps))
	cmd.AddCommand(newDisable(deps))
	cmd.AddCommand(newRuns(deps))
	return cmd
}

// NewCloudOnly stands in for the lavaflow command in the local tree. LavaFlow
// runs on Volcano's cloud service, which local development does not run, so
// the local server refuses every LavaFlow request. Hidden and without flag parsing for the reasons
// accesstokens.NewCloudOnly gives.
func NewCloudOnly() *cobra.Command {
	return &cobra.Command{
		Use:   "lavaflow",
		Short: groupShort,
		Long: groupLong + `

LavaFlow runs on Volcano's cloud service, which local development doesn't
run, so this command is cloud-only: run 'volcano cloud lavaflow ...' instead.`,
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if slices.ContainsFunc(args, isHelpFlag) {
				return cmd.Help()
			}
			return fmt.Errorf("%q is a cloud command: LavaFlow runs on Volcano's cloud service, "+
				"which local development doesn't run, so run 'volcano cloud lavaflow ...' instead", "lavaflow")
		},
	}
}

func isHelpFlag(arg string) bool {
	return arg == "--help" || arg == "-h"
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
