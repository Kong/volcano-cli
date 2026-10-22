package callbacks

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	projectID = "22222222-2222-4222-8222-222222222222"
	token     = "vcb_0000000000000000000000000000000000000000000"
)

// The CLI is logged in, but the answer carries only the token: the routes
// take no other credential, and the holder may not be a project member.
func TestSucceedSendsTheTokenAndResultAndNoCredential(t *testing.T) {
	setCallbacksTestHome(t)
	var body []byte
	var header http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/durable-callbacks/succeed", r.URL.Path)
		header = r.Header.Clone()
		var err error
		body, err = io.ReadAll(r.Body)
		assert.NoError(t, err)
		respondJSON(t, w, http.StatusOK, map[string]any{"status": "succeeded"})
	}))
	defer server.Close()

	out, err := executeCommand(t, newCallbacksCommand(server), "",
		"succeed", token, "--result", `{"amount":12345678901234567890}`)
	require.NoError(t, err)

	assert.JSONEq(t, `{"token":"`+token+`","result":{"amount":12345678901234567890}}`, string(body),
		"the result is sent as written, large numbers included")
	assert.Empty(t, header.Get("Authorization"))
	assert.Contains(t, out, "Succeeded the callback; the execution resumes with the result")
	assert.NotContains(t, out, token, "the token is not printed back")
}

// The system a workflow hands a token to has never logged in.
func TestAnswersNeedNoLoginOrProject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/durable-callbacks/heartbeat", r.URL.Path)
		respondJSON(t, w, http.StatusOK, map[string]any{"status": "waiting"})
	}))
	defer server.Close()

	out, err := executeCommand(t, newCallbacksCommand(server), "", "heartbeat", token)
	require.NoError(t, err)
	assert.Contains(t, out, "Heartbeat sent; the callback is still waiting")
}

func TestSucceedReadsTheResultFromEitherFlag(t *testing.T) {
	resultFile := filepath.Join(t.TempDir(), "charge.json")
	require.NoError(t, os.WriteFile(resultFile, []byte("[\"ch_1\"]\n"), 0o600))

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "inline string", args: []string{"--result", `"done"`}, want: `"done"`},
		{name: "inline false", args: []string{"--result", "false"}, want: `false`},
		{name: "file", args: []string{"--result-file", resultFile}, want: `["ch_1"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setCallbacksTestHome(t)
			var body []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var err error
				body, err = io.ReadAll(r.Body)
				assert.NoError(t, err)
				respondJSON(t, w, http.StatusOK, map[string]any{"status": "succeeded"})
			}))
			defer server.Close()

			_, err := executeCommand(t, newCallbacksCommand(server), "", append([]string{"succeed", token}, tc.args...)...)
			require.NoError(t, err)
			assert.JSONEq(t, `{"token":"`+token+`","result":`+tc.want+`}`, string(body))
		})
	}
}

func TestFailSendsTheError(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{
			name: "with a message", args: []string{"--type", "CardDeclined", "--message", "Insufficient funds"},
			want: `{"type":"CardDeclined","message":"Insufficient funds"}`,
		},
		{name: "type only", args: []string{"--type", "CardDeclined"}, want: `{"type":"CardDeclined"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setCallbacksTestHome(t)
			var body []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/durable-callbacks/fail", r.URL.Path)
				var err error
				body, err = io.ReadAll(r.Body)
				assert.NoError(t, err)
				respondJSON(t, w, http.StatusOK, map[string]any{"status": "failed"})
			}))
			defer server.Close()

			out, err := executeCommand(t, newCallbacksCommand(server), "", append([]string{"fail", token}, tc.args...)...)
			require.NoError(t, err)
			assert.JSONEq(t, `{"token":"`+token+`","error":`+tc.want+`}`, string(body))
			assert.Contains(t, out, "Failed the callback; the execution resumes with the error")
		})
	}
}

// A token piped in stays out of shell history and the process list.
func TestEachCommandReadsTheTokenFromStdin(t *testing.T) {
	for _, args := range [][]string{
		{"succeed", "-", "--result", "1"},
		{"fail", "-", "--type", "CardDeclined"},
		{"heartbeat", "-"},
	} {
		t.Run(args[0], func(t *testing.T) {
			setCallbacksTestHome(t)
			var sent struct {
				Token string `json:"token"`
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&sent))
				respondJSON(t, w, http.StatusOK, map[string]any{"status": "waiting"})
			}))
			defer server.Close()

			_, err := executeCommand(t, newCallbacksCommand(server), "  "+token+"\n", args...)
			require.NoError(t, err)
			assert.Equal(t, token, sent.Token)
		})
	}
}

// Volcano recorded the answer but has not handed it over yet. That is not a
// failure, and sending it again would be refused.
func TestAnAnswerStillBeingDeliveredSaysNotToResend(t *testing.T) {
	for _, args := range [][]string{
		{"succeed", token, "--result", "1"},
		{"fail", token, "--type", "CardDeclined"},
	} {
		t.Run(args[0], func(t *testing.T) {
			setCallbacksTestHome(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				respondJSON(t, w, http.StatusAccepted, map[string]any{"status": "delivering"})
			}))
			defer server.Close()

			out, err := executeCommand(t, newCallbacksCommand(server), "", args...)
			require.NoError(t, err)
			assert.Contains(t, out, "Volcano recorded the answer and is still delivering it to the execution; do not send it again")
			assert.NotContains(t, out, "the execution resumes")
		})
	}
}

// What a script reads from stdout under --json is the outcome alone.
func TestJSONPrintsOnlyTheOutcome(t *testing.T) {
	setCallbacksTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respondJSON(t, w, http.StatusAccepted, map[string]any{"status": "delivering"})
	}))
	defer server.Close()

	cmd := newCallbacksCommand(server)
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"succeed", token, "--result", "1", "--json"})
	require.NoError(t, cmd.Execute())
	assert.JSONEq(t, `{"status":"delivering"}`, stdout.String())
}

// Each of these is refused before any request, with a message that names the
// mistake and never quotes a token.
func TestRefusesBadArgumentsBeforeTheRequest(t *testing.T) {
	const almostAToken = "vcb_0000000000000000000000/00000000000000000"
	missingFile := filepath.Join(t.TempDir(), "missing.json")
	for _, tc := range []struct {
		name  string
		stdin string
		args  []string
		want  string
	}{
		{name: "malformed token", args: []string{"heartbeat", almostAToken}, want: "invalid callback token"},
		{name: "token without its prefix", args: []string{"heartbeat", "0000000000"}, want: "invalid callback token"},
		{name: "empty stdin", args: []string{"heartbeat", "-"}, want: "no callback token on stdin"},
		{name: "null result", args: []string{"succeed", token, "--result", " null "}, want: "result cannot be null"},
		{name: "malformed result", args: []string{"succeed", token, "--result", "{"}, want: "result must be JSON"},
		{name: "no result", args: []string{"succeed", token}, want: "at least one of the flags in the group [result result-file] is required"},
		{name: "both result flags", args: []string{"succeed", token, "--result", "1", "--result-file", missingFile}, want: "none of the others can be"},
		{name: "missing result file", args: []string{"succeed", token, "--result-file", missingFile}, want: "failed to read result file"},
		{name: "no type", args: []string{"fail", token}, want: `required flag(s) "type" not set`},
		{name: "blank type", args: []string{"fail", token, "--type", " "}, want: "--type needs a name the workflow can branch on"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setCallbacksTestHome(t)
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Error("no request may be sent")
			}))
			defer server.Close()

			_, err := executeCommand(t, newCallbacksCommand(server), tc.stdin, tc.args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.NotContains(t, err.Error(), almostAToken)
		})
	}
}

// A holder decides from the refusal whether to retry, so each says what became
// of the wait.
func TestExplainsARefusal(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		status int
		body   map[string]any
		want   string
	}{
		{
			name: "unknown token", args: []string{"heartbeat", token}, status: http.StatusNotFound,
			body: map[string]any{"error": "callback not found", "code": "callback_not_found"},
			want: "no callback has this token",
		},
		{
			name: "answered", args: []string{"succeed", token, "--result", "1"}, status: http.StatusConflict,
			body: map[string]any{"error": "callback already has an outcome", "code": "callback_completed"},
			want: "the callback already has an outcome; a token takes one answer",
		},
		{
			name: "timed out", args: []string{"fail", token, "--type", "CardDeclined"}, status: http.StatusGone,
			body: map[string]any{"error": "callback expired", "code": "callback_expired"},
			want: "the callback timed out; nothing will take an answer, so do not retry",
		},
		{
			name: "execution ended", args: []string{"heartbeat", token}, status: http.StatusGone,
			body: map[string]any{"error": "durable execution ended", "code": "execution_ended"},
			want: "the callback's execution has ended; nothing will take an answer, so do not retry",
		},
		{
			name: "heartbeat unavailable", args: []string{"heartbeat", token}, status: http.StatusServiceUnavailable,
			body: map[string]any{"error": "unavailable"},
			want: "the heartbeat could not reach the execution right now; the wait is unchanged, so retry with backoff",
		},
		{
			name: "rate limited", args: []string{"succeed", token, "--result", "1"}, status: http.StatusTooManyRequests,
			body: map[string]any{"error": "too many callback requests"},
			want: "failed to succeed the callback: HTTP 429: too many callback requests",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setCallbacksTestHome(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				respondJSON(t, w, tc.status, tc.body)
			}))
			defer server.Close()

			_, err := executeCommand(t, newCallbacksCommand(server), "", tc.args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func newCallbacksCommand(server *httptest.Server) *cobra.Command {
	return New(cliruntime.Deps{
		CommandPathPrefix: "volcano cloud",
		HTTPClient:        server.Client(),
		APIBaseURL:        server.URL,
	})
}

func executeCommand(t *testing.T, cmd *cobra.Command, stdin string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(bytes.NewBufferString(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func setCallbacksTestHome(t *testing.T) {
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
