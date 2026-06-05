package archive

import (
	"bytes"
	"io"
	"mime/multipart"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatSize(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{in: 0, want: "0B"},
		{in: -5, want: "0B"},
		{in: 1, want: "1B"},
		{in: 1023, want: "1023B"},
		{in: 1024, want: "1KB"},
		{in: 1536, want: "1.5KB"},
		{in: 1024 * 1024, want: "1MB"},
		{in: 5 * 1024 * 1024, want: "5MB"},
		{in: 1024 * 1024 * 1024, want: "1GB"},
		{in: 1024 * 1024 * 1024 * 1024, want: "1TB"},
		{in: 5 * 1024 * 1024 * 1024 * 1024, want: "5TB"},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, FormatSize(tc.in), "FormatSize(%d)", tc.in)
	}
}

func TestWriteArchivePart(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	require.NoError(t, WriteArchivePart(writer, "archive", "web", []byte("payload")))
	require.NoError(t, writer.Close())

	reader := multipart.NewReader(body, writer.Boundary())
	part, err := reader.NextPart()
	require.NoError(t, err)
	assert.Equal(t, "archive", part.FormName())
	assert.Equal(t, "web.tar.gz", part.FileName())
	contents, err := io.ReadAll(part)
	require.NoError(t, err)
	assert.Equal(t, []byte("payload"), contents)
}
