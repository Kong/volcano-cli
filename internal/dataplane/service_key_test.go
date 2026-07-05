package dataplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const testProjectID = "33333333-3333-4333-8333-333333333333"

func TestServiceKeyReturnsExistingCLIKey(t *testing.T) {
	setServiceKeyTestHome(t)
	saveServiceKeyTestConfig(t)

	var listHits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer platform-token", r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+testProjectID+"/service-keys":
			listHits++
			assert.Equal(t, "1", r.URL.Query().Get("page"))
			assert.Equal(t, "100", r.URL.Query().Get("limit"))
			writeServiceKeyJSON(t, w, http.StatusOK, map[string]any{
				"data": []any{
					serviceKeyPayload("11111111-1111-4111-8111-111111111111", CLIServiceKeyName, "sk-existing"),
				},
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
	key, err := service.ServiceKey(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "sk-existing", key)
	assert.Equal(t, 1, listHits)
}

func TestServiceKeyCreatesMissingCLIKey(t *testing.T) {
	setServiceKeyTestHome(t)
	saveServiceKeyTestConfig(t)

	var createBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer platform-token", r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+testProjectID+"/service-keys":
			writeServiceKeyJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{},
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    0,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+testProjectID+"/service-keys":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&createBody))
			writeServiceKeyJSON(t, w, http.StatusCreated, serviceKeyPayload("22222222-2222-4222-8222-222222222222", CLIServiceKeyName, "sk-created"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
	key, err := service.ServiceKey(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "sk-created", key)
	// Derive the expected scope from the source of truth so the two cannot drift.
	expectedPermissions := make([]any, len(cliDataPlanePermissions))
	for i, p := range cliDataPlanePermissions {
		expectedPermissions[i] = p
	}
	assert.Equal(t, map[string]any{
		"name":        CLIServiceKeyName,
		"permissions": expectedPermissions,
	}, createBody)
}

func TestServiceKeyReloadsAfterCreateConflict(t *testing.T) {
	setServiceKeyTestHome(t)
	saveServiceKeyTestConfig(t)

	listHits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer platform-token", r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+testProjectID+"/service-keys":
			listHits++
			data := []any{}
			if listHits > 1 {
				data = append(data, serviceKeyPayload("33333333-3333-4333-8333-333333333333", CLIServiceKeyName, "sk-raced"))
			}
			writeServiceKeyJSON(t, w, http.StatusOK, map[string]any{
				"data":     data,
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    len(data),
			})
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+testProjectID+"/service-keys":
			writeServiceKeyJSON(t, w, http.StatusConflict, map[string]any{"error": "already exists"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
	key, err := service.ServiceKey(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "sk-raced", key)
	assert.Equal(t, 2, listHits)
}

func TestServiceKeyUsesConfiguredServiceKeyDirectly(t *testing.T) {
	setServiceKeyTestHome(t)
	saveServiceKeyTestConfig(t)
	// The caller already holds a scoped data-plane service key (e.g. in CI via
	// VOLCANO_TOKEN); the CLI must use it as-is and never attempt the reserved-key
	// list/create, which a service key cannot perform against control-plane routes.
	t.Setenv("VOLCANO_TOKEN", "sk-provided-data-plane-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected API call %s %s: a provided service key must be used without a control-plane lookup", r.Method, r.URL.Path)
		http.Error(w, "unexpected control-plane call", http.StatusUnauthorized)
	}))
	defer server.Close()

	service := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
	key, err := service.ServiceKey(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "sk-provided-data-plane-key", key)
}

func setServiceKeyTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
}

func saveServiceKeyTestConfig(t *testing.T) {
	t.Helper()
	cfg := &cliconfig.Config{
		UserToken: "platform-token",
		CurrentProject: &cliconfig.ProjectConfig{
			ID:   testProjectID,
			Name: "Gamma",
		},
	}
	require.NoError(t, cfg.Save())
}

func writeServiceKeyJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func serviceKeyPayload(id, name, keyValue string) map[string]any {
	return map[string]any{
		"id":          id,
		"project_id":  testProjectID,
		"name":        name,
		"key_prefix":  keyValue[:min(len(keyValue), 12)],
		"key_value":   keyValue,
		"permissions": []string{"*"},
		"created_at":  "2026-05-20T00:00:00Z",
		"updated_at":  "2026-05-20T00:00:00Z",
	}
}
