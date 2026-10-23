package api

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testCommit = &DeployCommit{Repository: "acme/app", SHA: "0123456789abcdef0123456789abcdef01234567", Dirty: true}

const testCommitJSON = `{"repository":"acme/app","sha":"0123456789abcdef0123456789abcdef01234567","dirty":true}`

// The single uploads take the commit as one JSON-encoded string field, and
// send no field at all when there is none.
func TestBuildersSendTheCommitAsAJSONField(t *testing.T) {
	builds := map[string]func(*DeployCommit) (*bytes.Buffer, string, error){
		"function": func(commit *DeployCommit) (*bytes.Buffer, string, error) {
			return buildFunctionDeployMultipart(FunctionDeployInput{Name: "fn", SourceArchive: []byte("a"), Commit: commit})
		},
		"durable function": func(commit *DeployCommit) (*bytes.Buffer, string, error) {
			return buildDurableFunctionDeployMultipart(DurableFunctionDeployInput{Name: "fn", SourceArchive: []byte("a"), Commit: commit})
		},
		"frontend": func(commit *DeployCommit) (*bytes.Buffer, string, error) {
			return buildFrontendDeployMultipart(FrontendDeployInput{Name: "web", Archive: []byte("a"), Commit: commit})
		},
	}
	for name, build := range builds {
		body, contentType, err := build(testCommit)
		require.NoError(t, err, name)
		assert.JSONEq(t, testCommitJSON, parseMultipartFields(t, body.Bytes(), contentType)["commit"], name)

		body, contentType, err = build(nil)
		require.NoError(t, err, name)
		assert.NotContains(t, parseMultipartFields(t, body.Bytes(), contentType), "commit", name)
	}
}

// The batch upload nests the commit in each manifest entry as an object.
func TestBuildFunctionsBatchMultipartNestsTheCommit(t *testing.T) {
	body, contentType, err := buildFunctionsBatchMultipart([]FunctionDeployInput{
		{Name: "with", SourceArchive: []byte("a"), Commit: testCommit},
		{Name: "without", SourceArchive: []byte("b")},
	})
	require.NoError(t, err)

	var manifest []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(parseMultipartFields(t, body.Bytes(), contentType)["functions"]), &manifest))
	require.Len(t, manifest, 2)
	assert.JSONEq(t, testCommitJSON, string(manifest[0]["commit"]))
	assert.NotContains(t, manifest[1], "commit")
}
