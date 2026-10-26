package frontends

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	mountID          = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	docsFrontendID   = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	docsV2FrontendID = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	mountsAt         = "/projects/" + frontendProjectID + "/frontends/" + frontendID + "/frontend-routes"
)

// mountServer is the frontend web with a function route to session at
// /api/session and a Frontend route mounting docs at /docs, plus the docs and
// docs-v2 frontends it can mount.
type mountServer struct {
	t        *testing.T
	mounts   []map[string]any
	requests []string
	bodies   []map[string]any
	refuse   string
	// deleteRefusal answers a frontend delete with 409 and this message.
	deleteRefusal string
}

func newMountServer(t *testing.T) *mountServer {
	t.Helper()
	return &mountServer{t: t, mounts: []map[string]any{mountPayload(mountID, docsFrontendID, "/docs")}}
}

func (s *mountServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t := s.t
	s.requests = append(s.requests, r.Method+" "+r.URL.Path)
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends":
		web := frontendCommandPayload(frontendID, "web")
		web["function_routes"] = []map[string]any{routePayload(routeID, sessionFuncID, "/api/session", true)}
		web["frontend_routes"] = s.mounts
		writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{
			"data": []any{
				web,
				frontendCommandPayload(docsFrontendID, "docs"),
				frontendCommandPayload(docsV2FrontendID, "docs-v2"),
			},
			"has_more": false, "page": 1, "limit": 100, "total": 3,
		})
	case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/functions":
		writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{
			"data":     []any{routeFunctionPayload(sessionFuncID, "session", "public")},
			"has_more": false, "page": 1, "limit": 100, "total": 1,
		})
	case r.Method == http.MethodGet && r.URL.Path == frontendRoutesAt:
		writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{
			"data": []any{routePayload(routeID, sessionFuncID, "/api/session", true)},
		})
	case r.Method == http.MethodGet && r.URL.Path == mountsAt:
		writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{"data": s.mounts})
	case r.Method == http.MethodPost && r.URL.Path == mountsAt, r.Method == http.MethodPut && r.URL.Path == mountsAt+"/"+mountID:
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
		writeFrontendCommandJSON(t, w, status, mountPayload(mountID, body["target_frontend_id"].(string), body["path_prefix"].(string)))
	case r.Method == http.MethodPut && r.URL.Path == frontendRoutesAt+"/"+routeID:
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		s.bodies = append(s.bodies, body)
		writeFrontendCommandJSON(t, w, http.StatusOK, routePayload(routeID, sessionFuncID, "/api/session", true))
	case r.Method == http.MethodDelete && r.URL.Path == mountsAt+"/"+mountID:
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodDelete && r.URL.Path == "/projects/"+frontendProjectID+"/frontends/"+docsFrontendID:
		if s.deleteRefusal != "" {
			writeFrontendCommandJSON(t, w, http.StatusConflict, map[string]any{"error": s.deleteRefusal})
			return
		}
		w.WriteHeader(http.StatusAccepted)
	default:
		http.NotFound(w, r)
	}
}

func (s *mountServer) run(args ...string) (string, error) {
	s.t.Helper()
	server := httptest.NewServer(s)
	defer server.Close()
	return executeFrontendsCommand(s.t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), args...)
}

func TestFrontendsRoutesListShowsBothKinds(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	server := newMountServer(t)

	out, err := server.run("routes", "list", "web")
	require.NoError(t, err)
	function := lineContaining(t, out, "/api/session")
	assert.Contains(t, function, "function")
	assert.Contains(t, function, "session")
	mount := lineContaining(t, out, "/docs")
	assert.Contains(t, mount, "frontend")
	assert.Contains(t, mount, "docs")
	assert.Contains(t, mount, mountID)
	assert.Less(t, strings.Index(out, "/api/session"), strings.Index(out, "/docs"), "the longer prefix lists first")
	assert.Contains(t, out, "Total: 2 route(s)")
}

func TestFrontendsRoutesCreateMountsAFrontendByName(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	server := newMountServer(t)

	out, err := server.run("routes", "create", "web", "--path", "/guides", "--frontend", "docs-v2")
	require.NoError(t, err)
	assert.Equal(t, []map[string]any{{"path_prefix": "/guides", "target_frontend_id": docsV2FrontendID}}, server.bodies)
	assert.Contains(t, out, "Route /guides on frontend 'web' now serves frontend 'docs-v2'")
	assert.Contains(t, out, "Frontend: docs-v2")
	assert.Contains(t, out, "'docs-v2' receives the full path, so it must serve its pages under /guides.")
	assert.NotContains(t, out, "authenticate")
}

func TestFrontendsRoutesCreateMountRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		args   []string
		refuse string
		want   string
		sent   bool
	}{
		"both targets": {
			args: []string{"--function", "session", "--frontend", "docs"},
			want: "if any flags in the group [function frontend] are set none of the others can be",
		},
		"strip prefix": {
			args: []string{"--frontend", "docs", "--strip-prefix"},
			want: "--strip-prefix applies to function routes only: a mounted frontend always sees the full path",
		},
		"unknown frontend": {args: []string{"--frontend", "missing"}, want: `frontend "missing" not found`},
		"mounted frontend being deleted": {
			args:   []string{"--frontend", "docs"},
			refuse: "the mounted Frontend is being deleted",
			want:   "failed to create route: HTTP 409: the mounted Frontend is being deleted",
			sent:   true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			setFrontendCommandTestHome(t)
			saveFrontendCommandTestConfig(t)
			server := newMountServer(t)
			server.refuse = tc.refuse

			_, err := server.run(append([]string{"routes", "create", "web", "--path", "/guides"}, tc.args...)...)
			require.ErrorContains(t, err, tc.want)
			if tc.sent {
				assert.Len(t, server.bodies, 1)
			} else {
				assert.Empty(t, server.bodies)
			}
		})
	}
}

// A Frontend route update replaces the whole route, so the flags left out have
// to be filled from the route as it stands.
func TestFrontendsRoutesUpdateKeepsWhatAMountDoesNotChange(t *testing.T) {
	for name, tc := range map[string]struct {
		route string
		args  []string
		want  map[string]any
	}{
		"frontend": {
			route: "/docs/",
			args:  []string{"--frontend", "docs-v2"},
			want:  map[string]any{"path_prefix": "/docs", "target_frontend_id": docsV2FrontendID},
		},
		"path by id": {
			route: mountID,
			args:  []string{"--path", "/guides"},
			want:  map[string]any{"path_prefix": "/guides", "target_frontend_id": docsFrontendID},
		},
	} {
		t.Run(name, func(t *testing.T) {
			setFrontendCommandTestHome(t)
			saveFrontendCommandTestConfig(t)
			server := newMountServer(t)

			out, err := server.run(append([]string{"routes", "update", "web", tc.route}, tc.args...)...)
			require.NoError(t, err)
			assert.Equal(t, []map[string]any{tc.want}, server.bodies)
			assert.Contains(t, out, "on frontend 'web' updated")
			assert.Contains(t, out, "Frontend: ")
		})
	}
}

// The two kinds have separate APIs, so a route cannot be switched in place.
func TestFrontendsRoutesUpdateKeepsTheKind(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	server := newMountServer(t)

	_, err := server.run("routes", "update", "web", "/docs", "--function", "session")
	require.ErrorContains(t, err, `route /docs on frontend "web" mounts a frontend, so --function and --strip-prefix do not apply`)
	_, err = server.run("routes", "update", "web", "/docs", "--strip-prefix")
	require.ErrorContains(t, err, "--function and --strip-prefix do not apply")
	_, err = server.run("routes", "update", "web", "/api/session", "--frontend", "docs")
	require.ErrorContains(t, err, `route /api/session on frontend "web" forwards to a function, so --frontend does not apply`)
	assert.Empty(t, server.bodies)
}

func TestFrontendsRoutesDeleteRemovesAMount(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	server := newMountServer(t)

	out, err := server.run("routes", "delete", "web", "/docs", "--yes")
	require.NoError(t, err)
	assert.Contains(t, server.requests, "DELETE "+mountsAt+"/"+mountID)
	assert.NotContains(t, server.requests, "DELETE "+frontendRoutesAt+"/"+routeID)
	assert.Contains(t, out, "Route /docs deleted from frontend 'web'")
}

func TestFrontendsGetShowsFrontendRoutes(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	server := newMountServer(t)

	out, err := server.run("get", "web")
	require.NoError(t, err)
	assert.Contains(t, out, "Function routes:\n  /api/session -> session (public, strip prefix)\n")
	assert.Contains(t, out, "Frontend routes:\n  /docs -> frontend docs\n")
}

// The frontend listing only names the mounted frontends, so failing to read it
// must not hide the frontend or its routes.
func TestFrontendsGetShowsMountedFrontendIDsWhenFrontendsCannotBeListed(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends/"+frontendID:
			frontend := frontendCommandPayload(frontendID, "web")
			frontend["frontend_routes"] = []map[string]any{mountPayload(mountID, docsFrontendID, "/docs")}
			writeFrontendCommandJSON(t, w, http.StatusOK, frontend)
		default:
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()

	cmd := New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"get", frontendID})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, stdout.String(), "  /docs -> frontend "+docsFrontendID)
	assert.Contains(t, stderr.String(), "Frontend routes on 'web' show frontend IDs instead of names: failed to list frontends")
}

func mountPayload(id, targetFrontendID, pathPrefix string) map[string]any {
	return map[string]any{
		"id":                 id,
		"project_id":         frontendProjectID,
		"frontend_id":        frontendID,
		"target_frontend_id": targetFrontendID,
		"path_prefix":        pathPrefix,
		"created_at":         "2026-05-20T00:00:00Z",
		"updated_at":         "2026-05-20T00:00:00Z",
	}
}
