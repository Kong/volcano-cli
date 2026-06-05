package policy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestCreate(t *testing.T) {
	setPolicyCommandTestHome(t)
	savePolicyCommandTestConfig(t)

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == storagePoliciesPath {
			require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
			writePolicyJSON(t, w, http.StatusCreated, policyPayload("anon-read", "SELECT"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executePolicyCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"create", "uploads",
		"--name", "anon-read",
		"--operation", "select",
		"--definition", "auth.uid() = owner_id",
	)
	require.NoError(t, err)
	assert.Contains(t, out, "Policy 'anon-read' created on bucket 'uploads'")

	require.NotNil(t, captured)
	assert.Equal(t, "anon-read", captured["name"])
	assert.Equal(t, "SELECT", captured["operation"])
	assert.Equal(t, "auth.uid() = owner_id", captured["definition"])
}

func TestCreateRejectsInvalidOperation(t *testing.T) {
	setPolicyCommandTestHome(t)
	savePolicyCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executePolicyCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"create", "uploads",
		"--name", "bad",
		"--operation", "READ",
		"--definition", "true",
	)
	require.Error(t, err)
	assert.Contains(t, out, `invalid operation "READ"`)
}
