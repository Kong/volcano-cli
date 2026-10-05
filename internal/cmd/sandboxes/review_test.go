package sandboxes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandTimeoutAndTruncationOutput(t *testing.T) {
	t.Parallel()
	for _, useSession := range []bool{false, true} {
		for _, asJSON := range []bool{false, true} {
			t.Run(map[bool]string{true: "session", false: "oneshot"}[useSession]+"/"+map[bool]string{true: "json", false: "text"}[asJSON], func(t *testing.T) {
				t.Parallel()
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"stdout":"partial","stderr":"guest error","exit_code":-1,"timed_out":true,"stdout_truncated":true,"stderr_truncated":true}`))
				}))
				defer server.Close()
				cmd := New(testDeps(server))
				var out, errs bytes.Buffer
				cmd.SetOut(&out)
				cmd.SetErr(&errs)
				cmd.SilenceErrors = true
				cmd.SilenceUsage = true
				args := []string{"exec"}
				if asJSON {
					args = append(args, "--json")
				}
				if useSession {
					args = append(args, testSession)
				} else {
					args = append(args, "--preset", "node22")
				}
				args = append(args, "--", "true")
				cmd.SetArgs(args)
				var exited *ExitError
				require.ErrorAs(t, cmd.Execute(), &exited)
				assert.Equal(t, 124, exited.ExitCode())
				if asJSON {
					var body map[string]any
					require.NoError(t, json.Unmarshal(out.Bytes(), &body))
					assert.Equal(t, true, body["timed_out"])
					assert.Empty(t, errs.String())
				} else {
					assert.Equal(t, "partial", out.String())
					assert.Contains(t, errs.String(), "timed out")
					assert.Contains(t, errs.String(), "stdout was truncated")
					assert.Contains(t, errs.String(), "stderr was truncated")
				}
			})
		}
	}
}

func TestSandboxInputValidationBeforeTransport(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"exec", "--preset", "node22", "--timeout", "0", "--", "true"},
		{"exec", "--preset", "node22", "--timeout", "61", "--", "true"},
		{"exec", testSession, "--timeout", "3601", "--", "true"},
		{"shell", testSession, "--timeout", "0"},
		{"run", "--preset", "node22", "--duration", "29"},
		{"run", "--preset", "node22", "--duration", "28801"},
		{"templates", "create", "name"},
		{"templates", "create", "name", "--preset", ""},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				t.Error("invalid input must not reach transport")
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer server.Close()
			cmd := New(testDeps(server))
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(args)
			require.Error(t, cmd.Execute())
		})
	}
}

func TestShellAccepts64KiBCommand(t *testing.T) {
	t.Parallel()
	command := strings.Repeat("x", 65536)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Command string `json:"command"`
		}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, command, body.Command)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"stdout":"ok","stderr":"","exit_code":0}`))
	}))
	defer server.Close()
	cmd := New(testDeps(server))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(strings.NewReader(command + "\nexit\n"))
	cmd.SetArgs([]string{"shell", testSession})
	require.NoError(t, cmd.Execute())
	assert.Equal(t, "ok", out.String())
}

func TestManagementOutputIsJSONWithAndWithoutFlag(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()
	for _, args := range [][]string{{"presets"}, {"presets", "--json"}} {
		cmd := New(testDeps(server))
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs(args)
		require.NoError(t, cmd.Execute())
		var decoded map[string]any
		require.NoError(t, json.Unmarshal(out.Bytes(), &decoded))
		assert.Contains(t, decoded, "data")
	}
}
