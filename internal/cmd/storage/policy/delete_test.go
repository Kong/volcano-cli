package policy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestDeleteByName(t *testing.T) {
	setPolicyCommandTestHome(t)
	savePolicyCommandTestConfig(t)

	var sawDelete bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == storagePoliciesPath:
			writePolicyJSON(t, w, http.StatusOK, []any{
				policyPayload("anon-read", "SELECT"),
			})
		case r.Method == http.MethodDelete && r.URL.Path == storagePoliciesPath+"/"+storagePolicyID:
			sawDelete = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executePolicyCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"delete", "uploads", "anon-read", "--yes",
	)
	require.NoError(t, err)
	assert.True(t, sawDelete)
	assert.Contains(t, out, "Policy 'anon-read' deleted from bucket 'uploads'")
}

func TestDeletePromptsWithBucketContext(t *testing.T) {
	setPolicyCommandTestHome(t)
	savePolicyCommandTestConfig(t)

	var sawDelete bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			sawDelete = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cmd := New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
	cmd.SetIn(strings.NewReader("no\n"))
	out, err := executePolicyCommand(t, cmd, "delete", "uploads", "anon-read")
	require.NoError(t, err)
	assert.False(t, sawDelete)
	assert.Contains(t, out, "You are about to delete a resource permanently")
	assert.Contains(t, out, "Delete storage policy 'anon-read on bucket uploads'?")
	assert.Contains(t, out, "Delete cancelled.")
}

func TestDeleteNotFound(t *testing.T) {
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
		"delete", "uploads", "missing", "--yes",
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `policy "missing" not found on bucket "uploads"`)
}
