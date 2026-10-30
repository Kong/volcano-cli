package logs

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	logsProjectID = "22222222-2222-4222-8222-222222222222"
	checkoutID    = "33333333-3333-4333-8333-333333333333"
	pipelineID    = "44444444-4444-4444-8444-444444444444"
	webID         = "55555555-5555-4555-8555-555555555555"
	ordersDBID    = "66666666-6666-4666-8666-666666666666"
	projectPath   = "/projects/" + logsProjectID
)

// logsAPI is a fake platform that records every log request body and serves
// pages in order.
type logsAPI struct {
	t     *testing.T
	pages []map[string]any
	// activity is what the activity route answers.
	activity map[string]any

	mu     sync.Mutex
	bodies []map[string]any
	paths  []string
}

func newLogsAPI(t *testing.T) *logsAPI {
	t.Helper()
	return &logsAPI{t: t, pages: []map[string]any{logPage(false, "", logEvent("log-1", "checkout", "error", "timeout"))}}
}

func (a *logsAPI) serve() *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(a.handle))
	a.t.Cleanup(server.Close)
	return server
}

func (a *logsAPI) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == projectPath+"/functions":
		a.write(w, http.StatusOK, map[string]any{
			"data":     []any{map[string]any{"id": checkoutID, "name": "checkout", "status": "active"}},
			"has_more": false, "page": 1, "limit": 20, "total": 1,
		})
	case r.Method == http.MethodGet && r.URL.Path == projectPath+"/durable-functions/pipeline":
		a.write(w, http.StatusOK, map[string]any{"id": pipelineID, "name": "pipeline", "status": "active"})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, projectPath+"/durable-functions/"):
		a.write(w, http.StatusNotFound, map[string]any{"error": "durable function not found"})
	case r.Method == http.MethodGet && r.URL.Path == projectPath+"/frontends":
		a.write(w, http.StatusOK, map[string]any{
			"data":     []any{map[string]any{"id": webID, "name": "web"}},
			"has_more": false, "page": 1, "limit": 20, "total": 1,
		})
	case r.Method == http.MethodGet && r.URL.Path == projectPath+"/databases/orders":
		a.write(w, http.StatusOK, map[string]any{"id": ordersDBID, "name": "orders"})
	case r.Method == http.MethodPost && r.URL.Path == projectPath+"/logs/search":
		a.record(r)
		a.mu.Lock()
		page := a.pages[0]
		if len(a.pages) > 1 {
			a.pages = a.pages[1:]
		}
		a.mu.Unlock()
		a.write(w, http.StatusOK, page)
	case r.Method == http.MethodPost && r.URL.Path == projectPath+"/logs/activity":
		a.record(r)
		a.write(w, http.StatusOK, a.activity)
	case r.Method == http.MethodPost && r.URL.Path == projectPath+"/logs/stream":
		a.record(r)
		w.Header().Set("Content-Type", "text/event-stream")
		event, _ := json.Marshal(logEvent("stream-1", "checkout", "warn", "slow"))
		_, _ = w.Write([]byte(": connected\n\nid: cursor-1\nevent: log\ndata: " + string(event) + "\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		// A healthy backend holds the connection open and tails new events.
		<-r.Context().Done()
	default:
		http.NotFound(w, r)
	}
}

func (a *logsAPI) record(r *http.Request) {
	var body map[string]any
	// The handler runs off the test's goroutine, so it reports rather than stops.
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		a.t.Errorf("decode %s body: %v", r.URL.Path, err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.bodies = append(a.bodies, body)
	a.paths = append(a.paths, r.URL.Path)
}

func (a *logsAPI) sent() []map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]map[string]any(nil), a.bodies...)
}

func (a *logsAPI) write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	assert.NoError(a.t, json.NewEncoder(w).Encode(value))
}

func logEvent(id, resource, level, body string) map[string]any {
	return map[string]any{
		"id":            id,
		"body":          body,
		"level":         level,
		"region":        "us-east-1",
		"invocation_id": "inv-1",
		"timestamp":     "2026-10-30T09:00:00Z",
		"resource":      map[string]any{"type": "function", "id": checkoutID, "name": resource},
	}
}

func logPage(hasMore bool, cursor string, events ...map[string]any) map[string]any {
	page := map[string]any{"data": events, "has_more": hasMore, "limit": 100}
	if cursor != "" {
		page["next_cursor"] = cursor
	}
	return page
}

func setLogsTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
	cfg := &cliconfig.Config{
		UserToken:      "token",
		CurrentProject: &cliconfig.ProjectConfig{ID: logsProjectID, Name: "Beta"},
	}
	require.NoError(t, cfg.Save())
}

func newLogsCommand(server *httptest.Server) *cobra.Command {
	return New(cliruntime.Deps{
		CommandPathPrefix: "volcano cloud",
		HTTPClient:        server.Client(),
		APIBaseURL:        server.URL,
	})
}

func executeLogs(t *testing.T, server *httptest.Server, args ...string) (string, string, error) {
	t.Helper()
	cmd := newLogsCommand(server)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestLogsSearchesEveryFunctionByDefault(t *testing.T) {
	setLogsTestHome(t)
	api := newLogsAPI(t)

	out, _, err := executeLogs(t, api.serve())
	require.NoError(t, err)

	require.Len(t, api.sent(), 1)
	assert.Equal(t, map[string]any{
		"resource": map[string]any{"type": "function"},
		"limit":    float64(100),
	}, api.sent()[0])
	assert.Contains(t, out, "[us-east-1] error checkout: timeout")
}

// Names resolve to IDs through each type's own collection, and the filters
// become one query whose free text cannot swallow a filter.
func TestLogsSendsTheSelectionAndFilters(t *testing.T) {
	setLogsTestHome(t)
	api := newLogsAPI(t)

	_, _, err := executeLogs(t, api.serve(),
		"--name", "checkout,pipeline", "--name", "checkout",
		"--query", `timeout OR "connection reset"`, "--level", "ERROR", "--region", "us-east-1",
		"--invocation", `inv"1`, "--start", "2026-10-30T08:00:00Z", "--end", "2026-10-30T09:00:00Z", "--limit", "5")
	require.NoError(t, err)

	require.Len(t, api.sent(), 1)
	assert.Equal(t, map[string]any{
		"resource":   map[string]any{"type": "function", "ids": []any{checkoutID, pipelineID}},
		"q":          `(timeout OR "connection reset") level:"error" region:"us-east-1" invocation.id:"inv\"1"`,
		"start_time": "2026-10-30T08:00:00Z",
		"end_time":   "2026-10-30T09:00:00Z",
		"limit":      float64(5),
	}, api.sent()[0])
}

func TestLogsResolvesEachResourceType(t *testing.T) {
	for _, tc := range []struct {
		args     []string
		resource map[string]any
	}{
		{[]string{"--kind", "durable", "--name", "pipeline"}, map[string]any{"type": "function", "kind": "durable", "ids": []any{pipelineID}}},
		{[]string{"--type", "frontend", "--name", "web"}, map[string]any{"type": "frontend", "ids": []any{webID}}},
		{[]string{"--type", "database", "--name", "orders"}, map[string]any{"type": "database", "ids": []any{ordersDBID}}},
	} {
		t.Run(tc.args[1], func(t *testing.T) {
			setLogsTestHome(t)
			api := newLogsAPI(t)

			_, _, err := executeLogs(t, api.serve(), tc.args...)
			require.NoError(t, err)
			require.Len(t, api.sent(), 1)
			assert.Equal(t, tc.resource, api.sent()[0]["resource"])
		})
	}
}

func TestLogsRefusesAnUnknownName(t *testing.T) {
	setLogsTestHome(t)
	api := newLogsAPI(t)

	_, _, err := executeLogs(t, api.serve(), "--kind", "standard", "--name", "pipeline")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `function "pipeline"`)
	assert.Empty(t, api.sent())
}

// --limit bounds what is printed, not one page: paging continues through
// has_more, even past a short or empty page, and the request shrinks to what
// is left.
func TestLogsPagesUntilTheLimit(t *testing.T) {
	setLogsTestHome(t)
	api := newLogsAPI(t)
	api.pages = []map[string]any{
		logPage(true, "c2", logEvent("a", "checkout", "info", "first")),
		logPage(true, "c3"),
		logPage(true, "c4", logEvent("b", "checkout", "info", "second"), logEvent("c", "checkout", "info", "third")),
	}

	out, errOut, err := executeLogs(t, api.serve(), "--limit", "2")
	require.NoError(t, err)

	sent := api.sent()
	require.Len(t, sent, 3)
	assert.InDelta(t, 2, sent[0]["limit"], 0)
	assert.Equal(t, "c2", sent[1]["cursor"])
	assert.InDelta(t, 1, sent[1]["limit"], 0)
	assert.Equal(t, "c3", sent[2]["cursor"])
	assert.Contains(t, out, "first")
	assert.Contains(t, out, "second")
	assert.NotContains(t, out, "third")
	assert.Contains(t, errOut, "More logs match")
}

func TestLogsSaysWhenNothingMatches(t *testing.T) {
	setLogsTestHome(t)
	api := newLogsAPI(t)
	api.pages = []map[string]any{logPage(false, "")}

	out, errOut, err := executeLogs(t, api.serve())
	require.NoError(t, err)
	assert.Empty(t, out)
	assert.Contains(t, errOut, "No logs match.")
}

// --json prints one object per log on stdout, with the fields a script
// filters on, and leaves notes to stderr.
func TestLogsPrintsOneJSONObjectPerLog(t *testing.T) {
	setLogsTestHome(t)
	api := newLogsAPI(t)
	api.pages = []map[string]any{logPage(true, "c2",
		logEvent("a", "checkout", "error", "first"), logEvent("b", "checkout", "warn", "second"))}

	out, errOut, err := executeLogs(t, api.serve(), "--json", "--limit", "2")
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 2)
	var event map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &event))
	assert.Equal(t, "error", event["level"])
	assert.Equal(t, "us-east-1", event["region"])
	assert.Equal(t, "inv-1", event["invocation_id"])
	assert.Equal(t, "checkout", event["resource"].(map[string]any)["name"])
	assert.Contains(t, errOut, "More logs match")
}

func TestLogsFollowTailsTheSelection(t *testing.T) {
	setLogsTestHome(t)
	api := newLogsAPI(t)
	server := api.serve()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := newLogsCommand(server)
	var out syncBuffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--follow", "--level", "warn", "--since", "15m"})
	errCh := make(chan error, 1)
	go func() { errCh <- cmd.ExecuteContext(ctx) }()

	require.Eventually(t, func() bool {
		return strings.Contains(out.String(), "warn checkout: slow")
	}, 2*time.Second, 10*time.Millisecond)
	cancel()
	require.NoError(t, <-errCh)

	sent := api.sent()
	require.NotEmpty(t, sent)
	assert.Equal(t, map[string]any{"type": "function"}, sent[0]["resource"])
	assert.Equal(t, `level:"warn"`, sent[0]["q"])
	assert.Contains(t, sent[0], "start_time")
	assert.NotContains(t, sent[0], "end_time")
}

func TestLogsActivityCountsTheSelection(t *testing.T) {
	setLogsTestHome(t)
	api := newLogsAPI(t)
	api.activity = map[string]any{
		"total": 7,
		"data": []any{map[string]any{
			"start_time": "2026-10-30T08:00:00Z", "end_time": "2026-10-30T09:00:00Z", "total": 7,
			"counts": map[string]any{
				"levels":       map[string]any{"error": 2, "info": 5},
				"regions":      map[string]any{"us-east-1": 7},
				"resource_ids": map[string]any{checkoutID: 7},
			},
		}},
	}

	out, _, err := executeLogs(t, api.serve(), "activity", "--name", "checkout", "--buckets", "12", "--query", "timeout")
	require.NoError(t, err)

	require.Len(t, api.sent(), 1)
	assert.Equal(t, map[string]any{
		"resource":     map[string]any{"type": "function", "ids": []any{checkoutID}},
		"q":            "(timeout)",
		"bucket_count": float64(12),
	}, api.sent()[0])
	assert.Contains(t, out, "error 2, info 5")
	assert.Contains(t, out, "7 logs")

	out, _, err = executeLogs(t, api.serve(), "activity", "--json")
	require.NoError(t, err)
	var activity map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &activity))
	assert.InDelta(t, 7, activity["total"], 0)
}

func TestLogsRefusesBadFlags(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--type", "worker"}, `--type must be one of: function, frontend, database, not "worker"`},
		{[]string{"--type", "frontend", "--kind", "durable"}, "--kind applies only to --type function"},
		{[]string{"--kind", "scheduled"}, `--kind must be standard or durable`},
		{[]string{"--level", "loud"}, "--level must be one of"},
		{[]string{"--since", "1h", "--start", "2026-10-30T08:00:00Z"}, "pass --since or --start, not both"},
		{[]string{"--since", "soon"}, "--since must be a positive duration"},
		{[]string{"--start", "yesterday"}, "--start must be an RFC 3339 time"},
		{[]string{"--start", "2026-10-30T09:00:00Z", "--end", "2026-10-30T08:00:00Z"}, "must be before its end"},
		{[]string{"--follow", "--end", "2026-10-30T08:00:00Z"}, "--end cannot be combined with --follow"},
		{[]string{"--limit", "0"}, "--limit must be at least 1"},
		{[]string{"activity", "--buckets", "97"}, "--buckets must be between 1 and 96"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			setLogsTestHome(t)
			api := newLogsAPI(t)

			_, _, err := executeLogs(t, api.serve(), tc.args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Empty(t, api.sent())
		})
	}
}

func TestParseSinceTakesDays(t *testing.T) {
	since, err := parseSince("7d")
	require.NoError(t, err)
	assert.Equal(t, 7*24*time.Hour, since)
	since, err = parseSince("90m")
	require.NoError(t, err)
	assert.Equal(t, 90*time.Minute, since)
}

// syncBuffer is safe for the follow loop's writes while the test reads.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
