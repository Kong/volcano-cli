package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The frontend upload takes the commit as one JSON-encoded string field, with
// root sent even when it is the repository root, and sends no field at all
// when there is none.
func TestBuildFrontendDeployMultipartSendsTheCommit(t *testing.T) {
	for root, want := range map[string]string{
		"":         `{"repository":"acme/web","sha":"0123456789abcdef0123456789abcdef01234567","dirty":true,"root":""}`,
		"apps/web": `{"repository":"acme/web","sha":"0123456789abcdef0123456789abcdef01234567","dirty":true,"root":"apps/web"}`,
	} {
		commit := &DeployCommit{Repository: "acme/web", SHA: "0123456789abcdef0123456789abcdef01234567", Dirty: true, Root: root}
		body, contentType, err := buildFrontendDeployMultipart(FrontendDeployInput{Name: "web", Archive: []byte("a"), Commit: commit})
		require.NoError(t, err)
		assert.JSONEq(t, want, parseMultipartFields(t, body.Bytes(), contentType)["commit"])
	}

	body, contentType, err := buildFrontendDeployMultipart(FrontendDeployInput{Name: "web", Archive: []byte("a")})
	require.NoError(t, err)
	assert.NotContains(t, parseMultipartFields(t, body.Bytes(), contentType), "commit")
}
