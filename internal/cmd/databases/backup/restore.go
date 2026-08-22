package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/confirm"
	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type restoreOptions struct {
	deps      cliruntime.Deps
	database  string
	backup    string
	restoreTo time.Time
	yes       bool
	in        io.Reader
	out       io.Writer
}

// NewRestore returns the restore command. It sits beside the backups group
// rather than inside it because a restore also targets a point in time, which
// has no backup to name.
func NewRestore(deps cliruntime.Deps) *cobra.Command {
	var backupName string
	var restoreTo string
	var yes bool
	cmd := &cobra.Command{
		Use:   "restore <database>",
		Short: "Restore a database",
		Long: fmt.Sprintf(`Replace a database's data, either with one of its backups or with its state at
a point in time.

This is destructive: everything written after the point being restored is
discarded. The restore runs in the background and the database serves no
connections until it finishes, but its connection string never changes, so
nothing holding it needs updating.

Branches are not restored. They keep serving their own data, but resetting a
branch from this database is refused for up to 24 hours afterwards.

By default this command prompts for confirmation.
Use --yes to skip the prompt.

Examples:
  %s
  %s`,
			cliruntime.CommandPath(deps, "databases restore app --backup before_migration"),
			cliruntime.CommandPath(deps, "databases restore app --to 2026-01-15T09:30:00Z")),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			database, err := parseDatabase(args)
			if err != nil {
				return err
			}
			backup, target, err := parseRestoreTarget(backupName, restoreTo)
			if err != nil {
				return err
			}
			return runRestore(cmd.Context(), restoreOptions{
				deps:      deps,
				database:  database,
				backup:    backup,
				restoreTo: target,
				yes:       yes,
				in:        cmd.InOrStdin(),
				out:       cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().StringVar(&backupName, "backup", "", "Name of the backup to restore")
	cmd.Flags().StringVar(&restoreTo, "to", "", "Point in time to restore to, as RFC 3339 (e.g. 2026-01-15T09:30:00Z)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	cmd.MarkFlagsMutuallyExclusive("backup", "to")
	cmd.MarkFlagsOneRequired("backup", "to")
	return cmd
}

// parseRestoreTarget resolves the one target a restore takes. Cobra rejects
// --backup with --to and requires one of them, but an empty value satisfies
// both rules, so the emptiness check belongs here. Cobra also has no time flag,
// so --to arrives as a string and its format has to be named in the error.
func parseRestoreTarget(backupName, restoreTo string) (string, time.Time, error) {
	backupName = strings.TrimSpace(backupName)
	restoreTo = strings.TrimSpace(restoreTo)
	switch {
	case backupName != "":
		return backupName, time.Time{}, nil
	case restoreTo != "":
		target, err := time.Parse(time.RFC3339, restoreTo)
		if err != nil {
			return "", time.Time{}, fmt.Errorf("--to must be an RFC 3339 timestamp such as 2026-01-15T09:30:00Z: %w", err)
		}
		return "", target, nil
	default:
		return "", time.Time{}, errors.New("name a backup with --backup or a point in time with --to")
	}
}

func runRestore(ctx context.Context, opts restoreOptions) error {
	if !opts.yes {
		confirmed, err := confirm.Action(opts.in, opts.out,
			"Restoring replaces the database's data. Everything written after the point being restored is lost, "+
				"and the database serves no connections until the restore finishes.",
			fmt.Sprintf("Restore database '%s' %s?", opts.database, restoreTargetPhrase(opts)))
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	restore, err := startRestore(ctx, opts)
	if err != nil {
		return err
	}

	output.DatabaseRestoreStarted(opts.out, restore, opts.database, cliruntime.CommandPath(opts.deps, ""))
	return nil
}

func startRestore(ctx context.Context, opts restoreOptions) (*apiclient.DatabaseRestore, error) {
	service := clidatabase.NewService(opts.deps)
	if opts.backup != "" {
		return service.RestoreFromBackup(ctx, opts.database, opts.backup)
	}
	return service.RestoreToPointInTime(ctx, opts.database, opts.restoreTo)
}

func restoreTargetPhrase(opts restoreOptions) string {
	if opts.backup != "" {
		return fmt.Sprintf("from backup '%s'", opts.backup)
	}
	return "to " + opts.restoreTo.Format(time.RFC3339)
}
