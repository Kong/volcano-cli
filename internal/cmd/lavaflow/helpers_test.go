package lavaflow

import (
	"bytes"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	lavaflowProjectID  = "33333333-3333-4333-8333-333333333333"
	lavaflowRunID      = "44444444-4444-4444-8444-444444444444"
	lavaflowFailedID   = "55555555-5555-4555-8555-555555555555"
	lavaflowResourceID = "66666666-6666-4666-8666-666666666666"
	lavaflowRepo       = "acme/shop"
	// lavaflowIssueTitle is a real issue title: Volcano groups errors by a
	// template of their message, with values replaced by placeholders.
	lavaflowIssueTitle = "TypeError: Cannot read properties of undefined (reading <str>)"
	// lavaflowFailureMessage is the API's fixed text for a failure code.
	lavaflowFailureMessage = "LavaFlow couldn't read this function's deployed source."
)

// lavaflowServer is a fake API that keeps the project's settings, so a
// command sequence reads back what the previous command saved. A non-zero
// refuse answers every LavaFlow request with that status and body, or only
// those with refuseMethod when it is set.
type lavaflowServer struct {
	t  *testing.T
	mu sync.Mutex
	// capability is the status of LavaFlow's capabilities entry; empty
	// leaves the entry out.
	capability    string
	settings      map[string]any
	updates       []map[string]any
	stats         map[string]any
	resources     []map[string]any
	runs          []map[string]any
	nextCursor    string
	queries       map[string][]url.Values
	repoConnected bool
	repoFailure   int
	refuse        int
	refuseMethod  string
	refusal       map[string]any
	// routesMissing answers every LavaFlow route, capabilities included,
	// with the router's bare 404, as a platform release older than LavaFlow
	// does.
	routesMissing bool
}

func newLavaflowServer(t *testing.T) (*lavaflowServer, cliruntime.Deps) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	require.NoError(t, (&cliconfig.Config{
		UserToken:      "token",
		CurrentProject: &cliconfig.ProjectConfig{ID: lavaflowProjectID, Name: "Shop"},
	}).Save())

	fake := &lavaflowServer{
		t: t, capability: "available", settings: defaultLavaflowSettings(), stats: lavaflowStats(),
		resources: lavaflowResources(), runs: []map[string]any{fixedRun(), failedRun()},
		queries: map[string][]url.Values{}, repoConnected: true,
	}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return fake, cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL, CommandPathPrefix: "volcano cloud"}
}

// defaultLavaflowSettings is what the API returns for a project that never
// saved settings.
func defaultLavaflowSettings() map[string]any {
	return map[string]any{
		"enabled": false, "autonomy": "open_pr", "custom_context": "", "system_prompt": "",
		"updated_at": "2026-10-20T10:00:00Z", "can_update": true,
	}
}

func (s *lavaflowServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		s.t.Errorf("request without a token: %q", r.Header.Get("Authorization"))
	}
	project := "/projects/" + lavaflowProjectID
	if r.URL.Path == project+"/git-connection" {
		s.gitConnection(w)
		return
	}
	if s.routesMissing {
		http.NotFound(w, r)
		return
	}
	if r.URL.Path == project+"/capabilities" {
		items := []any{map[string]any{"id": "sandboxes", "status": "available"}}
		if s.capability != "" {
			items = append(items, map[string]any{"id": "lavaflow", "status": s.capability})
		}
		s.write(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	if s.refuse != 0 && (s.refuseMethod == "" || s.refuseMethod == r.Method) {
		s.write(w, s.refuse, s.refusal)
		return
	}
	route := strings.TrimPrefix(r.URL.Path, project+"/lavaflow")
	s.queries[route] = append(s.queries[route], r.URL.Query())
	switch {
	case r.Method == http.MethodGet && route == "":
		settings := maps.Clone(s.settings)
		// The API says a project access token can't change the settings.
		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer "+cliconfig.ProjectTokenPrefix) {
			settings["can_update"] = false
		}
		s.write(w, http.StatusOK, settings)
	case r.Method == http.MethodPut && route == "":
		var update map[string]any
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			s.t.Errorf("decode update: %v", err)
		}
		s.updates = append(s.updates, update)
		// An omitted field keeps its saved value.
		maps.Copy(s.settings, update)
		s.settings["updated_at"] = "2026-10-30T12:00:00Z"
		s.write(w, http.StatusOK, s.settings)
	case r.Method == http.MethodGet && route == "/stats":
		s.write(w, http.StatusOK, s.stats)
	case r.Method == http.MethodGet && route == "/resources":
		s.write(w, http.StatusOK, map[string]any{"items": s.resources})
	case r.Method == http.MethodGet && route == "/runs":
		page := map[string]any{"items": summaries(s.runs)}
		if s.nextCursor != "" {
			page["next_cursor"] = s.nextCursor
		}
		s.write(w, http.StatusOK, page)
	case r.Method == http.MethodGet && strings.HasPrefix(route, "/runs/"):
		id := strings.TrimPrefix(route, "/runs/")
		for _, run := range s.runs {
			if run["id"] == id {
				s.write(w, http.StatusOK, run)
				return
			}
		}
		s.write(w, http.StatusNotFound, map[string]any{"error": "run not found"})
	default:
		http.NotFound(w, r)
	}
}

func (s *lavaflowServer) gitConnection(w http.ResponseWriter) {
	if s.repoFailure != 0 {
		s.write(w, s.repoFailure, map[string]any{"error": "git connection unavailable"})
		return
	}
	if !s.repoConnected {
		s.write(w, http.StatusNotFound, map[string]any{"error": "no repository connected"})
		return
	}
	s.write(w, http.StatusOK, map[string]any{
		"repo_full_name": lavaflowRepo, "repo_id": 1, "repo_installation_id": 2,
		"production_branch": "main", "root_directory": "", "updated_at": "2026-10-20T10:00:00Z",
	})
}

// write answers from the server's goroutine, where require is not valid.
func (s *lavaflowServer) write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		s.t.Errorf("encode response: %v", err)
	}
}

func (s *lavaflowServer) lastUpdate(t *testing.T) map[string]any {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	require.NotEmpty(t, s.updates, "no update reached the API")
	return s.updates[len(s.updates)-1]
}

func (s *lavaflowServer) lastQuery(t *testing.T, route string) url.Values {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	require.NotEmpty(t, s.queries[route], "no request reached %q", route)
	return s.queries[route][len(s.queries[route])-1]
}

func (s *lavaflowServer) set(change func(*lavaflowServer)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	change(s)
}

func lavaflowStats() map[string]any {
	return map[string]any{
		"window_start": "2026-10-28", "window_end": "2026-10-30",
		"days": []any{
			map[string]any{"date": "2026-10-28", "issues_opened": 2, "resolutions_opened": 1},
			map[string]any{"date": "2026-10-29", "issues_opened": 0, "resolutions_opened": 0},
			map[string]any{"date": "2026-10-30", "issues_opened": 5, "resolutions_opened": 3},
		},
		"totals": map[string]any{"issues_opened": 7, "resolutions_opened": 4},
	}
}

func lavaflowResources() []map[string]any {
	return []map[string]any{
		{
			"id": lavaflowResourceID, "type": "function", "name": "checkout",
			"last_analyzed_at": "2026-10-30T09:00:00Z", "issues_opened": 6, "resolutions_submitted": 3,
			"anomaly": map[string]any{"flagged": true, "reason": "6 issues in the last 24 hours, 4 times the daily average"},
		},
		{
			"id": "77777777-7777-4777-8777-777777777777", "type": "durable_function", "name": "nightly-report",
			"issues_opened": 0, "resolutions_submitted": 0, "anomaly": map[string]any{"flagged": false},
		},
	}
}

func checkoutResource() map[string]any {
	return map[string]any{"id": lavaflowResourceID, "type": "function", "name": "checkout"}
}

// fixedRun is a run that diagnosed its issue and opened a pull request that
// passed review.
func fixedRun() map[string]any {
	return map[string]any{
		"id": lavaflowRunID, "status": "pr_open", "title": lavaflowIssueTitle, "issue_id": "issue-1",
		"resource":   checkoutResource(),
		"created_at": "2026-10-30T09:00:00Z", "finished_at": "2026-10-30T09:03:00Z",
		"pull_request": map[string]any{
			"number": 42, "url": "https://github.com/acme/shop/pull/42", "state": "open", "opened_at": "2026-10-30T09:03:00Z",
		},
		"diagnosis": map[string]any{
			"summary":    "Checkout reads the cart before it loads.",
			"root_cause": "handleCheckout dereferences cart without a guard.",
			"evidence":   []any{"The error starts with the 10:02 deploy."},
			"log_lines": []any{map[string]any{
				"timestamp": "2026-10-30T08:59:00Z", "level": "error",
				"message": "TypeError: Cannot read properties of undefined (reading 'items')",
			}},
		},
		"review": map[string]any{"verdict": "passed", "rounds": 2},
		"diff":   "--- a/functions/checkout.ts\n+++ b/functions/checkout.ts\n+  if (!cart) return conflict()\n",
	}
}

// failedRun is a run that stopped, with the API's fixed text for its code.
func failedRun() map[string]any {
	return map[string]any{
		"id": lavaflowFailedID, "status": "failed", "resource": checkoutResource(),
		"created_at": "2026-10-29T09:00:00Z", "finished_at": "2026-10-29T09:01:00Z",
		"failure": map[string]any{"code": "source_unavailable", "message": lavaflowFailureMessage},
	}
}

// summaries is what the list returns for runs: no diagnosis, review, diff or
// failure.
func summaries(runs []map[string]any) []any {
	items := make([]any, 0, len(runs))
	for _, run := range runs {
		summary := maps.Clone(run)
		for _, detail := range []string{"diagnosis", "review", "diff", "failure"} {
			delete(summary, detail)
		}
		items = append(items, summary)
	}
	return items
}

// executeLavaflowCommandSplit runs a command with stdin and returns its
// stdout and stderr apart.
func executeLavaflowCommandSplit(t *testing.T, cmd *cobra.Command, stdin io.Reader, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd.SetIn(stdin)
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

func executeLavaflowCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// decodeJSON decodes a command's --json output.
func decodeJSON(t *testing.T, out string) map[string]any {
	t.Helper()
	var value map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &value), out)
	return value
}

// asJSON is value as a command's --json output decodes it.
func asJSON(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return decodeJSON(t, string(data))
}

// assertLavaFlowWording checks what a user sees names LavaFlow only, never the
// machinery behind it, and never sends them to the website.
func assertLavaFlowWording(t *testing.T, out string) {
	t.Helper()
	lower := strings.ToLower(out)
	for _, word := range []string{"autofix", "model", "anthropic", "claude", "api key", "dashboard", "website"} {
		assert.NotContains(t, lower, word)
	}
}
