package sandboxes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestRunSessionLimits(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		args   []string
		limits map[string]any
		err    string
	}{
		{name: "omitted", args: nil},
		{name: "vcpus", args: []string{"--vcpus", "2"}, limits: map[string]any{"vcpus": json.Number("2")}},
		{name: "disk", args: []string{"--disk-mib", "256"}, limits: map[string]any{"disk_mib": json.Number("256")}},
		{name: "both", args: []string{"--vcpus", "1", "--disk-mib", "256"}, limits: map[string]any{"vcpus": json.Number("1"), "disk_mib": json.Number("256")}},
		{name: "lowest", args: []string{"--vcpus", "1", "--disk-mib", "64"}, limits: map[string]any{"vcpus": json.Number("1"), "disk_mib": json.Number("64")}},
		{name: "highest", args: []string{"--vcpus", "4", "--disk-mib", "6144"}, limits: map[string]any{"vcpus": json.Number("4"), "disk_mib": json.Number("6144")}},
		{name: "vcpus zero", args: []string{"--vcpus", "0"}, err: "--vcpus must be between 1 and 4"},
		{name: "vcpus above", args: []string{"--vcpus", "5"}, err: "--vcpus must be between 1 and 4"},
		{name: "disk zero", args: []string{"--disk-mib", "0"}, err: "--disk-mib must be between 64 and 6144"},
		{name: "disk below", args: []string{"--disk-mib", "63"}, err: "--disk-mib must be between 64 and 6144"},
		{name: "disk above", args: []string{"--vcpus", "2", "--disk-mib", "6145"}, err: "--disk-mib must be between 64 and 6144"},
	} {
		for _, local := range []bool{false, true} {
			name := map[bool]string{false: "cloud/", true: "local/"}[local] + tc.name
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				var requests atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					assert.Equal(t, http.MethodPost, r.Method)
					assert.Equal(t, "/projects/"+testProject+"/sandbox-sessions", r.URL.Path)
					var body map[string]any
					decoder := json.NewDecoder(r.Body)
					decoder.UseNumber()
					if err := decoder.Decode(&body); !assert.NoError(t, err) {
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					if tc.limits == nil {
						assert.NotContains(t, body, "limits", "no flags keep the request a session without caps")
					} else {
						assert.Equal(t, tc.limits, body["limits"])
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte(`{"id":"` + testSession + `","expires_at":null}`))
				}))
				defer server.Close()
				cmd := New(testDeps(server))
				if local {
					cmd = NewLocal(testDeps(server), testDeps(server))
				}
				cmd.SetArgs(append([]string{"run", "--preset", "node22"}, tc.args...))
				cmd.SetOut(&bytes.Buffer{})
				cmd.SetErr(&bytes.Buffer{})
				err := cmd.Execute()
				if tc.err != "" {
					require.ErrorContains(t, err, tc.err)
					assert.Zero(t, requests.Load(), "an out-of-range cap must not reach the API")
					return
				}
				require.NoError(t, err)
				assert.Equal(t, int32(1), requests.Load())
			})
		}
	}
}

func TestOneShotExecHasNoLimits(t *testing.T) {
	t.Parallel()
	for _, flag := range []string{"--vcpus", "--disk-mib"} {
		// Flag parsing refuses before any request, so no server is needed.
		cmd := New(cliruntime.Deps{})
		cmd.SetArgs([]string{"exec", "--preset", "node22", flag, "1", "--", "true"})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		require.ErrorContains(t, cmd.Execute(), "unknown flag: "+flag)
	}
}

func TestSessionOutputShowsLimits(t *testing.T) {
	t.Parallel()
	for _, applied := range []string{`true`, `false`, `"unknown"`} {
		limits := `{"vcpus":1,"disk_mib":256,"applied":` + applied + `}`
		session := `{"id":"` + testSession + `","project_id":"` + testProject + `","region":"us-east-1","memory_mb":1024,"state":"starting","desired_state":"running","created_at":"2026-11-04T00:00:00Z","expires_at":null,"sandbox_id":null,"limits":` + limits + `}`
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/projects/" + testProject + "/sandbox-sessions":
				if r.Method == http.MethodPost {
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte(session))
					return
				}
				_, _ = w.Write([]byte(`{"data":[` + session + `],"pagination":{"limit":10,"has_more":false}}`))
			case "/sandbox-sessions/" + testSession:
				_, _ = w.Write([]byte(session))
			default:
				t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		for _, args := range [][]string{
			{"run", "--preset", "node22", "--vcpus", "1", "--disk-mib", "256"},
			{"get", testSession},
			{"sessions"},
		} {
			for _, format := range [][]string{nil, {"--json"}} {
				line := append(append([]string{}, args...), format...)
				cmd := New(testDeps(server))
				var out bytes.Buffer
				cmd.SetOut(&out)
				cmd.SetArgs(line)
				require.NoError(t, cmd.Execute(), strings.Join(line, " "))
				assert.JSONEq(t, limits, readLimits(t, out.Bytes()), "applied=%s %s", applied, strings.Join(line, " "))
			}
		}
		server.Close()
	}
}

func TestSessionOutputOmitsLimitsWithoutCaps(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"` + testSession + `","project_id":"` + testProject + `","region":"us-east-1","memory_mb":1024,"state":"running","desired_state":"running","created_at":"2026-11-04T00:00:00Z","expires_at":null,"sandbox_id":null}`))
	}))
	defer server.Close()
	cmd := New(testDeps(server))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"get", testSession, "--json"})
	require.NoError(t, cmd.Execute())
	assert.NotContains(t, out.String(), "limits")
}

func TestRunExplainsLimitsRefusals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		code, hint string
	}{
		{"sandbox_limits_exceed_session", "Lower --vcpus or raise --memory: a 1024 MB session provides 2 vCPUs and a 2048 MB session 4"},
		{"sandbox_limits_unsupported", "The selected image predates session limits. Redeploy the template, or retry once the preset is updated"},
		{"", ""},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			body := map[string]any{"error": "refused by the API"}
			if tc.code != "" {
				body["code"] = tc.code
			}
			_ = json.NewEncoder(w).Encode(body)
		}))
		cmd := New(testDeps(server))
		cmd.SetArgs([]string{"run", "--preset", "node22", "--vcpus", "3"})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		err := cmd.Execute()
		server.Close()
		require.ErrorContains(t, err, "HTTP 400: refused by the API")
		assert.Equal(t, tc.code, api.Code(err), "the API's code stays the cause")
		assert.Equal(t, http.StatusBadRequest, api.Status(err))
		if tc.hint == "" {
			assert.Equal(t, "HTTP 400: refused by the API", err.Error())
		} else {
			assert.Contains(t, err.Error(), "\n"+tc.hint)
		}
	}
}

// readLimits returns the limits object of a session, or of the only session in
// a page, as JSON.
func readLimits(t *testing.T, output []byte) string {
	t.Helper()
	var value struct {
		Limits json.RawMessage   `json:"limits"`
		Data   []json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(output, &value), string(output))
	if value.Data != nil {
		require.Len(t, value.Data, 1)
		return readLimits(t, value.Data[0])
	}
	require.NotEmpty(t, value.Limits, string(output))
	return string(value.Limits)
}
