package functions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestFunctionsUpdateVisibility(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	var updateBody map[string]bool
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
		case r.Method == http.MethodPatch && r.URL.Path == "/projects/"+functionProjectID+"/functions/"+functionID:
			require.NoError(t, json.NewDecoder(r.Body).Decode(&updateBody))
			payload := functionCommandPayload(functionID, "hello")
			payload["is_public"] = updateBody["is_public"]
			writeFunctionCommandJSON(t, w, http.StatusOK, payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "update", "hello")
	require.ErrorContains(t, err, "specify exactly one visibility flag")

	_, err = executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "update", "hello", "--public", "--private")
	require.ErrorContains(t, err, "specify exactly one visibility flag")

	out, err := executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "update", "hello", "--public")
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"is_public": true}, updateBody)
	assert.Contains(t, out, "Function 'hello' visibility set to public")

	out, err = executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "update", "hello", "--private")
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"is_public": false}, updateBody)
	assert.Contains(t, out, "Function 'hello' visibility set to private")
}
