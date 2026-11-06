package sandboxes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEgressReachesEveryRouteThatCreatesASession(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, path, response string
		status               int
		args                 []string
	}{
		{"run", "/projects/" + testProject + "/sandbox-sessions", `{"id":"` + testSession + `","expires_at":null}`, http.StatusCreated, []string{"run", "--preset", "python3.12"}},
		{"one-shot exec", "/projects/" + testProject + "/sandbox-executions", `{"stdout":"","stderr":"","exit_code":0}`, http.StatusOK, []string{"exec", "--preset", "python3.12"}},
	} {
		for _, egress := range []string{"", "default", "deny_all"} {
			t.Run(tc.name+"/egress="+egress, func(t *testing.T) {
				t.Parallel()
				requests := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests++
					assert.Equal(t, tc.path, r.URL.Path)
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); !assert.NoError(t, err) {
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					if egress == "" {
						assert.NotContains(t, body, "network", "omitted keeps the server default")
					} else {
						assert.Equal(t, map[string]any{"egress": egress}, body["network"])
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.response))
				}))
				defer server.Close()
				args := slices.Clone(tc.args)
				if egress != "" {
					args = append(args, "--egress", egress)
				}
				if tc.name == "one-shot exec" {
					args = append(args, "--", "true")
				}
				err := execute(server, args...)
				require.NoError(t, err)
				assert.Equal(t, 1, requests)
			})
		}
	}
}

func TestEgressIsRefusedBeforeTransport(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"unknown policy":   {[]string{"run", "--preset", "python3.12", "--egress", "allow_all"}, `"--egress" flag: must be default or deny_all`},
		"empty policy":     {[]string{"run", "--preset", "python3.12", "--egress="}, `"--egress" flag: must be default or deny_all`},
		"empty exec":       {[]string{"exec", "--preset", "python3.12", "--egress", "", "--", "true"}, `"--egress" flag: must be default or deny_all`},
		"existing session": {[]string{"exec", testSession, "--egress", "deny_all", "--", "true"}, "egress only apply when creating a sandbox"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				assert.Fail(t, "a refused flag must not reach transport")
				w.WriteHeader(http.StatusBadRequest)
			}))
			defer server.Close()
			require.ErrorContains(t, execute(server, tc.args...), tc.want)
		})
	}
}

func execute(server *httptest.Server, args ...string) error {
	cmd := New(testDeps(server))
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs(args)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	return cmd.Execute()
}
