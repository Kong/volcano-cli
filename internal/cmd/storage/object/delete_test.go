package object

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestDeleteCancelAndConfirm(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		setObjectCommandTestHome(t)
		saveObjectCommandTestConfig(t)
		var sawDelete bool
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/storage/uploads/") {
				sawDelete = true
				w.WriteHeader(http.StatusNoContent)
				return
			}
			http.NotFound(w, r)
		}))
		defer server.Close()

		cmd := New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
		cmd.SetIn(strings.NewReader("no\n"))
		out, err := executeObjectCommand(t, cmd, "delete", "uploads", "greetings/hello.txt")
		require.NoError(t, err)
		assert.False(t, sawDelete)
		assert.Contains(t, out, "You are about to delete a resource permanently")
		assert.Contains(t, out, "Delete storage object 'greetings/hello.txt in bucket uploads'?")
		assert.Contains(t, out, "Delete cancelled.")
	})

	t.Run("yes", func(t *testing.T) {
		setObjectCommandTestHome(t)
		saveObjectCommandTestConfig(t)
		var sawDelete bool
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/storage/uploads/") {
				sawDelete = true
				w.WriteHeader(http.StatusNoContent)
				return
			}
			http.NotFound(w, r)
		}))
		defer server.Close()

		out, err := executeObjectCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
			"delete", "uploads", "greetings/hello.txt", "--yes",
		)
		require.NoError(t, err)
		assert.True(t, sawDelete)
		assert.Contains(t, out, "Object 'greetings/hello.txt' deleted from bucket 'uploads'")
	})
}
