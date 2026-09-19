package durable

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const durableDeploymentID = "77777777-7777-4777-8777-777777777777"

// Build logs are how a failed durable deploy says why it failed, and the
// standard functions collection cannot reach a durable function at all, so this
// path resolves through the durable collection and asks for the deployment the
// function is on.
func TestDurableLogsBuildReadsTheCurrentDeployment(t *testing.T) {
	setDurableCommandTestHome(t)
	var searchBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+durableProjectID+"/functions":
			t.Error("durable logs must not resolve through the standard functions collection")
			http.NotFound(w, r)
		case r.Method == http.MethodGet &&
			r.URL.Path == "/projects/"+durableProjectID+"/durable-functions/order-pipeline":
			function := durableFunctionPayload("order-pipeline", false)
			function["status"] = "failed"
			function["current_deployment_id"] = durableDeploymentID
			writeDurableCommandJSON(t, w, http.StatusOK, function)
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+durableProjectID+"/logs/search":
			require.NoError(t, decodeJSONBody(r, &searchBody))
			writeDurableCommandJSON(t, w, http.StatusOK, durableLogPage("npm ERR! missing script"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeDurableCommand(t, newCloudDurableCommand(server),
		"logs", "order-pipeline", "--type", "build")
	require.NoError(t, err)
	resource, ok := searchBody["resource"].(map[string]any)
	require.True(t, ok)
	deployments, ok := resource["deployments"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "function", resource["type"])
	assert.Equal(t, []any{durableFunctionID}, resource["ids"])
	assert.Equal(t, []any{durableDeploymentID}, deployments["ids"])
	assert.Contains(t, out, "Fetching build logs for durable function order-pipeline deployment "+durableDeploymentID)
	assert.Contains(t, out, "npm ERR! missing script")
}

func TestDurableLogsRuntimePagesEveryExecution(t *testing.T) {
	setDurableCommandTestHome(t)
	var searchBodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet &&
			r.URL.Path == "/projects/"+durableProjectID+"/durable-functions/order-pipeline":
			writeDurableCommandJSON(t, w, http.StatusOK, durableFunctionPayload("order-pipeline", false))
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+durableProjectID+"/logs/search":
			var body map[string]any
			require.NoError(t, decodeJSONBody(r, &body))
			searchBodies = append(searchBodies, body)
			if body["cursor"] == nil {
				writeDurableCommandJSON(t, w, http.StatusOK, durableLogPageWithCursor("charging order", "next token"))
				return
			}
			writeDurableCommandJSON(t, w, http.StatusOK, durableLogPage("order shipped"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeDurableCommand(t, newCloudDurableCommand(server),
		"logs", "order-pipeline", "--type", "runtime", "--limit", "2")
	require.NoError(t, err)
	require.Len(t, searchBodies, 2)
	resource, ok := searchBodies[0]["resource"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{durableFunctionID}, resource["ids"])
	assert.NotContains(t, resource, "deployments")
	assert.InEpsilon(t, 2, searchBodies[0]["limit"], 0)
	assert.Equal(t, "next token", searchBodies[1]["cursor"])
	assert.Contains(t, out, "Fetching runtime logs for durable function order-pipeline")
	assert.Contains(t, out, "charging order")
	assert.Contains(t, out, "order shipped")
}

// A durable function whose first deploy never got admitted has no deployment to
// read, and an empty log page would read as a build that logged nothing.
func TestDurableLogsBuildWithoutADeploymentSaysSo(t *testing.T) {
	setDurableCommandTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/projects/"+durableProjectID+"/logs/search" {
			t.Error("build logs without a deployment must not search")
		}
		writeDurableCommandJSON(t, w, http.StatusOK, durableFunctionPayload("order-pipeline", false))
	}))
	defer server.Close()

	out, err := executeDurableCommand(t, newCloudDurableCommand(server),
		"logs", "order-pipeline", "--type", "build")
	require.NoError(t, err)
	assert.Contains(t, out, "No deployments found for this durable function")
}

// The durable collection has no deployment read, so the function's own status is
// what tells the follow loop the deploy is over. It must keep polling while the
// function is still provisioning, even after the log stream has closed.
func TestDurableLogsFollowWaitsForTheFunctionToLeaveProvisioning(t *testing.T) {
	setDurableCommandTestHome(t)
	var reads atomic.Int32
	var caughtUp atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet &&
			r.URL.Path == "/projects/"+durableProjectID+"/durable-functions/"+durableFunctionID:
			function := durableFunctionPayload("order-pipeline", false)
			function["current_deployment_id"] = durableDeploymentID
			if reads.Add(1) == 1 {
				function["status"] = "provisioning"
			} else {
				function["status"] = "active"
			}
			writeDurableCommandJSON(t, w, http.StatusOK, function)
		case r.Method == http.MethodGet &&
			r.URL.Path == "/projects/"+durableProjectID+"/durable-functions/order-pipeline":
			function := durableFunctionPayload("order-pipeline", false)
			function["status"] = "provisioning"
			function["current_deployment_id"] = durableDeploymentID
			writeDurableCommandJSON(t, w, http.StatusOK, function)
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+durableProjectID+"/logs/stream":
			writeDurableLogStream(t, w, "installing dependencies")
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+durableProjectID+"/logs/search":
			caughtUp.Store(true)
			writeDurableCommandJSON(t, w, http.StatusOK, durableCatchUpLogPage())
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ticker := newDurableFakeTicker()
	out, errCh := streamDurableCommand(context.Background(), newCloudDurableFollowCommand(server, ticker),
		"logs", "order-pipeline", "--type", "build", "--follow")

	// The stream closing is not the deploy ending: that first read still says
	// provisioning, and the tick after it is what finds the deploy finished.
	require.Eventually(t, func() bool { return reads.Load() >= 1 }, 2*time.Second, 5*time.Millisecond)
	assert.False(t, caughtUp.Load(), "a provisioning function is not a finished deploy")
	ticker.tick()
	require.NoError(t, <-errCh)

	text := out.String()
	assert.Contains(t, text, "Following build logs for durable function order-pipeline deployment "+durableDeploymentID)
	assert.Contains(t, text, "installing dependencies")
	// The catch-up search backfills what the stream missed without reprinting
	// the line it already showed.
	assert.Contains(t, text, "build succeeded")
	assert.Equal(t, 1, strings.Count(text, "installing dependencies"))
}

// Runtime logs are what the function itself wrote, across every execution, so
// following them asks for the function and nothing narrower.
func TestDurableLogsRuntimeFollowStreamsTheFunction(t *testing.T) {
	setDurableCommandTestHome(t)
	var streamBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet &&
			r.URL.Path == "/projects/"+durableProjectID+"/durable-functions/order-pipeline":
			writeDurableCommandJSON(t, w, http.StatusOK, durableFunctionPayload("order-pipeline", false))
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+durableProjectID+"/logs/stream":
			require.NoError(t, decodeJSONBody(r, &streamBody))
			writeDurableLogStream(t, w, "step charge-card completed")
			// A healthy backend holds the connection open and tails new events.
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, errCh := streamDurableCommand(ctx, newCloudDurableCommand(server),
		"logs", "order-pipeline", "--type", "runtime", "--follow")

	require.Eventually(t, func() bool {
		return strings.Contains(out.String(), "step charge-card completed")
	}, 2*time.Second, 10*time.Millisecond)
	cancel()
	require.NoError(t, <-errCh)

	resource, ok := streamBody["resource"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "function", resource["type"])
	assert.Equal(t, []any{durableFunctionID}, resource["ids"])
	assert.NotContains(t, resource, "deployments")
	assert.Contains(t, out.String(), "Following runtime logs for durable function order-pipeline")
}

func TestDurableLogsValidatesType(t *testing.T) {
	_, err := executeDurableCommand(t, newCloudDurableCommand(nil), "logs", "order-pipeline", "--type", "deploy")
	require.ErrorContains(t, err, "--type must be one of: build, runtime")
}

func TestDurableLogsNamesTheCollectionOnNotFound(t *testing.T) {
	setDurableCommandTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeDurableCommandJSON(t, w, http.StatusNotFound, map[string]any{"error": "durable function not found"})
	}))
	defer server.Close()

	_, err := executeDurableCommand(t, newCloudDurableCommand(server), "logs", "hello", "--type", "runtime")
	require.ErrorContains(t, err, `durable function "hello" not found`)
}

// streamDurableCommand runs cmd on its own goroutine with a cancelable context,
// for the --follow paths that run until something ends them.
func streamDurableCommand(ctx context.Context, cmd *cobra.Command, args ...string) (*durableSyncBuffer, <-chan error) {
	out := &durableSyncBuffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(args)
	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.ExecuteContext(ctx)
	}()
	return out, errCh
}

type durableSyncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *durableSyncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *durableSyncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newCloudDurableFollowCommand(server *httptest.Server, ticker cliruntime.Ticker) *cobra.Command {
	return New(cliruntime.Deps{
		CommandPathPrefix: "volcano cloud",
		HTTPClient:        server.Client(),
		APIBaseURL:        server.URL,
		NewTicker:         func(time.Duration) cliruntime.Ticker { return ticker },
	})
}

type durableFakeTicker struct {
	ch chan time.Time
}

func newDurableFakeTicker() *durableFakeTicker {
	return &durableFakeTicker{ch: make(chan time.Time, 1)}
}

func (t *durableFakeTicker) C() <-chan time.Time { return t.ch }

func (t *durableFakeTicker) Reset(time.Duration) {}

func (t *durableFakeTicker) Stop() {}

func (t *durableFakeTicker) tick() { t.ch <- time.Now() }

func durableLogPage(body string) map[string]any {
	return map[string]any{
		"data": []any{
			map[string]any{
				"id":        "log-" + body,
				"body":      body,
				"region":    "aws-us-east-1",
				"timestamp": "2026-05-20T00:00:00Z",
			},
		},
		"has_more": false,
		"limit":    100,
		"page":     1,
		"total":    1,
	}
}

func durableLogPageWithCursor(body, cursor string) map[string]any {
	page := durableLogPage(body)
	page["has_more"] = true
	page["next_cursor"] = cursor
	return page
}

// durableCatchUpLogPage repeats the streamed line the follow loop already
// printed alongside one it missed.
func durableCatchUpLogPage() map[string]any {
	return map[string]any{
		"data": []any{
			map[string]any{
				"id":        "stream-log",
				"body":      "installing dependencies",
				"region":    "aws-us-east-1",
				"timestamp": "2026-05-20T00:00:00Z",
			},
			map[string]any{
				"id":        "catch-up-log",
				"body":      "build succeeded",
				"region":    "aws-us-east-1",
				"timestamp": "2026-05-20T00:00:01Z",
			},
		},
		"has_more": false,
		"limit":    100,
		"page":     1,
		"total":    2,
	}
}

func writeDurableLogStream(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = w.Write([]byte(": connected\n\n"))
	_, _ = w.Write([]byte("id: stream-cursor\n"))
	_, _ = w.Write([]byte("event: log\n"))
	_, _ = w.Write([]byte(`data: {"id":"stream-log","body":"` + body +
		`","timestamp":"2026-05-20T00:00:00Z","resource":{"type":"function","id":"` + durableFunctionID + `"}}` + "\n\n"))
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}
