package ignore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatcherAppliesDefaultsExtrasAndGitignore(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("secret/\n*.local\n"), 0o644))

	m, err := NewProjectMatcher(root, "node_modules", "dist")
	require.NoError(t, err)

	t.Run("default pattern", func(t *testing.T) {
		assert.True(t, m.ShouldIgnore(".git", true))
		assert.True(t, m.ShouldIgnore("a/b/.DS_Store", false))
	})

	t.Run("extra pattern", func(t *testing.T) {
		assert.True(t, m.ShouldIgnore("node_modules", true))
		assert.True(t, m.ShouldIgnore("packages/dist", true))
		assert.False(t, m.ShouldIgnore("packages/keep", true))
	})

	t.Run("gitignore", func(t *testing.T) {
		assert.True(t, m.ShouldIgnore("secret", true))
		assert.True(t, m.ShouldIgnore("config.local", false))
		assert.False(t, m.ShouldIgnore("config.json", false))
	})

	t.Run("nil matcher is no-op", func(t *testing.T) {
		var nilMatcher *Matcher
		assert.False(t, nilMatcher.ShouldIgnore("anything", false))
	})
}

func TestMatcherWithoutGitignore(t *testing.T) {
	root := t.TempDir()
	m, err := NewProjectMatcher(root)
	require.NoError(t, err)
	assert.False(t, m.ShouldIgnore("hello.js", false))
	assert.True(t, m.ShouldIgnore(".git", true))
}

func TestShouldIgnoreWithDifferingPaths(t *testing.T) {
	// gitignore should match against the project-relative path; pattern
	// matching uses the archive-relative path. They differ when archiving
	// shared resources from outside the walked tree.
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("excluded/\n"), 0o644))

	m, err := NewProjectMatcher(root, "node_modules")
	require.NoError(t, err)

	// Pattern match wins even though gitignore would not catch this.
	assert.True(t, m.ShouldIgnoreWith("node_modules/util", "shared/util", false))
	// Gitignore catches it via the project path.
	assert.True(t, m.ShouldIgnoreWith("lib/util", "excluded/util", true))
	// Neither matches.
	assert.False(t, m.ShouldIgnoreWith("lib/util", "lib/util", false))
}

func TestProjectRelPath(t *testing.T) {
	root := t.TempDir()
	m, err := NewProjectMatcher(root)
	require.NoError(t, err)
	rel, err := m.ProjectRelPath(filepath.Join(root, "apps", "web"))
	require.NoError(t, err)
	assert.Equal(t, "apps/web", rel)
}
