package backup

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduleGetRendersEntries(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == schedulePath {
			writeBackupJSON(t, w, http.StatusOK, map[string]any{
				"entries": []any{
					map[string]any{"frequency": "weekly", "hour": 4, "day": 0, "retention_seconds": 604800},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeBackupCommand(t, newTestScheduleCommand(server), "get", "app")
	require.NoError(t, err)
	assert.Contains(t, out, "weekly")
	assert.Contains(t, out, "Sunday 04:00")
	assert.Contains(t, out, "7d")
}

func TestScheduleGetReportsNoScheduledBackups(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeBackupJSON(t, w, http.StatusOK, map[string]any{"entries": []any{}})
	}))
	defer server.Close()

	out, err := executeBackupCommand(t, newTestScheduleCommand(server), "get", "app")
	require.NoError(t, err)
	assert.Contains(t, out, "No scheduled backups of database 'app'")
}

func TestScheduleSetSendsOneEntry(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == schedulePath {
			decodeBody(t, r, &body)
			writeBackupJSON(t, w, http.StatusOK, map[string]any{
				"entries": []any{
					map[string]any{"frequency": "daily", "hour": 3, "retention_seconds": 604800},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeBackupCommand(t, newTestScheduleCommand(server), "set", "app", "--frequency", "daily", "--hour", "3")
	require.NoError(t, err)
	entries, ok := body["entries"].([]any)
	require.True(t, ok, "entries should be a list, got %v", body["entries"])
	require.Len(t, entries, 1)
	entry, ok := entries[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "daily", entry["frequency"])
	assert.InDelta(t, float64(3), entry["hour"], 0)
	assert.NotContains(t, entry, "day")
	assert.NotContains(t, entry, "retention_seconds")
	assert.Contains(t, out, "Backup schedule of database 'app' updated")
}

// Retention is clamped to the plan's, so the schedule the API sends back is what
// the command reports rather than the one it asked for.
func TestScheduleSetReportsClampedRetention(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodeBody(t, r, &body)
		writeBackupJSON(t, w, http.StatusOK, map[string]any{
			"entries": []any{
				map[string]any{"frequency": "weekly", "hour": 4, "day": 2, "retention_seconds": 604800},
			},
		})
	}))
	defer server.Close()

	out, err := executeBackupCommand(t, newTestScheduleCommand(server),
		"set", "app", "--frequency", "weekly", "--day", "2", "--hour", "4", "--retention", "720h")
	require.NoError(t, err)
	entries, ok := body["entries"].([]any)
	require.True(t, ok)
	entry, ok := entries[0].(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, float64(2592000), entry["retention_seconds"], 0)
	assert.Contains(t, out, "Tuesday 04:00")
	assert.Contains(t, out, "7d")
}

func TestScheduleSetClearSendsEmptySchedule(t *testing.T) {
	setBackupCommandTestHome(t)
	saveBackupCommandTestConfig(t)

	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodeBody(t, r, &body)
		writeBackupJSON(t, w, http.StatusOK, map[string]any{"entries": []any{}})
	}))
	defer server.Close()

	out, err := executeBackupCommand(t, newTestScheduleCommand(server), "set", "app", "--clear")
	require.NoError(t, err)
	entries, ok := body["entries"].([]any)
	require.True(t, ok, "entries should be an empty list, got %v", body["entries"])
	assert.Empty(t, entries)
	assert.Contains(t, out, "Scheduled backups of database 'app' stopped")
}

func TestScheduleSetRejectsInvalidFlags(t *testing.T) {
	server := refusingServer(t)
	defer server.Close()

	for name, testCase := range map[string]struct {
		args    []string
		message string
	}{
		"day with daily":      {args: []string{"--frequency", "daily", "--day", "3"}, message: "--day does not apply"},
		"monthly without day": {args: []string{"--frequency", "monthly"}, message: "--day must be between 1 and 28"},
		"weekday out of range": {
			args:    []string{"--frequency", "weekly", "--day", "9"},
			message: "--day must be between 0 (Sunday) and 6 (Saturday)",
		},
		"hour out of range":   {args: []string{"--frequency", "daily", "--hour", "24"}, message: "--hour must be between 0 and 23"},
		"unknown frequency":   {args: []string{"--frequency", "hourly"}, message: "--frequency must be daily, weekly, or monthly"},
		"clear with schedule": {args: []string{"--clear", "--frequency", "daily"}, message: "were all set"},
		"no schedule":         {args: nil, message: "at least one of the flags"},
	} {
		t.Run(name, func(t *testing.T) {
			setBackupCommandTestHome(t)
			saveBackupCommandTestConfig(t)

			_, err := executeBackupCommand(t, newTestScheduleCommand(server), append([]string{"set", "app"}, testCase.args...)...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.message)
		})
	}
}
