package frontend

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeAppRoot(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "empty", input: ""},
		{name: "dot", input: "."},
		{name: "single segment", input: "apps", want: "apps"},
		{name: "nested", input: "apps/web", want: "apps/web"},
		{name: "absolute", input: "/apps/web", wantErr: "relative path"},
		{name: "leading slash", input: "/apps", wantErr: "relative path"},
		{name: "double dot", input: "apps/../web", wantErr: "normalized relative path"},
		{name: "trailing slash", input: "apps/", wantErr: "normalized relative path"},
		{name: "windows separator", input: `apps\web`, wantErr: "POSIX"},
		{name: "control char", input: "apps/we\x00b", wantErr: "control or non-printable"},
		{name: "ascii space", input: "apps web", wantErr: "whitespace"},
		{name: "nbsp", input: "apps/ web", wantErr: "whitespace"},
		{name: "zwsp", input: "apps/\u200bweb", wantErr: "control or non-printable"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeAppRoot(tc.input)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestValidateAppRootExists(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "apps", "web"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "apps", "note.txt"), []byte("hi"), 0o644))

	t.Run("empty app root is fine", func(t *testing.T) {
		assert.NoError(t, ValidateAppRootExists(root, ""))
	})

	t.Run("existing directory", func(t *testing.T) {
		assert.NoError(t, ValidateAppRootExists(root, "apps/web"))
	})

	t.Run("missing directory", func(t *testing.T) {
		err := ValidateAppRootExists(root, "apps/missing")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not exist")
	})

	t.Run("points at file", func(t *testing.T) {
		err := ValidateAppRootExists(root, "apps/note.txt")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must point to a directory")
	})

	t.Run("missing path with app root", func(t *testing.T) {
		err := ValidateAppRootExists("", "apps/web")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--path is required")
	})
}
