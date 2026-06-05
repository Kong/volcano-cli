package functions

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestFunctionsGetAcceptsNamePathOrID(t *testing.T) {
	for _, target := range []string{
		"hello",
		filepath.Join("volcano", "functions", "hello.js"),
		functionID,
	} {
		t.Run(target, func(t *testing.T) {
			setFunctionCommandTestHome(t)
			saveFunctionCommandTestConfig(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/projects/"+functionProjectID+"/functions":
					writeFunctionCommandJSON(t, w, http.StatusOK, map[string]any{
						"data":     []any{functionCommandPayload(functionID, "hello")},
						"has_more": false,
						"page":     1,
						"limit":    100,
						"total":    1,
					})
				case r.Method == http.MethodGet && r.URL.Path == "/projects/"+functionProjectID+"/functions/"+functionID:
					writeFunctionCommandJSON(t, w, http.StatusOK, functionCommandPayload(functionID, "hello"))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			out, err := executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "get", target)
			require.NoError(t, err)
			assert.Contains(t, out, "ID: "+functionID)
			assert.Contains(t, out, "Name: hello")
			assert.Contains(t, out, "Runtime: nodejs24.x")
			assert.Contains(t, out, "Handler: handler")
			assert.Contains(t, out, "Visibility: public")
			assert.Contains(t, out, "Invoke URL: https://"+functionID+".functions.volcano.dev/")
		})
	}
}
