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

// --keep-output reaches the API only when set, so a session command that
// omits it uses the session's setting.
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
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				w.Header().Set("Content-Type", "application/json")
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
		})
	}
}
