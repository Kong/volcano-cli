package api

import (
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readFunctionCandidateForm parses what buildFunctionCandidateMultipart wrote:
// its plain fields and each file part's name and content.
func readFunctionCandidateForm(t *testing.T, input FunctionCandidateInput) (map[string]string, map[string]string, map[string][]byte) {
	t.Helper()
	body, contentType, err := buildFunctionCandidateMultipart(input)
	require.NoError(t, err)
	mediaType, params, err := mime.ParseMediaType(contentType)
	require.NoError(t, err)
	require.Equal(t, "multipart/form-data", mediaType)
	reader := multipart.NewReader(body, params["boundary"])
	fields, filenames, files := map[string]string{}, map[string]string{}, map[string][]byte{}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		data, err := io.ReadAll(part)
		require.NoError(t, err)
		if part.FileName() != "" {
			filenames[part.FormName()] = part.FileName()
			files[part.FormName()] = data
			continue
		}
		fields[part.FormName()] = string(data)
	}
	return fields, filenames, files
}

func TestFunctionCandidateMultipartSendsTheNameTTLAndSource(t *testing.T) {
	fields, filenames, files := readFunctionCandidateForm(t, FunctionCandidateInput{
		Name: "checkout-v2", TTLSeconds: 3600, Source: []byte("archive"),
	})

	assert.Equal(t, map[string]string{"name": "checkout-v2", "ttl_seconds": "3600"}, fields)
	assert.Equal(t, "checkout-v2.tar.gz", filenames["code"])
	assert.Equal(t, []byte("archive"), files["code"])
}

func TestFunctionCandidateMultipartForARedeploySendsOnlyTheSource(t *testing.T) {
	fields, filenames, files := readFunctionCandidateForm(t, FunctionCandidateInput{Source: []byte("archive")})

	assert.Empty(t, fields, "no name, and no ttl_seconds, which would keep the preview until deleted")
	assert.Equal(t, "function.tar.gz", filenames["code"])
	assert.Equal(t, []byte("archive"), files["code"])
}
