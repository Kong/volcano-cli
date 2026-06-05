package migration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const migrationProjectID = "22222222-2222-4222-8222-222222222222"

type migrationExecution struct {
	connectionString string
	sql              string
}

func TestMigrationsUpAllExecutesSortedFiles(t *testing.T) {
	setMigrationCommandTestHome(t)
	t.Chdir(t.TempDir())
	require.NoError(t, os.MkdirAll(filepath.Join("volcano", "migrations", "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("volcano", "migrations", "002_second.sql"), []byte("CREATE TABLE second;"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join("volcano", "migrations", "001_first.sql"), []byte("CREATE TABLE first;"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join("volcano", "migrations", "003_third.sql"), []byte("CREATE TABLE third;"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join("volcano", "migrations", "nested", "004_nested.sql"), []byte("CREATE TABLE nested;"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join("volcano", "migrations", "notes.txt"), []byte("ignore me"), 0o644))
	saveMigrationCommandTestConfig(t)

	connectionString := "postgres://user:secret@example/app"
	server := newMigrationDatabaseServer(t, http.StatusOK, migrationDatabasePayload("app", "active", &connectionString))
	defer server.Close()

	var executions []migrationExecution
	executor := func(_ context.Context, conn, sql string) error {
		executions = append(executions, migrationExecution{connectionString: conn, sql: sql})
		return nil
	}

	out, err := executeMigrationCommand(t, newWithExecutor(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}, executor), "up", "--all", "-d", "app")
	require.NoError(t, err)

	require.Len(t, executions, 3)
	assert.Equal(t, []string{"CREATE TABLE first;", "CREATE TABLE second;", "CREATE TABLE third;"}, migrationExecutionSQL(executions))
	for _, execution := range executions {
		assert.Equal(t, connectionString, execution.connectionString)
	}
	for _, want := range []string{
		"Scanning volcano/migrations",
		"Found 3 file(s)",
		"001_first.sql",
		"002_second.sql",
		"003_third.sql",
		"Using database: app",
		"Warning: Migrations will be executed without tracking.",
		"Applying 001_first.sql... ok",
		"Applying 002_second.sql... ok",
		"Applying 003_third.sql... ok",
		"Migrations deployed!",
	} {
		assert.Contains(t, out, want)
	}
	for _, secret := range []string{connectionString, "CREATE TABLE first", "CREATE TABLE second", "CREATE TABLE third", "CREATE TABLE nested"} {
		assert.NotContains(t, out, secret)
	}
	assert.NotContains(t, out, "004_nested.sql")
}

func TestMigrationsUpFileAcceptsNameExtensionOrPath(t *testing.T) {
	for _, target := range []string{
		"001_create_users",
		"001_create_users.sql",
		filepath.Join("volcano", "migrations", "001_create_users.sql"),
	} {
		t.Run(target, func(t *testing.T) {
			setMigrationCommandTestHome(t)
			t.Chdir(t.TempDir())
			require.NoError(t, os.MkdirAll(filepath.Join("volcano", "migrations"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join("volcano", "migrations", "001_create_users.sql"), []byte("CREATE TABLE users;"), 0o644))
			saveMigrationCommandTestConfig(t)

			connectionString := "postgres://example/app"
			server := newMigrationDatabaseServer(t, http.StatusOK, migrationDatabasePayload("app", "active", &connectionString))
			defer server.Close()

			var executions []migrationExecution
			executor := func(_ context.Context, conn, sql string) error {
				executions = append(executions, migrationExecution{connectionString: conn, sql: sql})
				return nil
			}

			out, err := executeMigrationCommand(t, newWithExecutor(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}, executor), "up", "-d", "app", "-f", target)
			require.NoError(t, err)
			assert.Equal(t, []string{"CREATE TABLE users;"}, migrationExecutionSQL(executions))
			assert.Contains(t, out, "Executing migration: 001_create_users.sql")
			assert.Contains(t, out, "Applying 001_create_users.sql... ok")
		})
	}
}

func TestMigrationsUpValidatesFlags(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{
			name: "requires database",
			args: []string{"up", "--all"},
			want: "required flag(s) \"database\" not set",
		},
		{
			name: "requires target",
			args: []string{"up", "-d", "app"},
			want: "specify either --all",
		},
		{
			name: "rejects all and file",
			args: []string{"up", "--all", "-d", "app", "-f", "001_test"},
			want: "cannot use --all and --file together",
		},
		{
			name: "rejects positional all",
			args: []string{"up", "all", "-d", "app"},
			want: "unknown command \"all\"",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setMigrationCommandTestHome(t)
			t.Chdir(t.TempDir())

			out, err := executeMigrationCommand(t, newWithExecutor(cliruntime.Deps{}, nil), tc.args...)
			require.Error(t, err)
			assert.Contains(t, strings.ToLower(out+err.Error()), strings.ToLower(tc.want))
		})
	}
}

func TestMigrationsUpHandlesNoMigrationFiles(t *testing.T) {
	setMigrationCommandTestHome(t)
	t.Chdir(t.TempDir())

	out, err := executeMigrationCommand(t, newWithExecutor(cliruntime.Deps{}, nil), "up", "--all", "-d", "app")
	require.NoError(t, err)
	assert.Contains(t, out, "No migration files found in volcano/migrations/")

	_, err = executeMigrationCommand(t, newWithExecutor(cliruntime.Deps{}, nil), "up", "-d", "app", "-f", "001_missing")
	require.ErrorContains(t, err, "no migration files found in volcano/migrations")
}

func TestMigrationsUpReportsAvailableMigrationsWhenTargetMissing(t *testing.T) {
	setMigrationCommandTestHome(t)
	t.Chdir(t.TempDir())
	require.NoError(t, os.MkdirAll(filepath.Join("volcano", "migrations"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("volcano", "migrations", "001_create_users.sql"), []byte("CREATE TABLE users;"), 0o644))

	_, err := executeMigrationCommand(t, newWithExecutor(cliruntime.Deps{}, nil), "up", "-d", "app", "-f", "missing")
	require.ErrorContains(t, err, "migration \"missing\" not found")
	require.ErrorContains(t, err, "available migrations: 001_create_users.sql")
}

func TestMigrationsUpValidatesDatabase(t *testing.T) {
	for _, tc := range []struct {
		name           string
		responseStatus int
		payload        any
		want           string
	}{
		{
			name:           "not found",
			responseStatus: http.StatusNotFound,
			payload:        map[string]string{"error": "not found"},
			want:           "failed to get database",
		},
		{
			name:           "inactive",
			responseStatus: http.StatusOK,
			payload:        migrationDatabasePayload("app", "provisioning", stringPtr("postgres://example/app")),
			want:           "is not active",
		},
		{
			name:           "missing connection string",
			responseStatus: http.StatusOK,
			payload:        migrationDatabasePayload("app", "active", nil),
			want:           "does not have a connection string",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setMigrationCommandTestHome(t)
			t.Chdir(t.TempDir())
			require.NoError(t, os.MkdirAll(filepath.Join("volcano", "migrations"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join("volcano", "migrations", "001_create_users.sql"), []byte("CREATE TABLE users;"), 0o644))
			saveMigrationCommandTestConfig(t)

			server := newMigrationDatabaseServer(t, tc.responseStatus, tc.payload)
			defer server.Close()

			executor := func(context.Context, string, string) error {
				t.Fatal("executor should not be called")
				return nil
			}

			_, err := executeMigrationCommand(t, newWithExecutor(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}, executor), "up", "--all", "-d", "app")
			require.Error(t, err)
			assert.Contains(t, strings.ToLower(err.Error()), strings.ToLower(tc.want))
		})
	}
}

func TestMigrationsUpFailsFastOnSQLError(t *testing.T) {
	setMigrationCommandTestHome(t)
	t.Chdir(t.TempDir())
	require.NoError(t, os.MkdirAll(filepath.Join("volcano", "migrations"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("volcano", "migrations", "001_bad.sql"), []byte("BAD SQL;"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join("volcano", "migrations", "002_later.sql"), []byte("CREATE TABLE later;"), 0o644))
	saveMigrationCommandTestConfig(t)

	connectionString := "postgres://example/app"
	server := newMigrationDatabaseServer(t, http.StatusOK, migrationDatabasePayload("app", "active", &connectionString))
	defer server.Close()

	var executions []migrationExecution
	executor := func(_ context.Context, conn, sql string) error {
		executions = append(executions, migrationExecution{connectionString: conn, sql: sql})
		return errors.New("relation \"users\" already exists")
	}

	out, err := executeMigrationCommand(t, newWithExecutor(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}, executor), "up", "--all", "-d", "app")
	require.ErrorContains(t, err, "migration 001_bad.sql failed")
	require.ErrorContains(t, err, "relation \"users\" already exists")
	assert.Equal(t, []string{"BAD SQL;"}, migrationExecutionSQL(executions))
	assert.Contains(t, out, "Applying 001_bad.sql... error")
	assert.NotContains(t, out, "Applying 002_later.sql")
}

func executeMigrationCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func setMigrationCommandTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
}

func saveMigrationCommandTestConfig(t *testing.T) {
	t.Helper()
	cfg := &cliconfig.Config{
		UserToken: "token",
		CurrentProject: &cliconfig.ProjectConfig{
			ID:   migrationProjectID,
			Name: "Beta",
		},
	}
	require.NoError(t, cfg.Save())
}

func newMigrationDatabaseServer(t *testing.T, responseStatus int, payload any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		if r.Method != http.MethodGet || r.URL.Path != "/projects/"+migrationProjectID+"/databases/app" {
			http.NotFound(w, r)
			return
		}
		writeMigrationCommandJSON(t, w, responseStatus, payload)
	}))
}

func writeMigrationCommandJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func migrationDatabasePayload(name, status string, connectionString *string) map[string]any {
	payload := map[string]any{
		"created_at":    time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
		"id":            "33333333-3333-4333-8333-333333333333",
		"name":          name,
		"pg_version":    "16",
		"project_id":    migrationProjectID,
		"region":        "aws-us-east-1",
		"status":        status,
		"updated_at":    time.Now().Add(-30 * time.Minute).UTC().Format(time.RFC3339),
		"database_type": "volcano-db-s",
	}
	if connectionString != nil {
		payload["connection_string"] = *connectionString
	}
	return payload
}

func migrationExecutionSQL(executions []migrationExecution) []string {
	values := make([]string, 0, len(executions))
	for _, execution := range executions {
		values = append(values, execution.sql)
	}
	return values
}

func stringPtr(value string) *string {
	return &value
}
