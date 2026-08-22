package databases

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	backupcmd "github.com/Kong/volcano-cli/internal/cmd/databases/backup"
	branchcmd "github.com/Kong/volcano-cli/internal/cmd/databases/branch"
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
	cmd.AddCommand(branchcmd.New(deps))
	cmd.AddCommand(backupcmd.New(deps))
	cmd.AddCommand(backupcmd.NewRestore(deps))
	cmd.AddCommand(backupcmd.NewSchedule(deps))
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
	cmd.AddCommand(cloudOnly("backups", "backup"))
	cmd.AddCommand(cloudOnly("backup-schedule"))
	cmd.AddCommand(cloudOnly("restore"))
	cmd.AddCommand(cloudOnly("branches", "branch"))
	return cmd
}

// cloudOnly stands in for a database command the storage provider backs, which
// local development does not run. Without it cobra answers an unknown
// subcommand by printing the parent's help and exiting 0, which reads as if the
// command had run and says nothing about where it actually lives. The stub is
// hidden so local help still lists only what local mode can do, and takes its
// arguments raw so `restore app --backup nightly` reaches it rather than
// failing on an unknown flag.
func cloudOnly(name string, aliases ...string) *cobra.Command {
	return &cobra.Command{
		Use:                name,
		Aliases:            aliases,
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("%q is a cloud command: local development has no storage provider behind it, "+
				"so run 'volcano cloud databases %s' against a cloud project", name, name)
		},
	}
}
