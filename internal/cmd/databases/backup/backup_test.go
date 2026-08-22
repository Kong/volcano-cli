package backup

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListRendersBackupsAndRestoreWindow(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	manual := backupPayload("nightly", "manual")
	manual["size_bytes"] = 2048
	manual["expires_at"] = time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == backupsPath {
			writeBackupJSON(t, w, http.StatusOK, map[string]any{
				"data": []any{manual, backupPayload("scheduled_20260520", "scheduled")},
				"restore_window": map[string]any{
					"earliest_restore_at": "2026-05-13T00:00:00Z",
					"latest_restore_at":   "2026-05-20T00:00:00Z",
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeBackupCommand(t, newTestCommand(server), "list", "app")
	require.NoError(t, err)
	assert.Contains(t, out, "nightly")
	assert.Contains(t, out, "manual")
	assert.Contains(t, out, "scheduled")
	assert.Contains(t, out, "2.0 KiB")
	assert.Contains(t, out, "Showing 2 backup(s) of database 'app'")
	assert.Contains(t, out, "Point-in-time restore window:")
}

func TestListReportsNoBackups(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeBackupJSON(t, w, http.StatusOK, map[string]any{"data": []any{}})
	}))
	defer server.Close()

	out, err := executeBackupCommand(t, newTestCommand(server), "list", "app")
	require.NoError(t, err)
	assert.Contains(t, out, "No backups of database 'app'")
	assert.NotContains(t, out, "Point-in-time restore window:")
}

// A plan without point-in-time restore reports no window. The list still works,
// so an absent window prints nothing rather than an empty range.
func TestListOmitsRestoreWindowWhenPlanExcludesIt(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeBackupJSON(t, w, http.StatusOK, map[string]any{
			"data": []any{backupPayload("nightly", "manual")},
		})
	}))
	defer server.Close()

	out, err := executeBackupCommand(t, newTestCommand(server), "list", "app")
	require.NoError(t, err)
	assert.Contains(t, out, "nightly")
	assert.NotContains(t, out, "Point-in-time restore window:")
}

func TestCreateSendsNameAndReportsBackup(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == backupsPath {
			decodeBody(t, r, &body)
			writeBackupJSON(t, w, http.StatusCreated, backupPayload("nightly", "manual"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeBackupCommand(t, newTestCommand(server), "create", "app", "nightly")
	require.NoError(t, err)
	assert.Equal(t, "nightly", body["name"])
	assert.Contains(t, out, "Backup 'nightly' of database 'app' created")
	assert.Contains(t, out, "Expires: never")
}

func TestCreateSurfacesBackupCapError(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeBackupJSON(t, w, http.StatusForbidden, map[string]any{
			"error": "database has reached its backup limit (max 5 per database)",
		})
	}))
	defer server.Close()

	_, err := executeBackupCommand(t, newTestCommand(server), "create", "app", "nightly")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max 5 per database")
}

func TestGetReportsUncostedSize(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == backupPath {
			writeBackupJSON(t, w, http.StatusOK, backupPayload("nightly", "manual"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeBackupCommand(t, newTestCommand(server), "get", "app", "nightly")
	require.NoError(t, err)
	assert.Contains(t, out, "Name: nightly")
	assert.Contains(t, out, "Size: -")
}

func TestDeleteRequiresConfirmation(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := refusingServer(t)
	defer server.Close()

	out, err := executeBackupCommand(t, newTestCommand(server), "delete", "app", "nightly")
	require.NoError(t, err)
	assert.Contains(t, out, "Delete cancelled.")
}

func TestDeleteProceedsWithYes(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == backupPath {
			writeBackupJSON(t, w, http.StatusOK, map[string]any{
				"status":  "deleted",
				"message": "backup deleted",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeBackupCommand(t, newTestCommand(server), "delete", "app", "nightly", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "Backup 'nightly' of database 'app' deleted")
}

func TestDeleteSurfacesMissingBackup(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeBackupJSON(t, w, http.StatusNotFound, map[string]any{"error": "backup not found"})
	}))
	defer server.Close()

	_, err := executeBackupCommand(t, newTestCommand(server), "delete", "app", "nightly", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backup not found")
}
