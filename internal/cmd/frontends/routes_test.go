package frontends

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	routeID          = "77777777-7777-4777-8777-777777777777"
	sessionFuncID    = "88888888-8888-4888-8888-888888888888"
	sessionV2FuncID  = "99999999-9999-4999-8999-999999999999"
	membersFuncID    = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	frontendRoutesAt = "/projects/" + frontendProjectID + "/frontends/" + frontendID + "/function-routes"
)

// routeServer is a frontend with one route to session, plus the function list
// the commands resolve names and visibility from, and one durable function.
type routeServer struct {
	t        *testing.T
	routes   []map[string]any
	requests []string
	bodies   []map[string]any
	refuse   string
}

func newRouteServer(t *testing.T) *routeServer {
	t.Helper()
	return &routeServer{t: t, routes: []map[string]any{routePayload(routeID, sessionFuncID, "/api/session", true)}}
}

func (s *routeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t := s.t
	s.requests = append(s.requests, r.Method+" "+r.URL.Path)
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends":
		frontend := frontendCommandPayload(frontendID, "web")
		frontend["function_routes"] = s.routes
		writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{
			"data": []any{frontend}, "has_more": false, "page": 1, "limit": 100, "total": 1,
		})
	case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/functions":
		writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{
			"data": []any{
				routeFunctionPayload(sessionFuncID, "session", "public"),
				routeFunctionPayload(sessionV2FuncID, "session-v2", "public"),
				routeFunctionPayload(membersFuncID, "members", "authenticated"),
			},
			"has_more": false, "page": 1, "limit": 100, "total": 3,
		})
	case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/durable-functions/order-pipeline":
		writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{
			"id": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "project_id": frontendProjectID, "name": "order-pipeline",
			"runtime": "nodejs24.x", "handler": "handler", "kind": "durable", "status": "active",
			"visibility": "public", "is_public": true, "deployed_regions": []string{"aws-us-east-1"},
			"durable":    map[string]any{"execution_timeout_seconds": 3600, "retention_days": 30},
			"created_at": "2026-05-20T00:00:00Z", "updated_at": "2026-05-20T00:00:00Z",
		})
	case r.Method == http.MethodGet && r.URL.Path == frontendRoutesAt:
		writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{"data": s.routes})
	case r.Method == http.MethodPost && r.URL.Path == frontendRoutesAt, r.Method == http.MethodPut && r.URL.Path == frontendRoutesAt+"/"+routeID:
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		s.bodies = append(s.bodies, body)
		if s.refuse != "" {
			writeFrontendCommandJSON(t, w, http.StatusConflict, map[string]any{"error": s.refuse})
			return
		}
		status := http.StatusOK
		if r.Method == http.MethodPost {
			status = http.StatusCreated
		}
		strip, _ := body["strip_prefix"].(bool)
		writeFrontendCommandJSON(t, w, status, routePayload(routeID, body["function_id"].(string), body["path_prefix"].(string), strip))
	case r.Method == http.MethodDelete && r.URL.Path == frontendRoutesAt+"/"+routeID:
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (s *routeServer) run(args ...string) (string, error) {
	s.t.Helper()
	server := httptest.NewServer(s)
	defer server.Close()
	return executeFrontendsCommand(s.t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), args...)
}

func TestFrontendsRoutesList(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	server := newRouteServer(t)

	out, err := server.run("routes", "list", "web")
	require.NoError(t, err)
	row := lineContaining(t, out, "/api/session")
	assert.Contains(t, row, "session")
	assert.Contains(t, row, "public")
	assert.Contains(t, row, routeID)
	assert.Contains(t, out, "Total: 1 route(s)")

	server.routes = nil
	out, err = server.run("routes", "list", "web")
	require.NoError(t, err)
	assert.Contains(t, out, `No function routes on frontend "web"`)
}

func TestFrontendsRoutesCreateResolvesTheFunctionByName(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	server := newRouteServer(t)

	out, err := server.run("routes", "create", "web", "--path", "/api/v2", "--function", "session-v2", "--strip-prefix")
	require.NoError(t, err)
	assert.Equal(t, []map[string]any{
		{"path_prefix": "/api/v2", "function_id": sessionV2FuncID, "strip_prefix": true},
	}, server.bodies)
	assert.Contains(t, out, "Route /api/v2 on frontend 'web' now forwards to function 'session-v2'")
	assert.Contains(t, out, "Visibility: public")
	assert.Contains(t, out, "Anyone who can load 'web' can call 'session-v2' under /api/v2")
}

func TestFrontendsRoutesCreateRefusals(t *testing.T) {
	t.Run("missing flags", func(t *testing.T) {
		setFrontendCommandTestHome(t)
		saveFrontendCommandTestConfig(t)
		server := newRouteServer(t)

		_, err := server.run("routes", "create", "web", "--path", "/api")
		require.ErrorContains(t, err, `required flag(s) "function" not set`)
		assert.Empty(t, server.requests)
	})
	t.Run("unknown function", func(t *testing.T) {
		setFrontendCommandTestHome(t)
		saveFrontendCommandTestConfig(t)
		server := newRouteServer(t)

		_, err := server.run("routes", "create", "web", "--path", "/api", "--function", "missing")
		require.ErrorContains(t, err, `function "missing" not found`)
		assert.Empty(t, server.bodies)
	})
	t.Run("durable target", func(t *testing.T) {
		setFrontendCommandTestHome(t)
		saveFrontendCommandTestConfig(t)
		server := newRouteServer(t)

		_, err := server.run("routes", "create", "web", "--path", "/api", "--function", "order-pipeline")
		require.ErrorContains(t, err, `"order-pipeline" is a durable function: frontend routes forward only to standard functions`)
		assert.Empty(t, server.bodies)
	})
	t.Run("non-public target", func(t *testing.T) {
		setFrontendCommandTestHome(t)
		saveFrontendCommandTestConfig(t)
		server := newRouteServer(t)
		server.refuse = "Frontend Function routes require a public Function"

		_, err := server.run("routes", "create", "web", "--path", "/api", "--function", "members")
		require.ErrorContains(t, err, "Frontend Function routes require a public Function\n"+
			"make 'members' public first: volcano cloud functions update members --visibility public")
	})
	// A conflict over a public target is about something else, so it gets no
	// advice to make the function public.
	t.Run("conflict with a public target", func(t *testing.T) {
		setFrontendCommandTestHome(t)
		saveFrontendCommandTestConfig(t)
		server := newRouteServer(t)
		server.refuse = "route path prefix already exists"

		_, err := server.run("routes", "create", "web", "--path", "/api", "--function", "session")
		require.ErrorContains(t, err, "route path prefix already exists")
		assert.NotContains(t, err.Error(), "public first")
	})
}

// The API's request validator answers a malformed prefix with a bare "invalid
// request", so the CLI checks the prefix against the same rule first.
func TestFrontendsRoutesRefuseAnInvalidPathBeforeCallingTheAPI(t *testing.T) {
	for _, path := range []string{
		"/api/echo/",
		"api/echo",
		"/",
		"/api?x=1",
		"/api#top",
		`/api\echo`,
		"/" + strings.Repeat("a", 512),
	} {
		t.Run(path[:min(len(path), 12)], func(t *testing.T) {
			setFrontendCommandTestHome(t)
			saveFrontendCommandTestConfig(t)
			server := newRouteServer(t)

			want := fmt.Sprintf("invalid --path %q: a path prefix starts with /, does not end with /, "+
				`has no "?", "#", or "\", and is 2 to 512 characters long`, path)
			_, err := server.run("routes", "create", "web", "--path", path, "--function", "session")
			require.EqualError(t, err, want)
			_, err = server.run("routes", "update", "web", "/api/session", "--path", path)
			require.EqualError(t, err, want)
			assert.Empty(t, server.requests)
		})
	}
}

func TestFrontendsRoutesAcceptTheLongestValidPath(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	server := newRouteServer(t)
	path := "/" + strings.Repeat("a", 511)

	_, err := server.run("routes", "create", "web", "--path", path, "--function", "session")
	require.NoError(t, err)
	require.Len(t, server.bodies, 1)
	assert.Equal(t, path, server.bodies[0]["path_prefix"])
}

// A route update replaces the whole route, so the flags left out have to be
// filled from the route as it stands.
func TestFrontendsRoutesUpdateKeepsWhatIsNotChanged(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want map[string]any
	}{
		{
			name: "function",
			args: []string{"--function", "session-v2"},
			want: map[string]any{"path_prefix": "/api/session", "function_id": sessionV2FuncID, "strip_prefix": true},
		},
		{
			name: "path by id",
			args: []string{"--path", "/auth"},
			want: map[string]any{"path_prefix": "/auth", "function_id": sessionFuncID, "strip_prefix": true},
		},
		{
			name: "strip prefix off",
			args: []string{"--strip-prefix=false"},
			want: map[string]any{"path_prefix": "/api/session", "function_id": sessionFuncID, "strip_prefix": false},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setFrontendCommandTestHome(t)
			saveFrontendCommandTestConfig(t)
			server := newRouteServer(t)
			route := "/api/session/"
			if tc.name == "path by id" {
				route = routeID
			}

			out, err := server.run(append([]string{"routes", "update", "web", route}, tc.args...)...)
			require.NoError(t, err)
			assert.Equal(t, []map[string]any{tc.want}, server.bodies)
			assert.Contains(t, out, "on frontend 'web' updated")
		})
	}
}

func TestFrontendsRoutesUpdateRefusals(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	server := newRouteServer(t)

	_, err := server.run("routes", "update", "web", "/api/session")
	require.ErrorContains(t, err, "specify at least one of --path, --function, or --strip-prefix")
	assert.Empty(t, server.requests)

	_, err = server.run("routes", "update", "web", "/missing", "--path", "/x")
	require.ErrorContains(t, err, `frontend "web" has no route "/missing"`)
	assert.Empty(t, server.bodies)

	_, err = server.run("routes", "update", "web", "/api/session", "--function", "order-pipeline")
	require.ErrorContains(t, err, `"order-pipeline" is a durable function`)
	assert.Empty(t, server.bodies)
}

func TestFrontendsRoutesDeleteConfirmsTheRoute(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	server := newRouteServer(t)
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()

	cmd := New(cliruntime.Deps{HTTPClient: httpServer.Client(), APIBaseURL: httpServer.URL})
	cmd.SetIn(strings.NewReader("n\n"))
	out, err := executeFrontendsCommand(t, cmd, "routes", "delete", "web", "/api/session")
	require.NoError(t, err)
	assert.Contains(t, out, "/api/session on frontend web")
	assert.NotContains(t, server.requests, "DELETE "+frontendRoutesAt+"/"+routeID)

	out, err = server.run("routes", "delete", "web", "/api/session", "--yes")
	require.NoError(t, err)
	assert.Contains(t, server.requests, "DELETE "+frontendRoutesAt+"/"+routeID)
	assert.Contains(t, out, "Route /api/session deleted from frontend 'web'")
}

func TestFrontendsGetShowsRoutesWithTheirTargetsVisibility(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	server := newRouteServer(t)

	out, err := server.run("get", "web")
	require.NoError(t, err)
	assert.Contains(t, out, "Function routes:")
	assert.Contains(t, out, "  /api/session -> session (public, strip prefix)")
}

func lineContaining(t *testing.T, output, needle string) string {
	t.Helper()
	for line := range strings.SplitSeq(output, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	t.Fatalf("no line contains %q:\n%s", needle, output)
	return ""
}

func routePayload(id, functionID, pathPrefix string, stripPrefix bool) map[string]any {
	return map[string]any{
		"id":           id,
		"project_id":   frontendProjectID,
		"frontend_id":  frontendID,
		"function_id":  functionID,
		"path_prefix":  pathPrefix,
		"strip_prefix": stripPrefix,
		"created_at":   "2026-05-20T00:00:00Z",
		"updated_at":   "2026-05-20T00:00:00Z",
	}
}

func routeFunctionPayload(id, name, visibility string) map[string]any {
	return map[string]any{
		"id":               id,
		"project_id":       frontendProjectID,
		"name":             name,
		"status":           "active",
		"visibility":       visibility,
		"is_public":        visibility == "public",
		"deployed_regions": []string{"aws-us-east-1"},
		"created_at":       "2026-05-20T00:00:00Z",
		"updated_at":       "2026-05-20T00:00:00Z",
	}
}
