package functions

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestFunctionsRuntimesDoesNotRequireProject(t *testing.T) {
	setFunctionCommandTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/functions/runtimes", r.URL.Path)
		writeFunctionCommandJSON(t, w, http.StatusOK, map[string]any{
			"runtimes": []any{
				functionRuntimeCommandPayload("nodejs24.x", "nodejs", true, []string{".js", ".mjs"}, "index.js", "handler", []string{"package.json"}),
				functionRuntimeCommandPayload("python3.12", "python", false, []string{".py"}, "main.py", "handler", []string{"requirements.txt"}),
			},
		})
	}))
	defer server.Close()

	out, err := executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "runtimes")
	require.NoError(t, err)
	assert.Contains(t, out, "Runtime")
	assert.Contains(t, out, "nodejs24.x")
	assert.Contains(t, out, "yes")
	assert.Contains(t, out, "python3.12")
}
