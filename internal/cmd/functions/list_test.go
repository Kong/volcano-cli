package functions

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestFunctionsListPopulatedAndEmpty(t *testing.T) {
	for _, tc := range []struct {
		name string
		body map[string]any
		want []string
	}{
		{
			name: "populated",
			body: map[string]any{
				"data":     []any{functionCommandPayload(functionID, "hello")},
				"has_more": true,
				"page":     1,
				"limit":    100,
				"total":    2,
			},
			want: []string{
				"hello",
				"nodejs24.x",
				"invoke: https://" + functionID + ".functions.volcano.dev/",
				"Showing 1 of 2 function(s) (page 1, limit 100)",
				"Next page: volcano functions list --page 2 --limit 100",
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
				"No functions deployed",
				"Showing 0 of 0 function(s) (page 1, limit 100)",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setFunctionCommandTestHome(t)
			saveFunctionCommandTestConfig(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/projects/"+functionProjectID+"/functions", r.URL.Path)
				assert.Equal(t, "page=1&limit=100", r.URL.RawQuery)
				writeFunctionCommandJSON(t, w, http.StatusOK, tc.body)
			}))
			defer server.Close()

			out, err := executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "list")
			require.NoError(t, err)
			for _, want := range tc.want {
				assert.Contains(t, out, want)
			}
		})
	}
}
