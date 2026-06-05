package policy

import (
	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// New returns the policy command.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "policy",
		Aliases: []string{"policies"},
		Short:   "Manage storage bucket policies",
		Long:    "List, inspect, create, and delete row-level security policies attached to storage buckets.",
	}
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newGet(deps))
	cmd.AddCommand(newCreate(deps))
	cmd.AddCommand(newDelete(deps))
	return cmd
}
