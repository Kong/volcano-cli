package databases

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const databasePath = "/projects/" + databaseProjectID + "/databases/app"

// resizeAPI serves one database and records the sizes it was asked for.
type resizeAPI struct {
	t       *testing.T
	current string
	// refuse, when set, answers the resize with this status and message.
	refuse  int
	message string

	mu      sync.Mutex
	resizes []string
}

func (a *resizeAPI) serve() *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == databasePath:
			payload := databaseCommandPayload("11111111-1111-4111-8111-111111111111", databaseProjectID, "app")
			payload["database_type"] = a.current
			respondDatabaseJSON(a.t, w, http.StatusOK, payload)
		case r.Method == http.MethodPatch && r.URL.Path == databasePath+"/type":
			var body struct {
				DatabaseType string `json:"database_type"`
			}
			assert.NoError(a.t, json.NewDecoder(r.Body).Decode(&body))
			a.mu.Lock()
			a.resizes = append(a.resizes, body.DatabaseType)
			a.mu.Unlock()
			if a.refuse != 0 {
				respondDatabaseJSON(a.t, w, a.refuse, map[string]any{"error": a.message})
				return
			}
			payload := databaseCommandPayload("11111111-1111-4111-8111-111111111111", databaseProjectID, "app")
			payload["database_type"] = body.DatabaseType
			respondDatabaseJSON(a.t, w, http.StatusOK, payload)
		default:
			http.NotFound(w, r)
		}
	}))
	a.t.Cleanup(server.Close)
	return server
}

func (a *resizeAPI) sent() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.resizes...)
}

func runResizeCommand(t *testing.T, server *httptest.Server, answer string, args ...string) (string, string, error) {
	t.Helper()
	setDatabaseCommandTestHome(t)
	saveDatabaseCommandTestConfig(t, &cliconfig.Config{
		UserToken:      "token",
		CurrentProject: &cliconfig.ProjectConfig{ID: databaseProjectID, Name: "Beta"},
	})
	cmd := New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
	var stdout, stderr bytes.Buffer
	cmd.SetIn(strings.NewReader(answer))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(append([]string{"resize"}, args...))
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestResizeChangesTheSizeOnceConfirmed(t *testing.T) {
	api := &resizeAPI{t: t, current: "volcano-db-s"}

	out, _, err := runResizeCommand(t, api.serve(), "y\n", "app", "--type", "volcano-db-m")
	require.NoError(t, err)
	assert.Contains(t, out, "from volcano-db-s to volcano-db-m")
	assert.Contains(t, out, "Database 'app' resized to volcano-db-m")
	assert.Equal(t, []string{"volcano-db-m"}, api.sent())
}

func TestResizeDoesNothingWhenDeclined(t *testing.T) {
	api := &resizeAPI{t: t, current: "volcano-db-s"}

	out, _, err := runResizeCommand(t, api.serve(), "n\n", "app", "--type", "volcano-db-m")
	require.NoError(t, err)
	assert.Contains(t, out, "Cancelled.")
	assert.Empty(t, api.sent())
}

// --json keeps stdout parseable, so the prompt goes to stderr.
func TestResizePrintsTheDatabaseAsJSON(t *testing.T) {
	api := &resizeAPI{t: t, current: "volcano-db-s"}

	out, errOut, err := runResizeCommand(t, api.serve(), "yes\n", "app", "--type", "VOLCANO-DB-L", "--json")
	require.NoError(t, err)
	var database map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &database))
	assert.Equal(t, "volcano-db-l", database["database_type"])
	assert.NotContains(t, database, "connection_string", "the connection string carries the database password")
	assert.Contains(t, errOut, "Resize it?")

	out, errOut, err = runResizeCommand(t, api.serve(), "", "app", "--type", "volcano-db-xl", "--yes", "--json")
	require.NoError(t, err)
	assert.Empty(t, errOut)
	require.NoError(t, json.Unmarshal([]byte(out), &database))
	assert.Equal(t, []string{"volcano-db-l", "volcano-db-xl"}, api.sent())
}

func TestResizeSkipsTheSizeItAlreadyHas(t *testing.T) {
	api := &resizeAPI{t: t, current: "volcano-db-m"}

	out, _, err := runResizeCommand(t, api.serve(), "", "app", "--type", "volcano-db-m")
	require.NoError(t, err)
	assert.Contains(t, out, "Database 'app' is already volcano-db-m")

	out, _, err = runResizeCommand(t, api.serve(), "", "app", "--type", "volcano-db-m", "--json")
	require.NoError(t, err)
	assert.NotContains(t, out, "postgres://")
	assert.Empty(t, api.sent())
}

func TestResizeReportsTheRefusal(t *testing.T) {
	api := &resizeAPI{t: t, current: "volcano-db-s", refuse: http.StatusConflict, message: "database is restoring"}

	_, _, err := runResizeCommand(t, api.serve(), "", "app", "--type", "volcano-db-m", "--yes")
	require.ErrorContains(t, err, `failed to resize database "app"`)
	require.ErrorContains(t, err, "database is restoring")
}

func TestResizeRefusesAnUnknownSize(t *testing.T) {
	api := &resizeAPI{t: t, current: "volcano-db-s"}

	_, _, err := runResizeCommand(t, api.serve(), "", "app", "--type", "volcano-db-xxl", "--yes")
	require.ErrorContains(t, err, `--type must be one of: volcano-db-xs, volcano-db-s, volcano-db-m, volcano-db-l, volcano-db-xl, volcano-db-2xl, not "volcano-db-xxl"`)

	_, _, err = runResizeCommand(t, api.serve(), "", "app", "--yes")
	require.ErrorContains(t, err, `required flag(s) "type" not set`)
	assert.Empty(t, api.sent())
}
