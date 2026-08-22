// Package backup wires the volcano databases backups, restore, and
// backup-schedule subcommands.
package backup

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// New returns the backups command.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "backups",
		Aliases: []string{"backup"},
		Short:   "Manage database backups",
		Long: `Take point-in-time copies of a database and manage the ones you have.

A backup covers the database itself, not its branches, and is restorable in
place: restoring one replaces the database's data and keeps its connection
string. Restore a backup with the restore command.`,
	}
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newCreate(deps))
	cmd.AddCommand(newGet(deps))
	cmd.AddCommand(newDelete(deps))
	return cmd
}

// parseTarget validates the "<database> <backup>" arguments every per-backup
// command takes.
func parseTarget(args []string) (databaseName, backupName string, err error) {
	databaseName = strings.TrimSpace(args[0])
	backupName = strings.TrimSpace(args[1])
	if databaseName == "" {
		return "", "", errors.New("database name cannot be empty")
	}
	if backupName == "" {
		return "", "", errors.New("backup name cannot be empty")
	}
	return databaseName, backupName, nil
}

// parseDatabase validates the sole "<database>" argument the database-wide
// commands take.
func parseDatabase(args []string) (string, error) {
	databaseName := strings.TrimSpace(args[0])
	if databaseName == "" {
		return "", errors.New("database name cannot be empty")
	}
	return databaseName, nil
}
