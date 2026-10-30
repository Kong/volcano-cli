// Package lavaflow wires the volcano cloud lavaflow subcommands.
package lavaflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const groupShort = "Diagnose and fix the project's runtime errors"

var groupLong = clilavaflow.OnDescription + ` It covers every function, frontend
and durable function in the project, and turning it on is the whole setup.

Turning it on or off and changing its settings is a person's decision: a
project access token can read LavaFlow but not change it.`

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
	cmd.AddCommand(newSettings(deps))
	cmd.AddCommand(newStats(deps))
	cmd.AddCommand(newResources(deps))
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newShow(deps))
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
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return err
	}
	_, err := io.WriteString(w, escapeJSONControls(buf.String()))
	return err
}

// escapeJSONControls writes DEL and the C1 controls, which encoding/json
// passes through, as \u escapes, like the C0 controls it already escapes.
// They can only appear inside a JSON string, where the escape means the same.
func escapeJSONControls(encoded string) string {
	var b strings.Builder
	for _, r := range encoded {
		if r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			fmt.Fprintf(&b, "\\u%04x", r)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
