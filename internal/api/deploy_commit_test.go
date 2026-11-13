package api

import (
	"encoding/json"
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

// Function and durable function uploads take the commit as one JSON-encoded
// string field with no root, and send no field at all when there is none.
func TestBuildFunctionDeployMultipartSendsTheCommit(t *testing.T) {
	commit := &FunctionCommit{Repository: "acme/web", SHA: "0123456789abcdef0123456789abcdef01234567"}
	want := `{"repository":"acme/web","sha":"0123456789abcdef0123456789abcdef01234567","dirty":false}`

	body, contentType, err := buildFunctionDeployMultipart(FunctionDeployInput{Name: "fn", SourceArchive: []byte("a"), Commit: commit})
	require.NoError(t, err)
	assert.JSONEq(t, want, parseMultipartFields(t, body.Bytes(), contentType)["commit"])
	body, contentType, err = buildFunctionDeployMultipart(FunctionDeployInput{Name: "fn", SourceArchive: []byte("a")})
	require.NoError(t, err)
	assert.NotContains(t, parseMultipartFields(t, body.Bytes(), contentType), "commit")

	body, contentType, err = buildDurableFunctionDeployMultipart(DurableFunctionDeployInput{Name: "fn", SourceArchive: []byte("a"), Commit: commit})
	require.NoError(t, err)
	assert.JSONEq(t, want, parseMultipartFields(t, body.Bytes(), contentType)["commit"])
	body, contentType, err = buildDurableFunctionDeployMultipart(DurableFunctionDeployInput{Name: "fn", SourceArchive: []byte("a")})
	require.NoError(t, err)
	assert.NotContains(t, parseMultipartFields(t, body.Bytes(), contentType), "commit")
}

// A batch entry carries its commit as an object, and none when it has none.
func TestBuildFunctionsBatchMultipartSendsEachEntrysCommit(t *testing.T) {
	commit := &FunctionCommit{Repository: "acme/web", SHA: "0123456789abcdef0123456789abcdef01234567"}
	body, contentType, err := buildFunctionsBatchMultipart([]FunctionDeployInput{
		{Name: "clean", SourceArchive: []byte("a"), Commit: commit},
		{Name: "dirty", SourceArchive: []byte("b")},
	})
	require.NoError(t, err)

	var manifest []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(parseMultipartFields(t, body.Bytes(), contentType)["functions"]), &manifest))
	require.Len(t, manifest, 2)
	assert.JSONEq(t, `{"repository":"acme/web","sha":"0123456789abcdef0123456789abcdef01234567","dirty":false}`, string(manifest[0]["commit"]))
	assert.NotContains(t, manifest[1], "commit")
}
