// Package bucket provides storage bucket commands.
package bucket

import (
	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// New returns the bucket command.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "bucket",
		Aliases: []string{"buckets"},
		Short:   "Manage storage buckets",
		Long:    "List, inspect, create, update, and delete storage buckets in the current project.",
	}
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newGet(deps))
	cmd.AddCommand(newCreate(deps))
	cmd.AddCommand(newUpdate(deps))
	cmd.AddCommand(newDelete(deps))
	return cmd
}
