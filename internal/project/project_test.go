package project

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	projectAlphaID = "11111111-1111-4111-8111-111111111111"
	projectBetaID  = "22222222-2222-4222-8222-222222222222"
)

func TestListReturnsRequestedPage(t *testing.T) {
	setProjectTestHome(t)
	saveProjectTestConfig(t, &config.Config{UserToken: "token"})
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		writeProjectJSON(t, w, http.StatusOK, map[string]any{
			"data":     []any{projectTestPayload(projectBetaID, "Beta", "active")},
			"has_more": false,
			"page":     2,
			"limit":    25,
			"total":    2,
		})
	}))
	defer server.Close()

	_, projects, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).List(context.Background(), 2, 25)
	require.NoError(t, err)
	assert.Equal(t, []string{"page=2&limit=25"}, queries)
	require.Len(t, projects.Data, 1)
	assert.Equal(t, "Beta", projects.Data[0].Name)
	assert.Equal(t, 2, projects.Page)
	assert.Equal(t, 25, projects.Limit)
}

func TestCreateReturnsProject(t *testing.T) {
	setProjectTestHome(t)
	saveProjectTestConfig(t, &config.Config{UserToken: "token"})
	var requestPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/projects", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&requestPayload))
		writeProjectJSON(t, w, http.StatusCreated, projectTestPayload(projectAlphaID, "Alpha", "provisioning"))
	}))
	defer server.Close()

	project, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).Create(context.Background(), " Alpha ")
	require.NoError(t, err)
	assert.Equal(t, projectAlphaID, project.Id.String())
	assert.Equal(t, "Alpha", project.Name)
	assert.Equal(t, map[string]any{"name": "Alpha"}, requestPayload)
}

func TestCreateWrapsAPIError(t *testing.T) {
	setProjectTestHome(t)
	saveProjectTestConfig(t, &config.Config{UserToken: "token"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeProjectJSON(t, w, http.StatusBadRequest, map[string]string{"error": "name already exists"})
	}))
	defer server.Close()

	_, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).Create(context.Background(), "Alpha")
	require.ErrorContains(t, err, `failed to create project "Alpha"`)
	require.ErrorContains(t, err, "name already exists")
}

func TestUseByNameByIDAndNotFound(t *testing.T) {
	setProjectTestHome(t)
	saveProjectTestConfig(t, &config.Config{UserToken: "token"})
	var listRequests int
	var getRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/projects":
			listRequests++
			writeProjectJSON(t, w, http.StatusOK, map[string]any{
				"data": []any{
					projectTestPayload(projectAlphaID, "Alpha", "active"),
					projectTestPayload(projectBetaID, "Beta", "active"),
				},
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    2,
			})
		case "/projects/" + projectAlphaID:
			getRequests++
			writeProjectJSON(t, w, http.StatusOK, projectTestPayload(projectAlphaID, "Alpha", "active"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	service := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})

	selected, err := service.Use(context.Background(), "Beta")
	require.NoError(t, err)
	assert.Equal(t, projectBetaID, selected.Id.String())
	assert.Equal(t, 1, listRequests)
	assertCurrentProject(t, projectBetaID, "Beta")

	selected, err = service.Use(context.Background(), projectAlphaID)
	require.NoError(t, err)
	assert.Equal(t, projectAlphaID, selected.Id.String())
	assert.Equal(t, 1, getRequests)
	assert.Equal(t, 1, listRequests, "successful ID lookup should not list projects")
	assertCurrentProject(t, projectAlphaID, "Alpha")

	_, err = service.Use(context.Background(), "Missing")
	require.ErrorContains(t, err, "project not found: Missing")
	assert.Equal(t, 2, listRequests)
}

func TestUseByNameScansPagesWithoutCollectingAllProjects(t *testing.T) {
	setProjectTestHome(t)
	saveProjectTestConfig(t, &config.Config{UserToken: "token"})
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		switch r.URL.Query().Get("page") {
		case "1":
			writeProjectJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{projectTestPayload(projectAlphaID, "Alpha", "active")},
				"has_more": true,
				"page":     1,
				"limit":    100,
				"total":    2,
			})
		case "2":
			writeProjectJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{projectTestPayload(projectBetaID, "Beta", "active")},
				"has_more": false,
				"page":     2,
				"limit":    100,
				"total":    2,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	selected, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).Use(context.Background(), "Beta")
	require.NoError(t, err)
	assert.Equal(t, projectBetaID, selected.Id.String())
	assert.Equal(t, []string{"page=1&limit=100", "page=2&limit=100"}, queries)
	assertCurrentProject(t, projectBetaID, "Beta")
}

func TestUseByNameErrorsWhenPaginationDoesNotAdvance(t *testing.T) {
	setProjectTestHome(t)
	saveProjectTestConfig(t, &config.Config{UserToken: "token"})
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		writeProjectJSON(t, w, http.StatusOK, map[string]any{
			"data":     []any{},
			"has_more": true,
			"page":     1,
			"limit":    100,
			"total":    1,
		})
	}))
	defer server.Close()

	_, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).Use(context.Background(), "Missing")
	require.ErrorContains(t, err, "project pagination did not advance at page 1")
	assert.Equal(t, 1, requests)
}

func TestUseFallsBackToUUIDShapedNameAfterIDNotFound(t *testing.T) {
	setProjectTestHome(t)
	saveProjectTestConfig(t, &config.Config{UserToken: "token"})
	uuidShapedName := "33333333-3333-4333-8333-333333333333"
	var getRequests int
	var listRequests int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/projects/" + uuidShapedName:
			getRequests++
			writeProjectJSON(t, w, http.StatusNotFound, map[string]string{"error": "missing"})
		case "/projects":
			listRequests++
			writeProjectJSON(t, w, http.StatusOK, map[string]any{
				"data": []any{
					projectTestPayload(projectBetaID, uuidShapedName, "active"),
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

	selected, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).Use(context.Background(), uuidShapedName)
	require.NoError(t, err)
	assert.Equal(t, projectBetaID, selected.Id.String())
	assert.Equal(t, uuidShapedName, selected.Name)
	assert.Equal(t, 1, getRequests)
	assert.Equal(t, 1, listRequests)
	assertCurrentProject(t, projectBetaID, uuidShapedName)
}

func TestUsePrefersIDOverSameStringName(t *testing.T) {
	setProjectTestHome(t)
	saveProjectTestConfig(t, &config.Config{UserToken: "token"})
	var listRequests int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/projects/" + projectAlphaID:
			writeProjectJSON(t, w, http.StatusOK, projectTestPayload(projectAlphaID, "Alpha", "active"))
		case "/projects":
			listRequests++
			writeProjectJSON(t, w, http.StatusOK, map[string]any{
				"data": []any{
					projectTestPayload(projectBetaID, projectAlphaID, "active"),
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

	selected, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).Use(context.Background(), projectAlphaID)
	require.NoError(t, err)
	assert.Equal(t, projectAlphaID, selected.Id.String())
	assert.Equal(t, "Alpha", selected.Name)
	assert.Equal(t, 0, listRequests)
	assertCurrentProject(t, projectAlphaID, "Alpha")
}

func setProjectTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
}

func saveProjectTestConfig(t *testing.T, cfg *config.Config) {
	t.Helper()
	require.NoError(t, cfg.Save())
}

func assertCurrentProject(t *testing.T, id, name string) {
	t.Helper()
	cfg, err := config.Load()
	require.NoError(t, err)
	require.NotNil(t, cfg.CurrentProject)
	assert.Equal(t, id, cfg.CurrentProject.ID)
	assert.Equal(t, name, cfg.CurrentProject.Name)
}

func writeProjectJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func projectTestPayload(id, name, status string) map[string]any {
	return map[string]any{
		"id":               id,
		"name":             name,
		"status":           status,
		"all_regions":      true,
		"selected_regions": []string{},
		"created_at":       time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
		"updated_at":       time.Now().Add(-30 * time.Minute).UTC().Format(time.RFC3339),
	}
}
