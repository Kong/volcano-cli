package deployments

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	projectID   = "22222222-2222-4222-8222-222222222222"
	feedPath    = "/projects/" + projectID + "/deployments"
	summaryPath = feedPath + "/summary"
	checkoutID  = "33333333-3333-4333-8333-333333333333"
)

// deploymentsAPI is a fake feed that serves deployments in pages of the size
// asked for, and records every query.
type deploymentsAPI struct {
	t           *testing.T
	deployments []map[string]any
	summary     map[string]any
	// onRequest runs after each request is recorded.
	onRequest func()

	mu      sync.Mutex
	queries []url.Values
}

func newDeploymentsAPI(t *testing.T, count int) *deploymentsAPI {
	t.Helper()
	api := &deploymentsAPI{t: t}
	for i := range count {
		api.deployments = append(api.deployments, deploymentPayload(i, "function", "standard", "checkout", "active"))
	}
	return api
}

func deploymentPayload(i int, resourceType, kind, name, status string) map[string]any {
	resource := map[string]any{"type": resourceType, "id": checkoutID, "name": name}
	if kind != "" {
		resource["kind"] = kind
	}
	return map[string]any{
		"id":            "aaaaaaaa-aaaa-4aaa-8aaa-" + strings.Repeat("0", 12-len(strconv.Itoa(i))) + strconv.Itoa(i),
		"project_id":    projectID,
		"resource":      resource,
		"operation":     "deploy",
		"status":        status,
		"deploy_source": "cli",
		"created_at":    "2026-10-30T09:00:00Z",
		"completed_at":  "2026-10-30T09:01:32Z",
		"updated_at":    "2026-10-30T09:01:32Z",
	}
}

func (a *deploymentsAPI) serve() *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.queries = append(a.queries, r.URL.Query())
		a.mu.Unlock()
		if a.onRequest != nil {
			a.onRequest()
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == summaryPath:
			a.write(w, a.summary)
		case r.Method == http.MethodGet && r.URL.Path == feedPath:
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			start := min((page-1)*limit, len(a.deployments))
			end := min(start+limit, len(a.deployments))
			a.write(w, map[string]any{
				"data": a.deployments[start:end], "has_more": end < len(a.deployments),
				"page": page, "limit": limit, "total": len(a.deployments),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	a.t.Cleanup(server.Close)
	return server
}

func (a *deploymentsAPI) write(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		a.t.Errorf("encode response: %v", err)
	}
}

func (a *deploymentsAPI) sent() []url.Values {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]url.Values(nil), a.queries...)
}

func run(t *testing.T, server *httptest.Server, args ...string) (string, error) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
	require.NoError(t, (&cliconfig.Config{
		UserToken:      "token",
		CurrentProject: &cliconfig.ProjectConfig{ID: projectID, Name: "Beta"},
	}).Save())

	cmd := New(cliruntime.Deps{
		CommandPathPrefix: "volcano cloud",
		HTTPClient:        server.Client(),
		APIBaseURL:        server.URL,
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestListShowsTheNewestDeployments(t *testing.T) {
	api := newDeploymentsAPI(t, 3)
	api.deployments[1] = deploymentPayload(1, "function", "durable", "pipeline", "failed")
	api.deployments[2] = deploymentPayload(2, "frontend", "", "web", "active")

	out, err := run(t, api.serve(), "list")
	require.NoError(t, err)

	assert.Contains(t, out, "function checkout")
	assert.Contains(t, out, "durable function pipeline")
	assert.Contains(t, out, "frontend web")
	assert.Contains(t, out, "1m32s")
	assert.Contains(t, out, "Total: 3 deployment(s)")
	require.Len(t, api.sent(), 1)
	assert.Equal(t, "20", api.sent()[0].Get("limit"))
}

func TestListSendsTheFilters(t *testing.T) {
	api := newDeploymentsAPI(t, 1)

	_, err := run(t, api.serve(), "list",
		"--kind", "durable", "--name", "pipe", "--after", "2026-10-29T00:00:00Z")
	require.NoError(t, err)

	require.Len(t, api.sent(), 1)
	query := api.sent()[0]
	assert.Equal(t, "function", query.Get("resource_type"))
	assert.Equal(t, "durable", query.Get("function_kind"))
	assert.Equal(t, "pipe", query.Get("search"))
	assert.Equal(t, "2026-10-29T00:00:00Z", query.Get("created_after"))
}

// --limit bounds the listing, not one page, and the listing says when more
// match.
func TestListPagesToTheLimit(t *testing.T) {
	api := newDeploymentsAPI(t, 250)

	out, err := run(t, api.serve(), "list", "--limit", "150", "--json")
	require.NoError(t, err)

	var listed []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &listed))
	assert.Len(t, listed, 150)
	require.Len(t, api.sent(), 2)
	assert.Equal(t, "100", api.sent()[1].Get("limit"))
	assert.Equal(t, "2", api.sent()[1].Get("page"))

	out, err = run(t, api.serve(), "list", "--limit", "2")
	require.NoError(t, err)
	assert.Contains(t, out, "Showing the newest 2")
}

func TestListPrintsAnEmptyJSONArray(t *testing.T) {
	api := newDeploymentsAPI(t, 0)

	out, err := run(t, api.serve(), "list", "--json")
	require.NoError(t, err)
	assert.JSONEq(t, "[]", out)
}

func TestGetFindsADeploymentOnALaterPage(t *testing.T) {
	api := newDeploymentsAPI(t, 150)
	wanted := api.deployments[120]
	wanted["error_message"] = "build failed: npm ERR! missing script"
	wanted["status"] = "failed"
	wanted["progress"] = map[string]any{
		"started_at": "2026-10-30T09:00:00Z", "updated_at": "2026-10-30T09:01:32Z", "elapsed_seconds": 92,
		"phases": []any{
			map[string]any{"name": "queue", "status": "succeeded", "duration_seconds": 2},
			map[string]any{"name": "build", "status": "failed", "duration_seconds": 80},
		},
	}

	out, err := run(t, api.serve(), "get", wanted["id"].(string))
	require.NoError(t, err)

	assert.Contains(t, out, "Status: failed")
	assert.Contains(t, out, "Trigger: cli")
	assert.Contains(t, out, "Error: build failed: npm ERR! missing script")
	assert.Contains(t, out, "build           failed")
	assert.Contains(t, out, "volcano cloud functions logs checkout "+wanted["id"].(string)+" --type build")
	assert.Len(t, api.sent(), 2)
}

// Switching projects mid-search must not move the remaining pages to the
// other project.
func TestGetKeepsTheProjectItStartedWith(t *testing.T) {
	api := newDeploymentsAPI(t, 150)
	api.onRequest = func() {
		if err := (&cliconfig.Config{
			UserToken:      "token",
			CurrentProject: &cliconfig.ProjectConfig{ID: "44444444-4444-4444-8444-444444444444", Name: "Other"},
		}).Save(); err != nil {
			t.Errorf("switch project: %v", err)
		}
	}

	_, err := run(t, api.serve(), "get", api.deployments[120]["id"].(string))
	require.NoError(t, err)
	assert.Len(t, api.sent(), 2)
}

func TestGetNamesTheBuildLogsCommandForEachType(t *testing.T) {
	for _, tc := range []struct {
		resourceType, kind, name, want string
	}{
		{"frontend", "", "web", "volcano cloud frontends logs web "},
		{"function", "durable", "pipeline", ""},
	} {
		t.Run(tc.resourceType+tc.kind, func(t *testing.T) {
			api := newDeploymentsAPI(t, 0)
			api.deployments = []map[string]any{deploymentPayload(0, tc.resourceType, tc.kind, tc.name, "active")}

			out, err := run(t, api.serve(), "get", api.deployments[0]["id"].(string))
			require.NoError(t, err)
			if tc.want == "" {
				assert.NotContains(t, out, "Build logs:")
				return
			}
			assert.Contains(t, out, tc.want)
		})
	}
}

func TestGetRefusesAnUnknownOrMalformedID(t *testing.T) {
	api := newDeploymentsAPI(t, 3)

	_, err := run(t, api.serve(), "get", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deployment not found")

	_, err = run(t, api.serve(), "get", "latest")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"latest" is not a deployment ID`)
}

func TestGetPrintsJSON(t *testing.T) {
	api := newDeploymentsAPI(t, 1)

	out, err := run(t, api.serve(), "get", api.deployments[0]["id"].(string), "--json")
	require.NoError(t, err)
	var found map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &found))
	assert.Equal(t, "cli", found["deploy_source"])
}

func TestSummaryCountsOneResourceType(t *testing.T) {
	api := newDeploymentsAPI(t, 0)
	api.summary = map[string]any{
		"deployment_count": 42, "successful_count": 38, "failed_count": 3, "canceled_count": 1,
		"success_rate": 0.927, "median_build_duration_seconds": 72.4,
	}

	out, err := run(t, api.serve(), "summary", "--since", "7d")
	require.NoError(t, err)

	require.Len(t, api.sent(), 1)
	assert.Equal(t, "function", api.sent()[0].Get("resource_type"))
	assert.NotEmpty(t, api.sent()[0].Get("created_after"))
	assert.Contains(t, out, "Deployments: 42")
	assert.Contains(t, out, "Success rate: 92.7%")
	assert.Contains(t, out, "Median build: 1m12s")

	out, err = run(t, api.serve(), "summary", "--type", "frontend", "--json")
	require.NoError(t, err)
	assert.Equal(t, "frontend", api.sent()[1].Get("resource_type"))
	var summary map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &summary))
	assert.InDelta(t, 42, summary["deployment_count"], 0)
}

func TestSummaryShowsNoRateWithoutOutcomes(t *testing.T) {
	api := newDeploymentsAPI(t, 0)
	api.summary = map[string]any{
		"deployment_count": 0, "successful_count": 0, "failed_count": 0, "canceled_count": 0,
		"success_rate": nil, "median_build_duration_seconds": nil,
	}

	out, err := run(t, api.serve(), "summary")
	require.NoError(t, err)
	assert.Contains(t, out, "Success rate: -")
	assert.Contains(t, out, "Median build: -")
}

func TestDeploymentsRefuseBadFlags(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"list", "--type", "database"}, `--type must be function or frontend, not "database"`},
		{[]string{"list", "--type", "frontend", "--kind", "durable"}, "--kind applies only to functions"},
		{[]string{"list", "--kind", "scheduled"}, `--kind must be standard or durable`},
		{[]string{"list", "--since", "1h", "--after", "2026-10-30T00:00:00Z"}, "pass --since or --after, not both"},
		{[]string{"list", "--since", "soon"}, "--since must be a positive duration"},
		{[]string{"list", "--after", "yesterday"}, "--after must be an RFC 3339 time"},
		{[]string{"list", "--limit", "0"}, "--limit must be at least 1"},
		{[]string{"summary", "--type", ""}, "--type must be function or frontend"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			api := newDeploymentsAPI(t, 1)

			_, err := run(t, api.serve(), tc.args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Empty(t, api.sent())
		})
	}
}
