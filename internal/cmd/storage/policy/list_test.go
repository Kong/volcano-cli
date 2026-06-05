package policy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestList(t *testing.T) {
	setPolicyCommandTestHome(t)
	savePolicyCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == storagePoliciesPath {
			writePolicyJSON(t, w, http.StatusOK, []any{
				policyPayload("anon-read", "SELECT"),
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executePolicyCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "list", "uploads")
	require.NoError(t, err)
	assert.Contains(t, out, "anon-read")
	assert.Contains(t, out, "SELECT")
	assert.Contains(t, out, "Total: 1 policy(ies) on bucket 'uploads'")
}
