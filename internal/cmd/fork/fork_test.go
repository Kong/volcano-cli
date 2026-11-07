package fork

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	linkID    = "k3Vn8TqPz0aX"
	link      = "https://volcano.dev/deploy/" + linkID
	releaseID = "0b6c1f1e-4a55-4d6f-9c3e-5d2f7a1b9e10"
	copyID    = "33333333-3333-4333-8333-333333333333"
)

type fakeAPI struct {
	creates []map[string]any
	gets    int
}

func newServer(t *testing.T) (*fakeAPI, *httptest.Server) {
	t.Helper()
	fake := &fakeAPI{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var status int
		var value any
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/share-previews/"+linkID:
			status, value = http.StatusOK, preview()
		case r.Method == http.MethodPost && r.URL.Path == "/forks":
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			fake.creates = append(fake.creates, body)
			status, value = http.StatusCreated, project(body["name"].(string), "pending", "database")
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+copyID:
			fake.gets++
			status, value = http.StatusOK, project("notes-app", "ready", "ready")
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			status, value = http.StatusNotFound, map[string]any{"error": "not found"}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		require.NoError(t, json.NewEncoder(w).Encode(value))
	}))
	t.Cleanup(server.Close)
	return fake, server
}

func deps(server *httptest.Server) cliruntime.Deps {
	return cliruntime.Deps{
		HTTPClient: server.Client(),
		APIBaseURL: server.URL,
		NewTicker:  func(time.Duration) cliruntime.Ticker { return immediateTicker{} },
	}
}

type immediateTicker struct{}

func (immediateTicker) C() <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- time.Now()
	return ch
}
func (immediateTicker) Reset(time.Duration) {}
func (immediateTicker) Stop()               {}

func setHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	require.NoError(t, (&cliconfig.Config{UserToken: "token"}).Save())
}

func run(t *testing.T, server *httptest.Server, args ...string) (string, error) {
	t.Helper()
	cmd := New(deps(server))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// fork needs no values: it creates the copy under the link's suggested name,
// waits for it, lists the variables that start empty, and selects it.
func TestForkCreatesTheCopyWithNoValuesAndSelectsIt(t *testing.T) {
	setHome(t)
	fake, server := newServer(t)

	out, err := run(t, server, link, "--yes")
	require.NoError(t, err)

	require.Len(t, fake.creates, 1)
	assert.Equal(t, "notes-app", fake.creates[0]["name"], "the copy takes the name the link suggests")
	assert.Equal(t, map[string]any{"name": "notes-app", "share_id": linkID, "release_id": releaseID}, fake.creates[0],
		"no variable values are sent")
	for _, want := range []string{
		"Project ready: notes-app", "These variables start empty", "OPENAI_API_KEY",
		"Now using project: notes-app (" + copyID + ")",
		"Set them in an env file and run `volcano cloud variables deploy --file <env-file>`",
	} {
		assert.Contains(t, out, want)
	}
	assert.Less(t, strings.Index(out, "Now using project"), strings.Index(out, "These variables start empty"),
		"the hint follows the selection it relies on")
	cfg, err := cliconfig.Load()
	require.NoError(t, err)
	require.NotNil(t, cfg.CurrentProject)
	assert.Equal(t, copyID, cfg.CurrentProject.ID, "the copy is the active project")
}

func TestForkTakesANameAndVars(t *testing.T) {
	setHome(t)
	fake, server := newServer(t)

	_, err := run(t, server, linkID, "my-copy", "--var", "OPENAI_API_KEY=sk-test", "--yes")
	require.NoError(t, err)
	require.Len(t, fake.creates, 1)
	assert.Equal(t, "my-copy", fake.creates[0]["name"])
	assert.Equal(t, map[string]any{"OPENAI_API_KEY": "sk-test"}, fake.creates[0]["variables"])
}

// Inside a linked directory, selecting the copy would relink the directory, so
// the copy is not selected there.
func TestForkInALinkedDirectoryKeepsTheLink(t *testing.T) {
	setHome(t)
	_, server := newServer(t)
	dir := t.TempDir()
	t.Chdir(dir)
	_, err := cliconfig.WriteLink(dir, cliconfig.ProjectConfig{ID: "22222222-2222-4222-8222-222222222222", Name: "shop"})
	require.NoError(t, err)

	out, err := run(t, server, link, "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "This directory stays linked to shop.")
	assert.NotContains(t, out, "Now using project")
	assert.Contains(t, out, "select the copy with `volcano use "+copyID+"` outside a linked directory, "+
		"then set them in an env file", "the variables hint must not deploy to the linked project")
}

// VOLCANO_PROJECT_ID outranks the selected copy, so the variables hint selects
// the copy first rather than deploying to the project the variable names.
func TestForkUnderAProjectOverrideDoesNotSendVariablesToIt(t *testing.T) {
	setHome(t)
	_, server := newServer(t)
	t.Setenv("VOLCANO_PROJECT_ID", "44444444-4444-4444-8444-444444444444")

	out, err := run(t, server, link, "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "VOLCANO_PROJECT_ID selects project 44444444-4444-4444-8444-444444444444, "+
		"so `volcano cloud variables deploy --file <env-file>` would write to it: set VOLCANO_PROJECT_ID to "+copyID)
	assert.NotContains(t, out, "Set them in an env file and run `volcano cloud variables deploy")
	assert.NotContains(t, out, "select the copy with `volcano use")
}

func TestForkJSONPrintsTheProject(t *testing.T) {
	setHome(t)
	_, server := newServer(t)
	cmd := New(deps(server))
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{link, "--yes", "--json"})
	require.NoError(t, cmd.Execute())
	var got map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &got), stdout.String())
	assert.Equal(t, copyID, got["id"])
	assert.Contains(t, stderr.String(), "Project ready")
}

func TestForkWithoutYesFailsWhereNothingCanAnswer(t *testing.T) {
	setHome(t)
	t.Setenv("VOLCANO_NONINTERACTIVE", "1")
	fake, server := newServer(t)
	_, err := run(t, server, link)
	require.ErrorContains(t, err, "pass --yes")
	assert.Empty(t, fake.creates)
}

func preview() map[string]any {
	return map[string]any{
		"id": linkID, "suggested_name": "notes-app", "description": "Notes that sync",
		"release": map[string]any{
			"id": releaseID, "published_at": "2026-10-20T09:00:00Z", "sha256": strings.Repeat("ab", 32),
			"frontends": []string{"web"}, "functions": []string{"api"}, "service_key_functions": []string{},
			"variables": []any{map[string]any{"name": "OPENAI_API_KEY", "required": true, "secret": true}},
			"omissions": []any{},
		},
	}
}

func project(name, status, phase string) map[string]any {
	return map[string]any{
		"id": copyID, "name": name, "status": "active", "all_regions": true, "selected_regions": []string{},
		"created_at": "2026-10-20T09:00:00Z", "updated_at": "2026-10-20T09:00:00Z",
		"template_installation": map[string]any{
			"status": status, "phase": phase,
			"source": map[string]any{"kind": "share_release", "share_id": linkID, "release_id": releaseID},
		},
	}
}
