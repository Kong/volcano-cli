// Package domains wires the volcano domains subcommands.
package domains

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const groupShort = "Manage verified domains"

const groupLong = `Verify the domains your account owns. Once a domain is verified, you can
attach it, and any name below it, to frontends in any of your projects with no
further DNS record.

'verify' names the TXT record that proves you own a domain. Publish it with your
DNS provider, then run 'verify' again.

Verified domains belong to your account, so these commands need an account
token from 'volcano login'.`

// New returns the domains command.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "domains",
		Short: groupShort,
		Long:  groupLong,
	}
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newVerify(deps))
	cmd.AddCommand(newRemove(deps))
	return cmd
}

// NewCloudOnly stands in for the domains command in the local tree. Local
// development has one account and serves no custom domains, so there is no
// ownership to prove. Hidden and without flag parsing for the reasons
// accesstokens.NewCloudOnly gives.
func NewCloudOnly() *cobra.Command {
	return &cobra.Command{
		Use:   "domains",
		Short: groupShort,
		Long: groupLong + `

Local development serves no custom domains, so this command is cloud-only: run
'volcano cloud domains ...' instead.`,
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if slices.ContainsFunc(args, isHelpFlag) {
				return cmd.Help()
			}
			return fmt.Errorf("%q is a cloud command: local development serves no custom domains, "+
				"so run 'volcano cloud domains ...' instead", "domains")
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
