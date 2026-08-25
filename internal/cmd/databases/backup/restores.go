package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// NewRestores returns the restores command, which reads what the restore
// command started. A restore outlives the request that begins it, and the
// database it is rewinding only reports that it is restoring: the reason an
// attempt failed is on the restore.
func NewRestores(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "restores",
		Aliases: []string{"restore-history"},
		Short:   "Inspect database restores",
		Long: `Read a database's restores, running and finished.

Start one with the restore command. A restore takes minutes, so this is how you
watch it, and it is the only place that says why one did not finish.`,
	}
	cmd.AddCommand(newRestoresList(deps))
	cmd.AddCommand(newRestoresGet(deps))
	return cmd
}

type restoresListOptions struct {
	deps     cliruntime.Deps
	database string
	out      io.Writer
}

func newRestoresList(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list <database>",
		Short: "List a database's recent restores",
		Long: `List the 50 most recent restores of a database, newest first.

A restore that is pending or running is still in flight and the database is not
connectable. A failed one is retried; an exhausted one was given up on.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			database, err := parseDatabase(args)
			if err != nil {
				return err
			}
			return runRestoresList(cmd.Context(), restoresListOptions{
				deps:     deps,
				database: database,
				out:      cmd.OutOrStdout(),
			})
		},
	}
}

func runRestoresList(ctx context.Context, opts restoresListOptions) error {
	restores, err := clidatabase.NewService(opts.deps).ListRestores(ctx, opts.database)
	if err != nil {
		return err
	}

	output.DatabaseRestores(opts.out, restores, opts.database)
	return nil
}

type restoresGetOptions struct {
	deps      cliruntime.Deps
	database  string
	restoreID uuid.UUID
	out       io.Writer
}

func newRestoresGet(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get <database> <restore-id>",
		Short: "Show one restore",
		Long: `Show a restore's status, what it rewound to, and why it failed if it did.

The restore command prints the id.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			database, restoreID, err := parseRestoreTargetArgs(args)
			if err != nil {
				return err
			}
			return runRestoresGet(cmd.Context(), restoresGetOptions{
				deps:      deps,
				database:  database,
				restoreID: restoreID,
				out:       cmd.OutOrStdout(),
			})
		},
	}
}

// parseRestoreTargetArgs validates "<database> <restore-id>". The id is checked
// here rather than sent on, so a mistyped one is named as such instead of
// coming back as a 404 that reads like the restore is gone.
func parseRestoreTargetArgs(args []string) (string, uuid.UUID, error) {
	databaseName := strings.TrimSpace(args[0])
	if databaseName == "" {
		return "", uuid.Nil, errors.New("database name cannot be empty")
	}
	restoreID, err := uuid.Parse(strings.TrimSpace(args[1]))
	if err != nil {
		return "", uuid.Nil, fmt.Errorf("restore id must be a UUID, as printed by the restore command: %w", err)
	}
	return databaseName, restoreID, nil
}

func runRestoresGet(ctx context.Context, opts restoresGetOptions) error {
	restore, err := clidatabase.NewService(opts.deps).GetRestore(ctx, opts.database, opts.restoreID)
	if err != nil {
		return err
	}

	output.DatabaseRestore(opts.out, restore, opts.database)
	return nil
}
