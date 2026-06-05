package policy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestGetByName(t *testing.T) {
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

	out, err := executePolicyCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"get", "uploads", "anon-read",
	)
	require.NoError(t, err)
	assert.Contains(t, out, "Bucket: uploads")
	assert.Contains(t, out, "Name: anon-read")
	assert.Contains(t, out, "Operation: SELECT")
}

func TestGetNotFound(t *testing.T) {
	setPolicyCommandTestHome(t)
	savePolicyCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == storagePoliciesPath {
			writePolicyJSON(t, w, http.StatusOK, []any{})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	_, err := executePolicyCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"get", "uploads", "missing",
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `policy "missing" not found on bucket "uploads"`)
}
