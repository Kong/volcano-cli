package databases

import (
	"context"

	"github.com/spf13/cobra"

	migrationcmd "github.com/Kong/volcano-cli/internal/cmd/databases/migration"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// LocalCreateDefaults contains local-only defaults provided by the local server.
type LocalCreateDefaults struct {
	Region          string
	PostgresVersion string
}

// LocalCreateDefaultsFunc resolves local-only database create defaults.
type LocalCreateDefaultsFunc func(context.Context) (LocalCreateDefaults, error)

// LocalOptions configures the local databases command.
type LocalOptions struct {
	CreateDefaults LocalCreateDefaultsFunc
}

// New returns the databases command.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "databases",
		Short: "Manage cloud databases",
		Long:  "Create, list, and delete PostgreSQL databases for the current cloud project.",
	}
	cmd.AddCommand(newCreate(deps))
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newGet(deps))
	cmd.AddCommand(newDelete(deps))
	cmd.AddCommand(migrationcmd.New(deps))
	return cmd
}

// NewLocal returns the local databases command.
func NewLocal(deps cliruntime.Deps) *cobra.Command {
	return NewLocalWithOptions(deps, LocalOptions{})
}

// NewLocalWithOptions returns the local databases command with custom options.
func NewLocalWithOptions(deps cliruntime.Deps, opts LocalOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "databases",
		Short: "Manage local databases",
		Long:  "Create, list, and delete PostgreSQL databases for the local development project.",
	}
	cmd.AddCommand(newLocalCreate(deps, opts.CreateDefaults))
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newGet(deps))
	cmd.AddCommand(newDelete(deps))
	cmd.AddCommand(migrationcmd.New(deps))
	return cmd
}
