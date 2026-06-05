package frontends

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestFrontendsDeletePromptAndYes(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		setFrontendCommandTestHome(t)
		saveFrontendCommandTestConfig(t)
		var sawDelete bool
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
			case r.Method == http.MethodDelete:
				sawDelete = true
				w.WriteHeader(http.StatusAccepted)
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		cmd := New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
		cmd.SetIn(strings.NewReader("no\n"))
		out, err := executeFrontendsCommand(t, cmd, "delete", "web")
		require.NoError(t, err)
		assert.False(t, sawDelete)
		assert.Contains(t, out, "You are about to delete a resource permanently")
		assert.Contains(t, out, "Delete frontend 'web'?")
		assert.Contains(t, out, "Delete cancelled.")
	})

	t.Run("yes", func(t *testing.T) {
		setFrontendCommandTestHome(t)
		saveFrontendCommandTestConfig(t)
		var sawDelete bool
		var sawList bool
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends":
				sawList = true
				writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{
					"data":     []any{frontendCommandPayload(frontendID, "web")},
					"has_more": false,
					"page":     1,
					"limit":    100,
					"total":    1,
				})
			case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends/"+frontendID:
				writeFrontendCommandJSON(t, w, http.StatusOK, frontendCommandPayload(frontendID, "web"))
			case r.Method == http.MethodDelete && r.URL.Path == "/projects/"+frontendProjectID+"/frontends/"+frontendID:
				sawDelete = true
				w.WriteHeader(http.StatusAccepted)
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		out, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "delete", frontendID, "--yes")
		require.NoError(t, err)
		assert.True(t, sawDelete)
		assert.False(t, sawList, "UUID identifier should skip the list endpoint")
		assert.Contains(t, out, "Frontend 'web' deletion started")
		assert.Contains(t, out, "Status will be \"deleting\"")
	})
}
