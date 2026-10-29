package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

func newMigrationsTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
	require.NoError(t, err)
	return client
}

// A capability counts only when the list names it available: any other
// status, or none, is unavailable.
func TestCapabilityAvailable(t *testing.T) {
	client := newMigrationsTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"a","status":"available"},{"id":"b","status":"unavailable"},{"id":"c","status":"paused"}]}`))
	})
	for id, want := range map[string]bool{"a": true, "b": false, "c": false, "missing": false} {
		available, err := client.CapabilityAvailable(context.Background(), id)
		require.NoError(t, err)
		assert.Equal(t, want, available, id)
	}
}

func TestMigrationRunConflictKeepsTheRunItNames(t *testing.T) {
	runID := uuid.MustParse("7f3a1c2e-5d4b-4a91-8c6f-2b1e9d0a4c73")
	var key string
	client := newMigrationsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		key = r.Header.Get("Idempotency-Key")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"a migration run is already queued or running","code":"migration_run_in_progress","run_id":"` + runID.String() + `"}`))
	})

	_, err := client.CreateDatabaseMigrationRun(context.Background(), uuid.New(), "app", "volcano-cli-1",
		[]apiclient.DatabaseMigrationFile{{Name: "001_a.sql", Sql: "select 1;"}})
	inProgress, ok := errors.AsType[*MigrationRunInProgressError](err)
	require.True(t, ok, err)
	assert.Equal(t, runID, inProgress.RunID)
	assert.Equal(t, http.StatusConflict, Status(err))
	assert.Equal(t, CodeMigrationRunInProgress, Code(err))
	assert.Equal(t, "volcano-cli-1", key)
}

// An Idempotency-Key replay answers 200 with the run the key started.
func TestMigrationRunReplayReturnsTheRun(t *testing.T) {
	client := newMigrationsTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"7f3a1c2e-5d4b-4a91-8c6f-2b1e9d0a4c73","database":{"type":"database","id":"0b9c3f5e-2a41-4d7e-9f0a-6c8e1d2b3a45","name":"app"},` +
			`"status":"running","phase":"apply","source":"api","warnings":[],"created_at":"2026-10-29T10:00:00Z","updated_at":"2026-10-29T10:00:00Z"}`))
	})

	run, err := client.CreateDatabaseMigrationRun(context.Background(), uuid.New(), "app", "volcano-cli-1",
		[]apiclient.DatabaseMigrationFile{{Name: "001_a.sql", Sql: "select 1;"}})
	require.NoError(t, err)
	assert.Equal(t, apiclient.DatabaseMigrationRunStatusRunning, run.Status)
}
