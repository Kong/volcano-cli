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

// A database that is still provisioning has no storage behind it, so the reads
// answer 409 rather than an empty list. Reporting it as "no backups" would read
// as data loss on a database that never had any.
func TestListSurfacesADatabaseWithNoStorageYet(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeBackupJSON(t, w, http.StatusConflict, map[string]any{
			"error": "database has no provider project",
		})
	}))
	defer server.Close()

	_, err := executeBackupCommand(t, newTestCommand(server), "list", "app")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database has no provider project")
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

// The name is the backup's identity, so a create that collides is refused
// rather than making a second backup the user cannot tell apart. The same 409
// answers a backup taken moments ago.
func TestCreateSurfacesDuplicateName(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeBackupJSON(t, w, http.StatusConflict, map[string]any{
			"error": "backup name already exists",
		})
	}))
	defer server.Close()

	_, err := executeBackupCommand(t, newTestCommand(server), "create", "app", "nightly")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backup name already exists")
}

func TestGetSurfacesMissingBackup(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeBackupJSON(t, w, http.StatusNotFound, map[string]any{"error": "backup not found"})
	}))
	defer server.Close()

	_, err := executeBackupCommand(t, newTestCommand(server), "get", "app", "nightly")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backup not found")
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

// A restore is pinned to a backup it may not have used yet, so the platform
// refuses to delete one while a restore runs. The CLI has nothing to add beyond
// showing why the delete did not happen.
func TestDeleteSurfacesRestoreInProgress(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeBackupJSON(t, w, http.StatusConflict, map[string]any{
			"error": "a restore is already in progress for this database",
		})
	}))
	defer server.Close()

	_, err := executeBackupCommand(t, newTestCommand(server), "delete", "app", "nightly", "-y")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "restore is already in progress")
}

// Backups are a Pro capability, so the plan refusal reaches even the reads. The
// CLI has to pass the reason through: "403" on its own reads as a permissions
// problem with the token rather than a plan that does not include the feature.
func TestListSurfacesAPlanWithoutBackups(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeBackupJSON(t, w, http.StatusForbidden, map[string]any{
			"error": "backups are not available on this plan",
		})
	}))
	defer server.Close()

	_, err := executeBackupCommand(t, newTestCommand(server), "list", "app")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not available on this plan")
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
