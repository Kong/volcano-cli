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

func TestCopy(t *testing.T) {
	setObjectCommandTestHome(t)
	saveObjectCommandTestConfig(t)

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/storage/uploads/copy" {
			require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
			writeObjectJSON(t, w, http.StatusCreated, objectPayload("greetings/hello-copy.txt", 12, false))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeObjectCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"copy", "uploads", "greetings/hello.txt", "greetings/hello-copy.txt",
	)
	require.NoError(t, err)
	assert.Contains(t, out, "Object copied to 'greetings/hello-copy.txt' in bucket 'uploads'")

	require.NotNil(t, captured)
	assert.Equal(t, "greetings/hello.txt", captured["from"])
	assert.Equal(t, "greetings/hello-copy.txt", captured["to"])
}
