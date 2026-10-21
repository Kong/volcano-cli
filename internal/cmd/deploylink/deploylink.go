// Package deploylink wires the volcano deploy-link subcommands.
package deploylink

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const groupShort = "Manage deploy links for the project's GitHub repository"

const groupLong = `A deploy link lets anyone deploy the public GitHub repository your project is
connected to as their own Volcano project. Its address is
https://volcano.dev/deploy/<id>, and it carries no repository or branch, so it
cannot be edited to point at someone else's code.

The repository comes from the project's Git connection, so 'create' takes none.
The repository must be public, the connected GitHub account must be able to push
to it, and its branch must hold a frontend or a function to deploy. A project
can have up to 20 links.

Disabling a link needs no checks. Enabling one verifies it again, so a link
whose repository went private stays off until that is fixed.`

// New returns the deploy-link command.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "deploy-link",
		Aliases: []string{"deploy-links"},
		Short:   groupShort,
		Long:    groupLong,
	}
	cmd.AddCommand(newCreate(deps))
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newGet(deps))
	cmd.AddCommand(newStats(deps))
	cmd.AddCommand(newSetStatus(deps, statusDisable))
	cmd.AddCommand(newSetStatus(deps, statusEnable))
	cmd.AddCommand(newDelete(deps))
	return cmd
}

// NewCloudOnly stands in for the deploy-link command in the local tree. A
// deploy link points at a GitHub repository through a project's Git connection,
// which local development does not have. Hidden and without flag parsing for the
// reasons accesstokens.NewCloudOnly gives.
func NewCloudOnly() *cobra.Command {
	return &cobra.Command{
		Use:     "deploy-link",
		Aliases: []string{"deploy-links"},
		Short:   groupShort,
		Long: groupLong + `

Local development has no Git connection, so this command is cloud-only: run
'volcano cloud deploy-link ...' against a cloud project.`,
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if slices.ContainsFunc(args, isHelpFlag) {
				return cmd.Help()
			}
			return fmt.Errorf("%q is a cloud command: local development has no Git connection to deploy from, "+
				"so run 'volcano cloud deploy-link ...' against a cloud project", "deploy-link")
		},
	}
}

func isHelpFlag(arg string) bool {
	return arg == "--help" || arg == "-h"
}

// parseLinkID returns the link id an argument names. The id goes into a URL
// path, so an empty one is refused here rather than sent to the list route.
func parseLinkID(value string) (string, error) {
	id := strings.TrimSpace(value)
	if id == "" {
		return "", errors.New("the deploy link ID is empty")
	}
	return id, nil
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
