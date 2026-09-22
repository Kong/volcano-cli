package sandboxes

import (
	"bytes"
	"context"
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

	"github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/sandbox"
)

const projectID = "22222222-2222-4222-8222-222222222222"

func fixture(t *testing.T, handler http.HandlerFunc) cliruntime.Deps {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer platform-token", r.Header.Get("Authorization"))
		assert.Equal(t, sandbox.Version, r.Header.Get("X-Volcano-Sandbox-Version"))
		w.Header().Set("X-Volcano-Sandbox-Version", sandbox.Version)
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	t.Setenv("VOLCANO_SANDBOX_URL", server.URL)
	return cliruntime.Deps{HTTPClient: server.Client(), ConfigLoader: func() (*config.Config, error) {
		return &config.Config{UserToken: "platform-token", IgnoreEnv: true, CurrentProject: &config.ProjectConfig{ID: projectID}}, nil
	}}
}

func execute(t *testing.T, cmd *cobra.Command, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(t.Context())
	return stdout.String(), stderr.String(), err
}

func TestExecutionKnownExitAndNoReplay(t *testing.T) {
	hits := 0
	deps := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		assert.Equal(t, "/v1/projects/"+projectID+"/sandboxes/sb1/executions", r.URL.Path)
		assert.Equal(t, "3", r.Header.Get("X-Sandbox-Generation"))
		assert.Equal(t, "key-1", r.Header.Get("Idempotency-Key"))
		var body map[string]json.RawMessage
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.JSONEq(t, `{"command":"exit 42","working_directory":".","environment":{},"stdin":"","timeout_ms":60000}`, string(body["command"]))
		_, _ = io.WriteString(w, `{"result":{"stdout":"out","stderr":"err","state":"completed","exit_code":42,"operation_id":"op1"},"operation":null}`)
	})
	stdout, stderr, err := execute(t, New(deps), "exec", "sb1", "--generation", "3", "--command", "exit 42", "--idempotency-key", "key-1")
	var exit *ExitError
	require.ErrorAs(t, err, &exit)
	assert.Equal(t, 42, exit.ExitCode())
	assert.Equal(t, "out", stdout)
	assert.Contains(t, stderr, "err")
	assert.Equal(t, 1, hits)
}

func TestPendingExecutionNeverFabricatesExit(t *testing.T) {
	deps := fixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"result":null,"operation":{"id":"op1"}}`)
	})
	out, _, err := execute(t, New(deps), "exec", "sb1", "--generation", "1", "--command", "sleep 30", "--json")
	require.ErrorContains(t, err, "op1")
	assert.Contains(t, out, `"id":"op1"`)
	_, _, err = execute(t, New(deps), "exec", "sb1", "--generation", "1", "--command", "sleep 30", "--async")
	require.NoError(t, err)
}

func TestCreateAndOneShotUseImmutableTemplate(t *testing.T) {
	deps := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		if r.URL.Path == "/v1/projects/"+projectID+"/one-shot" {
			assert.Contains(t, string(body["sandbox"]), `"template_version":"1.2.3"`)
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"result":null,"operation":{"id":"op1"}}`)
			return
		}
		assert.JSONEq(t, `{"template_id":"node","template_version":"1.2.3","baseline":"1gib","dc":"dc1","lifetime_ms":600000}`, rawObject(t, body))
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"id":"op1","state":"pending"}`)
	})
	_, _, err := execute(t, New(deps), "create", "--template", "node", "--template-version", "1.2.3", "--dc", "dc1")
	require.NoError(t, err)
	_, _, err = execute(t, New(deps), "run", "--template", "node", "--template-version", "1.2.3", "--dc", "dc1", "--command", "node -v", "--async")
	require.NoError(t, err)
}

func rawObject(t *testing.T, value any) string {
	t.Helper()
	b, err := json.Marshal(value)
	require.NoError(t, err)
	return string(b)
}

func TestLifecycleNeverUpgradesGeneration(t *testing.T) {
	for _, generation := range []string{"1", "2"} {
		t.Run(generation, func(t *testing.T) {
			writes := 0
			deps := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					_, _ = io.WriteString(w, `{"ref":{"scope":{"owner_id":"owner","project_id":"`+projectID+`","project_generation":"7"},"sandbox_id":"sb1","generation":"1"}}`)
					return
				}
				writes++
				assert.Equal(t, "1", r.Header.Get("X-Sandbox-Generation"))
				var body map[string]json.RawMessage
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.Equal(t, `"1"`, string(body["generation"]))
				w.WriteHeader(http.StatusAccepted)
				_, _ = io.WriteString(w, `{"id":"op1"}`)
			})
			_, _, err := execute(t, New(deps), "terminate", "sb1", "--generation", generation)
			if generation == "1" {
				require.NoError(t, err)
				assert.Equal(t, 1, writes)
			} else {
				require.ErrorContains(t, err, "conflict")
				assert.Zero(t, writes)
			}
		})
	}
}

func TestUsagePreservesDecimalAndNull(t *testing.T) {
	deps := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "next", r.URL.Query().Get("cursor"))
		_, _ = io.WriteString(w, `{"items":[{"quantity":"9007199254740993"},{"quantity":null}],"next_cursor":"last"}`)
	})
	out, _, err := execute(t, New(deps), "usage", "--from", "2026-09-01T00:00:00Z", "--to", "2026-09-02T00:00:00Z", "--cursor", "next", "--json")
	require.NoError(t, err)
	assert.Contains(t, out, `"9007199254740993"`)
	assert.Contains(t, out, `null`)
	assert.Contains(t, out, `"next_cursor":"last"`)
}

func TestInputAndCapabilityFailuresDoNotDispatch(t *testing.T) {
	deps := fixture(t, func(_ http.ResponseWriter, _ *http.Request) { t.Error("unexpected request") })
	for _, args := range [][]string{{"exec", "sb1", "--command", "echo x"}, {"exec", "../s", "--generation", "1", "--command", "x"}, {"get", "../s"}, {"files", "read", "sb1", "../secret", "--generation", "1"}, {"shell", "sb1"}, {"templates", "get", "t1", ".."}, {"create"}, {"list", "--limit", "101"}} {
		_, _, err := execute(t, New(deps), args...)
		require.Error(t, err)
	}
	_, _, err := execute(t, NewLocal())
	require.ErrorContains(t, err, "capability_disabled")
}

func TestMissingAuthenticationAndCancellation(t *testing.T) {
	deps := cliruntime.Deps{ConfigLoader: func() (*config.Config, error) { return &config.Config{IgnoreEnv: true}, nil }}
	_, _, err := execute(t, New(deps), "list")
	require.Error(t, err)
	deps = fixture(t, func(_ http.ResponseWriter, _ *http.Request) { t.Error("cancelled request dispatched") })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cmd := New(deps)
	cmd.SetArgs([]string{"list"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	require.ErrorIs(t, cmd.ExecuteContext(ctx), context.Canceled)
}

func TestFileUploadAndReadsPreserveBoundsAndMetadata(t *testing.T) {
	file := filepath.Join(t.TempDir(), "input.bin")
	require.NoError(t, os.WriteFile(file, []byte{0, 1, 2, 255}, 0o600))
	deps := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "4", r.Header.Get("X-Sandbox-Generation"))
		assert.Equal(t, "/v1/projects/"+projectID+"/sandboxes/sb1/files", r.URL.Path)
		if r.Method == http.MethodPut {
			var body struct {
				Path    string `json:"path"`
				Content []byte `json:"content"`
				Digest  string `json:"expected_digest"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, []byte{0, 1, 2, 255}, body.Content)
			assert.Equal(t, "main.bin", body.Path)
			assert.Equal(t, "expected", body.Digest)
			_, _ = io.WriteString(w, `{"path":"main.bin","size_bytes":"4"}`)
			return
		}
		assert.Equal(t, "12", r.URL.Query().Get("offset"))
		_, _ = io.WriteString(w, `{"entry":{"path":"main.bin"},"content":"AAEC/w==","next_offset":16,"eof":false}`)
	})
	_, _, err := execute(t, New(deps), "files", "write", "sb1", "main.bin", "--generation", "4", "--source", file, "--expected-digest", "expected")
	require.NoError(t, err)
	out, _, err := execute(t, New(deps), "files", "read", "sb1", "main.bin", "--generation", "4", "--offset", "12", "--json")
	require.NoError(t, err)
	assert.Contains(t, out, `"next_offset":16`)
	assert.Contains(t, out, `"eof":false`)
}

func TestAllLeafCommandsProvideHelp(t *testing.T) {
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		if len(cmd.Commands()) == 0 {
			assert.NotEmpty(t, cmd.Short, cmd.CommandPath())
			assert.NotNil(t, cmd.RunE, cmd.CommandPath())
			return
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(New(cliruntime.Deps{}))
}

func TestFrozenPublicResourceRoutes(t *testing.T) {
	for _, tc := range []struct {
		args         []string
		method, path string
	}{
		{[]string{"capabilities"}, http.MethodGet, "/capabilities"},
		{[]string{"list"}, http.MethodGet, "/sandboxes"},
		{[]string{"get", "sb1"}, http.MethodGet, "/sandboxes/sb1"},
		{[]string{"templates", "list"}, http.MethodGet, "/templates"},
		{[]string{"templates", "get", "t1", "1.2.3"}, http.MethodGet, "/templates/t1/versions/1.2.3"},
		{[]string{"templates", "create", "--data", `{"name":"test","baseline":"1gib","language":"node"}`}, http.MethodPost, "/templates"},
		{[]string{"templates", "delete", "t1", "v1"}, http.MethodDelete, "/templates/t1/versions/v1"},
		{[]string{"templates", "deploy", "t1", "--data", `{"template_version":"v1","dc":"dc1","source_artifact_id":"a1","source_digest":"digest"}`}, http.MethodPost, "/templates/t1/deployments"},
		{[]string{"templates", "prepare-source", "t1", "--data", `{"size_bytes":"128","sha256":"digest"}`}, http.MethodPost, "/templates/t1/source-upload"},
		{[]string{"deployments", "list"}, http.MethodGet, "/deployments"},
		{[]string{"deployments", "get", "d1"}, http.MethodGet, "/deployments/d1"},
		{[]string{"deployments", "cancel", "d1"}, http.MethodPost, "/deployments/d1/cancel"},
		{[]string{"executions", "list", "sb1"}, http.MethodGet, "/sandboxes/sb1/executions"},
		{[]string{"executions", "get", "sb1", "e1"}, http.MethodGet, "/sandboxes/sb1/executions/e1"},
		{[]string{"operations", "get", "o1"}, http.MethodGet, "/operations/o1"},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			deps := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tc.method, r.Method)
				assert.Equal(t, "/v1/projects/"+projectID+tc.path, r.URL.Path)
				if tc.method != http.MethodGet {
					assert.NotEmpty(t, r.Header.Get("Idempotency-Key"))
				}
				_, _ = io.WriteString(w, `{}`)
			})
			_, _, err := execute(t, New(deps), tc.args...)
			require.NoError(t, err)
		})
	}
}
