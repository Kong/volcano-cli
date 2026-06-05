package frontends

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestFrontendsRedeployByName(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	var sawRedeploy bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends":
			writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{frontendCommandPayload(frontendID, "web")},
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    1,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+frontendProjectID+"/frontends/"+frontendID+"/redeploy":
			sawRedeploy = true
			writeFrontendCommandJSON(t, w, http.StatusOK, frontendCommandPayload(frontendID, "web"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "redeploy", "web")
	require.NoError(t, err)
	assert.True(t, sawRedeploy)
	assert.Contains(t, out, "Frontend 'web' redeploy started")
	assert.Contains(t, out, "Deployment: "+frontendDeploymentID)
}
