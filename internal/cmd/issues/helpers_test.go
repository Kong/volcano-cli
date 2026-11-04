package issues

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	testProjectID  = "33333333-3333-4333-8333-333333333333"
	testIssueID    = "88888888-8888-4888-8888-888888888888"
	testOtherIssue = "99999999-9999-4999-8999-999999999999"
	testRunID      = "44444444-4444-4444-8444-444444444444"
	testNoteID     = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	testRunNoteID  = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	testResourceID = "66666666-6666-4666-8666-666666666666"
	testAuthorID   = "11111111-1111-4111-8111-111111111111"
	// testIssueTitle is a real issue title: Volcano groups errors by a
	// template of their message, with values replaced by placeholders.
	testIssueTitle = "TypeError: Cannot read properties of undefined (reading <str>)"
)

// request is one request the fake API answered.
type request struct {
	method string
	path   string
	query  url.Values
	body   map[string]any
}

// issuesServer is a fake API with a project's issues and notes. A non-zero
// refuse answers every request with that status, body and headers.
type issuesServer struct {
	t        *testing.T
	mu       sync.Mutex
	issues   []map[string]any
	notes    []map[string]any
	next     string
	requests []request

	refuse         int
	refusal        map[string]any
	refusalHeaders map[string]string
	// routesMissing answers with the router's bare 404, as a platform
	// release without the routes does.
	routesMissing bool
	// local is a local development server, which the CLI sends no
	// credential.
	local bool
}

func newIssuesServer(t *testing.T) (*issuesServer, cliruntime.Deps) {
	t.Helper()
	return newIssuesServerWithToken(t, "token")
}

func newIssuesServerWithToken(t *testing.T, token string) (*issuesServer, cliruntime.Deps) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	require.NoError(t, (&cliconfig.Config{
		UserToken:      token,
		CurrentProject: &cliconfig.ProjectConfig{ID: testProjectID, Name: "Shop"},
	}).Save())

	fake := &issuesServer{t: t, issues: []map[string]any{openIssue(), resolvedIssue()}, notes: testNotes()}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return fake, cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL, CommandPathPrefix: "volcano cloud"}
}

func (s *issuesServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.local && !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		s.t.Errorf("request without a token: %q", r.Header.Get("Authorization"))
	}
	var body map[string]any
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
			s.t.Errorf("decode body: %v", err)
		}
	}
	s.requests = append(s.requests, request{method: r.Method, path: r.URL.Path, query: r.URL.Query(), body: body})
	if s.routesMissing {
		http.NotFound(w, r)
		return
	}
	if s.refuse != 0 {
		for name, value := range s.refusalHeaders {
			w.Header().Set(name, value)
		}
		s.write(w, s.refuse, s.refusal)
		return
	}
	issuesPath := "/projects/" + testProjectID + "/issues"
	switch {
	case r.Method == http.MethodGet && r.URL.Path == issuesPath:
		s.write(w, http.StatusOK, s.page(s.issues))
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, issuesPath+"/"):
		s.writeItem(w, s.issues, strings.TrimPrefix(r.URL.Path, issuesPath+"/"), "issue not found")
	case r.Method == http.MethodGet && r.URL.Path == "/issue-notes":
		s.write(w, http.StatusOK, s.page(s.notes))
	case r.Method == http.MethodPost && r.URL.Path == "/issue-notes":
		note := map[string]any{
			"id": testNoteID, "project_id": body["project_id"], "issue_id": body["issue_id"], "run_id": body["run_id"],
			"author": map[string]any{"id": testAuthorID, "email": "owner@example.com"}, "body": body["body"], "version": 1,
			"created_at": "2026-11-04T19:48:00Z", "updated_at": "2026-11-04T19:48:00Z",
		}
		s.write(w, http.StatusCreated, note)
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/issue-notes/"):
		note := map[string]any{
			"id": strings.TrimPrefix(r.URL.Path, "/issue-notes/"), "project_id": testProjectID, "issue_id": testIssueID,
			"run_id": nil, "author": map[string]any{"id": testAuthorID, "email": "owner@example.com"}, "body": body["body"],
			"version": 2, "created_at": "2026-11-04T19:48:00Z", "updated_at": "2026-11-04T20:01:00Z",
		}
		s.write(w, http.StatusOK, note)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/issue-notes/"):
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (s *issuesServer) page(items []map[string]any) map[string]any {
	data := make([]any, 0, len(items))
	for _, item := range items {
		data = append(data, item)
	}
	page := map[string]any{"data": data, "limit": 25, "has_more": s.next != ""}
	if s.next != "" {
		page["next_cursor"] = s.next
	}
	return page
}

func (s *issuesServer) writeItem(w http.ResponseWriter, items []map[string]any, id, notFound string) {
	for _, item := range items {
		if item["id"] == id {
			s.write(w, http.StatusOK, item)
			return
		}
	}
	s.write(w, http.StatusNotFound, map[string]any{"error": notFound})
}

// write answers from the server's goroutine, where require is not valid.
func (s *issuesServer) write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		s.t.Errorf("encode response: %v", err)
	}
}

func (s *issuesServer) set(change func(*issuesServer)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	change(s)
}

func (s *issuesServer) sent() []request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]request(nil), s.requests...)
}

func (s *issuesServer) last(t *testing.T) request {
	t.Helper()
	sent := s.sent()
	require.NotEmpty(t, sent, "no request reached the API")
	return sent[len(sent)-1]
}

func openIssue() map[string]any {
	return map[string]any{
		"id": testIssueID, "project_id": testProjectID,
		"resource": map[string]any{"type": "function", "id": testResourceID, "name": "checkout"},
		"title":    testIssueTitle, "error_type": "TypeError", "culprit": "chargeCustomer (src/payments.js)",
		"status": "open", "exception": true, "event_count": 42,
		"first_seen_at": "2026-10-13T09:12:04Z", "last_seen_at": "2026-10-13T11:47:31Z", "last_region": "us-east-1",
		"created_at": "2026-10-13T09:12:05Z", "updated_at": "2026-10-13T11:47:33Z",
	}
}

func resolvedIssue() map[string]any {
	return map[string]any{
		"id": testOtherIssue, "project_id": testProjectID,
		"resource": map[string]any{"type": "frontend", "id": testResourceID, "name": "storefront"},
		"title":    "connection refused", "status": "resolved", "exception": false, "event_count": 3,
		"first_seen_at": "2026-10-10T08:00:00Z", "last_seen_at": "2026-10-11T08:00:00Z",
		"resolved_at": "2026-10-12T08:00:00Z", "regressed_at": "2026-10-11T07:00:00Z",
		"created_at": "2026-10-10T08:00:01Z", "updated_at": "2026-10-12T08:00:00Z",
	}
}

// testNotes are a note on the issue, edited once, and a note on one of its
// runs by an account since deleted.
func testNotes() []map[string]any {
	return []map[string]any{
		{
			"id": testNoteID, "project_id": testProjectID, "issue_id": testIssueID, "run_id": nil,
			"author": map[string]any{"id": testAuthorID, "email": "owner@example.com"},
			"body":   "Coupons without an expiry never expire.\nSAVE10 is one.", "version": 2,
			"created_at": "2026-11-04T19:48:00Z", "updated_at": "2026-11-04T20:01:00Z",
		},
		{
			"id": testRunNoteID, "project_id": testProjectID, "issue_id": testIssueID, "run_id": testRunID,
			"author": nil, "body": "Closed the pull request: the fix belongs in the cart.", "version": 1,
			"created_at": "2026-11-04T19:40:00Z", "updated_at": "2026-11-04T19:40:00Z",
		},
	}
}

// execute runs a command with stdin and returns its stdout and stderr apart.
func execute(t *testing.T, cmd *cobra.Command, stdin io.Reader, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	if stdin == nil {
		stdin = strings.NewReader("")
	}
	cmd.SetIn(stdin)
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

// decodeJSON decodes a command's --json output.
func decodeJSON(t *testing.T, out string) map[string]any {
	t.Helper()
	var value map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &value), out)
	return value
}
