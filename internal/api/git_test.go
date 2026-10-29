package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A server that predates production_branch_follows_default leaves it out. Its
// branch came from the repository's default, so the connection follows it
// rather than reading as pinned; a field the server sends is kept as sent.
func TestProjectGitConnectionFollowsTheDefaultWhenTheServerOmitsIt(t *testing.T) {
	projectID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	for _, tc := range []struct {
		name           string
		field          string
		followsDefault bool
	}{
		{name: "omitted", followsDefault: true},
		{name: "pinned", field: `,"production_branch_follows_default":false`, followsDefault: false},
		{name: "following", field: `,"production_branch_follows_default":true`, followsDefault: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"project_id":"` + projectID.String() + `","repo_id":42,` +
					`"repo_full_name":"octo/storefront","production_branch":"main","root_directory":""` + tc.field + `}`))
			}))
			defer server.Close()
			client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
			require.NoError(t, err)

			connection, err := client.GetProjectGitConnection(context.Background(), projectID)
			require.NoError(t, err)
			assert.Equal(t, tc.followsDefault, connection.ProductionBranchFollowsDefault)

			connection, err = client.SetProjectGitProductionBranch(context.Background(), projectID, "main")
			require.NoError(t, err)
			assert.Equal(t, tc.followsDefault, connection.ProductionBranchFollowsDefault)
		})
	}
}
