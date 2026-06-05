package object

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestMove(t *testing.T) {
	setObjectCommandTestHome(t)
	saveObjectCommandTestConfig(t)

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/storage/uploads/move" {
			require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
			writeObjectJSON(t, w, http.StatusOK, objectPayload("greetings/renamed.txt", 12, false))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeObjectCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"move", "uploads", "greetings/hello.txt", "greetings/renamed.txt",
	)
	require.NoError(t, err)
	assert.Contains(t, out, "Object moved to 'greetings/renamed.txt' in bucket 'uploads'")

	require.NotNil(t, captured)
	assert.Equal(t, "greetings/hello.txt", captured["from"])
	assert.Equal(t, "greetings/renamed.txt", captured["to"])
}
