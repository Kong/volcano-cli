package frontends

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestFrontendsGetAcceptsNameOrID(t *testing.T) {
	for _, target := range []string{"web", frontendID} {
		t.Run(target, func(t *testing.T) {
			setFrontendCommandTestHome(t)
			saveFrontendCommandTestConfig(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends":
					writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{
						"data":     []any{frontendCommandPayload(frontendID, "web")},
						"has_more": false,
						"page":     1,
						"limit":    100,
						"total":    1,
					})
				case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends/"+frontendID:
					writeFrontendCommandJSON(t, w, http.StatusOK, frontendCommandPayload(frontendID, "web"))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			out, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "get", target)
			require.NoError(t, err)
			assert.Contains(t, out, "ID: "+frontendID)
			assert.Contains(t, out, "Name: web")
			assert.Contains(t, out, "Framework: nextjs")
			assert.Contains(t, out, "Status: ready")
			assert.Contains(t, out, "App Root: apps/web")
			assert.Contains(t, out, "Site URL: https://web.frontends.volcano.dev/")
			assert.Contains(t, out, "Current Deployment: "+frontendDeploymentID)
		})
	}
}

func TestFrontendsGetNotFound(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{
			"data":     []any{frontendCommandPayload(frontendID, "web")},
			"has_more": false,
			"page":     1,
			"limit":    100,
			"total":    1,
		})
	}))
	defer server.Close()

	_, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "get", "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `frontend "missing" not found`)
}
