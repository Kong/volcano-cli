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

func TestFunctionsRegionsListsTheCatalogWithoutAProject(t *testing.T) {
	setFunctionCommandTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/functions/regions", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode([]any{
			map[string]any{"code": "us-east-1", "label": "US East (N. Virginia)", "flag": "🇺🇸"},
			map[string]any{"code": "eu-central-1", "label": "Europe (Frankfurt)", "flag": "🇩🇪"},
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()
	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}

	out, err := executeFunctionsCommand(t, New(deps), "regions")
	require.NoError(t, err)
	assert.Contains(t, out, "us-east-1")
	assert.Contains(t, out, "Europe (Frankfurt)")

	out, err = executeFunctionsCommand(t, NewLocal(deps), "regions", "--json")
	require.NoError(t, err)
	var regions []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &regions))
	require.Len(t, regions, 2)
	assert.Equal(t, "eu-central-1", regions[1]["code"])
}
