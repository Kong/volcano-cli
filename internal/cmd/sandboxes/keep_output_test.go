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

// capabilitiesPath is the test project's capability list.
const capabilitiesPath = "/projects/" + testProject + "/capabilities"

func capabilitiesBody(status string) string {
	return `{"data":[{"id":"sandboxes","status":"available"},{"id":"sandboxes.output_not_kept","status":"` + status + `"}]}`
}

// --keep-output reaches the API only when set, so a session command that
// omits it uses the session's setting. Asking to keep no output reads the
// project's capabilities first.
func TestKeepOutputFlagIsSentOnlyWhenSet(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		args []string
		path string
		body string
		want map[string]any
	}{
		{"session exec inherits", []string{"exec", testSession, "--", "true"}, "/sandbox-sessions/" + testSession + "/exec", `{"stdout":"","stderr":"","exit_code":0,"output_kept":true}`, nil},
		{"session exec keeps none", []string{"exec", testSession, "--keep-output=false", "--", "true"}, "/sandbox-sessions/" + testSession + "/exec", `{"stdout":"","stderr":"","exit_code":0,"output_kept":false}`, map[string]any{"keep_output": false}},
		{"session exec keeps", []string{"exec", testSession, "--keep-output", "--", "true"}, "/sandbox-sessions/" + testSession + "/exec", `{"stdout":"","stderr":"","exit_code":0,"output_kept":true}`, map[string]any{"keep_output": true}},
		{"one-shot keeps none", []string{"exec", "--preset", "python3.12", "--keep-output=false", "--", "true"}, "/projects/" + testProject + "/sandbox-executions", `{"stdout":"","stderr":"","exit_code":0,"output_kept":false,"session_id":"` + testSession + `","region":"us-east-1","duration_ms":1}`, map[string]any{"keep_output": false}},
		{"session keeps none", []string{"run", "--preset", "python3.12", "--keep-output=false"}, "/projects/" + testProject + "/sandbox-sessions", `{"id":"` + testSession + `","executions":{"keep_output":false}}`, map[string]any{"executions": map[string]any{"keep_output": false}}},
		{"session default", []string{"run", "--preset", "python3.12"}, "/projects/" + testProject + "/sandbox-sessions", `{"id":"` + testSession + `","executions":{"keep_output":true}}`, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			requests, capabilityReads := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == capabilitiesPath {
					capabilityReads++
					_, _ = w.Write([]byte(capabilitiesBody("available")))
					return
				}
				requests++
				assert.Equal(t, test.path, r.URL.Path)
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); !assert.NoError(t, err) {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				for _, field := range []string{"keep_output", "executions"} {
					want, set := test.want[field]
					if set {
						assert.Equal(t, want, body[field])
					} else {
						assert.NotContains(t, body, field)
					}
				}
				if strings.HasSuffix(r.URL.Path, "/sandbox-sessions") {
					w.WriteHeader(http.StatusCreated)
				}
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			cmd := New(testDeps(server))
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(test.args)
			require.NoError(t, cmd.Execute())
			assert.Equal(t, 1, requests)
			keepsNone := strings.Contains(strings.Join(test.args, " "), "--keep-output=false")
			assert.Equal(t, keepsNone, capabilityReads == 1, "only keeping no output reads the capabilities")
		})
	}
}

// Where the capability is unavailable, asking to keep no output refuses before
// anything runs.
func TestKeepOutputFalseRefusesWhereUnavailable(t *testing.T) {
	t.Parallel()
	for name, args := range map[string][]string{
		"session exec": {"exec", testSession, "--keep-output=false", "--", "true"},
		"one-shot":     {"exec", "--preset", "python3.12", "--keep-output=false", "--", "true"},
		"session":      {"run", "--preset", "python3.12", "--keep-output=false"},
		"shell":        {"shell", testSession, "--keep-output=false"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != capabilitiesPath {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(capabilitiesBody("unavailable")))
			}))
			defer server.Close()
			cmd := New(testDeps(server))
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetIn(strings.NewReader("true\n"))
			cmd.SetArgs(args)
			err := cmd.Execute()
			require.ErrorIs(t, err, errOutputNotKeptUnavailable)
		})
	}
}
