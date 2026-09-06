package api

import (
	"bytes"
	"encoding/json"
	"mime"
	"mime/multipart"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseMultipartFields returns the non-file form fields of a built multipart body.
func parseMultipartFields(t *testing.T, body []byte, contentType string) map[string]string {
	t.Helper()
	_, params, err := mime.ParseMediaType(contentType)
	require.NoError(t, err)

	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	form, err := reader.ReadForm(1 << 20)
	require.NoError(t, err)
	t.Cleanup(func() { _ = form.RemoveAll() })

	fields := make(map[string]string, len(form.Value))
	for name, values := range form.Value {
		require.Len(t, values, 1, "field %q", name)
		fields[name] = values[0]
	}
	return fields
}

func TestBuildFunctionDeployMultipartOmitsVariableScopeWhenUnset(t *testing.T) {
	body, contentType, err := buildFunctionDeployMultipart(FunctionDeployInput{
		Name:          "fn",
		Runtime:       "nodejs24.x",
		Handler:       "handler",
		SourceArchive: []byte("archive"),
	})
	require.NoError(t, err)

	fields := parseMultipartFields(t, body.Bytes(), contentType)
	assert.Equal(t, "fn", fields["name"])
	// Neither field present means the server keeps the function's stored scope.
	assert.NotContains(t, fields, "variable_scope")
	assert.NotContains(t, fields, "variables")
}

func TestBuildFunctionDeployMultipartSendsScopedDeclaration(t *testing.T) {
	scope := "scoped"
	variables := []string{"API_KEY", "DB_URL"}
	body, contentType, err := buildFunctionDeployMultipart(FunctionDeployInput{
		Name:          "fn",
		Runtime:       "nodejs24.x",
		Handler:       "handler",
		SourceArchive: []byte("archive"),
		VariableScope: &scope,
		Variables:     &variables,
	})
	require.NoError(t, err)

	fields := parseMultipartFields(t, body.Bytes(), contentType)
	assert.Equal(t, "scoped", fields["variable_scope"])
	// This endpoint takes the names as a JSON-encoded string.
	assert.JSONEq(t, `["API_KEY","DB_URL"]`, fields["variables"])
}

func TestBuildFunctionDeployMultipartSendsEmptyDeclaredList(t *testing.T) {
	scope := "scoped"
	variables := []string{}
	body, contentType, err := buildFunctionDeployMultipart(FunctionDeployInput{
		Name:          "fn",
		Runtime:       "nodejs24.x",
		SourceArchive: []byte("archive"),
		VariableScope: &scope,
		Variables:     &variables,
	})
	require.NoError(t, err)

	fields := parseMultipartFields(t, body.Bytes(), contentType)
	assert.Equal(t, "scoped", fields["variable_scope"])
	// An empty list is not the same as an omitted one: it selects detected names only.
	assert.Equal(t, "[]", fields["variables"])
}

func TestBuildFunctionsBatchMultipartOmitsVariableScopeWhenUnset(t *testing.T) {
	body, contentType, err := buildFunctionsBatchMultipart([]FunctionDeployInput{
		{Name: "a", Runtime: "nodejs24.x", Handler: "handler", SourceArchive: []byte("a")},
	})
	require.NoError(t, err)

	fields := parseMultipartFields(t, body.Bytes(), contentType)
	var manifest []map[string]any
	require.NoError(t, json.Unmarshal([]byte(fields["functions"]), &manifest))
	require.Len(t, manifest, 1)
	assert.Equal(t, "a", manifest[0]["name"])
	assert.Equal(t, "code_0", manifest[0]["file_field"])
	assert.NotContains(t, manifest[0], "variable_scope")
	assert.NotContains(t, manifest[0], "variables")
}

func TestBuildFunctionsBatchMultipartSendsPerFunctionDeclaration(t *testing.T) {
	scoped := "scoped"
	all := "all"
	variables := []string{"API_KEY"}
	body, contentType, err := buildFunctionsBatchMultipart([]FunctionDeployInput{
		{Name: "scoped-fn", Runtime: "nodejs24.x", SourceArchive: []byte("a"), VariableScope: &scoped, Variables: &variables},
		{Name: "all-fn", Runtime: "nodejs24.x", SourceArchive: []byte("b"), VariableScope: &all},
		{Name: "plain-fn", Runtime: "nodejs24.x", SourceArchive: []byte("c")},
	})
	require.NoError(t, err)

	fields := parseMultipartFields(t, body.Bytes(), contentType)
	var manifest []map[string]any
	require.NoError(t, json.Unmarshal([]byte(fields["functions"]), &manifest))
	require.Len(t, manifest, 3)

	assert.Equal(t, "scoped", manifest[0]["variable_scope"])
	// The batch manifest nests the names as a real JSON array, not a string.
	assert.Equal(t, []any{"API_KEY"}, manifest[0]["variables"])

	assert.Equal(t, "all", manifest[1]["variable_scope"])
	assert.NotContains(t, manifest[1], "variables")

	// A function the manifest did not declare sends neither field.
	assert.NotContains(t, manifest[2], "variable_scope")
	assert.NotContains(t, manifest[2], "variables")
}
