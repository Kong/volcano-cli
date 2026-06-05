package storage

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const storageProjectID = "33333333-3333-4333-8333-333333333333"

func TestStats(t *testing.T) {
	setStatsTestHome(t)
	saveStatsTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/projects/"+storageProjectID+"/storage/stats" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"bucket_count": 2,
				"object_count": 17,
				"total_size":   1048576,
			}))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cmd := New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"stats"})
	require.NoError(t, cmd.Execute())

	assert.Contains(t, out.String(), "Buckets: 2")
	assert.Contains(t, out.String(), "Objects: 17")
	assert.Contains(t, out.String(), "Total size: 1.0 MiB")
}

func setStatsTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
}

func saveStatsTestConfig(t *testing.T) {
	t.Helper()
	cfg := &cliconfig.Config{
		UserToken: "token",
		CurrentProject: &cliconfig.ProjectConfig{
			ID:   storageProjectID,
			Name: "Gamma",
		},
	}
	require.NoError(t, cfg.Save())
}
