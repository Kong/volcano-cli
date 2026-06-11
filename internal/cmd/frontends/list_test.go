package frontends

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestFrontendsListPopulatedAndEmpty(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    map[string]any
		want    []string
		notWant []string
	}{
		{
			name: "populated",
			body: map[string]any{
				"data":     []any{frontendCommandPayload(frontendID, "web")},
				"has_more": true,
				"page":     1,
				"limit":    100,
				"total":    2,
			},
			want: []string{
				"web",
				"ready",
				"site: https://web.frontends.volcano.dev/",
				"Showing 1 of 2 frontend(s) (page 1, limit 100)",
				"Next page: volcano cloud frontends list --page 2 --limit 100",
			},
		},
		{
			name: "empty",
			body: map[string]any{
				"data":     []any{},
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    0,
			},
			want: []string{
				"No frontends deployed",
			},
			notWant: []string{
				"Showing 0 of 0",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setFrontendCommandTestHome(t)
			saveFrontendCommandTestConfig(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/projects/"+frontendProjectID+"/frontends", r.URL.Path)
				assert.Equal(t, "page=1&limit=100", r.URL.RawQuery)
				writeFrontendCommandJSON(t, w, http.StatusOK, tc.body)
			}))
			defer server.Close()

			out, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "list")
			require.NoError(t, err)
			for _, want := range tc.want {
				assert.Contains(t, out, want)
			}
			for _, notWant := range tc.notWant {
				assert.NotContains(t, out, notWant)
			}
		})
	}
}
