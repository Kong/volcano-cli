package database

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// MigrationsCapability is the capability GET /capabilities lists as available
// where migration runs and baselines are.
const MigrationsCapability = "databases.migrations"

// ErrMigrationRunsUnavailable reports an API that takes no migration runs or
// baselines: one where Volcano has not turned them on, or a release older than
// them. Reading the history and runs works either way.
var ErrMigrationRunsUnavailable = errors.New("tracked migration runs aren't available on this Volcano API")

const codeFeatureUnavailable = "feature_unavailable"

const (
	migrationRunPollInterval = 2 * time.Second
	// migrationRunWaitTimeout outlasts a run's one-hour limit and the retry
	// backoff of its last phase, so a wait ends with the run's own outcome.
	migrationRunWaitTimeout = 90 * time.Minute
	// migrationRunPollFailures is how many reads in a row may fail before a
	// wait gives up; the run carries on without the CLI.
	migrationRunPollFailures = 5
)

// MigrationRunDone reports whether a run has ended.
func MigrationRunDone(run *apiclient.DatabaseMigrationRun) bool {
	return run.Status == apiclient.DatabaseMigrationRunStatusSucceeded || run.Status == apiclient.DatabaseMigrationRunStatusFailed
}

// RequireMigrationRuns returns ErrMigrationRunsUnavailable unless the API
// lists MigrationsCapability as available. Callers check it before they change
// anything, so an unavailable API refuses the whole command.
func (s Service) RequireMigrationRuns(ctx context.Context) error {
	authenticated, err := s.sessions.Authenticated()
	if err != nil {
		return err
	}
	available, err := authenticated.API.CapabilityAvailable(ctx, MigrationsCapability)
	if api.RouteMissing(err) {
		// A release older than GET /capabilities is older than migration runs.
		return ErrMigrationRunsUnavailable
	}
	if err != nil {
		return fmt.Errorf("failed to read which capabilities the API offers: %w", err)
	}
	if !available {
		return ErrMigrationRunsUnavailable
	}
	return nil
}

// MigrationHistory returns the files a database's migration history records,
// and the run that is queued or running, if any.
func (s Service) MigrationHistory(ctx context.Context, databaseName string) (*apiclient.DatabaseMigrations, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	history, err := authenticated.API.GetDatabaseMigrations(ctx, authenticated.ProjectID, databaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to read the migration history of database %q: %w", databaseName, err)
	}
	return history, nil
}

// BaselineMigrations records files in a database's migration history as
// applied without running them.
func (s Service) BaselineMigrations(ctx context.Context, databaseName string, files []MigrationFile) (*apiclient.DatabaseMigrationBaseline, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	baseline, err := authenticated.API.BaselineDatabaseMigrations(ctx, authenticated.ProjectID, databaseName, requestFiles(files))
	if err != nil {
		return nil, unavailable(fmt.Errorf("failed to record a baseline on database %q: %w", databaseName, err))
	}
	return baseline, nil
}

// StartMigrationRun queues a run of files on a database. The API applies only
// the files the history does not record, so sending every file is safe.
func (s Service) StartMigrationRun(ctx context.Context, databaseName string, files []MigrationFile) (*apiclient.DatabaseMigrationRun, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	// A fresh key per command makes a retried request return the run it
	// started, while running the command again starts a new run.
	key := "volcano-cli-" + uuid.NewString()
	run, err := authenticated.API.CreateDatabaseMigrationRun(ctx, authenticated.ProjectID, databaseName, key, requestFiles(files))
	if err != nil {
		return nil, unavailable(fmt.Errorf("failed to start a migration run on database %q: %w", databaseName, err))
	}
	return run, nil
}

// MigrationRuns returns a database's 50 most recent migration runs.
func (s Service) MigrationRuns(ctx context.Context, databaseName string) (*apiclient.DatabaseMigrationRunList, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	runs, err := authenticated.API.ListDatabaseMigrationRuns(ctx, authenticated.ProjectID, databaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to list the migration runs of database %q: %w", databaseName, err)
	}
	return runs, nil
}

// MigrationRun returns one migration run of a database.
func (s Service) MigrationRun(ctx context.Context, databaseName string, runID uuid.UUID) (*apiclient.DatabaseMigrationRun, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	run, err := authenticated.API.GetDatabaseMigrationRun(ctx, authenticated.ProjectID, databaseName, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to get migration run %s of database %q: %w", runID, databaseName, err)
	}
	return run, nil
}

// ErrMigrationRunWaitTimedOut reports a run that had not ended when the CLI
// stopped waiting for it. The run carries on.
var ErrMigrationRunWaitTimedOut = errors.New("the migration run had not ended")

// WaitMigrationRun polls run until it ends and returns it as it ended.
// changed sees the run each time its status or phase changes.
func (s Service) WaitMigrationRun(ctx context.Context, databaseName string, run *apiclient.DatabaseMigrationRun, changed func(*apiclient.DatabaseMigrationRun)) (*apiclient.DatabaseMigrationRun, error) {
	if MigrationRunDone(run) {
		return run, nil
	}
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	timeout := cliruntime.NewTimer(s.deps, migrationRunWaitTimeout)
	defer timeout.Stop()
	ticker := cliruntime.NewTicker(s.deps, migrationRunPollInterval)
	defer ticker.Stop()

	failures := 0
	for {
		select {
		case <-ctx.Done():
			return run, ctx.Err()
		case <-timeout.C():
			return run, fmt.Errorf("%w after %s", ErrMigrationRunWaitTimedOut, migrationRunWaitTimeout)
		case <-ticker.C():
		}
		latest, err := authenticated.API.GetDatabaseMigrationRun(ctx, authenticated.ProjectID, databaseName, run.Id)
		if err != nil {
			failures++
			if transient(err) && failures < migrationRunPollFailures {
				continue
			}
			return run, fmt.Errorf("failed to get migration run %s of database %q: %w", run.Id, databaseName, err)
		}
		failures = 0
		if latest.Status != run.Status || latest.Phase != run.Phase {
			changed(latest)
		}
		run = latest
		if MigrationRunDone(run) {
			return run, nil
		}
	}
}

// transient reports a read worth trying again: the server failed, or it could
// not be reached.
func transient(err error) bool {
	status := api.Status(err)
	return status == 0 || status >= http.StatusInternalServerError
}

// unavailable replaces a refusal that says migration runs are not available
// with ErrMigrationRunsUnavailable, keeping the refusal so the exit code still
// reports it.
func unavailable(err error) error {
	if api.RouteMissing(err) || api.Code(err) == codeFeatureUnavailable {
		return api.WithCause(ErrMigrationRunsUnavailable, err)
	}
	return err
}

func requestFiles(files []MigrationFile) []apiclient.DatabaseMigrationFile {
	out := make([]apiclient.DatabaseMigrationFile, len(files))
	for i, file := range files {
		out[i] = apiclient.DatabaseMigrationFile{Name: file.Path, Sql: file.SQL}
	}
	return out
}
