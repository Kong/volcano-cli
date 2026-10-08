package functions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestFunctionsUpdateVisibility(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	var updateBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+functionProjectID+"/functions":
			writeFunctionCommandJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{functionCommandPayload(functionID, "hello")},
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    1,
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/projects/"+functionProjectID+"/functions/"+functionID:
			updateBody = nil
			require.NoError(t, json.NewDecoder(r.Body).Decode(&updateBody))
			payload := functionCommandPayload(functionID, "hello")
			payload["visibility"] = updateBody["visibility"]
			payload["is_public"] = updateBody["visibility"] == "public"
			writeFunctionCommandJSON(t, w, http.StatusOK, payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	run := func(args ...string) (string, error) {
		return executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), args...)
	}

	for _, level := range []string{"private", "authenticated", "public"} {
		out, err := run("update", "hello", "--visibility", level)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"visibility": level}, updateBody)
		assert.Contains(t, out, "Function 'hello' visibility set to "+level)
	}

	out, err := run("update", "hello", "--public")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"visibility": "public"}, updateBody)
	assert.Contains(t, out, "Function 'hello' visibility set to public")
}

func TestFunctionsUpdateRefusesBeforeCallingTheAPI(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "no flag", args: []string{"update", "hello"}, want: "specify --visibility private, authenticated, or public"},
		{name: "unknown level", args: []string{"update", "hello", "--visibility", "everyone"}, want: `invalid --visibility "everyone"`},
		{name: "both spellings", args: []string{"update", "hello", "--public", "--visibility", "private"}, want: "--public is the same as --visibility public"},
		// --private used to keep signed-in users in. Mapping it to either level
		// would surprise someone, so it names both instead.
		{name: "retired private", args: []string{"update", "hello", "--private"}, want: "--visibility private for service keys and schedulers only, or --visibility authenticated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setFunctionCommandTestHome(t)
			saveFunctionCommandTestConfig(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				http.NotFound(w, r)
			}))
			defer server.Close()

			_, err := executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), tc.args...)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

// Leaving public while a route forwards to the function is refused, and the
// refusal names the routes to remove first. The CLI manages routes only in the
// cloud, so a local project is pointed at its manifest instead.
func TestFunctionsUpdateShowsTheRouteConflict(t *testing.T) {
	for _, tc := range []struct {
		name string
		deps cliruntime.Deps
		want string
	}{
		{
			name: "cloud",
			deps: cliruntime.Deps{CommandPathPrefix: "volcano cloud"},
			want: "frontend routes forward to it; remove them first:\n" +
				"  volcano cloud frontends routes delete admin /hello\n" +
				"  volcano cloud frontends routes delete web /api/hello",
		},
		{
			name: "local",
			deps: cliruntime.Deps{CommandPathPrefix: "volcano", LocalMode: true},
			want: "frontend routes forward to it; remove them from function_routes in volcano-config.yaml and run volcano config deploy:\n" +
				"  admin /hello\n" +
				"  web /api/hello",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setFunctionCommandTestHome(t)
			saveFunctionCommandTestConfig(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/projects/"+functionProjectID+"/functions":
					writeFunctionCommandJSON(t, w, http.StatusOK, map[string]any{
						"data": []any{functionCommandPayload(functionID, "hello")}, "has_more": false, "page": 1, "limit": 100, "total": 1,
					})
				case r.Method == http.MethodPatch:
					writeFunctionCommandJSON(t, w, http.StatusConflict, map[string]any{
						"error": "remove attached Frontend Function routes before making the function non-public",
					})
				case r.Method == http.MethodGet && r.URL.Path == "/projects/"+functionProjectID+"/frontends":
					writeFunctionCommandJSON(t, w, http.StatusOK, frontendsWithRoutesPayload(map[string][]map[string]any{
						"web":   {functionRoutePayload(functionID, "/api/hello"), functionRoutePayload(otherFunctionID, "/api/other")},
						"admin": {functionRoutePayload(functionID, "/hello")},
					}))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			tc.deps.HTTPClient = server.Client()
			tc.deps.APIBaseURL = server.URL

			_, err := executeFunctionsCommand(t, New(tc.deps), "update", "hello", "--visibility", "authenticated")
			require.ErrorContains(t, err, "remove attached Frontend Function routes before making the function non-public")
			require.ErrorContains(t, err, tc.want)
			assert.NotContains(t, err.Error(), "/api/other")
		})
	}
}

// Without the route listing the refusal still stands, just without the list.
func TestFunctionsUpdateShowsTheRouteConflictWhenRoutesCannotBeListed(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+functionProjectID+"/functions":
			writeFunctionCommandJSON(t, w, http.StatusOK, map[string]any{
				"data": []any{functionCommandPayload(functionID, "hello")}, "has_more": false, "page": 1, "limit": 100, "total": 1,
			})
		case r.Method == http.MethodPatch:
			writeFunctionCommandJSON(t, w, http.StatusConflict, map[string]any{
				"error": "remove attached Frontend Function routes before making the function non-public",
			})
		default:
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()

	_, err := executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"update", "hello", "--visibility", "authenticated")
	require.ErrorContains(t, err, "remove attached Frontend Function routes before making the function non-public")
	assert.NotContains(t, err.Error(), "remove them first")
}

// The standard function list does not hold durable functions, so their names
// would otherwise read as missing.
func TestFunctionsUpdateExplainsADurableName(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+functionProjectID+"/functions":
			writeFunctionCommandJSON(t, w, http.StatusOK, map[string]any{
				"data": []any{}, "has_more": false, "page": 1, "limit": 100, "total": 0,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+functionProjectID+"/durable-functions/order-pipeline":
			payload := functionCommandPayload(otherFunctionID, "order-pipeline")
			payload["kind"] = "durable"
			payload["visibility"] = "private"
			writeFunctionCommandJSON(t, w, http.StatusOK, payload)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := executeFunctionsCommand(t,
		New(cliruntime.Deps{CommandPathPrefix: "volcano cloud", HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"update", "order-pipeline", "--visibility", "authenticated")
	require.EqualError(t, err, `"order-pipeline" is a durable function: declare visibility: authenticated under it `+
		"in volcano-config.yaml and run volcano cloud config deploy, "+
		"or redeploy it with volcano cloud durable deploy -f order-pipeline --visibility authenticated")
}

// A server that predates visibility levels either refuses the request with a
// bare 400 or answers without a visibility. Neither may read as success.
func TestFunctionsUpdateOnAServerWithoutVisibilityLevels(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   map[string]any
		want   string
	}{
		{
			name:   "refused",
			status: http.StatusBadRequest,
			body:   map[string]any{"error": "invalid request"},
			want:   "invalid request\na server that predates visibility levels refuses --visibility this way: upgrade your local-mode server image",
		},
		{
			name:   "ignored",
			status: http.StatusOK,
			body:   functionCommandPayload(functionID, "hello"),
			want:   "the server does not support visibility levels yet: check who can invoke 'hello' with volcano functions get hello",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setFunctionCommandTestHome(t)
			saveFunctionCommandTestConfig(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/projects/"+functionProjectID+"/functions":
					writeFunctionCommandJSON(t, w, http.StatusOK, map[string]any{
						"data": []any{functionCommandPayload(functionID, "hello")}, "has_more": false, "page": 1, "limit": 100, "total": 1,
					})
				case r.Method == http.MethodPatch:
					writeFunctionCommandJSON(t, w, tc.status, tc.body)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			out, err := executeFunctionsCommand(t,
				New(cliruntime.Deps{CommandPathPrefix: "volcano", LocalMode: true, HTTPClient: server.Client(), APIBaseURL: server.URL}),
				"update", "hello", "--visibility", "private")
			require.ErrorContains(t, err, tc.want)
			assert.NotContains(t, out, "visibility set to")
		})
	}
}

func TestFunctionsUpdateHidesTheRetiredPrivateFlag(t *testing.T) {
	out, err := executeFunctionsCommand(t, New(cliruntime.Deps{}), "update", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "--visibility")
	assert.Contains(t, out, "--public")
	assert.NotContains(t, out, "--private")
}
