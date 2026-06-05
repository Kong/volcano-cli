package object

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestVisibilityToggle(t *testing.T) {
	setObjectCommandTestHome(t)
	saveObjectCommandTestConfig(t)

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/visibility") {
			require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
			writeObjectJSON(t, w, http.StatusOK, objectPayload("greetings/hello.txt", 12, true))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeObjectCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"visibility", "uploads", "greetings/hello.txt", "--public",
	)
	require.NoError(t, err)
	assert.Contains(t, out, "is now public")
	require.NotNil(t, captured)
	assert.Equal(t, true, captured["is_public"])
}

func TestVisibilityRequiresAVisibilityFlag(t *testing.T) {
	setObjectCommandTestHome(t)
	saveObjectCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeObjectCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"visibility", "uploads", "greetings/hello.txt",
	)
	require.Error(t, err)
	assert.Contains(t, out, "at least one of the flags in the group [public private] is required")
}

func TestVisibilityRejectsBothFlags(t *testing.T) {
	setObjectCommandTestHome(t)
	saveObjectCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeObjectCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"visibility", "uploads", "greetings/hello.txt", "--public", "--private",
	)
	require.Error(t, err)
	assert.Contains(t, out, "none of the others can be")
}
