package object

import (
	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clistorage "github.com/Kong/volcano-cli/internal/storage"
)

// New returns the object command.
func New(deps cliruntime.Deps) *cobra.Command {
	return NewWithServiceOptions(deps)
}

// NewWithServiceOptions returns the object command with custom storage behavior.
func NewWithServiceOptions(deps cliruntime.Deps, serviceOptions ...clistorage.Option) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "object",
		Aliases: []string{"objects"},
		Short:   "Manage storage objects",
		Long:    "List, upload, download, delete, copy, move, and update visibility for storage objects.",
	}
	cmd.AddCommand(newList(deps, serviceOptions...))
	cmd.AddCommand(newUpload(deps, serviceOptions...))
	cmd.AddCommand(newDownload(deps, serviceOptions...))
	cmd.AddCommand(newDelete(deps, serviceOptions...))
	cmd.AddCommand(newCopy(deps, serviceOptions...))
	cmd.AddCommand(newMove(deps, serviceOptions...))
	cmd.AddCommand(newVisibility(deps, serviceOptions...))
	return cmd
}
