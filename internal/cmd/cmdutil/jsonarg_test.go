package cmdutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseJSONObjectRejectsNull(t *testing.T) {
	_, err := ParseJSONObject("input", "null")
	require.ErrorContains(t, err, "input must be a JSON object")
}

func TestParseJSONObjectRejectsInvalidFiles(t *testing.T) {
	dir := t.TempDir()
	large := filepath.Join(dir, "large.json")
	require.NoError(t, os.WriteFile(large, []byte(`{"value":"`+strings.Repeat("x", 256*1024)+`"}`), 0o600))

	for _, tc := range []struct {
		path string
		want string
	}{
		{path: dir, want: "must be a regular file"},
		{path: large, want: "exceeds 256 KiB"},
	} {
		_, err := ParseJSONObject("input", tc.path)
		require.ErrorContains(t, err, tc.want)
	}
}
