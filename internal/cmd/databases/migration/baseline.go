package migration

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/confirm"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// newBaseline returns `databases migration baseline`: record files in a
// database's migration history as applied, without running them, through
// Hosting's baseline route. It never reads the connection string, so it works
// under a delegated grant. The caller names the files; nothing is detected.
//
// Flags match the baseline command in Kong/volcano-cli#365, so the two merge.
func newBaseline(deps cliruntime.Deps) *cobra.Command {
	var database string
	var names []string
	var all, yes, jsonOutput bool
	cmd := &cobra.Command{
		Use:   "baseline",
		Short: "Record migrations as applied without running them",
		Long: fmt.Sprintf(`Record migration files in a database's migration history as applied, without
running them, so later migration runs skip them.

Use it for files the database already reflects, such as ones applied with
'databases migration up' before migration runs tracked them. A file recorded
this way never runs, so name only files whose objects the database already
holds. When a migration run fails because an object already exists, the error
names the object and the file; baseline that file if the database already
holds everything it creates.

Files the history already holds with the same content are skipped. A file
recorded with different content is refused (checksum_mismatch), and nothing is
recorded. Nothing reads the database's connection string, so this works with
a delegated grant (pg-).

By default this command prompts for confirmation. Use --yes to skip the prompt.

Usage:
  %s
  %s`,
			cliruntime.CommandPath(deps, "databases migration baseline -d mydb --all"),
			cliruntime.CommandPath(deps, "databases migration baseline -d mydb -f 001_create_users -f 002_add_posts")),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBaseline(cmd, baselineOptions{
				deps: deps, database: strings.TrimSpace(database), names: names, all: all, yes: yes, json: jsonOutput,
			})
		},
	}
	cmd.Flags().StringVarP(&database, "database", "d", "", "Database whose migration history to record in (required)")
	cmd.Flags().BoolVarP(&all, "all", "a", false, "Record every migration file")
	cmd.Flags().StringArrayVarP(&names, "file", "f", nil, "Record a migration by name or path; repeat for several")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit machine-readable JSON")
	if err := cmd.MarkFlagRequired("database"); err != nil {
		panic(err)
	}
	cmd.MarkFlagsMutuallyExclusive("all", "file")
	cmd.MarkFlagsOneRequired("all", "file")
	return cmd
}

type baselineOptions struct {
	deps     cliruntime.Deps
	database string
	names    []string
	all      bool
	yes      bool
	json     bool
}

func runBaseline(cmd *cobra.Command, opts baselineOptions) error {
	if opts.database == "" {
		return errors.New("specify the database with --database/-d")
	}
	all, err := scanMigrations(filepath.Join("volcano", "migrations"))
	if err != nil {
		return fmt.Errorf("failed to scan migrations: %w", err)
	}
	selected, err := selectBaselineFiles(all, opts.names, opts.all)
	if err != nil {
		return err
	}
	files, err := migrationRunFiles(selected)
	if err != nil {
		return err
	}
	project, err := clisession.NewFactory(opts.deps).CurrentProject()
	if err != nil {
		return err
	}

	if !opts.yes {
		// With --json, stdout carries only the JSON document.
		prompt := cmd.OutOrStdout()
		if opts.json {
			prompt = cmd.ErrOrStderr()
		}
		confirmed, err := confirmBaseline(cmd.InOrStdin(), prompt, opts.database, selected)
		if err != nil || !confirmed {
			return err
		}
	}

	baseline, err := project.API.BaselineDatabaseMigrations(cmd.Context(), project.ProjectID, opts.database, files)
	if err != nil {
		if api.Status(err) == http.StatusNotFound && api.Code(err) == api.FeatureUnavailableCode {
			return fmt.Errorf("migration runs are not available on this Volcano API, so there is no migration history "+
				"to record a baseline in: %w", err)
		}
		return fmt.Errorf("failed to record a baseline in %q: %w", opts.database, err)
	}
	out := cmd.OutOrStdout()
	if opts.json {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(baseline)
	}
	if len(baseline.Recorded) == 0 {
		fmt.Fprintf(out, "Nothing recorded: the migration history of %q already holds these files.\n", opts.database)
		return nil
	}
	fmt.Fprintf(out, "Recorded %d file(s) as applied in %q without running them:\n", len(baseline.Recorded), opts.database)
	for _, name := range baseline.Recorded {
		fmt.Fprintf(out, "  - %s\n", name)
	}
	return nil
}

// confirmBaseline asks before recording. A command no person answers, such
// as a worker's, has to pass --yes.
func confirmBaseline(in io.Reader, w io.Writer, database string, files []string) (bool, error) {
	if confirm.MarkedNonInteractive() {
		return false, confirm.ErrCannotPrompt
	}
	listed := make([]string, len(files))
	for i, file := range files {
		listed[i] = "  - " + filepath.Base(file)
	}
	return confirm.Action(in, w,
		fmt.Sprintf("A baseline records these files in the migration history of database '%s' as applied, "+
			"without running them. A file recorded this way never runs.\n%s", database, strings.Join(listed, "\n")),
		fmt.Sprintf("Record %d file(s) as applied?", len(files)))
}

// selectBaselineFiles picks the files a baseline records: all of them, or the
// ones named, each of which has to exist.
func selectBaselineFiles(all, names []string, everything bool) ([]string, error) {
	if len(all) == 0 {
		return nil, errors.New("no migration files found in volcano/migrations/")
	}
	if everything {
		return all, nil
	}
	var selected []string
	seen := map[string]bool{}
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			return nil, errors.New("--file needs a migration name")
		}
		files, err := selectMigrationFiles(all, name, false)
		if err != nil {
			return nil, err
		}
		if !seen[files[0]] {
			seen[files[0]] = true
			selected = append(selected, files[0])
		}
	}
	return selected, nil
}
