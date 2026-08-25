package backup

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRestoreFromBackupSendsBackupName(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == restoresPath {
			decodeBody(t, r, &body)
			payload := restorePayload("snapshot", "pending")
			payload["backup_name"] = "nightly"
			writeBackupJSON(t, w, http.StatusAccepted, payload)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeBackupCommand(t, newTestRestoreCommand(server), "app", "--backup", "nightly", "--yes")
	require.NoError(t, err)
	assert.Equal(t, "nightly", body["backup_name"])
	assert.NotContains(t, body, "restore_to")
	assert.Contains(t, out, "Restore of database 'app' started from backup 'nightly'")
	assert.Contains(t, out, "pending")
	// The id it just printed, in the command that reads it: the database only
	// reports that it is restoring, never why an attempt failed.
	assert.Contains(t, out, "volcano databases restores get app "+restoreID)
}

func TestRestoreToTimestampSendsRestoreTo(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == restoresPath {
			decodeBody(t, r, &body)
			payload := restorePayload("point_in_time", "pending")
			payload["restore_to"] = "2026-01-15T09:30:00Z"
			writeBackupJSON(t, w, http.StatusAccepted, payload)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeBackupCommand(t, newTestRestoreCommand(server), "app", "--to", "2026-01-15T09:30:00Z", "--yes")
	require.NoError(t, err)
	assert.Equal(t, "2026-01-15T09:30:00Z", body["restore_to"])
	assert.NotContains(t, body, "backup_name")
	assert.Contains(t, out, "Restore of database 'app' started to ")
}

func TestRestoreRejectsMalformedTimestamp(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := refusingServer(t)
	defer server.Close()

	_, err := executeBackupCommand(t, newTestRestoreCommand(server), "app", "--to", "yesterday", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RFC 3339")
}

func TestRestoreRejectsBothTargets(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := refusingServer(t)
	defer server.Close()

	_, err := executeBackupCommand(t, newTestRestoreCommand(server),
		"app", "--backup", "nightly", "--to", "2026-01-15T09:30:00Z", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "were all set")
}

func TestRestoreRequiresATarget(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := refusingServer(t)
	defer server.Close()

	_, err := executeBackupCommand(t, newTestRestoreCommand(server), "app", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backup")
}

// An empty --backup satisfies cobra's one-required rule, so the command has to
// reject it itself rather than silently starting a point-in-time restore.
func TestRestoreRejectsEmptyBackupName(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := refusingServer(t)
	defer server.Close()

	_, err := executeBackupCommand(t, newTestRestoreCommand(server), "app", "--backup", "  ", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name a backup with --backup")
}

// The same hole on the other flag: an empty --to must not read as "restore to
// the zero time", which the platform would answer with a window rejection long
// after the request was already meaningless.
func TestRestoreRejectsEmptyTimestamp(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := refusingServer(t)
	defer server.Close()

	_, err := executeBackupCommand(t, newTestRestoreCommand(server), "app", "--to", "  ", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name a backup with --backup")
}

func TestRestoreRequiresConfirmation(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := refusingServer(t)
	defer server.Close()

	out, err := executeBackupCommand(t, newTestRestoreCommand(server), "app", "--backup", "nightly")
	require.NoError(t, err)
	assert.Contains(t, out, "Restore database 'app' from backup 'nightly'?")
	assert.Contains(t, out, "Cancelled.")
}

func TestRestoreSurfacesConflict(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeBackupJSON(t, w, http.StatusConflict, map[string]any{
			"error": "a restore of this database is already in progress",
		})
	}))
	defer server.Close()

	_, err := executeBackupCommand(t, newTestRestoreCommand(server), "app", "--backup", "nightly", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already in progress")
}

func TestRestoreSurfacesWindowRejection(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeBackupJSON(t, w, http.StatusBadRequest, map[string]any{
			"error": "restore_to is outside the available restore window",
		})
	}))
	defer server.Close()

	_, err := executeBackupCommand(t, newTestRestoreCommand(server), "app", "--to", "2020-01-15T09:30:00Z", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside the available restore window")
}
