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

var groupLong = `LavaFlow reads the current project's runtime errors, groups them into issues,
diagnoses each one, and opens a draft pull request with a fix in the project's
connected repository. Turning it on is the whole setup.

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
// opens its fix pull requests through Volcano's GitHub App, which local
// development does not run, so the local server refuses every LavaFlow
// request. Hidden and without flag parsing for the reasons
// accesstokens.NewCloudOnly gives.
func NewCloudOnly() *cobra.Command {
	return &cobra.Command{
		Use:   "lavaflow",
		Short: groupShort,
		Long: groupLong + `

Local development has no GitHub App for LavaFlow to open fix pull requests
with, so this command is cloud-only: run 'volcano cloud lavaflow ...' instead.`,
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if slices.ContainsFunc(args, isHelpFlag) {
				return cmd.Help()
			}
			return fmt.Errorf("%q is a cloud command: local development has no GitHub App for LavaFlow "+
				"to open fix pull requests with, so run 'volcano cloud lavaflow ...' instead", "lavaflow")
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
