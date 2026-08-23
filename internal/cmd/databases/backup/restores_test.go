package backup

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRestoresListRendersHistory(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == restoresPath {
			fromBackup := restorePayload("snapshot", "completed")
			fromBackup["backup_name"] = "nightly"
			pointInTime := restorePayload("point_in_time", "running")
			pointInTime["id"] = "77777777-7777-4777-8777-777777777777"
			pointInTime["restore_to"] = "2026-01-15T09:30:00Z"
			writeBackupJSON(t, w, http.StatusOK, map[string]any{
				"data": []map[string]any{pointInTime, fromBackup},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeBackupCommand(t, newTestRestoresCommand(server), "list", "app")
	require.NoError(t, err)
	assert.Contains(t, out, restoreID)
	assert.Contains(t, out, "completed")
	assert.Contains(t, out, "running")
	assert.Contains(t, out, "nightly")
	assert.Contains(t, out, "Showing 2 restore(s) of database 'app'")
}

// A database that has never been restored is the normal case, not an error, and
// has to read as one.
func TestRestoresListReportsANeverRestoredDatabase(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == restoresPath {
			writeBackupJSON(t, w, http.StatusOK, map[string]any{"data": []map[string]any{}})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeBackupCommand(t, newTestRestoresCommand(server), "list", "app")
	require.NoError(t, err)
	assert.Contains(t, out, "Database 'app' has never been restored")
}

// The reason a restore was given up on lives on the restore and nowhere else:
// the database it left behind reports a status and nothing more. The note must
// not name that status, because an exhausted restore that never touched the data
// — this one, whose backup was gone — leaves the database active.
func TestRestoresGetReportsWhyARestoreWasGivenUpOn(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == restorePath {
			payload := restorePayload("snapshot", "exhausted")
			payload["backup_name"] = "nightly"
			payload["error"] = "snapshot no longer exists"
			payload["completed_at"] = "2026-05-20T01:00:00Z"
			writeBackupJSON(t, w, http.StatusOK, payload)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeBackupCommand(t, newTestRestoresCommand(server), "get", "app", restoreID)
	require.NoError(t, err)
	assert.Contains(t, out, "exhausted")
	assert.Contains(t, out, "snapshot no longer exists")
	assert.Contains(t, out, "Volcano gave up on this restore")
	assert.NotContains(t, out, "the database was left failed")
}

// A restore still running is the other reason to read one, and it has to say
// that the database is out of service rather than leaving that to be inferred.
func TestRestoresGetReportsARunningRestore(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == restorePath {
			writeBackupJSON(t, w, http.StatusOK, restorePayload("point_in_time", "running"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeBackupCommand(t, newTestRestoresCommand(server), "get", "app", restoreID)
	require.NoError(t, err)
	assert.Contains(t, out, "running")
	assert.Contains(t, out, "serves no connections until this finishes")
}

// A restore id from another database is refused by the API rather than read
// across the scope boundary.
func TestRestoresGetSurfacesAMissingRestore(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == restorePath {
			writeBackupJSON(t, w, http.StatusNotFound, map[string]any{"error": "restore not found"})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	_, err := executeBackupCommand(t, newTestRestoresCommand(server), "get", "app", restoreID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "restore not found")
}

// Checked before the request: sent on, a mistyped id comes back as a 404 that
// reads as though the restore had been deleted.
func TestRestoresGetRejectsAnIdThatIsNotOne(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := refusingServer(t)
	defer server.Close()

	_, err := executeBackupCommand(t, newTestRestoresCommand(server), "get", "app", "the-last-one")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "restore id must be a UUID")
}
