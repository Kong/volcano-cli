package sandboxes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	testProject = "11111111-1111-4111-8111-111111111111"
	testSession = "22222222-2222-4222-8222-222222222222"
	testRequest = "33333333-3333-4333-8333-333333333333"
)

func TestExecPreservesOutputExitAndReplayKey(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/projects/"+testProject+"/sandbox-executions", r.URL.Path)
		assert.Equal(t, "Bearer service-key", r.Header.Get("Authorization"))
		assert.Equal(t, testRequest, r.Header.Get("Idempotency-Key"))
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); !assert.NoError(t, err) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		assert.Equal(t, "'sh' '-c' 'printf out; printf err >&2; exit 7'", body["command"])
		assert.NotContains(t, body, "memory_mb", "inherit the template's memory")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"stdout":"out","stderr":"err","exit_code":7}`))
	}))
	defer server.Close()
	cmd := New(testDeps(server))
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	var out, errs bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errs)
	cmd.SetArgs([]string{"exec", "--template", testSession, "--request-id", testRequest, "--", "sh", "-c", "printf out; printf err >&2; exit 7"})
	err := cmd.Execute()
	var exited *ExitError
	require.ErrorAs(t, err, &exited)
	assert.Equal(t, 7, exited.ExitCode())
	assert.Equal(t, "out", out.String())
	assert.Equal(t, "err", errs.String())
}

func TestBinaryFilesUseBytesWithoutDoubleEncoding(t *testing.T) {
	t.Parallel()
	data := []byte{0, 255, 128, 10}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/sandbox-sessions/" + testSession + "/files/write":
			var body struct {
				Data []byte `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); !assert.NoError(t, err) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			assert.Equal(t, data, body.Data)
			w.WriteHeader(http.StatusNoContent)
		case "/sandbox-sessions/" + testSession + "/files/read":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	write := New(testDeps(server))
	write.SetIn(bytes.NewReader(data))
	write.SetOut(&bytes.Buffer{})
	write.SetArgs([]string{"files", "write", testSession, "/workspace/file"})
	require.NoError(t, write.Execute())
	read := New(testDeps(server))
	var out bytes.Buffer
	read.SetOut(&out)
	read.SetArgs([]string{"files", "read", testSession, "/workspace/file"})
	require.NoError(t, read.Execute())
	assert.Equal(t, data, out.Bytes())
}

func testDeps(server *httptest.Server) cliruntime.Deps {
	return cliruntime.Deps{APIBaseURL: server.URL, HTTPClient: server.Client(), ConfigLoader: func() (*config.Config, error) {
		return &config.Config{UserToken: "service-key", CurrentProject: &config.ProjectConfig{ID: testProject}}, nil
	}}
}

func TestShellCommandQuotesEveryArgument(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "'echo' 'it'\"'\"'s safe' '$(touch /tmp/no)'", shellCommand([]string{"echo", "it's safe", "$(touch /tmp/no)"}))
}

func TestShellExecutesLinesAndDetachesWithoutTermination(t *testing.T) {
	t.Parallel()
	commands := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/sandbox-sessions/"+testSession+"/exec", r.URL.Path)
		var body struct {
			Command string `json:"command"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); !assert.NoError(t, err) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		commands <- body.Command
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"stdout":"ok","stderr":"","exit_code":0}`))
	}))
	defer server.Close()
	cmd := New(testDeps(server))
	var out, errs bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errs)
	cmd.SetIn(bytes.NewBufferString("echo first\ncat value | wc -c\nexit\n"))
	cmd.SetArgs([]string{"shell", testSession})
	require.NoError(t, cmd.Execute())
	assert.Equal(t, "echo first", <-commands)
	assert.Equal(t, "cat value | wc -c", <-commands)
	assert.Equal(t, "okok", out.String())
}

func TestTemplateDeletionRequiresConfirmationAndListsNextPage(t *testing.T) {
	t.Parallel()
	requests := make(chan string, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Method
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		assert.Equal(t, "next-page", r.URL.Query().Get("cursor"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"pagination":{"limit":10,"has_more":false}}`))
	}))
	defer server.Close()
	declined := New(testDeps(server))
	var messages bytes.Buffer
	declined.SetIn(bytes.NewBufferString("no\n"))
	declined.SetErr(&messages)
	declined.SetArgs([]string{"templates", "delete", testSession})
	require.NoError(t, declined.Execute())
	assert.Empty(t, requests)
	assert.Contains(t, messages.String(), "terminates all of its sessions")
	accepted := New(testDeps(server))
	accepted.SetOut(&bytes.Buffer{})
	accepted.SetArgs([]string{"templates", "delete", testSession, "--yes"})
	require.NoError(t, accepted.Execute())
	assert.Equal(t, http.MethodDelete, <-requests)
	listed := New(testDeps(server))
	listed.SetOut(&bytes.Buffer{})
	listed.SetArgs([]string{"templates", "list", "--cursor", "next-page"})
	require.NoError(t, listed.Execute())
	assert.Equal(t, http.MethodGet, <-requests)
}
