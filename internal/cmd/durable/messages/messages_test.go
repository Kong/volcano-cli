package messages

import (
	"bytes"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	projectID   = "22222222-2222-4222-8222-222222222222"
	executionID = "66666666-6666-4666-8666-666666666666"
	messageID   = "88888888-8888-4888-8888-888888888888"
	waitID      = "99999999-9999-4999-8999-999999999999"
)

const (
	executionPath = "/projects/" + projectID + "/durable-functions/order-pipeline/executions/" + executionID
	messagesPath  = executionPath + "/messages"
	waitsPath     = executionPath + "/message-waits"
)

func TestSendPostsTheMessageAndReportsIt(t *testing.T) {
	setMessagesTestHome(t)
	var body []byte
	var header http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, messagesPath, r.URL.Path)
		header = r.Header.Clone()
		var err error
		body, err = io.ReadAll(r.Body)
		assert.NoError(t, err)
		respondJSON(t, w, http.StatusCreated, messagePayload(3, "in", "payment-confirmed"))
	}))
	defer server.Close()

	out, err := executeCommand(t, newMessagesCommand(server), "send", "order-pipeline", executionID,
		"payment-confirmed", "--data", `{"amount":12345678901234567890}`, "--idempotency-key", "pay-4417")
	require.NoError(t, err)

	assert.JSONEq(t, `{"name":"payment-confirmed","data":{"amount":12345678901234567890}}`, string(body),
		"the data is sent as written, large numbers included")
	assert.Equal(t, "pay-4417", header.Get("Idempotency-Key"))
	assert.Equal(t, "Bearer token", header.Get("Authorization"))
	for _, want := range []string{
		"Seq: 3", "Direction: in", "Name: payment-confirmed", "Status: queued", "Sender: project",
		`"amount": 4200`, `Sent "payment-confirmed" to execution ` + executionID,
	} {
		assert.Contains(t, out, want)
	}
	assert.NotContains(t, out, "Idempotency key", "a key the user chose is not repeated back")
}

// The generated key is printed before the request, so it is there to retry
// with when the send fails or its response never arrives.
func TestSendWithoutAKeyGeneratesOneAndPrintsItFirst(t *testing.T) {
	setMessagesTestHome(t)
	stderr := &lockedBuffer{}
	var key, printedBeforeRequest string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key = r.Header.Get("Idempotency-Key")
		printedBeforeRequest = stderr.String()
		respondJSON(t, w, http.StatusServiceUnavailable, map[string]any{"error": "unavailable"})
	}))
	defer server.Close()

	cmd := newMessagesCommand(server)
	cmd.SetOut(io.Discard)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"send", "order-pipeline", executionID, "payment-confirmed"})
	require.Error(t, cmd.Execute())

	_, parseErr := uuid.Parse(key)
	require.NoError(t, parseErr, "the generated key is a UUID: %q", key)
	assert.Contains(t, printedBeforeRequest, "Idempotency key "+key+"; rerun with --idempotency-key "+key)
}

// lockedBuffer is a writer a test server can read while the command writes.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Data is any JSON value, not only an object, and leaving it out sends a
// message without data rather than an empty string.
func TestSendReadsTheDataFromEitherFlag(t *testing.T) {
	dataFile := filepath.Join(t.TempDir(), "post.json")
	require.NoError(t, os.WriteFile(dataFile, []byte("[\"https://example.com/posts/1\"]\n"), 0o600))

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "no data", want: `{"name":"posted"}`},
		{name: "inline number", args: []string{"--data", "42"}, want: `{"name":"posted","data":42}`},
		{name: "inline string", args: []string{"--data", `"done"`}, want: `{"name":"posted","data":"done"}`},
		{name: "inline null", args: []string{"--data", "null"}, want: `{"name":"posted","data":null}`},
		{
			name: "file", args: []string{"--data-file", dataFile},
			want: `{"name":"posted","data":["https://example.com/posts/1"]}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setMessagesTestHome(t)
			var body []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var err error
				body, err = io.ReadAll(r.Body)
				assert.NoError(t, err)
				respondJSON(t, w, http.StatusCreated, messagePayload(1, "in", "posted"))
			}))
			defer server.Close()

			args := append([]string{"send", "order-pipeline", executionID, "posted"}, tc.args...)
			_, err := executeCommand(t, newMessagesCommand(server), args...)
			require.NoError(t, err)
			assert.JSONEq(t, tc.want, string(body))
		})
	}
}

func TestSendReportsARepeatedKeyAsNothingChanged(t *testing.T) {
	setMessagesTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respondJSON(t, w, http.StatusOK, messagePayload(3, "in", "payment-confirmed"))
	}))
	defer server.Close()

	out, err := executeCommand(t, newMessagesCommand(server), "send", "order-pipeline", executionID,
		"payment-confirmed", "--idempotency-key", "pay-4417")
	require.NoError(t, err)
	assert.Contains(t, out, "Already sent with this idempotency key; nothing changed")
	assert.NotContains(t, out, "Sent ")
}

// What a script reads from stdout under --json is the message alone.
func TestSendJSONPrintsOnlyTheMessage(t *testing.T) {
	setMessagesTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respondJSON(t, w, http.StatusCreated, messagePayload(3, "in", "payment-confirmed"))
	}))
	defer server.Close()

	stdout, _, err := executeCommandSplit(t, newMessagesCommand(server), "",
		"send", "order-pipeline", executionID, "payment-confirmed", "--json")
	require.NoError(t, err)
	var message map[string]any
	require.NoError(t, json.Unmarshal(stdout, &message), string(stdout))
	assert.Equal(t, messageID, message["id"])
	assert.InDelta(t, 3, message["seq"], 0)
}

// Each of these is refused before any request, with a message that names the
// mistake.
func TestSendRefusesBadArgumentsBeforeTheRequest(t *testing.T) {
	missingFile := filepath.Join(t.TempDir(), "missing.json")
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "execution name", args: []string{"send", "order-pipeline", "order-4417", "posted"}, want: `invalid execution ID "order-4417"`},
		{name: "malformed data", args: []string{"send", "order-pipeline", executionID, "posted", "--data", "{"}, want: "data must be JSON"},
		{name: "trailing data", args: []string{"send", "order-pipeline", executionID, "posted", "--data", "1 2"}, want: "unexpected data after the JSON value"},
		{name: "empty data", args: []string{"send", "order-pipeline", executionID, "posted", "--data", ""}, want: "data must be JSON"},
		{name: "missing file", args: []string{"send", "order-pipeline", executionID, "posted", "--data-file", missingFile}, want: "failed to read data file"},
		{name: "both flags", args: []string{"send", "order-pipeline", executionID, "posted", "--data", "1", "--data-file", missingFile}, want: "none of the others can be"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setMessagesTestHome(t)
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Error("no request may be sent")
			}))
			defer server.Close()

			_, err := executeCommand(t, newMessagesCommand(server), tc.args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestSendExplainsARefusal(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   map[string]any
		want   string
	}{
		{
			name: "ended execution", status: http.StatusConflict,
			body: map[string]any{"error": "durable execution is not running", "code": "execution_not_running"},
			want: "execution " + executionID + " has ended and takes no more messages",
		},
		{
			name: "full queue", status: http.StatusConflict,
			body: map[string]any{"error": "too many queued messages", "code": "too_many_queued_messages"},
			want: `failed to send a message to durable execution "` + executionID +
				`" of durable function "order-pipeline": HTTP 409: too many queued messages`,
		},
		{
			name: "read-only token", status: http.StatusForbidden,
			body: map[string]any{"error": "project access token is read-only"},
			want: "HTTP 403: project access token is read-only",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setMessagesTestHome(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				respondJSON(t, w, tc.status, tc.body)
			}))
			defer server.Close()

			_, err := executeCommand(t, newMessagesCommand(server), "send", "order-pipeline", executionID, "posted")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestListShowsTheMessagesAndHowToReadOn(t *testing.T) {
	setMessagesTestHome(t)
	var query map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, messagesPath, r.URL.Path)
		query = r.URL.Query()
		respondJSON(t, w, http.StatusOK, map[string]any{
			"data": []any{
				withFields(messagePayload(4, "out", "progress"), map[string]any{"data": map[string]any{"stage": "posting"}}),
				messagePayload(9, "out", "progress"),
			},
			"has_more": true,
		})
	}))
	defer server.Close()

	out, err := executeCommand(t, newMessagesCommand(server), "list", "order-pipeline", executionID,
		"--direction", "OUT", "--name", "progress", "--after", "3", "--limit", "2")
	require.NoError(t, err)

	assert.Equal(t, map[string][]string{
		"direction": {"out"}, "name": {"progress"}, "after": {"3"}, "limit": {"2"},
	}, query)
	for _, want := range []string{
		"Seq", "Direction", "Data", `{"stage":"posting"}`, "published", "progress",
		"Showing 2 message(s)",
		"Next page: volcano cloud durable messages list order-pipeline " + executionID +
			" --direction out --name progress --limit 2 --after 9",
	} {
		assert.Contains(t, out, want)
	}
}

func TestListWithNoMessages(t *testing.T) {
	setMessagesTestHome(t)
	var query map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		respondJSON(t, w, http.StatusOK, map[string]any{"data": []any{}, "has_more": false})
	}))
	defer server.Close()

	out, err := executeCommand(t, newMessagesCommand(server), "list", "order-pipeline", executionID)
	require.NoError(t, err)
	assert.Contains(t, out, "No messages found")
	assert.Equal(t, map[string][]string{"limit": {"100"}}, query, "an unset filter is not sent")
}

// A name can hold anything printable, and the hint has to paste back as the
// same name.
func TestListQuotesANameTheShellWouldSplit(t *testing.T) {
	setMessagesTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respondJSON(t, w, http.StatusOK, map[string]any{
			"data": []any{messagePayload(2, "in", "it's done")}, "has_more": true,
		})
	}))
	defer server.Close()

	out, err := executeCommand(t, newMessagesCommand(server), "list", "order-pipeline", executionID, "--name", "it's done")
	require.NoError(t, err)
	assert.Contains(t, out, `--name 'it'\''s done' --after 2`)
}

func TestListRefusesBadFlagsBeforeTheRequest(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "direction", args: []string{"--direction", "sideways"}, want: `unknown message direction "sideways": --direction takes in or out`},
		{name: "after", args: []string{"--after", "-1"}, want: "invalid --after -1"},
		{name: "zero limit", args: []string{"--limit", "0"}, want: "invalid --limit 0: expected 1 to 500"},
		{name: "negative limit", args: []string{"--limit", "-5"}, want: "invalid --limit -5: expected 1 to 500"},
		{name: "limit over the page size", args: []string{"--limit", "501"}, want: "invalid --limit 501: expected 1 to 500"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setMessagesTestHome(t)
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Error("no request may be sent")
			}))
			defer server.Close()

			args := append([]string{"list", "order-pipeline", executionID}, tc.args...)
			_, err := executeCommand(t, newMessagesCommand(server), args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// A follow reads on from the last seq it printed, and stops on the pass after
// it sees the execution end, so what was published before the end is printed.
func TestListFollowReadsUntilTheExecutionEnds(t *testing.T) {
	setMessagesTestHome(t)
	server, afters := newFollowServer(t, []followPass{
		{status: "running", pages: [][]any{{messagePayload(1, "out", "progress"), messagePayload(2, "out", "progress")}}},
		{status: "succeeded", pages: [][]any{{messagePayload(5, "out", "done")}}},
	})
	defer server.Close()

	out, err := executeCommand(t, newFollowCommand(server), "list", "order-pipeline", executionID, "--follow")
	require.NoError(t, err)

	assert.Equal(t, []string{"", "2"}, afters(), "the second pass reads after the last seq printed")
	assert.Equal(t, 1, strings.Count(out, "Direction"), "the header is printed once")
	for _, want := range []string{"progress", "done", "Execution order-4417 succeeded; it takes and publishes no more messages"} {
		assert.Contains(t, out, want)
	}
}

// One read returns at most a page, so a pass keeps reading while there is
// more, without waiting for the next tick.
func TestListFollowReadsEveryPageOfAPass(t *testing.T) {
	setMessagesTestHome(t)
	server, afters := newFollowServer(t, []followPass{
		{status: "failed", pages: [][]any{
			{messagePayload(1, "in", "posted")},
			{messagePayload(3, "out", "progress")},
		}},
	})
	defer server.Close()

	out, err := executeCommand(t, newFollowCommand(server), "list", "order-pipeline", executionID, "--follow")
	require.NoError(t, err)
	assert.Equal(t, []string{"", "1"}, afters())
	assert.Contains(t, out, "posted")
	assert.Contains(t, out, "Execution order-4417 failed")
}

// A follow outlives most commands, so a `volcano use` in another shell while it
// runs must not move its reads to a project where the execution ID names
// nothing.
func TestListFollowStaysInTheProjectItStartedIn(t *testing.T) {
	setMessagesTestHome(t)
	const otherProjectID = "33333333-3333-4333-8333-333333333333"
	var mu sync.Mutex
	var paths []string
	passes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case executionPath:
			passes++
			status := "running"
			if passes > 1 {
				status = "succeeded"
			}
			respondJSON(t, w, http.StatusOK, executionPayload(status))
		case messagesPath:
			switched := &cliconfig.Config{
				UserToken:      "token",
				CurrentProject: &cliconfig.ProjectConfig{ID: otherProjectID, Name: "Gamma"},
			}
			assert.NoError(t, switched.Save())
			respondJSON(t, w, http.StatusOK, map[string]any{"data": []any{}, "has_more": false})
		default:
			respondJSON(t, w, http.StatusNotFound, map[string]any{"error": "durable execution not found"})
		}
	}))
	defer server.Close()

	_, err := executeCommand(t, newFollowCommand(server), "list", "order-pipeline", executionID, "--follow")
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{executionPath, messagesPath, executionPath, messagesPath}, paths)
}

func TestListFollowJSONPrintsOneMessagePerLine(t *testing.T) {
	setMessagesTestHome(t)
	server, _ := newFollowServer(t, []followPass{
		{status: "stopped", pages: [][]any{{messagePayload(1, "out", "progress"), messagePayload(2, "out", "done")}}},
	})
	defer server.Close()

	stdout, _, err := executeCommandSplit(t, newFollowCommand(server), "",
		"list", "order-pipeline", executionID, "--follow", "--json")
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
	require.Len(t, lines, 2, string(stdout))
	for i, line := range lines {
		var message map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &message), line)
		assert.InDelta(t, i+1, message["seq"], 0)
	}
}

func TestListFollowOfAnExecutionWithoutMessages(t *testing.T) {
	setMessagesTestHome(t)
	server, _ := newFollowServer(t, []followPass{{status: "succeeded", pages: [][]any{{}}}})
	defer server.Close()

	out, err := executeCommand(t, newFollowCommand(server), "list", "order-pipeline", executionID, "--follow")
	require.NoError(t, err)
	assert.Contains(t, out, "No messages found")
	assert.Contains(t, out, "Execution order-4417 succeeded")
}

// The name and data come from a workflow or a sender, and are printed without
// the terminal escapes they carry.
func TestMessagesStripControlCharactersFromWhatSendersWrote(t *testing.T) {
	hostile := withFields(messagePayload(1, "in", "pay\x1b[2J-ment"), map[string]any{
		"data": map[string]any{"note": "a\u009b31mb"},
	})
	for _, args := range [][]string{
		{"list", "order-pipeline", executionID},
		{"send", "order-pipeline", executionID, "payment"},
	} {
		t.Run(args[0], func(t *testing.T) {
			setMessagesTestHome(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					respondJSON(t, w, http.StatusCreated, hostile)
					return
				}
				respondJSON(t, w, http.StatusOK, map[string]any{"data": []any{hostile}, "has_more": false})
			}))
			defer server.Close()

			out, err := executeCommand(t, newMessagesCommand(server), args...)
			require.NoError(t, err)
			assert.NotContains(t, out, "\x1b")
			assert.NotContains(t, out, "\u009b")
			assert.Contains(t, out, "pay[2J-ment")
		})
	}
}

func TestWaitsListsWhatTheExecutionWaitsFor(t *testing.T) {
	setMessagesTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, waitsPath, r.URL.Path)
		respondJSON(t, w, http.StatusOK, map[string]any{"data": []any{
			map[string]any{
				"id": waitID, "name": "payment-confirmed", "status": "waiting",
				"created_at": time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339),
				"expires_at": time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339),
			},
		}})
	}))
	defer server.Close()

	out, err := executeCommand(t, newMessagesCommand(server), "waits", "order-pipeline", executionID)
	require.NoError(t, err)
	for _, want := range []string{waitID, "payment-confirmed", "waiting", "5m ago", "in 2h"} {
		assert.Contains(t, out, want)
	}
}

func TestWaitsWithNothingOpen(t *testing.T) {
	setMessagesTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respondJSON(t, w, http.StatusOK, map[string]any{"data": []any{}})
	}))
	defer server.Close()

	out, err := executeCommand(t, newMessagesCommand(server), "waits", "order-pipeline", executionID)
	require.NoError(t, err)
	assert.Contains(t, out, "Execution "+executionID+" is not waiting for a message")
}

func TestWaitsReportsAnUnknownExecution(t *testing.T) {
	setMessagesTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respondJSON(t, w, http.StatusNotFound, map[string]any{"error": "durable execution not found"})
	}))
	defer server.Close()

	_, err := executeCommand(t, newMessagesCommand(server), "waits", "order-pipeline", executionID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `failed to list the message waits of durable execution "`+executionID+
		`" of durable function "order-pipeline": HTTP 404: durable execution not found`)
}

// followPass is what the server answers in one pass of a follow: the
// execution's status, then one list response per page.
type followPass struct {
	status string
	pages  [][]any
}

// newFollowServer answers each pass in turn and records the after each list
// asked for, "" for none.
func newFollowServer(t *testing.T, passes []followPass) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var afters []string
	pass, page := -1, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case executionPath:
			pass++
			page = 0
			if pass >= len(passes) {
				t.Errorf("follow read the execution %d times, after it ended", pass+1)
				respondJSON(t, w, http.StatusInternalServerError, map[string]any{"error": "unexpected"})
				return
			}
			respondJSON(t, w, http.StatusOK, executionPayload(passes[pass].status))
		case messagesPath:
			afters = append(afters, r.URL.Query().Get("after"))
			pages := passes[pass].pages
			respondJSON(t, w, http.StatusOK, map[string]any{"data": pages[page], "has_more": page < len(pages)-1})
			page++
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	return server, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), afters...)
	}
}

// newFollowCommand ticks as soon as the follow waits, so its passes run back
// to back.
func newFollowCommand(server *httptest.Server) *cobra.Command {
	deps := cliruntime.Deps{
		CommandPathPrefix: "volcano cloud",
		HTTPClient:        server.Client(),
		APIBaseURL:        server.URL,
		NewTicker:         func(time.Duration) cliruntime.Ticker { return immediateTicker{} },
	}
	return New(deps)
}

type immediateTicker struct{}

func (immediateTicker) C() <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- time.Now()
	return ch
}

func (immediateTicker) Reset(time.Duration) {}

func (immediateTicker) Stop() {}

func newMessagesCommand(server *httptest.Server) *cobra.Command {
	return New(cliruntime.Deps{
		CommandPathPrefix: "volcano cloud",
		HTTPClient:        server.Client(),
		APIBaseURL:        server.URL,
	})
}

func executeCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func executeCommandSplit(t *testing.T, cmd *cobra.Command, stdin string, args ...string) ([]byte, []byte, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(bytes.NewBufferString(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.Bytes(), stderr.Bytes(), err
}

func setMessagesTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")

	cfg := &cliconfig.Config{
		UserToken:      "token",
		CurrentProject: &cliconfig.ProjectConfig{ID: projectID, Name: "Beta"},
	}
	require.NoError(t, cfg.Save())
}

func respondJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func messagePayload(seq int, direction, name string) map[string]any {
	status := "queued"
	if direction == "out" {
		status = "published"
	}
	return map[string]any{
		"id":           messageID,
		"execution_id": executionID,
		"direction":    direction,
		"seq":          seq,
		"name":         name,
		"data":         map[string]any{"amount": 4200},
		"sender":       map[string]any{"type": "project", "user_id": nil},
		"status":       status,
		"sent_at":      "2026-10-12T09:00:00Z",
		"delivered_at": nil,
	}
}

func executionPayload(status string) map[string]any {
	return map[string]any{
		"id":                  executionID,
		"durable_function_id": "55555555-5555-4555-8555-555555555555",
		"project_id":          projectID,
		"name":                "order-4417",
		"status":              status,
		"region":              "aws-us-east-1",
		"created_at":          "2026-10-12T09:00:00Z",
		"updated_at":          "2026-10-12T09:00:00Z",
	}
}

func withFields(payload, fields map[string]any) map[string]any {
	maps.Copy(payload, fields)
	return payload
}
