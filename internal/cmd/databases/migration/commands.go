package migration

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// commands names the migration commands for one database, in the tree the
// command runs in.
type commands struct {
	deps     cliruntime.Deps
	database string
}

func (c commands) path(args string) string {
	return cliruntime.CommandPath(c.deps, "databases migration "+args+" -d "+cliruntime.ShellQuote(c.database))
}

func (c commands) apply() string    { return c.path("apply") }
func (c commands) up() string       { return c.path("up --all") }
func (c commands) baseline() string { return c.path("baseline --all") }

func (c commands) runsGet(runID string) string {
	return cliruntime.CommandPath(c.deps, "databases migration runs get "+runID+" -d "+cliruntime.ShellQuote(c.database))
}

func (c commands) output() output.MigrationCommands {
	return output.MigrationCommands{Apply: c.apply(), Baseline: c.baseline(), RunsGet: c.runsGet}
}

// explain says what to do about a refusal a migration command can meet,
// keeping the refusal so the exit code still reports it.
func (c commands) explain(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, clidatabase.ErrMigrationRunsUnavailable) {
		return api.WithCause(fmt.Errorf("%w (capability %q). "+
			"To run the files without tracking, which runs every file again, use '%s'",
			clidatabase.ErrMigrationRunsUnavailable, clidatabase.MigrationsCapability, c.up()), err)
	}
	if inProgress, ok := errors.AsType[*api.MigrationRunInProgressError](err); ok {
		return api.WithCause(fmt.Errorf("%w\nWait for it to end with '%s', then run this command again",
			err, c.runsGet(inProgress.RunID.String())), err)
	}
	if api.Code(err) == "history_missing" {
		return api.WithCause(fmt.Errorf("%w\nRecord the files that were already applied with '%s'",
			err, c.path("baseline -f <file>")), err)
	}
	return err
}

// databaseFlag adds the required --database flag.
func databaseFlag(cmd *cobra.Command, database *string) {
	cmd.Flags().StringVarP(database, "database", "d", "", "Database name (required)")
	if err := cmd.MarkFlagRequired("database"); err != nil {
		panic(err)
	}
}

func requireDatabase(database string) (string, error) {
	database = strings.TrimSpace(database)
	if database == "" {
		return "", errors.New("specify the database with --database/-d")
	}
	return database, nil
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
