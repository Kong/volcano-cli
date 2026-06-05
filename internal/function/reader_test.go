package function

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadDirectoryWithMatcherIncludesDoubleDotNames(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "..data"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "schema..json"), []byte("schema"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "..data", "config.json"), []byte("config"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "after.js"), []byte("after"), 0o644))

	files, err := readDirectoryWithMatcher(root, root, nil)
	require.NoError(t, err)

	assert.Equal(t, []byte("schema"), files["schema..json"])
	assert.Equal(t, []byte("config"), files[filepath.Join("..data", "config.json")])
	assert.Equal(t, []byte("after"), files["after.js"])
}
