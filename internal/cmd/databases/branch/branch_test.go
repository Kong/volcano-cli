package branch

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestListRendersBranches(t *testing.T) {
	setBranchCommandTestHome(t)
	saveBranchCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == branchesPath {
			writeBranchJSON(t, w, http.StatusOK, map[string]any{
				"data": []any{branchPayload("feature-x", "active")},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeBranchCommand(t, newTestCommand(server), "list", "app")
	require.NoError(t, err)
	assert.Contains(t, out, "feature-x")
	assert.Contains(t, out, "active")
	assert.Contains(t, out, "Showing 1 branch(es) of database 'app'")
}

func TestListReportsNoBranches(t *testing.T) {
	setBranchCommandTestHome(t)
	saveBranchCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeBranchJSON(t, w, http.StatusOK, map[string]any{"data": []any{}})
	}))
	defer server.Close()

	out, err := executeBranchCommand(t, newTestCommand(server), "list", "app")
	require.NoError(t, err)
	assert.Contains(t, out, "No branches of database 'app'")
}

func TestCreateSendsTTLAndReportsProvisioning(t *testing.T) {
	setBranchCommandTestHome(t)
	saveBranchCommandTestConfig(t)

	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == branchesPath {
			raw, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(raw, &body))
			writeBranchJSON(t, w, http.StatusAccepted, branchPayload("feature-x", "provisioning"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeBranchCommand(t, newTestCommand(server), "create", "app", "feature-x", "--ttl", "24h")
	require.NoError(t, err)
	assert.Equal(t, "feature-x", body["name"])
	assert.InDelta(t, float64(86400), body["ttl_seconds"], 0)
	assert.Contains(t, out, "Branch 'feature-x' of database 'app' created")
	assert.Contains(t, out, "provisioning")
}

func TestCreateOmitsTTLWhenFlagUnset(t *testing.T) {
	setBranchCommandTestHome(t)
	saveBranchCommandTestConfig(t)

	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &body))
		writeBranchJSON(t, w, http.StatusAccepted, branchPayload("feature-x", "provisioning"))
	}))
	defer server.Close()

	_, err := executeBranchCommand(t, newTestCommand(server), "create", "app", "feature-x")
	require.NoError(t, err)
	assert.NotContains(t, body, "ttl_seconds")
}

func TestCreateSurfacesBranchCapError(t *testing.T) {
	setBranchCommandTestHome(t)
	saveBranchCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeBranchJSON(t, w, http.StatusForbidden, map[string]any{
			"error": "database has reached its branch limit (max 10 per database)",
		})
	}))
	defer server.Close()

	_, err := executeBranchCommand(t, newTestCommand(server), "create", "app", "feature-x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max 10 per database")
}

func TestGetHidesConnectionStringByDefault(t *testing.T) {
	setBranchCommandTestHome(t)
	saveBranchCommandTestConfig(t)

	payload := branchPayload("feature-x", "active")
	payload["connection_string"] = "postgresql://branch:secret@host/db"
	payload["storage_bytes"] = 2048

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == branchPath {
			writeBranchJSON(t, w, http.StatusOK, payload)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	hidden, err := executeBranchCommand(t, newTestCommand(server), "get", "app", "feature-x")
	require.NoError(t, err)
	assert.NotContains(t, hidden, "secret")
	assert.Contains(t, hidden, "2.0 KiB")

	shown, err := executeBranchCommand(t, newTestCommand(server), "get", "app", "feature-x", "--show-connection-string")
	require.NoError(t, err)
	assert.Contains(t, shown, "postgresql://branch:secret@host/db")
}

func TestGetReportsUnsampledStorage(t *testing.T) {
	setBranchCommandTestHome(t)
	saveBranchCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeBranchJSON(t, w, http.StatusOK, branchPayload("feature-x", "provisioning"))
	}))
	defer server.Close()

	out, err := executeBranchCommand(t, newTestCommand(server), "get", "app", "feature-x")
	require.NoError(t, err)
	assert.Contains(t, out, "Storage: -")
}

func TestExtendSendsNewTTL(t *testing.T) {
	setBranchCommandTestHome(t)
	saveBranchCommandTestConfig(t)

	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && r.URL.Path == branchPath {
			raw, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(raw, &body))
			writeBranchJSON(t, w, http.StatusOK, branchPayload("feature-x", "active"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeBranchCommand(t, newTestCommand(server), "extend", "app", "feature-x", "--ttl", "72h")
	require.NoError(t, err)
	assert.InDelta(t, float64(259200), body["ttl_seconds"], 0)
	assert.Contains(t, out, "Branch 'feature-x' now expires")
}

func TestExtendRequiresTTL(t *testing.T) {
	setBranchCommandTestHome(t)
	saveBranchCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the API should not be called without a ttl")
	}))
	defer server.Close()

	_, err := executeBranchCommand(t, newTestCommand(server), "extend", "app", "feature-x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ttl")
}

func TestResetRequiresConfirmation(t *testing.T) {
	setBranchCommandTestHome(t)
	saveBranchCommandTestConfig(t)

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		writeBranchJSON(t, w, http.StatusOK, branchPayload("feature-x", "active"))
	}))
	defer server.Close()

	out, err := executeBranchCommand(t, newTestCommand(server), "reset", "app", "feature-x")
	require.NoError(t, err)
	assert.False(t, called, "reset should not call the API when the prompt is declined")
	assert.Contains(t, out, "Cancelled.")
}

func TestResetProceedsWithYes(t *testing.T) {
	setBranchCommandTestHome(t)
	saveBranchCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == branchPath+"/reset" {
			writeBranchJSON(t, w, http.StatusOK, branchPayload("feature-x", "active"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeBranchCommand(t, newTestCommand(server), "reset", "app", "feature-x", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "Branch 'feature-x' reset to database 'app'")
}

func TestRotatePasswordShowsNewConnectionStringOnRequest(t *testing.T) {
	setBranchCommandTestHome(t)
	saveBranchCommandTestConfig(t)

	payload := branchPayload("feature-x", "active")
	payload["connection_string"] = "postgresql://branch:s3cr3t@host/db"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == branchPath+"/reset-password" {
			writeBranchJSON(t, w, http.StatusOK, payload)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	hidden, err := executeBranchCommand(t, newTestCommand(server), "rotate-password", "app", "feature-x", "--yes")
	require.NoError(t, err)
	assert.NotContains(t, hidden, "s3cr3t")

	shown, err := executeBranchCommand(t, newTestCommand(server), "rotate-password", "app", "feature-x", "--yes", "--show-connection-string")
	require.NoError(t, err)
	assert.Contains(t, shown, "postgresql://branch:s3cr3t@host/db")
}

func TestDeleteRequiresConfirmation(t *testing.T) {
	setBranchCommandTestHome(t)
	saveBranchCommandTestConfig(t)

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	out, err := executeBranchCommand(t, newTestCommand(server), "delete", "app", "feature-x")
	require.NoError(t, err)
	assert.False(t, called, "delete should not call the API when the prompt is declined")
	assert.Contains(t, out, "Delete cancelled.")
}

func TestDeleteProceedsWithYes(t *testing.T) {
	setBranchCommandTestHome(t)
	saveBranchCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == branchPath {
			writeBranchJSON(t, w, http.StatusAccepted, map[string]any{"status": "deleting"})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeBranchCommand(t, newTestCommand(server), "delete", "app", "feature-x", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "Branch 'feature-x' of database 'app' deleted")
}

func newTestCommand(server *httptest.Server) *cobra.Command {
	return New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
}
