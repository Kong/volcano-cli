// Package accesstokens wires the volcano access-tokens subcommands.
package accesstokens

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// New returns the access tokens command.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "access-tokens",
		Aliases: []string{"tokens"},
		Short:   "Manage project access tokens",
		Long: `Create, inspect, and revoke access tokens for the current cloud project.

A project access token (prefix pt-) authenticates the same project-scoped
commands as your account token, but only for the project it was minted in, and
it cannot manage tokens. Use one for CI and other automation.

Managing tokens needs an account token, so run these commands logged in with
'volcano login'.`,
	}
	cmd.AddCommand(newCreate(deps))
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newGet(deps))
	cmd.AddCommand(newUsage(deps))
	cmd.AddCommand(newRevoke(deps))
	return cmd
}

// NewCloudOnly stands in for the access tokens command in the local tree.
// Local development is a single-tenant sandbox with no account and no
// credentials to mint, and without the stub cobra answers the unknown
// subcommand by printing the root help and exiting 0 — which reads as if the
// command had run. Hidden so local help lists only what local mode can do, and
// flag parsing is off so any flags the user typed reach it.
func NewCloudOnly() *cobra.Command {
	return &cobra.Command{
		Use:                "access-tokens",
		Aliases:            []string{"tokens"},
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("%q is a cloud command: local development issues no credentials, "+
				"so run 'volcano cloud access-tokens ...' against a cloud project", "access-tokens")
		},
	}
}

// writeJSON prints value as indented JSON for --json consumers.
func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
