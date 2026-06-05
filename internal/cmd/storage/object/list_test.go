package object

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestList(t *testing.T) {
	setObjectCommandTestHome(t)
	saveObjectCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/storage/uploads" {
			assert.Equal(t, "greetings/", r.URL.Query().Get("prefix"))
			writeObjectJSON(t, w, http.StatusOK, map[string]any{
				"objects": []any{
					objectPayload("greetings/hello.txt", 12, false),
				},
				"next_cursor": "next-page-token",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeObjectCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"list", "uploads", "--prefix", "greetings/",
	)
	require.NoError(t, err)
	assert.Contains(t, out, "greetings/hello.txt")
	assert.Contains(t, out, "Next page:")
	assert.Contains(t, out, "next-page-token")
}
