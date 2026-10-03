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
// refusal says what to do first.
func TestFunctionsUpdateShowsTheRouteConflict(t *testing.T) {
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
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"update", "hello", "--visibility", "authenticated")
	require.ErrorContains(t, err, "remove attached Frontend Function routes before making the function non-public")
}

func TestFunctionsUpdateHidesTheRetiredPrivateFlag(t *testing.T) {
	out, err := executeFunctionsCommand(t, New(cliruntime.Deps{}), "update", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "--visibility")
	assert.Contains(t, out, "--public")
	assert.NotContains(t, out, "--private")
}
