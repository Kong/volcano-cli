package lavaflow

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	lavaflowProjectID = "33333333-3333-4333-8333-333333333333"
	lavaflowRunID     = "44444444-4444-4444-8444-444444444444"
	lavaflowIssueID   = "55555555-5555-4555-8555-555555555555"
	lavaflowRepo      = "acme/shop"
)

// lavaflowServer is a fake API that keeps the project's settings, so a
// command sequence reads back what the previous command saved. A non-zero
// refuse answers every settings and run request with that status and body, or
// only those with refuseMethod when it is set.
type lavaflowServer struct {
	t             *testing.T
	mu            sync.Mutex
	settings      map[string]any
	updates       []map[string]any
	runs          []map[string]any
	nextCursor    string
	listQueries   []url.Values
	started       []string
	repoConnected bool
	repoFailure   int
	// onRepo runs when the repository is read, and the request then waits
	// for the client to go away, so a test can cancel mid-request.
	onRepo       func()
	refuse       int
	refuseMethod string
	refusal      map[string]any
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

	fake := &lavaflowServer{t: t, settings: defaultLavaflowSettings(), repoConnected: true}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return fake, cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL, CommandPathPrefix: "volcano cloud"}
}

// defaultLavaflowSettings is what the API returns for a project that never
// saved settings: off, and set to run on its own up to a pull request once on.
func defaultLavaflowSettings() map[string]any {
	return map[string]any{
		"enabled":              false,
		"max_stage":            "pull_request",
		"auto_trigger":         true,
		"plan_allows_lavaflow": true,
	}
}

func (s *lavaflowServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer token" {
		s.t.Errorf("request without the account token: %q", r.Header.Get("Authorization"))
	}
	project := "/projects/" + lavaflowProjectID
	if r.URL.Path == project+"/git-connection" {
		if s.onRepo != nil {
			s.onRepo()
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
				s.t.Error("the client kept waiting after it was canceled")
			}
			return
		}
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
		return
	}
	if s.refuse != 0 && (s.refuseMethod == "" || s.refuseMethod == r.Method) {
		s.write(w, s.refuse, s.refusal)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == project+"/lavaflow":
		s.write(w, http.StatusOK, s.settings)
	case r.Method == http.MethodPatch && r.URL.Path == project+"/lavaflow":
		var update map[string]any
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			s.t.Errorf("decode update: %v", err)
		}
		s.updates = append(s.updates, update)
		if len(update) == 0 {
			s.write(w, http.StatusBadRequest, map[string]any{"error": "request body must change at least one field"})
			return
		}
		// An omitted field keeps its saved value.
		maps.Copy(s.settings, update)
		s.write(w, http.StatusOK, s.settings)
	case r.Method == http.MethodGet && r.URL.Path == project+"/lavaflow-runs":
		s.listQueries = append(s.listQueries, r.URL.Query())
		page := map[string]any{"data": s.runs, "limit": 20, "has_more": s.nextCursor != ""}
		if s.nextCursor != "" {
			page["next_cursor"] = s.nextCursor
		}
		s.write(w, http.StatusOK, page)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, project+"/lavaflow-runs/"):
		id := strings.TrimPrefix(r.URL.Path, project+"/lavaflow-runs/")
		for _, run := range s.runs {
			if run["id"] == id {
				s.write(w, http.StatusOK, run)
				return
			}
		}
		s.write(w, http.StatusNotFound, map[string]any{"error": "run not found"})
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, project+"/issues/") &&
		strings.HasSuffix(r.URL.Path, "/lavaflow-runs"):
		issue := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, project+"/issues/"), "/lavaflow-runs")
		s.started = append(s.started, issue)
		run := queuedRun()
		run["trigger"] = issueTrigger("manual", issue, "TypeError: cart is undefined")
		s.write(w, http.StatusAccepted, run)
	default:
		http.NotFound(w, r)
	}
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

func (s *lavaflowServer) set(change func(*lavaflowServer)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	change(s)
}

func queuedRun() map[string]any {
	return map[string]any{
		"id": lavaflowRunID, "project_id": lavaflowProjectID,
		"trigger": issueTrigger("manual", lavaflowIssueID, "TypeError: cart is undefined"),
		"status":  "queued", "max_stage": "pull_request", "created_at": "2026-10-23T09:00:00Z",
	}
}

func issueTrigger(reason, issueID, title string) map[string]any {
	return map[string]any{"type": "issue", "reason": reason, "issue": map[string]any{"id": issueID, "title": title}}
}

// fixedRun is a run that diagnosed its issue and opened a pull request.
func fixedRun() map[string]any {
	run := queuedRun()
	maps.Copy(run, map[string]any{
		"trigger": issueTrigger("new_issue", lavaflowIssueID, "TypeError: cart is undefined"),
		"status":  "succeeded", "stage_reached": "pull_request",
		"started_at": "2026-10-23T09:00:05Z", "completed_at": "2026-10-23T09:03:00Z",
		"commit_sha": "0123abcd",
		"diagnosis": map[string]any{
			"summary":       "Checkout reads the cart before it loads.",
			"root_cause":    "handleCheckout dereferences cart without a guard.",
			"suggested_fix": "Return 409 until the cart has loaded.",
			"confidence":    "high",
			"fixable":       true,
			"files":         []any{"functions/checkout.ts"},
			"evidence": []any{map[string]any{
				"kind": "log", "detail": "TypeError: cart is undefined", "location": "2026-10-23T08:59:00Z",
			}},
		},
		"pull_request": map[string]any{
			"number": 42, "url": "https://github.com/acme/shop/pull/42",
			"branch": "volcano/lavaflow/" + lavaflowRunID, "state": "open",
		},
	})
	return run
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

// assertLavaFlowWording checks what a user sees names LavaFlow only, never the
// machinery behind it.
func assertLavaFlowWording(t *testing.T, out string) {
	t.Helper()
	lower := strings.ToLower(out)
	for _, word := range []string{"autofix", "model", "anthropic", "claude", "api key"} {
		assert.NotContains(t, lower, word)
	}
}
