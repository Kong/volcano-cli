package symlink

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func skipWithoutSymlinks(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs a symlink")
	}
}

func TestRefuseASymlinkedFile(t *testing.T) {
	skipWithoutSymlinks(t)
	root := t.TempDir()
	path := filepath.Join(root, "volcano", "cloud.env")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "config.json"), path))

	err := Refuse(root, filepath.Join("volcano", "cloud.env"))
	require.EqualError(t, err, path+" is a symlink, and the CLI doesn't write through one. Remove it and try again")
}

func TestRefuseAFileInASymlinkedDirectory(t *testing.T) {
	skipWithoutSymlinks(t)
	root := t.TempDir()
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(root, "volcano")))

	err := Refuse(root, filepath.Join("volcano", "cloud.env"))
	require.EqualError(t, err, filepath.Join(root, "volcano", "cloud.env")+" is inside "+filepath.Join(root, "volcano")+
		", a symlink, and the CLI doesn't write through one. Remove the symlink and try again")
}

func TestRefuseAllowsARegularOrMissingFile(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("x\n"), 0o600))

	assert.NoError(t, Refuse(root, ".gitignore"))
	assert.NoError(t, Refuse(root, filepath.Join("volcano", "cloud.env")))
}

func TestRefuseAllowsASymlinkedRoot(t *testing.T) {
	skipWithoutSymlinks(t)
	root := filepath.Join(t.TempDir(), "app")
	require.NoError(t, os.Symlink(t.TempDir(), root))

	assert.NoError(t, Refuse(root, "AGENTS.md"))
}
