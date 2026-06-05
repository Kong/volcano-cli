package functions

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestFunctionsDeletePromptAndYes(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		setFunctionCommandTestHome(t)
		saveFunctionCommandTestConfig(t)
		var sawDelete bool
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/projects/"+functionProjectID+"/functions":
				writeFunctionCommandJSON(t, w, http.StatusOK, map[string]any{
					"data":     []any{functionCommandPayload(functionID, "hello")},
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
		out, err := executeFunctionsCommand(t, cmd, "delete", "hello")
		require.NoError(t, err)
		assert.False(t, sawDelete)
		assert.Contains(t, out, "You are about to delete a resource permanently")
		assert.Contains(t, out, "Delete function 'hello'?")
		assert.Contains(t, out, "Delete cancelled.")
	})

	t.Run("yes", func(t *testing.T) {
		setFunctionCommandTestHome(t)
		saveFunctionCommandTestConfig(t)
		var sawDelete bool
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/projects/"+functionProjectID+"/functions":
				writeFunctionCommandJSON(t, w, http.StatusOK, map[string]any{
					"data":     []any{functionCommandPayload(functionID, "hello")},
					"has_more": false,
					"page":     1,
					"limit":    100,
					"total":    1,
				})
			case r.Method == http.MethodDelete && r.URL.Path == "/projects/"+functionProjectID+"/functions/"+functionID:
				sawDelete = true
				w.WriteHeader(http.StatusAccepted)
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		out, err := executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "delete", functionID, "--yes")
		require.NoError(t, err)
		assert.True(t, sawDelete)
		assert.Contains(t, out, "Function 'hello' deletion started")
		assert.Contains(t, out, "Status will be \"deleting\"")
	})
}
