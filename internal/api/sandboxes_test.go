package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

const execBodyCanary = "exec-output-canary-7d2e"

// A success body the client cannot read is the command's output, so the error
// names neither it nor anything in it.
func TestExecNeverPrintsAnUnreadableSuccessBody(t *testing.T) {
	body := "\x1b]0;" + execBodyCanary + "\x07\x1b[2J" + execBodyCanary + "\r\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL, "pk-account", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	calls := map[string]func() error{
		"session": func() error {
			_, err := client.ExecSandboxSession(context.Background(), uuid.New(), uuid.New(), apiclient.SandboxCommandRequest{Command: "true"})
			return err
		},
		"one-shot": func() error {
			_, err := client.ExecSandbox(context.Background(), uuid.New(), uuid.New(), apiclient.SandboxExecutionRequest{Command: "true"})
			return err
		},
	}
	for name, call := range calls {
		err := call()
		require.Error(t, err, name)
		assert.Equal(t, "HTTP 200: the command ran, but its result could not be read", err.Error(), name)
		assert.NotContains(t, err.Error(), execBodyCanary, name)
	}
}

// rightToLeftOverride is U+202E, a format character that reorders the text
// after it.
var rightToLeftOverride = string(rune(0x202e))

// A body printed as an error cannot send escape sequences to the terminal.
func TestApiErrorStripsControlAndEscapeCharacters(t *testing.T) {
	for name, tc := range map[string]struct {
		body, want string
	}{
		"plain text": {body: "bad\x1b[2J\x1b]0;title\x07 gate\rway" + rightToLeftOverride + "\x00\u009b31m\xff", want: "HTTP 502: bad[2J]0;title gateway31m"},
		"json":       {body: `{"error":"bad\u001b[31m gate\u000dway","code":"x\u001by"}`, want: "HTTP 502: bad[31m gateway"},
	} {
		err := apiError(http.StatusBadGateway, []byte(tc.body))
		assert.Equal(t, tc.want, err.Error(), name)
		for _, char := range []string{"\x1b", "\x07", "\r", "\x00", rightToLeftOverride, "\u009b"} {
			assert.NotContains(t, err.Error(), char, name)
		}
		var apiErr *Error
		require.ErrorAs(t, err, &apiErr, name)
		assert.NotContains(t, apiErr.Code, "\x1b", name)
	}
}

// A JSON error the generated client parses is printed the same way.
func TestParsedApiErrorStripsControlAndEscapeCharacters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad\u001b[31m gate\u000dway\u009b","code":"x\u001by"}`))
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL, "pk-account", WithHTTPClient(server.Client()))
	require.NoError(t, err)
	_, err = client.ExecSandboxSession(context.Background(), uuid.New(), uuid.New(), apiclient.SandboxCommandRequest{Command: "true"})
	require.Error(t, err)
	assert.Equal(t, "HTTP 400: bad[31m gateway", err.Error())
	var apiErr *Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "xy", apiErr.Code)
}
